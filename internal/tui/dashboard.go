package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/runner"
)

// Model is the bubbletea model for the Mesh TUI dashboard.
type Model struct {
	snapshot   Snapshot
	snapshotCh <-chan Snapshot // receives snapshots from orchestrator
	width      int
	height     int
	quitting   bool
}

// snapshotMsg wraps a Snapshot received from the channel.
type snapshotMsg Snapshot

// NewModel creates a TUI model that receives state snapshots via channel.
func NewModel(snapshotCh <-chan Snapshot) Model {
	return Model{
		snapshotCh: snapshotCh,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return m.waitForSnapshot()
}

// waitForSnapshot returns a tea.Cmd that blocks until a snapshot arrives.
func (m Model) waitForSnapshot() tea.Cmd {
	if m.snapshotCh == nil {
		return nil
	}
	ch := m.snapshotCh
	return func() tea.Msg {
		snap, ok := <-ch
		if !ok {
			return tea.Quit()
		}
		return snapshotMsg(snap)
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case snapshotMsg:
		m.snapshot = Snapshot(msg)
		return m, m.waitForSnapshot()
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	var b strings.Builder

	// Styles.
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("111")).
		MarginTop(1)

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// Header.
	b.WriteString(headerStyle.Render("Mesh Dashboard"))
	b.WriteString("\n\n")

	// Running Agents section.
	b.WriteString(sectionStyle.Render("Running Agents"))
	b.WriteString("\n")

	if len(m.snapshot.Running) == 0 {
		b.WriteString(dimStyle.Render("  No running agents"))
		b.WriteString("\n")
	} else {
		// Table header.
		fmt.Fprintf(&b, "  %-24s %-10s %6s %-20s %12s %10s\n",
			"Identifier", "State", "Turns", "Last Event", "Tokens", "Duration")
		fmt.Fprintf(&b, "  %-24s %-10s %6s %-20s %12s %10s\n",
			strings.Repeat("─", 24),
			strings.Repeat("─", 10),
			strings.Repeat("─", 6),
			strings.Repeat("─", 20),
			strings.Repeat("─", 12),
			strings.Repeat("─", 10))

		for _, entry := range m.snapshot.Running {
			dur := time.Since(entry.StartedAt).Truncate(time.Second)
			eventSummary := runner.HumanizeEvent(model.AgentEvent{
				Event:        entry.LastAgentEvent,
				Message:      entry.LastAgentMessage,
				TurnsUsed:    int(entry.TurnCount),
				InputTokens:  entry.AgentInputTokens,
				OutputTokens: entry.AgentOutputTokens,
			})
			fmt.Fprintf(&b, "  %-24s %-10s %6d %-20s %12s %10s\n",
				truncate(entry.Identifier, 24),
				truncate(entry.Issue.State, 10),
				entry.TurnCount,
				truncate(eventSummary, 20),
				formatTokens(entry.AgentTotalTokens),
				dur.String())
		}
	}

	// Retry Queue section.
	if len(m.snapshot.RetryQueue) > 0 {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Retry Queue"))
		b.WriteString("\n")

		fmt.Fprintf(&b, "  %-24s %8s %12s  %s\n",
			"Identifier", "Attempt", "Due In", "Error")
		fmt.Fprintf(&b, "  %-24s %8s %12s  %s\n",
			strings.Repeat("─", 24),
			strings.Repeat("─", 8),
			strings.Repeat("─", 12),
			strings.Repeat("─", 30))

		for _, entry := range m.snapshot.RetryQueue {
			dueAt := time.UnixMilli(entry.DueAtMs)
			dueIn := time.Until(dueAt).Truncate(time.Second)
			dueIn = max(dueIn, 0)
			errStr := ""
			if entry.Error != nil {
				errStr = *entry.Error
			}
			fmt.Fprintf(&b, "  %-24s %8d %12s  %s\n",
				truncate(entry.Identifier, 24),
				entry.Attempt,
				dueIn.String(),
				truncate(errStr, 40))
		}
	}

	// Totals bar.
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Totals"))
	b.WriteString("\n")

	totals := m.snapshot.AgentTotals
	dur := time.Duration(totals.SecondsRunning * float64(time.Second))
	fmt.Fprintf(&b, "  Tokens: %s  |  Running time: %s  |  Completed: %d\n",
		formatTokens(totals.TotalTokens),
		dur.Truncate(time.Second).String(),
		m.snapshot.Completed)

	// Rate limits section.
	if rl := m.snapshot.RateLimits; rl != nil {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Rate Limits"))
		b.WriteString("\n")
		fmt.Fprintf(&b, "  Requests: %d/%d  |  Tokens: %d/%d\n",
			rl.RequestsRemaining, rl.RequestsLimit,
			rl.TokensRemaining, rl.TokensLimit)
	}

	return b.String()
}

// truncate shortens a string to maxLen, adding "…" if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}

// formatTokens formats a token count with comma separators.
func formatTokens(n int64) string {
	if n == 0 {
		return "0"
	}

	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}

	// Insert commas from right to left.
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}
