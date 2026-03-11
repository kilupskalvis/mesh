package model

import "fmt"

// MeshError is a typed error with a classification code for structured error handling.
type MeshError struct {
	Kind    string
	Message string
	Cause   error
}

func (e *MeshError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *MeshError) Unwrap() error { return e.Cause }

// Error kinds for workflow/config failures.
const (
	ErrMissingWorkflowFile       = "missing_workflow_file"
	ErrWorkflowParseError        = "workflow_parse_error"
	ErrWorkflowFrontMatterNotMap = "workflow_front_matter_not_a_map"
	ErrTemplateParseError        = "template_parse_error"
	ErrTemplateRenderError       = "template_render_error"
)

// Error kinds for tracker failures.
const (
	ErrUnsupportedTrackerKind   = "unsupported_tracker_kind"
	ErrMissingTrackerAPIToken   = "missing_tracker_api_token"
	ErrMissingTrackerEmail      = "missing_tracker_email"
	ErrMissingTrackerProjectKey = "missing_tracker_project_key"
	ErrJiraAPIRequest           = "jira_api_request"
	ErrJiraAPIStatus            = "jira_api_status"
	ErrJiraAPIAuth              = "jira_api_auth"
	ErrJiraAPIPermission        = "jira_api_permission"
	ErrJiraAPIRateLimit         = "jira_api_rate_limit"
	ErrJiraMalformedResponse    = "jira_malformed_response"
)

// Error kinds for GitHub tracker failures.
const (
	ErrGitHubAPIRequest        = "github_api_request"
	ErrGitHubAPIAuth           = "github_api_auth"
	ErrGitHubAPIPermission     = "github_api_permission"
	ErrGitHubAPIRateLimit      = "github_api_rate_limit"
	ErrGitHubAPINotFound       = "github_api_not_found"
	ErrGitHubMalformedResponse = "github_malformed_response"
)

// Error kinds for agent runner failures.
const (
	ErrImageNotFound           = "image_not_found"
	ErrDockerDaemonUnavailable = "docker_daemon_unavailable"
	ErrInvalidWorkspaceCwd     = "invalid_workspace_cwd"
	ErrReadTimeout             = "read_timeout"
	ErrTurnTimeout             = "turn_timeout"
	ErrContainerExit           = "container_exit"
	ErrStallDetected           = "stall_detected"
)

// NewMeshError creates a new typed MeshError.
func NewMeshError(kind, message string, cause error) *MeshError {
	return &MeshError{Kind: kind, Message: message, Cause: cause}
}
