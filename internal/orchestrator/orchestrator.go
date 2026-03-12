// Package orchestrator implements the core poll loop, dispatch, reconciliation,
// and retry logic for the Symphony service.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kalvis/mesh/internal/config"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/runner"
	"github.com/kalvis/mesh/internal/workspace"
)

// TrackerClient is the interface the orchestrator uses for issue fetching.
type TrackerClient interface {
	FetchCandidateIssues(activeStates []string) ([]model.Issue, error)
	FetchIssuesByStates(stateNames []string) ([]model.Issue, error)
	FetchIssueStatesByIDs(issueIDs []string) ([]model.Issue, error)
}

// ErrorReporter is an interface for reporting errors to an external service (e.g. Sentry).
type ErrorReporter interface {
	CaptureError(err error, tags map[string]string)
}

// StateSnapshot is a point-in-time copy of orchestrator state for the TUI.
type StateSnapshot struct {
	Running          []model.RunningEntry
	RetryQueue       []model.RetryEntry
	Completed        int
	CompletedHistory []model.CompletedEntry
	ActivityLog      []model.LogEntry
	AgentTotals      model.AgentTotals
	RateLimits       *model.RateLimitSnapshot
}

// Orchestrator owns all in-memory scheduling state. All mutations happen in a
// single goroutine (the poll loop), so no mutexes are needed.
type Orchestrator struct {
	// Dependencies (injected)
	tracker            TrackerClient
	runner             runner.Runner
	workspace          *workspace.Manager
	config             *config.ServiceConfig
	promptTmpl         string
	logger             *slog.Logger
	errorReporter      ErrorReporter
	githubTokenProvider func() (string, error) // optional; set for github tracker kind

	// State
	running          map[string]*model.RunningEntry // issue_id -> running entry
	claimed          map[string]bool                // issue_id -> true
	retryQueue       map[string]*model.RetryEntry   // issue_id -> retry entry
	completed        map[string]bool                // issue_id -> true (bookkeeping only)
	completedHistory []model.CompletedEntry         // most recent first, capped
	activityLog      []model.LogEntry               // most recent last, capped
	agentTotals      model.AgentTotals
	agentRateLimits  *model.RateLimitSnapshot

	// Channels
	stopCh    chan struct{}
	refreshCh chan struct{} // triggers immediate poll+reconciliation

	// snapshotReqCh is used to safely read state from another goroutine.
	snapshotReqCh chan chan StateSnapshot

	// completionCh receives worker completion notifications.
	completionCh chan completionMsg

	// eventUpdateCh receives live event updates from worker goroutines.
	eventUpdateCh chan eventUpdateMsg

	// configReloadCh receives new config from the file watcher.
	configReloadCh chan configReloadMsg
}

// configReloadMsg carries a new ServiceConfig and prompt template to the orchestrator loop.
type configReloadMsg struct {
	Config     *config.ServiceConfig
	PromptTmpl string
}

// New creates an Orchestrator with the given dependencies.
func New(cfg *config.ServiceConfig, promptTmpl string, tracker TrackerClient,
	r runner.Runner, ws *workspace.Manager, logger *slog.Logger, opts ...func(*Orchestrator)) *Orchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Orchestrator{
		tracker:        tracker,
		runner:         r,
		workspace:      ws,
		config:         cfg,
		promptTmpl:     promptTmpl,
		logger:         logger,
		running:        make(map[string]*model.RunningEntry),
		claimed:        make(map[string]bool),
		retryQueue:     make(map[string]*model.RetryEntry),
		completed:      make(map[string]bool),
		stopCh:         make(chan struct{}),
		refreshCh:      make(chan struct{}, 1),
		snapshotReqCh:  make(chan chan StateSnapshot),
		completionCh:   make(chan completionMsg, 64),
		eventUpdateCh:  make(chan eventUpdateMsg, 256),
		configReloadCh: make(chan configReloadMsg, 1),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithErrorReporter returns an option that sets the error reporter for the orchestrator.
func WithErrorReporter(er ErrorReporter) func(*Orchestrator) {
	return func(o *Orchestrator) {
		o.errorReporter = er
	}
}

// WithGitHubTokenProvider returns an option that sets the GitHub token provider.
func WithGitHubTokenProvider(tp func() (string, error)) func(*Orchestrator) {
	return func(o *Orchestrator) {
		o.githubTokenProvider = tp
	}
}

// Start begins the poll loop. It blocks until Stop is called or the context is cancelled.
func (o *Orchestrator) Start(ctx context.Context) error {
	interval := time.Duration(o.config.PollIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run an immediate first tick.
	o.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			o.cleanup()
			return ctx.Err()
		case <-o.stopCh:
			o.cleanup()
			return nil
		case <-ticker.C:
			o.tick(ctx)
		case <-o.refreshCh:
			o.tick(ctx)
		case msg := <-o.completionCh:
			o.handleCompletion(msg)
		case msg := <-o.eventUpdateCh:
			o.handleEventUpdate(msg)
		case replyCh := <-o.snapshotReqCh:
			replyCh <- o.buildSnapshot()
		case msg := <-o.configReloadCh:
			oldInterval := o.config.PollIntervalMs
			o.config = msg.Config
			o.promptTmpl = msg.PromptTmpl
			o.logger.Info("config reloaded",
				"max_concurrent", msg.Config.MaxConcurrentAgents,
				"poll_interval_ms", msg.Config.PollIntervalMs,
			)
			// Reset ticker if poll interval changed.
			if msg.Config.PollIntervalMs != oldInterval {
				newInterval := time.Duration(msg.Config.PollIntervalMs) * time.Millisecond
				if newInterval <= 0 {
					newInterval = 30 * time.Second
				}
				ticker.Reset(newInterval)
				o.logger.Info("poll interval updated", "new_ms", msg.Config.PollIntervalMs)
			}
		}
	}
}

