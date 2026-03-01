package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
)

var blankLineRe = regexp.MustCompile(`\n{3,}`)

var mdRenderer *glamour.TermRenderer

func init() {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0), // let the terminal handle wrapping
	)
	if err == nil {
		mdRenderer = r
	}
}

// renderMarkdown renders markdown text with ANSI styling.
// Falls back to plain text if the renderer is unavailable.
func renderMarkdown(text string) string {
	if mdRenderer == nil || text == "" {
		return text
	}
	out, err := mdRenderer.Render(text)
	if err != nil {
		return text
	}
	out = blankLineRe.ReplaceAllString(out, "\n\n")
	out = strings.TrimRight(out, "\n")
	return out
}
