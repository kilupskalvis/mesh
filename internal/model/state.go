package model

import "time"

// RunStatus represents the current phase of a run attempt.
type RunStatus string

const (
	RunStatusPreparingWorkspace RunStatus = "PreparingWorkspace"
	RunStatusBuildingPrompt     RunStatus = "BuildingPrompt"
	RunStatusLaunchingContainer RunStatus = "LaunchingContainer"
	RunStatusWaitingForFirst    RunStatus = "WaitingForFirstEvent"
	RunStatusStreamingEvents    RunStatus = "StreamingEvents"
	RunStatusFinishing          RunStatus = "Finishing"
	RunStatusSucceeded          RunStatus = "Succeeded"
	RunStatusFailed             RunStatus = "Failed"
	RunStatusTimedOut           RunStatus = "TimedOut"
	RunStatusStalled            RunStatus = "Stalled"
	RunStatusCanceled           RunStatus = "CanceledByReconciliation"
)

// RunningEntry tracks state for a single running agent container.
type RunningEntry struct {
	Identifier               string     `json:"identifier"`
	Issue                    Issue      `json:"issue"`
	SessionID                string     `json:"session_id"`
	ContainerID              string     `json:"container_id"`
	WorkspacePath            string     `json:"workspace_path"`
	BranchName               string     `json:"branch_name"`
	LastAgentMessage         string     `json:"last_agent_message"`
	LastAgentEvent           string     `json:"last_agent_event"`
	LastAgentTimestamp       *time.Time `json:"last_agent_timestamp"`
	AgentInputTokens         int64      `json:"agent_input_tokens"`
	AgentOutputTokens        int64      `json:"agent_output_tokens"`
	AgentTotalTokens         int64      `json:"agent_total_tokens"`
	LastReportedInputTokens  int64      `json:"last_reported_input_tokens"`
	LastReportedOutputTokens int64      `json:"last_reported_output_tokens"`
	LastReportedTotalTokens  int64      `json:"last_reported_total_tokens"`
	RetryAttempt             int        `json:"retry_attempt"`
	StartedAt                time.Time  `json:"started_at"`
	TurnCount                int        `json:"turn_count"`
	CancelFunc               func()     `json:"-"`
}

// RetryEntry is a scheduled retry for an issue.
type RetryEntry struct {
	IssueID        string  `json:"issue_id"`
	Identifier     string  `json:"identifier"`
	Attempt        int     `json:"attempt"`
	DueAtMs        int64   `json:"due_at_ms"`
	Error          *string `json:"error"`
	IsContinuation bool    `json:"is_continuation"`
	CancelFunc     func()  `json:"-"`
}

// AgentTotals tracks aggregate token usage and runtime across all sessions.
type AgentTotals struct {
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	TotalTokens    int64   `json:"total_tokens"`
	SecondsRunning float64 `json:"seconds_running"`
}

// RateLimitSnapshot holds the latest rate-limit data from agent events.
type RateLimitSnapshot struct {
	RequestsLimit     int    `json:"requests_limit,omitempty"`
	RequestsRemaining int    `json:"requests_remaining,omitempty"`
	RequestsReset     string `json:"requests_reset,omitempty"`
	TokensLimit       int    `json:"tokens_limit,omitempty"`
	TokensRemaining   int    `json:"tokens_remaining,omitempty"`
	TokensReset       string `json:"tokens_reset,omitempty"`
}

// AgentEvent represents a parsed event from the Python agent's stdout stream.
type AgentEvent struct {
	Event        string         `json:"event"`
	Timestamp    string         `json:"ts"`
	SessionID    string         `json:"session_id,omitempty"`
	Turn         int            `json:"turn,omitempty"`
	Message      string         `json:"message,omitempty"`
	Code         string         `json:"code,omitempty"`
	InputTokens  int64          `json:"input_tokens,omitempty"`
	OutputTokens int64          `json:"output_tokens,omitempty"`
	TotalTokens  int64          `json:"total_tokens,omitempty"`
	TurnsUsed    int            `json:"turns_used,omitempty"`
	RateLimits   map[string]any `json:"rate_limits,omitempty"`
}

// CompletedEntry records the outcome of a finished agent run for the TUI history.
type CompletedEntry struct {
	Identifier  string        `json:"identifier"`
	Title       string        `json:"title"`
	Status      string        `json:"status"` // "success", "error", "cancelled"
	Error       string        `json:"error,omitempty"`
	TotalTokens int64         `json:"total_tokens"`
	Duration    time.Duration `json:"duration"`
	CompletedAt time.Time     `json:"completed_at"`
	Attempt     int           `json:"attempt"`
}

// LogEntry is a single entry in the TUI activity feed.
type LogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Identifier string    `json:"identifier"`
	Message    string    `json:"message"`
	Level      string    `json:"level"` // "info", "warn", "error"
}

// StdinPayload is the JSON object written to the agent container's stdin.
type StdinPayload struct {
	Issue        Issue              `json:"issue"`
	Prompt       string             `json:"prompt"`
	SystemPrompt string             `json:"system_prompt"`
	Attempt      *int               `json:"attempt"`
	Workspace    string             `json:"workspace"`
	Config       StdinPayloadConfig `json:"config"`
}

// StdinPayloadConfig carries agent config values to the container.
type StdinPayloadConfig struct {
	TurnTimeoutMs  int      `json:"turn_timeout_ms"`
	MaxTurns       int      `json:"max_turns"`
	Model          string   `json:"model,omitempty"`
	TerminalStates []string `json:"terminal_states,omitempty"`
	ApprovalPolicy string   `json:"approval_policy,omitempty"`
	Sandbox        string   `json:"sandbox,omitempty"`
	SandboxPolicy  string   `json:"sandbox_policy,omitempty"`
}