// Stop gracefully shuts down the orchestrator.
func (o *Orchestrator) Stop() {
	select {
	case o.stopCh <- struct{}{}:
	default:
	}
}

// ReloadConfig sends a new config to the orchestrator loop for hot-reload.
// It is safe to call from any goroutine. Non-blocking: if a reload is already
// pending, the new config replaces it.
func (o *Orchestrator) ReloadConfig(cfg *config.ServiceConfig, promptTmpl string) {
	select {
	case o.configReloadCh <- configReloadMsg{Config: cfg, PromptTmpl: promptTmpl}:
	default:
		// Channel full — drain and replace with latest.
		select {
		case <-o.configReloadCh:
		default:
		}
		o.configReloadCh <- configReloadMsg{Config: cfg, PromptTmpl: promptTmpl}
	}
}

// RequestRefresh queues an immediate poll+reconciliation cycle.
// Safe to call from any goroutine. Best-effort: coalesces repeated requests.
func (o *Orchestrator) RequestRefresh() {
	select {
	case o.refreshCh <- struct{}{}:
	default:
		// Already queued — coalesced.
	}
}

// Snapshot returns a copy of the current state for the TUI.
// It is safe to call from any goroutine.
func (o *Orchestrator) Snapshot() StateSnapshot {
	replyCh := make(chan StateSnapshot, 1)
	o.snapshotReqCh <- replyCh
	return <-replyCh
}

const (
	maxCompletedHistory = 50
	maxActivityLog      = 100
)

func (o *Orchestrator) buildSnapshot() StateSnapshot {
	snap := StateSnapshot{
		Running:          make([]model.RunningEntry, 0, len(o.running)),
		RetryQueue:       make([]model.RetryEntry, 0, len(o.retryQueue)),
		Completed:        len(o.completed),
		CompletedHistory: make([]model.CompletedEntry, len(o.completedHistory)),
		ActivityLog:      make([]model.LogEntry, len(o.activityLog)),
		AgentTotals:      o.agentTotals,
		RateLimits:       o.agentRateLimits,
	}
	copy(snap.CompletedHistory, o.completedHistory)
	copy(snap.ActivityLog, o.activityLog)

	// Add active-session elapsed time to the cumulative runtime total.
	now := time.Now()
	for _, entry := range o.running {
		snap.Running = append(snap.Running, *entry)
		snap.AgentTotals.SecondsRunning += now.Sub(entry.StartedAt).Seconds()
	}
	for _, entry := range o.retryQueue {
		snap.RetryQueue = append(snap.RetryQueue, *entry)
	}
	return snap
}

// recordCompletion adds a CompletedEntry to the history (most recent first).
func (o *Orchestrator) recordCompletion(entry model.CompletedEntry) {
	o.completedHistory = append([]model.CompletedEntry{entry}, o.completedHistory...)
	if len(o.completedHistory) > maxCompletedHistory {
		o.completedHistory = o.completedHistory[:maxCompletedHistory]
	}
}

// addLog appends an activity log entry (most recent last).
func (o *Orchestrator) addLog(level, identifier, message string) {
	o.activityLog = append(o.activityLog, model.LogEntry{
		Timestamp:  time.Now(),
		Identifier: identifier,
		Message:    message,
		Level:      level,
	})
	if len(o.activityLog) > maxActivityLog {
		o.activityLog = o.activityLog[len(o.activityLog)-maxActivityLog:]
	}
}

