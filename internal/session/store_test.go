package session

import (
	"path/filepath"
	"testing"

	"github.com/Kocoro-lab/shan/internal/client"
)

func TestStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sess := &Session{
		ID:    "test-123",
		Title: "Test session",
		CWD:   "/tmp/test",
		Messages: []client.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		},
	}

	if err := store.Save(sess); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Load("test-123")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Title != "Test session" {
		t.Errorf("expected 'Test session', got %q", loaded.Title)
	}
	if len(loaded.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(loaded.Messages))
	}
}

func TestStore_List(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Save(&Session{ID: "aaa", Title: "First"})
	store.Save(&Session{ID: "bbb", Title: "Second"})

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Save(&Session{ID: "del-me", Title: "Delete me"})

	if err := store.Delete("del-me"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	if _, err := store.Load("del-me"); err == nil {
		t.Error("expected error loading deleted session")
	}

	// Verify file is gone
	path := filepath.Join(dir, "del-me.json")
	if fileExists(path) {
		t.Error("session file should be deleted")
	}
}
