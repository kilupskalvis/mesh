package orchestrator

import (
	"sort"
	"time"

	"github.com/kalvis/mesh/internal/model"
)

// SelectCandidates filters and sorts issues for dispatch.
func (o *Orchestrator) SelectCandidates(issues []model.Issue) []model.Issue {
	var candidates []model.Issue

	for _, issue := range issues {
		if !o.isEligible(issue) {
			continue
		}
		candidates = append(candidates, issue)
	}

	// Sort: priority ASC (nil last), createdAt ASC, identifier ASC.
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		return issueDispatchLess(a, b)
	})

	return candidates
}

// issueDispatchLess returns true if a should be dispatched before b.
func issueDispatchLess(a, b model.Issue) bool {
	// Primary: priority ASC (nil sorts last).
	aPri := priorityRank(a.Priority)
	bPri := priorityRank(b.Priority)
	if aPri != bPri {
		return aPri < bPri
	}

	// Secondary: createdAt ASC.
	aTime := timeRank(a.CreatedAt)
	bTime := timeRank(b.CreatedAt)
	if !aTime.Equal(bTime) {
		return aTime.Before(bTime)
	}

	// Tertiary: identifier ASC.
	return a.Identifier < b.Identifier
}

// priorityRank returns a comparable rank for sorting. Nil priority sorts last (max int).
func priorityRank(p *int) int {
	if p == nil {
		return 1<<31 - 1 // max int31
	}
	return *p
}

// timeRank returns a time suitable for comparison. Nil times use the zero value.
func timeRank(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// isEligible checks whether an issue can be dispatched.
func (o *Orchestrator) isEligible(issue model.Issue) bool {
	// Must have required fields.
	if !issue.HasRequiredFields() {
		return false
	}

	// Must not be already running.
	if _, ok := o.running[issue.ID]; ok {
		return false
	}

	// Must not be already claimed.
	if o.claimed[issue.ID] {
		return false
	}

	// Note: completed set is bookkeeping only, not used for dispatch gating.

	// Must not be in retry queue.
	if _, ok := o.retryQueue[issue.ID]; ok {
		return false
	}

	// "To Do" issues must not have non-terminal blockers.
	if model.NormalizeState(issue.State) == "to do" && !o.blockersCleared(issue) {
		return false
	}

	return true
}

// blockersCleared returns true if all blockers are in terminal states.
func (o *Orchestrator) blockersCleared(issue model.Issue) bool {
	for _, blocker := range issue.BlockedBy {
		if !blocker.IsTerminal(o.config.TerminalStates) {
			return false
		}
	}
	return true
}

// hasSlot returns true if global and per-state concurrency limits allow dispatch.
func (o *Orchestrator) hasSlot(state string) bool {
	// Global limit: spec says available_slots = max(max_concurrent_agents - running_count, 0).
	if len(o.running) >= o.config.MaxConcurrentAgents {
		return false
	}

	// Per-state limit.
	normalizedState := model.NormalizeState(state)
	limit, hasLimit := o.config.MaxConcurrentByState[normalizedState]
	if !hasLimit {
		return true
	}

	stateCount := 0
	for _, entry := range o.running {
		if model.NormalizeState(entry.Issue.State) == normalizedState {
			stateCount++
		}
	}

	return stateCount < limit
}
