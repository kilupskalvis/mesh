package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kalvis/mesh/internal/logging"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/proxy"
	"github.com/kalvis/mesh/internal/runner"
	"github.com/kalvis/mesh/internal/template"
	"github.com/kalvis/mesh/internal/workspace"
)

// DispatchIssue starts an agent for a single issue. It claims the issue,
// prepares the workspace, renders the prompt, launches the container, and
// starts a monitoring goroutine.
func (o *Orchestrator) DispatchIssue(ctx context.Context, issue model.Issue, attempt *int) error {
	// 1. Mark as claimed.
	o.claimed[issue.ID] = true

	// 2. Prepare workspace.
	wsPath, created, err := o.workspace.CreateForIssue(issue.Identifier)
	if err != nil {
		delete(o.claimed, issue.ID)
		return err
	}

	// Run after_create hook if workspace was just created.
	if created && o.config.AfterCreateHook != "" {
		if err := workspace.RunHook("after_create", o.config.AfterCreateHook, wsPath, o.config.HookTimeoutMs); err != nil {
			delete(o.claimed, issue.ID)
			return err
		}
	}

	// 3. Run before_run hook.
	if o.config.BeforeRunHook != "" {
		if err := workspace.RunHook("before_run", o.config.BeforeRunHook, wsPath, o.config.HookTimeoutMs); err != nil {
			delete(o.claimed, issue.ID)
			return err
		}
	}

	// 4. Render prompt template.
	prompt, err := template.Render(o.promptTmpl, issue, attempt)
	if err != nil {
		o.runAfterRunHook(wsPath) // best-effort
		delete(o.claimed, issue.ID)
		return err
	}

	// 5. Build stdin payload.
	payload := model.StdinPayload{
		Issue:     issue,
		Prompt:    prompt,
		Attempt:   attempt,
		Workspace: wsPath,
		Config: model.StdinPayloadConfig{
			TurnTimeoutMs:  o.config.TurnTimeoutMs,
			MaxTurns:       o.config.MaxTurns,
			Model:          o.config.AgentModel,
			TerminalStates: o.config.TerminalStates,
			ApprovalPolicy: o.config.ApprovalPolicy,
			Sandbox:        o.config.Sandbox,
			SandboxPolicy:  o.config.SandboxPolicy,
		},
	}

	// 6. Build run params — secrets stay on host, only URLs go into container.
	envVars := make(map[string]string)
	for k, v := range o.config.DockerExtraEnv {
		envVars[k] = v
	}

	proxyBase := fmt.Sprintf("http://host.docker.internal:%d", o.config.ProxyListenPort)
	envVars["ANTHROPIC_BASE_URL"] = proxyBase
	envVars["PYTHONUNBUFFERED"] = "1"

	switch o.config.TrackerKind {
	case "jira":
		envVars["JIRA_ENDPOINT"] = proxyBase + "/jira"
		envVars["JIRA_PROJECT_KEY"] = o.config.TrackerProjectKey
		envVars["JIRA_ISSUE_ID"] = issue.ID
		envVars["JIRA_ISSUE_KEY"] = issue.Identifier
	case "github":
		envVars["GITHUB_REPO"] = o.config.TrackerOwner + "/" + o.config.TrackerRepo
		envVars["GITHUB_ISSUE_NUMBER"] = issue.ID
		if issue.URL != nil {
			envVars["GITHUB_ISSUE_URL"] = *issue.URL
		}
	}

	// GitHub: mint short-lived token or fall back to env.
	if ghToken, err := o.mintGitHubToken(); err == nil && ghToken != "" {
		envVars["GITHUB_TOKEN"] = ghToken
	}

	attemptVal := 0
	if attempt != nil {
		attemptVal = *attempt
	}
	sessionID := formatSessionID(issue.Identifier, attemptVal)
	params := runner.RunParams{
		Image:         o.config.AgentImage,
		WorkspacePath: wsPath,
		StdinPayload:  payload,
		EnvVars:       envVars,
		Memory:        o.config.DockerMemory,
		CPUs:          o.config.DockerCPUs,
		Network:       o.config.DockerNetwork,
		ReadTimeoutMs: o.config.ReadTimeoutMs,
	}

	// 7. Start container.
	runCtx, cancel := context.WithCancel(ctx)
	eventCh, resultCh, err := o.runner.Run(runCtx, params)
	if err != nil {
		cancel()
		o.runAfterRunHook(wsPath) // best-effort
		delete(o.claimed, issue.ID)
		return err
	}

	// 8. Move from claimed to running.
	containerID := runner.ContainerName(sessionID)
	now := time.Now()
	entry := &model.RunningEntry{
		Identifier:    issue.Identifier,
		Issue:         issue,
		SessionID:     sessionID,
		ContainerID:   containerID,
		WorkspacePath: wsPath,
		StartedAt:     now,
		CancelFunc:    cancel,
	}
	delete(o.claimed, issue.ID)
	o.running[issue.ID] = entry

	issueLogger := logging.WithIssueContext(o.logger, issue.ID, issue.Identifier)
	sessionLogger := logging.WithSessionContext(issueLogger, sessionID, containerID)
	sessionLogger.Info("dispatched issue")

	// 9. Start monitoring goroutine.
	go o.monitorWorker(issue.ID, issue.Identifier, attemptVal, eventCh, resultCh, entry.StartedAt, sessionLogger)

	return nil
}

