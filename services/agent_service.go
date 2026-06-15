package services

import (
	"context"
	"log"

	"leadingAgent/agent"
	"leadingAgent/agent/foundation"
)

// ChatResult 非流式对话的返回结果。
type ChatResult struct {
	Response  string
	SessionID string
}

// AgentService 封装 Agent 对话执行的核心业务逻辑，
// 会话生命周期管理委托给 SessionService，长期记忆委托给 MemoryService。
type AgentService struct {
	agent    *agent.Agent
	model    *foundation.Model
	sessions *SessionService
	memories *MemoryService
	logger   *log.Logger
}

// NewAgentService 创建 AgentService。
func NewAgentService(a *agent.Agent, m *foundation.Model, ss *SessionService, ms *MemoryService) *AgentService {
	return &AgentService{
		agent:    a,
		model:    m,
		sessions: ss,
		memories: ms,
		logger:   log.Default(),
	}
}

// StreamChat 流式对话，包含 session 生命周期管理 + Agent 执行。
// onEvent 由上层（handler）提供，负责将 StreamEvent 写入具体传输协议（如 SSE）。
func (s *AgentService) StreamChat(ctx context.Context, sessionID, userID, message string, onEvent agent.StreamCallback) error {
	sess, err := s.sessions.GetOrCreate(ctx, sessionID, userID)
	if err != nil {
		return err
	}

	// 1) 摘要注入：当新 session 替换了已过期的旧 session 时，用 LLM 生成旧对话摘要，注入 system prompt。
	// 这一步是 "best effort" — 摘要生成失败不阻塞主对话。
	prompt := sess.SystemPrompt
	if sess.MetaData != nil {
		if oldID, ok := sess.MetaData["replaced_from"].(string); ok && oldID != "" {
			if oldMsgs, mErr := s.sessions.GetSessionMessages(ctx, oldID, userID); mErr == nil && len(oldMsgs) > 0 {
				if summary, sErr := s.agent.Summarize(ctx, s.model, oldMsgs, 500); sErr == nil && summary != "" {
					s.logger.Printf("[AgentService] injecting session summary (session %s → %s): %s", oldID[:16], sess.ID, summary)
					prompt = prompt + "\n\n## 上一段对话的摘要\n" + summary
				} else if sErr != nil {
					s.logger.Printf("[AgentService] summary skipped: %v", sErr)
				}
			}
		}
	}

	updated, err := s.sessions.AppendUserMessage(ctx, sess.ID, userID, message)
	if err != nil {
		return err
	}

	// 2) 召回长期记忆，注入 system prompt
	if s.memories != nil {
		if memories := s.memories.Recall(ctx, userID, message, 5); memories != "" {
			s.logger.Printf("[AgentService] injecting long-term memory:\n%s", memories)
			prompt = prompt + "\n" + memories
		}
	}

	var assistantContent string
	wrap := func(event agent.StreamEvent) error {
		if event.Type == agent.StreamEventTextDelta {
			assistantContent += event.Content
		}
		if event.Type == agent.StreamEventDone {
			if sessionID != updated.ID {
				event.SessionID = updated.ID
			}
			if event.Content == "" {
				event.Content = assistantContent
			}
		}
		if onEvent != nil {
			return onEvent(event)
		}
		return nil
	}

	var history []foundation.Message
	if n := len(updated.Messages); n > 1 {
		history = updated.Messages[:n-1]
	}

	if err := s.agent.ExecuteStreaming(ctx, s.model, prompt, history, message, wrap); err != nil {
		s.sessions.RecordError(ctx, updated.ID, userID, 3)
		return err
	}

	if err := s.sessions.AppendAssistantMessage(ctx, updated.ID, userID, assistantContent); err != nil {
		s.logger.Printf("[AgentService] AppendAssistantMessage failed: %v", err)
	}
	return nil
}

// Chat 非流式对话，作为不支持 SSE 时的降级方案。
// 返回完整响应文本和新创建的 sessionID。
func (s *AgentService) Chat(ctx context.Context, sessionID, userID, message string) (*ChatResult, error) {
	sess, err := s.sessions.GetOrCreate(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}

	// 摘要注入：与 StreamChat 相同逻辑
	prompt := sess.SystemPrompt
	if sess.MetaData != nil {
		if oldID, ok := sess.MetaData["replaced_from"].(string); ok && oldID != "" {
			if oldMsgs, mErr := s.sessions.GetSessionMessages(ctx, oldID, userID); mErr == nil && len(oldMsgs) > 0 {
				if summary, sErr := s.agent.Summarize(ctx, s.model, oldMsgs, 500); sErr == nil && summary != "" {
					s.logger.Printf("[AgentService] injecting session summary (non-stream): %s", summary)
					prompt = prompt + "\n\n## 上一段对话的摘要\n" + summary
				}
			}
		}
	}

	updated, err := s.sessions.AppendUserMessage(ctx, sess.ID, userID, message)
	if err != nil {
		return nil, err
	}

	// 召回长期记忆，注入 system prompt
	if s.memories != nil {
		if memories := s.memories.Recall(ctx, userID, message, 5); memories != "" {
			s.logger.Printf("[AgentService] injecting long-term memory:\n%s", memories)
			prompt = prompt + "\n" + memories
		}
	}

	var history []foundation.Message
	if n := len(updated.Messages); n > 1 {
		history = updated.Messages[:n-1]
	}

	resp, runErr := s.agent.Execute(ctx, s.model, prompt, history, message)
	if runErr != nil {
		s.sessions.RecordError(ctx, updated.ID, userID, 3)
		return nil, runErr
	}

	if err := s.sessions.AppendAssistantMessage(ctx, updated.ID, userID, resp); err != nil {
		s.logger.Printf("[AgentService] AppendAssistantMessage failed: %v", err)
	}

	return &ChatResult{Response: resp, SessionID: updated.ID}, nil
}
