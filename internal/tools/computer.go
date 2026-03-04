package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Kocoro-lab/shan/internal/agent"
	"github.com/Kocoro-lab/shan/internal/client"
)

type ComputerTool struct {
	screenW int
	screenH int
}

func (t *ComputerTool) ensureScreenDims() {
	if t.screenW > 0 {
		return
	}
	w, h, err := GetScreenDimensions()
	if err != nil {
		t.screenW = DefaultAPIWidth
		t.screenH = DefaultAPIHeight
		return
	}
	t.screenW = w
	t.screenH = h
}

func (t *ComputerTool) scaleXY(apiX, apiY int) (int, int) {
	t.ensureScreenDims()
	x, y := ScaleCoordinates(apiX, apiY, DefaultAPIWidth, DefaultAPIHeight, t.screenW, t.screenH)
	return ClampCoordinates(x, y, t.screenW, t.screenH)
}

func (t *ComputerTool) captureAfterAction(result agent.ToolResult) agent.ToolResult {
	time.Sleep(500 * time.Millisecond)
	_, block, err := CaptureAndEncode(DefaultAPIWidth)
	if err != nil {
		return result // Non-fatal
	}
	result.Images = []agent.ImageBlock{block}
	return result
}

type computerArgs struct {
	Action     string `json:"action"`
	X          int    `json:"x,omitempty"`
	Y          int    `json:"y,omitempty"`
	Text       string `json:"text,omitempty"`
	Keys       string `json:"keys,omitempty"`
	Button     string `json:"button,omitempty"`
	Clicks     int    `json:"clicks,omitempty"`
	Coordinate []int  `json:"coordinate,omitempty"` // Anthropic native: [x, y]
}

// normalizeArgs maps Anthropic native action names and coordinate format
// to our internal format.
func normalizeArgs(args *computerArgs) {
	// Map Anthropic coordinate array to x, y
	if len(args.Coordinate) == 2 {
		args.X = args.Coordinate[0]
		args.Y = args.Coordinate[1]
	}

	// Map Anthropic native action names to our actions
	switch args.Action {
	case "left_click":
		args.Action = "click"
		args.Button = "left"
		args.Clicks = 1
	case "right_click":
		args.Action = "click"
		args.Button = "right"
		args.Clicks = 1
	case "double_click":
		args.Action = "click"
		args.Button = "left"
		args.Clicks = 2
	case "middle_click":
		args.Action = "click"
		args.Button = "left" // fallback — no middle click support
		args.Clicks = 1
	case "triple_click":
		args.Action = "click"
		args.Button = "left"
		args.Clicks = 3
	case "mouse_move":
		args.Action = "move"
	case "key":
		args.Action = "hotkey"
		if args.Text != "" && args.Keys == "" {
			args.Keys = args.Text // Anthropic sends key combo in "text" field
		}
	case "screenshot":
		args.Action = "screenshot"
	}
}

func (t *ComputerTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name:        "computer",
		Description: "OS-level mouse and keyboard control for macOS. Supports click, type, hotkey, and move actions.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{"type": "string", "description": "Action to perform: click, type, hotkey, move"},
				"x":      map[string]any{"type": "integer", "description": "Screen X coordinate (for click/move)"},
				"y":      map[string]any{"type": "integer", "description": "Screen Y coordinate (for click/move)"},
				"text":   map[string]any{"type": "string", "description": "Text to type (for type action)"},
				"keys":   map[string]any{"type": "string", "description": "Key combination like command+c, command+shift+4 (for hotkey action)"},
				"button": map[string]any{"type": "string", "description": "Mouse button: left (default), right (for click action)"},
				"clicks": map[string]any{"type": "integer", "description": "Number of clicks: 1 (default), 2 for double-click (for click action)"},
			},
		},
		Required: []string{"action"},
	}
}

func (t *ComputerTool) RequiresApproval() bool { return true }

