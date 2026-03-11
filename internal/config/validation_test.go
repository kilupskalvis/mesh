package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validRawConfig() map[string]any {
	return map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"api_token":   "tok",
			"email":       "user@test.com",
			"project_key": "PROJ",
		},
	}
}

func TestValidateDispatchConfig_Valid(t *testing.T) {
	t.Parallel()
	cfg, _ := NewServiceConfig(validRawConfig())
	err := ValidateDispatchConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateDispatchConfig_MissingTrackerKind(t *testing.T) {
	t.Parallel()
	cfg := &ServiceConfig{}
	err := ValidateDispatchConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tracker.kind")
}

func TestValidateDispatchConfig_MissingEndpoint(t *testing.T) {
	t.Parallel()
	cfg := &ServiceConfig{TrackerKind: "jira", TrackerAPIToken: "t", TrackerEmail: "e", TrackerProjectKey: "P", AgentImage: "img"}
	err := ValidateDispatchConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint")
}

func TestValidateDispatchConfig_MissingAgentImage(t *testing.T) {
	t.Parallel()
	cfg := &ServiceConfig{
		TrackerKind:       "jira",
		TrackerEndpoint:   "https://test.atlassian.net",
		TrackerAPIToken:   "tok",
		TrackerEmail:      "user@test.com",
		TrackerProjectKey: "PROJ",
	}
	err := ValidateDispatchConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent.image")
}
