package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Kocoro-lab/shan/internal/agent"
)

type FileWriteTool struct{}

type fileWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *FileWriteTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name:        "file_write",
		Description: "Write content to a file. Creates parent directories if needed. Overwrites existing files.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path to write"},
				"content": map[string]any{"type": "string", "description": "Content to write"},
			},
		},
		Required: []string{"path", "content"},
	}
}

func (t *FileWriteTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var args fileWriteArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if err := os.MkdirAll(filepath.Dir(args.Path), 0755); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error creating directory: %v", err), IsError: true}, nil
	}

	if err := os.WriteFile(args.Path, []byte(args.Content), 0644); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path)}, nil
}

func (t *FileWriteTool) RequiresApproval() bool { return true }
