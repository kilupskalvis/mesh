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

// continuationPrompt is appended to the system prompt for continuation runs.
const continuationPrompt = `

## Continuation

This is a continuation of previous work on this issue. A prior agent session
already made progress. Before starting fresh:

1. Run ` + "`git log --oneline -20`" + ` to see what was already committed.
2. Run ` + "`git diff`" + ` and ` + "`git status`" + ` to see uncommitted work.
3. Check the issue comments for context from the previous session.
4. Pick up where the previous session left off — do not redo completed work.

If the previous work looks complete (PR already created, tests passing),
set the label to ` + "`mesh-review`" + ` and stop.`

// DispatchIssue starts an agent for a single issue. It verifies the mesh label,
// swaps it to mesh-working, prepares the workspace, renders the prompt, launches
// the container, and starts a monitoring goroutine.
//
// isContinuation indicates whether this is a continuation retry (reuse workspace
// as-is) vs an error retry (reset worktree to origin/main).
func (o *Orchestrator) DispatchIssue(ctx context.Context, issue model.Issue, attempt *int, isContinuation bool) error {
	// 1. Label swap: mesh → mesh-working.
	if err := o.tracker.SetLabels(issue.ID, []string{"mesh-working"}); err != nil {
		return fmt.Errorf("label swap to mesh-working: %w", err)
	}

	// 2. Prepare workspace and launch container.
	err := o.launchContainer(ctx, issue, attempt, isContinuation, 0, 0)
	if err != nil {
		// Rollback label on failure.
		if rbErr := o.tracker.SetLabels(issue.ID, []string{"mesh"}); rbErr != nil {
			o.logger.Error("failed to rollback label to mesh",
				"issue", issue.Identifier, "error", rbErr)
		}
		return err
	}
	return nil
}

// DispatchRetry starts an agent for a retry. The label is already mesh-working
// so no label swap is needed. Counters are carried from the RetryEntry.
func (o *Orchestrator) DispatchRetry(ctx context.Context, retry *model.RetryEntry) error {
	issue := retry.Issue
	var attempt *int
	if retry.Attempt > 0 {
		a := retry.Attempt
		attempt = &a
	}
	return o.launchContainer(ctx, issue, attempt, retry.IsContinuation, retry.ErrorRetries, retry.ContinuationCount)
}

// launchContainer is the shared implementation for DispatchIssue and DispatchRetry.
func (o *Orchestrator) launchContainer(ctx context.Context, issue model.Issue, attempt *int, isContinuation bool, errorRetries, continuationCount int) error {
	// 1. Generate branch name.
	branchName := workspace.BranchName(issue.ID, issue.Title)

	// 2. Prepare workspace via worktree.
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
			return err
		}
		created = true
	}

	// Set branch name on issue for agent consumption.
	issue.BranchName = &branchName

	// Run after_create hook if workspace was just created.
	if created && o.config.AfterCreateHook != "" {
		if err := workspace.RunHook("after_create", o.config.AfterCreateHook, wsPath, o.config.HookTimeoutMs); err != nil {
			return err
		}
	}

	// 3. Run before_run hook.
	if o.config.BeforeRunHook != "" {
		if err := workspace.RunHook("before_run", o.config.BeforeRunHook, wsPath, o.config.HookTimeoutMs); err != nil {
			return err
		}
	}

	// 4. Render prompt template.
	prompt, err := template.Render(o.promptTmpl, issue, attempt)
	if err != nil {
		o.runAfterRunHook(wsPath) // best-effort
		return err
	}

	// 5. Build full system prompt.
	containerWorkDir := runner.ContainerWorkspacesRoot + "/" + branchName
	basePrompt := o.config.AgentSystemPrompt
	if basePrompt == "" {
		basePrompt = prompts.DefaultSystemPrompt
	}
	systemPrompt := buildSystemPrompt(basePrompt, issue, containerWorkDir, branchName)
	if isContinuation {
		systemPrompt += continuationPrompt
	}

	// 6. Build stdin payload.
	payload := model.StdinPayload{
		Issue:          issue,
		Prompt:         prompt,
		SystemPrompt:   systemPrompt,
		Attempt:        attempt,
		Workspace:      containerWorkDir,
		IsContinuation: isContinuation,
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

	// 7. Build run params — secrets stay on host, only URLs go into container.
	envVars := make(map[string]string)
	for k, v := range o.config.DockerExtraEnv {
		envVars[k] = v
	}

	proxyBase := fmt.Sprintf("http://host.docker.internal:%d", o.config.ProxyListenPort)
	envVars["ANTHROPIC_BASE_URL"] = proxyBase
	envVars["ANTHROPIC_API_KEY"] = "sk-proxy" // Placeholder — proxy injects the real key.
	envVars["PYTHONUNBUFFERED"] = "1"

	switch o.config.TrackerKind {
	case "jira":
		envVars["JIRA_ENDPOINT"] = proxyBase + "/jira"
		envVars["JIRA_PROJECT_KEY"] = o.config.TrackerProjectKey
		envVars["JIRA_ISSUE_ID"] = issue.ID
		envVars["JIRA_ISSUE_KEY"] = issue.Identifier
	case "github":
		envVars["GITHUB_REPO"] = o.config.TrackerRepo
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

	// 8. Start container.
	runCtx, cancel := context.WithCancel(ctx)
	eventCh, resultCh, err := o.runner.Run(runCtx, params)
	if err != nil {
		cancel()
		o.runAfterRunHook(wsPath) // best-effort
		return err
	}

	// 9. Add to running map with cumulative counters.
	containerID := runner.ContainerName(sessionID)
	now := time.Now()
	entry := &model.RunningEntry{
		Identifier:        issue.Identifier,
		Issue:             issue,
		SessionID:         sessionID,
		ContainerID:       containerID,
		WorkspacePath:     wsPath,
		BranchName:        branchName,
		StartedAt:         now,
		ErrorRetries:      errorRetries,
		ContinuationCount: continuationCount,
		CancelFunc:        cancel,
	}
	o.running[issue.ID] = entry

	issueLogger := logging.WithIssueContext(o.logger, issue.ID, issue.Identifier)
	sessionLogger := logging.WithSessionContext(issueLogger, sessionID, containerID)
	sessionLogger.Info("dispatched issue")
	o.addLog("info", issue.Identifier, fmt.Sprintf("Dispatched (attempt %d)", attemptVal))

	// 10. Start monitoring goroutine.
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

	o.completionCh <- completionMsg{
		IssueID:      issueID,
		Identifier:   identifier,
		Result:       result,
		Attempt:      attempt,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
		Duration:     duration,
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