// runAfterRunHook runs the after_run hook (best-effort, failure is logged and ignored).
func (o *Orchestrator) runAfterRunHook(wsPath string) {
	if o.config.AfterRunHook == "" || wsPath == "" {
		return
	}
	if err := workspace.RunHook("after_run", o.config.AfterRunHook, wsPath, o.config.HookTimeoutMs); err != nil {
		o.logger.Warn("after_run hook failed", "error", err)
	}
}

// monitorWorker reads events from the agent and sends a completion message
// when the agent finishes. The logger carries issue and session context fields.
func (o *Orchestrator) monitorWorker(
	issueID, identifier string,
	attempt int,
	eventCh <-chan model.AgentEvent,
	resultCh <-chan runner.RunResult,
	startedAt time.Time,
	logger *slog.Logger,
) {
	var (
		inputTokens  int64
		outputTokens int64
		totalTokens  int64
	)

	// Read events until channel closes.
	for ev := range eventCh {
		// Accumulate token counts for completion delta.
		if ev.InputTokens > 0 {
			inputTokens = ev.InputTokens
		}
		if ev.OutputTokens > 0 {
			outputTokens = ev.OutputTokens
		}
		if ev.TotalTokens > 0 {
			totalTokens = ev.TotalTokens
		}

		// Send live event update to orchestrator loop.
		select {
		case o.eventUpdateCh <- eventUpdateMsg{IssueID: issueID, Event: ev}:
		default:
			// Drop if channel is full — non-blocking to avoid backpressure.
		}
	}

	// Wait for result.
	result := <-resultCh

	duration := time.Since(startedAt)

	// Determine if this was a normal exit (continuation) or error.
	isContinuation := result.Error == nil && result.ExitCode == 0

	o.completionCh <- completionMsg{
		IssueID:        issueID,
		Identifier:     identifier,
		Result:         result,
		Attempt:        attempt,
		IsContinuation: isContinuation,
		InputTokens:    inputTokens,
		OutputTokens:   outputTokens,
		TotalTokens:    totalTokens,
		Duration:       duration,
	}
}

// mintGitHubToken returns a short-lived GitHub App installation token.
// Returns ("", nil) if GitHub App is not configured.
func (o *Orchestrator) mintGitHubToken() (string, error) {
	if o.config.GitHubAppID == "" || o.config.GitHubAppPrivateKeyPath == "" {
		// Not configured — fall back to GITHUB_TOKEN from env.
		if v := os.Getenv("GITHUB_TOKEN"); v != "" {
			return v, nil
		}
		return "", nil
	}

	keyPEM, err := os.ReadFile(o.config.GitHubAppPrivateKeyPath)
	if err != nil {
		return "", fmt.Errorf("reading GitHub App private key: %w", err)
	}

	return proxy.MintInstallationToken(
		"https://api.github.com",
		o.config.GitHubAppID,
		o.config.GitHubInstallationID,
		keyPEM,
	)
}
