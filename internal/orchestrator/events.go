package orchestrator

import (
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
	IssueID        string
	Identifier     string
	Result         runner.RunResult
	Attempt        int
	IsContinuation bool // true if exit was normal (schedule short retry)
	InputTokens    int64
	OutputTokens   int64
	TotalTokens    int64
	Duration       time.Duration
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

// handleCompletion processes a worker completion notification.
func (o *Orchestrator) handleCompletion(msg completionMsg) {
	entry, exists := o.running[msg.IssueID]
	if !exists {
		return
	}

	// Update aggregate totals.
	o.agentTotals.InputTokens += msg.InputTokens - entry.LastReportedInputTokens
	o.agentTotals.OutputTokens += msg.OutputTokens - entry.LastReportedOutputTokens
	o.agentTotals.TotalTokens += msg.TotalTokens - entry.LastReportedTotalTokens
	o.agentTotals.SecondsRunning += msg.Duration.Seconds()

	// Run after_run hook (failure is logged and ignored).
	o.runAfterRunHook(entry.WorkspacePath)

	// Remove from running.
	delete(o.running, msg.IssueID)

	// Bookkeeping only — does not gate dispatch.
	o.completed[msg.IssueID] = true

	// Determine retry strategy.
	if msg.Result.Error == nil || msg.IsContinuation {
		// Normal exit: schedule a short continuation retry.
		o.ScheduleRetry(msg.IssueID, msg.Identifier, 1, true, "")
	} else {
		// Abnormal exit: report to Sentry and schedule exponential backoff retry.
		o.reportError(msg.Result.Error, entry)
		errMsg := msg.Result.Error.Error()
		nextAttempt := msg.Attempt + 1
		o.ScheduleRetry(msg.IssueID, msg.Identifier, nextAttempt, false, errMsg)
	}
}
