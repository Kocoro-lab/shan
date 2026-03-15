package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Pixel color IDs — derived from Ptfrog sprite palette.
const (
	pxNone  = uint8(0) // transparent (terminal background)
	pxBody  = uint8(1) // #489458 body green
	pxBelly = uint8(2) // #7ec88e belly light green
	pxWhite = uint8(3) // #e6eee6 eye white
	pxPupil = uint8(4) // #181818 pupil
	pxMouth = uint8(5) // #32703e mouth
	pxDark  = uint8(6) // #2e6c3a feet/legs
	pxNose  = uint8(7) // #387e46 nostril
)

// frogRGB maps pixel color IDs to [R,G,B].
var frogRGB = [8][3]uint8{
	{0, 0, 0},       // 0: none (unused)
	{72, 148, 88},   // 1: body
	{126, 200, 142}, // 2: belly
	{230, 238, 230}, // 3: eye white
	{24, 24, 24},    // 4: pupil
	{50, 112, 62},   // 5: mouth
	{46, 108, 58},   // 6: dark
	{56, 126, 70},   // 7: nostril
}

// frogGrid is an 8-row × 10-col pixel grid → 4 terminal lines via half-blocks.
// 10 cols × 8 rows preserves the original 15×12 aspect ratio (1.25:1) at smaller size,
// since each half-block pixel renders as ~1:1 square in a typical 2:1 char cell.
type frogGrid [8][10]uint8

// Pixel art frames mapped from the Ptfrog sprite palette.
// Grid is 10 cols × 8 rows (same visual footprint as before: 1 transparent col
// on each side, 8-col body). Face row uses nostrils (7) + belly start derived
// from actual sprite data, replacing the old mouth band.
var (
	// frogBase: standard resting/landing pose.
	frogBase = frogGrid{
		{0, 1, 1, 0, 0, 0, 0, 1, 1, 0}, // row 0: eye domes
		{0, 3, 4, 0, 0, 0, 0, 4, 3, 0}, // row 1: eyes — white, pupil (mirrored)
		{0, 1, 1, 1, 1, 1, 1, 1, 1, 0}, // row 2: head
		{0, 1, 7, 2, 2, 2, 2, 7, 1, 0}, // row 3: nostrils + belly start
		{0, 1, 1, 2, 2, 2, 2, 1, 1, 0}, // row 4: belly
		{0, 0, 1, 2, 2, 2, 2, 1, 0, 0}, // row 5: lower body
		{0, 0, 6, 0, 6, 6, 0, 6, 0, 0}, // row 6: legs
		{0, 6, 6, 0, 6, 6, 0, 6, 6, 0}, // row 7: feet
	}

	// frogBlinkHalf: eyes half-closed (whites obscured, pupils remain).
	frogBlinkHalf = frogGrid{
		{0, 1, 1, 0, 0, 0, 0, 1, 1, 0},
		{0, 1, 4, 0, 0, 0, 0, 4, 1, 0}, // whites → body green
		{0, 1, 1, 1, 1, 1, 1, 1, 1, 0},
		{0, 1, 7, 2, 2, 2, 2, 7, 1, 0},
		{0, 1, 1, 2, 2, 2, 2, 1, 1, 0},
		{0, 0, 1, 2, 2, 2, 2, 1, 0, 0},
		{0, 0, 6, 0, 6, 6, 0, 6, 0, 0},
		{0, 6, 6, 0, 6, 6, 0, 6, 6, 0},
	}

	// frogBlinkClosed: eyes fully closed (all body green).
	frogBlinkClosed = frogGrid{
		{0, 1, 1, 0, 0, 0, 0, 1, 1, 0},
		{0, 1, 1, 0, 0, 0, 0, 1, 1, 0}, // all body
		{0, 1, 1, 1, 1, 1, 1, 1, 1, 0},
		{0, 1, 7, 2, 2, 2, 2, 7, 1, 0},
		{0, 1, 1, 2, 2, 2, 2, 1, 1, 0},
		{0, 0, 1, 2, 2, 2, 2, 1, 0, 0},
		{0, 0, 6, 0, 6, 6, 0, 6, 0, 0},
		{0, 6, 6, 0, 6, 6, 0, 6, 6, 0},
	}

	// frogCrouch: legs tucked inward (pre-jump pose).
	frogCrouch = frogGrid{
		{0, 1, 1, 0, 0, 0, 0, 1, 1, 0},
		{0, 3, 4, 0, 0, 0, 0, 4, 3, 0},
		{0, 1, 1, 1, 1, 1, 1, 1, 1, 0},
		{0, 1, 7, 2, 2, 2, 2, 7, 1, 0},
		{0, 1, 1, 2, 2, 2, 2, 1, 1, 0},
		{0, 0, 1, 2, 2, 2, 2, 1, 0, 0},
		{0, 0, 0, 6, 6, 6, 6, 0, 0, 0}, // legs tucked in
		{0, 0, 0, 6, 6, 6, 6, 0, 0, 0}, // feet tucked
	}

	// frogJump: legs fully splayed (airborne).
	frogJump = frogGrid{
		{0, 1, 1, 0, 0, 0, 0, 1, 1, 0},
		{0, 3, 4, 0, 0, 0, 0, 4, 3, 0},
		{0, 1, 1, 1, 1, 1, 1, 1, 1, 0},
		{0, 1, 7, 2, 2, 2, 2, 7, 1, 0},
		{0, 1, 1, 2, 2, 2, 2, 1, 1, 0},
		{0, 0, 1, 2, 2, 2, 2, 1, 0, 0},
		{6, 6, 0, 0, 6, 6, 0, 0, 6, 6}, // legs splayed wide
		{6, 0, 0, 0, 6, 6, 0, 0, 0, 6}, // feet at extremes
	}
)

