package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
)

// TokenProvider is a function that returns a fresh GitHub installation token.
type TokenProvider func() (string, error)

// GitHubHandler is an HTTP handler that proxies requests to the GitHub API,
// injecting authentication on the host side so the container never has tokens.
type GitHubHandler struct {
	apiBase       string
	tokenProvider TokenProvider
	mux           *http.ServeMux
}

// NewGitHubHandler creates a GitHubHandler that forwards to the given API base URL.
func NewGitHubHandler(apiBase string, tokenProvider TokenProvider) *GitHubHandler {
	h := &GitHubHandler{
		apiBase:       strings.TrimRight(apiBase, "/"),
		tokenProvider: tokenProvider,
		mux:           http.NewServeMux(),
	}
	h.mux.HandleFunc("/github/state", h.handleState)
	h.mux.HandleFunc("/github/comment", h.handleComment)
	h.mux.HandleFunc("/github/create-pr", h.handleCreatePR)
	h.mux.HandleFunc("/github/push", h.handlePush)
	return h
}

func (h *GitHubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// issueContext extracts owner/repo/issue from request headers.
func issueContext(r *http.Request) (owner, repo, issue string, err error) {
	owner = r.Header.Get("X-GitHub-Owner")
	repo = r.Header.Get("X-GitHub-Repo")
	issue = r.Header.Get("X-GitHub-Issue")
	if owner == "" || repo == "" || issue == "" {
		return "", "", "", fmt.Errorf("missing X-GitHub-Owner, X-GitHub-Repo, or X-GitHub-Issue header")
	}
	return owner, repo, issue, nil
}

func (h *GitHubHandler) handleState(w http.ResponseWriter, r *http.Request) {
	owner, repo, issue, err := issueContext(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%s", h.apiBase, owner, repo, issue)
	resp, err := h.doGitHub("GET", url, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Parse the full issue response, extract state and labels.
	var ghIssue struct {
		State  string `json:"state"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		http.Error(w, fmt.Sprintf("GitHub API: %d %s", resp.StatusCode, string(body)), http.StatusBadGateway)
		return
	}
	json.Unmarshal(body, &ghIssue)

	labels := make([]string, len(ghIssue.Labels))
	for i, l := range ghIssue.Labels {
		labels[i] = l.Name
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"state":  ghIssue.State,
		"labels": labels,
	})
}

func (h *GitHubHandler) handleComment(w http.ResponseWriter, r *http.Request) {
	owner, repo, issue, err := issueContext(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	url := fmt.Sprintf("%s/repos/%s/%s/issues/%s/comments", h.apiBase, owner, repo, issue)
	resp, err := h.doGitHub("POST", url, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	if resp.StatusCode == 201 {
		w.Write(body)
	} else {
		http.Error(w, fmt.Sprintf("GitHub API: %d %s", resp.StatusCode, string(body)), http.StatusBadGateway)
	}
}

func (h *GitHubHandler) handleCreatePR(w http.ResponseWriter, r *http.Request) {
	owner, repo, _, err := issueContext(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	url := fmt.Sprintf("%s/repos/%s/%s/pulls", h.apiBase, owner, repo)
	resp, err := h.doGitHub("POST", url, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	if resp.StatusCode == 201 {
		w.Write(body)
	} else {
		http.Error(w, fmt.Sprintf("GitHub API: %d %s", resp.StatusCode, string(body)), http.StatusBadGateway)
	}
}

func (h *GitHubHandler) handlePush(w http.ResponseWriter, r *http.Request) {
	_, _, _, err := issueContext(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	workspace := r.Header.Get("X-GitHub-Workspace")
	if workspace == "" {
		http.Error(w, "missing X-GitHub-Workspace header", http.StatusBadRequest)
		return
	}

	var body struct {
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Branch == "" {
		http.Error(w, "missing branch in request body", http.StatusBadRequest)
		return
	}

	token, err := h.tokenProvider()
	if err != nil {
		http.Error(w, fmt.Sprintf("getting token: %v", err), http.StatusInternalServerError)
		return
	}

	// Run git push on the host side with token-in-URL auth.
	// This works with GitHub's HTTPS auth: https://x-access-token:<token>@github.com
	remoteURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git",
		token,
		r.Header.Get("X-GitHub-Owner"),
		r.Header.Get("X-GitHub-Repo"),
	)
	cmd := exec.Command("git", "push", remoteURL, body.Branch)
	cmd.Dir = workspace

	output, err := cmd.CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "error",
			"output": string(output),
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"output": string(output),
	})
}

// doGitHub makes an authenticated request to the GitHub API.
func (h *GitHubHandler) doGitHub(method, url string, body io.Reader) (*http.Response, error) {
	token, err := h.tokenProvider()
	if err != nil {
		return nil, fmt.Errorf("getting GitHub token: %w", err)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return http.DefaultClient.Do(req)
}