// tick executes one full poll cycle: reconcile, fetch, filter, sort, dispatch.
func (o *Orchestrator) tick(ctx context.Context) {
	// 1. Reconcile running issues against tracker state.
	if err := o.Reconcile(ctx); err != nil {
		o.logger.Error("reconciliation failed", "error", err)
	}

	// 2. Process retries that are due.
	o.ProcessRetries(ctx)

	// 3. Drain any pending completions before fetching.
	o.drainCompletions()

	// Per-tick dispatch validation.
	if err := config.ValidateDispatchConfig(o.config); err != nil {
		o.logger.Error("dispatch validation failed, skipping dispatch", "error", err)
		return
	}

	// 4. Fetch candidate issues.
	issues, err := o.tracker.FetchCandidateIssues(o.config.ActiveStates)
	if err != nil {
		o.logger.Error("failed to fetch candidate issues", "error", err)
		return
	}

	// 5. Filter and sort.
	candidates := o.SelectCandidates(issues)

	// 6. Dispatch eligible candidates.
	for _, issue := range candidates {
		if !o.hasSlot(issue.State) {
			break
		}
		if err := o.DispatchIssue(ctx, issue, nil, false); err != nil {
			o.logger.Error("dispatch failed",
				"issue", issue.Identifier,
				"error", err,
			)
		}
	}
}

// drainCompletions processes any pending completion and event update messages without blocking.
func (o *Orchestrator) drainCompletions() {
	for {
		select {
		case msg := <-o.completionCh:
			o.handleCompletion(msg)
		case msg := <-o.eventUpdateCh:
			o.handleEventUpdate(msg)
		default:
			return
		}
	}
}

// cleanup stops all running containers and cleans up.
func (o *Orchestrator) cleanup() {
	for _, entry := range o.running {
		if entry.CancelFunc != nil {
			entry.CancelFunc()
		}
		if entry.ContainerID != "" {
			if err := o.runner.Stop(entry.ContainerID); err != nil {
				o.logger.Error("failed to stop container during cleanup",
					"container", entry.ContainerID,
					"error", err,
				)
			}
		}
	}
	// Cancel any pending retries.
	for _, entry := range o.retryQueue {
		if entry.CancelFunc != nil {
			entry.CancelFunc()
		}
	}
}

// CleanupTerminalWorkspaces queries the tracker for terminal-state issues and
// removes their worktree directories. Called once at startup. Failure is non-fatal.
func (o *Orchestrator) CleanupTerminalWorkspaces() {
	issues, err := o.tracker.FetchIssuesByStates(o.config.TerminalStates)
	if err != nil {
		o.logger.Warn("startup terminal cleanup: failed to fetch terminal issues", "error", err)
		return
	}
	cleaned := 0
	for _, issue := range issues {
		branch := workspace.BranchName(issue.ID, issue.Title)
		if !o.workspace.WorktreeExists(branch) {
			continue
		}
		if o.config.BeforeRemoveHook != "" {
			wsPath := o.workspace.WorktreePath(branch)
			_ = workspace.RunHook("before_remove", o.config.BeforeRemoveHook, wsPath, o.config.HookTimeoutMs)
		}
		if err := o.workspace.RemoveWorktree(branch); err != nil {
			o.logger.Warn("startup terminal cleanup: failed to remove worktree",
				"issue", issue.Identifier, "error", err)
			continue
		}
		cleaned++
	}
	if cleaned > 0 {
		o.logger.Info("startup terminal cleanup complete", "cleaned", cleaned)
	}
}

// RunningCount returns the number of currently running agents.
func (o *Orchestrator) RunningCount() int {
	return len(o.running)
}

// formatSessionID generates a session ID for an issue run.
func formatSessionID(identifier string, attempt int) string {
	return fmt.Sprintf("%s-attempt-%d-%d", identifier, attempt, time.Now().UnixNano())
}

// reportError sends an error to the configured error reporter (e.g. Sentry) with issue context tags.
func (o *Orchestrator) reportError(err error, entry *model.RunningEntry) {
	if o.errorReporter == nil || err == nil {
		return
	}
	tags := map[string]string{
		"issue_id":         entry.Issue.ID,
		"issue_identifier": entry.Identifier,
		"container_id":     entry.ContainerID,
	}
	o.errorReporter.CaptureError(err, tags)
}
