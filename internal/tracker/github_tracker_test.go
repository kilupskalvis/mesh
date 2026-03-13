package tracker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kalvis/mesh/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func staticToken(token string) TokenProvider {
	return func() (string, error) { return token, nil }
}

func newTestGitHubClient(t *testing.T, serverURL string) *GitHubClient {
	t.Helper()
	c := NewGitHubClient("testowner", "testrepo", staticToken("test-token"), 5000)
	c.baseURL = serverURL
	return c
}

func TestGitHubClient_FetchCandidateIssues(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		assert.Equal(t, "mesh", r.URL.Query().Get("labels"))

		issues := []ghIssue{
			{Number: 1, Title: "Fix bug", Body: "Description here", State: "open",
				HTMLURL:   "https://github.com/testowner/testrepo/issues/1",
				Labels:    []ghLabel{{Name: "mesh"}, {Name: "Bug"}},
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z"},
			{Number: 2, Title: "Add feature", State: "open",
				HTMLURL:   "https://github.com/testowner/testrepo/issues/2",
				Labels:    []ghLabel{{Name: "mesh"}},
				CreatedAt: "2026-01-03T00:00:00Z", UpdatedAt: "2026-01-03T00:00:00Z"},
		}
		json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	c.SetLabel("mesh")

	issues, err := c.FetchCandidateIssues([]string{"open"})
	require.NoError(t, err)
	require.Len(t, issues, 2)

	assert.Equal(t, "1", issues[0].ID)
	assert.Equal(t, "testowner/testrepo#1", issues[0].Identifier)
	assert.Equal(t, "Fix bug", issues[0].Title)
	assert.Equal(t, "open", issues[0].State)
	assert.NotNil(t, issues[0].Description)
	assert.Equal(t, "Description here", *issues[0].Description)
	assert.Equal(t, "https://github.com/testowner/testrepo/issues/1", *issues[0].URL)
	assert.Equal(t, "feature/testrepo-1", *issues[0].BranchName)
	assert.Equal(t, []string{"mesh", "bug"}, issues[0].Labels)
	assert.Empty(t, issues[0].BlockedBy)
	assert.Nil(t, issues[0].Priority)
	assert.NotNil(t, issues[0].CreatedAt)
	assert.NotNil(t, issues[0].UpdatedAt)
}

func TestGitHubClient_FetchCandidateIssues_NoOpenState(t *testing.T) {
	t.Parallel()

	c := NewGitHubClient("o", "r", staticToken("t"), 5000)
	issues, err := c.FetchCandidateIssues([]string{"closed"})
	require.NoError(t, err)
	assert.Nil(t, issues)
}

func TestGitHubClient_FiltersPullRequests(t *testing.T) {
	t.Parallel()

	pr := &struct{}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issues := []ghIssue{
			{Number: 1, Title: "Real Issue", State: "open",
				HTMLURL: "https://github.com/o/r/issues/1", Labels: []ghLabel{},
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
			{Number: 2, Title: "Pull Request", State: "open", PullRequest: pr,
				HTMLURL: "https://github.com/o/r/pull/2", Labels: []ghLabel{},
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		}
		json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	issues, err := c.FetchCandidateIssues([]string{"open"})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "Real Issue", issues[0].Title)
}

func TestGitHubClient_Pagination(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var issues []ghIssue
		if r.URL.Query().Get("page") == "" || r.URL.Query().Get("page") == "1" {
			for i := 1; i <= 100; i++ {
				issues = append(issues, ghIssue{
					Number: i, Title: "Issue", State: "open",
					HTMLURL: "https://github.com/o/r/issues/1",
					Labels:  []ghLabel{}, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"})
			}
		} else {
			issues = append(issues, ghIssue{
				Number: 101, Title: "Last Issue", State: "open",
				HTMLURL: "https://github.com/o/r/issues/101",
				Labels:  []ghLabel{}, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"})
		}
		json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	issues, err := c.FetchCandidateIssues([]string{"open"})
	require.NoError(t, err)
	assert.Len(t, issues, 101)
	assert.Equal(t, 2, callCount)
}

func TestGitHubClient_FetchIssuesByStates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "all", r.URL.Query().Get("state"))
		issues := []ghIssue{
			{Number: 1, Title: "Open", State: "open", HTMLURL: "https://github.com/o/r/issues/1",
				Labels: []ghLabel{}, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
			{Number: 2, Title: "Closed", State: "closed", HTMLURL: "https://github.com/o/r/issues/2",
				Labels: []ghLabel{}, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
		}
		json.NewEncoder(w).Encode(issues)
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	issues, err := c.FetchIssuesByStates([]string{"open", "closed"})
	require.NoError(t, err)
	assert.Len(t, issues, 2)
}

func TestGitHubClient_FetchIssueStatesByIDs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/repos/testowner/testrepo/issues/42")
		issue := ghIssue{
			Number: 42, Title: "Reconcile Me", State: "closed",
			HTMLURL: "https://github.com/testowner/testrepo/issues/42",
			Labels:  []ghLabel{}, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-05T00:00:00Z"}
		json.NewEncoder(w).Encode(issue)
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	issues, err := c.FetchIssueStatesByIDs([]string{"42"})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, "42", issues[0].ID)
	assert.Equal(t, "closed", issues[0].State)
}

func TestGitHubClient_AuthError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	_, err := c.FetchCandidateIssues([]string{"open"})
	require.Error(t, err)

	meshErr, ok := err.(*model.MeshError)
	require.True(t, ok)
	assert.Equal(t, "github_api_auth", meshErr.Kind)
}

func TestGitHubClient_RateLimitError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.WriteHeader(403)
		w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	_, err := c.FetchCandidateIssues([]string{"open"})
	require.Error(t, err)

	meshErr, ok := err.(*model.MeshError)
	require.True(t, ok)
	assert.Equal(t, "github_api_rate_limit", meshErr.Kind)
}

func TestGitHubClient_429Error(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		w.Write([]byte(`{"message":"rate limit"}`))
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	_, err := c.FetchCandidateIssues([]string{"open"})
	require.Error(t, err)

	meshErr, ok := err.(*model.MeshError)
	require.True(t, ok)
	assert.Equal(t, "github_api_rate_limit", meshErr.Kind)
}

func TestGitHubClient_NotFoundError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	_, err := c.FetchCandidateIssues([]string{"open"})
	require.Error(t, err)

	meshErr, ok := err.(*model.MeshError)
	require.True(t, ok)
	assert.Equal(t, "github_api_not_found", meshErr.Kind)
}

func TestGitHubClient_FetchPRReviewComments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/testowner/testrepo/pulls" && r.URL.Query().Get("head") == "testowner:fix-42":
			json.NewEncoder(w).Encode([]ghPull{{Number: 10, UpdatedAt: "2026-03-13T00:00:00Z"}})

		case r.URL.Path == "/repos/testowner/testrepo/pulls/10/comments":
			json.NewEncoder(w).Encode([]ghPRComment{
				{User: ghUser{Login: "alice"}, Body: "Fix this null check", Path: "main.go", Line: 42, CreatedAt: "2026-03-13T01:00:00Z"},
			})

		case r.URL.Path == "/repos/testowner/testrepo/pulls/10/reviews":
			json.NewEncoder(w).Encode([]ghReview{
				{User: ghUser{Login: "bob"}, Body: "Overall looks good, minor nits", State: "CHANGES_REQUESTED", SubmittedAt: "2026-03-13T02:00:00Z"},
			})

		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	comments, err := c.FetchPRReviewComments("42", "fix-42")
	require.NoError(t, err)
	require.Len(t, comments, 2)

	// Sorted by created_at — inline comment first.
	assert.Equal(t, "alice", comments[0].Author)
	assert.Equal(t, "Fix this null check", comments[0].Body)
	assert.Equal(t, "main.go", comments[0].Path)
	assert.Equal(t, 42, comments[0].Line)

	assert.Equal(t, "bob", comments[1].Author)
	assert.Equal(t, "Overall looks good, minor nits", comments[1].Body)
	assert.Equal(t, "", comments[1].Path) // review-level, no file
}

func TestGitHubClient_FetchPRReviewComments_NoPR(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]ghPull{}) // empty
	}))
	defer server.Close()

	c := newTestGitHubClient(t, server.URL)
	comments, err := c.FetchPRReviewComments("42", "fix-42")
	require.NoError(t, err)
	assert.Empty(t, comments)
}
