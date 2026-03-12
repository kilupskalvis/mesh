package tui

import (
	"testing"
	"time"

	"github.com/kalvis/mesh/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestNewModel(t *testing.T) {
	ch := make(chan Snapshot, 1)
	m := NewModel(ch)
	assert.NotNil(t, m)
	assert.Equal(t, 0, m.snapshot.Completed)
	assert.Empty(t, m.snapshot.Running)
}

func TestView_EmptySnapshot(t *testing.T) {
	m := Model{snapshot: Snapshot{}, width: 80, height: 24}
	output := m.View()
	assert.Contains(t, output, "Mesh")
	assert.Contains(t, output, "No running agents")
	assert.Contains(t, output, "No activity yet")
}

func TestView_WithRunningAgents(t *testing.T) {
	now := time.Now()
	m := Model{
		snapshot: Snapshot{
			Running: []model.RunningEntry{
				{
					Identifier:       "PROJ-101",
					Issue:            model.Issue{Title: "Add login page"},
					StartedAt:        now.Add(-2 * time.Minute),
					LastAgentEvent:   "turn_started",
					LastAgentMessage: "Reading source files",
					TurnCount:        5,
					AgentTotalTokens: 5000,
				},
				{
					Identifier:       "PROJ-202",
					Issue:            model.Issue{Title: "Fix auth bug"},
					StartedAt:        now.Add(-5 * time.Minute),
					LastAgentEvent:   "turn_started",
					LastAgentMessage: "Creating test file",
					TurnCount:        2,
					AgentTotalTokens: 12000,
				},
			},
		},
		width:  120,
		height: 40,
	}
	output := m.View()
	assert.Contains(t, output, "PROJ-101")
	assert.Contains(t, output, "PROJ-202")
	assert.Contains(t, output, "Add login page")
	assert.Contains(t, output, "Running Agents (2)")
	assert.NotContains(t, output, "No running agents")
}

func TestView_WithRetryQueue(t *testing.T) {
	errMsg := "connection timeout"
	m := Model{
		snapshot: Snapshot{
			RetryQueue: []model.RetryEntry{
				{
					Identifier: "PROJ-303",
					Attempt:    2,
					DueAtMs:    time.Now().Add(30 * time.Second).UnixMilli(),
					Error:      &errMsg,
				},
			},
		},
		width:  120,
		height: 40,
	}
	output := m.View()
	assert.Contains(t, output, "PROJ-303")
	assert.Contains(t, output, "connection timeout")
	assert.Contains(t, output, "Retry Queue")
}

func TestView_WithTotals(t *testing.T) {
	m := Model{
		snapshot: Snapshot{
			Completed: 7,
			AgentTotals: model.AgentTotals{
				InputTokens:    50000,
				OutputTokens:   30000,
				TotalTokens:    80000,
				SecondsRunning: 3661.5,
			},
		},
		width:  120,
		height: 40,
	}
	output := m.View()
	assert.Contains(t, output, "80,000")
	assert.Contains(t, output, "7")
}

func TestView_Quitting(t *testing.T) {
	m := Model{quitting: true, width: 80, height: 24}
	output := m.View()
	assert.Contains(t, output, "Goodbye")
}

func TestView_WithCompletedHistory(t *testing.T) {
	m := Model{
		snapshot: Snapshot{
			Completed: 2,
			CompletedHistory: []model.CompletedEntry{
				{
					Identifier:  "PROJ-50",
					Title:       "Add login page",
					Status:      "success",
					TotalTokens: 45000,
					Duration:    8*time.Minute + 20*time.Second,
					CompletedAt: time.Now().Add(-2 * time.Minute),
				},
				{
					Identifier:  "PROJ-49",
					Title:       "Fix validation",
					Status:      "error",
					Error:       "container exit code 1",
					TotalTokens: 12000,
					Duration:    3*time.Minute + 10*time.Second,
					CompletedAt: time.Now().Add(-5 * time.Minute),
				},
			},
		},
		width:  120,
		height: 40,
	}
	output := m.View()
	assert.Contains(t, output, "Completed (2)")
	assert.Contains(t, output, "PROJ-50")
	assert.Contains(t, output, "PROJ-49")
	assert.Contains(t, output, "Add login page")
}

func TestView_WithActivityLog(t *testing.T) {
	m := Model{
		snapshot: Snapshot{
			ActivityLog: []model.LogEntry{
				{
					Timestamp:  time.Now().Add(-30 * time.Second),
					Identifier: "PROJ-101",
					Message:    "Session started",
					Level:      "info",
				},
				{
					Timestamp:  time.Now().Add(-10 * time.Second),
					Identifier: "PROJ-101",
					Message:    "Turn 1 started",
					Level:      "info",
				},
			},
		},
		width:  120,
		height: 40,
	}
	output := m.View()
	assert.Contains(t, output, "Activity")
	assert.Contains(t, output, "PROJ-101")
	assert.Contains(t, output, "Session started")
	assert.NotContains(t, output, "No activity yet")
}

func TestKeyboardNavigation(t *testing.T) {
	m := Model{snapshot: Snapshot{}, width: 80, height: 24}

	// Tab should cycle focus.
	assert.Equal(t, sectionRunning, m.focus)
	m.focus = (m.focus + 1) % sectionCount
	assert.Equal(t, sectionCompleted, m.focus)
	m.focus = (m.focus + 1) % sectionCount
	assert.Equal(t, sectionActivity, m.focus)

	// Number keys should jump to section.
	m.focus = sectionRetry
	assert.Equal(t, sectionRetry, m.focus)
}

func TestFormatTokens(t *testing.T) {
	assert.Equal(t, "0", formatTokens(0))
	assert.Equal(t, "999", formatTokens(999))
	assert.Equal(t, "1,000", formatTokens(1000))
	assert.Equal(t, "1,000,000", formatTokens(1000000))
}

func TestTimeAgo(t *testing.T) {
	assert.Contains(t, timeAgo(time.Now().Add(-30*time.Second)), "s ago")
	assert.Contains(t, timeAgo(time.Now().Add(-5*time.Minute)), "m ago")
	assert.Contains(t, timeAgo(time.Now().Add(-2*time.Hour)), "h ago")
}
