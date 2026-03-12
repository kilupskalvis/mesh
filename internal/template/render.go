// Package template handles prompt rendering using Go's text/template.
package template

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/kalvis/mesh/internal/model"
)

// defaultPrompt is used when the workflow prompt body is empty.
const defaultPrompt = "You are working on an issue from Jira."

// templateData is the data structure passed to text/template.
type templateData struct {
	Issue   model.Issue
	Attempt *int
}

// Render renders a Go text/template prompt with issue and attempt variables.
//
// Template syntax uses Go conventions:
//
//	{{ .Issue.Identifier }}, {{ .Issue.Title }}, {{ range .Issue.Labels }}...{{ end }}
//	{{ if .Attempt }}Retry {{ .Attempt }}{{ end }}
func Render(tmpl string, issue model.Issue, attempt *int) (string, error) {
	if tmpl == "" {
		return defaultPrompt, nil
	}

	t, err := template.New("prompt").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("template parse error (note: syntax changed from Liquid to Go text/template — use {{ .Issue.Title }} instead of {{ issue.title }}): %w", err)
	}

	data := templateData{
		Issue:   issue,
		Attempt: attempt,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execution error: %w", err)
	}

	return buf.String(), nil
}
