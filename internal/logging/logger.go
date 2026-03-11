// Package logging provides structured logging configuration for the Mesh service.
package logging

import (
	"log/slog"
	"os"
)

// NewLogger creates a structured JSON logger that writes to stderr.
// This is the default logger for all Mesh components.
func NewLogger(level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}

// WithIssueContext returns a logger with issue-specific context fields.
func WithIssueContext(logger *slog.Logger, issueID, issueIdentifier string) *slog.Logger {
	return logger.With(
		"issue_id", issueID,
		"issue_identifier", issueIdentifier,
	)
}

// WithSessionContext returns a logger with session and container context.
func WithSessionContext(logger *slog.Logger, sessionID, containerID string) *slog.Logger {
	return logger.With(
		"session_id", sessionID,
		"container_id", containerID,
	)
}
