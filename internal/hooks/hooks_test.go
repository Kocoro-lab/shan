package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// newTestRunner creates a HookRunner that allows temp directories for script execution.
func newTestRunner(t *testing.T, config HookConfig) *HookRunner {
	t.Helper()
	runner := NewHookRunner(config)
	// t.TempDir() on macOS uses $TMPDIR (/var/folders/...), not /tmp.
	// Discover the actual temp base by creating a temp dir and taking its parent.
	probe := t.TempDir()
	runner.allowedDirs = []string{filepath.Dir(probe)}
	return runner
}

func TestMatcherRegex(t *testing.T) {
	runner := NewHookRunner(HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "^bash$", Command: "./hook.sh"},
			{Matcher: "file_.*", Command: "./hook2.sh"},
			{Matcher: "", Command: "./catch-all.sh"},
		},
	})

	tests := []struct {
		toolName string
		want     int
	}{
		{"bash", 2},       // matches ^bash$ and catch-all
		{"file_read", 2},  // matches file_.* and catch-all
		{"file_write", 2}, // matches file_.* and catch-all
		{"unknown", 1},    // matches catch-all only
		{"bash_extra", 1}, // no match for ^bash$, matches catch-all
	}

	for _, tt := range tests {
		matched := runner.matchEntries(runner.config.PreToolUse, tt.toolName)
		if len(matched) != tt.want {
			t.Errorf("matchEntries(%q) got %d matches, want %d", tt.toolName, len(matched), tt.want)
		}
	}
}

func TestMatcherInvalidRegex(t *testing.T) {
	runner := NewHookRunner(HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "[invalid", Command: "./hook.sh"},
		},
	})

	matched := runner.matchEntries(runner.config.PreToolUse, "bash")
	if len(matched) != 0 {
		t.Errorf("invalid regex should not match, got %d matches", len(matched))
	}
}

func TestHookExecutionExitCode0(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	script := createTempScript(t, `#!/bin/sh
cat > /dev/null
echo "hook output"
exit 0
`)

	runner := newTestRunner(t, HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "", Command: script},
		},
	})

	decision, reason, err := runner.RunPreToolUse(context.Background(), "bash", `{"command":"ls"}`, "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != "allow" {
		t.Errorf("got decision %q, want %q", decision, "allow")
	}
	if reason != "" {
		t.Errorf("got reason %q, want empty", reason)
	}
}

func TestHookExecutionExitCode2Deny(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	script := createTempScript(t, `#!/bin/sh
cat > /dev/null
echo "dangerous command detected" >&2
exit 2
`)

	runner := newTestRunner(t, HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "", Command: script},
		},
	})

	decision, reason, err := runner.RunPreToolUse(context.Background(), "bash", `{"command":"rm -rf /"}`, "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != "deny" {
		t.Errorf("got decision %q, want %q", decision, "deny")
	}
	if reason != "dangerous command detected" {
		t.Errorf("got reason %q, want %q", reason, "dangerous command detected")
	}
}

func TestHookExecutionExitCode2EmptyStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	script := createTempScript(t, `#!/bin/sh
cat > /dev/null
exit 2
`)

	runner := newTestRunner(t, HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "", Command: script},
		},
	})

	decision, reason, err := runner.RunPreToolUse(context.Background(), "bash", `{"command":"test"}`, "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != "deny" {
		t.Errorf("got decision %q, want %q", decision, "deny")
	}
	if reason != "blocked by hook" {
		t.Errorf("got reason %q, want %q", reason, "blocked by hook")
	}
}

func TestHookExecutionNonZeroNon2Warning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	script := createTempScript(t, `#!/bin/sh
cat > /dev/null
echo "something went wrong" >&2
exit 1
`)

	runner := newTestRunner(t, HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "", Command: script},
		},
	})

	decision, _, err := runner.RunPreToolUse(context.Background(), "bash", `{"command":"ls"}`, "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-zero, non-2 exit should NOT deny; it's a warning
	if decision != "allow" {
		t.Errorf("got decision %q, want %q (non-blocking warning)", decision, "allow")
	}
}

func TestHookTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	script := createTempScript(t, `#!/bin/sh
cat > /dev/null
sleep 30
`)

	runner := newTestRunner(t, HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "", Command: script},
		},
	})
	runner.timeout = 500 * time.Millisecond

	start := time.Now()
	decision, _, err := runner.RunPreToolUse(context.Background(), "bash", `{"command":"ls"}`, "test-session")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Timeout should cause the hook to fail with a warning, not block
	if decision != "allow" {
		t.Errorf("got decision %q, want %q after timeout", decision, "allow")
	}
	if elapsed > 3*time.Second {
		t.Errorf("hook took %v, expected to be killed near timeout of 500ms", elapsed)
	}
}

