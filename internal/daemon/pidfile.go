package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// PIDFile manages a flock-guarded PID file for daemon single-instance enforcement.
// The flock is held for the entire daemon lifetime. On crash, the OS releases
// the lock automatically — no stale PID file problem.
type PIDFile struct {
	path string
	file *os.File
}

// AcquirePIDFile attempts to acquire an exclusive flock on the PID file at path.
// If another daemon holds the lock, it returns an error with the existing PID.
// On success, the current process PID is written to the file and the lock is held
// until Close() is called (or the process exits).
func AcquirePIDFile(path string) (*PIDFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open pid file: %w", err)
	}

	// Non-blocking exclusive lock.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// Lock held by another process — read existing PID for the error message.
		existingPID := readPIDFromFile(f)
		f.Close()
		if existingPID > 0 {
			return nil, fmt.Errorf("daemon already running (PID %d)", existingPID)
		}
		return nil, fmt.Errorf("daemon already running (could not read PID)")
	}

	// Lock acquired — write our PID.
	if err := f.Truncate(0); err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, fmt.Errorf("truncate pid file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, fmt.Errorf("seek pid file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, fmt.Errorf("write pid: %w", err)
	}
	if err := f.Sync(); err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		return nil, fmt.Errorf("sync pid file: %w", err)
	}

	return &PIDFile{path: path, file: f}, nil
}

// Close removes the PID file, releases the flock, and closes the file descriptor.
// File is removed while lock is still held to prevent a window where the file
// exists but is unlocked.
func (p *PIDFile) Close() {
	if p.file == nil {
		return
	}
	os.Remove(p.path)
	syscall.Flock(int(p.file.Fd()), syscall.LOCK_UN)
	p.file.Close()
	p.file = nil
}

// ReadPID reads the PID from a PID file without acquiring a lock.
// Returns 0 and an error if the file doesn't exist or contains invalid data.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file content: %w", err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid: %d", pid)
	}
	return pid, nil
}

// IsLocked checks whether the PID file at path is currently locked by another process.
// Returns the PID if locked, 0 if not locked or file doesn't exist.
func IsLocked(path string) (int, bool) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return 0, false
	}
	defer f.Close()

	// Try non-blocking lock — if we get it, no one else holds it.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0, false
	}

	pid := readPIDFromFile(f)
	return pid, true
}

func readPIDFromFile(f *os.File) int {
	if _, err := f.Seek(0, 0); err != nil {
		return 0
	}
	buf := make([]byte, 32)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
