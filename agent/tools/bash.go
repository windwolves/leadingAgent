package tools

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

type BashExecutor struct{}

func (e *BashExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	start := time.Now()

	command, ok := params["command"].(string)
	if !ok || command == "" {
		return NewErrorResult("bash", "function", "command parameter is required", "MISSING_COMMAND", time.Since(start), "缺少必需参数: command")
	}

	var shell string
	var shellFlag string

	switch runtime.GOOS {
	case "windows":
		shell = "powershell"
		shellFlag = "-Command"
	default:
		shell = "bash"
		shellFlag = "-c"
	}

	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, shell, shellFlag, command)
	} else {
		cmd = exec.Command(shell, shellFlag, command)
	}

	output, err := cmd.CombinedOutput()
	executionTime := time.Since(start)

	if err != nil {
		return NewErrorResult(
			"bash",
			"function",
			err.Error(),
			"EXECUTION_ERROR",
			executionTime,
			"命令执行失败",
		)
	}

	return NewSuccessResult(
		"bash",
		"function",
		map[string]interface{}{
			"command":  command,
			"shell":    shell,
			"output":   string(output),
			"exitCode": cmd.ProcessState.ExitCode(),
		},
		executionTime,
		"命令执行成功",
	)
}

func NewBashTool() *Tool {
	return NewTool(
		"bash",
		"function",
		"在系统中执行命令。使用此工具执行shell命令、运行脚本或获取系统信息。",
		[]ToolParameter{
			{
				Name:        "command",
				Type:        "string",
				Description: "要执行的命令",
				Required:    true,
			},
		},
		&BashExecutor{},
	)
}
