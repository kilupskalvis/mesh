// Package model defines the core domain types shared across all Mesh packages.
package model

import (
	"regexp"
	"slices"
	"strings"
	"time"
)

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// Issue is the normalized issue record used by orchestration, prompt rendering,
// and observability. Fields are mapped from the Jira Cloud REST API response.
type Issue struct {
	ID          string       `json:"id"`
	Identifier  string       `json:"identifier"`
	Title       string       `json:"title"`
	Description *string      `json:"description"`
	Priority    *int         `json:"priority"`
	State       string       `json:"state"`
	BranchName  *string      `json:"branch_name"`
	URL         *string      `json:"url"`
	Labels      []string     `json:"labels"`
	BlockedBy   []BlockerRef `json:"blocked_by"`
	CreatedAt   *time.Time   `json:"created_at"`
	UpdatedAt   *time.Time   `json:"updated_at"`
}

// BlockerRef represents a blocking issue reference derived from Jira issue links.
type BlockerRef struct {
	ID         *string `json:"id"`
	Identifier *string `json:"identifier"`
	State      *string `json:"state"`
}

// HasRequiredFields returns true if the issue has all fields required for dispatch.
func (i *Issue) HasRequiredFields() bool {
	return i.ID != "" && i.Identifier != "" && i.Title != "" && i.State != ""
}

// IsTerminal returns true if the blocker's state is in the given terminal states list.
// States are compared after normalization (trim + lowercase).
func (b *BlockerRef) IsTerminal(terminalStates []string) bool {
	if b.State == nil {
		return false
	}
	normalized := NormalizeState(*b.State)
	return slices.Contains(terminalStates, normalized)
}

// SanitizeWorkspaceKey replaces any character not in [A-Za-z0-9._-] with underscore.
func SanitizeWorkspaceKey(identifier string) string {
	return unsafeChars.ReplaceAllString(identifier, "_")
}

// NormalizeState trims whitespace and lowercases a state string for comparison.
func NormalizeState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}
