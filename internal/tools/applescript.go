package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/Kocoro-lab/shan/internal/agent"
)

type AppleScriptTool struct{}

type appleScriptArgs struct {
	Script string `json:"script"`
}

func (t *AppleScriptTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name:        "applescript",
		Description: "Execute an AppleScript script via osascript. Can control macOS apps, UI automation, and system features.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"script": map[string]any{"type": "string", "description": "AppleScript code to execute"},
			},
		},
		Required: []string{"script"},
	}
}

func (t *AppleScriptTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var args appleScriptArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	cmd := exec.CommandContext(ctx, "osascript", "-e", args.Script)
	output, err := cmd.CombinedOutput()

	result := string(output)
	if len(result) > 10240 {
		result = result[:10240] + "\n... (truncated)"
	}

	if err != nil {
		return agent.ToolResult{
			Content: fmt.Sprintf("osascript error: %v\n%s", err, result),
			IsError: true,
		}, nil
	}

	if result == "" {
		return agent.ToolResult{Content: "script executed successfully (no output)"}, nil
	}

	return agent.ToolResult{Content: result}, nil
}

func (t *AppleScriptTool) RequiresApproval() bool { return true }
