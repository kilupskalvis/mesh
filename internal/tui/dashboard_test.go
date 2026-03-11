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
	// Should not panic and should have zero-value snapshot.
	assert.NotNil(t, m)
	assert.Equal(t, 0, m.snapshot.Completed)
	assert.Empty(t, m.snapshot.Running)
}

func TestView_EmptySnapshot(t *testing.T) {
	m := Model{snapshot: Snapshot{}}
	m.width = 80
	m.height = 24
	output := m.View()
	assert.Contains(t, output, "Mesh")
	assert.Contains(t, output, "No running agents")
}

func TestView_WithRunningAgents(t *testing.T) {
	now := time.Now()
	m := Model{
		snapshot: Snapshot{
			Running: []model.RunningEntry{
				{
					Identifier:       "PROJ-101",
					StartedAt:        now.Add(-2 * time.Minute),
					LastAgentEvent:   "tool_use",
					AgentTotalTokens: 5000,
				},
				{
					Identifier:       "PROJ-202",
					StartedAt:        now.Add(-5 * time.Minute),
					LastAgentEvent:   "thinking",
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
	assert.Contains(t, output, "Event: tool_use")
	assert.Contains(t, output, "Event: thinking")
	// Should NOT show "No running agents" when there are running agents.
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
