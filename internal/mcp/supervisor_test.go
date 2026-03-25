package mcp

import (
	"testing"
	"time"
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
