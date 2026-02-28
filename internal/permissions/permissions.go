package permissions

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// PermissionsConfig defines user-configurable permission rules.
type PermissionsConfig struct {
	AllowedDirs       []string `yaml:"allowed_dirs"`
	AllowedCommands   []string `yaml:"allowed_commands"`
	DeniedCommands    []string `yaml:"denied_commands"`
	SensitivePatterns []string `yaml:"sensitive_patterns"`
	NetworkAllowlist  []string `yaml:"network_allowlist"`
}

// hardBlockPatterns are always denied and cannot be overridden by config.
var hardBlockPatterns = []string{
	"rm -rf /",
	"rm -rf ~",
	"rm -rf /System",
	"rm -rf /Users",
	"rm -rf /*",
	"> /dev/sd*",
	"> /dev/disk*",
	"mkfs.*",
	"dd if=* of=/dev/*",
	"curl * | sh",
	"curl * | bash",
	"wget * | sh",
	"wget * | bash",
}

// defaultSensitivePatterns are built-in file patterns considered sensitive.
var defaultSensitivePatterns = []string{
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"id_rsa*",
	"id_ed25519*",
	".ssh/config",
	"*.keychain*",
	"tokens.json",
	"credentials.json",
	"*.secrets",
}

// shellSplitOperators are used to split compound commands.
var shellSplitOperators = []string{"&&", "||", ";", "|"}

// CheckCommand evaluates a bash command against the permission rules.
// Returns decision ("allow", "deny", "ask") and a reason string.
func CheckCommand(cmd string, config *PermissionsConfig) (string, string) {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return "deny", "empty command"
	}

	// 1. Hard-block patterns always deny
	for _, pattern := range hardBlockPatterns {
		if MatchesPattern(trimmed, pattern) {
			return "deny", "matches hard-block pattern: " + pattern
		}
	}

	if config == nil {
		return "ask", "no permission config; requires approval"
	}

	// 2. DeniedCommands patterns
	for _, pattern := range config.DeniedCommands {
		if MatchesPattern(trimmed, pattern) {
			return "deny", "matches denied command pattern: " + pattern
		}
	}

	// 3. Split compound commands and check each sub-command
	subCmds := splitCompoundCommand(trimmed)
	if len(subCmds) > 1 {
		for _, sub := range subCmds {
			decision, reason := checkSingleCommand(sub, config)
			if decision == "deny" {
				return "deny", "sub-command denied: " + reason
			}
		}
		// All sub-commands must be explicitly allowed for the compound to be allowed
		allAllowed := true
		for _, sub := range subCmds {
			decision, _ := checkSingleCommand(sub, config)
			if decision != "allow" {
				allAllowed = false
				break
			}
		}
		if allAllowed {
			return "allow", "all sub-commands allowed"
		}
		return "ask", "compound command requires approval"
	}

	return checkSingleCommand(trimmed, config)
}

// checkSingleCommand checks a single (non-compound) command against config.
func checkSingleCommand(cmd string, config *PermissionsConfig) (string, string) {
	trimmed := strings.TrimSpace(cmd)

	// Re-check hard-block for sub-commands
	for _, pattern := range hardBlockPatterns {
		if MatchesPattern(trimmed, pattern) {
			return "deny", "matches hard-block pattern: " + pattern
		}
	}

	// Re-check denied commands for sub-commands
	for _, pattern := range config.DeniedCommands {
		if MatchesPattern(trimmed, pattern) {
			return "deny", "matches denied command pattern: " + pattern
		}
	}

	// AllowedCommands patterns
	for _, pattern := range config.AllowedCommands {
		if MatchesPattern(trimmed, pattern) {
			return "allow", "matches allowed command pattern: " + pattern
		}
	}

	return "ask", "command not in allowed list; requires approval"
}

// splitCompoundCommand splits a command string on shell operators (&&, ||, ;, |).
func splitCompoundCommand(cmd string) []string {
	// Replace operators with a unique separator, then split.
	// Process longer operators first to avoid partial matches.
	result := cmd
	const sep = "\x00SPLIT\x00"
	for _, op := range shellSplitOperators {
		result = strings.ReplaceAll(result, op, sep)
	}
	parts := strings.Split(result, sep)
	var trimmed []string
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			trimmed = append(trimmed, s)
		}
	}
	return trimmed
}

