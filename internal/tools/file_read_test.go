package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileRead_Run(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644)

	tool := &FileReadTool{}
	result, err := tool.Run(context.Background(), `{"path": "`+path+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !contains(result.Content, "1") || !contains(result.Content, "line1") {
		t.Errorf("expected line-numbered output, got: %s", result.Content)
	}
}

func TestFileRead_NotFound(t *testing.T) {
	tool := &FileReadTool{}
	result, err := tool.Run(context.Background(), `{"path": "/nonexistent/file.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing file")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
