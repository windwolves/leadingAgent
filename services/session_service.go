package services

import (
	"context"
	"time"

	"leadingAgent/agent/foundation"
	"leadingAgent/session"
)

// SessionSummary 会话列表项（对外展示用）。
type SessionSummary struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	State     string `json:"state"`
	UpdatedAt string `json:"updated_at"`
	CreatedAt string `json:"created_at"`
	Preview   string `json:"preview"`
}

// MessageItem 历史消息条目（对外展示用）。
type MessageItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SessionService 封装会话生命周期管理，对上层屏蔽 session.Manager 和存储细节。
type SessionService struct {
	mgr *session.Manager
}

// NewSessionService 创建 SessionService。
func NewSessionService(mgr *session.Manager) *SessionService {
	if mgr == nil {
		mgr = session.NewManager(session.NewInMemoryRepository())
	}
	return &SessionService{mgr: mgr}
}

// GetOrCreate 获取或创建会话，返回完整 Session 供业务层使用。
func (s *SessionService) GetOrCreate(ctx context.Context, id, userID string) (*session.Session, error) {
	return s.mgr.GetOrCreate(ctx, id, userID)
}

// AppendUserMessage 追加用户消息并返回更新后的 Session。
func (s *SessionService) AppendUserMessage(ctx context.Context, id, userID, content string) (*session.Session, error) {
	msg := foundation.Message{Role: foundation.RoleUser, Content: content}
	return s.mgr.Append(ctx, id, userID, msg)
}

// AppendAssistantMessage 追加 assistant 消息（仅落盘，不返回 Session）。
func (s *SessionService) AppendAssistantMessage(ctx context.Context, id, userID, content string) {
	if content == "" {
		return
	}
	msg := foundation.Message{Role: foundation.RoleAssistant, Content: content}
	_, _ = s.mgr.Append(ctx, id, userID, msg)
}

// RecordError 记录会话错误，超过阈值自动置为 ERROR 状态。
func (s *SessionService) RecordError(ctx context.Context, id, userID string, threshold int) {
	_ = s.mgr.RecordError(ctx, id, userID, threshold)
}

// ListSessions 返回用户的会话列表，按更新时间倒序。
func (s *SessionService) ListSessions(ctx context.Context, userID string, limit int) ([]SessionSummary, error) {
	list, err := s.mgr.ListByUser(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SessionSummary, 0, len(list))
	for _, sess := range list {
		preview := ""
		if len(sess.Messages) > 0 {
			last := sess.Messages[len(sess.Messages)-1]
			p := last.Content
			if len(p) > 60 {
				p = p[:60] + "..."
			}
			preview = p
		}
		out = append(out, SessionSummary{
			ID:        sess.ID,
			UserID:    sess.UserID,
			State:     string(sess.State),
			UpdatedAt: sess.UpdatedAt.UTC().Format(time.RFC3339),
			CreatedAt: sess.CreatedAt.UTC().Format(time.RFC3339),
			Preview:   preview,
		})
	}
	return out, nil
}

// DeleteSession 删除指定会话。
func (s *SessionService) DeleteSession(ctx context.Context, sessionID, userID string) error {
	return s.mgr.Delete(ctx, sessionID, userID)
}

// GetMessages 返回会话的历史消息列表。
func (s *SessionService) GetMessages(ctx context.Context, sessionID, userID string) ([]MessageItem, error) {
	sess, err := s.mgr.Get(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]MessageItem, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		out = append(out, MessageItem{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	return out, nil
}
