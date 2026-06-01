package tools

import (
	"context"
	"path/filepath"
	"time"
)

const DEFAULT_LIMIT = 200
const DEFAULT_MAX_CHARS = 12000

type GlobSearchExecutor struct{}

func (e *GlobSearchExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	start := time.Now()

	_, ok := params["description"].(string)
	if !ok {
		return NewErrorResult("glob_search", "function", "description parameter is required", "MISSING_DESCRIPTION", time.Since(start), "缺少必需参数: description")
	}

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return NewErrorResult("glob_search", "function", "path parameter is required", "MISSING_PATH", time.Since(start), "缺少必需参数: path")
	}

	pattern, ok := params["pattern"].(string)
	if !ok || pattern == "" {
		return NewErrorResult("glob_search", "function", "pattern parameter is required", "MISSING_PATTERN", time.Since(start), "缺少必需参数: pattern")
	}

	limit := DEFAULT_LIMIT
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
		if limit <= 0 {
			limit = DEFAULT_LIMIT
		}
	}

	maxChars := DEFAULT_MAX_CHARS
	if mc, ok := params["max_chars"].(float64); ok {
		maxChars = int(mc)
		if maxChars <= 0 {
			maxChars = DEFAULT_MAX_CHARS
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return NewErrorResult("glob_search", "function", err.Error(), "PATH_ERROR", time.Since(start), "路径解析失败")
	}

	matches, err := filepath.Glob(filepath.Join(absPath, pattern))
	if err != nil {
		return NewErrorResult("glob_search", "function", err.Error(), "GLOB_SEARCH_FAILED", time.Since(start), "glob 搜索失败")
	}

	if len(matches) > limit {
		matches = matches[:limit]
	}

	var content string
	var truncated bool
	for i, match := range matches {
		if i > 0 {
			content += "\n"
		}
		content += match
	}

	if len(content) > maxChars {
		content = content[:maxChars]
		truncated = true
	}

	executionTime := time.Since(start)

	result := map[string]interface{}{
		"path":        absPath,
		"pattern":     pattern,
		"matchCount":  len(matches),
		"truncated":   truncated,
		"matches":     matches,
		"content":     content,
	}

	message := "搜索完成"
	if truncated {
		message += "（已截断）"
	}

	return NewSuccessResult("glob_search", "function", result, executionTime, message)
}

func NewGlobSearchTool() *Tool {
	return NewTool(
		"glob_search",
		"function",
		"在指定目录下查找匹配 glob 模式的文件。",
		[]ToolParameter{
			{
				Name:        "description",
				Type:        "string",
				Description: "说明为什么要查找文件。始终将 description 作为第一个参数。",
				Required:    true,
			},
			{
				Name:        "path",
				Type:        "string",
				Description: "要搜索的目录路径（相对路径或绝对路径）",
				Required:    true,
			},
			{
				Name:        "pattern",
				Type:        "string",
				Description: "Glob 模式，例如 **/*.go 或 src/**/*.tsx",
				Required:    true,
			},
			{
				Name:        "limit",
				Type:        "integer",
				Description: "最大匹配数量（默认200）",
				Required:    false,
				Default:     DEFAULT_LIMIT,
			},
			{
				Name:        "max_chars",
				Type:        "integer",
				Description: "最大返回字符数（默认12000）",
				Required:    false,
				Default:     DEFAULT_MAX_CHARS,
			},
		},
		&GlobSearchExecutor{},
	)
}
