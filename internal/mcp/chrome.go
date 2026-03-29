package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// DefaultCDPPort is the default Chrome DevTools Protocol debugging port.
	DefaultCDPPort = 9222
)

// cdpMu serializes all EnsureChromeDebugPort calls to prevent concurrent
// callers (boot, tool call, supervisor) from racing to launch/kill Chrome.
var cdpMu sync.Mutex

var (
	cdpExecCommand   = exec.Command
	cdpUserHomeDir   = os.UserHomeDir
	cdpSleep         = time.Sleep
	cdpChromeAliveFn = cdpChromeAlive
	cdpChromePIDFn   = cdpChromePID
)

// IsChromeCDPReachable checks if Chrome's CDP endpoint is responding on the given port.
// Checks both IPv4 and IPv6 — Chrome may bind to [::1] if 127.0.0.1 is already in use.
func IsChromeCDPReachable(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	for _, host := range []string{"127.0.0.1", "[::1]"} {
		resp, err := client.Get(fmt.Sprintf("http://%s:%d/json/version", host, port))
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true
		}
	}
	return false
}

// EnsureChromeDebugPort checks if Chrome's CDP is reachable; if not, launches
// a CDP Chrome instance (minimized). Returns nil if CDP is available after the call.
// Serialized — concurrent callers block rather than racing to launch Chrome.
func EnsureChromeDebugPort(port int) error {
	cdpMu.Lock()
	defer cdpMu.Unlock()
	if IsChromeCDPReachable(port) {
		return nil
	}
	return LaunchCDPChrome(port)
}

// LaunchCDPChrome launches a separate Chrome instance with a copied profile
// and --remote-debugging-port enabled. The window starts minimized to avoid
// stealing focus. The user's regular Chrome is left untouched.
// Only supported on macOS.
func LaunchCDPChrome(port int) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("Chrome CDP only supported on macOS")
	}

	home, err := cdpUserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	cdpDataDir := filepath.Join(home, ".shannon", "chrome-cdp")

	// If a CDP Chrome is already running with our profile, give it a few seconds
	// to respond. If it doesn't, kill it and relaunch — the CDP port may be stuck.
	if cdpChromeAlive() {
		log.Printf("[chrome-cdp] Chrome already running, checking CDP on port %d", port)
		for i := 0; i < 6; i++ {
			cdpSleep(500 * time.Millisecond)
			if IsChromeCDPReachable(port) {
				return nil
			}
		}
		log.Printf("[chrome-cdp] CDP not responding, killing stale Chrome and relaunching")
		StopCDPChrome()
		// Wait for ALL Chrome processes (main + helpers) to exit before relaunching.
		// If they won't die, bail out — launching against a still-active profile
		// causes corruption.
		dead := false
		for i := 0; i < 10; i++ {
			cdpSleep(500 * time.Millisecond)
			if !cdpChromeAliveFn() {
				dead = true
				break
			}
		}
		if !dead {
			// Escalate: SIGKILL the main browser process
			if pid := cdpChromePIDFn(); pid != "" {
				log.Printf("[chrome-cdp] Chrome pid %s won't die, sending SIGKILL", pid)
				cdpExecCommand("kill", "-9", pid).Run() //nolint:errcheck
				cdpSleep(1 * time.Second)
				if cdpChromeAliveFn() {
					return fmt.Errorf("Chrome processes still alive after SIGKILL — cannot relaunch safely")
				}
			}
		}
		// Remove stale profile locks so the new instance can start cleanly
		os.Remove(filepath.Join(cdpDataDir, "SingletonLock"))
		os.Remove(filepath.Join(cdpDataDir, "SingletonSocket"))
	}

	// Only seed the CDP profile on first launch — copying into an existing
	// profile while Chrome is running can corrupt lock files.
	cookiesPath := filepath.Join(cdpDataDir, "Default", "Cookies")
	if _, err := os.Stat(cookiesPath); err != nil {
		srcProfile := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
		if err := prepareCDPProfile(srcProfile, cdpDataDir); err != nil {
			return fmt.Errorf("failed to prepare CDP profile: %w", err)
		}
	}

	log.Printf("[chrome-cdp] Launching CDP Chrome minimized (port %d)", port)
	cmd := cdpExecCommand("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", cdpDataDir),
		"--no-startup-window",
		"--no-first-run",
		"--no-default-browser-check",
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch Chrome: %w", err)
	}
	// Persist Chrome PID so orphaned processes can be cleaned up after a hard kill.
	writeCDPPIDFile(home, cmd.Process.Pid)
	go cmd.Wait() //nolint:errcheck

	// Wait for CDP to become reachable.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		cdpSleep(500 * time.Millisecond)
		if IsChromeCDPReachable(port) {
			log.Printf("[chrome-cdp] Chrome CDP reachable on port %d", port)
			// Minimize after a short delay — window may not exist yet when CDP first becomes reachable.
			go func() {
				cdpSleep(2 * time.Second)
				minimizeCDPChromeSync()
			}()
			return nil
		}
	}

	return fmt.Errorf("Chrome launched but CDP not reachable on port %d after 15s", port)
}

