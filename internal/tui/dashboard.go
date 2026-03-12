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

// section identifies a scrollable section of the dashboard.
type section int

const (
	sectionRunning section = iota
	sectionCompleted
	sectionActivity
	sectionRetry
	sectionCount // sentinel
)

// Model is the bubbletea model for the Mesh TUI dashboard.
type Model struct {
	snapshot   Snapshot
	snapshotCh <-chan Snapshot // receives snapshots from orchestrator
	width      int
	height     int
	quitting   bool

	focus     section
	scrollPos [sectionCount]int
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
		case "tab":
			m.focus = (m.focus + 1) % sectionCount
		case "shift+tab":
			m.focus = (m.focus - 1 + sectionCount) % sectionCount
		case "1":
			m.focus = sectionRunning
		case "2":
			m.focus = sectionCompleted
		case "3":
			m.focus = sectionActivity
		case "4":
			m.focus = sectionRetry
		case "j", "down":
			m.scrollDown()
		case "k", "up":
			m.scrollUp()
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

func (m *Model) scrollDown() {
	maxOffset := m.maxScroll(m.focus)
	if m.scrollPos[m.focus] < maxOffset {
		m.scrollPos[m.focus]++
	}
}

func (m *Model) scrollUp() {
	if m.scrollPos[m.focus] > 0 {
		m.scrollPos[m.focus]--
	}
}

func (m Model) maxScroll(s section) int {
	var count int
	switch s {
	case sectionRunning:
		count = len(m.snapshot.Running)
	case sectionCompleted:
		count = len(m.snapshot.CompletedHistory)
	case sectionActivity:
		count = len(m.snapshot.ActivityLog)
	case sectionRetry:
		count = len(m.snapshot.RetryQueue)
	}
	viewport := m.viewportSize(s)
	if count <= viewport {
		return 0
	}
	return count - viewport
}

func (m Model) viewportSize(s section) int {
	switch s {
	case sectionRunning:
		return 8
	case sectionCompleted:
		return 5
	case sectionActivity:
		return 10
	case sectionRetry:
		return 4
	}
	return 5
}

// -- Styles --

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("111"))

	focusedSectionStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("111")).
				Underline(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	cyanStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("6"))

	greenStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("2"))

	redStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1"))

	yellowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3"))

	msgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)

	sepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("236"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))
)

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	var b strings.Builder

	// Header.
	now := time.Now().Format("15:04:05")
	b.WriteString(titleStyle.Render("Mesh Dashboard"))
	b.WriteString(dimStyle.Render("  " + now))
	b.WriteString("\n\n")

	// Running Agents section.
	m.renderRunning(&b)

	// Completed section.
	m.renderCompleted(&b)

	// Activity feed.
	m.renderActivity(&b)

	// Retry Queue.
	m.renderRetry(&b)

	// Totals bar.
	m.renderTotals(&b)

	// Help bar.
	b.WriteString(helpStyle.Render("j/k scroll · tab section · 1-4 jump · q quit"))
	b.WriteString("\n")

	return b.String()
}

func (m Model) sectionHeader(s section, title string, count int) string {
	style := sectionStyle
	if m.focus == s {
		style = focusedSectionStyle
	}
	label := title
	if count > 0 {
		label = fmt.Sprintf("%s (%d)", title, count)
	}
	return style.Render(label)
}

