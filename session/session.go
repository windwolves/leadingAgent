package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"leadingAgent/agent/foundation"
)

// 常见错误，供上层判断。
var (
	ErrNotFound = errors.New("session: session not found")
	ErrConflict = errors.New("session: version conflict")
	ErrExpired  = errors.New("session: session expired")
	ErrGone     = errors.New("session: session terminated")
	ErrInvalid  = errors.New("session: invalid argument")
)

// SessionState 描述会话生命周期状态。
type SessionState string

const (
	StateCreated   SessionState = "CREATED"
	StateActive    SessionState = "ACTIVE"
	StatePaused    SessionState = "PAUSED"
	StateCompleted SessionState = "COMPLETED"
	StateExpired   SessionState = "EXPIRED"
	StateError     SessionState = "ERROR"
	StateArchived  SessionState = "ARCHIVED"
)

// 可终结状态：不可再接收新消息。
func (s SessionState) IsTerminal() bool {
	switch s {
	case StateCompleted, StateExpired, StateError, StateArchived:
		return true
	}
	return false
}

// TokenUsage 记录一次或累计 token 使用量。
type TokenUsage struct {
	Prompt     int32 `json:"prompt_tokens"`
	Completion int32 `json:"completion_tokens"`
	Total      int32 `json:"total_tokens"`
}

// ToolCallMeta 工具调用审计元数据，便于审计和计费。
type ToolCallMeta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartedAt time.Time `json:"started_at"`
	LatencyMs int64     `json:"latency_ms"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
}

// ModelConfig 会话级别模型配置，支持每个 session 可独立覆盖。
type ModelConfig struct {
	Name        string                 `json:"name"`
	Provider    string                 `json:"provider"`
	Temperature float64                `json:"temperature,omitempty"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
}

// Session 是一条会话的完整状态。
type Session struct {
	ID           string                 `json:"id"`
	UserID       string                 `json:"user_id"`
	State        SessionState           `json:"state"`
	SystemPrompt string                 `json:"system_prompt"`
	Messages     []foundation.Message   `json:"messages"`
	ToolCalls    []ToolCallMeta         `json:"tool_calls"`
	ModelConfig  ModelConfig            `json:"model_config"`
	MetaData     map[string]interface{} `json:"meta,omitempty"`
	TokenUsage   TokenUsage             `json:"token_usage"`
	ErrorCount   int                    `json:"error_count"`
	MaxMessages  int                    `json:"max_messages"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	ExpiresAt    time.Time              `json:"expires_at"`
	Version      int64                  `json:"version"`
}

// NewID 生成 16 字节随机 ID（128bit，等价 ULID 的轻量替代）。
func NewID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// IsWritable 返回会话是否还能追加消息。
func (s *Session) IsWritable() bool {
	if s.State.IsTerminal() {
		return false
	}
	if s.MaxMessages > 0 && len(s.Messages) >= s.MaxMessages {
		return false
	}
	return true
}

// AppendMessage 追加消息并推进版本/时间戳。调用方必须已经持有写锁或依赖存储层乐观锁。
func (s *Session) AppendMessage(msgs ...foundation.Message) {
	s.Messages = append(s.Messages, msgs...)
	s.UpdatedAt = time.Now().UTC()
	s.Version++
}

// Touch 滑动刷新过期时间。
func (s *Session) Touch(ttl time.Duration) {
	s.ExpiresAt = time.Now().UTC().Add(ttl)
	s.UpdatedAt = s.ExpiresAt.Add(-ttl)
	s.Version++
}

// Repository 可插拔存储接口。
//
// 约定：
//   - Session 调用方会通过 AppendMessage / Touch 递增 Version，
//     Update 语义为"乐观锁写入新版本"，失败返回 ErrConflict 或 ErrNotFound。
//   - ListExpired 只返回未到终态且过期的会话，供后台 GC 处理。
//
// 实现：
//   - InMemoryRepository：纯内存（见 in_memory_repository.go）
//   - SQLiteRepository：落盘（见 sqlite_repository.go）
type Repository interface {
	Create(ctx context.Context, s *Session) error
	Get(ctx context.Context, id string) (*Session, error)
	Update(ctx context.Context, s *Session) error
	Delete(ctx context.Context, id string) error
	ListByUser(ctx context.Context, userID string, limit int) ([]*Session, error)
	ListExpired(ctx context.Context, before time.Time, limit int) ([]*Session, error)
	Touch(ctx context.Context, id string, newExpiresAt time.Time) error
}
