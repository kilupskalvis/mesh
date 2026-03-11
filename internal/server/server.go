// Package server implements the optional HTTP server extension.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/orchestrator"
)

// SnapshotProvider returns the current orchestrator state.
type SnapshotProvider interface {
	Snapshot() orchestrator.StateSnapshot
	RequestRefresh()
}

// Server is the optional HTTP server for observability and operational control.
type Server struct {
	httpServer *http.Server
	provider   SnapshotProvider
	logger     *slog.Logger
	addr       string

	mu            sync.Mutex
	lastRefreshAt time.Time
}

// New creates a new HTTP server bound to the given port.
// Port 0 requests an ephemeral port. Binds to loopback by default.
func New(port int, provider SnapshotProvider, logger *slog.Logger) *Server {
	s := &Server{
		provider: provider,
		logger:   logger,
		addr:     fmt.Sprintf("127.0.0.1:%d", port),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleDashboard)
	mux.HandleFunc("GET /api/v1/state", s.handleState)
	mux.HandleFunc("GET /api/v1/{identifier}", s.handleIssue)
	mux.HandleFunc("POST /api/v1/refresh", s.handleRefresh)

	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s
}

// Start starts the HTTP server in a background goroutine.
// Returns the actual address the server is listening on.
func (s *Server) Start() (string, error) {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return "", fmt.Errorf("server listen: %w", err)
	}
	actualAddr := ln.Addr().String()
	s.logger.Info("HTTP server started", "addr", actualAddr)

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", "error", err)
		}
	}()

	return actualAddr, nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

// stateResponse is the JSON shape for GET /api/v1/state.
type stateResponse struct {
	GeneratedAt string                   `json:"generated_at"`
	Counts      map[string]int           `json:"counts"`
	Running     []runningRow             `json:"running"`
	Retrying    []retryRow               `json:"retrying"`
	AgentTotals agentTotalsJSON          `json:"agent_totals"`
	RateLimits  *model.RateLimitSnapshot `json:"rate_limits"`
}

type runningRow struct {
	IssueID         string    `json:"issue_id"`
	IssueIdentifier string    `json:"issue_identifier"`
	State           string    `json:"state"`
	SessionID       string    `json:"session_id"`
	TurnCount       int       `json:"turn_count"`
	LastEvent       string    `json:"last_event"`
	LastMessage     string    `json:"last_message"`
	StartedAt       string    `json:"started_at"`
	LastEventAt     *string   `json:"last_event_at"`
	Tokens          tokenJSON `json:"tokens"`
}

type retryRow struct {
	IssueID         string  `json:"issue_id"`
	IssueIdentifier string  `json:"issue_identifier"`
	Attempt         int     `json:"attempt"`
	DueAt           string  `json:"due_at"`
	Error           *string `json:"error"`
}

type agentTotalsJSON struct {
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	TotalTokens    int64   `json:"total_tokens"`
	SecondsRunning float64 `json:"seconds_running"`
}

