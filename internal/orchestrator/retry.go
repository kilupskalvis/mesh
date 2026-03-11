package orchestrator

import (
	"context"
	"math"
	"time"

	"github.com/kalvis/mesh/internal/logging"
	"github.com/kalvis/mesh/internal/model"
)

// BackoffMs calculates the exponential backoff delay in milliseconds.
// Formula: min(10000 * 2^(attempt-1), maxBackoffMs)
// For attempt <= 0, returns 10000 (the base delay).
func BackoffMs(attempt int, maxBackoffMs int) int64 {
	if attempt <= 0 {
		attempt = 1
	}
	base := int64(10000)
	exponent := attempt - 1
	if exponent > 30 {
		exponent = 30 // prevent overflow
	}
	delay := base * int64(math.Pow(2, float64(exponent)))
	if delay < 0 {
		delay = int64(maxBackoffMs)
	}
	return min(delay, int64(maxBackoffMs))
}

// ScheduleRetry schedules a retry for a failed or completed issue.
// Continuation retries (turn limit reached, normal exit) use a 1s delay.
// Error retries use exponential backoff: min(10000 * 2^(attempt-1), max_retry_backoff_ms).
func (o *Orchestrator) ScheduleRetry(issueID, identifier string, attempt int, isContinuation bool, errMsg string) {
	// Cancel any existing retry for this issue.
	if existing, ok := o.retryQueue[issueID]; ok {
		if existing.CancelFunc != nil {
			existing.CancelFunc()
		}
		delete(o.retryQueue, issueID)
	}

	var delayMs int64
	if isContinuation {
		delayMs = 1000
	} else {
		delayMs = BackoffMs(attempt, o.config.MaxRetryBackoffMs)
	}

	dueAtMs := time.Now().UnixMilli() + delayMs

	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}

	entry := &model.RetryEntry{
		IssueID:    issueID,
		Identifier: identifier,
		Attempt:    attempt,
		DueAtMs:    dueAtMs,
		Error:      errPtr,
	}

	o.retryQueue[issueID] = entry

	issueLogger := logging.WithIssueContext(o.logger, issueID, identifier)
	issueLogger.Info("scheduled retry",
		"attempt", attempt,
		"delay_ms", delayMs,
		"continuation", isContinuation,
	)
}

// ProcessRetries checks the retry queue and dispatches retries that are due.
func (o *Orchestrator) ProcessRetries(ctx context.Context) {
	nowMs := time.Now().UnixMilli()

	// Collect due retries.
	var dueRetries []*model.RetryEntry
	for _, entry := range o.retryQueue {
		if entry.DueAtMs <= nowMs {
			dueRetries = append(dueRetries, entry)
		}
	}

	if len(dueRetries) == 0 {
		return
	}

	// Fetch active candidate issues to check eligibility.
	issues, err := o.tracker.FetchCandidateIssues(o.config.ActiveStates)
	if err != nil {
		o.logger.Error("failed to fetch candidates for retry processing", "error", err)
		// Requeue all due retries with incremented attempt.
		for _, retry := range dueRetries {
			nextAttempt := retry.Attempt + 1
			errMsg := "retry poll failed"
			o.ScheduleRetry(retry.IssueID, retry.Identifier, nextAttempt, false, errMsg)
		}
		return
	}

	// Build lookup by ID.
	issueMap := make(map[string]model.Issue, len(issues))
	for _, issue := range issues {
		issueMap[issue.ID] = issue
	}

	for _, retry := range dueRetries {
		retryLogger := logging.WithIssueContext(o.logger, retry.IssueID, retry.Identifier)

		issue, found := issueMap[retry.IssueID]
		if !found {
			// Issue no longer active — release claim.
			retryLogger.Info("retry: issue no longer active, releasing claim")
			delete(o.retryQueue, retry.IssueID)
			continue
		}

		// Check if we have a slot.
		if !o.hasSlot(issue.State) {
			// No slot — requeue with error.
			retryLogger.Info("retry: no available slots, requeuing")
			errMsg := "no available orchestrator slots"
			retry.Error = &errMsg
			retry.DueAtMs = time.Now().UnixMilli() + 5000 // retry in 5s
			continue
		}

		// Remove from retry queue before dispatching.
		delete(o.retryQueue, retry.IssueID)

		// Dispatch.
		retryAttempt := retry.Attempt
		if err := o.DispatchIssue(ctx, issue, &retryAttempt); err != nil {
			retryLogger.Error("retry dispatch failed", "error", err)
			// Re-schedule with incremented attempt.
			nextAttempt := retry.Attempt + 1
			errStr := err.Error()
			o.ScheduleRetry(retry.IssueID, retry.Identifier, nextAttempt, false, errStr)
		}
	}
}
