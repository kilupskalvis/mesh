package orchestrator

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/kalvis/mesh/internal/logging"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/workspace"
)

// Reconcile checks running agents against current tracker state.
// It performs two passes:
//   - Part A: Stall detection (kill workers that haven't produced events recently)
//   - Part B: Tracker state refresh (stop workers whose issues are terminal or out of scope)
func (o *Orchestrator) Reconcile(ctx context.Context) error {
	if len(o.running) == 0 {
		return nil
	}

	// Part A: Stall detection.
	o.detectStalls()

	// Part A2: Turn timeout enforcement.
	o.detectTurnTimeouts()

	// Part B: Tracker state refresh.
	return o.reconcileTrackerState(ctx)
}

// detectStalls checks each running entry for stall conditions.
func (o *Orchestrator) detectStalls() {
	if o.config.StallTimeoutMs <= 0 {
		return
	}

	stallTimeout := time.Duration(o.config.StallTimeoutMs) * time.Millisecond
	now := time.Now()

	// Collect stalled issue IDs first to avoid mutating the map during iteration.
	var stalled []string

	for issueID, entry := range o.running {
		var referenceTime time.Time
		if entry.LastAgentTimestamp != nil {
			referenceTime = *entry.LastAgentTimestamp
		} else {
			referenceTime = entry.StartedAt
		}

		elapsed := now.Sub(referenceTime)
		if elapsed > stallTimeout {
			stalled = append(stalled, issueID)
		}
	}

	for _, issueID := range stalled {
		entry := o.running[issueID]
		issueLogger := logging.WithIssueContext(o.logger, issueID, entry.Identifier)
		issueLogger.Warn("stall detected, stopping worker",
			"container", entry.ContainerID,
		)

		stallErr := fmt.Errorf("stall detected for %s", entry.Identifier)
		o.reportError(stallErr, entry)

		// Stop the container.
		if entry.CancelFunc != nil {
			entry.CancelFunc()
		}
		if entry.ContainerID != "" {
			_ = o.runner.Stop(entry.ContainerID)
		}

		// Remove from running and schedule retry.
		attempt := entry.RetryAttempt + 1
		delete(o.running, issueID)
		o.ScheduleRetry(issueID, entry.Identifier, attempt, false, "stall detected")
	}
}

// detectTurnTimeouts checks if any running container has exceeded the total
// wall-clock turn timeout. This is distinct from stall detection: stall means
// no events, turn timeout means total elapsed time since launch.
func (o *Orchestrator) detectTurnTimeouts() {
	if o.config.TurnTimeoutMs <= 0 {
		return
	}

	turnTimeout := time.Duration(o.config.TurnTimeoutMs) * time.Millisecond
	now := time.Now()

	var timedOut []string
	for issueID, entry := range o.running {
		if now.Sub(entry.StartedAt) > turnTimeout {
			timedOut = append(timedOut, issueID)
		}
	}

	for _, issueID := range timedOut {
		entry := o.running[issueID]
		issueLogger := logging.WithIssueContext(o.logger, issueID, entry.Identifier)
		issueLogger.Warn("turn timeout exceeded, stopping worker",
			"container", entry.ContainerID,
			"elapsed", time.Since(entry.StartedAt).String(),
		)

		timeoutErr := fmt.Errorf("turn timeout exceeded for %s", entry.Identifier)
		o.reportError(timeoutErr, entry)

		if entry.CancelFunc != nil {
			entry.CancelFunc()
		}
		if entry.ContainerID != "" {
			_ = o.runner.Stop(entry.ContainerID)
		}

		attempt := entry.RetryAttempt + 1
		delete(o.running, issueID)
		o.ScheduleRetry(issueID, entry.Identifier, attempt, false, "turn timeout exceeded")
	}
}

// reconcileTrackerState fetches current states for running issues and stops
// those that are terminal or no longer active.
func (o *Orchestrator) reconcileTrackerState(ctx context.Context) error {
	// Collect running issue IDs.
	issueIDs := make([]string, 0, len(o.running))
	for id := range o.running {
		issueIDs = append(issueIDs, id)
	}

	if len(issueIDs) == 0 {
		return nil
	}

	// Fetch current states from tracker.
	freshIssues, err := o.tracker.FetchIssueStatesByIDs(issueIDs)
	if err != nil {
		return err
	}

	// Build a lookup map.
	freshMap := make(map[string]model.Issue, len(freshIssues))
	for _, issue := range freshIssues {
		freshMap[issue.ID] = issue
	}

	// Normalize active and terminal state lists for comparison.
	activeStates := make([]string, len(o.config.ActiveStates))
	for i, s := range o.config.ActiveStates {
		activeStates[i] = model.NormalizeState(s)
	}
	terminalStates := make([]string, len(o.config.TerminalStates))
	for i, s := range o.config.TerminalStates {
		terminalStates[i] = model.NormalizeState(s)
	}

	// Check each running issue.
	var toStop []struct {
		issueID  string
		terminal bool
		cleanWs  bool
	}

	for issueID, entry := range o.running {
		fresh, found := freshMap[issueID]
		if !found {
			// Issue not found in tracker — stop without cleanup.
			toStop = append(toStop, struct {
				issueID  string
				terminal bool
				cleanWs  bool
			}{issueID, false, false})
			continue
		}

		normalizedState := model.NormalizeState(fresh.State)

		if slices.Contains(terminalStates, normalizedState) {
			// Terminal: stop and clean workspace.
			toStop = append(toStop, struct {
				issueID  string
				terminal bool
				cleanWs  bool
			}{issueID, true, true})
			continue
		}

		if slices.Contains(activeStates, normalizedState) {
			// Still active: update the in-memory snapshot.
			entry.Issue = fresh
			continue
		}

		// Neither active nor terminal: stop without workspace cleanup.
		toStop = append(toStop, struct {
			issueID  string
			terminal bool
			cleanWs  bool
		}{issueID, false, false})
	}

	// Execute stops.
	for _, item := range toStop {
		entry, ok := o.running[item.issueID]
		if !ok {
			continue
		}

		issueLogger := logging.WithIssueContext(o.logger, item.issueID, entry.Identifier)
		issueLogger.Info("reconciliation: stopping worker",
			"terminal", item.terminal,
			"clean_workspace", item.cleanWs,
		)

		if entry.CancelFunc != nil {
			entry.CancelFunc()
		}
		if entry.ContainerID != "" {
			_ = o.runner.Stop(entry.ContainerID)
		}

		if item.cleanWs {
			if entry.BranchName != "" {
				if o.config.BeforeRemoveHook != "" {
					wsPath := o.workspace.WorktreePath(entry.BranchName)
					_ = workspace.RunHook("before_remove", o.config.BeforeRemoveHook, wsPath, o.config.HookTimeoutMs)
				}
				if err := o.workspace.RemoveWorktree(entry.BranchName); err != nil {
					issueLogger.Error("failed to remove worktree", "error", err)
				}
			}
			o.completed[item.issueID] = true
			o.recordCompletion(model.CompletedEntry{
				Identifier:  entry.Identifier,
				Title:       entry.Issue.Title,
				Status:      "cancelled",
				TotalTokens: entry.AgentTotalTokens,
				Duration:    time.Since(entry.StartedAt),
				CompletedAt: time.Now(),
			})
			o.addLog("warn", entry.Identifier, "Cancelled by reconciliation (issue moved to terminal state)")
		}

		delete(o.running, item.issueID)
		// Remove from claimed if present.
		delete(o.claimed, item.issueID)
	}

	return nil
}
