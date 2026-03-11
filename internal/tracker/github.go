package tracker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kalvis/mesh/internal/model"
)

// TokenProvider returns a valid GitHub token, minting a new one if needed.
type TokenProvider func() (string, error)

// GitHubClient communicates with the GitHub Issues REST API.
type GitHubClient struct {
	Owner         string
	Repo          string
	Label         string
	TimeoutMs     int
	PageSize      int
	tokenProvider TokenProvider
	httpClient    *http.Client
	baseURL       string // for testing; defaults to "https://api.github.com"
}

// NewGitHubClient creates a GitHub Issues client with a token provider.
func NewGitHubClient(owner, repo string, tokenProvider TokenProvider, timeoutMs int) *GitHubClient {
	return &GitHubClient{
		Owner:         owner,
		Repo:          repo,
		tokenProvider: tokenProvider,
		PageSize:      100,
		TimeoutMs:     timeoutMs,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutMs) * time.Millisecond,
		},
		baseURL: "https://api.github.com",
	}
}

// SetLabel sets the required label filter for candidate issues.
func (c *GitHubClient) SetLabel(label string) {
	c.Label = label
}

// FetchCandidateIssues retrieves open issues with the configured label.
func (c *GitHubClient) FetchCandidateIssues(activeStates []string) ([]model.Issue, error) {
	hasOpen := false
	for _, s := range activeStates {
		if strings.ToLower(s) == "open" {
			hasOpen = true
			break
		}
	}
	if !hasOpen {
		return nil, nil
	}

	return c.fetchIssues("open", c.Label)
}

// FetchIssuesByStates retrieves issues by state. Used for terminal workspace cleanup.
func (c *GitHubClient) FetchIssuesByStates(stateNames []string) ([]model.Issue, error) {
	if len(stateNames) == 0 {
		return nil, nil
	}

	hasOpen := false
	hasClosed := false
	for _, s := range stateNames {
		switch strings.ToLower(s) {
		case "open":
			hasOpen = true
		case "closed":
			hasClosed = true
		}
	}

	state := ""
	switch {
	case hasOpen && hasClosed:
		state = "all"
	case hasOpen:
		state = "open"
	case hasClosed:
		state = "closed"
	default:
		return nil, nil
	}

	return c.fetchIssues(state, "")
}

// FetchIssueStatesByIDs retrieves individual issues by number for reconciliation.
func (c *GitHubClient) FetchIssueStatesByIDs(issueIDs []string) ([]model.Issue, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}

	var issues []model.Issue
	for _, id := range issueIDs {
		reqURL := fmt.Sprintf("%s/repos/%s/%s/issues/%s", c.baseURL, c.Owner, c.Repo, id)
		body, err := c.doGet(reqURL)
		if err != nil {
			return nil, err
		}

		var raw ghIssue
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, model.NewMeshError(model.ErrGitHubMalformedResponse,
				"failed to parse GitHub issue response", err)
		}

		if raw.PullRequest != nil {
			continue
		}

		issues = append(issues, c.normalizeIssue(raw))
	}

	return issues, nil
}

func (c *GitHubClient) fetchIssues(state, label string) ([]model.Issue, error) {
	var allIssues []model.Issue
	page := 1

	for {
		reqURL := fmt.Sprintf("%s/repos/%s/%s/issues?state=%s&per_page=%d&page=%d&sort=created&direction=asc",
			c.baseURL, c.Owner, c.Repo, state, c.PageSize, page)
		if label != "" {
			reqURL += "&labels=" + label
		}

		body, err := c.doGet(reqURL)
		if err != nil {
			return nil, err
		}

		var rawIssues []ghIssue
		if err := json.Unmarshal(body, &rawIssues); err != nil {
			return nil, model.NewMeshError(model.ErrGitHubMalformedResponse,
				"failed to parse GitHub issues response", err)
		}

		for _, raw := range rawIssues {
			if raw.PullRequest != nil {
				continue
			}
			allIssues = append(allIssues, c.normalizeIssue(raw))
		}

		if len(rawIssues) < c.PageSize {
			break
		}
		page++
	}

	return allIssues, nil
}

func (c *GitHubClient) doGet(reqURL string) ([]byte, error) {
	token, err := c.tokenProvider()
	if err != nil {
		return nil, model.NewMeshError(model.ErrGitHubAPIAuth, "failed to get token", err)
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, model.NewMeshError(model.ErrGitHubAPIRequest, "failed to create request", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, model.NewMeshError(model.ErrGitHubAPIRequest, "request failed", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, model.NewMeshError(model.ErrGitHubAPIRequest, "failed to read response body", err)
	}

	switch {
	case resp.StatusCode == 401:
		return nil, model.NewMeshError(model.ErrGitHubAPIAuth, "authentication failed", nil)
	case resp.StatusCode == 403:
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		if remaining == "0" {
			return nil, model.NewMeshError(model.ErrGitHubAPIRateLimit,
				fmt.Sprintf("rate limited (reset: %s)", resp.Header.Get("X-RateLimit-Reset")), nil)
		}
		return nil, model.NewMeshError(model.ErrGitHubAPIPermission, "permission denied", nil)
	case resp.StatusCode == 404:
		return nil, model.NewMeshError(model.ErrGitHubAPINotFound,
			fmt.Sprintf("not found: %s", reqURL), nil)
	case resp.StatusCode == 429:
		return nil, model.NewMeshError(model.ErrGitHubAPIRateLimit,
			fmt.Sprintf("rate limited (Retry-After: %s)", resp.Header.Get("Retry-After")), nil)
	case resp.StatusCode >= 400:
		return nil, model.NewMeshError(model.ErrGitHubAPIRequest,
			fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)), nil)
	}

	return body, nil
}

func (c *GitHubClient) normalizeIssue(raw ghIssue) model.Issue {
	number := strconv.Itoa(raw.Number)
	identifier := fmt.Sprintf("%s/%s#%d", c.Owner, c.Repo, raw.Number)
	branchName := fmt.Sprintf("feature/%s-%d", c.Repo, raw.Number)

	issue := model.Issue{
		ID:         number,
		Identifier: identifier,
		Title:      raw.Title,
		State:      raw.State,
		BranchName: &branchName,
	}

	if raw.Body != "" {
		issue.Description = &raw.Body
	}

	issue.URL = &raw.HTMLURL

	issue.Labels = make([]string, len(raw.Labels))
	for i, l := range raw.Labels {
		issue.Labels[i] = strings.ToLower(l.Name)
	}

	issue.BlockedBy = []model.BlockerRef{}

	if t, err := time.Parse(time.RFC3339, raw.CreatedAt); err == nil {
		issue.CreatedAt = &t
	}
	if t, err := time.Parse(time.RFC3339, raw.UpdatedAt); err == nil {
		issue.UpdatedAt = &t
	}

	return issue
}

// GitHub API response types (internal).
type ghIssue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	HTMLURL     string    `json:"html_url"`
	Labels      []ghLabel `json:"labels"`
	PullRequest *struct{} `json:"pull_request"`
	CreatedAt   string    `json:"created_at"`
	UpdatedAt   string    `json:"updated_at"`
}

type ghLabel struct {
	Name string `json:"name"`
}
