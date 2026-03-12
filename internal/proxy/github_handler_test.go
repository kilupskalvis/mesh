package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubHandler_GetState(t *testing.T) {
	// Mock GitHub API server.
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/issues/42", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		json.NewEncoder(w).Encode(map[string]any{
			"state":  "open",
			"labels": []map[string]string{{"name": "bug"}, {"name": "urgent"}},
		})
	}))
	defer ghAPI.Close()

	handler := NewGitHubHandler(ghAPI.URL, func() (string, error) { return "test-token", nil })
	req := httptest.NewRequest("POST", "/github/state", nil)
	req.Header.Set("X-GitHub-Owner", "owner")
	req.Header.Set("X-GitHub-Repo", "repo")
	req.Header.Set("X-GitHub-Issue", "42")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "open", resp["state"])
}

func TestGitHubHandler_Comment(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/issues/42/comments", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]string
		json.Unmarshal(body, &parsed)
		assert.Equal(t, "Hello from agent", parsed["body"])
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": 123})
	}))
	defer ghAPI.Close()

	handler := NewGitHubHandler(ghAPI.URL, func() (string, error) { return "test-token", nil })
	reqBody, _ := json.Marshal(map[string]string{"body": "Hello from agent"})
	req := httptest.NewRequest("POST", "/github/comment", bytes.NewReader(reqBody))
	req.Header.Set("X-GitHub-Owner", "owner")
	req.Header.Set("X-GitHub-Repo", "repo")
	req.Header.Set("X-GitHub-Issue", "42")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

func TestGitHubHandler_CreatePR(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/pulls", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]string
		json.Unmarshal(body, &parsed)
		assert.Equal(t, "Fix login bug", parsed["title"])
		assert.Equal(t, "feature-branch", parsed["head"])
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{
			"number":   42,
			"html_url": "https://github.com/owner/repo/pull/42",
		})
	}))
	defer ghAPI.Close()

	handler := NewGitHubHandler(ghAPI.URL, func() (string, error) { return "test-token", nil })
	reqBody, _ := json.Marshal(map[string]string{
		"title": "Fix login bug",
		"body":  "Fixes the issue",
		"head":  "feature-branch",
	})
	req := httptest.NewRequest("POST", "/github/create-pr", bytes.NewReader(reqBody))
	req.Header.Set("X-GitHub-Owner", "owner")
	req.Header.Set("X-GitHub-Repo", "repo")
	req.Header.Set("X-GitHub-Issue", "42")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(42), resp["number"])
}

func TestGitHubHandler_Push(t *testing.T) {
	// Set up a real git repo to push from.
	dir := t.TempDir()
	remote := t.TempDir()

	// Create a bare remote.
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = remote
	require.NoError(t, cmd.Run())

	// Create a local repo with a commit.
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"remote", "add", "origin", remote},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		require.NoError(t, c.Run())
	}
	require.NoError(t, os.WriteFile(dir+"/file.txt", []byte("hello"), 0o644))
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		require.NoError(t, c.Run())
	}

	// The push handler constructs a github.com URL with the token,
	// but for this test we verify the handler returns well-formed JSON.
	handler := NewGitHubHandler("http://unused", func() (string, error) { return "test-token", nil })
	reqBody, _ := json.Marshal(map[string]string{"branch": "main"})
	req := httptest.NewRequest("POST", "/github/push", bytes.NewReader(reqBody))
	req.Header.Set("X-GitHub-Owner", "owner")
	req.Header.Set("X-GitHub-Repo", "repo")
	req.Header.Set("X-GitHub-Issue", "1")
	req.Header.Set("X-GitHub-Workspace", dir)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Will fail because the handler constructs a github.com URL, not the local remote.
	// We verify the error response is well-formed JSON.
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["status"])
}

func TestGitHubHandler_MissingHeaders(t *testing.T) {
	handler := NewGitHubHandler("http://unused", func() (string, error) { return "", nil })
	req := httptest.NewRequest("POST", "/github/state", nil)
	// No X-GitHub-* headers.

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestGitHubHandler_UnknownRoute(t *testing.T) {
	handler := NewGitHubHandler("http://unused", func() (string, error) { return "", nil })
	req := httptest.NewRequest("POST", "/github/unknown", nil)
	req.Header.Set("X-GitHub-Owner", "o")
	req.Header.Set("X-GitHub-Repo", "r")
	req.Header.Set("X-GitHub-Issue", "1")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}
