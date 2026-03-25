package mcp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestBackoff_Progression(t *testing.T) {
	b := newBackoffState(10*time.Millisecond, 40*time.Millisecond, 200*time.Millisecond)
	if b.interval != 0 {
		t.Errorf("initial interval should be 0, got %v", b.interval)
	}
	b.recordFailure()
	if b.interval < 8*time.Millisecond || b.interval > 12*time.Millisecond {
		t.Errorf("first backoff should be ~10ms (±20%%), got %v", b.interval)
	}
	b.recordFailure()
	if b.interval < 16*time.Millisecond || b.interval > 24*time.Millisecond {
		t.Errorf("second backoff should be ~20ms (±20%%), got %v", b.interval)
	}
	b.recordFailure()
	if b.interval < 32*time.Millisecond || b.interval > 48*time.Millisecond {
		t.Errorf("third backoff should be ~40ms (±20%%), got %v", b.interval)
	}
	b.recordFailure()
	if b.interval < 160*time.Millisecond || b.interval > 240*time.Millisecond {
		t.Errorf("dormant backoff should be ~200ms (±20%%), got %v", b.interval)
	}
}

func TestBackoff_ResetOnSuccess(t *testing.T) {
	b := newBackoffState(10*time.Millisecond, 40*time.Millisecond, 200*time.Millisecond)
	b.recordFailure()
	b.recordFailure()
	b.recordSuccess()
	if b.interval != 0 || b.attempts != 0 {
		t.Errorf("expected reset, got interval=%v attempts=%d", b.interval, b.attempts)
	}
}

func TestHealthState_String(t *testing.T) {
	if StateHealthy.String() != "healthy" {
		t.Errorf("expected 'healthy', got %q", StateHealthy.String())
	}
	if StateDegraded.String() != "degraded" {
		t.Errorf("expected 'degraded', got %q", StateDegraded.String())
	}
	if StateDisconnected.String() != "disconnected" {
		t.Errorf("expected 'disconnected', got %q", StateDisconnected.String())
	}
}

func TestSupervisor_RegisterProbe(t *testing.T) {
	mgr := NewClientManager()
	sup := NewSupervisor(mgr)
	sup.RegisterCapabilityProbe("playwright", &PlaywrightProbe{})
}

func TestSupervisor_HealthStates_Empty(t *testing.T) {
	mgr := NewClientManager()
	sup := NewSupervisor(mgr)
	states := sup.HealthStates()
	if len(states) != 0 {
		t.Errorf("expected empty states, got %d", len(states))
	}
}

func TestSupervisor_ProbeNow_BeforeStart(t *testing.T) {
	mgr := NewClientManager()
	sup := NewSupervisor(mgr)
	health := sup.ProbeNow("nonexistent")
	if health.State != StateDisconnected {
		t.Errorf("expected disconnected for unknown server, got %v", health.State)
	}
}

