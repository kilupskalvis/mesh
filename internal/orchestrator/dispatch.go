package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kalvis/mesh/internal/logging"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/prompts"
	"github.com/kalvis/mesh/internal/runner"
	"github.com/kalvis/mesh/internal/template"
	"github.com/kalvis/mesh/internal/workspace"
)

// DispatchIssue starts an agent for a single issue. It claims the issue,
// prepares the workspace via git worktree, renders the prompt, launches the
// container, and starts a monitoring goroutine.
//
// isContinuation indicates whether this is a continuation retry (reuse workspace
// as-is) vs an error retry (reset worktree to origin/main).
func (o *Orchestrator) DispatchIssue(ctx context.Context, issue model.Issue, attempt *int, isContinuation bool) error {
	// 1. Mark as claimed.
	o.claimed[issue.ID] = true

	// 2. Generate branch name.
	branchName := workspace.BranchName(issue.ID, issue.Title)

	// 3. Prepare workspace via worktree.
	var wsPath string
	var created bool

	if o.workspace.WorktreeExists(branchName) {
		wsPath = o.workspace.WorktreePath(branchName)
		if !isContinuation {
			// Error retry: fetch and reset the worktree to a clean state.
			if token := o.mintToken(); token != "" {
				_ = o.workspace.Fetch(token)
			}
			if err := o.workspace.ResetWorktree(branchName); err != nil {
				delete(o.claimed, issue.ID)
				return err
			}
		}
		// Continuation: reuse as-is.
	} else {
		// First dispatch: fetch and create worktree.
		if token := o.mintToken(); token != "" {
			_ = o.workspace.Fetch(token)
		}
		var err error
		wsPath, err = o.workspace.CreateWorktree(branchName)
		if err != nil {
			delete(o.claimed, issue.ID)
			return err
		}
		created = true
	}

	// Set branch name on issue for agent consumption.
	issue.BranchName = &branchName

	// Run after_create hook if workspace was just created.
	if created && o.config.AfterCreateHook != "" {
		if err := workspace.RunHook("after_create", o.config.AfterCreateHook, wsPath, o.config.HookTimeoutMs); err != nil {
			delete(o.claimed, issue.ID)
			return err
		}
	}

	// 4. Run before_run hook.
	if o.config.BeforeRunHook != "" {
		if err := workspace.RunHook("before_run", o.config.BeforeRunHook, wsPath, o.config.HookTimeoutMs); err != nil {
			delete(o.claimed, issue.ID)
			return err
		}
	}

	// 5. Render prompt template.
	prompt, err := template.Render(o.promptTmpl, issue, attempt)
	if err != nil {
		o.runAfterRunHook(wsPath) // best-effort
		delete(o.claimed, issue.ID)
		return err
	}

	// 6. Build full system prompt.
	containerWorkDir := runner.ContainerWorkspacesRoot + "/" + branchName
	basePrompt := o.config.AgentSystemPrompt
	if basePrompt == "" {
		basePrompt = prompts.DefaultSystemPrompt
	}
	systemPrompt := buildSystemPrompt(basePrompt, issue, containerWorkDir, branchName)

	// 7. Build stdin payload.
	payload := model.StdinPayload{
		Issue:        issue,
		Prompt:       prompt,
		SystemPrompt: systemPrompt,
		Attempt:      attempt,
		Workspace:    containerWorkDir,
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

	// 8. Build run params — secrets stay on host, only URLs go into container.
	envVars := make(map[string]string)
	for k, v := range o.config.DockerExtraEnv {
		envVars[k] = v
	}

	proxyBase := fmt.Sprintf("http://host.docker.internal:%d", o.config.ProxyListenPort)
	envVars["ANTHROPIC_BASE_URL"] = proxyBase
	envVars["ANTHROPIC_API_KEY"] = "sk-proxy"  // Placeholder — proxy injects the real key.
	envVars["PYTHONUNBUFFERED"] = "1"

	switch o.config.TrackerKind {
	case "jira":
		envVars["JIRA_ENDPOINT"] = proxyBase + "/jira"
		envVars["JIRA_PROJECT_KEY"] = o.config.TrackerProjectKey
		envVars["JIRA_ISSUE_ID"] = issue.ID
		envVars["JIRA_ISSUE_KEY"] = issue.Identifier
	case "github":
		envVars["GITHUB_REPO"] = o.config.TrackerOwner + "/" + o.config.TrackerRepo
		envVars["GITHUB_OWNER"] = o.config.TrackerOwner
		envVars["GITHUB_ISSUE_NUMBER"] = issue.ID
		if issue.URL != nil {
			envVars["GITHUB_ISSUE_URL"] = *issue.URL
		}
		ghProxyBase := fmt.Sprintf("http://host.docker.internal:%d", o.config.ProxyListenPort+1)
		envVars["GITHUB_ENDPOINT"] = ghProxyBase + "/github"
		envVars["GITHUB_WORKSPACE"] = wsPath // host-side path for git push handler
	}

	attemptVal := 0
	if attempt != nil {
		attemptVal = *attempt
	}
	sessionID := formatSessionID(issue.Identifier, attemptVal)
	params := runner.RunParams{
		Image:            o.config.AgentImage,
		WorkspaceRoot:    o.workspace.Root,
		ContainerWorkDir: containerWorkDir,
		StdinPayload:     payload,
		EnvVars:          envVars,
		Memory:           o.config.DockerMemory,
		CPUs:             o.config.DockerCPUs,
		Network:          o.config.DockerNetwork,
		ReadTimeoutMs:    o.config.ReadTimeoutMs,
	}

	// 9. Start container.
	runCtx, cancel := context.WithCancel(ctx)
	eventCh, resultCh, err := o.runner.Run(runCtx, params)
	if err != nil {
		cancel()
		o.runAfterRunHook(wsPath) // best-effort
		delete(o.claimed, issue.ID)
		return err
	}

	// 10. Move from claimed to running.
	containerID := runner.ContainerName(sessionID)
	now := time.Now()
	entry := &model.RunningEntry{
		Identifier:    issue.Identifier,
		Issue:         issue,
		SessionID:     sessionID,
		ContainerID:   containerID,
		WorkspacePath: wsPath,
		BranchName:    branchName,
		StartedAt:     now,
		CancelFunc:    cancel,
	}
	delete(o.claimed, issue.ID)
	o.running[issue.ID] = entry

	issueLogger := logging.WithIssueContext(o.logger, issue.ID, issue.Identifier)
	sessionLogger := logging.WithSessionContext(issueLogger, sessionID, containerID)
	sessionLogger.Info("dispatched issue")
	o.addLog("info", issue.Identifier, fmt.Sprintf("Dispatched (attempt %d)", attemptVal))

	// 11. Start monitoring goroutine.
	go o.monitorWorker(issue.ID, issue.Identifier, attemptVal, eventCh, resultCh, entry.StartedAt, sessionLogger)

	return nil
}

// mintToken returns a fresh GitHub installation token, or "" if unavailable.
func (o *Orchestrator) mintToken() string {
	if o.githubTokenProvider == nil {
		return ""
	}
	token, err := o.githubTokenProvider()
	if err != nil {
		o.logger.Warn("failed to mint GitHub token for workspace", "error", err)
		return ""
	}
	return token
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

// buildSystemPrompt assembles the complete system prompt from a base prompt
// and task context. Empty description and labels are omitted.
func buildSystemPrompt(base string, issue model.Issue, workspace, branch string) string {
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n---\n\n## Current Task\n")
	fmt.Fprintf(&b, "Issue: %s — %s\n", issue.Identifier, issue.Title)
	fmt.Fprintf(&b, "Workspace: %s\n", workspace)
	fmt.Fprintf(&b, "Branch: %s\n", branch)
	if issue.Description != nil && *issue.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", *issue.Description)
	}
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	return b.String()
}

