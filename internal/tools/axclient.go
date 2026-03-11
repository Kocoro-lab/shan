package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

// AXRequest is a JSON-RPC request sent to ax_server.
type AXRequest struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

// AXResponse is a JSON-RPC response from ax_server.
type AXResponse struct {
	ID     int64            `json:"id"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *AXError         `json:"error,omitempty"`
}

// AXError is an error returned by ax_server.
type AXError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// AXClient manages a persistent ax_server subprocess and multiplexes
// requests by ID. Multiple goroutines can call Call() concurrently.
type AXClient struct {
	mu      sync.Mutex // guards process lifecycle (start/restart)
	writeMu sync.Mutex // guards stdin writes
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	nextID  atomic.Int64
	started bool

	pendingMu sync.Mutex
	pending   map[int64]chan AXResponse
}

// Ensure starts the ax_server process if not already running.
func (c *AXClient) Ensure(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	binPath, err := axServerPath()
	if err != nil {
		return err
	}

	// Use exec.Command (not CommandContext) — the process lifecycle is managed
	// independently of any single request's context. Per-request cancellation
	// is handled in Call()'s select block.
	c.cmd = exec.Command(binPath)
	var pipeErr error
	c.stdin, pipeErr = c.cmd.StdinPipe()
	if pipeErr != nil {
		return fmt.Errorf("ax_server stdin pipe: %w", pipeErr)
	}
	stdout, pipeErr := c.cmd.StdoutPipe()
	if pipeErr != nil {
		return fmt.Errorf("ax_server stdout pipe: %w", pipeErr)
	}
	c.cmd.Stderr = os.Stderr
	c.pending = make(map[int64]chan AXResponse)

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("ax_server start: %w", err)
	}
	c.started = true

	// Reader goroutine dispatches responses by ID.
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			var resp AXResponse
			if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
				continue
			}
			c.pendingMu.Lock()
			ch, ok := c.pending[resp.ID]
			if ok {
				delete(c.pending, resp.ID)
			}
			c.pendingMu.Unlock()
			if ok {
				ch <- resp
			}
		}
		// EOF: ax_server died — unblock all pending callers
		c.pendingMu.Lock()
		for id, ch := range c.pending {
			ch <- AXResponse{ID: id, Error: &AXError{Code: -1, Message: "ax_server: unexpected EOF"}}
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()

		// Wait for process exit and mark as dead so next Ensure() restarts it.
		c.cmd.Wait()
		c.mu.Lock()
		c.started = false
		c.mu.Unlock()
	}()

	return nil
}

// Call sends a request and waits for the response.
func (c *AXClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("ax_server is macOS-only")
	}

	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}

	id := c.nextID.Add(1)
	req := AXRequest{ID: id, Method: method, Params: params}

	// Register pending channel BEFORE writing
	ch := make(chan AXResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	data, _ := json.Marshal(req)
	data = append(data, '\n')

	c.writeMu.Lock()
	n, writeErr := c.stdin.Write(data)
	if writeErr == nil && n < len(data) {
		writeErr = io.ErrShortWrite
	}
	c.writeMu.Unlock()

	if writeErr != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("ax_server write: %w", writeErr)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("ax_server: %s", resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// Close terminates the ax_server process.
func (c *AXClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	c.started = false
}

func axServerPath() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		// Same directory as shan binary
		p := filepath.Join(dir, "ax_server")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		// npm: bin/ax_server
		p = filepath.Join(dir, "bin", "ax_server")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Development: relative to working directory
	p := filepath.Join("internal", "tools", "axserver", ".build", "debug", "ax_server")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("ax_server binary not found")
}
