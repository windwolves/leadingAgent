package tools

import (
	"context"
	"time"
)

// WriteMemoryFunc 用于注入 MemoryService.Remember 回调。
// 由 cmd/gateway/main.go 在组装依赖后设置。
var WriteMemoryFunc func(ctx context.Context, userID, category, content string) (string, error)

// SetWriteMemoryFunc 设置记忆写入回调。
func SetWriteMemoryFunc(fn func(ctx context.Context, userID, category, content string) (string, error)) {
	WriteMemoryFunc = fn
}

// rememberFactExecutor 实现 ToolExecutor 接口。
type rememberFactExecutor struct{}

func (e *rememberFactExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	if WriteMemoryFunc == nil {
		return NewErrorResult("remember_fact", "function",
			"Memory service is not available", "MEMORY_UNAVAILABLE", 0, "记忆服务未初始化")
	}

	userID, _ := params["userId"].(string)
	category, _ := params["category"].(string)
	content, _ := params["content"].(string)

	if userID == "" {
		return NewErrorResult("remember_fact", "function",
			"userId is required", "MISSING_PARAM", 0, "缺少 userId 参数")
	}
	if category == "" {
		category = "facts"
	}
	if content == "" {
		return NewErrorResult("remember_fact", "function",
			"content is required", "MISSING_PARAM", 0, "缺少 content 参数")
	}

	start := time.Now()
	id, err := WriteMemoryFunc(ctx, userID, category, content)
	elapsed := time.Since(start)

	if err != nil {
		return NewErrorResult("remember_fact", "function",
			err.Error(), "WRITE_FAILED", elapsed, "记忆写入失败: "+err.Error())
	}

	return NewSuccessResult("remember_fact", "function",
		map[string]interface{}{
			"id":       id,
			"category": category,
		},
		elapsed,
		"记忆已保存 (id="+id+", category="+category+")",
	)
}

func (e *rememberFactExecutor) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"userId": map[string]interface{}{
				"type":        "string",
				"description": "当前用户 ID，必须填写。通常从对话上下文中获取。",
			},
			"category": map[string]interface{}{
				"type":        "string",
				"description": "记忆分类: personal(个人信息), preferences(偏好), technical(技术), facts(事实)",
				"enum":        []string{"personal", "preferences", "technical", "facts"},
				"default":     "facts",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "要记住的内容。用一句话描述，例如: '用户名字是Bob，喜欢简洁回复。'",
			},
		},
		"required": []string{"userId", "content"},
	}
}

// NewRememberFactTool 创建 remember_fact 工具。
func NewRememberFactTool() *Tool {
	return NewTool(
		"remember_fact",
		"function",
		"记录一条关于用户的长期记忆。当你了解到用户的重要信息（姓名、偏好、技术栈等）时，主动调用此工具保存。系统会自动去重。",
		[]ToolParameter{
			{
				Name:        "userId",
				Type:        "string",
				Description: "当前用户 ID",
				Required:    true,
			},
			{
				Name:        "category",
				Type:        "string",
				Description: "记忆分类: personal, preferences, technical, facts",
				Required:    false,
			},
			{
				Name:        "content",
				Type:        "string",
				Description: "要记住的内容，一句话描述",
				Required:    true,
			},
		},
		&rememberFactExecutor{},
	)
}
