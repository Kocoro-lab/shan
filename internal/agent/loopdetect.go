package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
)

// LoopAction tells the agent loop how to respond to a detection signal.
type LoopAction int

const (
	LoopContinue  LoopAction = iota // proceed normally
	LoopNudge                        // inject "try different approach" message
	LoopForceStop                    // force final response without tools
)

// ToolCallRecord tracks a single tool invocation for loop detection.
type ToolCallRecord struct {
	Name      string
	ArgsHash  string // hex-encoded hash of raw args
	TopicHash string // hex-encoded hash of normalized args (web tools)
	ResultSig string // domain signature from results (web tools)
	IsError   bool
	ErrorSig  string // first 100 chars of error for grouping
	IsSleep   bool   // bash command contains sleep
}

// LoopDetector uses a sliding window of recent tool calls to detect stuck loops.
//
// Nine detection paths (checked in order, first match wins):
//   - ToolModeSwitch: visual tool after successful GUI-adjacent tool (applescript/browser)
//   - SuccessAfterError: visual tool after error recovery
//   - ConsecutiveDuplicate: back-to-back identical calls (catches web_search→web_search)
//   - ExactDuplicate: same name+argsHash spread across window (catches read→edit→read→edit→read)
//   - SameToolError: same tool returns errors N+ times in window
//   - FamilyNoProgress: tools in the same family, counted by topic similarity
//     (3 same-topic → nudge, 5 → stronger nudge, 7 → force stop)
//     Fallback: same-tool count when topic tracking unavailable (5 → nudge, 7 → force stop)
//   - SearchEscalation: consecutive search-family calls without intervening non-search tools
//     (3 consecutive → nudge, 5 consecutive → force stop)
//   - NoProgress: same tool called M+ times regardless of args (skip visual tools)
//   - Sleep: bash commands containing sleep (2 → nudge, 4 → force stop)
//
// Response escalation: threshold = nudge, threshold+1 = force stop (consecutive), 2x threshold = force stop (others).
type LoopDetector struct {
	history     []ToolCallRecord
	historySize int

	consecDupThreshold   int // back-to-back identical calls (default 2)
	exactDupThreshold    int // spread-out identical calls in window (default 3)
	sameToolErrThreshold int
	noProgressThreshold  int

	repeatableTools map[string]bool

	// ToolModeSwitch detector state
	lastNonGUISuccess bool
	lastNonGUITool    string
	modeSwitchNudged  bool

	// SuccessAfterError detector state
	recentRecovery bool
	recoveredTool  string
}

// GUITools are tools that indicate GUI automation tasks.
// Used by both LoopDetector (exempt from NoProgress) and effectiveMaxIter (higher limit).
var GUITools = map[string]bool{
	"screenshot": true, "computer": true, "applescript": true, "browser": true, "accessibility": true,
}

// visualTools are tools used purely for visual verification (screenshots, mouse/keyboard).
// Separate from GUITools because applescript/browser return structured data results.
// Used by the mode-switch detector to distinguish data tools from visual verification.
var visualTools = map[string]bool{
	"screenshot": true, "computer": true, "accessibility": true,
}

// repeatableGUITools extends visualTools with browser — multi-step browser workflows
// (navigate → snapshot → click → type → snapshot) naturally use many calls with
// different actions, so the no-progress fallback should not over-trigger.
// Topic-based detection (FamilyNoProgress) still catches same-URL loops.
var repeatableGUITools = map[string]bool{
	"screenshot": true, "computer": true, "accessibility": true, "browser": true,
}

// NewLoopDetector creates a detector with production defaults.
func NewLoopDetector() *LoopDetector {
	return &LoopDetector{
		history:              make([]ToolCallRecord, 0, 20),
		historySize:          20,
		consecDupThreshold:   2,
		exactDupThreshold:    3,
		sameToolErrThreshold: 4,
		noProgressThreshold:  8,
		repeatableTools:      repeatableGUITools,
	}
}

// Record adds a tool call to the sliding window.
func (ld *LoopDetector) Record(name, argsJSON string, isError bool, errMsg string, resultSig string) {
	topicHash := ""
	if ToolFamilies[name] != "" {
		normalized := normalizeWebQuery(argsJSON)
		if normalized != "" {
			topicHash = hashArgs(normalized)
		}
	}
	rec := ToolCallRecord{
		Name:      name,
		ArgsHash:  hashArgs(argsJSON),
		TopicHash: topicHash,
		ResultSig: resultSig,
		IsError:   isError,
		ErrorSig:  truncateErrSig(errMsg, 100),
		IsSleep:   name == "bash" && isSleepCommand(argsJSON),
	}
	ld.history = append(ld.history, rec)
	if len(ld.history) > ld.historySize {
		ld.history = ld.history[len(ld.history)-ld.historySize:]
	}

	// Track non-visual tool success for mode-switch detection.
	// Uses visualTools (not GUITools) because applescript/browser return structured data.
	if !visualTools[name] {
		if isError {
			ld.lastNonGUISuccess = false
			ld.lastNonGUITool = ""
		} else {
			ld.lastNonGUISuccess = true
			ld.lastNonGUITool = name
			ld.modeSwitchNudged = false
		}
	}

	// Track error recovery for SuccessAfterError detection
	if !isError && !visualTools[name] {
		// Check if this tool had a previous error in the window with different args
		hasEarlierError := false
		for _, rec := range ld.history[:len(ld.history)-1] { // exclude the just-recorded entry
			if rec.Name == name && rec.IsError && rec.ArgsHash != ld.history[len(ld.history)-1].ArgsHash {
				hasEarlierError = true
				break
			}
		}
		if hasEarlierError {
			ld.recentRecovery = true
			ld.recoveredTool = name
		} else if name != ld.recoveredTool {
			// Moving to different non-visual tool → reset recovery state
			ld.recentRecovery = false
			ld.recoveredTool = ""
		}
	}
}