// minimizeCDPChromeSync minimizes the CDP Chrome windows using its PID.
// Runs synchronously — call from the launch flow after Chrome is ready.
func minimizeCDPChromeSync() {
	pid := cdpChromePID()
	if pid == "" {
		return
	}
	script := fmt.Sprintf(`
tell application "System Events"
	try
		set p to first process whose unix id is %s
		repeat with w in (every window of p)
			set miniaturized of w to true
		end repeat
	end try
end tell`, pid)
	if err := cdpExecCommand("osascript", "-e", script).Run(); err != nil {
		log.Printf("[chrome-cdp] minimize failed: %v", err)
	}
}

// StopCDPChrome sends SIGTERM to the CDP Chrome instance. Non-blocking — best
// effort for the signal-handler path where the daemon is about to exit anyway.
// The PID file is left intact; the next daemon startup will clean up via
// CleanupOrphanedCDPChrome if Chrome survives.
func StopCDPChrome() {
	home, err := cdpUserHomeDir()
	if err != nil {
		return
	}
	cdpDataDir := filepath.Join(home, ".shannon", "chrome-cdp")
	out, err := cdpExecCommand("pgrep", "-f", fmt.Sprintf("user-data-dir=%s", cdpDataDir)).Output()
	if err != nil || len(out) == 0 {
		removeCDPPIDFile(home)
		return
	}
	cdpExecCommand("pkill", "-f", fmt.Sprintf("user-data-dir=%s", cdpDataDir)).Run() //nolint:errcheck
	log.Printf("[chrome-cdp] SIGTERM sent to CDP Chrome")
}

// CleanupOrphanedCDPChrome kills any Chrome CDP processes left behind by a
// previous daemon that was hard-killed (SIGKILL). Must be called AFTER the
// daemon PID file lock is acquired — this guarantees no other daemon is alive,
// so any Chrome CDP we find is truly orphaned.
//
// Uses SIGTERM → wait → SIGKILL escalation and only removes the PID file once
// Chrome is confirmed dead.
func CleanupOrphanedCDPChrome() {
	home, err := cdpUserHomeDir()
	if err != nil {
		return
	}

	// Check if any CDP Chrome is alive — by PID file first, pgrep fallback.
	alive := false
	pidFile := filepath.Join(home, ".shannon", "chrome-cdp.pid")
	data, err := os.ReadFile(pidFile)
	if err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pidStr != "" {
			if cdpExecCommand("kill", "-0", pidStr).Run() == nil {
				alive = true
			} else {
				// Stale PID file — process already dead.
				os.Remove(pidFile)
			}
		} else {
			os.Remove(pidFile)
		}
	}
	if !alive {
		// No PID file or PID is dead — fallback: check by process pattern.
		alive = cdpChromeAliveFn()
	}
	if !alive {
		return
	}

	log.Printf("[chrome-cdp] Orphaned CDP Chrome from previous run, cleaning up")

	// SIGTERM first.
	cdpDataDir := filepath.Join(home, ".shannon", "chrome-cdp")
	cdpExecCommand("pkill", "-f", fmt.Sprintf("user-data-dir=%s", cdpDataDir)).Run() //nolint:errcheck

	// Wait up to 3s for graceful exit.
	for i := 0; i < 6; i++ {
		cdpSleep(500 * time.Millisecond)
		if !cdpChromeAliveFn() {
			removeCDPPIDFile(home)
			log.Printf("[chrome-cdp] Orphaned CDP Chrome stopped")
			return
		}
	}

	// Escalate: SIGKILL the main browser process.
	if pid := cdpChromePIDFn(); pid != "" {
		log.Printf("[chrome-cdp] Chrome won't die, sending SIGKILL to pid %s", pid)
		cdpExecCommand("kill", "-9", pid).Run() //nolint:errcheck
		cdpSleep(1 * time.Second)
	}

	if !cdpChromeAliveFn() {
		removeCDPPIDFile(home)
		log.Printf("[chrome-cdp] Orphaned CDP Chrome stopped (after SIGKILL)")
	} else {
		// Don't remove PID file — preserve it for manual investigation.
		log.Printf("[chrome-cdp] WARNING: orphaned CDP Chrome still alive after SIGKILL")
	}
}

