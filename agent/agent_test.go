package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"leadingAgent/agent/foundation"
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

	agent := NewAgent()

	result := agent.act(ctx, foundation.ToolUseContent{
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

	result := agent.act(ctx, foundation.ToolUseContent{
		Name:  "nonexistent",
		Input: map[string]interface{}{},
	})

	if result.IsSuccess() {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestExecute_ReActWithReadFile(t *testing.T) {
	tempFile := createTempFile(t, "test content line 1\ntest content line 2\nhello world")

	agent := NewAgent()

	callCount := 0
	agent.modelCaller = func(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
		callCount++
		if callCount == 1 {
			return &foundation.Message{
				Role:    foundation.RoleAssistant,
				Content: "Let me read the file.",
				ToolCalls: []foundation.ToolUseContent{
					{
						Type: "tool_use",
						ID:   "call_test_001",
						Name: "read_file",
						Input: map[string]interface{}{
							"path": tempFile,
						},
					},
				},
			}, nil
		}
		return &foundation.Message{
			Role:    foundation.RoleAssistant,
			Content: "I have read the file successfully.",
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, _, err := agent.Execute(ctx, &foundation.Model{Name: "deepseek-chat"}, "", nil, "read my test file")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if resp != "I have read the file successfully." {
		t.Fatalf("expected final response, got: %q", resp)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 model calls (think + final), got %d", callCount)
	}
}

func TestExecute_ReActWithBash(t *testing.T) {
	agent := NewAgent()

	callCount := 0
	agent.modelCaller = func(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
		callCount++
		if callCount == 1 {
			return &foundation.Message{
				Role:    foundation.RoleAssistant,
				Content: "I'll run the echo command.",
				ToolCalls: []foundation.ToolUseContent{
					{
						Type: "tool_use",
						ID:   "call_test_002",
						Name: "bash",
						Input: map[string]interface{}{
							"command": "echo hello from agent test",
						},
					},
				},
			}, nil
		}
		return &foundation.Message{
			Role:    foundation.RoleAssistant,
			Content: "The bash command executed successfully.",
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, _, err := agent.Execute(ctx, &foundation.Model{Name: "deepseek-chat"}, "", nil, "run echo command")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if resp != "The bash command executed successfully." {
		t.Fatalf("expected final response, got: %q", resp)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 model calls, got %d", callCount)
	}
}

func TestExecute_ParallelTools(t *testing.T) {
	tempFile1 := createTempFile(t, "content of file one")
	tempFile2 := createTempFile(t, "content of file two")

	agent := NewAgent()

	agent.modelCaller = func(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
		return &foundation.Message{
			Role:    foundation.RoleAssistant,
			Content: "Let me read both files and run a command.",
			ToolCalls: []foundation.ToolUseContent{
				{
					Type: "tool_use",
					ID:   "call_read_1",
					Name: "read_file",
					Input: map[string]interface{}{
						"path": tempFile1,
					},
				},
				{
					Type: "tool_use",
					ID:   "call_bash_1",
					Name: "bash",
					Input: map[string]interface{}{
						"command": "echo parallel test",
					},
				},
				{
					Type: "tool_use",
					ID:   "call_read_2",
					Name: "read_file",
					Input: map[string]interface{}{
						"path": tempFile2,
					},
				},
			},
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	toolCalls := []foundation.ToolUseContent{
		{Type: "tool_use", ID: "call_read_1", Name: "read_file", Input: map[string]interface{}{"path": tempFile1}},
		{Type: "tool_use", ID: "call_bash_1", Name: "bash", Input: map[string]interface{}{"command": "echo parallel"}},
		{Type: "tool_use", ID: "call_read_2", Name: "read_file", Input: map[string]interface{}{"path": tempFile2}},
	}

	msgs := agent.executeParallel(ctx, toolCalls)

	if len(msgs) != 3 {
		t.Fatalf("expected 3 tool result messages, got %d", len(msgs))
	}

	for i, msg := range msgs {
		if msg.Role != foundation.RoleTool {
			t.Fatalf("message %d: expected RoleTool, got %s", i, msg.Role)
		}
		if msg.ToolResult == nil {
			t.Fatalf("message %d: ToolResult is nil", i)
		}
		if msg.ToolResult.ToolUseID != toolCalls[i].ID {
			t.Fatalf("message %d: expected ToolUseID=%s, got %s", i, toolCalls[i].ID, msg.ToolResult.ToolUseID)
		}
	}
}

func TestExecute_ContextCancellation(t *testing.T) {
	agent := NewAgent()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := agent.Execute(ctx, &foundation.Model{Name: "deepseek-chat"}, "", nil, "test query")

	if err == nil {
		t.Fatal("expected context canceled error")
	}
	if err != context.Canceled {
		t.Fatalf("expected Canceled, got: %v", err)
	}
}

func TestWithCostSaver_CalledOnAccumulateUsage(t *testing.T) {
	type call struct {
		caller, model, sessionID string
		prompt, completion       int
	}
	var calls []call
	done := make(chan struct{})

	a := NewAgent()
	a.SetSessionID("sess-123")
	a.WithCostSaver(func(caller, model, sessionID string, prompt, completion int) {
		calls = append(calls, call{caller, model, sessionID, prompt, completion})
		close(done)
	})

	a.accumulateUsage("callModel", "deepseek-chat", 100, 50)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CostSaver was not called within 1s")
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	c := calls[0]
	if c.caller != "callModel" {
		t.Errorf("caller: got %q, want %q", c.caller, "callModel")
	}
	if c.model != "deepseek-chat" {
		t.Errorf("model: got %q, want %q", c.model, "deepseek-chat")
	}
	if c.sessionID != "sess-123" {
		t.Errorf("sessionID: got %q, want %q", c.sessionID, "sess-123")
	}
	if c.prompt != 100 {
		t.Errorf("prompt: got %d, want 100", c.prompt)
	}
	if c.completion != 50 {
		t.Errorf("completion: got %d, want 50", c.completion)
	}
}

func TestWithCostSaver_NotCalledWhenNil(t *testing.T) {
	a := NewAgent()
	// No CostSaver set — accumulateUsage must not panic.
	a.accumulateUsage("callModel", "deepseek-chat", 10, 5)

	// Give the goroutine scheduler a moment; no crash = pass.
	time.Sleep(10 * time.Millisecond)
}

func TestAccumulateUsage_CumulatesCorrectly(t *testing.T) {
	a := NewAgent()
	a.accumulateUsage("callModel", "deepseek-chat", 100, 50)
	a.accumulateUsage("critic", "deepseek-chat", 30, 20)

	u := a.UsageSummary()
	if u.PromptTokens != 130 {
		t.Errorf("PromptTokens: got %d, want 130", u.PromptTokens)
	}
	if u.CompletionTokens != 70 {
		t.Errorf("CompletionTokens: got %d, want 70", u.CompletionTokens)
	}
	if u.TotalTokens != 200 {
		t.Errorf("TotalTokens: got %d, want 200", u.TotalTokens)
	}
}
