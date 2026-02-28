package tools

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
)

func TestClipboard_Info(t *testing.T) {
	tool := &ClipboardTool{}
	info := tool.Info()
	if info.Name != "clipboard" {
		t.Errorf("expected name 'clipboard', got %q", info.Name)
	}
	if len(info.Required) != 1 || info.Required[0] != "action" {
		t.Errorf("expected required [action], got %v", info.Required)
	}
}

func TestClipboard_InvalidArgs(t *testing.T) {
	tool := &ClipboardTool{}
	result, err := tool.Run(context.Background(), `not valid json`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid JSON")
	}
}

func TestClipboard_UnknownAction(t *testing.T) {
	tool := &ClipboardTool{}
	result, err := tool.Run(context.Background(), `{"action": "delete"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for unknown action")
	}
	if !contains(result.Content, "unknown action") {
		t.Errorf("expected 'unknown action' in error, got: %s", result.Content)
	}
}

func TestClipboard_WriteEmptyContent(t *testing.T) {
	tool := &ClipboardTool{}
	result, err := tool.Run(context.Background(), `{"action": "write"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for write without content")
	}
}

func TestClipboard_ReadWrite(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("clipboard tests require macOS")
	}
	if _, err := exec.LookPath("pbcopy"); err != nil {
		t.Skip("pbcopy not available")
	}

	tool := &ClipboardTool{}

	// Write
	result, err := tool.Run(context.Background(), `{"action": "write", "content": "shannon-test-clipboard"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}

	// Read back
	result, err = tool.Run(context.Background(), `{"action": "read"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !contains(result.Content, "shannon-test-clipboard") {
		t.Errorf("expected clipboard content 'shannon-test-clipboard', got: %s", result.Content)
	}
}

func TestClipboard_RequiresApproval(t *testing.T) {
	tool := &ClipboardTool{}
	if !tool.RequiresApproval() {
		t.Error("expected RequiresApproval to return true")
	}
}
