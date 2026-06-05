package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type GrepSearchExecutor struct{}

func (e *GrepSearchExecutor) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Reason for searching file contents. Always provide description as the first parameter",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory path to search in (relative or absolute)",
			},
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Text or regex pattern to search for",
			},
			"glob": map[string]interface{}{
				"type":        "string",
				"description": "Optional glob filter, e.g. *.go",
			},
			"case_sensitive": map[string]interface{}{
				"type":        "boolean",
				"description": "Case sensitive search (default true)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of matches (default 200)",
			},
			"max_chars": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum characters returned (default 12000)",
			},
		},
		"required": []string{"description", "path", "pattern"},
	}
}

func (e *GrepSearchExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	start := time.Now()

	_, ok := params["description"].(string)
	if !ok {
		return NewErrorResult("grep_search", "function", "description parameter is required", "MISSING_DESCRIPTION", time.Since(start), "缺少必需参数: description")
	}

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return NewErrorResult("grep_search", "function", "path parameter is required", "MISSING_PATH", time.Since(start), "缺少必需参数: path")
	}

	pattern, ok := params["pattern"].(string)
	if !ok || pattern == "" {
		return NewErrorResult("grep_search", "function", "pattern parameter is required", "MISSING_PATTERN", time.Since(start), "缺少必需参数: pattern")
	}

	glob, _ := params["glob"].(string)

	caseSensitive := true
	if cs, ok := params["case_sensitive"].(bool); ok {
		caseSensitive = cs
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
		return NewErrorResult("grep_search", "function", err.Error(), "PATH_ERROR", time.Since(start), "路径解析失败")
	}

	var matches []string
	var searchMode string

	matches, searchMode, err = searchFiles(ctx, absPath, pattern, glob, caseSensitive, limit)
	if err != nil {
		return NewErrorResult("grep_search", "function", err.Error(), "SEARCH_FAILED", time.Since(start), "搜索失败")
	}

	totalMatches := len(matches)
	shownMatches := totalMatches
	if len(matches) > limit {
		matches = matches[:limit]
		shownMatches = limit
	}

	content := strings.Join(matches, "\n")
	truncated := false
	if len(content) > maxChars {
		content = content[:maxChars]
		truncated = true
	}

	if shownMatches < totalMatches {
		truncated = true
	}

	executionTime := time.Since(start)

	result := map[string]interface{}{
		"path":           absPath,
		"pattern":        pattern,
		"glob":           glob,
		"caseSensitive":  caseSensitive,
		"totalMatches":   totalMatches,
		"shownMatches":   shownMatches,
		"truncated":      truncated,
		"matches":        matches,
		"content":        content,
		"searchMode":     searchMode,
	}

	message := "搜索完成"
	if truncated {
		message += "（已截断）"
	}

	return NewSuccessResult("grep_search", "function", result, executionTime, message)
}

func searchFiles(ctx context.Context, dirPath, pattern, glob string, caseSensitive bool, limit int) ([]string, string, error) {
	var matches []string

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, "", err
	}

	if !caseSensitive {
		re, err = regexp.Compile("(?i)" + pattern)
		if err != nil {
			return nil, "", err
		}
	}

	err = filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if glob != "" {
			matched, _ := filepath.Match(glob, d.Name())
			if !matched {
				return nil
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, path+":"+fmt.Sprintf("%d", i+1)+":"+line)
				if len(matches) >= limit {
					return filepath.SkipAll
				}
			}
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return nil, "", err
	}

	return matches, "go_stdlib", nil
}

func NewGrepSearchTool() *Tool {
	return NewTool(
		"grep_search",
		"function",
		"在指定目录下搜索文件内容。使用 Go 标准库实现，无需额外依赖。",
		[]ToolParameter{
			{
				Name:        "description",
				Type:        "string",
				Description: "说明为什么要搜索文件内容。始终将 description 作为第一个参数。",
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
				Description: "要搜索的文本或正则表达式模式",
				Required:    true,
			},
			{
				Name:        "glob",
				Type:        "string",
				Description: "可选的 glob 过滤器，例如 *.go",
				Required:    false,
			},
			{
				Name:        "case_sensitive",
				Type:        "boolean",
				Description: "是否区分大小写（默认 true）",
				Required:    false,
				Default:     true,
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
		&GrepSearchExecutor{},
	)
}
