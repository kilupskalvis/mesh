// Package tui implements the terminal dashboard for Mesh.
package tui

import (
	"github.com/kalvis/mesh/internal/model"
)

// Snapshot is the orchestrator state snapshot consumed by the TUI.
// This mirrors orchestrator.StateSnapshot so the TUI package does not
// depend on the orchestrator package.
type Snapshot struct {
	Running     []model.RunningEntry
	RetryQueue  []model.RetryEntry
	Completed   int
	AgentTotals model.AgentTotals
	RateLimits  *model.RateLimitSnapshot
}
