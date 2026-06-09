package handlers

import (
	"context"
	"log"

	"leadingAgent/agent"
	"leadingAgent/agent/foundation"
	"leadingAgent/session"
)

type ChatRequest struct {
	Message   string `json:"message"`
	SessionId string `json:"sessionId,omitempty"`
	UserId    string `json:"userId,omitempty"`
}

type ChatResponse struct {
	Response         string `json:"response"`
	SessionId        string `json:"sessionId,omitempty"`
	Success          bool   `json:"success"`
	Error            string `json:"error,omitempty"`
	PromptTokens     int32  `json:"promptTokens,omitempty"`
	CompletionTokens int32  `json:"completionTokens,omitempty"`
	TotalTokens      int32  `json:"totalTokens,omitempty"`
}

type StreamChatResponse struct {
	Response         string `json:"response"`
	SessionId        string `json:"sessionId,omitempty"`
	IsLast           bool   `json:"isLast"`
	Success          bool   `json:"success"`
	Error            string `json:"error,omitempty"`
	PromptTokens     int32  `json:"promptTokens,omitempty"`
	CompletionTokens int32  `json:"completionTokens,omitempty"`
	TotalTokens      int32  `json:"totalTokens,omitempty"`
}

// AgentHandler 对外暴露的对话接口，现在依赖 session.Manager 保留上下文。
type AgentHandler struct {
	agent    *agent.Agent
	model    *foundation.Model
	sessions *session.Manager
	logger   *log.Logger
}

func NewAgentHandler(a *agent.Agent, m *foundation.Model, sm *session.Manager) *AgentHandler {
	if sm == nil {
		// 退化到无 session 模式，保证初始化安全
		sm = session.NewManager(session.NewInMemoryRepository())
	}
	return &AgentHandler{
		agent:    a,
		model:    m,
		sessions: sm,
		logger:   log.Default(),
	}
}

// Chat 单轮问答，使用 session 保存对话历史。
func (h *AgentHandler) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if req == nil || req.Message == "" {
		return &ChatResponse{Success: false, Error: "empty message"}, nil
	}

	sess, err := h.sessions.GetOrCreate(ctx, req.SessionId, req.UserId)
	if err != nil {
		h.logger.Printf("[AgentHandler] session error: %v", err)
		return &ChatResponse{Success: false, Error: "session unavailable"}, nil
	}

	// 把历史消息拼到 agent 输入。
	userMsg := foundation.Message{Role: foundation.RoleUser, Content: req.Message}
	updated, err := h.sessions.Append(ctx, sess.ID, req.UserId, userMsg)
	if err != nil {
		h.logger.Printf("[AgentHandler] append user msg: %v", err)
		return &ChatResponse{SessionId: sess.ID, Success: false, Error: err.Error()}, nil
	}

	// 当前用户消息已经 Append 进 session，取 Messages[:len-1] 作为"不含本轮 user"的历史；
	// 并把当前 userMsg 以独立参数形式传入，避免重复。
	var history []foundation.Message
	if n := len(updated.Messages); n > 1 {
		history = updated.Messages[:n-1]
	}
	response, runErr := h.agent.Execute(ctx, h.model, updated.SystemPrompt, history, req.Message)

	if runErr != nil {
		_ = h.sessions.RecordError(ctx, updated.ID, req.UserId, 3)
		return &ChatResponse{SessionId: updated.ID, Success: false, Error: runErr.Error()}, nil
	}

	assistantMsg := foundation.Message{Role: foundation.RoleAssistant, Content: response}
	if _, err := h.sessions.Append(ctx, updated.ID, req.UserId, assistantMsg); err != nil {
		h.logger.Printf("[AgentHandler] append assistant msg: %v", err)
	}

	return &ChatResponse{
		Response:  response,
		SessionId: updated.ID,
		Success:   true,
	}, nil
}

// StreamChat 流式对话；同样走 session 保留上下文。
func (h *AgentHandler) StreamChat(ctx context.Context, req *ChatRequest, onEvent agent.StreamCallback) error {
	if req == nil || req.Message == "" {
		if onEvent != nil {
			_ = onEvent(agent.StreamEvent{Type: agent.StreamEventDone, Content: "empty message"})
		}
		return nil
	}

	sess, err := h.sessions.GetOrCreate(ctx, req.SessionId, req.UserId)
	if err != nil {
		return err
	}
	userMsg := foundation.Message{Role: foundation.RoleUser, Content: req.Message}
	updated, err := h.sessions.Append(ctx, sess.ID, req.UserId, userMsg)
	if err != nil {
		return err
	}

	var assistantContent string
	wrap := func(event agent.StreamEvent) error {
		if event.Type == agent.StreamEventTextDelta {
			assistantContent += event.Content
		}
		if onEvent != nil {
			return onEvent(event)
		}
		return nil
	}

	// 同上：当前 userMsg 已进入 session.Messages 最后一条，喂历史时排除。
	var history []foundation.Message
	if n := len(updated.Messages); n > 1 {
		history = updated.Messages[:n-1]
	}

	if err := h.agent.ExecuteStreaming(ctx, h.model, updated.SystemPrompt, history, req.Message, wrap); err != nil {
		_ = h.sessions.RecordError(ctx, updated.ID, req.UserId, 3)
		return err
	}

	if assistantContent != "" {
		assistantMsg := foundation.Message{Role: foundation.RoleAssistant, Content: assistantContent}
		_, _ = h.sessions.Append(ctx, updated.ID, req.UserId, assistantMsg)
	}
	return nil
}
