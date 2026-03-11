// Package template handles prompt rendering using Liquid-compatible templates.
package template

import (
	"encoding/json"

	"github.com/kalvis/mesh/internal/model"
	"github.com/osteele/liquid"
)

// Render renders a Liquid-compatible prompt template with issue and attempt variables.
// defaultPrompt is used when the workflow prompt body is empty.
const defaultPrompt = "You are working on an issue from Jira."

func Render(tmpl string, issue model.Issue, attempt *int) (string, error) {
	if tmpl == "" {
		return defaultPrompt, nil
	}

	engine := liquid.NewEngine()
	engine.StrictVariables()
	// Unknown filters already fail by default in osteele/liquid.

	// Convert issue to a map[string]any for template compatibility.
	issueMap, err := structToMap(issue)
	if err != nil {
		return "", model.NewMeshError(model.ErrTemplateRenderError,
			"failed to convert issue to template vars", err)
	}

	bindings := map[string]any{
		"issue":   issueMap,
		"attempt": attempt,
	}

	result, err := engine.ParseAndRenderString(tmpl, bindings)
	if err != nil {
		return "", model.NewMeshError(model.ErrTemplateRenderError,
			"prompt template rendering failed", err)
	}
	return result, nil
}

// structToMap converts a struct to map[string]any via JSON round-trip.
func structToMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
