package orchestrator

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kalvis/mesh/internal/config"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectCandidates_FiltersDuplicates(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(os.TempDir() + "/mesh_test_filter")
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	// Pre-populate state to mark some issues as already handled.
	orch.running["2"] = &model.RunningEntry{
		Identifier: "PROJ-2",
		Issue:      makeIssue("2", "PROJ-2", "Running Issue", "In Progress"),
	}
	orch.claimed["3"] = true
	orch.completed["4"] = true // bookkeeping only, should NOT gate dispatch
	orch.retryQueue["5"] = &model.RetryEntry{IssueID: "5", Identifier: "PROJ-5"}

	issues := []model.Issue{
		makeIssue("1", "PROJ-1", "New Issue", "To Do"),
		makeIssue("2", "PROJ-2", "Running Issue", "In Progress"),
		makeIssue("3", "PROJ-3", "Claimed Issue", "To Do"),
		makeIssue("4", "PROJ-4", "Completed Issue", "To Do"),
		makeIssue("5", "PROJ-5", "Retry Issue", "To Do"),
		makeIssue("6", "PROJ-6", "Another New", "To Do"),
	}

	candidates := orch.SelectCandidates(issues)

	// Issues 1, 4, and 6 should be eligible. Issue 4 is in completed but
	// that's bookkeeping only per spec, not dispatch gating.
	assert.Len(t, candidates, 3)
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ID
	}
	assert.Contains(t, ids, "1")
	assert.Contains(t, ids, "6")
}

func TestSelectCandidates_SortsByPriority(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(os.TempDir() + "/mesh_test_sort")
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	pri1 := 1
	pri2 := 2
	pri3 := 3

	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)

	issues := []model.Issue{
		{ID: "3", Identifier: "PROJ-3", Title: "Low Priority", State: "To Do", Priority: &pri3, CreatedAt: &t1},
		{ID: "1", Identifier: "PROJ-1", Title: "High Priority", State: "To Do", Priority: &pri1, CreatedAt: &t3},
		{ID: "2", Identifier: "PROJ-2", Title: "Medium Priority", State: "To Do", Priority: &pri2, CreatedAt: &t2},
		{ID: "4", Identifier: "PROJ-4", Title: "No Priority", State: "To Do", Priority: nil, CreatedAt: &t1},
	}

	candidates := orch.SelectCandidates(issues)

	require := assert.New(t)
	require.Len(candidates, 4)
	require.Equal("1", candidates[0].ID, "highest priority first")
	require.Equal("2", candidates[1].ID, "medium priority second")
	require.Equal("3", candidates[2].ID, "low priority third")
	require.Equal("4", candidates[3].ID, "nil priority last")
}

func TestSelectCandidates_SortsByCreatedAtThenIdentifier(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(os.TempDir() + "/mesh_test_sort2")
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	pri := 2
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)

	issues := []model.Issue{
		{ID: "3", Identifier: "PROJ-C", Title: "Same Time B", State: "To Do", Priority: &pri, CreatedAt: &t1},
		{ID: "2", Identifier: "PROJ-B", Title: "Later", State: "To Do", Priority: &pri, CreatedAt: &t2},
		{ID: "1", Identifier: "PROJ-A", Title: "Same Time A", State: "To Do", Priority: &pri, CreatedAt: &t1},
	}

	candidates := orch.SelectCandidates(issues)

	assert.Len(t, candidates, 3)
	assert.Equal(t, "PROJ-A", candidates[0].Identifier, "same time, A before C")
	assert.Equal(t, "PROJ-C", candidates[1].Identifier, "same time, C after A")
	assert.Equal(t, "PROJ-B", candidates[2].Identifier, "later time")
}

func TestSelectCandidates_FiltersBlockedIssues(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(os.TempDir() + "/mesh_test_blocked")
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	doneState := "done"
	inProgressState := "in progress"

	issues := []model.Issue{
		{
			ID: "1", Identifier: "PROJ-1", Title: "Unblocked", State: "To Do",
			BlockedBy: []model.BlockerRef{
				{ID: strPtr("10"), Identifier: strPtr("PROJ-10"), State: &doneState},
			},
		},
		{
			ID: "2", Identifier: "PROJ-2", Title: "Blocked", State: "To Do",
			BlockedBy: []model.BlockerRef{
				{ID: strPtr("11"), Identifier: strPtr("PROJ-11"), State: &inProgressState},
			},
		},
		{
			ID: "3", Identifier: "PROJ-3", Title: "No Blockers", State: "To Do",
			BlockedBy: []model.BlockerRef{},
		},
	}

	candidates := orch.SelectCandidates(issues)

	assert.Len(t, candidates, 2)
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ID
	}
	assert.Contains(t, ids, "1")
	assert.Contains(t, ids, "3")
	assert.NotContains(t, ids, "2")
}

