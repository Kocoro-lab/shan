package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewAuditLogger(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")

	logger, err := NewAuditLogger(logDir)
	if err != nil {
		t.Fatalf("NewAuditLogger() error: %v", err)
	}
	defer logger.Close()

	// Directory should be created
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Error("log directory was not created")
	}

	// File should exist
	logPath := filepath.Join(logDir, "audit.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("audit.log was not created")
	}
}

func TestNewAuditLogger_EmptyDir(t *testing.T) {
	_, err := NewAuditLogger("")
	if err == nil {
		t.Error("expected error for empty logDir")
	}
}

func TestAuditLogger_Log(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger() error: %v", err)
	}

	entry := AuditEntry{
		Timestamp:     time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		SessionID:     "test-session-123",
		ToolName:      "bash",
		InputSummary:  "ls -la /tmp",
		OutputSummary: "total 0\ndrwxrwxrwt  2 root root",
		Decision:      "allow",
		Approved:      true,
		DurationMs:    42,
	}

	logger.Log(entry)
	logger.Close()

	// Read back
	logPath := filepath.Join(dir, "audit.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read audit.log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var decoded AuditEntry
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("failed to parse JSON line: %v", err)
	}

	if decoded.SessionID != "test-session-123" {
		t.Errorf("SessionID = %q, want %q", decoded.SessionID, "test-session-123")
	}
	if decoded.ToolName != "bash" {
		t.Errorf("ToolName = %q, want %q", decoded.ToolName, "bash")
	}
	if decoded.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", decoded.Decision, "allow")
	}
	if decoded.DurationMs != 42 {
		t.Errorf("DurationMs = %d, want %d", decoded.DurationMs, 42)
	}
}

func TestAuditLogger_MultipleEntries(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger() error: %v", err)
	}

	for i := range 5 {
		logger.Log(AuditEntry{
			Timestamp:  time.Now(),
			SessionID:  "session",
			ToolName:   "bash",
			Decision:   "allow",
			DurationMs: int64(i),
		})
	}
	logger.Close()

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
}

func TestAuditLogger_AppendMode(t *testing.T) {
	dir := t.TempDir()

	// Write first entry
	logger1, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger() error: %v", err)
	}
	logger1.Log(AuditEntry{SessionID: "first"})
	logger1.Close()

	// Open again and write second entry
	logger2, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger() error: %v", err)
	}
	logger2.Log(AuditEntry{SessionID: "second"})
	logger2.Close()

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines (append mode), got %d", len(lines))
	}
}

func TestAuditLogger_RedactsOnWrite(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger() error: %v", err)
	}

	logger.Log(AuditEntry{
		InputSummary:  "curl -H 'Authorization: Bearer mytoken123' https://api.example.com",
		OutputSummary: "API_KEY=supersecretkey123",
	})
	logger.Close()

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	content := string(data)
	if strings.Contains(content, "mytoken123") {
		t.Error("Bearer token was not redacted")
	}
	if strings.Contains(content, "supersecretkey123") {
		t.Error("API_KEY value was not redacted")
	}
	if !strings.Contains(content, "[REDACTED]") {
		t.Error("expected [REDACTED] placeholder in output")
	}
}

func TestAuditLogger_TruncatesLongSummaries(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewAuditLogger(dir)
	if err != nil {
		t.Fatalf("NewAuditLogger() error: %v", err)
	}

	longInput := strings.Repeat("x", 1000)
	logger.Log(AuditEntry{
		InputSummary:  longInput,
		OutputSummary: longInput,
	})
	logger.Close()

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	var decoded AuditEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &decoded); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(decoded.InputSummary) > maxSummaryLen {
		t.Errorf("InputSummary len = %d, want <= %d", len(decoded.InputSummary), maxSummaryLen)
	}
	if len(decoded.OutputSummary) > maxSummaryLen {
		t.Errorf("OutputSummary len = %d, want <= %d", len(decoded.OutputSummary), maxSummaryLen)
	}
	if !strings.HasSuffix(decoded.InputSummary, "...") {
		t.Error("truncated summary should end with ...")
	}
}

func TestRedactSecrets_AWSAccessKey(t *testing.T) {
	input := "using key AKIAIOSFODNN7EXAMPLE for auth"
	got := RedactSecrets(input)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("AWS access key was not redacted")
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Error("expected [REDACTED] placeholder")
	}
}

func TestRedactSecrets_JWT(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	input := "token: " + jwt
	got := RedactSecrets(input)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiI") {
		t.Error("JWT was not redacted")
	}
}

func TestRedactSecrets_SKKey(t *testing.T) {
	input := "key is sk-abcdefghijklmnopqrstuvwxyz"
	got := RedactSecrets(input)
	if strings.Contains(got, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Error("sk- key was not redacted")
	}
}

func TestRedactSecrets_KeyDashKey(t *testing.T) {
	input := "api key-abcdefghijklmnopqrstuvwxyz"
	got := RedactSecrets(input)
	if strings.Contains(got, "key-abcdefghijklmnopqrstuvwxyz") {
		t.Error("key- pattern was not redacted")
	}
}

func TestRedactSecrets_BearerToken(t *testing.T) {
	input := "Authorization: Bearer abc123xyz"
	got := RedactSecrets(input)
	if strings.Contains(got, "abc123xyz") {
		t.Error("Bearer token was not redacted")
	}
}

func TestRedactSecrets_PEMMarker(t *testing.T) {
	input := "-----BEGIN RSA PRIVATE KEY-----"
	got := RedactSecrets(input)
	if strings.Contains(got, "-----BEGIN") {
		t.Error("PEM marker was not redacted")
	}
}

func TestRedactSecrets_EnvVarAssignments(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"API_KEY=mysecret123", "mysecret123"},
		{"DB_PASSWORD=hunter2", "hunter2"},
		{"AUTH_TOKEN=tok_abc123", "tok_abc123"},
		{"AWS_SECRET=very_secret", "very_secret"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := RedactSecrets(tt.input)
			if strings.Contains(got, tt.contains) {
				t.Errorf("RedactSecrets(%q) still contains %q", tt.input, tt.contains)
			}
		})
	}
}

func TestRedactSecrets_NoFalsePositives(t *testing.T) {
	safe := []string{
		"running ls -la",
		"file: readme.md",
		"go build ./...",
		"git status",
	}

	for _, input := range safe {
		t.Run(input, func(t *testing.T) {
			got := RedactSecrets(input)
			if got != input {
				t.Errorf("RedactSecrets(%q) = %q, should be unchanged", input, got)
			}
		})
	}
}

func TestRedactSecrets_EmptyString(t *testing.T) {
	got := RedactSecrets("")
	if got != "" {
		t.Errorf("RedactSecrets(\"\") = %q, want \"\"", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 8, "hello..."},
		{"ab", 3, "ab"},
		{"abcdef", 6, "abcdef"},
		{"abcdefg", 6, "abc..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
