package handlers

import (
	"context"
	"fmt"

	"leadingAgent/config"
	"leadingAgent/models"
	"leadingAgent/models/deepseek"
	"leadingAgent/repository"
	"leadingAgent/services"

	"github.com/apache/thrift/lib/go/thrift"
)

type LdAgentHandler struct {
	client   *deepseek.Client
	costRepo repository.CostRepository
	model    string
	apiURL   string
}

func NewLdAgentHandler(cfg *config.Config) (*LdAgentHandler, error) {
	client := deepseek.NewClient(cfg.DeepSeekAPIKey, cfg.DeepSeekAPIURL, cfg.DeepSeekModel)

	costRepo, err := repository.NewCostRepository("cost.db")
	if err != nil {
		return nil, err
	}

	return &LdAgentHandler{
		client:   client,
		costRepo: costRepo,
		model:    cfg.DeepSeekModel,
		apiURL:   cfg.DeepSeekAPIURL,
	}, nil
}

func (h *LdAgentHandler) Chat(ctx context.Context, request *services.ChatRequest) (*services.ChatResponse, error) {
	messages := []deepseek.Message{
		{
			Role:    "system",
			Content: "You are a helpful assistant.",
		},
		{
			Role:    "user",
			Content: request.Message,
		},
	}

	resp, err := h.client.Chat(messages, nil, 4096, "low")
	if err != nil {
		return &services.ChatResponse{
			Response: "",
			Success:  false,
			Error:    err.Error(),
		}, nil
	}

	if len(resp.Choices) > 0 {
		fullResponse := resp.Choices[0].Message.Content
		if resp.Choices[0].Message.ReasoningContent != "" {
			// V4 模型会有推理内容，拼接在正式回答前
			fullResponse = resp.Choices[0].Message.ReasoningContent + "\n\n" + resp.Choices[0].Message.Content
		}
		response := &services.ChatResponse{
			Response:         fullResponse,
			SessionId:        request.SessionId,
			Success:          true,
			Error:            "",
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}

		go func() {
			cost := &models.TokenCost{
				RequestID:        fmt.Sprintf("%d", ctx.Value("request_id")),
				Provider:         "deepseek",
				Model:            h.model,
				RequestType:      "chat",
				Endpoint:         h.apiURL,
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
				CacheHit:         false,
				CacheReadTokens:  0,
			}
			h.costRepo.Save(cost)
		}()

		return response, nil
	}

	return &services.ChatResponse{
		Response: "",
		Success:  false,
		Error:    "No response from DeepSeek API",
	}, nil
}

func (h *LdAgentHandler) StreamChat(ctx context.Context, request *services.ChatRequest, sender func(*services.StreamChatResponse) error) error {
	messages := []deepseek.Message{
		{
			Role:    "system",
			Content: "You are a helpful assistant.",
		},
		{
			Role:    "user",
			Content: request.Message,
		},
	}

	var promptTokens, completionTokens, totalTokens int

	err := h.client.StreamChat(messages, nil, 4096, "low", func(streamResp *deepseek.StreamChatResponse) error {
		isLast := false
		content := ""

		if len(streamResp.Choices) > 0 {
			reasoning := streamResp.Choices[0].Delta.ReasoningContent
			content = streamResp.Choices[0].Delta.Content
			if reasoning != "" {
				// 将推理内容通过 sender 发送为额外的 text delta
				if rerr := sender(&services.StreamChatResponse{
					Response: reasoning,
					SessionId: request.SessionId,
					IsLast:   false,
					Success:  true,
				}); rerr != nil {
					return rerr
				}
			}
			if streamResp.Choices[0].FinishReason != "" {
				isLast = true
			}
		}

		if streamResp.Usage != nil {
			promptTokens = streamResp.Usage.PromptTokens
			completionTokens = streamResp.Usage.CompletionTokens
			totalTokens = streamResp.Usage.TotalTokens
		}

		return sender(&services.StreamChatResponse{
			Response:         content,
			SessionId:        request.SessionId,
			IsLast:           isLast,
			Success:          true,
			Error:            "",
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		})
	})

	if err != nil {
		return err
	}

	go func() {
		cost := &models.TokenCost{
			RequestID:        fmt.Sprintf("%d", ctx.Value("request_id")),
			Provider:         "deepseek",
			Model:            h.model,
			RequestType:      "stream_chat",
			Endpoint:         h.apiURL,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			CacheHit:         false,
			CacheReadTokens:  0,
		}
		h.costRepo.Save(cost)
	}()

	return nil
}

func (h *LdAgentHandler) Processor() thrift.TProcessor {
	return services.NewLdAgentServiceProcessor(h)
}
