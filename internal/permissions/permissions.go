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
	AllowedDirs       []string `yaml:"allowed_dirs"        json:"allowed_dirs"`
	AllowedCommands   []string `yaml:"allowed_commands"    json:"allowed_commands"`
	DeniedCommands    []string `yaml:"denied_commands"     json:"denied_commands"`
	SensitivePatterns []string `yaml:"sensitive_patterns"  json:"sensitive_patterns"`
	NetworkAllowlist  []string `yaml:"network_allowlist"   json:"network_allowlist"`
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

// defaultSafeCommands are commands allowed by default without user config.
// These are read-only, informational commands with no side effects.
// Resolution order: hard-block → denied → allowed → defaultSafe → ask.
// Users can override with denied_commands if needed.
var defaultSafeCommands = []defaultSafeEntry{
	// --- System info & file inspection ---
	{prefix: "ls"},
	{exact: "pwd"},
	{prefix: "which"},
	{prefix: "whereis"},
	{prefix: "type"},
	{prefix: "echo"},
	{prefix: "cat"},
	{prefix: "head"},
	{prefix: "tail"},
	{prefix: "wc"},
	{prefix: "file"},
	{prefix: "stat"},
	{prefix: "du"},
	{prefix: "df"},
	{exact: "id"},
	{exact: "whoami"},
	{exact: "hostname"},
	{prefix: "uname"},
	{exact: "uptime"},
	{prefix: "date"},
	{prefix: "cal"},
	{exact: "env"}, // exact only — "env CMD" runs a command
	{exact: "printenv"},
	{prefix: "basename"},
	{prefix: "dirname"},
	{prefix: "realpath"},
	{prefix: "readlink"},
	{exact: "true"},
	{exact: "false"},
	{prefix: "seq"},
	{exact: "nproc"},
	{exact: "arch"},
	{exact: "tty"},

	// --- Checksums ---
	{prefix: "md5"},
	{prefix: "md5sum"},
	{prefix: "shasum"},
	{prefix: "sha256sum"},
	{prefix: "cksum"},

	// --- Text processing (stdout only) ---
	{prefix: "grep"},
	{prefix: "egrep"},
	{prefix: "fgrep"},
	{prefix: "rg"},
	{prefix: "ag"},
	{prefix: "ack"},
	{prefix: "sort"},
	{prefix: "uniq"},
	{prefix: "tr"},
	{prefix: "cut"},
	{prefix: "paste"},
	{prefix: "fold"},
	{prefix: "fmt"},
	{prefix: "nl"},
	{prefix: "rev"},
	{prefix: "expand"},
	{prefix: "unexpand"},
	{prefix: "column"},
	{prefix: "comm"},
	{prefix: "diff"},
	{prefix: "colordiff"},
	{prefix: "cmp"},
	{prefix: "strings"},
	{prefix: "od"},
	{prefix: "hexdump"},
	{prefix: "base64"},
	{prefix: "jq"},
	{prefix: "yq"},

	// --- File finding (read-only, find excluded — supports -delete/-exec) ---
	{prefix: "fd"},
	{prefix: "locate"},
	{prefix: "mdfind"},
	{prefix: "tree"},

	// --- Process info ---
	{prefix: "ps"},
	{prefix: "top -l"}, // macOS one-shot mode only
	{prefix: "pgrep"},
	{prefix: "lsof"},
	{exact: "w"},
	{prefix: "who"},
	{prefix: "last"},
	{exact: "groups"},

	// --- Network diagnostics (read-only) ---
	{prefix: "ping"},
	{prefix: "traceroute"},
	{prefix: "tracepath"},
	{prefix: "dig"},
	{prefix: "nslookup"},
	{prefix: "host"},
	{prefix: "ifconfig"},
	{prefix: "ip addr"},
	{prefix: "ip route"},
	{prefix: "ip link"},
	{prefix: "netstat"},
	{prefix: "ss"},
	{prefix: "route"},
	{prefix: "arp"},

	// --- Man / help ---
	{prefix: "man"},
	{prefix: "info"},
	{prefix: "help"},

	// --- Git (read-only subcommands) ---
	{prefix: "git status"},
	{prefix: "git diff"},
	{prefix: "git log"},
	{prefix: "git show"},
	{prefix: "git branch"},
	{prefix: "git tag"},
	{prefix: "git remote"},
	{prefix: "git stash list"},
	{prefix: "git ls-files"},
	{prefix: "git ls-remote"},
	{prefix: "git ls-tree"},
	{prefix: "git rev-parse"},
	{prefix: "git rev-list"},
	{prefix: "git cat-file"},
	{prefix: "git name-rev"},
	{prefix: "git describe"},
	{prefix: "git shortlog"},
	{prefix: "git reflog"},
	{prefix: "git config --get"},
	{prefix: "git config --list"},
	{prefix: "git config -l"},
	{prefix: "git blame"},
	{prefix: "git count-objects"},
	{prefix: "git verify-commit"},
	{prefix: "git verify-tag"},

	// --- Version / info commands ---
	{prefix: "go version"},
	{prefix: "go env"},
	{prefix: "go doc"},
	{prefix: "go list"},
	{prefix: "node --version"},
	{prefix: "python --version"},
	{prefix: "python3 --version"},
	{prefix: "rustc --version"},
	{prefix: "rustup show"},
	{prefix: "rustup which"},
	{prefix: "swift --version"},
	{prefix: "java --version"},
	{prefix: "javac --version"},
	{prefix: "cmake --version"},

	// --- Build & test (dev-trusted, execute project code) ---
	{prefix: "go build"},
	{prefix: "go test"},
	{prefix: "go vet"},
	{prefix: "go mod download"},
	{prefix: "go mod verify"},
	{prefix: "go mod graph"},
	{prefix: "go mod why"},
	{prefix: "make"},
	{prefix: "cargo build"},
	{prefix: "cargo test"},
	{prefix: "cargo check"},
	{prefix: "cargo clippy"},
	{prefix: "cargo bench"},
	{prefix: "cargo doc"},
	{prefix: "npm test"},
	{prefix: "npm run"},
	{prefix: "yarn test"},
	{prefix: "pnpm test"},
	{prefix: "bun test"},
	{prefix: "bun run"},
	{prefix: "deno test"},
	{prefix: "deno lint"},
	{prefix: "deno check"},
	{prefix: "deno info"},
	{prefix: "python -m pytest"},
	{prefix: "python -m py_compile"},
	{prefix: "python -m json.tool"},
	{prefix: "python3 -m pytest"},
	{prefix: "python3 -m py_compile"},
	{prefix: "python3 -m json.tool"},
	{prefix: "pytest"},
	{prefix: "swift build"},
	{prefix: "swift test"},
	{prefix: "mvn test"},
	{prefix: "mvn compile"},
	{prefix: "mvn verify"},
	{prefix: "mvn dependency:tree"},
	{prefix: "gradle test"},
	{prefix: "gradle build"},
	{prefix: "gradle dependencies"},

	// --- Linters (read-only analysis) ---
	{prefix: "eslint"},
	{prefix: "shellcheck"},
	{prefix: "hadolint"},
	{prefix: "yamllint"},
	{prefix: "markdownlint"},
	{prefix: "mypy"},
	{prefix: "pyright"},
	{prefix: "pylint"},
	{prefix: "flake8"},
	{prefix: "bandit"},
	{prefix: "golangci-lint"},
	{prefix: "staticcheck"},
	{prefix: "ruff check"},
	{prefix: "buf lint"},

	// --- Package manager queries (read-only) ---
	{prefix: "brew list"},
	{prefix: "brew info"},
	{prefix: "brew search"},
	{prefix: "brew outdated"},
	{prefix: "brew deps"},
	{prefix: "brew leaves"},
	{prefix: "brew config"},
	{prefix: "brew doctor"},
	{prefix: "brew --version"},
	{prefix: "pip list"},
	{prefix: "pip show"},
	{prefix: "pip freeze"},
	{prefix: "pip check"},
	{prefix: "pip3 list"},
	{prefix: "pip3 show"},
	{prefix: "pip3 freeze"},
	{prefix: "pip3 check"},
	{prefix: "npm list"},
	{prefix: "npm ls"},
	{prefix: "npm outdated"},
	{prefix: "npm view"},
	{prefix: "npm info"},
	{prefix: "npm search"},
	{prefix: "npm audit"},
	{prefix: "npm explain"},
	{prefix: "npm why"},
	{prefix: "yarn list"},
	{prefix: "yarn info"},
	{prefix: "yarn why"},
	{prefix: "cargo tree"},
	{prefix: "cargo metadata"},
	{prefix: "cargo version"},
	{prefix: "cargo search"},
	{prefix: "cargo verify-project"},
	{prefix: "cargo read-manifest"},

	// --- Docker (read-only) ---
	{prefix: "docker ps"},
	{prefix: "docker images"},
	{prefix: "docker image ls"},
	{prefix: "docker inspect"},
	{prefix: "docker logs"},
	{prefix: "docker stats"},
	{prefix: "docker top"},
	{prefix: "docker version"},
	{prefix: "docker info"},
	{prefix: "docker network ls"},
	{prefix: "docker volume ls"},
	{prefix: "docker compose ps"},
	{prefix: "docker compose logs"},
	{prefix: "docker compose config"},

	// --- Kubernetes (read-only) ---
	{prefix: "kubectl get"},
	{prefix: "kubectl describe"},
	{prefix: "kubectl logs"},
	{prefix: "kubectl top"},
	{prefix: "kubectl version"},
	{prefix: "kubectl config view"},
	{prefix: "kubectl config current-context"},
	{prefix: "kubectl config get-contexts"},
	{prefix: "kubectl api-resources"},
	{prefix: "kubectl api-versions"},
	{prefix: "kubectl explain"},
	{prefix: "kubectl cluster-info"},

	// --- Terraform (read-only) ---
	{prefix: "terraform version"},
	{prefix: "terraform validate"},
	{prefix: "terraform state list"},
	{prefix: "terraform state show"},
	{prefix: "terraform output"},
	{prefix: "terraform providers"},
	{prefix: "terraform graph"},
	{prefix: "terraform show"},

	// --- GitHub CLI (read-only) ---
	{prefix: "gh pr list"},
	{prefix: "gh pr view"},
	{prefix: "gh pr status"},
	{prefix: "gh pr diff"},
	{prefix: "gh pr checks"},
	{prefix: "gh issue list"},
	{prefix: "gh issue view"},
	{prefix: "gh issue status"},
	{prefix: "gh repo view"},
	{prefix: "gh release list"},
	{prefix: "gh release view"},
	{prefix: "gh run list"},
	{prefix: "gh run view"},
	{prefix: "gh auth status"},
	{prefix: "gh status"},

	// --- AWS (read-only) ---
	{prefix: "aws sts get-caller-identity"},
	{prefix: "aws s3 ls"},
	{prefix: "aws --version"},

	// --- GCP (read-only) ---
	{prefix: "gcloud version"},
	{prefix: "gcloud info"},
	{prefix: "gcloud config list"},
	{prefix: "gcloud auth list"},
	{prefix: "gcloud projects list"},

	// --- macOS system info ---
	{prefix: "sw_vers"},
	{prefix: "system_profiler"},
	{prefix: "ioreg"},
	{prefix: "diskutil list"},
	{prefix: "defaults read"},
	{prefix: "sysctl"},
	{prefix: "launchctl list"},
}

