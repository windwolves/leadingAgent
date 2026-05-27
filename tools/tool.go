package tools

import "context"

type ToolParameter struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default,omitempty"`
}

type Tool struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Parameters  []ToolParameter `json:"parameters"`
	Executor    ToolExecutor    `json:"-"`
}

type ToolExecutor interface {
	Execute(ctx context.Context, params map[string]interface{}) ToolResult
}

func NewTool(name, toolType, description string, parameters []ToolParameter, executor ToolExecutor) *Tool {
	return &Tool{
		Name:        name,
		Type:        toolType,
		Description: description,
		Parameters:  parameters,
		Executor:    executor,
	}
}

func (t *Tool) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	if t.Executor == nil {
		return NewErrorResult(t.Name, t.Type, "Executor not set", "NO_EXECUTOR", 0, "工具执行器未设置")
	}
	return t.Executor.Execute(ctx, params)
}

func (t *Tool) ValidateParams(params map[string]interface{}) error {
	for _, param := range t.Parameters {
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
