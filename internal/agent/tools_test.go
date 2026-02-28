package agent

import (
	"context"
	"testing"
)

func TestToolRegistry_Get(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockTool{name: "file_read"})

	tool, ok := reg.Get("file_read")
	if !ok {
		t.Fatal("expected to find file_read")
	}
	if tool.Info().Name != "file_read" {
		t.Errorf("expected 'file_read', got %q", tool.Info().Name)
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestToolRegistry_Schemas(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockTool{name: "file_read"})
	reg.Register(&mockTool{name: "bash"})

	schemas := reg.Schemas()
	if len(schemas) != 2 {
		t.Errorf("expected 2 schemas, got %d", len(schemas))
	}
}

type mockTool struct {
	name string
}

func (m *mockTool) Info() ToolInfo {
	return ToolInfo{
		Name:        m.name,
		Description: "mock tool",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (m *mockTool) Run(ctx context.Context, args string) (ToolResult, error) {
	return ToolResult{Content: "mock result"}, nil
}

func (m *mockTool) RequiresApproval() bool { return false }