// defaultSafeEntry defines a safe command match rule.
type defaultSafeEntry struct {
	exact  string // exact match (no arguments allowed)
	prefix string // prefix match (command + any arguments)
}

// isDefaultSafe checks if a command matches the built-in safe defaults.
func isDefaultSafe(cmd string) bool {
	for _, entry := range defaultSafeCommands {
		if entry.exact != "" {
			if cmd == entry.exact {
				return true
			}
		} else if entry.prefix != "" {
			if cmd == entry.prefix || strings.HasPrefix(cmd, entry.prefix+" ") {
				return true
			}
		}
	}
	return false
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

	// AllowedCommands patterns (user config)
	for _, pattern := range config.AllowedCommands {
		if MatchesPattern(trimmed, pattern) {
			return "allow", "matches allowed command pattern: " + pattern
		}
	}

	// Built-in safe defaults (lowest-priority allow layer)
	if isDefaultSafe(trimmed) {
		return "allow", "built-in safe command"
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
		cmd := ExtractField(argsJSON, "command")
		return CheckCommand(cmd, config)
	case "file_read":
		path := ExtractField(argsJSON, "path")
		return CheckFilePath(path, "read", config)
	case "file_write", "file_edit":
		path := ExtractField(argsJSON, "path")
		return CheckFilePath(path, "write", config)
	case "glob", "grep":
		path := ExtractField(argsJSON, "path")
		if path == "" {
			path = ExtractField(argsJSON, "pattern")
		}
		return CheckFilePath(path, "read", config)
	case "directory_list":
		path := ExtractField(argsJSON, "path")
		if path == "" {
			path = "."
		}
		return CheckFilePath(path, "read", config)
	case "http":
		url := ExtractField(argsJSON, "url")
		return CheckNetworkEgress(url, config)
	}
	return "", ""
}

// IsHardBlocked checks if a command matches any hard-block pattern.
func IsHardBlocked(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	for _, pattern := range hardBlockPatterns {
		if MatchesPattern(trimmed, pattern) {
			return true
		}
	}
	return false
}

// ExtractField extracts a string field from a JSON args string.
func ExtractField(argsJSON string, field string) string {
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