func TestSelectCandidates_FiltersMissingRequiredFields(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(os.TempDir() + "/mesh_test_required")
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	issues := []model.Issue{
		{ID: "1", Identifier: "PROJ-1", Title: "Valid", State: "To Do"},
		{ID: "2", Identifier: "", Title: "Missing Identifier", State: "To Do"},
		{ID: "", Identifier: "PROJ-3", Title: "Missing ID", State: "To Do"},
	}

	candidates := orch.SelectCandidates(issues)
	assert.Len(t, candidates, 1)
	assert.Equal(t, "1", candidates[0].ID)
}

func TestDispatchIssue_PopulatesModelAndTerminalStates(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.AgentModel = "claude-opus-4-6"
	cfg.TerminalStates = []string{"done", "cancelled", "duplicate"}

	orch := New(cfg, "Work on {{ issue.title }}", tracker, r, ws, testLogger())

	ctx := context.Background()
	issue := makeIssue("42", "PROJ-42", "Test Issue", "To Do")
	err := orch.DispatchIssue(ctx, issue, nil)
	require.NoError(t, err)

	r.mu.Lock()
	params := r.lastParams
	r.mu.Unlock()

	assert.Equal(t, "claude-opus-4-6", params.StdinPayload.Config.Model)
	assert.Equal(t, []string{"done", "cancelled", "duplicate"}, params.StdinPayload.Config.TerminalStates)
}

