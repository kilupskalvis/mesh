package runner

import (
	"testing"

	"github.com/kalvis/mesh/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ParseEvent tests ---

func TestParseEvent_ValidEvent(t *testing.T) {
	t.Parallel()

	line := `{"event":"session_started","ts":"2025-01-15T10:30:00Z","session_id":"abc-123","message":"Agent session started"}`
	ev, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "session_started", ev.Event)
	assert.Equal(t, "2025-01-15T10:30:00Z", ev.Timestamp)
	assert.Equal(t, "abc-123", ev.SessionID)
	assert.Equal(t, "Agent session started", ev.Message)
}

func TestParseEvent_ValidEventWithTokens(t *testing.T) {
	t.Parallel()

	line := `{"event":"turn_completed","ts":"2025-01-15T10:31:00Z","session_id":"abc-123","turn":3,"input_tokens":1500,"output_tokens":800,"total_tokens":2300}`
	ev, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "turn_completed", ev.Event)
	assert.Equal(t, 3, ev.Turn)
	assert.Equal(t, int64(1500), ev.InputTokens)
	assert.Equal(t, int64(800), ev.OutputTokens)
	assert.Equal(t, int64(2300), ev.TotalTokens)
}

func TestParseEvent_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseEvent(`{not valid json}`)
	assert.Error(t, err)
}

func TestParseEvent_EmptyLine(t *testing.T) {
	t.Parallel()

	_, err := ParseEvent("")
	assert.Error(t, err)

	_, err = ParseEvent("   ")
	assert.Error(t, err)
}

func TestParseEvent_MissingEventField(t *testing.T) {
	t.Parallel()

	_, err := ParseEvent(`{"ts":"2025-01-15T10:30:00Z","session_id":"abc-123"}`)
	assert.Error(t, err)
}

func TestParseEvent_WithRateLimits(t *testing.T) {
	t.Parallel()

	line := `{"event":"usage","ts":"2025-01-15T10:31:00Z","rate_limits":{"requests_limit":100,"requests_remaining":42}}`
	ev, err := ParseEvent(line)
	require.NoError(t, err)
	assert.Equal(t, "usage", ev.Event)
	assert.NotNil(t, ev.RateLimits)
	assert.Equal(t, float64(100), ev.RateLimits["requests_limit"])
	assert.Equal(t, float64(42), ev.RateLimits["requests_remaining"])
}

// --- HumanizeEvent tests ---

func TestHumanizeEvent_SessionStarted(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Session started", HumanizeEvent(model.AgentEvent{Event: "session_started"}))
}

func TestHumanizeEvent_TurnStarted(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Turn 3 started", HumanizeEvent(model.AgentEvent{Event: "turn_started", Turn: 3}))
	assert.Equal(t, "Turn started", HumanizeEvent(model.AgentEvent{Event: "turn_started"}))
}

func TestHumanizeEvent_Completed(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Session completed (5 turns)", HumanizeEvent(model.AgentEvent{Event: "completed", TurnsUsed: 5}))
	assert.Equal(t, "Session completed", HumanizeEvent(model.AgentEvent{Event: "completed"}))
}

func TestHumanizeEvent_Notification(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Agent: Working on tests", HumanizeEvent(model.AgentEvent{Event: "notification", Message: "Working on tests"}))
	assert.Equal(t, "Agent notification", HumanizeEvent(model.AgentEvent{Event: "notification"}))
}

func TestHumanizeEvent_Error(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Error: something broke", HumanizeEvent(model.AgentEvent{Event: "error", Message: "something broke"}))
}

func TestHumanizeEvent_Usage(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Usage: 1000 in / 500 out tokens", HumanizeEvent(model.AgentEvent{Event: "usage", InputTokens: 1000, OutputTokens: 500}))
}

func TestHumanizeEvent_Malformed(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Malformed agent output", HumanizeEvent(model.AgentEvent{Event: "malformed"}))
}

func TestHumanizeEvent_Unknown(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Event: custom_event", HumanizeEvent(model.AgentEvent{Event: "custom_event"}))
}

// --- RunParams container name tests ---

func TestRunParams_ContainerName(t *testing.T) {
	t.Parallel()

	name := ContainerName("sess-abc-123")
	assert.Equal(t, "mesh-sess-abc-123", name)
}