// ShowCDPChrome restores all CDP Chrome windows and brings them to front.
// Creates a tab first if Chrome has no pages (--no-startup-window).
// Uses CDP directly to guarantee we control the right Chrome instance.
func ShowCDPChrome() error {
	if !IsChromeCDPReachable(DefaultCDPPort) {
		return ErrChromeNotRunning
	}
	targets, err := getAllCDPPageTargets(DefaultCDPPort)
	if err != nil || len(targets) == 0 {
		// No pages — create one so there's a window.
		newID, createErr := createCDPTarget(DefaultCDPPort)
		if createErr != nil {
			return fmt.Errorf("show chrome: %w", createErr)
		}
		targets = []string{newID}
	}
	// Collect unique window IDs and restore all of them.
	windowIDs, err := getUniqueWindowIDs(DefaultCDPPort, targets)
	if err != nil {
		return fmt.Errorf("show chrome: %w", err)
	}
	for _, wid := range windowIDs {
		setWindowBoundsByID(DefaultCDPPort, wid, "normal") //nolint:errcheck
	}
	// Activate the first target to bring Chrome to front.
	cdpBrowserCall(DefaultCDPPort, "Target.activateTarget", map[string]interface{}{
		"targetId": targets[0],
	}) //nolint:errcheck
	return nil
}

// HideCDPChrome minimizes all CDP Chrome windows via CDP.
func HideCDPChrome() error {
	if !IsChromeCDPReachable(DefaultCDPPort) {
		return ErrChromeNotRunning
	}
	targets, err := getAllCDPPageTargets(DefaultCDPPort)
	if err != nil || len(targets) == 0 {
		return nil // no pages = nothing to hide
	}
	windowIDs, err := getUniqueWindowIDs(DefaultCDPPort, targets)
	if err != nil {
		return fmt.Errorf("hide chrome: %w", err)
	}
	for _, wid := range windowIDs {
		setWindowBoundsByID(DefaultCDPPort, wid, "minimized") //nolint:errcheck
	}
	return nil
}

// CDPChromeStatus describes the state of the CDP Chrome process.
type CDPChromeStatus struct {
	Running    bool
	Visible    bool
	ProbeError bool // true if visibility could not be determined
}

// GetCDPChromeStatus queries the CDP Chrome state via CDP.
// Visible is true if ANY window is in a non-minimized state.
func GetCDPChromeStatus() CDPChromeStatus {
	if !IsChromeCDPReachable(DefaultCDPPort) {
		return CDPChromeStatus{}
	}
	targets, err := getAllCDPPageTargets(DefaultCDPPort)
	if err != nil || len(targets) == 0 {
		return CDPChromeStatus{Running: true, Visible: false}
	}
	windowIDs, err := getUniqueWindowIDs(DefaultCDPPort, targets)
	if err != nil {
		return CDPChromeStatus{Running: true, ProbeError: true}
	}
	for _, wid := range windowIDs {
		state, err := getWindowStatByID(DefaultCDPPort, wid)
		if err != nil {
			continue
		}
		if state != "minimized" {
			return CDPChromeStatus{Running: true, Visible: true}
		}
	}
	return CDPChromeStatus{Running: true, Visible: false}
}

