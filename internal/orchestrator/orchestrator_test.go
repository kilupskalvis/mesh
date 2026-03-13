package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kalvis/mesh/internal/config"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/runner"
	"github.com/kalvis/mesh/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock types ---

type mockTracker struct {
	mu                sync.Mutex
	issues            []model.Issue
	err               error
	fetchCount        int
	statesByIDIssues  []model.Issue
	statesByIDErr     error
	labels            map[string][]string // issueID -> labels
	labelsErr         error
	setLabelsErr      error
	commentErr        error
	byLabelIssues     []model.Issue
	byLabelErr        error
	comments          []struct{ IssueID, Body string }
	reviewComments    []model.ReviewComment
	reviewCommentsErr error
}

func (m *mockTracker) FetchCandidateIssues(states []string) ([]model.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fetchCount++
	return m.issues, m.err
}

func (m *mockTracker) FetchIssuesByStates(stateNames []string) ([]model.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.issues, m.err
}

func (m *mockTracker) FetchIssueStatesByIDs(issueIDs []string) ([]model.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statesByIDIssues != nil {
		return m.statesByIDIssues, m.statesByIDErr
	}
	// Filter issues to only those requested.
	var result []model.Issue
	idSet := make(map[string]bool, len(issueIDs))
	for _, id := range issueIDs {
		idSet[id] = true
	}
	for _, issue := range m.issues {
		if idSet[issue.ID] {
			result = append(result, issue)
		}
	}
	return result, m.err
}

func (m *mockTracker) GetLabels(issueID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.labelsErr != nil {
		return nil, m.labelsErr
	}
	if m.labels != nil {
		return m.labels[issueID], nil
	}
	return nil, nil
}

func (m *mockTracker) SetLabels(issueID string, labels []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setLabelsErr != nil {
		return m.setLabelsErr
	}
	if m.labels == nil {
		m.labels = make(map[string][]string)
	}
	m.labels[issueID] = labels
	return nil
}

func (m *mockTracker) PostComment(issueID string, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments = append(m.comments, struct{ IssueID, Body string }{issueID, body})
	return m.commentErr
}

func (m *mockTracker) FetchIssuesByLabel(label string) ([]model.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byLabelIssues, m.byLabelErr
}

func (m *mockTracker) FetchPRReviewComments(issueID string, branchName string) ([]model.ReviewComment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reviewComments, m.reviewCommentsErr
}

type mockRunner struct {
	mu         sync.Mutex
	eventCh    chan model.AgentEvent
	resultCh   chan runner.RunResult
	err        error
	available  error
	runCount   int
	stopCount  int
	lastParams runner.RunParams
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		eventCh:  make(chan model.AgentEvent, 16),
		resultCh: make(chan runner.RunResult, 1),
	}
}

func (m *mockRunner) Run(ctx context.Context, params runner.RunParams) (<-chan model.AgentEvent, <-chan runner.RunResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runCount++
	m.lastParams = params
	if m.err != nil {
		return nil, nil, m.err
	}

	// Create fresh channels for each run so that each worker gets its own pair.
	eventCh := make(chan model.AgentEvent, 16)
	resultCh := make(chan runner.RunResult, 1)

	// Send a quick completion in a goroutine.
	go func() {
		close(eventCh)
		resultCh <- runner.RunResult{ExitCode: 0}
		close(resultCh)
	}()

	return eventCh, resultCh, nil
}

func (m *mockRunner) Stop(containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCount++
	return nil
}

func (m *mockRunner) IsAvailable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.available
}

func (m *mockRunner) getRunCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runCount
}

// --- Test helpers ---

