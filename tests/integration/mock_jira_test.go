//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// mockJiraServer is a configurable mock Jira REST API server.
// It serves the /rest/api/3/search endpoint with configurable issue data.
type mockJiraServer struct {
	mu     sync.Mutex
	issues []map[string]any
	server *httptest.Server
}

func newMockJiraServer() *mockJiraServer {
	m := &mockJiraServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/search", m.handleSearch)
	m.server = httptest.NewServer(mux)
	return m
}

func (m *mockJiraServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	resp := map[string]any{
		"issues":     m.issues,
		"total":      len(m.issues),
		"startAt":    0,
		"maxResults": 50,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (m *mockJiraServer) setIssues(issues []map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.issues = issues
}

func (m *mockJiraServer) URL() string {
	return m.server.URL
}

func (m *mockJiraServer) Close() {
	m.server.Close()
}

// makeJiraIssue creates a Jira-format issue map suitable for the mock server.
func makeJiraIssue(id, key, summary, statusName string, priorityID string) map[string]any {
	issue := map[string]any{
		"id":  id,
		"key": key,
		"fields": map[string]any{
			"summary":     summary,
			"description": nil,
			"status": map[string]any{
				"name": statusName,
			},
			"labels":     []any{},
			"issuelinks": []any{},
			"created":    "2025-01-01T00:00:00Z",
			"updated":    "2025-01-01T00:00:00Z",
		},
	}
	if priorityID != "" {
		fields := issue["fields"].(map[string]any)
		fields["priority"] = map[string]any{"id": priorityID}
	}
	return issue
}
