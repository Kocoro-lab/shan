package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"

	"github.com/Kocoro-lab/shan/internal/agent"
)

type refEntry struct {
	path string
	role string
	pid  int
}

type AccessibilityTool struct {
	client  *AXClient
	refs    map[string]refEntry
	lastPID int
}

type accessibilityArgs struct {
	Action       string  `json:"action"`
	App          string  `json:"app,omitempty"`
	MaxDepth     int     `json:"max_depth,omitempty"`
	Budget       int     `json:"semantic_budget,omitempty"`
	Filter       string  `json:"filter,omitempty"`
	Ref          string  `json:"ref,omitempty"`
	Value        *string `json:"value,omitempty"`
	Query        string  `json:"query,omitempty"`
	Role         string  `json:"role,omitempty"`
	Identifier   string  `json:"identifier,omitempty"`
	DX           int     `json:"dx,omitempty"`
	DY           int     `json:"dy,omitempty"`
}

func (t *AccessibilityTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name:        "accessibility",
		Description: "Read the macOS accessibility tree and interact with UI elements by reference. Use read_tree to see elements, then click/press/set_value/get_value by ref. Use find to search elements by text/role. More reliable than coordinate-based clicking for standard macOS apps.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":          map[string]any{"type": "string", "description": "Action: read_tree, click, press, set_value, get_value, find, scroll"},
				"app":             map[string]any{"type": "string", "description": "Target app name (defaults to frontmost app)"},
				"max_depth":       map[string]any{"type": "integer", "description": "Tree depth (default: 25 semantic budget, layout containers cost 0)"},
				"semantic_budget": map[string]any{"type": "integer", "description": "Semantic depth budget (default: 25, layout containers cost 0 depth)"},
				"filter":          map[string]any{"type": "string", "description": "Filter: all (default) or interactive (for read_tree)"},
				"ref":             map[string]any{"type": "string", "description": "Element ref from read_tree (e.g. e14, for click/press/set_value/get_value/scroll)"},
				"value":           map[string]any{"type": "string", "description": "Value to set (for set_value)"},
				"query":           map[string]any{"type": "string", "description": "Text to search for (for find, case-insensitive substring)"},
				"role":            map[string]any{"type": "string", "description": "AX role filter (for find, e.g. AXButton)"},
				"identifier":     map[string]any{"type": "string", "description": "AX identifier to find (exact match, for find)"},
				"dx":              map[string]any{"type": "integer", "description": "Horizontal scroll amount in pixels (for scroll)"},
				"dy":              map[string]any{"type": "integer", "description": "Vertical scroll amount in pixels (for scroll, positive=down)"},
			},
		},
		Required: []string{"action"},
	}
}

func (t *AccessibilityTool) RequiresApproval() bool { return true }

func (t *AccessibilityTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	if runtime.GOOS != "darwin" || t.client == nil {
		return agent.ToolResult{Content: "accessibility tool is only available on macOS", IsError: true}, nil
	}

	var args accessibilityArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if args.Action == "" {
		return agent.ToolResult{Content: "missing required parameter: action", IsError: true}, nil
	}

	switch args.Action {
	case "read_tree":
		return t.readTree(ctx, args)
	case "click", "press":
		return t.performAction(ctx, args.Action, args.Ref)
	case "set_value":
		return t.setValue(ctx, args.Ref, args.Value)
	case "get_value":
		return t.getValue(ctx, args.Ref)
	case "find":
		return t.find(ctx, args)
	case "scroll":
		return t.scroll(ctx, args)
	default:
		return agent.ToolResult{
			Content: fmt.Sprintf("unknown action: %q (valid: read_tree, click, press, set_value, get_value, find, scroll)", args.Action),
			IsError: true,
		}, nil
	}
}

// validAppName checks that an app name contains only safe characters.
var validAppNamePattern = regexp.MustCompile(`^[a-zA-Z0-9 ._\-()]+$`)

func (t *AccessibilityTool) resolvePID(ctx context.Context, appName string) (int, error) {
	if appName == "" {
		return 0, nil // ax_server will use frontmost
	}
	if !validAppNamePattern.MatchString(appName) {
		return 0, fmt.Errorf("invalid app name %q — only letters, numbers, spaces, dots, hyphens, underscores, and parentheses allowed", appName)
	}
	result, err := t.client.Call(ctx, "resolve_pid", map[string]any{"app_name": appName})
	if err != nil {
		return 0, fmt.Errorf("app %q not found or not running", appName)
	}
	var pidResult struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(result, &pidResult); err != nil {
		return 0, fmt.Errorf("could not parse PID for %q", appName)
	}
	return pidResult.PID, nil
}

