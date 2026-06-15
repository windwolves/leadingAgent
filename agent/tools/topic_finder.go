package tools

import (
	"context"
	"encoding/json"
	"time"

	subagent "leadingAgent/agent/subAgent"
)

// topicFinderExecutor 实现 ToolExecutor，将 TopicFinder 包装为 Agent 可调用的工具。
type topicFinderExecutor struct {
	tf *subagent.TopicFinder
}

func (e *topicFinderExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	query, _ := params["query"].(string)
	if query == "" {
		return NewErrorResult("topic_finder", "function",
			"query is required", "MISSING_PARAM", 0, "缺少 query 参数")
	}

	start := time.Now()
	report, err := e.tf.Execute(ctx, query)
	elapsed := time.Since(start)

	if err != nil {
		return NewErrorResult("topic_finder", "function",
			err.Error(), "TOPIC_FINDER_FAILED", elapsed, "选题查找失败: "+err.Error())
	}

	return NewSuccessResult("topic_finder", "function",
		map[string]interface{}{
			"report": report,
		},
		elapsed,
		"选题报告已生成 ("+formatBytes(len(report))+" 字符)",
	)
}

func (e *topicFinderExecutor) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "选题关键字，例如 'AI 医疗'、'新能源政策'。支持中文/英文，建议 2-15 个字。",
			},
		},
		"required": []string{"query"},
	}
}

// NewTopicFinderTool 创建 topic_finder 工具。
// 当用户提到新闻选题、报道方向、热点话题等需求时，Agent 会自动调用此工具。
// TopicFinder 内部集成了 DuckDuckGo 搜索用于获取实时数据。
func NewTopicFinderTool() *Tool {
	// 创建 DDG 搜索工具供 TopicFinder 内部 ReAct 循环使用（无需 API Key）
	searchTool := NewTool(
		"web_search",
		"search",
		"搜索互联网获取实时信息",
		[]ToolParameter{
			{Name: "query", Type: "string", Description: "搜索关键词", Required: true},
		},
		NewDuckDuckGoExecutor(),
	)

	searchFunc := func(ctx context.Context, query string) (string, error) {
		result := searchTool.Execute(ctx, map[string]interface{}{"query": query})
		if !result.IsSuccess() {
			return "", &searchError{msg: result.GetMessage()}
		}
		// 将搜索结果序列化为 JSON 字符串，供 LLM 消费
		if sr, ok := result.(*SuccessResult); ok && sr.Result != nil {
			b, err := json.Marshal(sr.Result)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
		return result.GetMessage(), nil
	}

	return NewTool(
		"topic_finder",
		"function",
		"根据用户提供的关键字，搜索并生成结构化的新闻选题报告（包含选题清单和推荐优先级）。当用户询问新闻选题、报道方向、热点话题建议时使用此工具。",
		[]ToolParameter{
			{
				Name:        "query",
				Type:        "string",
				Description: "选题关键字（如 'AI 医疗'、'新能源政策'）",
				Required:    true,
			},
		},
		&topicFinderExecutor{
			tf: subagent.NewTopicFinder(searchFunc),
		},
	)
}

type searchError struct {
	msg string
}

func (e *searchError) Error() string { return e.msg }

func formatBytes(n int) string {
	if n < 1000 {
		return itoa(n)
	}
	if n < 10000 {
		return itoa(n/1000) + "k"
	}
	return itoa(n/1000) + "k"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10 + '0')
		n /= 10
	}
	return string(buf[i:])
}
