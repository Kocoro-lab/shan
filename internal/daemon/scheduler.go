package daemon

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
	"github.com/Kocoro-lab/ShanClaw/internal/schedule"
	"github.com/adhocore/gronx"
)

const maxConcurrentSchedules = 5

// Scheduler evaluates cron schedules each minute and fires RunAgent for due entries.
type Scheduler struct {
	manager   *schedule.Manager
	deps      *ServerDeps
	gron      *gronx.Gronx
	mu        sync.Mutex
	lastFired map[string]time.Time // scheduleID -> last fired minute (truncated)
	sem       chan struct{}         // bounded concurrency
}

// NewScheduler creates a Scheduler that evaluates schedules from mgr.
func NewScheduler(mgr *schedule.Manager, deps *ServerDeps) *Scheduler {
	return &Scheduler{
		manager:   mgr,
		deps:      deps,
		gron:      gronx.New(),
		lastFired: make(map[string]time.Time),
		sem:       make(chan struct{}, maxConcurrentSchedules),
	}
}

// Start blocks until ctx is cancelled, evaluating schedules each minute.
func (s *Scheduler) Start(ctx context.Context) {
	// Catch-up: evaluate immediately on startup.
	s.tick(ctx)

	// Align to next wall-clock minute boundary.
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	select {
	case <-time.After(next.Sub(now)):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick evaluates due schedules and fires goroutines for each.
// Non-blocking: if all concurrency slots are full, skip the schedule (log + drop)
// rather than blocking tick and potentially missing the next minute boundary.
func (s *Scheduler) tick(ctx context.Context) {
	due := s.EvaluateDue(time.Now())
	for _, sched := range due {
		select {
		case s.sem <- struct{}{}:
			go func(sc schedule.Schedule) {
				defer func() { <-s.sem }()
				s.runSchedule(ctx, sc)
			}(sched)
		default:
			log.Printf("scheduler: skipping schedule %s (all %d slots busy)", sched.ID, maxConcurrentSchedules)
		}
	}
}

// EvaluateDue returns schedules that are due at the given time.
// Exported for testing.
func (s *Scheduler) EvaluateDue(now time.Time) []schedule.Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()

	schedules, err := s.manager.List()
	if err != nil {
		log.Printf("scheduler: failed to list schedules: %v", err)
		return nil
	}

	// Build set of active IDs for pruning.
	activeIDs := make(map[string]struct{}, len(schedules))
	for _, sc := range schedules {
		activeIDs[sc.ID] = struct{}{}
	}
	// Prune lastFired entries for deleted schedules.
	for id := range s.lastFired {
		if _, ok := activeIDs[id]; !ok {
			delete(s.lastFired, id)
		}
	}

	truncated := now.Truncate(time.Minute)
	var due []schedule.Schedule
	for _, sc := range schedules {
		if !sc.Enabled {
			continue
		}
		isDue, err := s.gron.IsDue(sc.Cron, now)
		if err != nil {
			log.Printf("scheduler: invalid cron %q for schedule %s: %v", sc.Cron, sc.ID, err)
			continue
		}
		if !isDue {
			continue
		}
		// Dedup: skip if already fired this minute.
		if last, ok := s.lastFired[sc.ID]; ok && last.Equal(truncated) {
			continue
		}
		s.lastFired[sc.ID] = truncated
		due = append(due, sc)
	}
	return due
}

// runSchedule fires a single scheduled agent run.
func (s *Scheduler) runSchedule(ctx context.Context, sched schedule.Schedule) {
	req := RunAgentRequest{
		Text:    sched.Prompt,
		Agent:   sched.Agent,
		Source:  ChannelSchedule,
		Channel: ChannelSchedule + "-" + sched.ID,
		Sender:  "scheduler",
		// Named agents resume their single long-lived session.
		// Default agent (no name) gets a fresh session per run.
		NewSession: sched.Agent == "",
	}
	_, err := RunAgent(ctx, s.deps, req, &scheduleHandler{})
	if err != nil {
		log.Printf("scheduler: agent run failed for schedule %s: %v", sched.ID, err)
	}
}

// scheduleHandler is a silent EventHandler for scheduled agent runs.
// Auto-approves all tool calls for unattended execution.
type scheduleHandler struct{}

func (h *scheduleHandler) OnToolCall(name string, args string)                                      {}
func (h *scheduleHandler) OnToolResult(name string, args string, result agent.ToolResult, elapsed time.Duration) {
}
func (h *scheduleHandler) OnText(text string)                                    {}
func (h *scheduleHandler) OnStreamDelta(delta string)                            {}
func (h *scheduleHandler) OnUsage(usage agent.TurnUsage)                         {}
func (h *scheduleHandler) OnCloudAgent(agentID, status, message string)          {}
func (h *scheduleHandler) OnCloudProgress(completed, total int)                  {}
func (h *scheduleHandler) OnCloudPlan(planType, content string, needsReview bool) {}
func (h *scheduleHandler) OnApprovalNeeded(tool string, args string) bool        { return true }
