package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/schedule"
)

func TestSchedulerDedupSameMinute(t *testing.T) {
	dir := t.TempDir()
	mgr := schedule.NewManager(filepath.Join(dir, "schedules.json"))

	id, err := mgr.Create("bot", "* * * * *", "hello")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = id

	s := NewScheduler(mgr, nil)

	now := time.Date(2026, 3, 18, 10, 30, 0, 0, time.UTC)

	// First call at this minute should return 1.
	due := s.EvaluateDue(now)
	if len(due) != 1 {
		t.Fatalf("first call: got %d due, want 1", len(due))
	}

	// Second call at the same minute should return 0 (dedup).
	due = s.EvaluateDue(now.Add(15 * time.Second))
	if len(due) != 0 {
		t.Fatalf("second call same minute: got %d due, want 0", len(due))
	}

	// Next minute should return 1 again.
	due = s.EvaluateDue(now.Add(time.Minute))
	if len(due) != 1 {
		t.Fatalf("next minute: got %d due, want 1", len(due))
	}
}

func TestSchedulerSkipsDisabled(t *testing.T) {
	dir := t.TempDir()
	mgr := schedule.NewManager(filepath.Join(dir, "schedules.json"))

	id, err := mgr.Create("bot", "* * * * *", "hello")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	disabled := false
	if err := mgr.Update(id, &schedule.UpdateOpts{Enabled: &disabled}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	s := NewScheduler(mgr, nil)
	now := time.Date(2026, 3, 18, 10, 30, 0, 0, time.UTC)

	due := s.EvaluateDue(now)
	if len(due) != 0 {
		t.Fatalf("got %d due, want 0 (disabled)", len(due))
	}
}

func TestSchedulerPrunesDeletedEntries(t *testing.T) {
	dir := t.TempDir()
	mgr := schedule.NewManager(filepath.Join(dir, "schedules.json"))

	id, err := mgr.Create("bot", "* * * * *", "hello")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	s := NewScheduler(mgr, nil)
	now := time.Date(2026, 3, 18, 10, 30, 0, 0, time.UTC)

	// Evaluate to populate lastFired.
	due := s.EvaluateDue(now)
	if len(due) != 1 {
		t.Fatalf("first call: got %d due, want 1", len(due))
	}

	// Verify lastFired has the entry.
	s.mu.Lock()
	if _, ok := s.lastFired[id]; !ok {
		s.mu.Unlock()
		t.Fatal("expected lastFired entry after evaluate")
	}
	s.mu.Unlock()

	// Delete the schedule.
	if err := mgr.Remove(id); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Evaluate again — should prune the deleted entry.
	_ = s.EvaluateDue(now.Add(time.Minute))

	s.mu.Lock()
	if _, ok := s.lastFired[id]; ok {
		s.mu.Unlock()
		t.Fatal("expected lastFired entry to be pruned after delete")
	}
	s.mu.Unlock()
}

func TestSchedulerSkipsMalformedCron(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "schedules.json")

	// Write bad JSON directly to bypass validation.
	bad := `[{"id":"bad1","agent":"bot","cron":"not a cron","prompt":"hello","enabled":true,"sync_status":"ok","created_at":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(indexPath, []byte(bad), 0600); err != nil {
		t.Fatalf("write bad schedule: %v", err)
	}

	mgr := schedule.NewManager(indexPath)
	s := NewScheduler(mgr, nil)

	now := time.Date(2026, 3, 18, 10, 30, 0, 0, time.UTC)
	due := s.EvaluateDue(now)
	if len(due) != 0 {
		t.Fatalf("got %d due, want 0 (malformed cron)", len(due))
	}
}
