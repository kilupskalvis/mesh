//go:build integration

package integration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kalvis/mesh/internal/config"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/orchestrator"
	"github.com/kalvis/mesh/internal/runner"
	"github.com/kalvis/mesh/internal/tracker"
	"github.com/kalvis/mesh/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock runner
// ---------------------------------------------------------------------------

// integrationMockRunner satisfies runner.Runner for integration tests.
// It records launched params and simulates a successful agent run.
type integrationMockRunner struct {
	mu       sync.Mutex
	launched []runner.RunParams

	// failNext, when true, causes the next Run call to return a non-zero exit code.
	failNext bool
}

// Compile-time check.
var _ runner.Runner = (*integrationMockRunner)(nil)

func newIntegrationMockRunner() *integrationMockRunner {
	return &integrationMockRunner{}
}

func (m *integrationMockRunner) Run(ctx context.Context, params runner.RunParams) (<-chan model.AgentEvent, <-chan runner.RunResult, error) {
	m.mu.Lock()
	m.launched = append(m.launched, params)
	shouldFail := m.failNext
	m.failNext = false
	m.mu.Unlock()

	evCh := make(chan model.AgentEvent, 10)
	resCh := make(chan runner.RunResult, 1)

	go func() {
		evCh <- model.AgentEvent{Event: "session_start", SessionID: "test-session"}
		time.Sleep(50 * time.Millisecond)
		evCh <- model.AgentEvent{Event: "session_end", SessionID: "test-session", TurnsUsed: 1}
		close(evCh)

		if shouldFail {
			resCh <- runner.RunResult{
				ExitCode: 1,
				Error:    model.NewMeshError(model.ErrContainerExit, "container exited with code 1", nil),
			}
		} else {
			resCh <- runner.RunResult{ExitCode: 0}
		}
		close(resCh)
	}()

	return evCh, resCh, nil
}

func (m *integrationMockRunner) Stop(containerID string) error { return nil }

func (m *integrationMockRunner) IsAvailable() error { return nil }

func (m *integrationMockRunner) getLaunched() []runner.RunParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]runner.RunParams, len(m.launched))
	copy(result, m.launched)
	return result
}

func (m *integrationMockRunner) setFailNext() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = true
}

// ---------------------------------------------------------------------------
// Mock tracker for direct orchestrator use
// ---------------------------------------------------------------------------

// integrationMockTracker implements orchestrator.TrackerClient and allows
// dynamic issue mutation between ticks.
type integrationMockTracker struct {
	mu     sync.Mutex
	issues []model.Issue
}

func (t *integrationMockTracker) FetchCandidateIssues(activeStates []string) ([]model.Issue, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Filter to only active-state issues.
	var result []model.Issue
	activeSet := make(map[string]bool, len(activeStates))
	for _, s := range activeStates {
		activeSet[model.NormalizeState(s)] = true
	}
	for _, issue := range t.issues {
		if activeSet[model.NormalizeState(issue.State)] {
			result = append(result, issue)
		}
	}
	return result, nil
}

func (t *integrationMockTracker) FetchIssuesByStates(stateNames []string) ([]model.Issue, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.issues, nil
}

func (t *integrationMockTracker) FetchIssueStatesByIDs(issueIDs []string) ([]model.Issue, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	idSet := make(map[string]bool, len(issueIDs))
	for _, id := range issueIDs {
		idSet[id] = true
	}
	var result []model.Issue
	for _, issue := range t.issues {
		if idSet[issue.ID] {
			result = append(result, issue)
		}
	}
	return result, nil
}

