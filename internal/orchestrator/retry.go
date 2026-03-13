package orchestrator

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/kalvis/mesh/internal/logging"
	"github.com/kalvis/mesh/internal/model"
)

// BackoffMs calculates the exponential backoff delay in milliseconds with jitter.
// Formula: capped = min(baseMs * 2^(attempt-1), maxBackoffMs)
//
//	jitter = random uniform [0, capped * 0.25]
//	delay  = capped + jitter
func BackoffMs(attempt int, baseMs int, maxBackoffMs int) int64 {
	if attempt <= 0 {
		attempt = 1
	}
	if baseMs <= 0 {
		baseMs = 10000
	}
	exponent := attempt - 1
	if exponent > 30 {
		exponent = 30 // prevent overflow
	}
	raw := int64(baseMs) * int64(math.Pow(2, float64(exponent)))
	if raw < 0 {
		raw = int64(maxBackoffMs)
	}
	capped := min(raw, int64(maxBackoffMs))

	// Add 0-25% jitter on top of the capped delay.
	jitter := int64(float64(capped) * 0.25 * rand.Float64())
	return capped + jitter
}

// ScheduleRetry schedules a retry for a failed or completed issue.
// Continuation retries (turn limit reached, normal exit) use a 1s delay.
// Error retries use exponential backoff with jitter.
// Counters (errorRetries, continuationCount) and the Issue snapshot are carried through.
func (o *Orchestrator) ScheduleRetry(issueID, identifier string, attempt int, isContinuation bool, errMsg string, errorRetries, continuationCount int, issue model.Issue) {
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
		delayMs = BackoffMs(attempt, o.config.RetryBackoffBaseMs, o.config.MaxRetryBackoffMs)
	}

	dueAtMs := time.Now().UnixMilli() + delayMs

	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}

	entry := &model.RetryEntry{
		IssueID:           issueID,
		Identifier:        identifier,
		Attempt:           attempt,
		DueAtMs:           dueAtMs,
		Error:             errPtr,
		IsContinuation:    isContinuation,
		ErrorRetries:      errorRetries,
		ContinuationCount: continuationCount,
		Issue:             issue,
	}

	o.retryQueue[issueID] = entry

	issueLogger := logging.WithIssueContext(o.logger, issueID, identifier)
	issueLogger.Info("scheduled retry",
		"attempt", attempt,
		"delay_ms", delayMs,
		"continuation", isContinuation,
		"error_retries", errorRetries,
		"continuation_count", continuationCount,
	)
}

// ProcessRetries checks the retry queue and dispatches retries that are due.
// For each due retry, it checks the current label via GetLabels before dispatching.
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

	for _, retry := range dueRetries {
		retryLogger := logging.WithIssueContext(o.logger, retry.IssueID, retry.Identifier)

		// Check current labels via API.
		labels, err := o.tracker.GetLabels(retry.IssueID)
		if err != nil {
			// GetLabels failed — requeue with short delay.
			retryLogger.Error("retry: failed to check labels, requeuing", "error", err)
			retry.DueAtMs = time.Now().UnixMilli() + 5000
			retry.Error = strPtr(fmt.Sprintf("GetLabels failed: %v", err))
			continue
		}

		meshLabel := findMeshLabel(labels)

		switch meshLabel {
		case "mesh-working":
			// Expected state — proceed with dispatch.

		case "mesh-review", "mesh-failed":
			// Resolved externally — remove from queue.
			retryLogger.Info("retry: issue resolved externally", "label", meshLabel)
			delete(o.retryQueue, retry.IssueID)
			continue

		case "mesh", "":
			// Human intervened or labels removed — remove from queue.
			retryLogger.Info("retry: labels changed externally, removing from queue", "label", meshLabel)
			delete(o.retryQueue, retry.IssueID)
			continue

		default:
			retryLogger.Warn("retry: unexpected mesh label", "label", meshLabel)
			delete(o.retryQueue, retry.IssueID)
			continue
		}

		// Check if we have a slot.
		if !o.hasSlot(retry.Issue.State) {
			retryLogger.Info("retry: no available slots, requeuing")
			retry.DueAtMs = time.Now().UnixMilli() + 5000
			retry.Error = strPtr("no available orchestrator slots")
			continue
		}

		// Remove from retry queue before dispatching.
		delete(o.retryQueue, retry.IssueID)

		// Dispatch via retry path (skips label swap — already mesh-working).
		if err := o.DispatchRetry(ctx, retry); err != nil {
			retryLogger.Error("retry dispatch failed", "error", err)
			// Re-schedule with incremented attempt and counters.
			nextAttempt := retry.Attempt + 1
			errRetries := retry.ErrorRetries + 1
			if errRetries >= o.config.MaxErrorRetries {
				// Max retries — set mesh-failed.
				if err := o.tracker.SetLabels(retry.IssueID, []string{"mesh-failed"}); err != nil {
					retryLogger.Error("failed to set mesh-failed after max retries", "error", err)
				}
				comment := fmt.Sprintf("Mesh agent gave up after %d error retries. Last error: %v",
					errRetries, err)
				_ = o.tracker.PostComment(retry.IssueID, comment)
				o.addLog("error", retry.Identifier, "Max retries exceeded during dispatch")
				continue
			}
			o.ScheduleRetry(retry.IssueID, retry.Identifier, nextAttempt, false, err.Error(),
				errRetries, retry.ContinuationCount, retry.Issue)
		}
	}
}

// hasMeshPrefix returns true if any label starts with "mesh".
func hasMeshPrefix(labels []string) bool {
	for _, l := range labels {
		if strings.HasPrefix(strings.ToLower(l), "mesh") {
			return true
		}
	}
	return false
}

func strPtr(s string) *string { return &s }
