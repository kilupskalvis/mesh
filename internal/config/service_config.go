package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/kalvis/mesh/internal/model"
)

// ServiceConfig holds typed, validated runtime configuration derived from WORKFLOW.md
// front matter plus environment variable resolution.
type ServiceConfig struct {
	// Tracker
	TrackerKind       string
	TrackerEndpoint   string
	TrackerEmail      string
	TrackerAPIToken   string
	TrackerProjectKey string
	TrackerOwner          string
	TrackerRepo           string
	TrackerLabel          string
	GitHubAppID           string
	GitHubAppPrivateKey   string // path to .pem file
	GitHubInstallationID  string
	ActiveStates          []string
	TerminalStates        []string

	// Polling
	PollIntervalMs int

	// Workspace
	WorkspaceRoot    string
	WorkspaceRepoURL string

	// Hooks
	AfterCreateHook  string
	BeforeRunHook    string
	AfterRunHook     string
	BeforeRemoveHook string
	HookTimeoutMs    int

	// Agent
	AgentImage           string
	MaxConcurrentAgents  int
	MaxTurns             int
	MaxRetryBackoffMs    int
	MaxConcurrentByState map[string]int
	TurnTimeoutMs        int
	ReadTimeoutMs        int
	StallTimeoutMs       int
	DockerMemory         string
	DockerCPUs           string
	DockerNetwork        string
	DockerExtraEnv       map[string]string
	AgentModel           string
	AgentSystemPrompt    string
	ApprovalPolicy       string
	Sandbox              string
	SandboxPolicy        string

	// Agent API key
	ClaudeAPIKey string

	// Observability
	SentryDSN string

	// Proxy (credential isolation)
	ProxyListenPort int

	// Server (optional HTTP extension)
	ServerPort int
}

