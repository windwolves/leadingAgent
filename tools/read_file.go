package tools

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ReadFileExecutor struct{}

func (e *ReadFileExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	start := time.Now()

	path, ok := params["path"].(string)
	if !ok || path == "" {
		return NewErrorResult("read_file", "function", "path parameter is required", "MISSING_PATH", time.Since(start), "缺少必需参数: path")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return NewErrorResult("read_file", "function", err.Error(), "PATH_ERROR", time.Since(start), "路径解析失败")
	}

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return NewErrorResult("read_file", "function", err.Error(), "FILE_NOT_FOUND", time.Since(start), "文件不存在")
	}

	if fileInfo.IsDir() {
		return NewErrorResult("read_file", "function", "path is a directory", "IS_DIRECTORY", time.Since(start), "路径指向目录而非文件")
	}

	startLine := 0
	if sl, ok := params["start_line"].(float64); ok {
		startLine = int(sl)
	}
	if startLine < 0 {
		startLine = 0
	}

	endLine := 0
	if el, ok := params["end_line"].(float64); ok {
		endLine = int(el)
	}

	if startLine > 0 && endLine > 0 && startLine > endLine {
		return NewErrorResult("read_file", "function", "startLine must be less than or equal to endLine", "INVALID_RANGE", time.Since(start), "起始行号必须小于或等于结束行号")
	}

	maxChars := 12000
	if mc, ok := params["max_chars"].(float64); ok {
		maxChars = int(mc)
	}
	if maxChars <= 0 {
		maxChars = 12000
	}

	file, err := os.Open(absPath)
	if err != nil {
		return NewErrorResult("read_file", "function", err.Error(), "FILE_OPEN_ERROR", time.Since(start), "文件打开失败")
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []string
	currentLine := 0
	totalLines := 0

	for scanner.Scan() {
		totalLines++
		if startLine == 0 || currentLine >= startLine-1 {
			if endLine == 0 || currentLine <= endLine-1 {
				lines = append(lines, scanner.Text())
			}
		}
		currentLine++
	}

	if err := scanner.Err(); err != nil {
		return NewErrorResult("read_file", "function", err.Error(), "FILE_READ_ERROR", time.Since(start), "文件读取失败")
	}

	if startLine > 0 && startLine > totalLines {
		return NewErrorResult("read_file", "function", "startLine is out of range", "START_LINE_OUT_OF_RANGE", time.Since(start), "起始行号超出文件范围")
	}

	var content string
	var truncated bool
	if startLine > 0 || endLine > 0 {
		var sb strings.Builder
		for i, line := range lines {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(line)
		}
		content = sb.String()
	} else {
		content = strings.Join(lines, "\n")
	}

	if len(content) > maxChars {
		content = content[:maxChars]
		truncated = true
	}

	executionTime := time.Since(start)

	result := map[string]interface{}{
		"path":       absPath,
		"content":    content,
		"totalLines": totalLines,
		"startLine":  startLine,
		"endLine":    endLine,
		"readLines":  len(lines),
		"truncated":  truncated,
		"maxChars":   maxChars,
		"charCount":  len(content),
	}

	message := "文件读取成功"
	if startLine > 0 || endLine > 0 {
		message = "文件部分内容读取成功"
	}
	if truncated {
		message += "（已截断）"
	}

	return NewSuccessResult("read_file", "function", result, executionTime, message)
}

func NewReadFileTool() *Tool {
	return NewTool(
		"read_file",
		"function",
		"读取文件内容。支持按行范围读取和字符数限制，避免大文件占用过多上下文。",
		[]ToolParameter{
			{
				Name:        "path",
				Type:        "string",
				Description: "文件路径（相对路径或绝对路径）",
				Required:    true,
			},
			{
				Name:        "start_line",
				Type:        "integer",
				Description: "起始行号（从1开始，默认0表示从开头）",
				Required:    false,
				Default:     0,
			},
			{
				Name:        "end_line",
				Type:        "integer",
				Description: "结束行号（默认0表示读到末尾）",
				Required:    false,
				Default:     0,
			},
			{
				Name:        "max_chars",
				Type:        "integer",
				Description: "最大返回字符数（默认12000）",
				Required:    false,
				Default:     12000,
			},
		},
		&ReadFileExecutor{},
	)
}
