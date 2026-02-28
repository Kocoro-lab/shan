package tools

import (
	"context"
	"strings"
	"testing"
)

func TestComputer_Info(t *testing.T) {
	tool := &ComputerTool{}
	info := tool.Info()
	if info.Name != "computer" {
		t.Errorf("expected name 'computer', got %q", info.Name)
	}
	if len(info.Required) != 1 || info.Required[0] != "action" {
		t.Errorf("expected required [action], got %v", info.Required)
	}
	props, ok := info.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map in parameters")
	}
	for _, key := range []string{"action", "x", "y", "text", "keys", "button", "clicks"} {
		if _, exists := props[key]; !exists {
			t.Errorf("expected property %q in schema", key)
		}
	}
}

func TestComputer_RequiresApproval(t *testing.T) {
	tool := &ComputerTool{}
	if !tool.RequiresApproval() {
		t.Error("expected RequiresApproval to return true")
	}
}

func TestComputer_InvalidArgs(t *testing.T) {
	tool := &ComputerTool{}
	result, err := tool.Run(context.Background(), `not valid json`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid JSON")
	}
}

func TestComputer_MissingAction(t *testing.T) {
	tool := &ComputerTool{}
	result, err := tool.Run(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing action")
	}
	if !contains(result.Content, "missing required parameter: action") {
		t.Errorf("expected 'missing required parameter: action' in error, got: %s", result.Content)
	}
}

func TestComputer_UnknownAction(t *testing.T) {
	tool := &ComputerTool{}
	result, err := tool.Run(context.Background(), `{"action": "fly"}`)
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

func TestComputer_TypeMissingText(t *testing.T) {
	tool := &ComputerTool{}
	result, err := tool.Run(context.Background(), `{"action": "type"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for type without text")
	}
	if !contains(result.Content, "type action requires 'text' parameter") {
		t.Errorf("expected text parameter error, got: %s", result.Content)
	}
}

func TestComputer_HotkeyMissingKeys(t *testing.T) {
	tool := &ComputerTool{}
	result, err := tool.Run(context.Background(), `{"action": "hotkey"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for hotkey without keys")
	}
	if !contains(result.Content, "hotkey action requires 'keys' parameter") {
		t.Errorf("expected keys parameter error, got: %s", result.Content)
	}
}

func TestComputer_BuildClickScript_LeftSingle(t *testing.T) {
	script := buildClickScript(100, 200, "", 0)
	if !strings.Contains(script, "(100, 200)") {
		t.Errorf("expected coordinates (100, 200) in script, got:\n%s", script)
	}
	if !strings.Contains(script, "kCGEventLeftMouseDown") {
		t.Errorf("expected kCGEventLeftMouseDown in script, got:\n%s", script)
	}
	if !strings.Contains(script, "kCGEventLeftMouseUp") {
		t.Errorf("expected kCGEventLeftMouseUp in script, got:\n%s", script)
	}
	if !strings.Contains(script, "range(1)") {
		t.Errorf("expected range(1) for single click, got:\n%s", script)
	}
}

func TestComputer_BuildClickScript_RightDouble(t *testing.T) {
	script := buildClickScript(50, 75, "right", 2)
	if !strings.Contains(script, "(50, 75)") {
		t.Errorf("expected coordinates (50, 75) in script, got:\n%s", script)
	}
	if !strings.Contains(script, "kCGEventRightMouseDown") {
		t.Errorf("expected kCGEventRightMouseDown in script, got:\n%s", script)
	}
	if !strings.Contains(script, "kCGEventRightMouseUp") {
		t.Errorf("expected kCGEventRightMouseUp in script, got:\n%s", script)
	}
	if !strings.Contains(script, "range(2)") {
		t.Errorf("expected range(2) for double click, got:\n%s", script)
	}
}

func TestComputer_BuildMoveScript(t *testing.T) {
	script := buildMoveScript(300, 400)
	if !strings.Contains(script, "(300, 400)") {
		t.Errorf("expected coordinates (300, 400) in script, got:\n%s", script)
	}
	if !strings.Contains(script, "kCGEventMouseMoved") {
		t.Errorf("expected kCGEventMouseMoved in script, got:\n%s", script)
	}
}

func TestComputer_BuildHotkeyScript_SingleKey(t *testing.T) {
	script, err := buildHotkeyScript("a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `tell application "System Events" to keystroke "a"`
	if script != expected {
		t.Errorf("expected %q, got %q", expected, script)
	}
}

func TestComputer_BuildHotkeyScript_CommandC(t *testing.T) {
	script, err := buildHotkeyScript("command+c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, `keystroke "c"`) {
		t.Errorf("expected keystroke c in script, got: %s", script)
	}
	if !strings.Contains(script, "command down") {
		t.Errorf("expected command down in script, got: %s", script)
	}
}

func TestComputer_BuildHotkeyScript_CmdAlias(t *testing.T) {
	script, err := buildHotkeyScript("cmd+v")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "command down") {
		t.Errorf("expected command down for cmd alias, got: %s", script)
	}
}

func TestComputer_BuildHotkeyScript_MultiModifier(t *testing.T) {
	script, err := buildHotkeyScript("command+shift+4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "command down") {
		t.Errorf("expected command down in script, got: %s", script)
	}
	if !strings.Contains(script, "shift down") {
		t.Errorf("expected shift down in script, got: %s", script)
	}
	if !strings.Contains(script, `keystroke "4"`) {
		t.Errorf("expected keystroke 4 in script, got: %s", script)
	}
}

func TestComputer_BuildHotkeyScript_AltCtrl(t *testing.T) {
	script, err := buildHotkeyScript("alt+ctrl+t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(script, "option down") {
		t.Errorf("expected option down for alt, got: %s", script)
	}
	if !strings.Contains(script, "control down") {
		t.Errorf("expected control down for ctrl, got: %s", script)
	}
}

func TestComputer_BuildHotkeyScript_UnknownModifier(t *testing.T) {
	_, err := buildHotkeyScript("super+a")
	if err == nil {
		t.Error("expected error for unknown modifier 'super'")
	}
	if !strings.Contains(err.Error(), "unknown modifier") {
		t.Errorf("expected 'unknown modifier' in error, got: %s", err.Error())
	}
}
