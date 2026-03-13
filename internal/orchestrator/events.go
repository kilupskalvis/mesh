package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/runner"
)

// eventUpdateMsg is sent from monitorWorker goroutines to update RunningEntry fields.
type eventUpdateMsg struct {
	IssueID string
	Event   model.AgentEvent
}

// completionMsg is sent from worker goroutines back to the orchestrator loop.
type completionMsg struct {
	IssueID      string
	Identifier   string
	Result       runner.RunResult
	Attempt      int
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	Duration     time.Duration
}

// handleEventUpdate applies a live event update to a running entry.
func (o *Orchestrator) handleEventUpdate(msg eventUpdateMsg) {
	entry, exists := o.running[msg.IssueID]
	if !exists {
		return
	}

	ev := msg.Event
	entry.LastAgentEvent = ev.Event

	if ev.Message != "" {
		m := ev.Message
		if len(m) > 200 {
			m = m[:200]
		}
		entry.LastAgentMessage = m
	}

	if ev.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, ev.Timestamp); err == nil {
			entry.LastAgentTimestamp = &t
		}
	}

	if ev.InputTokens > 0 {
		entry.AgentInputTokens = ev.InputTokens
	}
	if ev.OutputTokens > 0 {
		entry.AgentOutputTokens = ev.OutputTokens
	}
	if ev.TotalTokens > 0 {
		entry.AgentTotalTokens = ev.TotalTokens
	}
	if ev.Event == "turn_start" || ev.Event == "turn_started" {
		entry.TurnCount++
	}

	// Log key events to the activity feed.
	switch ev.Event {
	case "session_started", "turn_started", "completed", "error", "notification":
		humanized := runner.HumanizeEvent(ev)
		level := "info"
		if ev.Event == "error" {
			level = "error"
		}
		o.addLog(level, entry.Identifier, humanized)
	}

	// Track latest rate-limit snapshot from agent events.
	if ev.RateLimits != nil {
		rl := &model.RateLimitSnapshot{}
		if v, ok := ev.RateLimits["requests_limit"]; ok {
			if n, ok := v.(float64); ok {
				rl.RequestsLimit = int(n)
			}
		}
		if v, ok := ev.RateLimits["requests_remaining"]; ok {
			if n, ok := v.(float64); ok {
				rl.RequestsRemaining = int(n)
			}
		}
		if v, ok := ev.RateLimits["requests_reset"]; ok {
			if s, ok := v.(string); ok {
				rl.RequestsReset = s
			}
		}
		if v, ok := ev.RateLimits["tokens_limit"]; ok {
			if n, ok := v.(float64); ok {
				rl.TokensLimit = int(n)
			}
		}
		if v, ok := ev.RateLimits["tokens_remaining"]; ok {
			if n, ok := v.(float64); ok {
				rl.TokensRemaining = int(n)
			}
		}
		if v, ok := ev.RateLimits["tokens_reset"]; ok {
			if s, ok := v.(string); ok {
				rl.TokensReset = s
			}
		}
		o.agentRateLimits = rl
	}
}