type tokenJSON struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// handleState handles GET /api/v1/state.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	snap := s.provider.Snapshot()

	running := make([]runningRow, 0, len(snap.Running))
	for _, e := range snap.Running {
		row := runningRow{
			IssueID:         e.Issue.ID,
			IssueIdentifier: e.Identifier,
			State:           e.Issue.State,
			SessionID:       e.SessionID,
			TurnCount:       e.TurnCount,
			LastEvent:       e.LastAgentEvent,
			LastMessage:     e.LastAgentMessage,
			StartedAt:       e.StartedAt.UTC().Format(time.RFC3339),
			Tokens: tokenJSON{
				InputTokens:  e.AgentInputTokens,
				OutputTokens: e.AgentOutputTokens,
				TotalTokens:  e.AgentTotalTokens,
			},
		}
		if e.LastAgentTimestamp != nil {
			ts := e.LastAgentTimestamp.UTC().Format(time.RFC3339)
			row.LastEventAt = &ts
		}
		running = append(running, row)
	}

	retrying := make([]retryRow, 0, len(snap.RetryQueue))
	for _, e := range snap.RetryQueue {
		retrying = append(retrying, retryRow{
			IssueID:         e.IssueID,
			IssueIdentifier: e.Identifier,
			Attempt:         e.Attempt,
			DueAt:           time.UnixMilli(e.DueAtMs).UTC().Format(time.RFC3339),
			Error:           e.Error,
		})
	}

	resp := stateResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Counts: map[string]int{
			"running":  len(snap.Running),
			"retrying": len(snap.RetryQueue),
		},
		Running:  running,
		Retrying: retrying,
		AgentTotals: agentTotalsJSON{
			InputTokens:    snap.AgentTotals.InputTokens,
			OutputTokens:   snap.AgentTotals.OutputTokens,
			TotalTokens:    snap.AgentTotals.TotalTokens,
			SecondsRunning: snap.AgentTotals.SecondsRunning,
		},
		RateLimits: snap.RateLimits,
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleIssue handles GET /api/v1/{identifier}.
func (s *Server) handleIssue(w http.ResponseWriter, r *http.Request) {
	identifier := r.PathValue("identifier")
	if identifier == "" {
		writeError(w, http.StatusBadRequest, "missing_identifier", "issue identifier is required")
		return
	}

	snap := s.provider.Snapshot()

	// Search running entries.
	for _, e := range snap.Running {
		if strings.EqualFold(e.Identifier, identifier) {
			row := runningRow{
				IssueID:         e.Issue.ID,
				IssueIdentifier: e.Identifier,
				State:           e.Issue.State,
				SessionID:       e.SessionID,
				TurnCount:       e.TurnCount,
				LastEvent:       e.LastAgentEvent,
				LastMessage:     e.LastAgentMessage,
				StartedAt:       e.StartedAt.UTC().Format(time.RFC3339),
				Tokens: tokenJSON{
					InputTokens:  e.AgentInputTokens,
					OutputTokens: e.AgentOutputTokens,
					TotalTokens:  e.AgentTotalTokens,
				},
			}
			if e.LastAgentTimestamp != nil {
				ts := e.LastAgentTimestamp.UTC().Format(time.RFC3339)
				row.LastEventAt = &ts
			}

			resp := map[string]any{
				"issue_identifier": e.Identifier,
				"issue_id":         e.Issue.ID,
				"status":           "running",
				"workspace":        map[string]string{"path": e.WorkspacePath},
				"running":          row,
				"retry":            nil,
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	// Search retry queue.
	for _, e := range snap.RetryQueue {
		if strings.EqualFold(e.Identifier, identifier) {
			resp := map[string]any{
				"issue_identifier": e.Identifier,
				"issue_id":         e.IssueID,
				"status":           "retrying",
				"running":          nil,
				"retry": retryRow{
					IssueID:         e.IssueID,
					IssueIdentifier: e.Identifier,
					Attempt:         e.Attempt,
					DueAt:           time.UnixMilli(e.DueAtMs).UTC().Format(time.RFC3339),
					Error:           e.Error,
				},
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	writeError(w, http.StatusNotFound, "issue_not_found",
		fmt.Sprintf("issue %q is not tracked in the current session", identifier))
}

// handleRefresh handles POST /api/v1/refresh.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	coalesced := time.Since(s.lastRefreshAt) < 2*time.Second
	s.lastRefreshAt = time.Now()
	s.mu.Unlock()

	if !coalesced {
		s.provider.RequestRefresh()
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"queued":       true,
		"coalesced":    coalesced,
		"requested_at": time.Now().UTC().Format(time.RFC3339),
		"operations":   []string{"poll", "reconcile"},
	})
}

// handleDashboard serves a human-readable HTML dashboard at /.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	snap := s.provider.Snapshot()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	renderDashboardHTML(w, snap)
}
