package orchestrator

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kalvis/mesh/internal/logging"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/workspace"
)

// Reconcile checks running agents against current tracker state.
// It performs multiple passes:
//   - Part A: Stall detection (kill workers that haven't produced events recently)
//   - Part A2: Turn timeout enforcement
//   - Part B: Tracker state refresh (stop workers whose issues are terminal or out of scope)
//   - Part C: Orphan detection (mesh-working issues with no running container or retry)
//   - Part D: External label change detection
func (o *Orchestrator) Reconcile(ctx context.Context) error {
	// Part A: Stall detection.
	o.detectStalls()

	// Part A2: Turn timeout enforcement.
	o.detectTurnTimeouts()

	// Part B: Tracker state refresh (only if containers running).
	if len(o.running) > 0 {
		if err := o.reconcileTrackerState(ctx); err != nil {
			o.logger.Error("tracker state reconciliation failed", "error", err)
		}
	}

	// Part C: Orphan detection.
	o.detectOrphans()

	// Part D: External label change detection.
	o.detectExternalLabelChanges()

	return nil
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

		// Read counters from RunningEntry, increment error retries.
		errorRetries := entry.ErrorRetries + 1
		continuationCount := entry.ContinuationCount
		issueSnapshot := entry.Issue

		delete(o.running, issueID)

		if errorRetries >= o.config.MaxErrorRetries {
			// Max retries — set mesh-failed.
			if err := o.tracker.SetLabels(issueID, []string{"mesh-failed"}); err != nil {
				issueLogger.Error("failed to set mesh-failed after stall max retries", "error", err)
			}
			comment := fmt.Sprintf("Mesh agent gave up after %d error retries (last: stall detected)", errorRetries)
			_ = o.tracker.PostComment(issueID, comment)
			o.recordCompletion(model.CompletedEntry{
				Identifier:  entry.Identifier,
				Title:       issueSnapshot.Title,
				Status:      "error",
				Error:       "stall detected, max retries exceeded",
				TotalTokens: entry.AgentTotalTokens,
				Duration:    time.Since(entry.StartedAt),
				CompletedAt: time.Now(),
			})
			o.addLog("error", entry.Identifier, "Max retries exceeded after stall")
			continue
		}

		attempt := entry.RetryAttempt + 1
		o.ScheduleRetry(issueID, entry.Identifier, attempt, false, "stall detected",
			errorRetries, continuationCount, issueSnapshot)
	}
}

// detectTurnTimeouts checks if any running container has exceeded the total
// wall-clock turn timeout.
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

		// Read counters, increment error retries.
		errorRetries := entry.ErrorRetries + 1
		continuationCount := entry.ContinuationCount
		issueSnapshot := entry.Issue

		delete(o.running, issueID)

		if errorRetries >= o.config.MaxErrorRetries {
			if err := o.tracker.SetLabels(issueID, []string{"mesh-failed"}); err != nil {
				issueLogger.Error("failed to set mesh-failed after timeout max retries", "error", err)
			}
			comment := fmt.Sprintf("Mesh agent gave up after %d error retries (last: turn timeout exceeded)", errorRetries)
			_ = o.tracker.PostComment(issueID, comment)
			o.recordCompletion(model.CompletedEntry{
				Identifier:  entry.Identifier,
				Title:       issueSnapshot.Title,
				Status:      "error",
				Error:       "turn timeout exceeded, max retries exceeded",
				TotalTokens: entry.AgentTotalTokens,
				Duration:    time.Since(entry.StartedAt),
				CompletedAt: time.Now(),
			})
			o.addLog("error", entry.Identifier, "Max retries exceeded after turn timeout")
			continue
		}

		attempt := entry.RetryAttempt + 1
		o.ScheduleRetry(issueID, entry.Identifier, attempt, false, "turn timeout exceeded",
			errorRetries, continuationCount, issueSnapshot)
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
	}

	return nil
}

// detectOrphans finds mesh-working issues with no running container or pending retry
// and rolls them back to mesh.
func (o *Orchestrator) detectOrphans() {
	o.logger.Info("detectOrphans: fetching mesh-working issues")
	issues, err := o.tracker.FetchIssuesByLabel("mesh-working")
	o.logger.Info("detectOrphans: fetch done", "count", len(issues), "err", err)
	if err != nil {
		o.logger.Warn("orphan detection: failed to fetch mesh-working issues", "error", err)
		return
	}

	for _, issue := range issues {
		if _, running := o.running[issue.ID]; running {
			continue
		}
		if _, inRetry := o.retryQueue[issue.ID]; inRetry {
			continue
		}
		// Orphan — roll back to mesh.
		if err := o.tracker.SetLabels(issue.ID, []string{"mesh"}); err != nil {
			o.logger.Warn("orphan detection: failed to roll back label",
				"issue", issue.Identifier, "error", err)
			continue
		}
		o.logger.Info("orphan detection: rolled back to mesh", "issue", issue.Identifier)
		o.addLog("warn", issue.Identifier, "Orphan detected: rolled back to mesh")
	}
}

// detectExternalLabelChanges checks if labels on running issues were changed externally.
func (o *Orchestrator) detectExternalLabelChanges() {
	if len(o.running) == 0 {
		return
	}

	var toStop []string

	for issueID, entry := range o.running {
		labels, err := o.tracker.GetLabels(issueID)
		if err != nil {
			// Can't read labels — skip this check, don't kill the container.
			continue
		}

		meshLabel := findMeshLabel(labels)

		switch meshLabel {
		case "mesh-working":
			// Expected — container is running.
			continue
		case "mesh-review":
			// Agent may be finishing up — leave it running.
			continue
		case "mesh-failed":
			// External system marked as failed — stop container.
			o.logger.Info("external label change: mesh-failed, stopping", "issue", entry.Identifier)
			toStop = append(toStop, issueID)
		case "mesh":
			// Shouldn't happen while running — log warning, stop.
			o.logger.Warn("external label change: rolled back to mesh while running", "issue", entry.Identifier)
			toStop = append(toStop, issueID)
		case "":
			if !hasMeshPrefix(labels) {
				// Human removed all mesh labels — stop container.
				o.logger.Info("external label change: all mesh labels removed, stopping", "issue", entry.Identifier)
				toStop = append(toStop, issueID)
			}
		}
	}

	for _, issueID := range toStop {
		entry, ok := o.running[issueID]
		if !ok {
			continue
		}

		if entry.CancelFunc != nil {
			entry.CancelFunc()
		}
		if entry.ContainerID != "" {
			_ = o.runner.Stop(entry.ContainerID)
		}

		delete(o.running, issueID)

		labels, _ := o.tracker.GetLabels(issueID)
		meshLabel := findMeshLabel(labels)
		status := "cancelled"
		if meshLabel == "mesh-failed" {
			status = "error"
		}

		o.recordCompletion(model.CompletedEntry{
			Identifier:  entry.Identifier,
			Title:       entry.Issue.Title,
			Status:      status,
			TotalTokens: entry.AgentTotalTokens,
			Duration:    time.Since(entry.StartedAt),
			CompletedAt: time.Now(),
		})
		o.addLog("warn", entry.Identifier, fmt.Sprintf("Stopped by external label change (%s)", strings.Join(labels, ", ")))
	}
}
