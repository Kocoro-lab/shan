package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// isPathUnderCWD returns true if the given path resolves to a location
// under the current working directory. Used by read-only tools to
// auto-approve safe paths.
func isPathUnderCWD(path string) bool {
	if path == "" || path == "." {
		return true
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false
	}

	// Resolve the path to absolute
	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(cwd, path)
	}
	absPath = filepath.Clean(absPath)

	cwdClean := filepath.Clean(cwd)
	if absPath == cwdClean {
		return true
	}
	return strings.HasPrefix(absPath, cwdClean+string(filepath.Separator))
}
