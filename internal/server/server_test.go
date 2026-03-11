package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	snap         orchestrator.StateSnapshot
	refreshCount int
}

func (m *mockProvider) Snapshot() orchestrator.StateSnapshot { return m.snap }
func (m *mockProvider) RequestRefresh()                      { m.refreshCount++ }

func testSnapshot() orchestrator.StateSnapshot {
	now := time.Now()
	return orchestrator.StateSnapshot{
		Running: []model.RunningEntry{
			{
				Identifier:        "PROJ-1",
				Issue:             model.Issue{ID: "abc123", Identifier: "PROJ-1", State: "In Progress"},
				SessionID:         "session-1",
				TurnCount:         3,
				LastAgentEvent:    "turn_completed",
				LastAgentMessage:  "Working on tests",
				StartedAt:         now.Add(-5 * time.Minute),
				AgentInputTokens:  1000,
				AgentOutputTokens: 500,
				AgentTotalTokens:  1500,
			},
		},
		RetryQueue: []model.RetryEntry{
			{
				IssueID:    "def456",
				Identifier: "PROJ-2",
				Attempt:    2,
				DueAtMs:    now.Add(30 * time.Second).UnixMilli(),
			},
		},
		Completed: 5,
		AgentTotals: model.AgentTotals{
			InputTokens:    10000,
			OutputTokens:   5000,
			TotalTokens:    15000,
			SecondsRunning: 300.5,
		},
		RateLimits: &model.RateLimitSnapshot{
			RequestsLimit:     100,
			RequestsRemaining: 42,
			TokensLimit:       100000,
			TokensRemaining:   75000,
		},
	}
}

func TestHandleState(t *testing.T) {
	provider := &mockProvider{snap: testSnapshot()}
	srv := &Server{provider: provider}

	req := httptest.NewRequest("GET", "/api/v1/state", nil)
	w := httptest.NewRecorder()
	srv.handleState(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp stateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, 1, resp.Counts["running"])
	assert.Equal(t, 1, resp.Counts["retrying"])
	assert.Len(t, resp.Running, 1)
	assert.Equal(t, "PROJ-1", resp.Running[0].IssueIdentifier)
	assert.Equal(t, 3, resp.Running[0].TurnCount)
	assert.Len(t, resp.Retrying, 1)
	assert.Equal(t, "PROJ-2", resp.Retrying[0].IssueIdentifier)
	assert.Equal(t, int64(15000), resp.AgentTotals.TotalTokens)
	assert.NotNil(t, resp.RateLimits)
	assert.Equal(t, 42, resp.RateLimits.RequestsRemaining)
}

func TestHandleIssueFound(t *testing.T) {
	provider := &mockProvider{snap: testSnapshot()}
	srv := &Server{provider: provider}

	req := httptest.NewRequest("GET", "/api/v1/PROJ-1", nil)
	req.SetPathValue("identifier", "PROJ-1")
	w := httptest.NewRecorder()
	srv.handleIssue(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "PROJ-1", resp["issue_identifier"])
	assert.Equal(t, "running", resp["status"])
}

func TestHandleIssueNotFound(t *testing.T) {
	provider := &mockProvider{snap: testSnapshot()}
	srv := &Server{provider: provider}

	req := httptest.NewRequest("GET", "/api/v1/PROJ-999", nil)
	req.SetPathValue("identifier", "PROJ-999")
	w := httptest.NewRecorder()
	srv.handleIssue(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp["error"])
}

func TestHandleIssueInRetryQueue(t *testing.T) {
	provider := &mockProvider{snap: testSnapshot()}
	srv := &Server{provider: provider}

	req := httptest.NewRequest("GET", "/api/v1/PROJ-2", nil)
	req.SetPathValue("identifier", "PROJ-2")
	w := httptest.NewRecorder()
	srv.handleIssue(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "retrying", resp["status"])
}

func TestHandleRefresh(t *testing.T) {
	provider := &mockProvider{snap: testSnapshot()}
	srv := &Server{provider: provider}

	req := httptest.NewRequest("POST", "/api/v1/refresh", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	srv.handleRefresh(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["queued"])
	assert.Equal(t, 1, provider.refreshCount)
}

func TestHandleDashboard(t *testing.T) {
	provider := &mockProvider{snap: testSnapshot()}
	srv := &Server{provider: provider}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.handleDashboard(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "Mesh Dashboard")
	assert.Contains(t, w.Body.String(), "PROJ-1")
}

func TestHandleStateNilRateLimits(t *testing.T) {
	provider := &mockProvider{snap: orchestrator.StateSnapshot{}}
	srv := &Server{provider: provider}

	req := httptest.NewRequest("GET", "/api/v1/state", nil)
	w := httptest.NewRecorder()
	srv.handleState(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp stateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp.RateLimits)
}
