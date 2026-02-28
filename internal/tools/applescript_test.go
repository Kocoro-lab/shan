package tools

import (
	"context"
	"runtime"
	"testing"
)

func TestAppleScript_Info(t *testing.T) {
	tool := &AppleScriptTool{}
	info := tool.Info()
	if info.Name != "applescript" {
		t.Errorf("expected name 'applescript', got %q", info.Name)
	}
	if len(info.Required) != 1 || info.Required[0] != "script" {
		t.Errorf("expected required [script], got %v", info.Required)
	}
}

func TestAppleScript_InvalidArgs(t *testing.T) {
	tool := &AppleScriptTool{}
	result, err := tool.Run(context.Background(), `not valid json`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid JSON")
	}
}

func TestAppleScript_SimpleScript(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("applescript tests require macOS")
	}
	tool := &AppleScriptTool{}
	result, err := tool.Run(context.Background(), `{"script": "return 1 + 1"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !contains(result.Content, "2") {
		t.Errorf("expected '2' in output, got: %s", result.Content)
	}
}

func TestAppleScript_InvalidScript(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("applescript tests require macOS")
	}
	tool := &AppleScriptTool{}
	result, err := tool.Run(context.Background(), `{"script": "this is not valid applescript code!!!"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid script")
	}
}

func TestAppleScript_RequiresApproval(t *testing.T) {
	tool := &AppleScriptTool{}
	if !tool.RequiresApproval() {
		t.Error("expected RequiresApproval to return true")
	}
}