func (t *ComputerTool) NativeToolDef() *client.NativeToolDef {
	return &client.NativeToolDef{
		Type:            "computer_20251124",
		Name:            "computer",
		DisplayWidthPx:  DefaultAPIWidth,
		DisplayHeightPx: DefaultAPIHeight,
	}
}

var quartzChecked bool
var quartzAvailable bool

func checkQuartz() bool {
	if quartzChecked {
		return quartzAvailable
	}
	quartzChecked = true
	err := exec.CommandContext(context.Background(), "python3", "-c", "import Quartz").Run()
	quartzAvailable = err == nil
	return quartzAvailable
}

func (t *ComputerTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var args computerArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if args.Action == "" {
		return agent.ToolResult{Content: "missing required parameter: action", IsError: true}, nil
	}

	normalizeArgs(&args)

	switch args.Action {
	case "click":
		return t.click(ctx, args)
	case "type":
		return t.typeText(ctx, args)
	case "hotkey":
		return t.hotkey(ctx, args)
	case "move":
		return t.move(ctx, args)
	case "screenshot":
		return t.screenshot()
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("unknown action: %q (valid: click, type, hotkey, move, screenshot)", args.Action),
			IsError: true,
		}, nil
	}
}

func (t *ComputerTool) screenshot() (agent.ToolResult, error) {
	path, block, err := CaptureAndEncode(DefaultAPIWidth)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("screenshot error: %v", err), IsError: true}, nil
	}
	return agent.ToolResult{
		Content: fmt.Sprintf("Screenshot captured. Saved to: %s", path),
		Images:  []agent.ImageBlock{block},
	}, nil
}

func (t *ComputerTool) click(ctx context.Context, args computerArgs) (agent.ToolResult, error) {
	if !checkQuartz() {
		return agent.ToolResult{
			Content: "click/move requires pyobjc-framework-Quartz. Install with: pip3 install pyobjc-framework-Quartz",
			IsError: true,
		}, nil
	}
	x, y := t.scaleXY(args.X, args.Y)
	script := buildClickScript(x, y, args.Button, args.Clicks)
	out, err := exec.CommandContext(ctx, "python3", "-c", script).CombinedOutput()
	if err != nil {
		return agent.ToolResult{
			Content: fmt.Sprintf("click error: %v\n%s", err, string(out)),
			IsError: true,
		}, nil
	}
	clicks := args.Clicks
	if clicks < 1 {
		clicks = 1
	}
	button := args.Button
	if button == "" {
		button = "left"
	}
	result := agent.ToolResult{Content: fmt.Sprintf("Clicked %s button %d time(s) at (%d, %d)", button, clicks, x, y)}
	return t.captureAfterAction(result), nil
}

func (t *ComputerTool) typeText(ctx context.Context, args computerArgs) (agent.ToolResult, error) {
	if args.Text == "" {
		return agent.ToolResult{Content: "type action requires 'text' parameter", IsError: true}, nil
	}

	// For short text (≤20 chars), use direct keystroke — fast and reliable.
	// For longer text, use clipboard paste to avoid focus-loss races during
	// character-by-character keystroke delivery.
	if len(args.Text) <= 20 {
		escaped := escapeAppleScript(args.Text)
		script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)
		out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
		if err != nil {
			return agent.ToolResult{
				Content: fmt.Sprintf("type error: %v\n%s", err, string(out)),
				IsError: true,
			}, nil
		}
	} else {
		// Save clipboard, paste text, restore clipboard
		escaped := escapeAppleScript(args.Text)
		script := fmt.Sprintf(`
set savedClip to the clipboard
set the clipboard to "%s"
delay 0.05
tell application "System Events" to keystroke "v" using {command down}
delay 0.1
set the clipboard to savedClip`, escaped)
		out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
		if err != nil {
			return agent.ToolResult{
				Content: fmt.Sprintf("type error: %v\n%s", err, string(out)),
				IsError: true,
			}, nil
		}
	}

	result := agent.ToolResult{Content: fmt.Sprintf("Typed: %s", args.Text)}
	return t.captureAfterAction(result), nil
}

