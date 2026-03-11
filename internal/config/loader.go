// Package config handles WORKFLOW.md parsing, typed config extraction, validation,
// and dynamic reload.
package config

import (
	"os"
	"strings"

	"github.com/kalvis/mesh/internal/model"
	"gopkg.in/yaml.v3"
)

// LoadWorkflow reads and parses a WORKFLOW.md file into a WorkflowDefinition.
// The file is expected to have optional YAML front matter delimited by '---' lines,
// followed by a Markdown prompt body.
func LoadWorkflow(path string) (*model.WorkflowDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, model.NewMeshError(model.ErrMissingWorkflowFile,
			"cannot read workflow file: "+path, err)
	}

	content := string(data)
	config, promptTemplate, err := parseFrontMatter(content)
	if err != nil {
		return nil, err
	}

	return &model.WorkflowDefinition{
		Config:         config,
		PromptTemplate: strings.TrimSpace(promptTemplate),
	}, nil
}

// parseFrontMatter splits a WORKFLOW.md file into YAML config and prompt body.
func parseFrontMatter(content string) (map[string]any, string, error) {
	if !strings.HasPrefix(content, "---") {
		return map[string]any{}, content, nil
	}

	// Find the closing ---
	rest := content[3:]
	yamlBlock, body, found := strings.Cut(rest, "\n---")
	if !found {
		return map[string]any{}, content, nil
	}

	var parsed any
	if err := yaml.Unmarshal([]byte(yamlBlock), &parsed); err != nil {
		return nil, "", model.NewMeshError(model.ErrWorkflowParseError,
			"invalid YAML front matter", err)
	}

	if parsed == nil {
		return map[string]any{}, strings.TrimSpace(body), nil
	}

	configMap, ok := parsed.(map[string]any)
	if !ok {
		return nil, "", model.NewMeshError(model.ErrWorkflowFrontMatterNotMap,
			"front matter must be a YAML map, got a different type", nil)
	}

	return configMap, body, nil
}
