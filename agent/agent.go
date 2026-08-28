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

	"github.com/google/uuid"

	"leadingAgent/agent/foundation"
	"leadingAgent/agent/tools"
	"leadingAgent/models/openai"
)

// StreamEvent types sent to the streaming callback.
const (
	StreamEventThinking   = "thinking"
	StreamEventTextDelta  = "text_delta"
	StreamEventReasoning  = "reasoning"
	StreamEventToolCall   = "tool_call"
	StreamEventToolResult = "tool_result"
	StreamEventDone       = "done"
	StreamEventError      = "error"
	StreamEventCritique   = "critique"
)

// maxCritiqueRetries caps how many times the critic can send the agent back
// for another turn on the same final answer, preventing an infinite loop if
// the critic and the model can't converge.
const maxCritiqueRetries = 2

// TokenUsage tracks cumulative token consumption for a single Agent session.
type TokenUsage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

// StreamEvent represents a single streaming event.
type StreamEvent struct {
	Type      string                 `json:"type"`
	Content   string                 `json:"content,omitempty"`
	Reasoning string                 `json:"reasoning,omitempty"`
	Tool      string                 `json:"tool,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	Result    string                 `json:"result,omitempty"`
	Turns     int                    `json:"turns,omitempty"`
	SessionID string                 `json:"sessionId,omitempty"`
	// Token counts populated on StreamEventDone; zero on all other events.
	Usage *TokenUsage `json:"usage,omitempty"`
}

// StreamCallback is called for each streaming event.
type StreamCallback func(event StreamEvent) error

// CostSaver is a callback invoked after each model call to persist token usage.
type CostSaver func(caller, model, sessionID string, prompt, completion int)

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

	usageMu   sync.Mutex
	usage     TokenUsage
	saveCost  CostSaver
	sessionID string
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
		tools.NewWebSearchTool(),
		tools.NewRememberFactTool(),
		tools.NewTopicFinderTool(),
	}
}

// Close releases resources held by the Agent (e.g. log file handles).
func (a *Agent) Close() error {
	if a.logFile != nil {
		return a.logFile.Close()
	}
	return nil
}

// WithCostSaver sets the cost saver callback and returns the agent for chaining.
// Safe for concurrent use: writes are protected by usageMu, the same lock used
// in accumulateUsage when reading these fields.
func (a *Agent) WithCostSaver(fn CostSaver) *Agent {
	a.usageMu.Lock()
	a.saveCost = fn
	a.usageMu.Unlock()
	return a
}

// SetSessionID sets the session ID used when persisting cost records.
// Safe for concurrent use: see WithCostSaver.
func (a *Agent) SetSessionID(id string) {
	a.usageMu.Lock()
	a.sessionID = id
	a.usageMu.Unlock()
}

// accumulateUsage records token counts from one model call and logs them.
func (a *Agent) accumulateUsage(caller, model string, prompt, completion int) {
	a.usageMu.Lock()
	a.usage.PromptTokens += prompt
	a.usage.CompletionTokens += completion
	a.usage.TotalTokens += prompt + completion
	sessPrompt := a.usage.PromptTokens
	sessCompl := a.usage.CompletionTokens
	sessTotal := a.usage.TotalTokens
	saveCost := a.saveCost
	sessionID := a.sessionID
	a.usageMu.Unlock()
	a.logger.Printf("[Usage] %s model=%s prompt=%d completion=%d total=%d | session prompt=%d completion=%d total=%d",
		caller, model, prompt, completion, prompt+completion,
		sessPrompt, sessCompl, sessTotal)
	if saveCost != nil {
		go saveCost(caller, model, sessionID, prompt, completion)
	}
}

// UsageSummary returns a snapshot of cumulative token usage for this session.
func (a *Agent) UsageSummary() TokenUsage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	return a.usage
}

const defaultSystemPrompt = "You are a helpful assistant. When you need information, use the available tools to search or take action. When you learn personal facts about the user, use the remember_fact tool to save them."

func (a *Agent) Execute(ctx context.Context, model *foundation.Model, systemPrompt string, history []foundation.Message, userQuery string) (string, string, error) {
	a.logger.Printf("[Agent] Execute: query=%q model=%s history=%d", userQuery, model.Name, len(history))
	messages := a.buildMessages(systemPrompt, history, userQuery)

	critiqueAttempts := 0
	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		default:
		}

		assistantMsg, err := a.think(ctx, model, messages)
		if err != nil {
			return "", "", err
		}
		if assistantMsg == nil {
			return "", "", fmt.Errorf("model returned nil response")
		}

		messages = append(messages, *assistantMsg)

		if len(assistantMsg.ToolCalls) > 0 {
			a.logger.Printf("[Agent] Think → %d tool call(s): %v", len(assistantMsg.ToolCalls), toolCallNames(assistantMsg.ToolCalls))
			toolMsgs := a.executeParallel(ctx, assistantMsg.ToolCalls)
			messages = append(messages, toolMsgs...)
			continue
		}

		if critiqueAttempts < maxCritiqueRetries {
			verdict := a.reviewFinalAnswer(ctx, model, userQuery, assistantMsg.Content)
			if !verdict.Approved {
				critiqueAttempts++
				a.logger.Printf("[Critic] rejected final answer (attempt %d/%d): %s", critiqueAttempts, maxCritiqueRetries, verdict.Feedback)
				messages = append(messages, foundation.NewMessage(
					foundation.RoleUser,
					fmt.Sprintf("（评审反馈，请据此修正你的回答）%s", verdict.Feedback),
				))
				continue
			}
		}

		a.logger.Printf("[Agent] Final response: %s", assistantMsg.Content)
		return assistantMsg.Content, assistantMsg.Reasoning, nil
	}
}

// Summarize 生成对话内容的简洁摘要。不触发工具调用，只做纯文本模型推理。
// 用于：过期 session 的上下文迁移 — 将旧对话摘要注入新 session 的 system prompt。
func (a *Agent) Summarize(ctx context.Context, model *foundation.Model, messages []foundation.Message, maxChars int) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("请用中文生成这段对话的简洁摘要（不超过 ")
	fmt.Fprintf(&sb, "%d", maxChars)
	sb.WriteString(" 字符），只保留关键事实、偏好和决策：\n\n")
	for _, m := range messages {
		switch m.Role {
		case foundation.RoleUser:
			sb.WriteString("用户：")
		case foundation.RoleAssistant:
			sb.WriteString("助手：")
		case foundation.RoleSystem:
			sb.WriteString("系统：")
		case foundation.RoleTool:
			sb.WriteString("工具：")
		}
		content := m.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		sb.WriteString(content)
		sb.WriteString("\n")
	}

	// 直接调 deepseek client，空工具列表 → 避免 remember_fact 等工具被触发
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

	client := openai.NewClient(apiKey, apiURL, modelName)
	summMsgs := []foundation.Message{
		foundation.NewMessage(foundation.RoleSystem, "你是一个精确、简洁的摘要生成器。"),
		foundation.NewMessage(foundation.RoleUser, sb.String()),
	}
	resp, err := client.Chat(openai.ConvertMessages(summMsgs), nil, 100000, "low", nil)
	if err != nil {
		return "", fmt.Errorf("summary call failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("summary call returned no choices")
	}
	summary := strings.TrimSpace(resp.Choices[0].Message.Content)
	if len(summary) > maxChars {
		summary = summary[:maxChars] + "..."
	}
	a.logger.Printf("[Agent] Summarize: %d messages → %d chars", len(messages), len(summary))
	return summary, nil
}

// ExecuteStreaming runs the ReAct loop with streaming callbacks for each event.
func (a *Agent) ExecuteStreaming(ctx context.Context, model *foundation.Model, systemPrompt string, history []foundation.Message, userQuery string, onEvent StreamCallback) error {
	a.logger.Printf("[Agent] ExecuteStreaming: query=%q model=%s history=%d", userQuery, model.Name, len(history))
	messages := a.buildMessages(systemPrompt, history, userQuery)

	turn := 0
	critiqueAttempts := 0
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
		if assistantMsg == nil {
			err := fmt.Errorf("model returned nil response")
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
			continue
		}

		if critiqueAttempts < maxCritiqueRetries {
			verdict := a.reviewFinalAnswer(ctx, model, userQuery, assistantMsg.Content)
			if !verdict.Approved {
				critiqueAttempts++
				a.logger.Printf("[Critic] rejected final answer (attempt %d/%d): %s", critiqueAttempts, maxCritiqueRetries, verdict.Feedback)
				onEvent(StreamEvent{Type: StreamEventCritique, Content: verdict.Feedback, Turns: turn})
				messages = append(messages, foundation.NewMessage(
					foundation.RoleUser,
					fmt.Sprintf("（评审反馈，请据此修正你的回答）%s", verdict.Feedback),
				))
				continue
			}
		}

		a.logger.Printf("[Agent] Final response: %s", assistantMsg.Content)
		summary := a.UsageSummary()
		onEvent(StreamEvent{Type: StreamEventDone, Content: assistantMsg.Content, Reasoning: assistantMsg.Reasoning, Turns: turn, Usage: &summary})
		return nil
	}
}

// buildMessages 拼接 system prompt + 历史消息 + 当前用户消息，作为本轮推理的输入。
func (a *Agent) buildMessages(systemPrompt string, history []foundation.Message, userQuery string) []foundation.Message {
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	out := make([]foundation.Message, 0, len(history)+2)
	out = append(out, foundation.NewMessage(foundation.RoleSystem, systemPrompt))
	out = append(out, history...)
	if userQuery != "" {
		out = append(out, foundation.NewMessage(foundation.RoleUser, userQuery))
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
			ID:        uuid.NewString(),
			Role:      foundation.RoleTool,
			CreatedAt: time.Now().UTC(),
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

// resolveClient builds an OpenAI-compatible client from the model's options,
// falling back to DeepSeek env vars. Shared by callModel, callModelStreaming,
// and the critic's review call so all three resolve credentials identically.
func (a *Agent) resolveClient(model *foundation.Model) *openai.Client {
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

	return openai.NewClient(apiKey, apiURL, modelName)
}

func (a *Agent) callModel(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
	client := a.resolveClient(model)

	openaiMessages := openai.ConvertMessages(messages)

	// 检查是否用精简工具模式（doubao 等对 context 敏感的模型）
	liteTools := false
	if v, ok := model.Options["lite_tools"].(bool); ok {
		liteTools = v
	}

	var toolDefs []openai.ToolDef
	if liteTools {
		toolDefs = nil
		// 在 system prompt 末尾追加工具摘要
		if len(openaiMessages) > 0 && openaiMessages[0].Role == "system" {
			openaiMessages[0].Content += a.buildToolSummary()
		}
	} else {
		toolDefs = a.buildToolDefinitions()
	}

	maxTokens := 4096
	if v, ok := model.Options["max_tokens"].(int); ok && v > 0 {
		maxTokens = v
	} else if f, ok := model.Options["max_tokens"].(float64); ok && f > 0 {
		maxTokens = int(f)
	}

	reasoningEffort := "low"
	if v, ok := model.Options["reasoning_effort"].(string); ok {
		reasoningEffort = v // 允许空字符串覆盖默认值（doubao 不传此参数）
	}

	var thinking json.RawMessage
	if t, ok := model.Options["thinking"].(string); ok && t != "" {
		thinking = json.RawMessage(t)
	}

	resp, err := client.Chat(openaiMessages, toolDefs, maxTokens, reasoningEffort, thinking)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("DeepSeek returned empty choices")
	}

	choice := resp.Choices[0]
	now := time.Now().UTC()
	foundationMsg := &foundation.Message{
		ID:        uuid.NewString(),
		Role:      foundation.RoleAssistant,
		CreatedAt: now,
		Content:   choice.Message.Content,
		Reasoning: choice.Message.ReasoningContent,
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
		a.accumulateUsage("callModel", resp.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}

	return foundationMsg, nil
}

func (a *Agent) callModelStreaming(ctx context.Context, model *foundation.Model, messages []foundation.Message, onEvent StreamCallback) (*foundation.Message, error) {
	client := a.resolveClient(model)
	openaiMsgs := openai.ConvertMessages(messages)

	// 检查是否用精简工具模式
	var toolDefs []openai.ToolDef
	if v, ok := model.Options["lite_tools"].(bool); ok && v {
		toolDefs = nil
		if len(openaiMsgs) > 0 && openaiMsgs[0].Role == "system" {
			openaiMsgs[0].Content += a.buildToolSummary()
		}
		a.logger.Printf("[Agent] lite_tools mode: toolDefs=nil, systemPrompt=%d chars", len(openaiMsgs[0].Content))
	} else {
		toolDefs = a.buildToolDefinitions()
		a.logger.Printf("[Agent] full tools mode: %d tool defs", len(toolDefs))
	}

	maxTokens := 4096
	if v, ok := model.Options["max_tokens"].(int); ok && v > 0 {
		maxTokens = v
	} else if f, ok := model.Options["max_tokens"].(float64); ok && f > 0 {
		maxTokens = int(f)
	}

	reasoningEffort := "low"
	if v, ok := model.Options["reasoning_effort"].(string); ok {
		reasoningEffort = v
	}

	var thinkingStream json.RawMessage
	if t, ok := model.Options["thinking"].(string); ok && t != "" {
		thinkingStream = json.RawMessage(t)
	}

	var (
		fullContent     strings.Builder
		reasoningBuffer strings.Builder
		now             = time.Now().UTC()
		foundationMsg   = foundation.Message{
			ID:        uuid.NewString(),
			Role:      foundation.RoleAssistant,
			CreatedAt: now,
		}
		streamModel  string
		streamPrompt int
		streamCompl  int
	)

	// Accumulate tool calls from streaming chunks.
	// DeepSeek streams tool_calls incrementally across chunks with the same index.
	toolCallMap := make(map[int]*foundation.ToolUseContent) // index → partial tool call

	err := client.StreamChat(openaiMsgs, toolDefs, maxTokens, reasoningEffort, thinkingStream, func(streamResp *openai.StreamChatResponse) error {
		if streamResp.Model != "" {
			streamModel = streamResp.Model
		}
		if streamResp.Usage != nil {
			streamPrompt = streamResp.Usage.PromptTokens
			streamCompl = streamResp.Usage.CompletionTokens
		}
		if len(streamResp.Choices) == 0 {
			return nil
		}

		delta := streamResp.Choices[0].Delta

		// Stream reasoning content (V4 / reasoner models) as a separate event
		if delta.ReasoningContent != "" {
			reasoningBuffer.WriteString(delta.ReasoningContent)
			onEvent(StreamEvent{Type: StreamEventReasoning, Content: delta.ReasoningContent})
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

	if streamPrompt > 0 || streamCompl > 0 {
		a.accumulateUsage("callModelStreaming", streamModel, streamPrompt, streamCompl)
	}

	foundationMsg.Content = fullContent.String()
	foundationMsg.Reasoning = reasoningBuffer.String()

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

func (a *Agent) buildToolDefinitions() []openai.ToolDef {
	defs := make([]openai.ToolDef, 0, len(a.tools))
	for _, t := range a.tools {
		schema := t.InputSchema()
		if schema == nil {
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			}
		}

		defs = append(defs, openai.ToolDef{
			Type: "function",
			Function: openai.ToolDefFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  schema,
			},
		})
	}
	return defs
}

// buildToolSummary 生成一行/工具的简洁摘要，用于 doubao 等对 context 敏感的模型。
func (a *Agent) buildToolSummary() string {
	var sb strings.Builder
	sb.WriteString("\n\n## 可用工具清单\n")
	for _, t := range a.tools {
		sb.WriteString("- ")
		sb.WriteString(t.Name())
		sb.WriteString(": ")
		sb.WriteString(t.Description())
		sb.WriteString("\n")
	}
	sb.WriteString("\n当你需要使用工具时，请在回复开头用一行 `[TOOL: 工具名 | 参数...]` 标记，系统会在下一轮为你调用。")
	return sb.String()
}

// normalizeModelName 将模型名规范化（全小写）。
// DeepSeek 接受的格式：deepseek-chat / deepseek-reasoner / deepseek-v4-flash / deepseek-v4-pro
// 豆包接受的格式：doubao-seed-1-6-250715 等
func normalizeModelName(name string) string {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	return trimmed
}
