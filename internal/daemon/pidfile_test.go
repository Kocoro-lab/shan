package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquirePIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	pf, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// PID file should contain our PID.
	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("read pid: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}

	pf.Close()
}

func TestDoubleAcquireFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	pf1, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer pf1.Close()

	// Second acquire should fail.
	_, err = AcquirePIDFile(path)
	if err == nil {
		t.Fatal("second acquire should have failed")
	}
	t.Logf("expected error: %v", err)
}

func TestCloseReleasesLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	pf1, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	pf1.Close()

	// After close, a new acquire should succeed.
	pf2, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("re-acquire after close: %v", err)
	}
	pf2.Close()
}

func TestStalePIDFileReclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// Write a stale PID file (no lock held).
	if err := os.WriteFile(path, []byte("99999\n"), 0600); err != nil {
		t.Fatalf("write stale pid: %v", err)
	}

	// Should succeed because no flock is held.
	pf, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("acquire stale: %v", err)
	}
	defer pf.Close()

	// Should have our PID now, not the stale one.
	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("read pid: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}

func TestReadPIDMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.pid")
	_, err := ReadPID(path)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadPIDInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(path, []byte("not-a-number\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ReadPID(path)
	if err == nil {
		t.Fatal("expected error for invalid content")
	}
}

func TestIsLocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// No file — not locked.
	_, locked := IsLocked(path)
	if locked {
		t.Fatal("should not be locked when file doesn't exist")
	}

	// Stale file — not locked.
	os.WriteFile(path, []byte("12345\n"), 0600)
	_, locked = IsLocked(path)
	if locked {
		t.Fatal("should not be locked for stale file")
	}

	// Acquire lock — should be locked.
	pf, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pid, locked := IsLocked(path)
	if !locked {
		t.Fatal("should be locked after acquire")
	}
	if pid != os.Getpid() {
		t.Errorf("locked pid = %d, want %d", pid, os.Getpid())
	}

	// Close — no longer locked.
	pf.Close()
	_, locked = IsLocked(path)
	if locked {
		t.Fatal("should not be locked after close")
	}
}

func TestCloseIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	pf, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	pf.Close()
	pf.Close() // should not panic
}
