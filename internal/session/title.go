package session

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// AgentTitle returns a fixed title for a named agent's long-lived session.
func AgentTitle(agentName string) string {
	return fmt.Sprintf("%s conversation", agentName)
}

// Title creates a short, readable title from user input.
// Truncates to 50 chars at a word boundary, strips leading/trailing whitespace
// and newlines, and ensures single-line output.
func Title(input string) string {
	// Take first line only
	if idx := strings.IndexAny(input, "\n\r"); idx >= 0 {
		input = input[:idx]
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return "New session"
	}
	const maxRunes = 50
	if utf8.RuneCountInString(input) <= maxRunes {
		return input
	}
	// Truncate at rune boundary
	runes := []rune(input)
	truncated := string(runes[:maxRunes])
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > len(truncated)/2 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "..."
}