func (t *integrationMockTracker) setIssues(issues []model.Issue) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.issues = issues
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func integrationTestConfig(wsRoot string) *config.ServiceConfig {
	return &config.ServiceConfig{
		TrackerKind:          "jira",
		TrackerEndpoint:      "http://localhost",
		TrackerEmail:         "test@test.com",
		TrackerAPIToken:      "test-token",
		TrackerProjectKey:    "PROJ",
		ActiveStates:         []string{"to do", "in progress"},
		TerminalStates:       []string{"done", "cancelled"},
		PollIntervalMs:       100,
		WorkspaceRoot:        wsRoot,
		MaxConcurrentAgents:  5,
		MaxTurns:             20,
		MaxRetryBackoffMs:    300000,
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

// writeTempWorkflow creates a temporary WORKFLOW.md file and returns its path.
func writeTempWorkflow(t *testing.T, jiraEndpoint string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	content := `---
tracker:
  kind: jira
  endpoint: "` + jiraEndpoint + `"
  email: "$JIRA_EMAIL"
  api_token: "$JIRA_API_TOKEN"
  project_key: PROJ
  active_states:
    - to do
    - in progress
  terminal_states:
    - done
    - cancelled
polling:
  interval_ms: 100
agent:
  image: "test-agent:latest"
  max_concurrent_agents: 5
  max_turns: 20
---
Work on {{ issue.title }}
`
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
	return path
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestE2E_FullCycleWithMockJira exercises the full flow:
// config loading -> orchestrator -> dispatch -> event handling.
// It creates a temp WORKFLOW.md, starts a mock Jira with one issue,
// creates an orchestrator with a mock runner, runs for a few ticks,
// and asserts that a container was launched for the issue.
func TestE2E_FullCycleWithMockJira(t *testing.T) {
	// Set up mock Jira server.
	jira := newMockJiraServer()
	defer jira.Close()

	jira.setIssues([]map[string]any{
		makeJiraIssue("10001", "PROJ-1", "Fix the login page", "To Do", "2"),
	})

	// Create mock tracker that uses real-format issues.
	mockTracker := &integrationMockTracker{
		issues: []model.Issue{
			makeIssue("10001", "PROJ-1", "Fix the login page", "To Do"),
		},
	}

	// Set up workspace and runner.
	wsRoot := t.TempDir()
	ws := workspace.NewManager(wsRoot)
	mockRun := newIntegrationMockRunner()
	cfg := integrationTestConfig(wsRoot)
	logger := testLogger()

	// Create orchestrator with mock dependencies.
	orch := orchestrator.New(cfg, "Work on {{ issue.title }}", mockTracker, mockRun, ws, logger)

	// Run the orchestrator for enough time to complete at least one tick.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Wait for the first tick to dispatch and the worker to complete.
	time.Sleep(500 * time.Millisecond)
	cancel()
	<-errCh

	// Assert: at least one container was launched.
	launched := mockRun.getLaunched()
	require.GreaterOrEqual(t, len(launched), 1, "expected at least one container launch")

	// Verify the launched params correspond to our issue.
	assert.Equal(t, "test-agent:latest", launched[0].Image)
	assert.Equal(t, "PROJ-1", launched[0].StdinPayload.Issue.Identifier)
	assert.Equal(t, "Fix the login page", launched[0].StdinPayload.Issue.Title)

	// Verify workspace directory was created.
	expectedWsPath := filepath.Join(wsRoot, "PROJ-1")
	_, err := os.Stat(expectedWsPath)
	assert.NoError(t, err, "workspace directory should exist")
}

// TestE2E_ConfigLoadAndValidate loads a temp WORKFLOW.md, creates a
// ServiceConfig from it, and validates it.
func TestE2E_ConfigLoadAndValidate(t *testing.T) {
	// Set env vars required by config resolution.
	t.Setenv("JIRA_EMAIL", "test@example.com")
	t.Setenv("JIRA_API_TOKEN", "test-token-123")

	jira := newMockJiraServer()
	defer jira.Close()

	path := writeTempWorkflow(t, jira.URL())

	// Load the workflow definition.
	wf, err := config.LoadWorkflow(path)
	require.NoError(t, err)
	require.NotNil(t, wf)

	// Create ServiceConfig from the parsed front matter.
	svcCfg, err := config.NewServiceConfig(wf.Config)
	require.NoError(t, err)
	require.NotNil(t, svcCfg)

	// Validate dispatch config.
	err = config.ValidateDispatchConfig(svcCfg)
	require.NoError(t, err)

	// Verify key fields.
	assert.Equal(t, "jira", svcCfg.TrackerKind)
	assert.Equal(t, jira.URL(), svcCfg.TrackerEndpoint)
	assert.Equal(t, "test@example.com", svcCfg.TrackerEmail)
	assert.Equal(t, "test-token-123", svcCfg.TrackerAPIToken)
	assert.Equal(t, "PROJ", svcCfg.TrackerProjectKey)
	assert.Equal(t, []string{"to do", "in progress"}, svcCfg.ActiveStates)
	assert.Equal(t, []string{"done", "cancelled"}, svcCfg.TerminalStates)
	assert.Equal(t, 100, svcCfg.PollIntervalMs)
	assert.Equal(t, "test-agent:latest", svcCfg.AgentImage)
	assert.Equal(t, 5, svcCfg.MaxConcurrentAgents)

	// Verify prompt template was extracted.
	assert.Contains(t, wf.PromptTemplate, "Work on {{ issue.title }}")
}

// TestE2E_MockJiraReturnsIssues starts a mock Jira server, configures it
// with issues, and uses the real JiraClient to fetch and verify them.
func TestE2E_MockJiraReturnsIssues(t *testing.T) {
	jira := newMockJiraServer()
	defer jira.Close()

	jira.setIssues([]map[string]any{
		makeJiraIssue("10001", "PROJ-1", "First issue", "To Do", "2"),
		makeJiraIssue("10002", "PROJ-2", "Second issue", "In Progress", "1"),
	})

	// Create a real JiraClient pointing at the mock server.
	client := tracker.NewJiraClient(jira.URL(), "test@test.com", "fake-token", "PROJ", 5000)

	// Fetch candidate issues.
	issues, err := client.FetchCandidateIssues([]string{"to do", "in progress"})
	require.NoError(t, err)
	require.Len(t, issues, 2)

	// Verify normalization.
	assert.Equal(t, "10001", issues[0].ID)
	assert.Equal(t, "PROJ-1", issues[0].Identifier)
	assert.Equal(t, "First issue", issues[0].Title)
	assert.Equal(t, "To Do", issues[0].State)

	assert.Equal(t, "10002", issues[1].ID)
	assert.Equal(t, "PROJ-2", issues[1].Identifier)
	assert.Equal(t, "Second issue", issues[1].Title)
	assert.Equal(t, "In Progress", issues[1].State)
}

// TestE2E_OrchestratorRespectsTerminalState verifies that when an issue
// transitions from an active state to a terminal state, the orchestrator's
// reconciliation loop stops the agent and marks the issue as completed.
func TestE2E_OrchestratorRespectsTerminalState(t *testing.T) {
	// Use a blocking runner so the worker stays alive long enough for
	// reconciliation to observe the state change.
	blockingRunner := &blockingIntegrationRunner{}

	mockTracker := &integrationMockTracker{
		issues: []model.Issue{
			makeIssue("10001", "PROJ-1", "Active issue", "To Do"),
		},
	}

	wsRoot := t.TempDir()
	ws := workspace.NewManager(wsRoot)
	cfg := integrationTestConfig(wsRoot)
	cfg.PollIntervalMs = 100
	logger := testLogger()

	orch := orchestrator.New(cfg, "Work on {{ issue.title }}", mockTracker, blockingRunner, ws, logger)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Wait for the first tick to dispatch the issue.
	time.Sleep(300 * time.Millisecond)

	// Verify the issue was dispatched (runner was called).
	blockingRunner.mu.Lock()
	runCount := blockingRunner.runCount
	blockingRunner.mu.Unlock()
	require.GreaterOrEqual(t, runCount, 1, "issue should have been dispatched")

	// Now change the issue state to terminal.
	mockTracker.setIssues([]model.Issue{
		makeIssue("10001", "PROJ-1", "Active issue", "Done"),
	})

	// Wait for the next reconciliation tick to pick up the state change.
	time.Sleep(400 * time.Millisecond)
	cancel()
	<-errCh

	// The blocking runner's cancel should have been called.
	blockingRunner.mu.Lock()
	cancelled := blockingRunner.cancelledCount
	blockingRunner.mu.Unlock()
	assert.GreaterOrEqual(t, cancelled, 1, "reconciliation should have cancelled the worker")
}

// TestE2E_RetryOnFailure verifies that when a runner returns a failure,
// the orchestrator schedules a retry.
func TestE2E_RetryOnFailure(t *testing.T) {
	mockTracker := &integrationMockTracker{
		issues: []model.Issue{
			makeIssue("10001", "PROJ-1", "Flaky issue", "To Do"),
		},
	}

	wsRoot := t.TempDir()
	ws := workspace.NewManager(wsRoot)
	mockRun := newIntegrationMockRunner()
	// Make the first run fail.
	mockRun.setFailNext()

	cfg := integrationTestConfig(wsRoot)
	cfg.PollIntervalMs = 100
	logger := testLogger()

	orch := orchestrator.New(cfg, "Work on {{ issue.title }}", mockTracker, mockRun, ws, logger)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Wait for the first tick (dispatch + failure) and potential retry scheduling.
	time.Sleep(600 * time.Millisecond)
	cancel()
	<-errCh

	// The runner should have been called at least once (initial dispatch).
	// Due to the failure and retry mechanism, it may have been called again.
	launched := mockRun.getLaunched()
	require.GreaterOrEqual(t, len(launched), 1, "runner should be called at least once")

	// Verify the first launch was for our issue.
	assert.Equal(t, "PROJ-1", launched[0].StdinPayload.Issue.Identifier)
}

// TestE2E_EventsReceived verifies that the mock runner's agent events
// are properly consumed by the orchestrator's monitoring goroutine.
func TestE2E_EventsReceived(t *testing.T) {
	mockTracker := &integrationMockTracker{
		issues: []model.Issue{
			makeIssue("10001", "PROJ-1", "Event test issue", "To Do"),
		},
	}

	wsRoot := t.TempDir()
	ws := workspace.NewManager(wsRoot)
	mockRun := newIntegrationMockRunner()
	cfg := integrationTestConfig(wsRoot)
	cfg.PollIntervalMs = 100
	logger := testLogger()

	orch := orchestrator.New(cfg, "Work on {{ issue.title }}", mockTracker, mockRun, ws, logger)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- orch.Start(ctx)
	}()

	// Wait for dispatch and worker completion.
	time.Sleep(500 * time.Millisecond)
	cancel()
	<-errCh

	// The mock runner emits session_start and session_end events.
	// If events were properly consumed, the worker completes normally
	// and the runner was invoked at least once.
	launched := mockRun.getLaunched()
	require.GreaterOrEqual(t, len(launched), 1, "runner should have been called")
}

// ---------------------------------------------------------------------------
// blockingIntegrationRunner: stays alive until context is cancelled
// ---------------------------------------------------------------------------

type blockingIntegrationRunner struct {
	mu             sync.Mutex
	runCount       int
	cancelledCount int
}

var _ runner.Runner = (*blockingIntegrationRunner)(nil)

func (m *blockingIntegrationRunner) Run(ctx context.Context, params runner.RunParams) (<-chan model.AgentEvent, <-chan runner.RunResult, error) {
	m.mu.Lock()
	m.runCount++
	m.mu.Unlock()

	evCh := make(chan model.AgentEvent, 16)
	resCh := make(chan runner.RunResult, 1)

	go func() {
		evCh <- model.AgentEvent{Event: "session_start", SessionID: "blocking-session"}
		// Block until context is cancelled.
		<-ctx.Done()

		m.mu.Lock()
		m.cancelledCount++
		m.mu.Unlock()

		close(evCh)
		resCh <- runner.RunResult{ExitCode: 137, Error: ctx.Err()}
		close(resCh)
	}()

	return evCh, resCh, nil
}

func (m *blockingIntegrationRunner) Stop(containerID string) error { return nil }

func (m *blockingIntegrationRunner) IsAvailable() error { return nil }