func (t *AccessibilityTool) readTree(ctx context.Context, args accessibilityArgs) (agent.ToolResult, error) {
	pid, err := t.resolvePID(ctx, args.App)
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	params := map[string]any{
		"filter": args.Filter,
	}
	if args.Filter == "" {
		params["filter"] = "all"
	}
	if pid > 0 {
		params["pid"] = pid
	}
	// Semantic budget takes priority over max_depth
	if args.Budget > 0 {
		params["semantic_budget"] = args.Budget
	} else if args.MaxDepth > 0 {
		params["max_depth"] = args.MaxDepth
	}

	result, err := t.client.Call(ctx, "read_tree", params)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("accessibility error: %v", err), IsError: true}, nil
	}

	var treeResult struct {
		App      string                            `json:"app"`
		PID      int                               `json:"pid"`
		Window   string                            `json:"window"`
		Elements []any                             `json:"elements"`
		RefPaths map[string]map[string]string       `json:"ref_paths"`
	}
	if err := json.Unmarshal(result, &treeResult); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("parse error: %v", err), IsError: true}, nil
	}

	t.refs = make(map[string]refEntry)
	t.lastPID = treeResult.PID

	for ref, entry := range treeResult.RefPaths {
		t.refs[ref] = refEntry{
			path: entry["path"],
			role: entry["role"],
			pid:  treeResult.PID,
		}
	}

	// Remove ref_paths from output (agent doesn't need them)
	var outputMap map[string]any
	json.Unmarshal(result, &outputMap)
	delete(outputMap, "ref_paths")

	outputJSON, _ := json.MarshalIndent(outputMap, "", "  ")
	content := string(outputJSON)

	// Truncate if too large
	if len(content) > 8000 {
		if elems, ok := outputMap["elements"].([]any); ok {
			lo, hi := 0, len(elems)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				outputMap["elements"] = elems[:mid]
				trial, _ := json.MarshalIndent(outputMap, "", "  ")
				if len(trial) <= 7800 {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			outputMap["elements"] = elems[:lo]
			outputMap["truncated"] = fmt.Sprintf("showing %d of %d elements — use filter='interactive' or lower semantic_budget", lo, len(elems))
			outputJSON, _ = json.MarshalIndent(outputMap, "", "  ")
			content = string(outputJSON)
		}
	}

	return agent.ToolResult{Content: content}, nil
}

func (t *AccessibilityTool) lookupRef(ref string) (refEntry, error) {
	if ref == "" {
		return refEntry{}, fmt.Errorf("missing required parameter: ref")
	}
	if t.refs == nil || len(t.refs) == 0 {
		return refEntry{}, fmt.Errorf("no refs available — call read_tree first")
	}
	entry, ok := t.refs[ref]
	if !ok {
		return refEntry{}, fmt.Errorf("unknown ref %q — call read_tree to get current refs", ref)
	}
	return entry, nil
}

func (t *AccessibilityTool) performAction(ctx context.Context, action string, ref string) (agent.ToolResult, error) {
	entry, err := t.lookupRef(ref)
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	params := map[string]any{
		"pid":  entry.pid,
		"path": entry.path,
	}
	if entry.role != "" {
		params["expected_role"] = entry.role
	}

	result, err := t.client.Call(ctx, action, params)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("accessibility error: %v", err), IsError: true}, nil
	}

	var actionResult struct {
		Result string `json:"result"`
	}
	json.Unmarshal(result, &actionResult)
	return agent.ToolResult{Content: actionResult.Result}, nil
}

func (t *AccessibilityTool) setValue(ctx context.Context, ref string, value *string) (agent.ToolResult, error) {
	entry, err := t.lookupRef(ref)
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if value == nil {
		return agent.ToolResult{Content: "set_value requires 'value' parameter", IsError: true}, nil
	}

	params := map[string]any{
		"pid":   entry.pid,
		"path":  entry.path,
		"value": *value,
	}
	if entry.role != "" {
		params["expected_role"] = entry.role
	}

	result, err := t.client.Call(ctx, "set_value", params)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("accessibility error: %v", err), IsError: true}, nil
	}

	var actionResult struct {
		Result string `json:"result"`
	}
	json.Unmarshal(result, &actionResult)
	return agent.ToolResult{Content: actionResult.Result}, nil
}

func (t *AccessibilityTool) getValue(ctx context.Context, ref string) (agent.ToolResult, error) {
	entry, err := t.lookupRef(ref)
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	params := map[string]any{
		"pid":  entry.pid,
		"path": entry.path,
	}

	result, err := t.client.Call(ctx, "get_value", params)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("accessibility error: %v", err), IsError: true}, nil
	}

	var actionResult struct {
		Result string `json:"result"`
		Role   string `json:"role"`
	}
	json.Unmarshal(result, &actionResult)
	msg := actionResult.Result
	if actionResult.Role != "" {
		msg = fmt.Sprintf("%s (role: %s)", msg, actionResult.Role)
	}
	return agent.ToolResult{Content: msg}, nil
}

func (t *AccessibilityTool) find(ctx context.Context, args accessibilityArgs) (agent.ToolResult, error) {
	pid, err := t.resolvePID(ctx, args.App)
	if err != nil {
		return agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	params := map[string]any{}
	if pid > 0 {
		params["pid"] = pid
	}
	if args.Query != "" {
		params["query"] = args.Query
	}
	if args.Role != "" {
		params["role"] = args.Role
	}
	if args.Identifier != "" {
		params["identifier"] = args.Identifier
	}

	result, err := t.client.Call(ctx, "find", params)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("find error: %v", err), IsError: true}, nil
	}

	outputJSON, _ := json.MarshalIndent(json.RawMessage(result), "", "  ")
	content := string(outputJSON)
	if len(content) > 8000 {
		content = content[:7900] + "\n... [truncated]"
	}
	return agent.ToolResult{Content: content}, nil
}

func (t *AccessibilityTool) scroll(ctx context.Context, args accessibilityArgs) (agent.ToolResult, error) {
	pid := t.lastPID
	var path *string
	if args.Ref != "" {
		entry, err := t.lookupRef(args.Ref)
		if err != nil {
			return agent.ToolResult{Content: err.Error(), IsError: true}, nil
		}
		pid = entry.pid
		path = &entry.path
	}

	params := map[string]any{
		"dx": args.DX,
		"dy": args.DY,
	}
	if pid > 0 {
		params["pid"] = pid
	}
	if path != nil {
		params["path"] = *path
	}

	result, err := t.client.Call(ctx, "scroll", params)
	if err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("scroll error: %v", err), IsError: true}, nil
	}

	var actionResult struct {
		Result string `json:"result"`
	}
	json.Unmarshal(result, &actionResult)
	return agent.ToolResult{Content: actionResult.Result}, nil
}