func TestDispatchIssue_InjectsProxyURLsNotSecrets(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.ProxyListenPort = 9480
	cfg.TrackerEndpoint = "https://real.atlassian.net"
	cfg.TrackerEmail = "secret@email.com"
	cfg.TrackerAPIToken = "super-secret-token"
	cfg.SentryDSN = "https://sentry.example.com/123"
	cfg.DockerExtraEnv = map[string]string{"CUSTOM_VAR": "custom_value"}

	orch := New(cfg, "Work on {{ issue.title }}", tracker, r, ws, testLogger())

	ctx := context.Background()
	issue := makeIssue("42", "PROJ-42", "Test Issue", "To Do")
	err := orch.DispatchIssue(ctx, issue, nil)
	require.NoError(t, err)

	r.mu.Lock()
	env := r.lastParams.EnvVars
	r.mu.Unlock()

	// Proxy URLs should be injected.
	assert.Equal(t, "http://host.docker.internal:9480", env["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "http://host.docker.internal:9480/jira", env["JIRA_ENDPOINT"])

	// Raw secrets should NOT be present.
	_, hasClaude := env["CLAUDE_API_KEY"]
	assert.False(t, hasClaude, "CLAUDE_API_KEY should not be in container")
	_, hasJiraToken := env["JIRA_API_TOKEN"]
	assert.False(t, hasJiraToken, "JIRA_API_TOKEN should not be in container")
	_, hasJiraEmail := env["JIRA_EMAIL"]
	assert.False(t, hasJiraEmail, "JIRA_EMAIL should not be in container")
	_, hasSentry := env["SENTRY_DSN"]
	assert.False(t, hasSentry, "SENTRY_DSN should not be in container")

	// Non-secret Jira context should still be present.
	assert.Equal(t, "PROJ", env["JIRA_PROJECT_KEY"])
	assert.Equal(t, "42", env["JIRA_ISSUE_ID"])
	assert.Equal(t, "PROJ-42", env["JIRA_ISSUE_KEY"])
	assert.Equal(t, "1", env["PYTHONUNBUFFERED"])
	assert.Equal(t, "custom_value", env["CUSTOM_VAR"])
}

func TestDispatchIssue_GitHubTokenProvider(t *testing.T) {
	t.Parallel()

	tr := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.TrackerKind = "github"
	cfg.TrackerOwner = "testowner"
	cfg.TrackerRepo = "testrepo"
	cfg.ProxyListenPort = 9480

	tokenProvider := func() (string, error) { return "minted-token", nil }
	orch := New(cfg, "Work on {{ issue.title }}", tr, r, ws, testLogger(),
		WithGitHubTokenProvider(tokenProvider))

	ctx := context.Background()
	err := orch.DispatchIssue(ctx, makeIssue("1", "PROJ-1", "Test", "open"), nil)
	require.NoError(t, err)

	r.mu.Lock()
	env := r.lastParams.EnvVars
	r.mu.Unlock()

	assert.Equal(t, "minted-token", env["GITHUB_TOKEN"])
}

func TestBackoffMs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		attempt  int
		maxMs    int
		expected int64
	}{
		{"attempt 1", 1, 300000, 10000},
		{"attempt 2", 2, 300000, 20000},
		{"attempt 3", 3, 300000, 40000},
		{"attempt 4", 4, 300000, 80000},
		{"attempt 5", 5, 300000, 160000},
		{"attempt 6 hits cap", 6, 300000, 300000},
		{"attempt 0 defaults to 1", 0, 300000, 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := BackoffMs(tt.attempt, tt.maxMs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBackoffMs_CapsAtMax(t *testing.T) {
	t.Parallel()

	// Very high attempt should be capped.
	result := BackoffMs(20, 300000)
	assert.Equal(t, int64(300000), result)

	// Custom max.
	result = BackoffMs(3, 30000)
	assert.Equal(t, int64(30000), result)
}

func TestScheduleRetry_ContinuationUsesShortDelay(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	orch := New(cfg, "test", &mockTracker{}, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	nowMs := time.Now().UnixMilli()
	orch.ScheduleRetry("1", "PROJ-1", 1, true, "")

	entry, ok := orch.retryQueue["1"]
	assert.True(t, ok)
	// Should be approximately 1000ms in the future.
	assert.InDelta(t, nowMs+1000, entry.DueAtMs, 100)
	assert.Nil(t, entry.Error)
}

func TestScheduleRetry_ErrorUsesExponentialBackoff(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	orch := New(cfg, "test", &mockTracker{}, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	nowMs := time.Now().UnixMilli()
	orch.ScheduleRetry("1", "PROJ-1", 2, false, "container crashed")

	entry, ok := orch.retryQueue["1"]
	assert.True(t, ok)
	// Attempt 2: 10000 * 2^(2-1) = 20000ms
	assert.InDelta(t, nowMs+20000, entry.DueAtMs, 200)
	assert.NotNil(t, entry.Error)
	assert.Equal(t, "container crashed", *entry.Error)
}

func TestScheduleRetry_CancelsExistingRetry(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	orch := New(cfg, "test", &mockTracker{}, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	cancelled := false
	orch.retryQueue["1"] = &model.RetryEntry{
		IssueID:    "1",
		Identifier: "PROJ-1",
		Attempt:    1,
		DueAtMs:    time.Now().UnixMilli() + 60000,
		CancelFunc: func() { cancelled = true },
	}

	orch.ScheduleRetry("1", "PROJ-1", 2, false, "new error")

	assert.True(t, cancelled, "existing retry should have been cancelled")
	assert.Equal(t, 2, orch.retryQueue["1"].Attempt)
}

func TestHasSlot_GlobalLimit(t *testing.T) {
	t.Parallel()

	cfg := &config.ServiceConfig{
		MaxConcurrentAgents:  2,
		MaxConcurrentByState: map[string]int{},
	}
	orch := New(cfg, "test", &mockTracker{}, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	// No running or claimed — should have slots.
	assert.True(t, orch.hasSlot("To Do"))

	// Add 2 running entries — should be full.
	orch.running["1"] = &model.RunningEntry{Issue: makeIssue("1", "PROJ-1", "A", "To Do")}
	orch.running["2"] = &model.RunningEntry{Issue: makeIssue("2", "PROJ-2", "B", "To Do")}
	assert.False(t, orch.hasSlot("To Do"))

	// Replace one running with one claimed — only running counts per spec,
	// so 1 running < max 2 means slot is available.
	delete(orch.running, "2")
	orch.claimed["3"] = true
	assert.True(t, orch.hasSlot("To Do"))
}

func TestDispatchIssue_GitHubEnvVars(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.TrackerKind = "github"
	cfg.TrackerOwner = "kilupskalvis"
	cfg.TrackerRepo = "mesh"
	cfg.ProxyListenPort = 9480

	orch := New(cfg, "Work on {{ issue.title }}", tracker, r, ws, testLogger())

	issueURL := "https://github.com/kilupskalvis/mesh/issues/42"
	issue := model.Issue{
		ID:         "42",
		Identifier: "kilupskalvis/mesh#42",
		Title:      "Test Issue",
		State:      "open",
		URL:        &issueURL,
	}

	ctx := context.Background()
	err := orch.DispatchIssue(ctx, issue, nil)
	require.NoError(t, err)

	r.mu.Lock()
	env := r.lastParams.EnvVars
	r.mu.Unlock()

	// GitHub-specific env vars.
	assert.Equal(t, "kilupskalvis/mesh", env["GITHUB_REPO"])
	assert.Equal(t, "42", env["GITHUB_ISSUE_NUMBER"])
	assert.Equal(t, "https://github.com/kilupskalvis/mesh/issues/42", env["GITHUB_ISSUE_URL"])

	// Common env vars.
	assert.Equal(t, "http://host.docker.internal:9480", env["ANTHROPIC_BASE_URL"])
	assert.Equal(t, "1", env["PYTHONUNBUFFERED"])

	// Jira env vars should NOT be present.
	_, hasJiraEndpoint := env["JIRA_ENDPOINT"]
	assert.False(t, hasJiraEndpoint, "JIRA_ENDPOINT should not be set for github tracker")
	_, hasJiraKey := env["JIRA_PROJECT_KEY"]
	assert.False(t, hasJiraKey, "JIRA_PROJECT_KEY should not be set for github tracker")
}

func strPtr(s string) *string { return &s }
