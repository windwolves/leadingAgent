package foundation

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	ID         string             `json:"id"`
	Role       Role               `json:"role"`
	Content    string             `json:"content,omitempty"`
	Reasoning  string             `json:"reasoning,omitempty"`
	ToolCalls  []ToolUseContent   `json:"tool_calls,omitempty"`
	ToolResult *ToolResultContent `json:"tool_result,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
}

// NewMessage creates a simple message with auto-generated ID and timestamp.
func NewMessage(role Role, content string) Message {
	return Message{
		ID:        uuid.NewString(),
		Role:      role,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
}

// NewMessageWithToolCalls creates a message with tool calls (auto ID + timestamp).
func NewMessageWithToolCalls(role Role, content string, toolCalls []ToolUseContent) Message {
	return Message{
		ID:        uuid.NewString(),
		Role:      role,
		Content:   content,
		ToolCalls: toolCalls,
		CreatedAt: time.Now().UTC(),
	}
}

type SystemMessage struct {
	Role    Role                 `json:"role"`
	Content SystemMessageContent `json:"content"`
}

func NewSystemMessage(content SystemMessageContent) *SystemMessage {
	return &SystemMessage{
		Role:    RoleSystem,
		Content: content,
	}
}

type UserMessage struct {
	Role    Role               `json:"role"`
	Content UserMessageContent `json:"content"`
}

func NewUserMessage(content UserMessageContent) *UserMessage {
	return &UserMessage{
		Role:    RoleUser,
		Content: content,
	}
}

type AssistantMessage struct {
	Role    Role                    `json:"role"`
	Content AssistantMessageContent `json:"content"`
}

func NewAssistantMessage(content AssistantMessageContent) *AssistantMessage {
	return &AssistantMessage{
		Role:    RoleAssistant,
		Content: content,
	}
}

type ToolMessage struct {
	Role    Role               `json:"role"`
	Content ToolMessageContent `json:"content"`
}

func NewToolMessage(content ToolMessageContent) *ToolMessage {
	return &ToolMessage{
		Role:    RoleTool,
		Content: content,
	}
}