func (m Model) renderRunning(b *strings.Builder) {
	b.WriteString(m.sectionHeader(sectionRunning, "Running Agents", len(m.snapshot.Running)))
	b.WriteString("\n")

	if len(m.snapshot.Running) == 0 {
		b.WriteString(dimStyle.Render("  No running agents"))
		b.WriteString("\n\n")
		return
	}

	b.WriteString(sepStyle.Render("  " + strings.Repeat("─", 78)))
	b.WriteString("\n")

	offset := m.scrollPos[sectionRunning]
	viewport := m.viewportSize(sectionRunning)
	entries := m.snapshot.Running
	if offset > len(entries) {
		offset = 0
	}
	end := offset + viewport
	if end > len(entries) {
		end = len(entries)
	}

	for _, entry := range entries[offset:end] {
		dur := time.Since(entry.StartedAt).Truncate(time.Second)
		eventSummary := runner.HumanizeEvent(model.AgentEvent{
			Event:        entry.LastAgentEvent,
			Message:      entry.LastAgentMessage,
			TurnsUsed:    int(entry.TurnCount),
			InputTokens:  entry.AgentInputTokens,
			OutputTokens: entry.AgentOutputTokens,
		})

		// Line 1: indicator + identifier + title + turn + tokens + duration
		indicator := cyanStyle.Render("▸")
		ident := cyanStyle.Render(fmt.Sprintf("%-12s", truncate(entry.Identifier, 12)))
		title := truncate(entry.Issue.Title, 28)
		fmt.Fprintf(b, "  %s %s %-28s  Turn %-3d %10s  %8s\n",
			indicator,
			ident,
			title,
			entry.TurnCount,
			formatTokens(entry.AgentTotalTokens)+" tok",
			dur.String())

		// Line 2: last event/message
		msgText := truncate(eventSummary, 70)
		fmt.Fprintf(b, "  %s\n", msgStyle.Render("  "+strings.Repeat(" ", 12)+" "+msgText))
	}

	if len(entries) > viewport {
		shown := fmt.Sprintf("  showing %d-%d of %d", offset+1, end, len(entries))
		b.WriteString(dimStyle.Render(shown))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func (m Model) renderCompleted(b *strings.Builder) {
	history := m.snapshot.CompletedHistory
	if len(history) == 0 {
		return
	}

	b.WriteString(m.sectionHeader(sectionCompleted, "Completed", len(history)))
	b.WriteString("\n")
	b.WriteString(sepStyle.Render("  " + strings.Repeat("─", 78)))
	b.WriteString("\n")

	offset := m.scrollPos[sectionCompleted]
	viewport := m.viewportSize(sectionCompleted)
	if offset > len(history) {
		offset = 0
	}
	end := offset + viewport
	if end > len(history) {
		end = len(history)
	}

	for _, entry := range history[offset:end] {
		var indicator string
		switch entry.Status {
		case "success":
			indicator = greenStyle.Render("✓")
		case "error":
			indicator = redStyle.Render("✗")
		case "cancelled":
			indicator = dimStyle.Render("⊘")
		default:
			indicator = dimStyle.Render("·")
		}

		dur := entry.Duration.Truncate(time.Second)
		ago := timeAgo(entry.CompletedAt)
		title := truncate(entry.Title, 24)

		if entry.Status == "error" && entry.Error != "" {
			errMsg := truncate(entry.Error, 30)
			fmt.Fprintf(b, "  %s %-12s %-24s %10s  %8s  %8s  %s\n",
				indicator,
				truncate(entry.Identifier, 12),
				title,
				formatTokens(entry.TotalTokens)+" tok",
				dur.String(),
				ago,
				redStyle.Render(errMsg))
		} else {
			fmt.Fprintf(b, "  %s %-12s %-24s %10s  %8s  %8s\n",
				indicator,
				truncate(entry.Identifier, 12),
				title,
				formatTokens(entry.TotalTokens)+" tok",
				dur.String(),
				ago)
		}
	}

	if len(history) > viewport {
		shown := fmt.Sprintf("  showing %d-%d of %d", offset+1, end, len(history))
		b.WriteString(dimStyle.Render(shown))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func (m Model) renderActivity(b *strings.Builder) {
	log := m.snapshot.ActivityLog
	b.WriteString(m.sectionHeader(sectionActivity, "Activity", 0))
	b.WriteString("\n")

	if len(log) == 0 {
		b.WriteString(dimStyle.Render("  No activity yet"))
		b.WriteString("\n\n")
		return
	}

	b.WriteString(sepStyle.Render("  " + strings.Repeat("─", 78)))
	b.WriteString("\n")

	// Activity log is stored oldest-first. Show most recent at top.
	// Reverse the visible slice.
	viewport := m.viewportSize(sectionActivity)
	offset := m.scrollPos[sectionActivity]

	// We display reversed (newest first), so adjust offset logic.
	total := len(log)
	if total <= viewport {
		offset = 0
	} else if offset > total-viewport {
		offset = total - viewport
	}

	start := total - offset - viewport
	if start < 0 {
		start = 0
	}
	end := total - offset

	for i := end - 1; i >= start; i-- {
		entry := log[i]
		ts := entry.Timestamp.Format("15:04:05")

		var levelColor lipgloss.Style
		switch entry.Level {
		case "error":
			levelColor = redStyle
		case "warn":
			levelColor = yellowStyle
		default:
			levelColor = dimStyle
		}

		msg := truncate(entry.Message, 50)
		fmt.Fprintf(b, "  %s  %-12s  %s\n",
			dimStyle.Render(ts),
			cyanStyle.Render(truncate(entry.Identifier, 12)),
			levelColor.Render(msg))
	}

	if total > viewport {
		shown := fmt.Sprintf("  showing %d of %d entries", min(viewport, total), total)
		b.WriteString(dimStyle.Render(shown))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func (m Model) renderRetry(b *strings.Builder) {
	// Filter out continuation retries — only show error retries.
	var entries []model.RetryEntry
	for _, e := range m.snapshot.RetryQueue {
		if !e.IsContinuation {
			entries = append(entries, e)
		}
	}

	if len(entries) == 0 {
		return
	}

	b.WriteString(m.sectionHeader(sectionRetry, "Retry Queue", len(entries)))
	b.WriteString("\n")
	b.WriteString(sepStyle.Render("  " + strings.Repeat("─", 78)))
	b.WriteString("\n")

	offset := m.scrollPos[sectionRetry]
	viewport := m.viewportSize(sectionRetry)
	if offset > len(entries) {
		offset = 0
	}
	end := offset + viewport
	if end > len(entries) {
		end = len(entries)
	}

	for _, entry := range entries[offset:end] {
		dueAt := time.UnixMilli(entry.DueAtMs)
		dueIn := time.Until(dueAt).Truncate(time.Second)
		if dueIn < 0 {
			dueIn = 0
		}
		errStr := ""
		if entry.Error != nil {
			errStr = truncate(*entry.Error, 40)
		}
		fmt.Fprintf(b, "  %s  %-12s  #%-3d  in %8s  %s\n",
			yellowStyle.Render("↻"),
			yellowStyle.Render(truncate(entry.Identifier, 12)),
			entry.Attempt,
			dueIn.String(),
			dimStyle.Render(errStr))
	}
	b.WriteString("\n")
}

func (m Model) renderTotals(b *strings.Builder) {
	b.WriteString(sepStyle.Render("  " + strings.Repeat("─", 78)))
	b.WriteString("\n")

	totals := m.snapshot.AgentTotals
	dur := time.Duration(totals.SecondsRunning * float64(time.Second))
	fmt.Fprintf(b, "  Tokens: %s  ·  Runtime: %s  ·  Completed: %d\n",
		formatTokens(totals.TotalTokens),
		dur.Truncate(time.Second).String(),
		m.snapshot.Completed)

	if rl := m.snapshot.RateLimits; rl != nil {
		fmt.Fprintf(b, "  Requests: %d/%d  ·  Tokens: %s/%s\n",
			rl.RequestsRemaining, rl.RequestsLimit,
			formatTokens(int64(rl.TokensRemaining)), formatTokens(int64(rl.TokensLimit)))
	}
	b.WriteString("\n")
}

// -- Helpers --

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

// timeAgo returns a human-readable relative time string.
func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}