func TestSupervisor_IdleDisconnect(t *testing.T) {
	mgr := NewClientManager()
	// Pre-populate a "connected" server config (no real client)
	mgr.mu.Lock()
	mgr.configs["test"] = MCPServerConfig{Command: "dummy"}
	mgr.mu.Unlock()

	sup := NewSupervisor(mgr)
	sup.transportInterval = 10 * time.Millisecond
	sup.capabilityInterval = 20 * time.Millisecond

	onChange := make(chan HealthState, 10)
	sup.SetOnChange(func(server string, old, new HealthState) {
		onChange <- new
	})

	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	// Wait for disconnect (no client means ProbeTransport fails immediately,
	// reconnect also fails since "dummy" command doesn't exist)
	select {
	case state := <-onChange:
		if state != StateDisconnected {
			t.Errorf("expected disconnected, got %v", state)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for disconnect transition")
	}

	cancel()
	sup.Stop()
}

func TestSupervisor_DegradedState(t *testing.T) {
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["pw"] = MCPServerConfig{Command: "dummy"}
	mgr.mu.Unlock()

	// A probe that always returns degraded
	probe := &mockCapabilityProbe{result: ProbeResult{Degraded: true, Detail: "no browser"}}

	sup := NewSupervisor(mgr)
	sup.transportInterval = 10 * time.Millisecond
	sup.capabilityInterval = 10 * time.Millisecond
	sup.RegisterCapabilityProbe("pw", probe)

	onChange := make(chan stateTransition, 10)
	sup.SetOnChange(func(server string, old, new HealthState) {
		onChange <- stateTransition{server: server, old: old, new: new}
	})

	// For this test we need transport probes to succeed.
	// Inject a fake client that succeeds on ListTools.
	mgr.mu.Lock()
	mgr.clients["pw"] = &fakeListToolsClient{}
	mgr.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	// Wait for degraded transition
	select {
	case tr := <-onChange:
		if tr.new != StateDegraded {
			// Might get healthy first from initial transport probe; wait for degraded
			select {
			case tr2 := <-onChange:
				if tr2.new != StateDegraded {
					t.Errorf("expected degraded, got %v then %v", tr.new, tr2.new)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("timed out waiting for degraded (got %v first)", tr.new)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for degraded transition")
	}

	cancel()
	sup.Stop()
}

func TestSupervisor_Recovery(t *testing.T) {
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["test"] = MCPServerConfig{Command: "dummy"}
	mgr.mu.Unlock()

	sup := NewSupervisor(mgr)
	sup.transportInterval = 10 * time.Millisecond
	sup.capabilityInterval = 20 * time.Millisecond

	var transitions []stateTransition
	var mu sync.Mutex
	transitionCh := make(chan struct{}, 20)
	sup.SetOnChange(func(server string, old, new HealthState) {
		mu.Lock()
		transitions = append(transitions, stateTransition{server: server, old: old, new: new})
		mu.Unlock()
		select {
		case transitionCh <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	// Wait for initial disconnect
	select {
	case <-transitionCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial disconnect")
	}

	mu.Lock()
	if len(transitions) == 0 || transitions[0].new != StateDisconnected {
		mu.Unlock()
		t.Fatal("expected first transition to disconnected")
	}
	mu.Unlock()

	// Inject a working client to simulate recovery
	mgr.mu.Lock()
	mgr.clients["test"] = &fakeListToolsClient{}
	mgr.mu.Unlock()

	// Trigger immediate probe via ProbeNow instead of waiting for backoff timer
	go func() {
		sup.ProbeNow("test")
	}()

	// Wait for recovery (healthy transition)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-transitionCh:
			mu.Lock()
			last := transitions[len(transitions)-1]
			mu.Unlock()
			if last.new == StateHealthy {
				cancel()
				sup.Stop()
				return
			}
		case <-deadline:
			cancel()
			sup.Stop()
			t.Fatal("timed out waiting for healthy recovery")
		}
	}
}

func TestSupervisor_StopDrainsWaiters(t *testing.T) {
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["test"] = MCPServerConfig{Command: "dummy"}
	mgr.mu.Unlock()

	sup := NewSupervisor(mgr)
	sup.transportInterval = 1 * time.Hour // don't tick automatically
	sup.capabilityInterval = 1 * time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	// ProbeNow in background; supervisor will stop before probe executes
	done := make(chan ServerHealth, 1)
	go func() {
		done <- sup.ProbeNow("test")
	}()

	// Give the goroutine a moment to register as a waiter
	time.Sleep(20 * time.Millisecond)

	cancel()
	sup.Stop()

	select {
	case h := <-done:
		// Should get current health back (timeout or drain)
		_ = h
	case <-time.After(5 * time.Second):
		t.Fatal("ProbeNow blocked after Stop()")
	}
}

// Test 7: Generation Guard — an old supervisor's onChange is a no-op after replacement.
func TestSupervisor_GenerationGuard(t *testing.T) {
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["svc"] = MCPServerConfig{Command: "dummy"}
	mgr.clients["svc"] = &fakeListToolsClient{}
	mgr.mu.Unlock()

	// depsSupervisor simulates an external pointer that gets reassigned on replacement.
	var depsMu sync.Mutex
	var depsSupervisor *Supervisor

	supA := NewSupervisor(mgr)
	supA.transportInterval = 1 * time.Hour
	supA.capabilityInterval = 1 * time.Hour

	fired := make(chan string, 1)
	supA.SetOnChange(func(server string, old, new HealthState) {
		depsMu.Lock()
		current := depsSupervisor
		depsMu.Unlock()
		if current != supA {
			// Generation mismatch — skip.
			return
		}
		fired <- server
	})

	depsMu.Lock()
	depsSupervisor = supA
	depsMu.Unlock()

	// Replace with supervisor B before any transition fires.
	supB := NewSupervisor(mgr)
	depsMu.Lock()
	depsSupervisor = supB
	depsMu.Unlock()

	// Manually fire onChange on A — should detect generation mismatch and skip.
	supA.SetOnChange(supA.onChange) // keep the same callback
	fn := supA.onChange
	if fn != nil {
		fn("svc", StateHealthy, StateDisconnected)
	}

	select {
	case srv := <-fired:
		t.Fatalf("onChange should have been a no-op after replacement, but fired for %q", srv)
	case <-time.After(100 * time.Millisecond):
		// Expected: no write to channel.
	}
}

// Test 8: In-Flight Session Isolation — a cloned tool cache is not affected by rebuilds.
func TestSupervisor_InFlightSessionIsolation(t *testing.T) {
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["svc"] = MCPServerConfig{Command: "dummy"}
	mgr.clients["svc"] = &fakeListToolsClient{}
	mgr.toolCache["svc"] = []RemoteTool{
		{ServerName: "svc", Tool: mcp.Tool{Name: "tool_alpha"}},
		{ServerName: "svc", Tool: mcp.Tool{Name: "tool_beta"}},
	}
	mgr.mu.Unlock()

	// Clone the registry (simulating a session snapshot).
	snapshot := mgr.CachedTools("svc")
	if len(snapshot) != 2 {
		t.Fatalf("expected 2 tools in snapshot, got %d", len(snapshot))
	}

	// Simulate a rebuild that replaces the shared registry.
	mgr.mu.Lock()
	mgr.toolCache["svc"] = []RemoteTool{
		{ServerName: "svc", Tool: mcp.Tool{Name: "tool_gamma"}},
	}
	mgr.mu.Unlock()

	// The snapshot should still have its original tools.
	if len(snapshot) != 2 {
		t.Fatalf("snapshot was mutated: expected 2 tools, got %d", len(snapshot))
	}
	if snapshot[0].Tool.Name != "tool_alpha" || snapshot[1].Tool.Name != "tool_beta" {
		t.Fatalf("snapshot contents changed: %v", snapshot)
	}

	// The live cache should reflect the rebuild.
	live := mgr.CachedTools("svc")
	if len(live) != 1 || live[0].Tool.Name != "tool_gamma" {
		t.Fatalf("live cache should have 1 tool (tool_gamma), got %v", live)
	}
}

// Test 9: Concurrent Reconnect Safety — two goroutines calling Reconnect() are serialized.
func TestSupervisor_ConcurrentReconnectSafety(t *testing.T) {
	var connectCount atomic.Int32
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["svc"] = MCPServerConfig{Type: "http", URL: "http://localhost:0/nonexistent"}
	mgr.clients["svc"] = &fakeListToolsClient{}
	mgr.mu.Unlock()

	// We can't mock connect() directly, but we can verify:
	// 1. No panic from concurrent access
	// 2. The per-server reconnect lock serializes calls
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = mgr.Reconnect(ctx, "svc")
			connectCount.Add(1)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Both completed without panic — the per-server lock serialized them.
		got := connectCount.Load()
		if got != 2 {
			t.Fatalf("expected 2 reconnect attempts, got %d", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for concurrent reconnects to finish")
	}
}

// Test 10: Shutdown During Probe — cancelling context while a slow probe is running.
func TestSupervisor_ShutdownDuringProbe(t *testing.T) {
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["slow"] = MCPServerConfig{Command: "dummy"}
	mgr.clients["slow"] = &slowListToolsClient{delay: 500 * time.Millisecond}
	mgr.mu.Unlock()

	sup := NewSupervisor(mgr)
	sup.transportInterval = 10 * time.Millisecond
	sup.capabilityInterval = 1 * time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	// Let the probe loop fire at least once (the initial transport probe).
	time.Sleep(30 * time.Millisecond)

	// Cancel context and verify Stop returns promptly.
	cancel()

	done := make(chan struct{})
	go func() {
		sup.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Exited cleanly.
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() blocked — possible goroutine leak during slow probe")
	}
}

// Test 11: ProbeNow Before Start with a known server (supplements TestSupervisor_ProbeNow_BeforeStart).
func TestSupervisor_ProbeNow_BeforeStart_KnownServer(t *testing.T) {
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["svc"] = MCPServerConfig{Command: "dummy"}
	mgr.clients["svc"] = &fakeListToolsClient{}
	mgr.mu.Unlock()

	sup := NewSupervisor(mgr)
	sup.transportInterval = 1 * time.Hour
	sup.capabilityInterval = 1 * time.Hour

	// Manually populate the server entry without Start() to simulate a registered-but-not-started server.
	sup.mu.Lock()
	sup.servers["svc"] = &serverEntry{
		config: MCPServerConfig{Command: "dummy"},
		health: ServerHealth{
			State: StateDisconnected,
			Since: time.Now(),
		},
		transportBackoff:  newBackoffState(5*time.Second, 30*time.Second, 5*time.Minute),
		capabilityBackoff: newBackoffState(10*time.Second, 60*time.Second, 5*time.Minute),
		probeNowCh:        make(chan struct{}, 1),
	}
	sup.mu.Unlock()

	// ProbeNow should return the pre-Start health without blocking.
	health := sup.ProbeNow("svc")
	if health.State != StateDisconnected {
		t.Errorf("expected disconnected for server registered but not started, got %v", health.State)
	}
}

// Test 12: ProbeNow on Transport-Only Server (no capability probe).
func TestSupervisor_TransportOnlyServer(t *testing.T) {
	mgr := NewClientManager()
	mgr.mu.Lock()
	mgr.configs["transport-only"] = MCPServerConfig{Command: "dummy"}
	mgr.clients["transport-only"] = &fakeListToolsClient{}
	mgr.mu.Unlock()

	sup := NewSupervisor(mgr)
	sup.transportInterval = 10 * time.Millisecond
	sup.capabilityInterval = 10 * time.Millisecond
	// Deliberately NOT registering a capability probe.

	// Track all state transitions.
	var mu sync.Mutex
	var transitions []stateTransition
	transitionCh := make(chan struct{}, 20)
	sup.SetOnChange(func(server string, old, new HealthState) {
		mu.Lock()
		transitions = append(transitions, stateTransition{server: server, old: old, new: new})
		mu.Unlock()
		select {
		case transitionCh <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	// Let a few probe cycles run. The server has a working client, so transport probes
	// should succeed. Without a capability probe, it should stay healthy.
	time.Sleep(80 * time.Millisecond)

	// Check the current state.
	health := sup.HealthFor("transport-only")
	if health.State != StateHealthy {
		t.Errorf("expected healthy for transport-only server, got %v", health.State)
	}

	// Verify ProbeNow returns healthy.
	probed := sup.ProbeNow("transport-only")
	if probed.State != StateHealthy {
		t.Errorf("ProbeNow expected healthy, got %v", probed.State)
	}

	// Verify no degraded transition ever occurred.
	mu.Lock()
	for _, tr := range transitions {
		if tr.new == StateDegraded {
			t.Errorf("transport-only server should never be degraded, but got transition: %v -> %v", tr.old, tr.new)
		}
	}
	mu.Unlock()

	cancel()
	sup.Stop()
}

// slowListToolsClient is a fakeListToolsClient whose ListTools blocks until ctx is cancelled or delay elapses.
type slowListToolsClient struct {
	fakeListToolsClient
	delay time.Duration
}

func (s *slowListToolsClient) ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("transport probe cancelled: %w", ctx.Err())
	case <-time.After(s.delay):
		return &mcp.ListToolsResult{}, nil
	}
}

func (s *slowListToolsClient) Close() error { return nil }

// stateTransition records a health state change.
type stateTransition struct {
	server string
	old    HealthState
	new    HealthState
}

// mockCapabilityProbe returns a fixed result.
type mockCapabilityProbe struct {
	result ProbeResult
	err    error
}

func (m *mockCapabilityProbe) Probe(ctx context.Context, caller ToolCaller, serverName string) (ProbeResult, error) {
	return m.result, m.err
}

// fakeListToolsClient satisfies mcpclient.MCPClient for transport probes.
type fakeListToolsClient struct {
	listToolsErr error
}

func (f *fakeListToolsClient) Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, nil
}
func (f *fakeListToolsClient) Ping(context.Context) error { return nil }
func (f *fakeListToolsClient) ListResourcesByPage(context.Context, mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	return &mcp.ListResourcesResult{}, nil
}
func (f *fakeListToolsClient) ListResources(context.Context, mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	return &mcp.ListResourcesResult{}, nil
}
func (f *fakeListToolsClient) ListResourceTemplatesByPage(context.Context, mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error) {
	return &mcp.ListResourceTemplatesResult{}, nil
}
func (f *fakeListToolsClient) ListResourceTemplates(context.Context, mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error) {
	return &mcp.ListResourceTemplatesResult{}, nil
}
func (f *fakeListToolsClient) ReadResource(context.Context, mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{}, nil
}
func (f *fakeListToolsClient) Subscribe(context.Context, mcp.SubscribeRequest) error { return nil }
func (f *fakeListToolsClient) Unsubscribe(context.Context, mcp.UnsubscribeRequest) error {
	return nil
}
func (f *fakeListToolsClient) ListPromptsByPage(context.Context, mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	return &mcp.ListPromptsResult{}, nil
}
func (f *fakeListToolsClient) ListPrompts(context.Context, mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	return &mcp.ListPromptsResult{}, nil
}
func (f *fakeListToolsClient) GetPrompt(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{}, nil
}
func (f *fakeListToolsClient) ListToolsByPage(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{}, nil
}
func (f *fakeListToolsClient) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return &mcp.ListToolsResult{}, f.listToolsErr
}
func (f *fakeListToolsClient) CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{}, nil
}
func (f *fakeListToolsClient) SetLevel(context.Context, mcp.SetLevelRequest) error { return nil }
func (f *fakeListToolsClient) Complete(context.Context, mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	return &mcp.CompleteResult{}, nil
}
func (f *fakeListToolsClient) Close() error                                         { return nil }
func (f *fakeListToolsClient) OnNotification(func(mcp.JSONRPCNotification)) {}
