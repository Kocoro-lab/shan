package mcp

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// HealthState represents the health of an MCP server.
type HealthState int

const (
	StateHealthy      HealthState = iota
	StateDegraded
	StateDisconnected
)

func (s HealthState) String() string {
	switch s {
	case StateHealthy:
		return "healthy"
	case StateDegraded:
		return "degraded"
	case StateDisconnected:
		return "disconnected"
	default:
		return "unknown"
	}
}

// ServerHealth tracks per-server health evidence.
type ServerHealth struct {
	State               HealthState
	Since               time.Time
	LastTransportOK     time.Time
	LastCapabilityOK    time.Time
	LastTransportError  string
	LastCapabilityError string
	ConsecutiveFailures int
}

// ProbeResult is the structured return from a capability probe.
type ProbeResult struct {
	Degraded bool
	Detail   string
}

// ToolCaller is the subset of ClientManager that probes need.
type ToolCaller interface {
	CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, bool, error)
}

// CapabilityProbe tests whether an MCP server's real dependency is usable.
type CapabilityProbe interface {
	Probe(ctx context.Context, caller ToolCaller, serverName string) (ProbeResult, error)
}

// backoffState tracks retry interval progression for a single probe tier.
type backoffState struct {
	interval time.Duration
	baseMin  time.Duration
	baseMax  time.Duration
	dormant  time.Duration
	attempts int
}

func newBackoffState(baseMin, baseMax, dormant time.Duration) *backoffState {
	return &backoffState{
		baseMin: baseMin,
		baseMax: baseMax,
		dormant: dormant,
	}
}

func (b *backoffState) recordFailure() {
	b.attempts++
	base := b.baseMin * time.Duration(1<<(b.attempts-1))
	if base > b.baseMax {
		base = b.dormant
	}
	jitter := time.Duration(float64(base) * (0.8 + 0.4*rand.Float64()))
	b.interval = jitter
}

func (b *backoffState) recordSuccess() {
	b.interval = 0
	b.attempts = 0
}

func (b *backoffState) isDormant() bool {
	return b.interval >= b.dormant
}

// Supervisor monitors MCP server health via periodic transport and capability probes.
type Supervisor struct {
	mu                 sync.Mutex
	mgr                *ClientManager
	servers            map[string]*serverEntry
	probes             map[string]CapabilityProbe
	onChange           func(serverName string, oldState, newState HealthState)
	cancel             context.CancelFunc
	started            bool
	transportInterval  time.Duration
	capabilityInterval time.Duration
	wg                 sync.WaitGroup
}

type serverEntry struct {
	config            MCPServerConfig
	health            ServerHealth
	transportBackoff  *backoffState
	capabilityBackoff *backoffState
	probeNowCh        chan struct{}       // signal channel (buffered size 1)
	waitersMu         sync.Mutex         // protects waiters slice
	waiters           []chan ServerHealth // pending ProbeNow callers
}

// NewSupervisor creates a Supervisor that monitors MCP servers via the given ClientManager.
func NewSupervisor(mgr *ClientManager) *Supervisor {
	return &Supervisor{
		mgr:                mgr,
		servers:            make(map[string]*serverEntry),
		probes:             make(map[string]CapabilityProbe),
		transportInterval:  30 * time.Second,
		capabilityInterval: 60 * time.Second,
	}
}

// RegisterCapabilityProbe associates a capability probe with a server name.
func (s *Supervisor) RegisterCapabilityProbe(serverName string, probe CapabilityProbe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probes[serverName] = probe
}

// SetOnChange registers a callback invoked on health state transitions.
func (s *Supervisor) SetOnChange(fn func(serverName string, oldState, newState HealthState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// HealthStates returns a snapshot of all monitored servers' health.
func (s *Supervisor) HealthStates() map[string]ServerHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]ServerHealth, len(s.servers))
	for name, entry := range s.servers {
		result[name] = entry.health
	}
	return result
}

// HealthFor returns the health of a single server, or StateDisconnected if unknown.
func (s *Supervisor) HealthFor(serverName string) ServerHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.servers[serverName]; ok {
		return entry.health
	}
	return ServerHealth{State: StateDisconnected}
}

// ProbeNow requests an immediate probe for a server. Before Start(), returns current health.
// Uses waiter list for coalescing: all concurrent callers get the same result.
func (s *Supervisor) ProbeNow(serverName string) ServerHealth {
	s.mu.Lock()
	if !s.started {
		if entry, ok := s.servers[serverName]; ok {
			h := entry.health
			s.mu.Unlock()
			return h
		}
		s.mu.Unlock()
		return ServerHealth{State: StateDisconnected}
	}
	entry, ok := s.servers[serverName]
	if !ok {
		s.mu.Unlock()
		return ServerHealth{State: StateDisconnected}
	}

	health := entry.health
	inBackoff := entry.transportBackoff.interval > 0 || entry.capabilityBackoff.interval > 0
	stale := time.Since(health.LastTransportOK) > 60*time.Second
	s.mu.Unlock()

	if health.State == StateHealthy && !inBackoff && !stale {
		return health
	}

	respCh := make(chan ServerHealth, 1)
	entry.waitersMu.Lock()
	entry.waiters = append(entry.waiters, respCh)
	entry.waitersMu.Unlock()

	select {
	case entry.probeNowCh <- struct{}{}:
	default:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case h := <-respCh:
		return h
	case <-ctx.Done():
		s.mu.Lock()
		h := entry.health
		s.mu.Unlock()
		return h
	}
}