// NewServiceConfig constructs a validated ServiceConfig from raw WORKFLOW.md front matter.
func NewServiceConfig(raw map[string]any) (*ServiceConfig, error) {
	cfg := &ServiceConfig{}

	// Tracker
	cfg.TrackerKind = getNestedString(raw, "tracker", "kind")
	if cfg.TrackerKind == "" {
		return nil, model.NewMeshError(model.ErrUnsupportedTrackerKind, "tracker.kind is required", nil)
	}
	if cfg.TrackerKind != "jira" && cfg.TrackerKind != "github" {
		return nil, model.NewMeshError(model.ErrUnsupportedTrackerKind,
			fmt.Sprintf("unsupported tracker kind: %q (supported: jira, github)", cfg.TrackerKind), nil)
	}

	switch cfg.TrackerKind {
	case "jira":
		cfg.TrackerEndpoint = getNestedString(raw, "tracker", "endpoint")
		if cfg.TrackerEndpoint == "" {
			return nil, model.NewMeshError(model.ErrMissingTrackerProjectKey,
				"tracker.endpoint is required for jira", nil)
		}

		rawToken := getNestedString(raw, "tracker", "api_token")
		if rawToken == "" {
			rawToken = "$JIRA_API_TOKEN"
		}
		token, ok := resolveEnvVar(rawToken)
		if !ok {
			return nil, model.NewMeshError(model.ErrMissingTrackerAPIToken,
				"tracker.api_token is missing or empty after resolution", nil)
		}
		cfg.TrackerAPIToken = token

		rawEmail := getNestedString(raw, "tracker", "email")
		if rawEmail == "" {
			rawEmail = "$JIRA_EMAIL"
		}
		email, ok := resolveEnvVar(rawEmail)
		if !ok {
			return nil, model.NewMeshError(model.ErrMissingTrackerEmail,
				"tracker.email is missing or empty after resolution", nil)
		}
		cfg.TrackerEmail = email

		cfg.TrackerProjectKey = getNestedString(raw, "tracker", "project_key")
		if cfg.TrackerProjectKey == "" {
			return nil, model.NewMeshError(model.ErrMissingTrackerProjectKey,
				"tracker.project_key is required for jira", nil)
		}

	case "github":
		cfg.TrackerOwner = getNestedString(raw, "tracker", "owner")
		if cfg.TrackerOwner == "" {
			return nil, model.NewMeshError(model.ErrUnsupportedTrackerKind,
				"tracker.owner is required for github", nil)
		}
		cfg.TrackerRepo = getNestedString(raw, "tracker", "repo")
		if cfg.TrackerRepo == "" {
			return nil, model.NewMeshError(model.ErrUnsupportedTrackerKind,
				"tracker.repo is required for github", nil)
		}
		cfg.TrackerLabel = getNestedString(raw, "tracker", "label")

		rawAppID := getNestedString(raw, "tracker", "app_id")
		if rawAppID == "" {
			return nil, model.NewMeshError(model.ErrMissingTrackerAPIToken,
				"tracker.app_id is required for github", nil)
		}
		appID, ok := resolveEnvVar(rawAppID)
		if !ok {
			return nil, model.NewMeshError(model.ErrMissingTrackerAPIToken,
				"tracker.app_id is empty after resolution", nil)
		}
		cfg.GitHubAppID = appID

		rawInstID := getNestedString(raw, "tracker", "installation_id")
		if rawInstID == "" {
			return nil, model.NewMeshError(model.ErrMissingTrackerAPIToken,
				"tracker.installation_id is required for github", nil)
		}
		instID, ok := resolveEnvVar(rawInstID)
		if !ok {
			return nil, model.NewMeshError(model.ErrMissingTrackerAPIToken,
				"tracker.installation_id is empty after resolution", nil)
		}
		cfg.GitHubInstallationID = instID
		rawKeyPath := getNestedString(raw, "tracker", "private_key_path")
		if rawKeyPath == "" {
			return nil, model.NewMeshError(model.ErrMissingTrackerAPIToken,
				"tracker.private_key_path is required for github", nil)
		}
		cfg.GitHubAppPrivateKey = expandHome(rawKeyPath)
	}

	// State lists — defaults depend on tracker kind
	trackerMap := getNestedMap(raw, "tracker")
	var activeRaw, terminalRaw any
	if trackerMap != nil {
		activeRaw = trackerMap["active_states"]
		terminalRaw = trackerMap["terminal_states"]
	}
	var defaultActive, defaultTerminal []string
	switch cfg.TrackerKind {
	case "github":
		defaultActive = []string{"open"}
		defaultTerminal = []string{"closed"}
	default:
		defaultActive = []string{"to do", "in progress"}
		defaultTerminal = []string{"done", "cancelled", "canceled", "closed", "duplicate"}
	}
	cfg.ActiveStates = parseStateList(activeRaw, defaultActive)
	cfg.TerminalStates = parseStateList(terminalRaw, defaultTerminal)

	// Polling
	cfg.PollIntervalMs = getNestedInt(raw, 30000, "polling", "interval_ms")

	// Workspace (supports $VAR and ~ expansion)
	wsRoot := getNestedString(raw, "workspace", "root")
	if wsRoot == "" {
		wsRoot = os.TempDir() + "/mesh_workspaces"
	}
	if resolved, ok := resolveEnvVar(wsRoot); ok {
		wsRoot = resolved
	}
	cfg.WorkspaceRoot = expandHome(wsRoot)

	// Workspace repo URL (for bare clone). Must be set explicitly in WORKFLOW.md.
	wsRepoURL := getNestedString(raw, "workspace", "repo_url")
	if resolved, ok := resolveEnvVar(wsRepoURL); ok {
		wsRepoURL = resolved
	}
	cfg.WorkspaceRepoURL = wsRepoURL

	// Hooks
	hooksMap := getNestedMap(raw, "hooks")
	if hooksMap != nil {
		if v, ok := hooksMap["after_create"].(string); ok {
			cfg.AfterCreateHook = v
		}
		if v, ok := hooksMap["before_run"].(string); ok {
			cfg.BeforeRunHook = v
		}
		if v, ok := hooksMap["after_run"].(string); ok {
			cfg.AfterRunHook = v
		}
		if v, ok := hooksMap["before_remove"].(string); ok {
			cfg.BeforeRemoveHook = v
		}
	}
	cfg.HookTimeoutMs = getNestedInt(raw, 60000, "hooks", "timeout_ms")
	if cfg.HookTimeoutMs <= 0 {
		cfg.HookTimeoutMs = 60000
	}

	// Agent — spec uses "agent.command", implementation also supports "agent.image"
	cfg.AgentImage = getNestedString(raw, "agent", "command")
	if cfg.AgentImage == "" {
		cfg.AgentImage = getNestedString(raw, "agent", "image")
	}
	if cfg.AgentImage == "" {
		cfg.AgentImage = "mesh-agent:latest"
	}
	cfg.MaxConcurrentAgents = getNestedInt(raw, 10, "agent", "max_concurrent_agents")
	cfg.MaxTurns = getNestedInt(raw, 20, "agent", "max_turns")
	cfg.MaxRetryBackoffMs = getNestedInt(raw, 300000, "agent", "max_retry_backoff_ms")
	cfg.TurnTimeoutMs = getNestedInt(raw, 3600000, "agent", "turn_timeout_ms")
	cfg.ReadTimeoutMs = getNestedInt(raw, 300000, "agent", "read_timeout_ms")
	cfg.StallTimeoutMs = getNestedInt(raw, 300000, "agent", "stall_timeout_ms")

	// Per-state concurrency
	cfg.MaxConcurrentByState = make(map[string]int)
	byStateRaw := getNestedMap(raw, "agent", "max_concurrent_agents_by_state")
	for k, v := range byStateRaw {
		var intVal int
		switch n := v.(type) {
		case int:
			intVal = n
		case float64:
			intVal = int(n)
		default:
			continue
		}
		if intVal > 0 {
			cfg.MaxConcurrentByState[strings.ToLower(strings.TrimSpace(k))] = intVal
		}
	}

	// Docker options
	dockerOpts := getNestedMap(raw, "agent", "docker_options")
	if dockerOpts != nil {
		if v, ok := dockerOpts["memory"].(string); ok {
			cfg.DockerMemory = v
		}
		if v, ok := dockerOpts["cpus"].(string); ok {
			cfg.DockerCPUs = v
		}
		if v, ok := dockerOpts["network"].(string); ok {
			cfg.DockerNetwork = v
		}
		if extraEnv, ok := dockerOpts["extra_env"].(map[string]any); ok {
			cfg.DockerExtraEnv = make(map[string]string)
			for ek, ev := range extraEnv {
				if s, ok := ev.(string); ok {
					cfg.DockerExtraEnv[ek] = s
				}
			}
		}
	}

	// Agent model
	cfg.AgentModel = getNestedString(raw, "agent", "model")
	if cfg.AgentModel == "" {
		cfg.AgentModel = "claude-sonnet-4-6"
	}

	// Agent system prompt (optional override, defaults to tracker-kind prompt)
	cfg.AgentSystemPrompt = getNestedString(raw, "agent", "system_prompt")

	// Agent API key
	rawAPIKey := getNestedString(raw, "agent", "api_key")
	if rawAPIKey == "" {
		rawAPIKey = "$CLAUDE_API_KEY"
	}
	apiKey, _ := resolveEnvVar(rawAPIKey)
	cfg.ClaudeAPIKey = apiKey

	// Agent policy (implementation-defined defaults)
	cfg.ApprovalPolicy = getNestedString(raw, "agent", "approval_policy")
	cfg.Sandbox = getNestedString(raw, "agent", "sandbox")
	cfg.SandboxPolicy = getNestedString(raw, "agent", "sandbox_policy")

	// Server (optional HTTP extension)
	cfg.ServerPort = getNestedInt(raw, -1, "server", "port")

	// Observability
	rawDSN := getNestedString(raw, "observability", "sentry_dsn")
	if rawDSN != "" {
		dsn, _ := resolveEnvVar(rawDSN)
		cfg.SentryDSN = dsn
	}

	// Proxy (credential isolation)
	cfg.ProxyListenPort = getNestedInt(raw, 9480, "proxy", "listen_port")

	return cfg, nil
}
