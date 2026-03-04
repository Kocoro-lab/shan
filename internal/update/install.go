package update

import (
	"os"
	"strings"
)

// IsHomebrewPath returns true if the binary path indicates a Homebrew installation.
// Resolves symlinks first (e.g. /usr/local/bin/shan → /usr/local/Cellar/shan/…).
func IsHomebrewPath(path string) bool {
	if resolved, err := os.Readlink(path); err == nil {
		path = resolved
	}
	return strings.Contains(path, "/Cellar/") ||
		strings.HasPrefix(path, "/opt/homebrew/")
}