func (t *ComputerTool) hotkey(ctx context.Context, args computerArgs) (agent.ToolResult, error) {
	if args.Keys == "" {
		return agent.ToolResult{Content: "hotkey action requires 'keys' parameter", IsError: true}, nil
	}
	script, err := buildHotkeyScript(args.Keys)
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	out, execErr := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if execErr != nil {
		return agent.ToolResult{
			Content: fmt.Sprintf("hotkey error: %v\n%s", execErr, string(out)),
			IsError: true,
		}, nil
	}
	result := agent.ToolResult{Content: fmt.Sprintf("Pressed: %s", args.Keys)}
	return t.captureAfterAction(result), nil
}

func (t *ComputerTool) move(ctx context.Context, args computerArgs) (agent.ToolResult, error) {
	if !checkQuartz() {
		return agent.ToolResult{
			Content: "click/move requires pyobjc-framework-Quartz. Install with: pip3 install pyobjc-framework-Quartz",
			IsError: true,
		}, nil
	}
	x, y := t.scaleXY(args.X, args.Y)
	script := buildMoveScript(x, y)
	out, err := exec.CommandContext(ctx, "python3", "-c", script).CombinedOutput()
	if err != nil {
		return agent.ToolResult{
			Content: fmt.Sprintf("move error: %v\n%s", err, string(out)),
			IsError: true,
		}, nil
	}
	result := agent.ToolResult{Content: fmt.Sprintf("Moved cursor to (%d, %d)", x, y)}
	return t.captureAfterAction(result), nil
}

func buildClickScript(x, y int, button string, clicks int) string {
	if clicks < 1 {
		clicks = 1
	}
	if button == "" {
		button = "left"
	}

	mouseButton := "kCGMouseButtonLeft"
	mouseDown := "kCGEventLeftMouseDown"
	mouseUp := "kCGEventLeftMouseUp"
	if button == "right" {
		mouseButton = "kCGMouseButtonRight"
		mouseDown = "kCGEventRightMouseDown"
		mouseUp = "kCGEventRightMouseUp"
	}

	return fmt.Sprintf(`import Quartz
point = (%d, %d)
for i in range(%d):
    event = Quartz.CGEventCreateMouseEvent(None, Quartz.%s, point, Quartz.%s)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, event)
    event = Quartz.CGEventCreateMouseEvent(None, Quartz.%s, point, Quartz.%s)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, event)
`, x, y, clicks, mouseDown, mouseButton, mouseUp, mouseButton)
}

func buildMoveScript(x, y int) string {
	return fmt.Sprintf(`import Quartz
point = (%d, %d)
event = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventMouseMoved, point, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, event)
`, x, y)
}

// escapeAppleScript escapes a string for safe embedding in AppleScript string literals.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

var modifierMap = map[string]string{
	"command": "command down",
	"cmd":     "command down",
	"shift":   "shift down",
	"option":  "option down",
	"alt":     "option down",
	"control": "control down",
	"ctrl":    "control down",
}

func buildHotkeyScript(keys string) (string, error) {
	parts := strings.Split(strings.ToLower(keys), "+")
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid key combination: %q", keys)
	}

	key := strings.TrimSpace(parts[len(parts)-1])
	var modifiers []string
	for _, part := range parts[:len(parts)-1] {
		part = strings.TrimSpace(part)
		mod, ok := modifierMap[part]
		if !ok {
			return "", fmt.Errorf("unknown modifier: %q (valid: command, cmd, shift, option, alt, control, ctrl)", part)
		}
		modifiers = append(modifiers, mod)
	}

	escapedKey := escapeAppleScript(key)
	if len(modifiers) == 0 {
		return fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escapedKey), nil
	}

	modStr := strings.Join(modifiers, ", ")
	return fmt.Sprintf(`tell application "System Events" to keystroke "%s" using {%s}`, escapedKey, modStr), nil
}