// Check evaluates all detectors for the named tool.
// Returns the most severe action and an appropriate message.
func (ld *LoopDetector) Check(name string) (LoopAction, string) {
	if len(ld.history) < 2 {
		return LoopContinue, ""
	}

	// 0. Mode switch: visual tool used right after successful GUI-adjacent tool
	// (applescript, browser). Only fire for GUI-adjacent tools where visual
	// verification is likely redundant. Don't fire after file_read, bash, etc.
	if visualTools[name] && ld.lastNonGUISuccess && !ld.modeSwitchNudged && GUITools[ld.lastNonGUITool] {
		ld.modeSwitchNudged = true
		return LoopNudge, fmt.Sprintf(
			"Your previous non-GUI tool call (%s) returned a success result. Visual verification is likely unnecessary — consider whether you can summarize the result and stop.", ld.lastNonGUITool)
	}

	// 0b. Success after error: agent recovered from error but continues verifying
	if visualTools[name] && ld.recentRecovery {
		ld.recentRecovery = false
		return LoopNudge, fmt.Sprintf(
			"You recovered from the earlier %s error and the retry succeeded. The successful result is your confirmation — proceed to your final answer.", ld.recoveredTool)
	}

	// Find latest argsHash for this tool (must be called right after Record).
	var latestHash string
	for i := len(ld.history) - 1; i >= 0; i-- {
		if ld.history[i].Name == name {
			latestHash = ld.history[i].ArgsHash
			break
		}
	}

	// 1a. Consecutive exact duplicate — catches back-to-back identical calls
	// like web_search→web_search. Does NOT fire for read→edit→read patterns
	// because the intervening edit breaks the consecutive run.
	consecCount := 0
	for i := len(ld.history) - 1; i >= 0; i-- {
		if ld.history[i].Name != name || ld.history[i].ArgsHash != latestHash {
			break
		}
		consecCount++
	}
	if consecCount >= ld.consecDupThreshold+1 {
		return LoopForceStop, fmt.Sprintf(
			"You have called %s with identical arguments %d times in a row. Stop retrying and provide your answer now.", name, consecCount)
	}
	if consecCount >= ld.consecDupThreshold {
		return LoopNudge, fmt.Sprintf(
			"You've called %s %d times consecutively with identical arguments. The results won't change. Use the results you already have or try a different approach.", name, consecCount)
	}

	// 1b. Window-based exact duplicate — catches spread-out repeats
	// like read→edit→read→edit→read (same args appearing 3+ times in window).
	dupCount := 0
	if latestHash != "" {
		for _, rec := range ld.history {
			if rec.Name == name && rec.ArgsHash == latestHash {
				dupCount++
			}
		}
	}
	if dupCount >= ld.exactDupThreshold*2 {
		return LoopForceStop, fmt.Sprintf(
			"You have called %s with identical arguments %d times. Stop retrying and provide your answer now.", name, dupCount)
	}
	if dupCount >= ld.exactDupThreshold {
		return LoopNudge, fmt.Sprintf(
			"You've called %s %d times with identical arguments and similar results. Try a fundamentally different approach.", name, dupCount)
	}

	// 2. Same tool error detector: same tool returning errors
	errCount := 0
	var lastErr string
	for _, rec := range ld.history {
		if rec.Name == name && rec.IsError {
			errCount++
			lastErr = rec.ErrorSig
		}
	}
	if errCount >= ld.sameToolErrThreshold*2 {
		return LoopForceStop, fmt.Sprintf(
			"Tool %s has failed %d times. Stop using it and provide your answer now.", name, errCount)
	}
	if errCount >= ld.sameToolErrThreshold {
		return LoopNudge, fmt.Sprintf(
			"Tool %s has failed %d times with: %s. Do NOT retry this tool. Use a different approach or ask the user.", name, errCount, lastErr)
	}

	// 3. Family no-progress: web tools in the same family, counted by topic similarity.
	// Tiered escalation: 3 same-topic → nudge, 5 → stronger nudge, 7 → force stop.
	family := ToolFamilies[name]
	if family != "" {
		latestTopic := ""
		latestResult := ""
		for i := len(ld.history) - 1; i >= 0; i-- {
			if ToolFamilies[ld.history[i].Name] == family {
				if latestTopic == "" && ld.history[i].TopicHash != "" {
					latestTopic = ld.history[i].TopicHash
				}
				if latestResult == "" && ld.history[i].ResultSig != "" {
					latestResult = ld.history[i].ResultSig
				}
				if latestTopic != "" && latestResult != "" {
					break
				}
			}
		}

		familyCount := 0
		sameTopicCount := 0
		sameResultCount := 0
		for _, rec := range ld.history {
			if ToolFamilies[rec.Name] != family {
				continue
			}
			familyCount++
			if latestTopic != "" && rec.TopicHash == latestTopic {
				sameTopicCount++
			}
			if latestResult != "" && rec.ResultSig == latestResult {
				sameResultCount++
			}
		}

		progressCount := sameTopicCount
		if sameResultCount > progressCount {
			progressCount = sameResultCount
		}

		if progressCount >= 7 {
			return LoopForceStop, fmt.Sprintf(
				"You have made %d web calls with %d on the same topic. Return your collected results now.", familyCount, progressCount)
		}
		if progressCount >= 5 {
			return LoopNudge, fmt.Sprintf(
				"You've searched the same topic %d times. Summarize what you've found and present it to the user. Do not search again.", progressCount)
		}
		if progressCount >= 3 {
			return LoopNudge, fmt.Sprintf(
				"You've searched the same topic %d times with similar results. Use the results you already have or try a fundamentally different query.", progressCount)
		}

		// Fallback for families without topic/result tracking (e.g., GUI tools
		// where normalizer can't extract topics from script/screenshot args).
		// Count same-tool occurrences as a proxy for lack of progress.
		// Skip repeatable tools (screenshot, computer, accessibility) — they're
		// expected to be called many times in GUI automation workflows.
		if progressCount == 0 && !ld.repeatableTools[name] {
			sameToolInFamily := 0
			for _, rec := range ld.history {
				if rec.Name == name {
					sameToolInFamily++
				}
			}
			if sameToolInFamily >= 7 {
				return LoopForceStop, fmt.Sprintf(
					"You have called %s %d times without meaningful progress. Provide your answer now.", name, sameToolInFamily)
			}
			if sameToolInFamily >= 5 {
				return LoopNudge, fmt.Sprintf(
					"You've called %s %d times. Consider whether you're making progress or stuck in a loop.", name, sameToolInFamily)
			}
		}
	}

	// 4. Search escalation: consecutive search-family calls without other tools between them.
	// Catches grep→grep→grep or grep→glob→grep without acting on results.
	if family == "search" {
		consecSearch := 0
		for i := len(ld.history) - 1; i >= 0; i-- {
			if ToolFamilies[ld.history[i].Name] == "search" {
				consecSearch++
			} else {
				break
			}
		}
		if consecSearch >= 5 {
			return LoopForceStop, fmt.Sprintf(
				"You have made %d consecutive search calls without acting on results. Stop searching and use what you have, or ask the user for guidance.", consecSearch)
		}
		if consecSearch >= 3 {
			return LoopNudge, fmt.Sprintf(
				"You've made %d search calls without finding useful results. Reconsider your approach — try different search terms, check if the file/pattern exists, or ask the user for guidance.", consecSearch)
		}
	}

	// 5. No progress detector: same tool called too many times (skip for GUI tools)
	if !ld.repeatableTools[name] {
		count := 0
		for _, rec := range ld.history {
			if rec.Name == name {
				count++
			}
		}
		if count >= ld.noProgressThreshold*2 {
			return LoopForceStop, fmt.Sprintf(
				"You have called %s %d times without meaningful progress. Provide your answer now.", name, count)
		}
		if count >= ld.noProgressThreshold {
			return LoopNudge, fmt.Sprintf(
				"You've called %s %d times. Summarize what you've learned and try a different approach.", name, count)
		}
	}

	// 6. Sleep detector: bash commands containing sleep indicate polling/waiting
	sleepCount := 0
	for _, rec := range ld.history {
		if rec.IsSleep {
			sleepCount++
		}
	}
	if sleepCount >= 4 {
		return LoopForceStop, fmt.Sprintf(
			"You have used `sleep` in bash commands %d times. Stop polling and provide your answer now.", sleepCount)
	}
	if sleepCount >= 2 {
		return LoopNudge, fmt.Sprintf(
			"You've used `sleep` in bash commands %d times. Do not poll or wait in loops — diagnose the root cause, use a check command, or ask the user.", sleepCount)
	}

	return LoopContinue, ""
}

func hashArgs(args string) string {
	h := sha256.Sum256([]byte(args))
	return hex.EncodeToString(h[:8])
}

// sleepPattern matches `sleep` followed by a number, as a word boundary.
// Catches: "sleep 5", "sleep 1 && curl ...", "while true; do sleep 1; done"
// Avoids: "sleep.log", "sleeper"
var sleepPattern = regexp.MustCompile(`\bsleep\s+\d`)

// isSleepCommand checks whether a bash tool's JSON args contain a sleep command.
func isSleepCommand(argsJSON string) bool {
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return false
	}
	return sleepPattern.MatchString(args.Command)
}

func truncateErrSig(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen])
}
