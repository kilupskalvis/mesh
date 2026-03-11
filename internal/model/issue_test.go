package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeWorkspaceKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple key", "PROJ-123", "PROJ-123"},
		{"slash replaced", "PROJ/123", "PROJ_123"},
		{"spaces replaced", "PROJ 123", "PROJ_123"},
		{"dots preserved", "PROJ.123", "PROJ.123"},
		{"underscore preserved", "PROJ_123", "PROJ_123"},
		{"multiple specials", "PROJ@#$123", "PROJ___123"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, SanitizeWorkspaceKey(tt.input))
		})
	}
}

func TestNormalizeState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected string
	}{
		{"  To Do  ", "to do"},
		{"In Progress", "in progress"},
		{"DONE", "done"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, NormalizeState(tt.input))
		})
	}
}

func TestIssueHasRequiredFields(t *testing.T) {
	t.Parallel()

	valid := Issue{
		ID:         "10042",
		Identifier: "PROJ-123",
		Title:      "Fix login",
		State:      "To Do",
	}
	assert.True(t, valid.HasRequiredFields())

	missing := Issue{ID: "10042"}
	assert.False(t, missing.HasRequiredFields())
}

func TestBlockerRef(t *testing.T) {
	t.Parallel()

	blocker := BlockerRef{
		ID:         strPtr("10043"),
		Identifier: strPtr("PROJ-124"),
		State:      strPtr("Done"),
	}
	assert.True(t, blocker.IsTerminal([]string{"done", "closed"}))
	assert.False(t, blocker.IsTerminal([]string{"cancelled"}))

	nonTerminal := BlockerRef{
		State: strPtr("In Progress"),
	}
	assert.False(t, nonTerminal.IsTerminal([]string{"done", "closed"}))

	nilState := BlockerRef{}
	assert.False(t, nilState.IsTerminal([]string{"done", "closed"}))
}

func strPtr(s string) *string { return &s }