// halfBlockCache is a pre-computed lookup table of ANSI-escaped half-block
// characters for all 8×8 top/bottom pixel color combinations.
var halfBlockCache [8][8]string

func init() {
	for top := uint8(0); top < 8; top++ {
		for bot := uint8(0); bot < 8; bot++ {
			halfBlockCache[top][bot] = buildHalfBlock(top, bot)
		}
	}
}

func buildHalfBlock(top, bot uint8) string {
	hex := func(c uint8) lipgloss.Color {
		r := frogRGB[c]
		return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r[0], r[1], r[2]))
	}
	switch {
	case top == pxNone && bot == pxNone:
		return " "
	case top == pxNone:
		return lipgloss.NewStyle().Background(hex(bot)).Render(" ")
	case bot == pxNone:
		return lipgloss.NewStyle().Foreground(hex(top)).Render("▀")
	case top == bot:
		return lipgloss.NewStyle().Foreground(hex(top)).Render("█")
	default:
		return lipgloss.NewStyle().Foreground(hex(top)).Background(hex(bot)).Render("▀")
	}
}

// renderFrogGrid renders a pixel grid as 4 terminal lines using the Unicode
// half-block technique: ▀ with fg = upper pixel, bg = lower pixel.
// Each line is 10 characters wide (10 terminal columns).
func renderFrogGrid(g frogGrid) []string {
	lines := make([]string, 4)
	for i := 0; i < 4; i++ {
		top, bot := i*2, i*2+1
		var sb strings.Builder
		for col := 0; col < 10; col++ {
			sb.WriteString(halfBlockCache[g[top][col]][g[bot][col]])
		}
		lines[i] = sb.String()
	}
	return lines
}

// frogAnimFrame returns the pixel grid for the given startup animation frame.
// Sequence (12 frames × 80ms ≈ 1s): crouch×2 → jump×2 → land×3 → blink×3 → idle×2.
func frogAnimFrame(frame int) frogGrid {
	switch {
	case frame < 2:
		return frogCrouch
	case frame < 4:
		return frogJump
	case frame < 7:
		return frogBase
	case frame == 7:
		return frogBlinkHalf
	case frame == 8:
		return frogBlinkClosed
	case frame == 9:
		return frogBlinkHalf
	default:
		return frogBase
	}
}
