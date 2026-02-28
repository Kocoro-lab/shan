package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/Kocoro-lab/shan/internal/agent"
)

type BrowserTool struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	active bool
}

type browserArgs struct {
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
	Script   string `json:"script,omitempty"`
	Timeout  int    `json:"timeout,omitempty"`
}

func (t *BrowserTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name: "browser",
		Description: "Control a browser with an isolated profile (no access to user's logged-in sessions or saved data). " +
			"Launches Chrome for Testing or a temporary Chrome instance. " +
			"Actions: navigate, click, type, screenshot, read_page, execute_js, wait, close.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":   map[string]any{"type": "string", "description": "Action to perform: navigate, click, type, screenshot, read_page, execute_js, wait, close"},
				"url":      map[string]any{"type": "string", "description": "URL to navigate to (for navigate action)"},
				"selector": map[string]any{"type": "string", "description": "CSS selector (for click, type, read_page, wait actions)"},
				"text":     map[string]any{"type": "string", "description": "Text to type (for type action)"},
				"script":   map[string]any{"type": "string", "description": "JavaScript to execute (for execute_js action)"},
				"timeout":  map[string]any{"type": "integer", "description": "Timeout in seconds (default: 30)"},
			},
		},
		Required: []string{"action"},
	}
}

func (t *BrowserTool) RequiresApproval() bool { return true }

func (t *BrowserTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var args browserArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if args.Action == "" {
		return agent.ToolResult{Content: "missing required parameter: action", IsError: true}, nil
	}

	timeout := 30 * time.Second
	if args.Timeout > 0 {
		timeout = time.Duration(args.Timeout) * time.Second
	}

	switch args.Action {
	case "navigate":
		return t.navigate(ctx, args, timeout)
	case "click":
		return t.click(ctx, args, timeout)
	case "type":
		return t.typeText(ctx, args, timeout)
	case "screenshot":
		return t.screenshot(ctx, timeout)
	case "read_page":
		return t.readPage(ctx, args, timeout)
	case "execute_js":
		return t.executeJS(ctx, args, timeout)
	case "wait":
		return t.waitVisible(ctx, args, timeout)
	case "close":
		return t.closeBrowser()
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("unknown action: %q (valid: navigate, click, type, screenshot, read_page, execute_js, wait, close)", args.Action),
			IsError: true,
		}, nil
	}
}

func (t *BrowserTool) ensureBrowser() (context.Context, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If we have an active browser, verify the context is still alive
	if t.active {
		if t.ctx.Err() == nil {
			return t.ctx, nil
		}
		// Browser context is dead (Chrome crashed or devtools disconnected) — restart
		t.cancel()
		t.ctx = nil
		t.cancel = nil
		t.active = false
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)

	// Eagerly start the browser process so it's fully ready before any actions.
	// Without this, the first chromedp.Run both starts Chrome AND executes actions,
	// which can cause races with devtools session setup.
	if err := chromedp.Run(browserCtx); err != nil {
		browserCancel()
		allocCancel()
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	t.ctx = browserCtx
	t.cancel = func() {
		browserCancel()
		allocCancel()
	}
	t.active = true

	return t.ctx, nil
}

func (t *BrowserTool) navigate(_ context.Context, args browserArgs, timeout time.Duration) (agent.ToolResult, error) {
	if args.URL == "" {
		return agent.ToolResult{Content: "navigate action requires 'url' parameter", IsError: true}, nil
	}

	bCtx, err := t.ensureBrowser()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to start browser: %v", err), IsError: true}, nil
	}

	tCtx, cancel := context.WithTimeout(bCtx, timeout)
	defer cancel()

	var title string
	err = chromedp.Run(tCtx,
		chromedp.Navigate(args.URL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Title(&title),
	)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("navigate error: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: fmt.Sprintf("Navigated to: %s\nTitle: %s", args.URL, title)}, nil
}

func (t *BrowserTool) click(_ context.Context, args browserArgs, timeout time.Duration) (agent.ToolResult, error) {
	if args.Selector == "" {
		return agent.ToolResult{Content: "click action requires 'selector' parameter", IsError: true}, nil
	}

	bCtx, err := t.ensureBrowser()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to start browser: %v", err), IsError: true}, nil
	}

	tCtx, cancel := context.WithTimeout(bCtx, timeout)
	defer cancel()

	err = chromedp.Run(tCtx, chromedp.Click(args.Selector))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("click error: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: fmt.Sprintf("Clicked: %s", args.Selector)}, nil
}

func (t *BrowserTool) typeText(_ context.Context, args browserArgs, timeout time.Duration) (agent.ToolResult, error) {
	if args.Selector == "" {
		return agent.ToolResult{Content: "type action requires 'selector' parameter", IsError: true}, nil
	}

	bCtx, err := t.ensureBrowser()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to start browser: %v", err), IsError: true}, nil
	}

	tCtx, cancel := context.WithTimeout(bCtx, timeout)
	defer cancel()

	err = chromedp.Run(tCtx, chromedp.SendKeys(args.Selector, args.Text))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("type error: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: fmt.Sprintf("Typed into: %s", args.Selector)}, nil
}

func (t *BrowserTool) screenshot(_ context.Context, timeout time.Duration) (agent.ToolResult, error) {
	bCtx, err := t.ensureBrowser()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to start browser: %v", err), IsError: true}, nil
	}

	tCtx, cancel := context.WithTimeout(bCtx, timeout)
	defer cancel()

	var buf []byte
	err = chromedp.Run(tCtx, chromedp.FullScreenshot(&buf, 90))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("screenshot error: %v", err), IsError: true}, nil
	}

	f, err := os.CreateTemp("", "browser-screenshot-*.png")
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to create temp file: %v", err), IsError: true}, nil
	}
	defer f.Close()

	if _, err := f.Write(buf); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to write screenshot: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: fmt.Sprintf("Screenshot saved to: %s", f.Name())}, nil
}

