package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceConfig_Defaults(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"api_token":   "tok",
			"email":       "user@test.com",
			"project_key": "PROJ",
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)

	assert.Equal(t, 30000, cfg.PollIntervalMs)
	assert.Equal(t, 10, cfg.MaxConcurrentAgents)
	assert.Equal(t, 20, cfg.MaxTurns)
	assert.Equal(t, 300000, cfg.MaxRetryBackoffMs)
	assert.Equal(t, 3600000, cfg.TurnTimeoutMs)
	assert.Equal(t, 300000, cfg.ReadTimeoutMs)
	assert.Equal(t, 300000, cfg.StallTimeoutMs)
	assert.Equal(t, 60000, cfg.HookTimeoutMs)
	assert.Equal(t, "mesh-agent:latest", cfg.AgentImage)
	assert.Contains(t, cfg.ActiveStates, "to do")
	assert.Contains(t, cfg.TerminalStates, "done")
	assert.Contains(t, cfg.TerminalStates, "duplicate")
}

func TestServiceConfig_EnvVarResolution(t *testing.T) {
	os.Setenv("TEST_JIRA_TOKEN", "resolved-token")
	defer os.Unsetenv("TEST_JIRA_TOKEN")

	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"api_token":   "$TEST_JIRA_TOKEN",
			"email":       "user@test.com",
			"project_key": "PROJ",
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, "resolved-token", cfg.TrackerAPIToken)
}

func TestServiceConfig_EmptyEnvVarTreatedAsMissing(t *testing.T) {
	os.Setenv("TEST_EMPTY_TOKEN", "")
	defer os.Unsetenv("TEST_EMPTY_TOKEN")

	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"api_token":   "$TEST_EMPTY_TOKEN",
			"email":       "user@test.com",
			"project_key": "PROJ",
		},
	}
	_, err := NewServiceConfig(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing_tracker_api_token")
}

func TestServiceConfig_TildeExpansion(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"api_token":   "tok",
			"email":       "user@test.com",
			"project_key": "PROJ",
		},
		"workspace": map[string]any{
			"root": "~/mesh_workspaces",
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)

	home, _ := os.UserHomeDir()
	assert.Equal(t, home+"/mesh_workspaces", cfg.WorkspaceRoot)
}

func TestServiceConfig_ActiveStatesFromCSV(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":          "jira",
			"endpoint":      "https://test.atlassian.net",
			"api_token":     "tok",
			"email":         "user@test.com",
			"project_key":   "PROJ",
			"active_states": "Backlog, Selected for Development",
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"backlog", "selected for development"}, cfg.ActiveStates)
}

func TestServiceConfig_PerStateConcurrency(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"api_token":   "tok",
			"email":       "user@test.com",
			"project_key": "PROJ",
		},
		"agent": map[string]any{
			"max_concurrent_agents_by_state": map[string]any{
				"To Do":       2,
				"In Progress": 5,
				"invalid":     -1,
			},
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, 2, cfg.MaxConcurrentByState["to do"])
	assert.Equal(t, 5, cfg.MaxConcurrentByState["in progress"])
	assert.NotContains(t, cfg.MaxConcurrentByState, "invalid")
}

func TestServiceConfig_UnsupportedTrackerKind(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind": "linear",
		},
	}
	_, err := NewServiceConfig(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported_tracker_kind")
}

func TestServiceConfig_AgentModel(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"api_token":   "tok",
			"email":       "user@test.com",
			"project_key": "PROJ",
		},
		"agent": map[string]any{
			"model": "claude-opus-4-6",
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4-6", cfg.AgentModel)
}

func TestServiceConfig_ProxyListenPort(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"email":       "test@test.com",
			"api_token":   "token",
			"project_key": "PROJ",
		},
		"proxy": map[string]any{
			"listen_port": 9480,
		},
	}

	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, 9480, cfg.ProxyListenPort)
}

func TestServiceConfig_ProxyListenPortDefault(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"email":       "test@test.com",
			"api_token":   "token",
			"project_key": "PROJ",
		},
	}

	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, 9480, cfg.ProxyListenPort)
}

func TestServiceConfig_GitHubApp(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":             "github",
			"owner":            "kilupskalvis",
			"repo":             "mesh",
			"app_id":           "123456",
			"installation_id":  "789",
			"private_key_path": "/tmp/key.pem",
		},
	}

	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, "123456", cfg.GitHubAppID)
	assert.Equal(t, "/tmp/key.pem", cfg.GitHubAppPrivateKey)
	assert.Equal(t, "789", cfg.GitHubInstallationID)
}

func TestServiceConfig_AgentModelDefault(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":        "jira",
			"endpoint":    "https://test.atlassian.net",
			"api_token":   "tok",
			"email":       "user@test.com",
			"project_key": "PROJ",
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-6", cfg.AgentModel)
}

func TestServiceConfig_GitHub_Defaults(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":             "github",
			"owner":            "kilupskalvis",
			"repo":             "mesh",
			"app_id":           "123",
			"installation_id":  "456",
			"private_key_path": "/tmp/key.pem",
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)

	assert.Equal(t, "github", cfg.TrackerKind)
	assert.Equal(t, "kilupskalvis", cfg.TrackerOwner)
	assert.Equal(t, "mesh", cfg.TrackerRepo)
	assert.Equal(t, "", cfg.TrackerLabel)
	assert.Equal(t, []string{"open"}, cfg.ActiveStates)
	assert.Equal(t, []string{"closed"}, cfg.TerminalStates)
}

func TestServiceConfig_GitHub_WithLabel(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":             "github",
			"owner":            "kilupskalvis",
			"repo":             "mesh",
			"app_id":           "123",
			"installation_id":  "456",
			"private_key_path": "/tmp/key.pem",
			"label":            "mesh",
		},
	}
	cfg, err := NewServiceConfig(raw)
	require.NoError(t, err)

	assert.Equal(t, "mesh", cfg.TrackerLabel)
}

func TestServiceConfig_GitHub_MissingOwner(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":             "github",
			"repo":             "mesh",
			"app_id":           "123",
			"installation_id":  "456",
			"private_key_path": "/tmp/key.pem",
		},
	}
	_, err := NewServiceConfig(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner")
}

func TestServiceConfig_GitHub_MissingRepo(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":             "github",
			"owner":            "kilupskalvis",
			"app_id":           "123",
			"installation_id":  "456",
			"private_key_path": "/tmp/key.pem",
		},
	}
	_, err := NewServiceConfig(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repo")
}

func TestServiceConfig_GitHub_MissingAppID(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":             "github",
			"owner":            "kilupskalvis",
			"repo":             "mesh",
			"installation_id":  "456",
			"private_key_path": "/tmp/key.pem",
		},
	}
	_, err := NewServiceConfig(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app_id")
}