func TestOutputTruncation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	// Generate output larger than 10KB using dd
	script := createTempScript(t, `#!/bin/sh
cat > /dev/null
dd if=/dev/zero bs=1 count=20000 2>/dev/null | tr '\0' 'A'
exit 0
`)

	runner := newTestRunner(t, HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "", Command: script},
		},
	})

	entry := runner.config.PreToolUse[0]
	input := HookInput{
		Event:        PreToolUse,
		ToolName:     "bash",
		ToolInput:    toRawJSON(`{"command":"ls"}`),
		ToolResponse: nullJSON(),
		SessionID:    "test-session",
	}

	_, stdout, _, err := runner.runHook(context.Background(), entry, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stdout) > maxOutputBytes {
		t.Errorf("stdout length %d exceeds max %d", len(stdout), maxOutputBytes)
	}
}

func TestPathRestrictionRejectAbsolute(t *testing.T) {
	_, err := resolveCommand("/usr/bin/something", nil)
	if err == nil {
		t.Fatal("expected error for absolute path outside ~/.shannon/")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error message %q should mention rejection", err.Error())
	}
}

func TestPathRestrictionAllowRelative(t *testing.T) {
	result, err := resolveCommand("./hooks/my-hook.sh", nil)
	if err != nil {
		t.Fatalf("unexpected error for relative path: %v", err)
	}
	if result != "./hooks/my-hook.sh" {
		t.Errorf("got %q, want %q", result, "./hooks/my-hook.sh")
	}
}

func TestPathRestrictionRejectBareCommand(t *testing.T) {
	_, err := resolveCommand("my-hook.sh", nil)
	if err == nil {
		t.Fatal("expected error for bare command name, got nil")
	}
	if !strings.Contains(err.Error(), "bare command") {
		t.Errorf("expected bare command error, got: %v", err)
	}
}

func TestPathRestrictionAllowShannonDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot resolve home directory")
	}

	shannonPath := filepath.Join(home, ".shannon", "hooks", "my-hook.sh")
	result, err := resolveCommand("~/.shannon/hooks/my-hook.sh", nil)
	if err != nil {
		t.Fatalf("unexpected error for ~/.shannon/ path: %v", err)
	}
	if result != shannonPath {
		t.Errorf("got %q, want %q", result, shannonPath)
	}
}

func TestPathRestrictionRejectAbsoluteOutsideShannon(t *testing.T) {
	_, err := resolveCommand("/tmp/malicious.sh", nil)
	if err == nil {
		t.Fatal("expected error for absolute path outside ~/.shannon/")
	}
}

func TestPathRestrictionEmptyCommand(t *testing.T) {
	_, err := resolveCommand("", nil)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestPathRestrictionExtraAllowed(t *testing.T) {
	result, err := resolveCommand("/tmp/hooks/test.sh", []string{"/tmp/hooks"})
	if err != nil {
		t.Fatalf("unexpected error for extra allowed path: %v", err)
	}
	if result != "/tmp/hooks/test.sh" {
		t.Errorf("got %q, want %q", result, "/tmp/hooks/test.sh")
	}
}

func TestEmptyConfigNoHooksRun(t *testing.T) {
	runner := NewHookRunner(HookConfig{})

	decision, reason, err := runner.RunPreToolUse(context.Background(), "bash", `{"command":"ls"}`, "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != "" {
		t.Errorf("got decision %q, want empty for no hooks", decision)
	}
	if reason != "" {
		t.Errorf("got reason %q, want empty for no hooks", reason)
	}

	err = runner.RunPostToolUse(context.Background(), "bash", `{"command":"ls"}`, "result", "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = runner.RunSessionStart(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = runner.RunStop(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNilRunnerSafe(t *testing.T) {
	var runner *HookRunner

	decision, reason, err := runner.RunPreToolUse(context.Background(), "bash", `{}`, "s")
	if err != nil || decision != "" || reason != "" {
		t.Errorf("nil runner RunPreToolUse should be no-op, got %q %q %v", decision, reason, err)
	}

	err = runner.RunPostToolUse(context.Background(), "bash", `{}`, "", "s")
	if err != nil {
		t.Errorf("nil runner RunPostToolUse should be no-op: %v", err)
	}

	err = runner.RunSessionStart(context.Background(), "s")
	if err != nil {
		t.Errorf("nil runner RunSessionStart should be no-op: %v", err)
	}

	err = runner.RunStop(context.Background(), "s")
	if err != nil {
		t.Errorf("nil runner RunStop should be no-op: %v", err)
	}
}

func TestStdinJSONFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	// Script that echoes stdin to stdout so we can inspect it
	script := createTempScript(t, `#!/bin/sh
cat
exit 0
`)

	runner := newTestRunner(t, HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "", Command: script},
		},
	})

	entry := runner.config.PreToolUse[0]
	input := HookInput{
		Event:        PreToolUse,
		ToolName:     "bash",
		ToolInput:    toRawJSON(`{"command":"rm -rf /tmp/test"}`),
		ToolResponse: nullJSON(),
		SessionID:    "2026-02-28-a3f9c1",
	}

	_, stdout, _, err := runner.runHook(context.Background(), entry, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse the JSON that was sent to stdin
	var received map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &received); err != nil {
		t.Fatalf("stdin was not valid JSON: %v\nGot: %s", err, stdout)
	}

	// Verify required fields
	var event string
	json.Unmarshal(received["event"], &event)
	if event != "PreToolUse" {
		t.Errorf("event = %q, want %q", event, "PreToolUse")
	}

	var toolName string
	json.Unmarshal(received["tool_name"], &toolName)
	if toolName != "bash" {
		t.Errorf("tool_name = %q, want %q", toolName, "bash")
	}

	var sessionID string
	json.Unmarshal(received["session_id"], &sessionID)
	if sessionID != "2026-02-28-a3f9c1" {
		t.Errorf("session_id = %q, want %q", sessionID, "2026-02-28-a3f9c1")
	}

	// tool_input should be an object with "command" key
	var toolInput map[string]string
	if err := json.Unmarshal(received["tool_input"], &toolInput); err != nil {
		t.Fatalf("tool_input not a valid object: %v", err)
	}
	if toolInput["command"] != "rm -rf /tmp/test" {
		t.Errorf("tool_input.command = %q, want %q", toolInput["command"], "rm -rf /tmp/test")
	}

	// tool_response should be null
	if string(received["tool_response"]) != "null" {
		t.Errorf("tool_response = %s, want null", received["tool_response"])
	}
}

func TestPostToolUseIncludesResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	script := createTempScript(t, `#!/bin/sh
cat
exit 0
`)

	runner := newTestRunner(t, HookConfig{
		PostToolUse: []HookEntry{
			{Matcher: "", Command: script},
		},
	})

	entry := runner.config.PostToolUse[0]
	input := HookInput{
		Event:        PostToolUse,
		ToolName:     "bash",
		ToolInput:    toRawJSON(`{"command":"ls"}`),
		ToolResponse: toRawJSON(`"file1.txt\nfile2.txt"`),
		SessionID:    "test-session",
	}

	_, stdout, _, err := runner.runHook(context.Background(), entry, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var received map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &received); err != nil {
		t.Fatalf("stdin was not valid JSON: %v", err)
	}

	if string(received["tool_response"]) == "null" {
		t.Error("tool_response should not be null for PostToolUse")
	}
}

func TestSessionStartHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	script := createTempScript(t, `#!/bin/sh
cat > /dev/null
exit 0
`)

	runner := newTestRunner(t, HookConfig{
		SessionStart: []HookEntry{
			{Command: script},
		},
	})

	err := runner.RunSessionStart(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStopHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	script := createTempScript(t, `#!/bin/sh
cat > /dev/null
exit 0
`)

	runner := newTestRunner(t, HookConfig{
		Stop: []HookEntry{
			{Command: script},
		},
	})

	err := runner.RunStop(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToRawJSONValidJSON(t *testing.T) {
	raw := toRawJSON(`{"key":"value"}`)
	if string(raw) != `{"key":"value"}` {
		t.Errorf("got %s, want %s", raw, `{"key":"value"}`)
	}
}

func TestToRawJSONInvalidJSON(t *testing.T) {
	raw := toRawJSON("plain text")
	// Should be marshaled as a JSON string
	if string(raw) != `"plain text"` {
		t.Errorf("got %s, want %s", raw, `"plain text"`)
	}
}

func TestToRawJSONEmpty(t *testing.T) {
	raw := toRawJSON("")
	if string(raw) != "null" {
		t.Errorf("got %s, want null", raw)
	}
}

func TestLimitedWriter(t *testing.T) {
	input := strings.Repeat("A", 100)
	buf := new(bytes.Buffer)
	lw := &limitedWriter{
		buf:   buf,
		limit: 50,
	}

	n, err := lw.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 100 {
		t.Errorf("Write returned %d, want %d (should report all bytes)", n, 100)
	}
	if buf.Len() != 50 {
		t.Errorf("buffer has %d bytes, want %d", buf.Len(), 50)
	}

	// Second write should be fully discarded
	n, err = lw.Write([]byte("more"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Errorf("Write returned %d, want %d (should report all bytes)", n, 4)
	}
	if buf.Len() != 50 {
		t.Errorf("buffer has %d bytes after second write, want %d", buf.Len(), 50)
	}
}

func TestMultipleHooksFirstDeniesSecondSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	denyScript := createTempScript(t, `#!/bin/sh
cat > /dev/null
echo "blocked" >&2
exit 2
`)

	// This script should never run because the first one denies
	markerFile := filepath.Join(t.TempDir(), "should-not-exist")
	allowScript := createTempScript(t, `#!/bin/sh
cat > /dev/null
touch `+markerFile+`
exit 0
`)

	runner := newTestRunner(t, HookConfig{
		PreToolUse: []HookEntry{
			{Matcher: "", Command: denyScript},
			{Matcher: "", Command: allowScript},
		},
	})

	decision, reason, err := runner.RunPreToolUse(context.Background(), "bash", `{}`, "s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision != "deny" {
		t.Errorf("got decision %q, want %q", decision, "deny")
	}
	if reason != "blocked" {
		t.Errorf("got reason %q, want %q", reason, "blocked")
	}

	// Verify second hook did not run
	if _, err := os.Stat(markerFile); err == nil {
		t.Error("second hook should not have run after first denied")
	}
}

// createTempScript creates a temporary executable shell script and returns its path.
func createTempScript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("failed to create temp script: %v", err)
	}
	return path
}