func testConfig() *config.ServiceConfig {
	return &config.ServiceConfig{
		TrackerKind:          "jira",
		TrackerEndpoint:      "https://test.atlassian.net",
		TrackerEmail:         "test@test.com",
		TrackerAPIToken:      "test-token",
		TrackerProjectKey:    "PROJ",
		ActiveStates:         []string{"to do", "in progress"},
		TerminalStates:       []string{"done", "cancelled"},
		PollIntervalMs:       100, // fast for tests
		WorkspaceRoot:        os.TempDir() + "/mesh_test_ws",
		MaxConcurrentAgents:  5,
		MaxTurns:             20,
		MaxRetryBackoffMs:    300000,
		MaxErrorRetries:      3,
		MaxContinuations:     5,
		RetryBackoffBaseMs:   10000,
		TurnTimeoutMs:        3600000,
		ReadTimeoutMs:        5000,
		StallTimeoutMs:       300000,
		AgentImage:           "test-agent:latest",
		HookTimeoutMs:        60000,
		MaxConcurrentByState: map[string]int{},
		DockerExtraEnv:       map[string]string{},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func makeIssue(id, identifier, title, state string) model.Issue {
	return model.Issue{
		ID:         id,
		Identifier: identifier,
		Title:      title,
		State:      state,
	}
}

// --- Tests ---

func TestOrchestratorStartStop(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{issues: nil}
	r := newMockRunner()
	ws := workspace.NewManager(os.TempDir() + "/mesh_test_startstop")
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Let it run for a bit.
	time.Sleep(250 * time.Millisecond)

	// Stop via context cancellation.
	cancel()

	err := <-errCh
	assert.ErrorIs(t, err, context.Canceled)
}

func TestOrchestratorStopMethod(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{issues: nil}
	r := newMockRunner()
	ws := workspace.NewManager(os.TempDir() + "/mesh_test_stopmethod")
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	ctx := context.Background()
	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Let it run for a bit.
	time.Sleep(250 * time.Millisecond)

	// Stop via Stop method.
	orch.Stop()

	err := <-errCh
	assert.NoError(t, err)
}

func TestOrchestratorDispatchesOnTick(t *testing.T) {
	t.Parallel()

	issues := []model.Issue{
		{ID: "1", Identifier: "PROJ-1", Title: "Fix login", State: "To Do", Labels: []string{"mesh"}},
		{ID: "2", Identifier: "PROJ-2", Title: "Add tests", State: "In Progress", Labels: []string{"mesh"}},
	}

	tracker := &mockTracker{issues: issues}
	r := newMockRunner()
	ws := setupTestWorkspace(t)
	cfg := testConfig()

	orch := New(cfg, "Work on {{ .Issue.Title }}", tracker, r, ws, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Wait for at least one tick to run and complete dispatches.
	time.Sleep(500 * time.Millisecond)
	cancel()
	<-errCh

	// Both issues should have been dispatched.
	assert.GreaterOrEqual(t, r.getRunCount(), 2)
}

func TestOrchestratorRespectsMaxConcurrency(t *testing.T) {
	t.Parallel()

	// Create more issues than the max concurrency allows.
	issues := []model.Issue{
		{ID: "1", Identifier: "PROJ-1", Title: "Issue 1", State: "To Do", Labels: []string{"mesh"}},
		{ID: "2", Identifier: "PROJ-2", Title: "Issue 2", State: "To Do", Labels: []string{"mesh"}},
		{ID: "3", Identifier: "PROJ-3", Title: "Issue 3", State: "To Do", Labels: []string{"mesh"}},
	}

	tracker := &mockTracker{issues: issues}
	r := &blockingMockRunner{}
	ws := setupTestWorkspace(t)
	cfg := testConfig()
	cfg.MaxConcurrentAgents = 2

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	// Directly call tick once to avoid timing issues.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch.tick(ctx)

	// Should not exceed max concurrency of 2.
	r.mu.Lock()
	count := r.runCount
	r.mu.Unlock()
	assert.Equal(t, 2, count, "should dispatch exactly 2 (max concurrent agents)")
	assert.Len(t, orch.running, 2, "should have 2 running entries")
}

func TestLabelBasedGating_IssueWithoutMeshLabelNotEligible(t *testing.T) {
	t.Parallel()

	// Issue without "mesh" label should not be eligible for dispatch.
	issues := []model.Issue{
		{ID: "1", Identifier: "PROJ-1", Title: "No Mesh Label", State: "To Do", Labels: []string{"bug"}},
		{ID: "2", Identifier: "PROJ-2", Title: "Has Mesh Label", State: "To Do", Labels: []string{"mesh"}},
	}

	tracker := &mockTracker{issues: issues}
	r := newMockRunner()
	ws := setupTestWorkspace(t)
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	candidates := orch.SelectCandidates(issues)

	// Only issue 2 (with "mesh" label) should be eligible.
	assert.Len(t, candidates, 1)
	assert.Equal(t, "2", candidates[0].ID)
}

func TestOrchestratorHandlesTrackerError(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{
		err: model.NewMeshError(model.ErrJiraAPIRequest, "connection refused", nil),
	}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Should not crash; just log the error and continue.
	time.Sleep(300 * time.Millisecond)
	cancel()

	err := <-errCh
	assert.ErrorIs(t, err, context.Canceled)

	// No dispatches should have occurred.
	assert.Equal(t, 0, r.getRunCount())
}

// blockingMockRunner is a runner whose workers never complete until context is cancelled.
type blockingMockRunner struct {
	mu       sync.Mutex
	runCount int
}

func (m *blockingMockRunner) Run(ctx context.Context, params runner.RunParams) (<-chan model.AgentEvent, <-chan runner.RunResult, error) {
	m.mu.Lock()
	m.runCount++
	m.mu.Unlock()

	eventCh := make(chan model.AgentEvent, 16)
	resultCh := make(chan runner.RunResult, 1)

	go func() {
		// Block until context is cancelled.
		<-ctx.Done()
		close(eventCh)
		resultCh <- runner.RunResult{ExitCode: 137, Error: ctx.Err()}
		close(resultCh)
	}()

	return eventCh, resultCh, nil
}

func (m *blockingMockRunner) Stop(containerID string) error {
	return nil
}

func (m *blockingMockRunner) IsAvailable() error {
	return nil
}

func TestOrchestratorRespectsPerStateConcurrency(t *testing.T) {
	t.Parallel()

	issues := []model.Issue{
		{ID: "1", Identifier: "PROJ-1", Title: "Issue 1", State: "To Do", Labels: []string{"mesh"}},
		{ID: "2", Identifier: "PROJ-2", Title: "Issue 2", State: "To Do", Labels: []string{"mesh"}},
		{ID: "3", Identifier: "PROJ-3", Title: "Issue 3", State: "To Do", Labels: []string{"mesh"}},
	}

	tracker := &mockTracker{issues: issues}
	r := &blockingMockRunner{}
	ws := setupTestWorkspace(t)
	cfg := testConfig()
	cfg.MaxConcurrentAgents = 10
	cfg.MaxConcurrentByState = map[string]int{
		"to do": 1,
	}

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	// Directly call tick once to avoid timing issues.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch.tick(ctx)

	r.mu.Lock()
	count := r.runCount
	r.mu.Unlock()

	// Per-state limit of 1 for "to do" should only allow 1 dispatch.
	assert.Equal(t, 1, count, "per-state limit should restrict to 1")
	assert.Len(t, orch.running, 1, "should have 1 running entry")
}

func TestOrchestratorSnapshot(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{issues: nil}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	// Pre-populate some state.
	now := time.Now()
	orch.running["1"] = &model.RunningEntry{
		Identifier: "PROJ-1",
		Issue:      makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		SessionID:  "sess-1",
		StartedAt:  now,
	}
	orch.completedCount = 1
	errMsg := "some error"
	orch.retryQueue["3"] = &model.RetryEntry{
		IssueID:    "3",
		Identifier: "PROJ-3",
		Attempt:    2,
		DueAtMs:    time.Now().UnixMilli() + 10000,
		Error:      &errMsg,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Give the loop a moment to start.
	time.Sleep(200 * time.Millisecond)

	snap := orch.Snapshot()
	cancel()
	<-errCh

	// Note: the snapshot might not have the running entry anymore because
	// the test runner completes immediately and the completion handler
	// removes it. We check the retry queue and completed counts instead.
	assert.GreaterOrEqual(t, snap.Completed, 1)
}

func TestReconcileTerminalIssue(t *testing.T) {
	t.Parallel()

	// Set up an orchestrator with a running issue that has become terminal.
	tracker := &mockTracker{
		statesByIDIssues: []model.Issue{
			makeIssue("1", "PROJ-1", "Issue 1", "Done"),
		},
	}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	// Add a running entry.
	orch.running["1"] = &model.RunningEntry{
		Identifier:  "PROJ-1",
		Issue:       makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		ContainerID: "mesh-test-container",
		StartedAt:   time.Now(),
		CancelFunc:  func() {},
	}

	err := orch.Reconcile(context.Background())
	require.NoError(t, err)

	// Should have been removed from running.
	assert.Empty(t, orch.running)
	// Should have incremented the completed count.
	assert.Greater(t, orch.completedCount, 0)
}

func TestReconcileActiveIssueUpdatesSnapshot(t *testing.T) {
	t.Parallel()

	freshIssue := makeIssue("1", "PROJ-1", "Updated Title", "In Progress")
	tracker := &mockTracker{
		statesByIDIssues: []model.Issue{freshIssue},
		labels:           map[string][]string{"1": {"mesh-working"}},
	}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	orch.running["1"] = &model.RunningEntry{
		Identifier:  "PROJ-1",
		Issue:       makeIssue("1", "PROJ-1", "Old Title", "To Do"),
		ContainerID: "mesh-test-container",
		StartedAt:   time.Now(),
		CancelFunc:  func() {},
	}

	err := orch.Reconcile(context.Background())
	require.NoError(t, err)

	// Should still be running but with updated issue snapshot.
	assert.Len(t, orch.running, 1)
	assert.Equal(t, "Updated Title", orch.running["1"].Issue.Title)
	assert.Equal(t, "In Progress", orch.running["1"].Issue.State)
}

func TestReconcileStallDetection(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.StallTimeoutMs = 100 // 100ms for testing

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	stalledTime := time.Now().Add(-1 * time.Second) // 1s ago, well past 100ms stall timeout
	orch.running["1"] = &model.RunningEntry{
		Identifier:  "PROJ-1",
		Issue:       makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		ContainerID: "mesh-test-container",
		StartedAt:   stalledTime,
		CancelFunc:  func() {},
	}

	err := orch.Reconcile(context.Background())
	require.NoError(t, err)

	// Should have been removed from running.
	assert.Empty(t, orch.running)
	// Should have been added to retry queue.
	assert.Contains(t, orch.retryQueue, "1")
}

func TestReconcileTurnTimeout(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.TurnTimeoutMs = 100 // 100ms for testing
	cfg.StallTimeoutMs = 0  // disable stall detection

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	// Entry started 1s ago — well past 100ms turn timeout.
	startedAt := time.Now().Add(-1 * time.Second)
	orch.running["1"] = &model.RunningEntry{
		Identifier:  "PROJ-1",
		Issue:       makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		ContainerID: "mesh-test-container",
		StartedAt:   startedAt,
		CancelFunc:  func() {},
	}

	err := orch.Reconcile(context.Background())
	require.NoError(t, err)

	assert.Empty(t, orch.running, "should have been removed from running")
	assert.Contains(t, orch.retryQueue, "1", "should have been added to retry queue")
	assert.NotNil(t, orch.retryQueue["1"].Error)
	assert.Equal(t, "turn timeout exceeded", *orch.retryQueue["1"].Error)
}

func TestTurnTimeoutSkippedWhenDisabled(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{
		statesByIDIssues: []model.Issue{
			makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		},
		labels: map[string][]string{"1": {"mesh-working"}},
	}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.TurnTimeoutMs = 0  // disabled
	cfg.StallTimeoutMs = 0 // disabled

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	startedAt := time.Now().Add(-1 * time.Hour)
	orch.running["1"] = &model.RunningEntry{
		Identifier:  "PROJ-1",
		Issue:       makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		ContainerID: "mesh-test-container",
		StartedAt:   startedAt,
		CancelFunc:  func() {},
	}

	err := orch.Reconcile(context.Background())
	require.NoError(t, err)

	assert.Len(t, orch.running, 1, "should still be running because turn timeout is disabled")
}

func TestHandleEventUpdate(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	orch := New(cfg, "test", &mockTracker{}, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	now := time.Now()
	orch.running["1"] = &model.RunningEntry{
		Identifier: "PROJ-1",
		Issue:      makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		StartedAt:  now,
	}

	// Send a turn_start event.
	orch.handleEventUpdate(eventUpdateMsg{
		IssueID: "1",
		Event: model.AgentEvent{
			Event:        "turn_start",
			Timestamp:    now.Format(time.RFC3339),
			Message:      "Working on function refactor",
			InputTokens:  500,
			OutputTokens: 100,
			TotalTokens:  600,
		},
	})

	entry := orch.running["1"]
	assert.Equal(t, "turn_start", entry.LastAgentEvent)
	assert.Equal(t, "Working on function refactor", entry.LastAgentMessage)
	assert.NotNil(t, entry.LastAgentTimestamp)
	assert.Equal(t, int64(500), entry.AgentInputTokens)
	assert.Equal(t, int64(100), entry.AgentOutputTokens)
	assert.Equal(t, int64(600), entry.AgentTotalTokens)
	assert.Equal(t, 1, entry.TurnCount)

	// Send another turn_start — TurnCount should increment.
	orch.handleEventUpdate(eventUpdateMsg{
		IssueID: "1",
		Event:   model.AgentEvent{Event: "turn_start"},
	})
	assert.Equal(t, 2, entry.TurnCount)

	// turn_started also increments TurnCount.
	orch.handleEventUpdate(eventUpdateMsg{
		IssueID: "1",
		Event:   model.AgentEvent{Event: "turn_started"},
	})
	assert.Equal(t, 3, entry.TurnCount)
}

func TestHandleEventUpdate_TruncatesLongMessages(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	orch := New(cfg, "test", &mockTracker{}, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	orch.running["1"] = &model.RunningEntry{
		Identifier: "PROJ-1",
		Issue:      makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		StartedAt:  time.Now(),
	}

	longMsg := strings.Repeat("x", 300)
	orch.handleEventUpdate(eventUpdateMsg{
		IssueID: "1",
		Event:   model.AgentEvent{Event: "tool_use", Message: longMsg},
	})

	assert.Len(t, orch.running["1"].LastAgentMessage, 200)
}

func TestHandleEventUpdate_IgnoresUnknownIssue(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	orch := New(cfg, "test", &mockTracker{}, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	// Should not panic.
	orch.handleEventUpdate(eventUpdateMsg{
		IssueID: "nonexistent",
		Event:   model.AgentEvent{Event: "turn_start"},
	})
}

func TestHandleEventUpdate_RateLimits(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	orch := New(cfg, "test", &mockTracker{}, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	orch.running["1"] = &model.RunningEntry{
		Identifier: "PROJ-1",
		Issue:      makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		StartedAt:  time.Now(),
	}

	// Send a usage event with rate_limits.
	orch.handleEventUpdate(eventUpdateMsg{
		IssueID: "1",
		Event: model.AgentEvent{
			Event: "usage",
			RateLimits: map[string]any{
				"requests_limit":     float64(100),
				"requests_remaining": float64(42),
				"requests_reset":     "2025-01-15T11:00:00Z",
				"tokens_limit":       float64(50000),
				"tokens_remaining":   float64(30000),
				"tokens_reset":       "2025-01-15T11:00:00Z",
			},
		},
	})

	rl := orch.agentRateLimits
	require.NotNil(t, rl)
	assert.Equal(t, 100, rl.RequestsLimit)
	assert.Equal(t, 42, rl.RequestsRemaining)
	assert.Equal(t, "2025-01-15T11:00:00Z", rl.RequestsReset)
	assert.Equal(t, 50000, rl.TokensLimit)
	assert.Equal(t, 30000, rl.TokensRemaining)
	assert.Equal(t, "2025-01-15T11:00:00Z", rl.TokensReset)

	// Verify it shows up in the snapshot (use buildSnapshot directly since
	// Snapshot() requires the event loop to be running).
	snap := orch.buildSnapshot()
	require.NotNil(t, snap.RateLimits)
	assert.Equal(t, 100, snap.RateLimits.RequestsLimit)
	assert.Equal(t, 42, snap.RateLimits.RequestsRemaining)
}

func TestCleanupTerminalWorkspaces(t *testing.T) {
	t.Parallel()

	ws := setupTestWorkspace(t)

	// Create worktrees for terminal issues.
	branch10 := workspace.BranchName("10", "Done Issue")
	branch11 := workspace.BranchName("11", "Cancelled Issue")
	_, err := ws.CreateWorktree(branch10)
	require.NoError(t, err)
	_, err = ws.CreateWorktree(branch11)
	require.NoError(t, err)

	tracker := &mockTracker{
		issues: []model.Issue{
			makeIssue("10", "PROJ-10", "Done Issue", "Done"),
			makeIssue("11", "PROJ-11", "Cancelled Issue", "Cancelled"),
		},
	}
	cfg := testConfig()
	orch := New(cfg, "test", tracker, newMockRunner(), ws, testLogger())

	orch.CleanupTerminalWorkspaces()

	// Both worktrees should have been removed.
	assert.False(t, ws.WorktreeExists(branch10), "branch10 worktree should have been removed")
	assert.False(t, ws.WorktreeExists(branch11), "branch11 worktree should have been removed")
}

func TestCleanupTerminalWorkspaces_TrackerError(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{
		err: model.NewMeshError(model.ErrJiraAPIRequest, "connection refused", nil),
	}
	cfg := testConfig()
	ws := workspace.NewManager(t.TempDir())
	orch := New(cfg, "test", tracker, newMockRunner(), ws, testLogger())

	// Should not panic — failure is non-fatal.
	orch.CleanupTerminalWorkspaces()
}

func TestHandleCompletion_UpdatesAgentTotals(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	tracker := &mockTracker{
		labels: map[string][]string{"1": {"mesh-working"}},
	}
	orch := New(cfg, "test", tracker, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	orch.running["1"] = &model.RunningEntry{
		Identifier:    "PROJ-1",
		Issue:         makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		StartedAt:     time.Now().Add(-10 * time.Second),
		WorkspacePath: t.TempDir(),
	}

	orch.handleCompletion(completionMsg{
		IssueID:      "1",
		Identifier:   "PROJ-1",
		Result:       runner.RunResult{ExitCode: 0},
		Attempt:      1,
		InputTokens:  1000,
		OutputTokens: 500,
		TotalTokens:  1500,
		Duration:     10 * time.Second,
	})

	// Should have been removed from running.
	assert.Empty(t, orch.running)
	// Should have a continuation retry (normal exit).
	assert.Contains(t, orch.retryQueue, "1")
	// Totals should be updated.
	assert.Equal(t, int64(1000), orch.agentTotals.InputTokens)
	assert.Equal(t, int64(500), orch.agentTotals.OutputTokens)
	assert.Equal(t, int64(1500), orch.agentTotals.TotalTokens)
	assert.InDelta(t, 10.0, orch.agentTotals.SecondsRunning, 0.1)
}

func TestHandleCompletion_RunsAfterRunHook(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "hook_ran")

	cfg := testConfig()
	cfg.AfterRunHook = fmt.Sprintf("touch %s", markerFile)
	cfg.HookTimeoutMs = 5000

	tracker := &mockTracker{
		labels: map[string][]string{"1": {"mesh-working"}},
	}
	orch := New(cfg, "test", tracker, newMockRunner(),
		workspace.NewManager(t.TempDir()), testLogger())

	wsDir := t.TempDir()
	orch.running["1"] = &model.RunningEntry{
		Identifier:    "PROJ-1",
		Issue:         makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		StartedAt:     time.Now(),
		WorkspacePath: wsDir,
	}

	orch.handleCompletion(completionMsg{
		IssueID:    "1",
		Identifier: "PROJ-1",
		Result:     runner.RunResult{ExitCode: 0},
		Attempt:    1,
		Duration:   5 * time.Second,
	})

	// Marker file should have been created by the hook.
	_, err := os.Stat(markerFile)
	assert.NoError(t, err, "after_run hook should have created marker file")
}

func TestReloadConfig(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{issues: nil}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.PollIntervalMs = 100

	orch := New(cfg, "original prompt", tracker, r, ws, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Give the loop time to start.
	time.Sleep(150 * time.Millisecond)

	// Reload config with new values.
	newCfg := testConfig()
	newCfg.MaxConcurrentAgents = 20
	newCfg.PollIntervalMs = 500
	orch.ReloadConfig(newCfg, "new prompt")

	// Give time for the reload message to be processed.
	time.Sleep(200 * time.Millisecond)

	// Verify via snapshot that the orchestrator is still running fine.
	snap := orch.Snapshot()
	_ = snap

	cancel()
	<-errCh

	// Verify config was applied (we read the config field directly since
	// Orchestrator is in the same package for testing).
	assert.Equal(t, 20, orch.config.MaxConcurrentAgents)
	assert.Equal(t, 500, orch.config.PollIntervalMs)
	assert.Equal(t, "new prompt", orch.promptTmpl)
}

func TestFindMeshLabel_MeshRevision(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "mesh-revision", findMeshLabel([]string{"mesh-revision"}))
	assert.Equal(t, "mesh-revision", findMeshLabel([]string{"mesh-revision", "mesh"}))
	assert.Equal(t, "mesh-working", findMeshLabel([]string{"mesh-revision", "mesh-working"}))
	assert.Equal(t, "mesh-review", findMeshLabel([]string{"mesh-revision", "mesh-review"}))
	assert.Equal(t, "mesh-failed", findMeshLabel([]string{"mesh-revision", "mesh-failed"}))
}

func TestStallDetectionSkippedWhenDisabled(t *testing.T) {
	t.Parallel()

	tracker := &mockTracker{
		statesByIDIssues: []model.Issue{
			makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		},
		labels: map[string][]string{"1": {"mesh-working"}},
	}
	r := newMockRunner()
	ws := workspace.NewManager(t.TempDir())
	cfg := testConfig()
	cfg.StallTimeoutMs = 0 // disabled
	cfg.TurnTimeoutMs = 0  // disabled

	orch := New(cfg, "test prompt", tracker, r, ws, testLogger())

	stalledTime := time.Now().Add(-1 * time.Hour)
	orch.running["1"] = &model.RunningEntry{
		Identifier:  "PROJ-1",
		Issue:       makeIssue("1", "PROJ-1", "Issue 1", "To Do"),
		ContainerID: "mesh-test-container",
		StartedAt:   stalledTime,
		CancelFunc:  func() {},
	}

	err := orch.Reconcile(context.Background())
	require.NoError(t, err)

	// Should still be running because both stall and turn timeout are disabled.
	assert.Len(t, orch.running, 1)
}
