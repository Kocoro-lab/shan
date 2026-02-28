package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// AuditEntry represents a single audited tool call event.
type AuditEntry struct {
	Timestamp     time.Time `json:"timestamp"`
	SessionID     string    `json:"session_id"`
	ToolName      string    `json:"tool_name"`
	InputSummary  string    `json:"input_summary"`
	OutputSummary string    `json:"output_summary"`
	Decision      string    `json:"decision"`
	Approved      bool      `json:"approved"`
	DurationMs    int64     `json:"duration_ms"`
}

// AuditLogger writes audit entries as JSON lines to a log file.
type AuditLogger struct {
	mu   sync.Mutex
	file *os.File
}

const maxSummaryLen = 500

// NewAuditLogger creates a logger that writes to the given logDir/audit.log.
// Creates the directory if it does not exist.
func NewAuditLogger(logDir string) (*AuditLogger, error) {
	if logDir == "" {
		return nil, fmt.Errorf("logDir must not be empty")
	}

	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create log directory %s: %w", logDir, err)
	}

	logPath := filepath.Join(logDir, "audit.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log %s: %w", logPath, err)
	}

	return &AuditLogger{file: f}, nil
}

// Log records a tool call event. Input and output summaries are truncated
// and have secrets redacted before writing.
func (a *AuditLogger) Log(entry AuditEntry) {
	entry.InputSummary = RedactSecrets(truncate(entry.InputSummary, maxSummaryLen))
	entry.OutputSummary = RedactSecrets(truncate(entry.OutputSummary, maxSummaryLen))

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.file.Write(data)
	a.file.Write([]byte("\n"))
}

// Close closes the underlying log file.
func (a *AuditLogger) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.file.Close()
}

// redaction patterns compiled once at package init
var redactPatterns []*regexp.Regexp

func init() {
	patterns := []string{
		// AWS access key IDs
		`AKIA[0-9A-Z]{16}`,
		// JWT tokens
		`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`,
		// sk- style API keys (OpenAI, Stripe, etc.)
		`sk-[a-zA-Z0-9]{20,}`,
		// key- style API keys
		`key-[a-zA-Z0-9]{20,}`,
		// Bearer tokens
		`Bearer [A-Za-z0-9_-]+`,
		// PEM content markers
		`-----BEGIN[A-Z \-]*-----`,
		// Env var assignments with secret-like names
		`(?i)[A-Z_]*(?:KEY|SECRET|TOKEN|PASSWORD)\s*=\s*\S+`,
	}

	for _, p := range patterns {
		redactPatterns = append(redactPatterns, regexp.MustCompile(p))
	}
}

// RedactSecrets replaces known secret patterns with [REDACTED].
func RedactSecrets(text string) string {
	result := text
	for _, re := range redactPatterns {
		result = re.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

// truncate shortens text to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
