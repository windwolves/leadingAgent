package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"leadingAgent/agent/foundation"
	"leadingAgent/agent/tools"
	"leadingAgent/models/deepseek"
)

// StreamEvent types sent to the streaming callback.
const (
	StreamEventThinking   = "thinking"
	StreamEventTextDelta  = "text_delta"
	StreamEventToolCall   = "tool_call"
	StreamEventToolResult = "tool_result"
	StreamEventDone       = "done"
	StreamEventError      = "error"
)

// StreamEvent represents a single streaming event.
type StreamEvent struct {
	Type      string                 `json:"type"`
	Content   string                 `json:"content,omitempty"`
	Tool      string                 `json:"tool,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	Result    string                 `json:"result,omitempty"`
	Turns     int                    `json:"turns,omitempty"`
	SessionID string                 `json:"sessionId,omitempty"`
}

// StreamCallback is called for each streaming event.
type StreamCallback func(event StreamEvent) error

type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, input map[string]interface{}) tools.ToolResult
	InputSchema() map[string]interface{}
}

type Agent struct {
	tools       []Tool
	modelCaller func(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error)
	logger      *log.Logger
	logFile     *os.File
}

func NewAgent() *Agent {
	logDir := filepath.Join("log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("[Agent] warning: failed to create log directory %s: %v", logDir, err)
	}

	logFile, err := os.OpenFile(
		filepath.Join(logDir, fmt.Sprintf("agent-%s.log", time.Now().Format("2006-01-02"))),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644,
	)

	var logger *log.Logger
	if err == nil {
		// Write to both file and stdout
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		logger = log.New(multiWriter, "", log.LstdFlags)
	} else {
		logger = log.Default()
	}

	a := &Agent{
		tools:   loadTools(),
		logger:  logger,
		logFile: logFile,
	}
	a.modelCaller = a.callModel
	logger.Printf("[Agent] initialized with %d tools", len(a.tools))
	return a
}

func loadTools() []Tool {
	return []Tool{
		tools.NewBashTool(),
		tools.NewReadFileTool(),
		tools.NewWriteFileTool(),
		tools.NewStrReplaceTool(),
		tools.NewGlobSearchTool(),
		tools.NewGrepSearchTool(),
		tools.NewBingSearchTool(),
		tools.NewRememberFactTool(),
	}
}

// Close releases resources held by the Agent (e.g. log file handles).
func (a *Agent) Close() error {
	if a.logFile != nil {
		return a.logFile.Close()
	}
	return nil
}

const defaultSystemPrompt = "You are a helpful assistant. When you need information, use the available tools to search or take action. When you learn personal facts about the user, use the remember_fact tool to save them."

func (a *Agent) Execute(ctx context.Context, model *foundation.Model, systemPrompt string, history []foundation.Message, userQuery string) (string, error) {
	a.logger.Printf("[Agent] Execute: query=%q model=%s history=%d", userQuery, model.Name, len(history))
	messages := a.buildMessages(systemPrompt, history, userQuery)

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		assistantMsg, err := a.think(ctx, model, messages)
		if err != nil {
			return "", err
		}

		messages = append(messages, *assistantMsg)

		if len(assistantMsg.ToolCalls) > 0 {
			a.logger.Printf("[Agent] Think → %d tool call(s): %v", len(assistantMsg.ToolCalls), toolCallNames(assistantMsg.ToolCalls))
			toolMsgs := a.executeParallel(ctx, assistantMsg.ToolCalls)
			messages = append(messages, toolMsgs...)
		} else {
			a.logger.Printf("[Agent] Final response: %s", assistantMsg.Content)
			return assistantMsg.Content, nil
		}
	}
}

// ExecuteStreaming runs the ReAct loop with streaming callbacks for each event.
func (a *Agent) ExecuteStreaming(ctx context.Context, model *foundation.Model, systemPrompt string, history []foundation.Message, userQuery string, onEvent StreamCallback) error {
	a.logger.Printf("[Agent] ExecuteStreaming: query=%q model=%s history=%d", userQuery, model.Name, len(history))
	messages := a.buildMessages(systemPrompt, history, userQuery)

	turn := 0
	for {
		select {
		case <-ctx.Done():
			onEvent(StreamEvent{Type: StreamEventError, Content: ctx.Err().Error()})
			return ctx.Err()
		default:
		}

		turn++

		assistantMsg, err := a.thinkStreaming(ctx, model, messages, onEvent, turn)
		if err != nil {
			onEvent(StreamEvent{Type: StreamEventError, Content: err.Error()})
			return err
		}

		messages = append(messages, *assistantMsg)

		if len(assistantMsg.ToolCalls) > 0 {
			a.logger.Printf("[Agent] Think → %d tool call(s): %v", len(assistantMsg.ToolCalls), toolCallNames(assistantMsg.ToolCalls))
			for _, tc := range assistantMsg.ToolCalls {
				onEvent(StreamEvent{
					Type:  StreamEventToolCall,
					Tool:  tc.Name,
					Input: tc.Input,
				})
			}

			toolMsgs := a.executeParallel(ctx, assistantMsg.ToolCalls)
			for _, tm := range toolMsgs {
				onEvent(StreamEvent{
					Type:   StreamEventToolResult,
					Result: tm.ToolResult.Content,
				})
			}
			messages = append(messages, toolMsgs...)
		} else {
			a.logger.Printf("[Agent] Final response: %s", assistantMsg.Content)
			onEvent(StreamEvent{Type: StreamEventDone, Content: assistantMsg.Content, Turns: turn})
			return nil
		}
	}
}

// buildMessages 拼接 system prompt + 历史消息 + 当前用户消息，作为本轮推理的输入。
func (a *Agent) buildMessages(systemPrompt string, history []foundation.Message, userQuery string) []foundation.Message {
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	out := make([]foundation.Message, 0, len(history)+2)
	out = append(out, foundation.Message{Role: foundation.RoleSystem, Content: systemPrompt})
	out = append(out, history...)
	if userQuery != "" {
		out = append(out, foundation.Message{Role: foundation.RoleUser, Content: userQuery})
	}
	return out
}

func (a *Agent) think(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
	return a.modelCaller(ctx, model, messages)
}

func (a *Agent) thinkStreaming(ctx context.Context, model *foundation.Model, messages []foundation.Message, onEvent StreamCallback, turn int) (*foundation.Message, error) {
	onEvent(StreamEvent{Type: StreamEventThinking, Turns: turn})
	return a.callModelStreaming(ctx, model, messages, onEvent)
}

func (a *Agent) act(ctx context.Context, toolCall foundation.ToolUseContent) tools.ToolResult {
	for _, t := range a.tools {
		if t.Name() == toolCall.Name {
			return t.Execute(ctx, toolCall.Input)
		}
	}
	return tools.NewErrorResult(toolCall.Name, "function",
		fmt.Sprintf("tool %s not found", toolCall.Name),
		"TOOL_NOT_FOUND", 0, "工具未找到")
}

func (a *Agent) executeParallel(ctx context.Context, toolCalls []foundation.ToolUseContent) []foundation.Message {
	a.logger.Printf("[Agent] Act → executing %d tool(s) in parallel", len(toolCalls))
	results := make([]tools.ToolResult, len(toolCalls))
	var wg sync.WaitGroup

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, tc foundation.ToolUseContent) {
			defer wg.Done()
			results[idx] = a.act(ctx, tc)
		}(i, tc)
	}
	wg.Wait()

	msgs := make([]foundation.Message, 0, len(toolCalls))
	for i, tc := range toolCalls {
		content := results[i].GetMessage()
		if results[i].IsSuccess() {
			if sr, ok := results[i].(*tools.SuccessResult); ok {
				if jsonBytes, err := json.Marshal(sr.Result); err == nil {
					content = string(jsonBytes)
				}
			}
		}

		msgs = append(msgs, foundation.Message{
			Role: foundation.RoleTool,
			ToolResult: &foundation.ToolResultContent{
				Type:      "tool_result",
				ToolUseID: tc.ID,
				Content:   content,
			},
		})
	}
	return msgs
}

func toolCallNames(calls []foundation.ToolUseContent) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}

func (a *Agent) callModel(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
	apiKey := ""
	apiURL := "https://api.deepseek.com/v1"

	if key, ok := model.Options["api_key"].(string); ok && key != "" {
		apiKey = key
	}
	if url, ok := model.Options["api_url"].(string); ok && url != "" {
		apiURL = url
	}
	if apiKey == "" {
		apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}
	if envURL := os.Getenv("DEEPSEEK_API_URL"); envURL != "" {
		apiURL = envURL
	}

	modelName := model.Name
	if modelName == "" {
		modelName = os.Getenv("DEEPSEEK_MODEL")
	}
	modelName = normalizeModelName(modelName)
	if modelName == "" {
		modelName = "deepseek-chat"
	}

	client := deepseek.NewClient(apiKey, apiURL, modelName)

	deepseekMessages := deepseek.ConvertMessages(messages)
	toolDefs := a.buildToolDefinitions()

	maxTokens := 4096
	if v, ok := model.Options["max_tokens"].(int); ok && v > 0 {
		maxTokens = v
	} else if f, ok := model.Options["max_tokens"].(float64); ok && f > 0 {
		maxTokens = int(f)
	}

	reasoningEffort := "low"
	if v, ok := model.Options["reasoning_effort"].(string); ok && v != "" {
		reasoningEffort = v
	}

	resp, err := client.Chat(deepseekMessages, toolDefs, maxTokens, reasoningEffort)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("DeepSeek returned empty choices")
	}

	choice := resp.Choices[0]
	foundationMsg := &foundation.Message{
		Role: foundation.RoleAssistant,
	}
	if choice.Message.ReasoningContent != "" {
		// V4 模型的推理内容放在 content 前，方便前端/下游处理
		foundationMsg.Content = choice.Message.ReasoningContent
		if choice.Message.Content != "" {
			foundationMsg.Content = choice.Message.ReasoningContent + "\n\n" + choice.Message.Content
		}
	} else {
		foundationMsg.Content = choice.Message.Content
	}

	if len(choice.Message.ToolCalls) > 0 {
		for _, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]interface{}{"raw": tc.Function.Arguments}
			}
			foundationMsg.ToolCalls = append(foundationMsg.ToolCalls, foundation.ToolUseContent{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: args,
			})
		}
	}

	if resp.Usage.TotalTokens > 0 {
		a.logger.Printf("[DeepSeek] model=%s tokens: prompt=%d completion=%d total=%d",
			resp.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}

	return foundationMsg, nil
}

func (a *Agent) callModelStreaming(ctx context.Context, model *foundation.Model, messages []foundation.Message, onEvent StreamCallback) (*foundation.Message, error) {
	apiKey := ""
	apiURL := "https://api.deepseek.com/v1"

	if key, ok := model.Options["api_key"].(string); ok && key != "" {
		apiKey = key
	}
	if url, ok := model.Options["api_url"].(string); ok && url != "" {
		apiURL = url
	}
	if apiKey == "" {
		apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}
	if envURL := os.Getenv("DEEPSEEK_API_URL"); envURL != "" {
		apiURL = envURL
	}

	modelName := model.Name
	if modelName == "" {
		modelName = os.Getenv("DEEPSEEK_MODEL")
	}
	// DeepSeek API 只接受小写的模型名。常见的有效名称：deepseek-chat / deepseek-reasoner / deepseek-v4-flash / deepseek-v4-pro
	modelName = normalizeModelName(modelName)
	if modelName == "" {
		modelName = "deepseek-chat"
	}

	client := deepseek.NewClient(apiKey, apiURL, modelName)
	deepseekMessages := deepseek.ConvertMessages(messages)
	toolDefs := a.buildToolDefinitions()

	maxTokens := 4096
	if v, ok := model.Options["max_tokens"].(int); ok && v > 0 {
		maxTokens = v
	} else if f, ok := model.Options["max_tokens"].(float64); ok && f > 0 {
		maxTokens = int(f)
	}

	reasoningEffort := "low"
	if v, ok := model.Options["reasoning_effort"].(string); ok && v != "" {
		reasoningEffort = v
	}

	var (
		fullContent   strings.Builder
		foundationMsg = foundation.Message{Role: foundation.RoleAssistant}
	)

	// Accumulate tool calls from streaming chunks.
	// DeepSeek streams tool_calls incrementally across chunks with the same index.
	toolCallMap := make(map[int]*foundation.ToolUseContent) // index → partial tool call

	err := client.StreamChat(deepseekMessages, toolDefs, maxTokens, reasoningEffort, func(streamResp *deepseek.StreamChatResponse) error {
		if len(streamResp.Choices) == 0 {
			return nil
		}

		delta := streamResp.Choices[0].Delta

		// Stream reasoning content (V4 / reasoner models)
		if delta.ReasoningContent != "" {
			fullContent.WriteString(delta.ReasoningContent)
			onEvent(StreamEvent{Type: StreamEventTextDelta, Content: delta.ReasoningContent})
		}

		// Stream text deltas
		if delta.Content != "" {
			fullContent.WriteString(delta.Content)
			onEvent(StreamEvent{Type: StreamEventTextDelta, Content: delta.Content})
		}

		// Accumulate tool calls from streaming deltas
		if len(delta.ToolCalls) > 0 {
			for i, tc := range delta.ToolCalls {
				idx := tc.Index
				if idx == 0 && i > 0 {
					idx = i
				}
				if tc.ID != "" {
					// If we see an ID, this is the first chunk for this tool call
					if existing, ok := toolCallMap[idx]; ok && existing.ID == tc.ID {
						// Append arguments
						if tc.Function.Arguments != "" {
							existing.Input["_raw_arguments"] = existing.Input["_raw_arguments"].(string) + tc.Function.Arguments
						}
					} else {
						tcInput := map[string]interface{}{
							"_raw_arguments": tc.Function.Arguments,
						}
						if tc.Function.Name != "" {
							tcInput["_name"] = tc.Function.Name
						}
						toolCallMap[idx] = &foundation.ToolUseContent{
							Type:  "tool_use",
							ID:    tc.ID,
							Name:  tc.Function.Name,
							Input: tcInput,
						}
					}
				} else if tc.Function.Arguments != "" {
					// Chunk with no ID, just appending arguments to the right tool call
					if existing, ok := toolCallMap[idx]; ok {
						existing.Input["_raw_arguments"] = existing.Input["_raw_arguments"].(string) + tc.Function.Arguments
					}
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("DeepSeek streaming API call failed: %w", err)
	}

	foundationMsg.Content = fullContent.String()

	// Parse accumulated tool calls
	for i := 0; ; i++ {
		tc, ok := toolCallMap[i]
		if !ok {
			break
		}
		rawArgs := tc.Input["_raw_arguments"].(string)
		delete(tc.Input, "_raw_arguments")

		var args map[string]interface{}
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			args = map[string]interface{}{"raw": rawArgs}
		}

		name := tc.Name
		if name == "" {
			if n, ok := tc.Input["_name"].(string); ok {
				name = n
			}
			delete(tc.Input, "_name")
		}

		foundationMsg.ToolCalls = append(foundationMsg.ToolCalls, foundation.ToolUseContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  name,
			Input: args,
		})
	}

	return &foundationMsg, nil
}

func (a *Agent) buildToolDefinitions() []deepseek.ToolDef {
	defs := make([]deepseek.ToolDef, 0, len(a.tools))
	for _, t := range a.tools {
		schema := t.InputSchema()
		if schema == nil {
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			}
		}

		defs = append(defs, deepseek.ToolDef{
			Type: "function",
			Function: deepseek.ToolDefFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  schema,
			},
		})
	}
	return defs
}

// normalizeModelName 将任意大小写的模型名规范化为 DeepSeek API 接受的格式。
// 接受的格式：deepseek-chat / deepseek-reasoner / deepseek-v4-flash / deepseek-v4-pro
// 传入空字符串或无法识别的名称时返回空字符串（由调用方决定默认值）。
func normalizeModelName(name string) string {
	// DeepSeek 接受的合法模型名（全小写）。
	// - deepseek-chat: 基础模型，无推理内容
	// - deepseek-v4-flash / deepseek-v4-pro / deepseek-reasoner: 会生成 reasoning_content
	trimmed := strings.ToLower(strings.TrimSpace(name))
	return trimmed
}
