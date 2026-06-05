package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"leadingAgent/agent/foundation"
	"leadingAgent/agent/tools"
)

func createTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "test_agent_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		t.Fatalf("failed to write temp file: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestAct_ToolFound(t *testing.T) {
	ctx := context.Background()
	tempFile := createTempFile(t, "hello world from test")

	agent := NewAgent(tools.NewReadFileTool())

	result := agent.act(ctx, &foundation.ToolUseContent{
		Name: "read_file",
		Input: map[string]interface{}{
			"path": tempFile,
		},
	})

	if !result.IsSuccess() {
		t.Fatalf("expected success, got: %s", result.GetMessage())
	}
}

func TestAct_ToolNotFound(t *testing.T) {
	ctx := context.Background()
	agent := NewAgent()

	result := agent.act(ctx, &foundation.ToolUseContent{
		Name:  "nonexistent",
		Input: map[string]interface{}{},
	})

	if result.IsSuccess() {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestExecute_ReActWithReadFile(t *testing.T) {
	tempFile := createTempFile(t, "test content line 1\ntest content line 2\nhello world")

	agent := NewAgent(tools.NewReadFileTool())

	callCount := 0
	agent.modelCaller = func(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
		callCount++
		if callCount == 1 {
			return &foundation.Message{
				Role:    foundation.RoleAssistant,
				Content: "Let me read the file.",
				ToolCall: &foundation.ToolUseContent{
					Type: "tool_use",
					ID:   "call_test_001",
					Name: "read_file",
					Input: map[string]interface{}{
						"path": tempFile,
					},
				},
			}, nil
		}
		return &foundation.Message{
			Role:    foundation.RoleAssistant,
			Content: "I have read the file. The content is: test content line 1, test content line 2, hello world.",
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := agent.Execute(ctx, &foundation.Model{Name: "deepseek-chat"}, "read my test file")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 model calls (think + final), got %d", callCount)
	}
}

func TestExecute_ReActWithBash(t *testing.T) {
	agent := NewAgent(tools.NewBashTool())

	callCount := 0
	agent.modelCaller = func(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
		callCount++
		if callCount == 1 {
			return &foundation.Message{
				Role:    foundation.RoleAssistant,
				Content: "I'll run the echo command.",
				ToolCall: &foundation.ToolUseContent{
					Type: "tool_use",
					ID:   "call_test_002",
					Name: "bash",
					Input: map[string]interface{}{
						"command": "echo hello from agent test",
					},
				},
			}, nil
		}
		return &foundation.Message{
			Role:    foundation.RoleAssistant,
			Content: "The bash command executed successfully. Output: hello from agent test",
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := agent.Execute(ctx, &foundation.Model{Name: "deepseek-chat"}, "run echo command")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 model calls, got %d", callCount)
	}
}

func TestExecute_ContextCancellation(t *testing.T) {
	agent := NewAgent(tools.NewBashTool())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := agent.Execute(ctx, &foundation.Model{Name: "deepseek-chat"}, "test query")

	if err == nil {
		t.Fatal("expected context canceled error")
	}
	if err != context.Canceled {
		t.Fatalf("expected Canceled, got: %v", err)
	}
}
