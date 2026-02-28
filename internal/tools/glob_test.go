package tools

import (
	"context"
	"strings"
	"testing"
)

func TestGlob_RecursivePattern(t *testing.T) {
	tool := &GlobTool{}
	// Use the project root as base; tests run from internal/tools/
	result, err := tool.Run(context.Background(), `{"pattern": "**/*.go", "path": "../.."}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "main.go") {
		t.Errorf("expected to find main.go in recursive glob, got: %s", result.Content)
	}
}
