package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdinPayloadConfig_JSON(t *testing.T) {
	t.Parallel()

	cfg := StdinPayloadConfig{
		TurnTimeoutMs:  3600000,
		MaxTurns:       20,
		Model:          "claude-opus-4-6",
		TerminalStates: []string{"done", "cancelled"},
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded StdinPayloadConfig
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, "claude-opus-4-6", decoded.Model)
	assert.Equal(t, []string{"done", "cancelled"}, decoded.TerminalStates)
}

func TestStdinPayloadConfig_OmitsEmpty(t *testing.T) {
	t.Parallel()

	cfg := StdinPayloadConfig{
		TurnTimeoutMs: 60000,
		MaxTurns:      10,
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.NotContains(t, raw, "model")
	assert.NotContains(t, raw, "terminal_states")
}
