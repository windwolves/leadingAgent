package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"leadingAgent/agent/foundation"
	"leadingAgent/config"
	"leadingAgent/models/openai"
)

// ---------------------------------------------------------------------------
// 流式事件类型（自包含，不依赖 agent 包，避免循环依赖）
// ---------------------------------------------------------------------------

// TopicStreamEvent 选题查找流的单次事件。
type TopicStreamEvent struct {
	Type    string `json:"type"` // "thinking" | "text_delta" | "done" | "error"
	Content string `json:"content,omitempty"`
}

// TopicStreamCallback 选题查找流的回调签名。
type TopicStreamCallback func(event TopicStreamEvent) error

// ---------------------------------------------------------------------------
// SearchFunc — 可注入的搜索能力，避免循环依赖 agent/tools
// ---------------------------------------------------------------------------

// SearchFunc 执行一次 web 搜索。
// 返回 JSON 格式的搜索结果字符串。
type SearchFunc func(ctx context.Context, query string) (string, error)

// webSearchToolDef 是硬编码的 web_search 工具定义，与 agent/tools/web_search.go 的
// InputSchema 保持同步。
var webSearchToolDef = openai.ToolDef{
	Type: "function",
	Function: openai.ToolDefFunction{
		Name:        "web_search",
		Description: "搜索互联网获取实时信息。当需要最新数据、新闻、事实核查时使用。",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "搜索关键词",
				},
			},
			"required": []string{"query"},
		},
	},
}

// ---------------------------------------------------------------------------
// TopicFinder
// ---------------------------------------------------------------------------

// TopicFinder 新闻选题发现子代理。
// 两步流水线：
//  1. 以"资深新闻编辑"角色将用户关键字优化为专业的选题搜寻 prompt
//  2. 将优化后的 prompt 发给 LLM（可选集成 web_search），生成结构化选题报告
type TopicFinder struct {
	client     *openai.Client
	model      string
	logger     *log.Logger
	searchFunc SearchFunc // nil = 无搜索能力，回退到单次 Chat
}

// NewTopicFinder 创建 TopicFinder。
// searchFunc 为可选的 web 搜索回调；为 nil 时选题报告不包含实时数据。
func NewTopicFinder(searchFunc SearchFunc) *TopicFinder {
	var (
		apiKey    string
		apiURL    string
		modelName string
	)

	if cfg, err := config.LoadConfig(); err == nil {
		if cfg.DeepSeekAPIKey != "" {
			apiKey = cfg.DeepSeekAPIKey
		}
		if cfg.DeepSeekAPIURL != "" {
			apiURL = cfg.DeepSeekAPIURL
		}
		if cfg.DeepSeekModel != "" {
			modelName = cfg.DeepSeekModel
		}
	}
	if apiURL == "" {
		apiURL = "https://api.deepseek.com/v1"
	}
	if modelName == "" {
		modelName = "deepseek-chat"
	}
	return &TopicFinder{
		client:     openai.NewClient(apiKey, apiURL, modelName),
		model:      modelName,
		logger:     log.New(os.Stdout, "[TopicFinder] ", log.LstdFlags),
		searchFunc: searchFunc,
	}
}

// ---------------------------------------------------------------------------
// System Prompts
// ---------------------------------------------------------------------------

const (
	promptOptimizerSystem = `你是一位资深新闻编辑，拥有 20 年新闻行业经验。

你的任务是将用户给的选题关键字，扩展成一段完整、专业的"选题搜寻" prompt，让 LLM 能够据此搜索并整理出有价值的新闻选题。

要求：
- 保留用户的核心意图和关键信息
- 补充选题角度（政策、市场、技术、社会影响等维度）
- 补全新闻 5W1H 要素（Who / What / When / Where / Why / How）
- 指定文体风格（如客观报道、深度分析、评论等）
- 输出只包含优化后的搜寻 prompt，不要前缀、解释或格式标记`

	reportGeneratorSystem = `你是一位资深新闻编辑，擅长搜索、分析并整理新闻选题。

你可以使用 web_search 工具搜索互联网获取实时信息。请遵循以下规则：
1. 先搜索核心关键词，获取最新动态
2. 拿到搜索结果后，立即基于已有信息生成选题报告
3. 不要反复搜索同一个方向——搜 1-2 轮就够了，然后出报告

请根据以下搜寻 prompt，生成一份结构化的"选题报告"。报告必须使用 Markdown 格式，包含以下部分：

## 选题清单
为每个选题提供：
- **选题名称**：一句话概括
- **选题角度**：可以从哪些方向切入报道
- **新闻价值**：为什么值得报道（时效性、公共性、冲突性、接近性等）
- **目标受众**：谁会关注
- **信息来源建议**：可以从哪里获取素材（政府网站、行业报告、专家采访等）

## 推荐优先级
对以上选题按新闻价值和可行性排序，并给出理由。

如果用户输入的关键字不足以支撑完整的选题报告，请说明还需要补充哪些信息。
重要：搜完一轮后必须给出最终报告，不要再继续搜索。`
)

