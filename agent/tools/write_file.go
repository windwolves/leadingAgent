package tools

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

type WriteFileExecutor struct{}

func (e *WriteFileExecutor) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Reason for writing this file. Always provide description as the first parameter",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path (relative or absolute)",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "File content to write",
			},
		},
		"required": []string{"description", "path", "content"},
	}
}

func (e *WriteFileExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	start := time.Now()

	_, ok := params["description"].(string)
	if !ok {
		return NewErrorResult("write_file", "function", "description parameter is required", "MISSING_DESCRIPTION", time.Since(start), "缺少必需参数: description")
	}

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return NewErrorResult("write_file", "function", "path parameter is required", "MISSING_PATH", time.Since(start), "缺少必需参数: path")
	}

	content, ok := params["content"].(string)
	if !ok {
		return NewErrorResult("write_file", "function", "content parameter is required", "MISSING_CONTENT", time.Since(start), "缺少必需参数: content")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return NewErrorResult("write_file", "function", err.Error(), "PATH_ERROR", time.Since(start), "路径解析失败")
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return NewErrorResult("write_file", "function", err.Error(), "CREATE_DIR_ERROR", time.Since(start), "创建父目录失败")
	}

	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return NewErrorResult("write_file", "function", err.Error(), "WRITE_FILE_ERROR", time.Since(start), "写入文件失败")
	}

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return NewErrorResult("write_file", "function", err.Error(), "GET_FILE_INFO_ERROR", time.Since(start), "获取文件信息失败")
	}

	executionTime := time.Since(start)

	result := map[string]interface{}{
		"path":        absPath,
		"contentSize": len(content),
		"fileSize":    fileInfo.Size(),
		"created":     fileInfo.ModTime(),
	}

	return NewSuccessResult("write_file", "function", result, executionTime, "文件写入成功")
}

func NewWriteFileTool() *Tool {
	return NewTool(
		"write_file",
		"function",
		"创建或覆盖文件。自动创建不存在的父目录。",
		[]ToolParameter{
			{
				Name:        "description",
				Type:        "string",
				Description: "说明为什么要写入文件。始终将 description 作为第一个参数。",
				Required:    true,
			},
			{
				Name:        "path",
				Type:        "string",
				Description: "文件路径（相对路径或绝对路径）",
				Required:    true,
			},
			{
				Name:        "content",
				Type:        "string",
				Description: "文件内容",
				Required:    true,
			},
		},
		&WriteFileExecutor{},
	)
}
