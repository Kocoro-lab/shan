package tools

import (
	"context"
	"testing"
)

func TestNotify_Info(t *testing.T) {
	tool := &NotifyTool{}
	info := tool.Info()
	if info.Name != "notify" {
		t.Errorf("expected name 'notify', got %q", info.Name)
	}
	if len(info.Required) != 1 || info.Required[0] != "title" {
		t.Errorf("expected required [title], got %v", info.Required)
	}
}

func TestNotify_InvalidArgs(t *testing.T) {
	tool := &NotifyTool{}
	result, err := tool.Run(context.Background(), `not valid json`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for invalid JSON")
	}
}

func TestNotify_BuildScript(t *testing.T) {
	tests := []struct {
		title string
		body  string
		sound bool
		want  string
	}{
		{
			title: "Test",
			body:  "Hello",
			sound: false,
			want:  `display notification "Hello" with title "Test"`,
		},
		{
			title: "Test",
			body:  "Hello",
			sound: true,
			want:  `display notification "Hello" with title "Test" sound name "default"`,
		},
		{
			title: "Test",
			body:  "",
			sound: false,
			want:  `display notification "" with title "Test"`,
		},
		{
			title: `Say "hi"`,
			body:  `It's "great"`,
			sound: false,
			want:  `display notification "It's \"great\"" with title "Say \"hi\""`,
		},
	}
	for _, tt := range tests {
		got := buildNotifyScript(tt.title, tt.body, tt.sound)
		if got != tt.want {
			t.Errorf("buildNotifyScript(%q, %q, %v) = %q, want %q", tt.title, tt.body, tt.sound, got, tt.want)
		}
	}
}

func TestNotify_RequiresApproval(t *testing.T) {
	tool := &NotifyTool{}
	if !tool.RequiresApproval() {
		t.Error("expected RequiresApproval to return true")
	}
}
