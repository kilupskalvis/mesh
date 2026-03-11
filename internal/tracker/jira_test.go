package tracker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraClient_BasicAuth(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{
			"issues":     []any{},
			"total":      0,
			"startAt":    0,
			"maxResults": 50,
		})
	}))
	defer srv.Close()

	client := NewJiraClient(srv.URL, "user@test.com", "test-token", "PROJ", 30000)
	_, err := client.FetchCandidateIssues([]string{"to do"})
	require.NoError(t, err)
	assert.Contains(t, gotAuth, "Basic ")
}

func TestJiraClient_FetchCandidateIssues_BuildsJQL(t *testing.T) {
	t.Parallel()
	var gotJQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotJQL = r.URL.Query().Get("jql")
		json.NewEncoder(w).Encode(map[string]any{
			"issues":     []any{},
			"total":      0,
			"startAt":    0,
			"maxResults": 50,
		})
	}))
	defer srv.Close()

	client := NewJiraClient(srv.URL, "u@t.com", "tok", "PROJ", 30000)
	_, err := client.FetchCandidateIssues([]string{"to do", "in progress"})
	require.NoError(t, err)
	assert.Contains(t, gotJQL, `project = PROJ`)
	assert.Contains(t, gotJQL, `"To Do"`)
	assert.Contains(t, gotJQL, `"In Progress"`)
}

func TestJiraClient_FetchCandidateIssues_Pagination(t *testing.T) {
	t.Parallel()
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []any{
					map[string]any{
						"id":  "1",
						"key": "PROJ-1",
						"fields": map[string]any{
							"summary": "First",
							"status":  map[string]any{"name": "To Do"},
						},
					},
				},
				"total":      2,
				"startAt":    0,
				"maxResults": 1,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"issues": []any{
					map[string]any{
						"id":  "2",
						"key": "PROJ-2",
						"fields": map[string]any{
							"summary": "Second",
							"status":  map[string]any{"name": "To Do"},
						},
					},
				},
				"total":      2,
				"startAt":    1,
				"maxResults": 1,
			})
		}
	}))
	defer srv.Close()

	client := NewJiraClient(srv.URL, "u@t.com", "tok", "PROJ", 30000)
	client.PageSize = 1
	issues, err := client.FetchCandidateIssues([]string{"to do"})
	require.NoError(t, err)
	assert.Len(t, issues, 2)
	assert.Equal(t, "PROJ-1", issues[0].Identifier)
	assert.Equal(t, "PROJ-2", issues[1].Identifier)
}

func TestJiraClient_FetchIssueStatesByIDs_EmptyList(t *testing.T) {
	t.Parallel()
	client := NewJiraClient("http://unused", "u", "t", "P", 30000)
	issues, err := client.FetchIssueStatesByIDs([]string{})
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestJiraClient_401_ReturnsAuthError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	client := NewJiraClient(srv.URL, "u", "t", "P", 30000)
	_, err := client.FetchCandidateIssues([]string{"to do"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jira_api_auth")
}

func TestJiraClient_429_ReturnsRateLimitError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
	}))
	defer srv.Close()

	client := NewJiraClient(srv.URL, "u", "t", "P", 30000)
	_, err := client.FetchCandidateIssues([]string{"to do"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jira_api_rate_limit")
}
