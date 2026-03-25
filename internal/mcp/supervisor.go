package mcp

import (
	"context"
	"math/rand"
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
