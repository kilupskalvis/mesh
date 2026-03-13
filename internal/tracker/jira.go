// Package tracker implements the Jira Cloud REST API v3 client for issue
// fetching, state refresh, and normalization.
package tracker

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kalvis/mesh/internal/model"
)

// JiraClient communicates with the Jira Cloud REST API v3.
type JiraClient struct {
	Endpoint   string
	Email      string
	Token      string
	ProjectKey string
	TimeoutMs  int
	PageSize   int
	httpClient *http.Client
}

// NewJiraClient creates a Jira Cloud client with Basic auth.
func NewJiraClient(endpoint, email, token, projectKey string, timeoutMs int) *JiraClient {
	return &JiraClient{
		Endpoint:   strings.TrimRight(endpoint, "/"),
		Email:      email,
		Token:      token,
		ProjectKey: projectKey,
		TimeoutMs:  timeoutMs,
		PageSize:   50,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutMs) * time.Millisecond,
		},
	}
}

// FetchCandidateIssues retrieves issues in active states for the configured project.
func (c *JiraClient) FetchCandidateIssues(activeStates []string) ([]model.Issue, error) {
	quoted := make([]string, len(activeStates))
	for i, s := range activeStates {
		quoted[i] = fmt.Sprintf("%q", titleCase(s))
	}
	jql := fmt.Sprintf("project = %s AND status in (%s) ORDER BY priority ASC, created ASC",
		c.ProjectKey, strings.Join(quoted, ", "))

	return c.searchAllPages(jql)
}

// FetchIssuesByStates retrieves issues in the given states.
func (c *JiraClient) FetchIssuesByStates(stateNames []string) ([]model.Issue, error) {
	if len(stateNames) == 0 {
		return nil, nil
	}
	quoted := make([]string, len(stateNames))
	for i, s := range stateNames {
		quoted[i] = fmt.Sprintf("%q", titleCase(s))
	}
	jql := fmt.Sprintf("project = %s AND status in (%s)",
		c.ProjectKey, strings.Join(quoted, ", "))

	return c.searchAllPages(jql)
}

// FetchIssueStatesByIDs retrieves current states for specific issue IDs.
func (c *JiraClient) FetchIssueStatesByIDs(issueIDs []string) ([]model.Issue, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}
	jql := fmt.Sprintf("id in (%s)", strings.Join(issueIDs, ", "))
	return c.searchAllPages(jql)
}

// GetLabels is not yet implemented for Jira (deferred to follow-up spec).
func (c *JiraClient) GetLabels(issueID string) ([]string, error) {
	return nil, fmt.Errorf("GetLabels not implemented for Jira tracker")
}

// SetLabels is not yet implemented for Jira (deferred to follow-up spec).
func (c *JiraClient) SetLabels(issueID string, labels []string) error {
	return fmt.Errorf("SetLabels not implemented for Jira tracker")
}

// PostComment is not yet implemented for Jira (deferred to follow-up spec).
func (c *JiraClient) PostComment(issueID string, body string) error {
	return fmt.Errorf("PostComment not implemented for Jira tracker")
}

// FetchIssuesByLabel is not yet implemented for Jira (deferred to follow-up spec).
func (c *JiraClient) FetchIssuesByLabel(label string) ([]model.Issue, error) {
	return nil, fmt.Errorf("FetchIssuesByLabel not implemented for Jira tracker")
}

func (c *JiraClient) searchAllPages(jql string) ([]model.Issue, error) {
	var allIssues []model.Issue
	startAt := 0

	for {
		searchURL := fmt.Sprintf("%s/rest/api/3/search?jql=%s&startAt=%d&maxResults=%d&fields=summary,description,priority,status,labels,issuelinks,created,updated",
			c.Endpoint, url.QueryEscape(jql), startAt, c.PageSize)

		body, err := c.doGet(searchURL)
		if err != nil {
			return nil, err
		}

		var result jiraSearchResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, model.NewMeshError(model.ErrJiraMalformedResponse,
				"failed to parse Jira search response", err)
		}

		for _, raw := range result.Issues {
			issue := normalizeIssue(raw, c.Endpoint)
			allIssues = append(allIssues, issue)
		}

		if startAt+len(result.Issues) >= result.Total {
			break
		}
		startAt += len(result.Issues)
	}

	return allIssues, nil
}

func (c *JiraClient) doGet(reqURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, model.NewMeshError(model.ErrJiraAPIRequest, "failed to create request", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(c.Email + ":" + c.Token))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, model.NewMeshError(model.ErrJiraAPIRequest, "request failed", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, model.NewMeshError(model.ErrJiraAPIRequest, "failed to read response body", err)
	}

	switch {
	case resp.StatusCode == 401:
		return nil, model.NewMeshError(model.ErrJiraAPIAuth, "authentication failed", nil)
	case resp.StatusCode == 403:
		return nil, model.NewMeshError(model.ErrJiraAPIPermission, "permission denied", nil)
	case resp.StatusCode == 429:
		return nil, model.NewMeshError(model.ErrJiraAPIRateLimit,
			fmt.Sprintf("rate limited (Retry-After: %s)", resp.Header.Get("Retry-After")), nil)
	case resp.StatusCode >= 400:
		return nil, model.NewMeshError(model.ErrJiraAPIStatus,
			fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)), nil)
	}

	return body, nil
}

// titleCase converts a lowercased state name back to Title Case for JQL queries.
// e.g. "to do" -> "To Do", "in progress" -> "In Progress"
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// Jira API response types (internal).
type jiraSearchResponse struct {
	Issues     []jiraIssue `json:"issues"`
	Total      int         `json:"total"`
	StartAt    int         `json:"startAt"`
	MaxResults int         `json:"maxResults"`
}

type jiraIssue struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"`
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Summary     string          `json:"summary"`
	Description any             `json:"description"`
	Priority    *jiraPriority   `json:"priority"`
	Status      jiraStatus      `json:"status"`
	Labels      []string        `json:"labels"`
	IssueLinks  []jiraIssueLink `json:"issuelinks"`
	Created     string          `json:"created"`
	Updated     string          `json:"updated"`
}

type jiraPriority struct {
	ID string `json:"id"`
}

type jiraStatus struct {
	Name string `json:"name"`
}

type jiraIssueLink struct {
	Type         jiraLinkType `json:"type"`
	InwardIssue  *jiraLinked  `json:"inwardIssue"`
	OutwardIssue *jiraLinked  `json:"outwardIssue"`
}

type jiraLinkType struct {
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

type jiraLinked struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Status jiraStatus `json:"status"`
	} `json:"fields"`
}