// handleCompletion processes a worker completion notification using the label-based
// state machine. Labels are the source of truth for what happened during the run.
func (o *Orchestrator) handleCompletion(msg completionMsg) {
	entry, exists := o.running[msg.IssueID]
	if !exists {
		return
	}

	// Read cumulative counters from the RunningEntry.
	errorRetries := entry.ErrorRetries
	continuationCount := entry.ContinuationCount

	// Update aggregate totals.
	o.agentTotals.InputTokens += msg.InputTokens - entry.LastReportedInputTokens
	o.agentTotals.OutputTokens += msg.OutputTokens - entry.LastReportedOutputTokens
	o.agentTotals.TotalTokens += msg.TotalTokens - entry.LastReportedTotalTokens
	o.agentTotals.SecondsRunning += msg.Duration.Seconds()

	// Snapshot for retry/completion records.
	issueSnapshot := entry.Issue

	// Run after_run hook (failure is logged and ignored).
	o.runAfterRunHook(entry.WorkspacePath)

	// Remove from running.
	delete(o.running, msg.IssueID)

	// Try to read labels from tracker (10s HTTP timeout on the client).
	labels, err := o.tracker.GetLabels(msg.IssueID)
	if err != nil {
		// GetLabels failed — treat as error retry.
		o.logger.Error("handleCompletion: failed to read labels, treating as error retry",
			"issue", msg.Identifier, "error", err)
		errorRetries++
		if errorRetries >= o.config.MaxErrorRetries {
			o.handleMaxRetriesExceeded(msg, issueSnapshot, errorRetries, continuationCount, "label read failure")
			return
		}
		nextAttempt := msg.Attempt + 1
		o.ScheduleRetry(msg.IssueID, msg.Identifier, nextAttempt, false,
			fmt.Sprintf("GetLabels failed: %v", err), errorRetries, continuationCount, issueSnapshot)
		return
	}

	// Determine which mesh label is present.
	meshLabel := findMeshLabel(labels)
	o.logger.Info("handleCompletion: resolved state",
		"issue", msg.Identifier,
		"labels", labels,
		"meshLabel", meshLabel,
		"exitCode", msg.Result.ExitCode,
		"errorRetries", errorRetries,
		"continuationCount", continuationCount,
	)

	switch meshLabel {
	case "mesh-review":
		// Agent completed successfully — PR created.
		o.recordCompletion(model.CompletedEntry{
			Identifier:  msg.Identifier,
			Title:       issueSnapshot.Title,
			Status:      "success",
			TotalTokens: msg.TotalTokens,
			Duration:    msg.Duration,
			CompletedAt: time.Now(),
			Attempt:     msg.Attempt,
		})
		o.addLog("info", msg.Identifier, fmt.Sprintf("Completed (success, %d tokens, %s)", msg.TotalTokens, msg.Duration.Truncate(time.Second)))

	case "mesh-failed":
		// Agent declared failure.
		o.recordCompletion(model.CompletedEntry{
			Identifier:  msg.Identifier,
			Title:       issueSnapshot.Title,
			Status:      "error",
			Error:       "agent declared failure (mesh-failed)",
			TotalTokens: msg.TotalTokens,
			Duration:    msg.Duration,
			CompletedAt: time.Now(),
			Attempt:     msg.Attempt,
		})
		o.addLog("warn", msg.Identifier, "Agent declared failure (mesh-failed)")

	case "mesh-working":
		// Agent exited without transitioning labels — determine retry strategy.
		if msg.Result.ExitCode == 0 {
			// Clean exit — needs more turns (continuation).
			continuationCount++
			if continuationCount >= o.config.MaxContinuations {
				o.handleMaxRetriesExceeded(msg, issueSnapshot, errorRetries, continuationCount, "max continuations exceeded")
				return
			}
			o.ScheduleRetry(msg.IssueID, msg.Identifier, 1, true, "", errorRetries, continuationCount, issueSnapshot)
			o.addLog("info", msg.Identifier, fmt.Sprintf("Scheduling continuation %d/%d", continuationCount, o.config.MaxContinuations))
		} else {
			// Crash or error.
			errorRetries++
			errMsg := ""
			if msg.Result.Error != nil {
				errMsg = msg.Result.Error.Error()
				o.reportError(msg.Result.Error, entry)
			}
			if errorRetries >= o.config.MaxErrorRetries {
				o.handleMaxRetriesExceeded(msg, issueSnapshot, errorRetries, continuationCount, errMsg)
				return
			}
			nextAttempt := msg.Attempt + 1
			o.ScheduleRetry(msg.IssueID, msg.Identifier, nextAttempt, false, errMsg, errorRetries, continuationCount, issueSnapshot)
			o.addLog("error", msg.Identifier, fmt.Sprintf("Error retry %d/%d: %s", errorRetries, o.config.MaxErrorRetries, errMsg))
		}

	case "mesh":
		// Race condition or external relabel — log warning, do nothing.
		o.logger.Warn("handleCompletion: issue has mesh label (unexpected during completion)",
			"issue", msg.Identifier)
		o.addLog("warn", msg.Identifier, "Unexpected: mesh label present at completion")

	default:
		// No mesh-prefixed labels — human removed them. Record completion.
		o.recordCompletion(model.CompletedEntry{
			Identifier:  msg.Identifier,
			Title:       issueSnapshot.Title,
			Status:      "cancelled",
			TotalTokens: msg.TotalTokens,
			Duration:    msg.Duration,
			CompletedAt: time.Now(),
			Attempt:     msg.Attempt,
		})
		o.addLog("info", msg.Identifier, "Labels removed — treating as cancelled")
	}
}

// handleMaxRetriesExceeded sets the issue label to mesh-failed and posts a comment.
func (o *Orchestrator) handleMaxRetriesExceeded(msg completionMsg, issue model.Issue, errorRetries, continuationCount int, reason string) {
	if err := o.tracker.SetLabels(msg.IssueID, []string{"mesh-failed"}); err != nil {
		o.logger.Error("handleCompletion: failed to set mesh-failed label",
			"issue", msg.Identifier, "error", err)
	}

	comment := fmt.Sprintf("Mesh agent gave up after %d error retries and %d continuations.\nReason: %s",
		errorRetries, continuationCount, reason)
	if err := o.tracker.PostComment(msg.IssueID, comment); err != nil {
		o.logger.Error("handleCompletion: failed to post max-retry comment",
			"issue", msg.Identifier, "error", err)
	}

	o.recordCompletion(model.CompletedEntry{
		Identifier:  msg.Identifier,
		Title:       issue.Title,
		Status:      "error",
		Error:       reason,
		TotalTokens: msg.TotalTokens,
		Duration:    msg.Duration,
		CompletedAt: time.Now(),
		Attempt:     msg.Attempt,
	})
	o.addLog("error", msg.Identifier, fmt.Sprintf("Max retries exceeded: %s", reason))
}

// findMeshLabel returns the first mesh-prefixed label found, or "" if none.
// Priority: mesh-review > mesh-failed > mesh-working > mesh.
func findMeshLabel(labels []string) string {
	var found string
	for _, l := range labels {
		lower := strings.ToLower(l)
		switch lower {
		case "mesh-review":
			return "mesh-review" // highest priority
		case "mesh-failed":
			found = "mesh-failed"
		case "mesh-working":
			if found == "" || found == "mesh" {
				found = "mesh-working"
			}
		case "mesh":
			if found == "" {
				found = "mesh"
			}
		}
	}
	return found
}