func TestRunParams_ContainerNameSanitized(t *testing.T) {
	t.Parallel()

	// Docker container names only allow [a-zA-Z0-9_.-]
	name := ContainerName("sess/with spaces@special")
	assert.Equal(t, "mesh-sess_with-spaces_special", name)
}

// --- buildDockerArgs tests ---

func TestBuildDockerArgs_AllOptions(t *testing.T) {
	t.Parallel()

	d := NewDockerRunner()
	params := RunParams{
		Image:         "my-agent:latest",
		WorkspacePath: "/tmp/workspace/PROJ-123",
		EnvVars: map[string]string{
			"API_KEY": "secret123",
			"DEBUG":   "true",
		},
		Memory:  "2g",
		CPUs:    "2",
		Network: "none",
	}

	args := d.buildDockerArgs(params, "mesh-test-container")

	assert.Contains(t, args, "run")
	assert.Contains(t, args, "-i")
	assert.Contains(t, args, "--rm")
	assert.Contains(t, args, "--name")

	// Check container name
	nameIdx := indexOf(args, "--name")
	require.Greater(t, nameIdx, -1)
	assert.Equal(t, "mesh-test-container", args[nameIdx+1])

	// Check volume mount
	assert.Contains(t, args, "-v")
	volIdx := indexOf(args, "-v")
	require.Greater(t, volIdx, -1)
	assert.Equal(t, "/tmp/workspace/PROJ-123:/workspace", args[volIdx+1])

	// Check working directory
	wIdx := indexOf(args, "-w")
	require.Greater(t, wIdx, -1)
	assert.Equal(t, "/workspace", args[wIdx+1])

	// Check memory
	assert.Contains(t, args, "--memory")
	memIdx := indexOf(args, "--memory")
	require.Greater(t, memIdx, -1)
	assert.Equal(t, "2g", args[memIdx+1])

	// Check cpus
	assert.Contains(t, args, "--cpus")
	cpuIdx := indexOf(args, "--cpus")
	require.Greater(t, cpuIdx, -1)
	assert.Equal(t, "2", args[cpuIdx+1])

	// Check network
	assert.Contains(t, args, "--network")
	netIdx := indexOf(args, "--network")
	require.Greater(t, netIdx, -1)
	assert.Equal(t, "none", args[netIdx+1])

	// Check env vars (both should be present)
	envCount := 0
	for i, a := range args {
		if a == "-e" {
			envCount++
			val := args[i+1]
			assert.True(t, val == "API_KEY=secret123" || val == "DEBUG=true",
				"unexpected env var: %s", val)
		}
	}
	assert.Equal(t, 2, envCount)

	// Image should be the last argument
	assert.Equal(t, "my-agent:latest", args[len(args)-1])
}

func TestBuildDockerArgs_MinimalOptions(t *testing.T) {
	t.Parallel()

	d := NewDockerRunner()
	params := RunParams{
		Image:         "my-agent:latest",
		WorkspacePath: "/tmp/workspace/PROJ-456",
	}

	args := d.buildDockerArgs(params, "mesh-minimal")

	assert.Contains(t, args, "run")
	assert.Contains(t, args, "-i")
	assert.Contains(t, args, "--rm")
	assert.Contains(t, args, "--name")
	assert.Contains(t, args, "-v")

	// Should NOT contain optional flags
	assert.Equal(t, -1, indexOf(args, "--memory"))
	assert.Equal(t, -1, indexOf(args, "--cpus"))
	assert.Equal(t, -1, indexOf(args, "--network"))
	assert.Equal(t, -1, indexOf(args, "-e"))

	// Image should be the last argument
	assert.Equal(t, "my-agent:latest", args[len(args)-1])
}

func TestDockerRunner_IsAvailable(t *testing.T) {
	t.Parallel()

	d := NewDockerRunner()
	// This test just verifies IsAvailable returns an error type we understand.
	// It may pass or fail depending on whether Docker is installed.
	err := d.IsAvailable()
	if err != nil {
		meshErr, ok := err.(*model.MeshError)
		if ok {
			assert.Equal(t, model.ErrDockerDaemonUnavailable, meshErr.Kind)
		}
	}
	// If Docker is available, err should be nil — both outcomes are fine for CI.
}

// indexOf returns the index of the first occurrence of target in slice, or -1.
func indexOf(slice []string, target string) int {
	for i, s := range slice {
		if s == target {
			return i
		}
	}
	return -1
}