func (t *BrowserTool) readPage(_ context.Context, args browserArgs, timeout time.Duration) (agent.ToolResult, error) {
	bCtx, err := t.ensureBrowser()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to start browser: %v", err), IsError: true}, nil
	}

	tCtx, cancel := context.WithTimeout(bCtx, timeout)
	defer cancel()

	selector := "html"
	if args.Selector != "" {
		selector = args.Selector
	}

	var html string
	err = chromedp.Run(tCtx, chromedp.OuterHTML(selector, &html))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("read_page error: %v", err), IsError: true}, nil
	}

	// Extract text content via JS for cleaner output
	var textContent string
	err = chromedp.Run(tCtx, chromedp.Evaluate(
		fmt.Sprintf(`document.querySelector(%q)?.innerText || ""`, selector),
		&textContent,
	))
	if err != nil {
		// Fall back to raw HTML if text extraction fails
		textContent = html
	}

	const maxLen = 10240
	if len(textContent) > maxLen {
		textContent = textContent[:maxLen] + "\n... [truncated to 10KB]"
	}

	return agent.ToolResult{Content: textContent}, nil
}

func (t *BrowserTool) executeJS(_ context.Context, args browserArgs, timeout time.Duration) (agent.ToolResult, error) {
	if args.Script == "" {
		return agent.ToolResult{Content: "execute_js action requires 'script' parameter", IsError: true}, nil
	}

	bCtx, err := t.ensureBrowser()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to start browser: %v", err), IsError: true}, nil
	}

	tCtx, cancel := context.WithTimeout(bCtx, timeout)
	defer cancel()

	var result any
	err = chromedp.Run(tCtx, chromedp.Evaluate(args.Script, &result))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("execute_js error: %v", err), IsError: true}, nil
	}

	output := fmt.Sprintf("%v", result)
	const maxLen = 10240
	if len(output) > maxLen {
		output = output[:maxLen] + "\n... [truncated to 10KB]"
	}

	return agent.ToolResult{Content: output}, nil
}

func (t *BrowserTool) waitVisible(_ context.Context, args browserArgs, timeout time.Duration) (agent.ToolResult, error) {
	if args.Selector == "" {
		return agent.ToolResult{Content: "wait action requires 'selector' parameter", IsError: true}, nil
	}

	bCtx, err := t.ensureBrowser()
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("failed to start browser: %v", err), IsError: true}, nil
	}

	tCtx, cancel := context.WithTimeout(bCtx, timeout)
	defer cancel()

	err = chromedp.Run(tCtx, chromedp.WaitVisible(args.Selector))
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("wait error: %v", err), IsError: true}, nil
	}

	return agent.ToolResult{Content: fmt.Sprintf("Element visible: %s", args.Selector)}, nil
}

func (t *BrowserTool) closeBrowser() (agent.ToolResult, error) {
	t.mu.Lock()
	wasActive := t.active
	t.mu.Unlock()

	if !wasActive {
		return agent.ToolResult{Content: "Browser is not running"}, nil
	}

	t.cleanup()
	return agent.ToolResult{Content: "Browser closed"}, nil
}

// Cleanup shuts down the browser if running. Safe to call multiple times.
func (t *BrowserTool) Cleanup() {
	t.cleanup()
}

func (t *BrowserTool) cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.active {
		return
	}

	t.cancel()
	t.ctx = nil
	t.cancel = nil
	t.active = false
}