// ---------------------------------------------------------------------------
// 公开方法
// ---------------------------------------------------------------------------

// Execute 非流式执行：关键字 → 优化 prompt → 选题报告。
// 两步串行，总耗时为两次 LLM 调用之和（含可选的搜索轮次）。
func (t *TopicFinder) Execute(ctx context.Context, query string) (string, error) {
	if query == "" {
		return "", fmt.Errorf("topicfinder: query is empty")
	}

	// Step 1: 优化 prompt
	optimizedPrompt, err := t.optimizePrompt(ctx, query)
	if err != nil {
		return "", fmt.Errorf("topicfinder: prompt optimization failed: %w", err)
	}
	t.logger.Printf("optimized prompt (%d chars): %s", len(optimizedPrompt), optimizedPrompt)

	// Step 2: 生成选题报告（带搜索能力的 ReAct 循环）
	report, err := t.generateReport(ctx, optimizedPrompt)
	if err != nil {
		return "", fmt.Errorf("topicfinder: report generation failed: %w", err)
	}
	t.logger.Printf("report generated (%d chars)", len(report))
	return report, nil
}

// ExecuteStreaming 流式执行：Step 1 同 Execute（非流式），Step 2 通过回调逐块推送。
// 注意：流式模式下 Step 2 内部不运行 ReAct 循环（搜索结果需整体注入后再流式输出）。
func (t *TopicFinder) ExecuteStreaming(ctx context.Context, query string, onEvent TopicStreamCallback) error {
	if query == "" {
		return fmt.Errorf("topicfinder: query is empty")
	}

	// Step 1: 优化 prompt（非流式，快速完成）
	optimizedPrompt, err := t.optimizePrompt(ctx, query)
	if err != nil {
		sendOrIgnore(onEvent, TopicStreamEvent{Type: "error", Content: err.Error()})
		return fmt.Errorf("topicfinder: prompt optimization failed: %w", err)
	}
	t.logger.Printf("optimized prompt (%d chars): %s", len(optimizedPrompt), optimizedPrompt)

	// Step 2: 流式生成选题报告
	sendOrIgnore(onEvent, TopicStreamEvent{Type: "thinking"})

	report, err := t.generateReport(ctx, optimizedPrompt)
	if err != nil {
		sendOrIgnore(onEvent, TopicStreamEvent{Type: "error", Content: err.Error()})
		return fmt.Errorf("topicfinder: report generation failed: %w", err)
	}

	sendOrIgnore(onEvent, TopicStreamEvent{Type: "done", Content: report})
	return nil
}

// ---------------------------------------------------------------------------
// 内部方法
// ---------------------------------------------------------------------------

