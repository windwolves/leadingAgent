package handlers

import (
	"context"
	"log"

	"leadingAgent/agent"
	"leadingAgent/agent/foundation"
)

type ChatRequest struct {
	Message   string `json:"message"`
	SessionId string `json:"sessionId,omitempty"`
}

type ChatResponse struct {
	Response        string `json:"response"`
	SessionId       string `json:"sessionId,omitempty"`
	Success         bool   `json:"success"`
	Error           string `json:"error,omitempty"`
	PromptTokens    int32  `json:"promptTokens,omitempty"`
	CompletionTokens int32 `json:"completionTokens,omitempty"`
	TotalTokens     int32  `json:"totalTokens,omitempty"`
}

type StreamChatResponse struct {
	Response        string `json:"response"`
	SessionId       string `json:"sessionId,omitempty"`
	IsLast          bool   `json:"isLast"`
	Success         bool   `json:"success"`
	Error           string `json:"error,omitempty"`
	PromptTokens    int32  `json:"promptTokens,omitempty"`
	CompletionTokens int32 `json:"completionTokens,omitempty"`
	TotalTokens     int32  `json:"totalTokens,omitempty"`
}

type AgentHandler struct {
	agent  *agent.Agent
	model  *foundation.Model
	logger *log.Logger
}

func NewAgentHandler(agent *agent.Agent, model *foundation.Model) *AgentHandler {
	return &AgentHandler{
		agent:  agent,
		model:  model,
		logger: log.Default(),
	}
}

func (h *AgentHandler) Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error) {
	h.logger.Printf("[AgentHandler] Chat request: message=%q sessionId=%q", request.Message, request.SessionId)

	response, err := h.agent.Execute(ctx, h.model, request.Message)
	if err != nil {
		h.logger.Printf("[AgentHandler] Chat error: %v", err)
		return &ChatResponse{
			SessionId: request.SessionId,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	return &ChatResponse{
		Response:  response,
		SessionId: request.SessionId,
		Success:   true,
	}, nil
}

func (h *AgentHandler) StreamChat(ctx context.Context, request *ChatRequest, onEvent agent.StreamCallback) error {
	h.logger.Printf("[AgentHandler] StreamChat request: message=%q sessionId=%q", request.Message, request.SessionId)
	return h.agent.ExecuteStreaming(ctx, h.model, request.Message, onEvent)
}
