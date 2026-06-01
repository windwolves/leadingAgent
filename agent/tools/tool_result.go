package tools

import "time"

type ToolResult interface {
	IsSuccess() bool
	GetMessage() string
}

type SuccessResult struct {
	ToolName      string                 `json:"toolName"`
	ToolType      string                 `json:"toolType"`
	Result        map[string]interface{} `json:"result"`
	ExecutionTime time.Duration          `json:"executionTime"`
	Message       string                 `json:"message"`
}

func NewSuccessResult(toolName, toolType string, result map[string]interface{}, executionTime time.Duration, message string) *SuccessResult {
	return &SuccessResult{
		ToolName:      toolName,
		ToolType:      toolType,
		Result:        result,
		ExecutionTime: executionTime,
		Message:       message,
	}
}

func (r *SuccessResult) IsSuccess() bool {
	return true
}

func (r *SuccessResult) GetMessage() string {
	return r.Message
}

type ErrorResult struct {
	ToolName      string `json:"toolName"`
	ToolType      string `json:"toolType"`
	ErrorMessage  string `json:"errorMessage"`
	ErrorCode     string `json:"errorCode"`
	ExecutionTime time.Duration `json:"executionTime"`
	Message       string `json:"message"`
}

func NewErrorResult(toolName, toolType, errorMessage, errorCode string, executionTime time.Duration, message string) *ErrorResult {
	return &ErrorResult{
		ToolName:      toolName,
		ToolType:      toolType,
		ErrorMessage:  errorMessage,
		ErrorCode:     errorCode,
		ExecutionTime: executionTime,
		Message:       message,
	}
}

func (r *ErrorResult) IsSuccess() bool {
	return false
}

func (r *ErrorResult) GetMessage() string {
	return r.Message
}

type ToolCallInfo struct {
	ToolName string                 `json:"toolName"`
	ToolType string                 `json:"toolType"`
	Params   map[string]interface{} `json:"params"`
}
