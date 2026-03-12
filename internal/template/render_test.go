package template

import (
	"testing"

	"github.com/kalvis/mesh/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender_BasicIssue(t *testing.T) {
	t.Parallel()
	tmpl := "Working on {{ .Issue.Identifier }}: {{ .Issue.Title }}"
	issue := model.Issue{
		ID: "1", Identifier: "PROJ-123", Title: "Fix bug", State: "To Do",
	}
	result, err := Render(tmpl, issue, nil)
	require.NoError(t, err)
	assert.Equal(t, "Working on PROJ-123: Fix bug", result)
}

func TestRender_WithAttempt(t *testing.T) {
	t.Parallel()
	tmpl := "Attempt: {{ .Attempt }}"
	issue := model.Issue{ID: "1", Identifier: "X-1", Title: "T", State: "S"}
	attempt := 3
	result, err := Render(tmpl, issue, &attempt)
	require.NoError(t, err)
	assert.Equal(t, "Attempt: 3", result)
}

func TestRender_NullAttempt(t *testing.T) {
	t.Parallel()
	tmpl := "{{ if .Attempt }}Retry {{ .Attempt }}{{ else }}First run{{ end }}"
	issue := model.Issue{ID: "1", Identifier: "X-1", Title: "T", State: "S"}
	result, err := Render(tmpl, issue, nil)
	require.NoError(t, err)
	assert.Equal(t, "First run", result)
}

func TestRender_Labels(t *testing.T) {
	t.Parallel()
	tmpl := "Labels: {{ range .Issue.Labels }}{{ . }} {{ end }}"
	issue := model.Issue{
		ID: "1", Identifier: "X-1", Title: "T", State: "S",
		Labels: []string{"bug", "urgent"},
	}
	result, err := Render(tmpl, issue, nil)
	require.NoError(t, err)
	assert.Equal(t, "Labels: bug urgent ", result)
}

func TestRender_EmptyTemplate_ReturnsDefaultPrompt(t *testing.T) {
	t.Parallel()
	issue := model.Issue{ID: "1", Identifier: "X-1", Title: "T", State: "S"}
	result, err := Render("", issue, nil)
	require.NoError(t, err)
	assert.Equal(t, defaultPrompt, result)
}

func TestRender_InvalidSyntax_ReturnsError(t *testing.T) {
	t.Parallel()
	issue := model.Issue{ID: "1", Identifier: "X-1", Title: "T", State: "S"}
	_, err := Render("{{ .BadField }", issue, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "template")
}