// optimizePrompt Step 1：将用户关键字优化为选题搜寻 prompt。
func (t *TopicFinder) optimizePrompt(ctx context.Context, query string) (string, error) {
	msgs := []foundation.Message{
		{Role: foundation.RoleSystem, Content: promptOptimizerSystem},
		{Role: foundation.RoleUser, Content: fmt.Sprintf("请为以下选题关键字生成新闻选题搜寻 prompt：%s", query)},
	}

	resp, err := t.client.Chat(openai.ConvertMessages(msgs), nil, 100000, "", nil)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response for prompt optimization")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

// generateReport Step 2：根据优化后的 prompt 生成结构化选题报告。
// 如果有 searchFunc，运行 ReAct 循环让 LLM 自主调用 web_search。
func (t *TopicFinder) generateReport(ctx context.Context, optimizedPrompt string) (string, error) {
	messages := []openai.Message{
		{Role: "system", Content: reportGeneratorSystem},
		{Role: "user", Content: optimizedPrompt},
	}

	if t.searchFunc == nil {
		// 无搜索能力：单次 Chat 调用
		return t.singleChat(ctx, messages)
	}

	// 带搜索能力的 ReAct 循环
	return t.reactLoop(ctx, messages)
}

// singleChat 无搜索能力时的单次 Chat 调用。
func (t *TopicFinder) singleChat(ctx context.Context, messages []openai.Message) (string, error) {
	resp, err := t.client.Chat(messages, nil, 100000, "", nil)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response for report generation")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

// reactLoop 运行 ReAct 循环：LLM 可以自主决定是否调用 web_search。
// 最多 5 轮工具调用，防止无限循环。
const maxReActTurns = 5

func (t *TopicFinder) reactLoop(ctx context.Context, messages []openai.Message) (string, error) {
	tools := []openai.ToolDef{webSearchToolDef}

	for turn := 0; turn < maxReActTurns; turn++ {
		resp, err := t.client.Chat(messages, tools, 100000, "", nil)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("empty response in react loop (turn %d)", turn)
		}

		choice := resp.Choices[0]
		assistantMsg := choice.Message

		// 追加 assistant 消息
		messages = append(messages, assistantMsg)

		// 检查是否有 tool_calls
		if len(assistantMsg.ToolCalls) == 0 {
			// 没有工具调用 → 最终回复
			return strings.TrimSpace(assistantMsg.Content), nil
		}

		// 并行执行所有工具调用
		t.logger.Printf("react turn %d: %d tool call(s)", turn, len(assistantMsg.ToolCalls))

		type toolResult struct {
			index int
			msg   openai.Message
		}
		results := make([]toolResult, len(assistantMsg.ToolCalls))
		var wg sync.WaitGroup

		for i, tc := range assistantMsg.ToolCalls {
			wg.Add(1)
			go func(i int, tc openai.ToolCall) {
				defer wg.Done()

				if tc.Function.Name != "web_search" {
					results[i] = toolResult{index: i, msg: openai.Message{
						Role:       "tool",
						Content:    fmt.Sprintf(`{"error": "unknown tool: %s"}`, tc.Function.Name),
						ToolCallID: tc.ID,
					}}
					return
				}

				// 解析参数
				query := ""
				if tc.Function.Arguments != "" {
					var args map[string]interface{}
					if err := parseJSON(tc.Function.Arguments, &args); err == nil {
						if q, ok := args["query"].(string); ok {
							query = q
						}
					}
				}
				if query == "" {
					query = "news topics"
				}

				t.logger.Printf("web_search: %q", query)
				searchResult, err := t.searchFunc(ctx, query)
				if err != nil {
					results[i] = toolResult{index: i, msg: openai.Message{
						Role:       "tool",
						Content:    fmt.Sprintf(`{"error": "search failed: %s"}`, err.Error()),
						ToolCallID: tc.ID,
					}}
					return
				}

				results[i] = toolResult{index: i, msg: openai.Message{
					Role:       "tool",
					Content:    searchResult,
					ToolCallID: tc.ID,
				}}
			}(i, tc)
		}
		wg.Wait()

		for i := 0; i < len(results); i++ {
			messages = append(messages, results[i].msg)
		}
		// 循环继续 → 下一轮 LLM 思考
	}

	// 达到最大轮次，最后一轮一定是无 tool_calls 的 LLM 响应
	return "", fmt.Errorf("react loop exceeded max turns (%d) without final response", maxReActTurns)
}

// ---------------------------------------------------------------------------
// 工具
// ---------------------------------------------------------------------------

// sendOrIgnore 发送回调事件，若回调为 nil 则静默跳过。
func sendOrIgnore(cb TopicStreamCallback, event TopicStreamEvent) {
	if cb == nil {
		return
	}
	_ = cb(event)
}

// parseJSON 简单的 JSON 解析。
func parseJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}