// ErrChromeNotRunning indicates the CDP Chrome process is not running.
var ErrChromeNotRunning = fmt.Errorf("chrome CDP not running")

// --- CDP window control helpers ---

var cdpMsgID atomic.Int64

// getAllCDPPageTargets returns IDs of all "page" targets.
func getAllCDPPageTargets(port int) ([]string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var targets []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, err
	}
	var ids []string
	for _, t := range targets {
		if t.Type == "page" {
			ids = append(ids, t.ID)
		}
	}
	return ids, nil
}

// createCDPTarget creates a new blank page and returns its target ID.
func createCDPTarget(port int) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/new?about:blank", port))
	if err != nil {
		return "", fmt.Errorf("create tab: %w", err)
	}
	defer resp.Body.Close()
	var target struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&target); err != nil {
		return "", fmt.Errorf("parse new tab: %w", err)
	}
	return target.ID, nil
}

// getUniqueWindowIDs maps target IDs to unique window IDs.
func getUniqueWindowIDs(port int, targetIDs []string) ([]int, error) {
	seen := make(map[int]bool)
	var windowIDs []int
	for _, tid := range targetIDs {
		result, err := cdpBrowserCall(port, "Browser.getWindowForTarget", map[string]interface{}{
			"targetId": tid,
		})
		if err != nil {
			continue
		}
		var win struct {
			WindowID int `json:"windowId"`
		}
		if err := json.Unmarshal(result, &win); err != nil {
			continue
		}
		if !seen[win.WindowID] {
			seen[win.WindowID] = true
			windowIDs = append(windowIDs, win.WindowID)
		}
	}
	if len(windowIDs) == 0 {
		return nil, fmt.Errorf("no windows found")
	}
	return windowIDs, nil
}

// setWindowBoundsByID sets the window state for a specific window ID.
func setWindowBoundsByID(port, windowID int, state string) error {
	_, err := cdpBrowserCall(port, "Browser.setWindowBounds", map[string]interface{}{
		"windowId": windowID,
		"bounds":   map[string]interface{}{"windowState": state},
	})
	return err
}

// getWindowStatByID returns the window state for a specific window ID.
func getWindowStatByID(port, windowID int) (string, error) {
	result, err := cdpBrowserCall(port, "Browser.getWindowBounds", map[string]interface{}{
		"windowId": windowID,
	})
	if err != nil {
		return "", err
	}
	var bounds struct {
		Bounds struct {
			WindowState string `json:"windowState"`
		} `json:"bounds"`
	}
	if err := json.Unmarshal(result, &bounds); err != nil {
		return "", err
	}
	return bounds.Bounds.WindowState, nil
}

// getCDPBrowserWSURL returns the browser-level WebSocket debugger URL.
func getCDPBrowserWSURL(port int) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var info struct {
		WSURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.WSURL == "" {
		return "", fmt.Errorf("no webSocketDebuggerUrl")
	}
	return info.WSURL, nil
}

// cdpBrowserCall sends a single CDP Browser domain command and returns the result.
func cdpBrowserCall(port int, method string, params map[string]interface{}) (json.RawMessage, error) {
	wsURL, err := getCDPBrowserWSURL(port)
	if err != nil {
		return nil, fmt.Errorf("get ws url: %w", err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	id := cdpMsgID.Add(1)
	msg := map[string]interface{}{
		"id":     id,
		"method": method,
	}
	if params != nil {
		msg["params"] = params
	}
	if err := conn.WriteJSON(msg); err != nil {
		return nil, fmt.Errorf("ws write: %w", err)
	}

	// Read responses until we get ours (skip events).
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("ws read: %w", err)
		}
		var resp struct {
			ID     int64            `json:"id"`
			Result json.RawMessage  `json:"result"`
			Error  *json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			continue // skip malformed
		}
		if resp.ID != id {
			continue // skip events / other responses
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("cdp error: %s", string(*resp.Error))
		}
		return resp.Result, nil
	}
}




