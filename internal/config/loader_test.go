package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadWorkflow_ValidFile(t *testing.T) {
	t.Parallel()
	wf, err := LoadWorkflow("testdata/valid.md")
	require.NoError(t, err)
	assert.Equal(t, "jira", wf.Config["tracker"].(map[string]any)["kind"])
	assert.Contains(t, wf.PromptTemplate, "You are working on")
}

func TestLoadWorkflow_NoFrontMatter(t *testing.T) {
	t.Parallel()
	wf, err := LoadWorkflow("testdata/no_frontmatter.md")
	require.NoError(t, err)
	assert.Empty(t, wf.Config)
	assert.Equal(t, "Just a prompt body.", wf.PromptTemplate)
}

func TestLoadWorkflow_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := LoadWorkflow("testdata/nonexistent.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing_workflow_file")
}

func TestLoadWorkflow_InvalidYAML(t *testing.T) {
	t.Parallel()
	_, err := LoadWorkflow("testdata/invalid_yaml.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow_parse_error")
}

func TestLoadWorkflow_NonMapFrontMatter(t *testing.T) {
	t.Parallel()
	_, err := LoadWorkflow("testdata/non_map.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow_front_matter_not_a_map")
}

func TestLoadWorkflow_EmptyPromptBody(t *testing.T) {
	t.Parallel()
	wf, err := LoadWorkflow("testdata/empty_body.md")
	require.NoError(t, err)
	assert.Equal(t, "", wf.PromptTemplate)
}
