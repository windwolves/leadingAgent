package tools

import (
	"context"
)

type ToolParameter struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default,omitempty"`
}

type Tool struct {
	name        string          `json:"name"`
	typ         string          `json:"type"`
	description string          `json:"description"`
	parameters  []ToolParameter `json:"parameters"`
	executor    ToolExecutor    `json:"-"`
}

func (t *Tool) Name() string                    { return t.name }
func (t *Tool) Type() string                    { return t.typ }
func (t *Tool) Description() string             { return t.description }
func (t *Tool) Parameters() []ToolParameter     { return t.parameters }

func (t *Tool) InputSchema() map[string]interface{} {
	if t.executor != nil {
		return t.executor.InputSchema()
	}
	return nil
}

type ToolExecutor interface {
	Execute(ctx context.Context, params map[string]interface{}) ToolResult
	InputSchema() map[string]interface{}
}

func NewTool(name, toolType, description string, parameters []ToolParameter, executor ToolExecutor) *Tool {
	return &Tool{
		name:        name,
		typ:         toolType,
		description: description,
		parameters:  parameters,
		executor:    executor,
	}
}

func (t *Tool) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	if t.executor == nil {
		return NewErrorResult(t.name, t.typ, "Executor not set", "NO_EXECUTOR", 0, "工具执行器未设置")
	}
	return t.executor.Execute(ctx, params)
}

func (t *Tool) ValidateParams(params map[string]interface{}) error {
	for _, param := range t.parameters {
		if param.Required {
			if _, exists := params[param.Name]; !exists {
				return &ToolError{Code: "MISSING_PARAM", Message: "缺少必需参数: " + param.Name}
			}
		}
	}
	return nil
}

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ToolError) Error() string {
	return e.Code + ": " + e.Message
}
