package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type StrReplaceExecutor struct{}

func (e *StrReplaceExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	start := time.Now()

	_, ok := params["description"].(string)
	if !ok {
		return NewErrorResult("str_replace", "function", "description parameter is required", "MISSING_DESCRIPTION", time.Since(start), "缺少必需参数: description")
	}

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return NewErrorResult("str_replace", "function", "path parameter is required", "MISSING_PATH", time.Since(start), "缺少必需参数: path")
	}

	oldText, ok := params["old_text"].(string)
	if !ok || oldText == "" {
		return NewErrorResult("str_replace", "function", "old_text parameter is required", "MISSING_OLD_TEXT", time.Since(start), "缺少必需参数: old_text")
	}

	newText, ok := params["new_text"].(string)
	if !ok {
		return NewErrorResult("str_replace", "function", "new_text parameter is required", "MISSING_NEW_TEXT", time.Since(start), "缺少必需参数: new_text")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return NewErrorResult("str_replace", "function", err.Error(), "PATH_ERROR", time.Since(start), "路径解析失败")
	}

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return NewErrorResult("str_replace", "function", err.Error(), "FILE_NOT_FOUND", time.Since(start), "文件不存在")
	}

	if fileInfo.IsDir() {
		return NewErrorResult("str_replace", "function", "path is a directory", "IS_DIRECTORY", time.Since(start), "路径指向目录而非文件")
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return NewErrorResult("str_replace", "function", err.Error(), "READ_FILE_ERROR", time.Since(start), "读取文件失败")
	}

	contentStr := string(content)
	oldTextCount := strings.Count(contentStr, oldText)

	if oldTextCount == 0 {
		return NewErrorResult("str_replace", "function", "old_text not found in file", "OLD_TEXT_NOT_FOUND", time.Since(start), "旧文本在文件中未找到")
	}

	if oldTextCount > 1 {
		return NewErrorResult("str_replace", "function", "old_text is not unique in file", "OLD_TEXT_NOT_UNIQUE", time.Since(start), "旧文本在文件中不唯一，请确保 old_text 唯一")
	}

	newContent := strings.Replace(contentStr, oldText, newText, 1)

	if err := os.WriteFile(absPath, []byte(newContent), fileInfo.Mode()); err != nil {
		return NewErrorResult("str_replace", "function", err.Error(), "WRITE_FILE_ERROR", time.Since(start), "写入文件失败")
	}

	executionTime := time.Since(start)

	result := map[string]interface{}{
		"path":           absPath,
		"oldTextLength":  len(oldText),
		"newTextLength":  len(newText),
		"oldContentSize": len(contentStr),
		"newContentSize": len(newContent),
		"replaced":       true,
	}

	return NewSuccessResult("str_replace", "function", result, executionTime, "替换成功")
}

func NewStrReplaceTool() *Tool {
	return NewTool(
		"str_replace",
		"function",
		"在文件内搜索并替换文本。确保 old_text 在文件中是唯一的。",
		[]ToolParameter{
			{
				Name:        "description",
				Type:        "string",
				Description: "说明为什么要执行此替换操作。始终将 description 作为第一个参数。",
				Required:    true,
			},
			{
				Name:        "path",
				Type:        "string",
				Description: "文件路径（相对路径或绝对路径）",
				Required:    true,
			},
			{
				Name:        "old_text",
				Type:        "string",
				Description: "要被替换的旧文本。确保此文本在文件中唯一。",
				Required:    true,
			},
			{
				Name:        "new_text",
				Type:        "string",
				Description: "新文本",
				Required:    true,
			},
		},
		&StrReplaceExecutor{},
	)
}