// CheckFilePath evaluates a file path for read/write access.
// Uses filepath.EvalSymlinks() to resolve symlinks before checking.
// Returns decision ("allow", "deny", "ask") and a reason string.
func CheckFilePath(path string, action string, config *PermissionsConfig) (string, string) {
	if path == "" {
		return "deny", "empty path"
	}

	// Expand ~ prefix
	expanded := expandHome(path)

	// Resolve symlinks to get the real path
	realPath, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		// If the file doesn't exist yet, use the cleaned expanded path
		realPath = filepath.Clean(expanded)
	}

	// Check sensitive file patterns
	if IsSensitiveFile(filepath.Base(realPath)) {
		if action == "read" {
			return "ask", "sensitive file requires approval for read: " + filepath.Base(realPath)
		}
		return "ask", "sensitive file requires approval: " + filepath.Base(realPath)
	}

	if config == nil {
		return "ask", "no permission config; requires approval"
	}

	// Check if path is within allowed_dirs
	inAllowed := false
	for _, dir := range config.AllowedDirs {
		expandedDir := expandHome(dir)
		absDir, err := filepath.Abs(expandedDir)
		if err != nil {
			continue
		}
		absPath, err := filepath.Abs(realPath)
		if err != nil {
			continue
		}
		if isSubPath(absPath, absDir) {
			inAllowed = true
			break
		}
	}

	if inAllowed && action == "read" {
		return "allow", "path within allowed directory"
	}

	if action == "write" {
		return "ask", "write operations always require approval"
	}

	if inAllowed {
		return "allow", "path within allowed directory"
	}

	return "ask", "path not in allowed directories; requires approval"
}

// CheckNetworkEgress evaluates an HTTP request URL against the network allowlist.
// localhost/127.0.0.1 are always allowed.
// Returns decision ("allow", "deny", "ask") and a reason string.
func CheckNetworkEgress(rawURL string, config *PermissionsConfig) (string, string) {
	if rawURL == "" {
		return "deny", "empty URL"
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "deny", "malformed URL: " + err.Error()
	}

	host := parsed.Hostname()

	// localhost and 127.0.0.1 are always allowed
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return "allow", "localhost always allowed"
	}

	if config == nil {
		return "ask", "no permission config; requires approval"
	}

	// Check network allowlist
	for _, allowed := range config.NetworkAllowlist {
		if host == allowed {
			return "allow", "host in network allowlist"
		}
		// Support wildcard subdomain matching: *.example.com
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:] // ".example.com"
			if strings.HasSuffix(host, suffix) || host == allowed[2:] {
				return "allow", "host matches network allowlist pattern: " + allowed
			}
		}
	}

	return "ask", "host not in network allowlist; requires approval"
}

// CheckToolCall evaluates a tool call against permission rules based on tool name.
// Returns decision ("allow", "deny", "ask", "") and a reason string.
// An empty decision means the tool is not handled by the permissions engine.
func CheckToolCall(toolName, argsJSON string, config *PermissionsConfig) (string, string) {
	switch toolName {
	case "bash":
		cmd := extractField(argsJSON, "command")
		return CheckCommand(cmd, config)
	case "file_read":
		path := extractField(argsJSON, "path")
		return CheckFilePath(path, "read", config)
	case "file_write", "file_edit":
		path := extractField(argsJSON, "path")
		return CheckFilePath(path, "write", config)
	case "glob", "grep":
		path := extractField(argsJSON, "path")
		if path == "" {
			path = extractField(argsJSON, "pattern")
		}
		return CheckFilePath(path, "read", config)
	case "directory_list":
		path := extractField(argsJSON, "path")
		if path == "" {
			path = "."
		}
		return CheckFilePath(path, "read", config)
	case "http":
		url := extractField(argsJSON, "url")
		return CheckNetworkEgress(url, config)
	}
	return "", ""
}

// extractField extracts a string field from a JSON args string.
func extractField(argsJSON string, field string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return ""
	}
	if v, ok := m[field]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// IsSensitiveFile checks if a filename matches known sensitive file patterns.
func IsSensitiveFile(filename string) bool {
	if filename == "" {
		return false
	}
	for _, pattern := range defaultSensitivePatterns {
		if MatchesPattern(filename, pattern) {
			return true
		}
	}
	return false
}

// IsSensitiveFileWithConfig checks against both default and user-configured sensitive patterns.
func IsSensitiveFileWithConfig(filename string, config *PermissionsConfig) bool {
	if IsSensitiveFile(filename) {
		return true
	}
	if config == nil {
		return false
	}
	for _, pattern := range config.SensitivePatterns {
		if MatchesPattern(filename, pattern) {
			return true
		}
	}
	return false
}

// MatchesPattern checks if a string matches a glob-like pattern.
// Supports * as a wildcard matching any sequence of characters.
func MatchesPattern(s string, pattern string) bool {
	return matchGlob(s, pattern)
}

// matchGlob implements simple glob matching with * wildcards.
func matchGlob(s, pattern string) bool {
	// Use a two-pointer approach for * wildcard matching
	si, pi := 0, 0
	starIdx, matchIdx := -1, 0

	for si < len(s) {
		if pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]) {
			si++
			pi++
		} else if pi < len(pattern) && pattern[pi] == '*' {
			starIdx = pi
			matchIdx = si
			pi++
		} else if starIdx != -1 {
			pi = starIdx + 1
			matchIdx++
			si = matchIdx
		} else {
			return false
		}
	}

	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}

	return pi == len(pattern)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// isSubPath checks if path is within or equal to dir.
func isSubPath(path, dir string) bool {
	// Normalize paths
	path = filepath.Clean(path)
	dir = filepath.Clean(dir)

	if path == dir {
		return true
	}

	// Ensure dir ends with separator for prefix matching
	dirWithSep := dir + string(filepath.Separator)
	return strings.HasPrefix(path, dirWithSep)
}