// BringCDPChromeToFront unminimizes and activates the CDP Chrome.
// Runs asynchronously to avoid blocking tool calls.
// Deprecated: use ShowCDPChrome() for synchronous control.
func BringCDPChromeToFront() {
	go func() { ShowCDPChrome() }()
}

// CDPChromePID returns the PID of the CDP Chrome main process, or "" if not running.
func CDPChromePID() string {
	return cdpChromePID()
}

// cdpChromeAlive returns true if any process (main or helper) is still running
// with our CDP user-data-dir. Used for shutdown/relaunch safety — ensures all
// Chrome processes have exited before relaunching against the same profile.
func cdpChromeAlive() bool {
	home, err := cdpUserHomeDir()
	if err != nil {
		return false
	}
	cdpDataDir := filepath.Join(home, ".shannon", "chrome-cdp")
	out, err := cdpExecCommand("pgrep", "-f", fmt.Sprintf("user-data-dir=%s", cdpDataDir)).Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// cdpChromePID returns the PID of the CDP Chrome main browser process, or "" if not running.
// Filters out Chrome Helper subprocesses which share the same --user-data-dir flag.
// Use for window management (front/hide/minimize) and targeted force-kill.
func cdpChromePID() string {
	home, err := cdpUserHomeDir()
	if err != nil {
		return ""
	}
	cdpDataDir := filepath.Join(home, ".shannon", "chrome-cdp")
	out, err := cdpExecCommand("pgrep", "-f", fmt.Sprintf("user-data-dir=%s", cdpDataDir)).Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	// pgrep returns all matching PIDs (main + helpers). Find the main browser
	// process by checking each PID's command — helpers contain "Helper" in path.
	for _, pid := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		cmdOut, err := cdpExecCommand("ps", "-p", pid, "-o", "command=").Output()
		if err != nil {
			continue
		}
		cmd := strings.TrimSpace(string(cmdOut))
		if strings.Contains(cmd, "Helper") || strings.Contains(cmd, "--type=") {
			continue // skip renderer, GPU, network, storage helpers
		}
		return pid
	}
	return ""
}

// cdpPIDFilePath returns the path to the Chrome CDP PID file.
func cdpPIDFilePath(home string) string {
	return filepath.Join(home, ".shannon", "chrome-cdp.pid")
}

// writeCDPPIDFile records the Chrome main process PID so it can be cleaned up
// after a hard kill.
func writeCDPPIDFile(home string, pid int) {
	path := cdpPIDFilePath(home)
	os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0600) //nolint:errcheck
}

// removeCDPPIDFile removes the Chrome CDP PID file.
func removeCDPPIDFile(home string) {
	os.Remove(cdpPIDFilePath(home))
}

// prepareCDPProfile creates a Chrome user-data-dir for CDP by copying key
// session files from the user's real Chrome profile.
func prepareCDPProfile(srcProfile, cdpDir string) error {
	defaultSrc := filepath.Join(srcProfile, "Default")
	defaultDst := filepath.Join(cdpDir, "Default")

	if err := os.MkdirAll(defaultDst, 0700); err != nil {
		return err
	}

	if err := copyFile(filepath.Join(srcProfile, "Local State"), filepath.Join(cdpDir, "Local State")); err != nil {
		log.Printf("[chrome-cdp] failed to copy Local State: %v", err)
	}

	// Critical files are logged on failure; others are best-effort.
	criticalFiles := map[string]bool{
		"Cookies":    true,
		"Login Data": true,
	}
	sessionFiles := []string{
		"Cookies",
		"Login Data",
		"Web Data",
		"Preferences",
		"Secure Preferences",
		"Network/Cookies",
		"Network/TransportSecurity",
	}
	for _, f := range sessionFiles {
		src := filepath.Join(defaultSrc, f)
		dst := filepath.Join(defaultDst, f)
		os.MkdirAll(filepath.Dir(dst), 0700) //nolint:errcheck
		if err := copyFile(src, dst); err != nil && criticalFiles[f] {
			log.Printf("[chrome-cdp] failed to copy critical file %s: %v", f, err)
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}
