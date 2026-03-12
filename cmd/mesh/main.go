// Package main is the CLI entrypoint for the Mesh service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kalvis/mesh/internal/config"
	"github.com/kalvis/mesh/internal/logging"
	"github.com/kalvis/mesh/internal/model"
	"github.com/kalvis/mesh/internal/orchestrator"
	"github.com/kalvis/mesh/internal/proxy"
	"github.com/kalvis/mesh/internal/runner"
	meshsentry "github.com/kalvis/mesh/internal/sentry"
	"github.com/kalvis/mesh/internal/server"
	"github.com/kalvis/mesh/internal/tracker"
	"github.com/kalvis/mesh/internal/tui"
	"github.com/kalvis/mesh/internal/workspace"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "mesh: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// 1. Parse CLI args
	fs := flag.NewFlagSet("mesh", flag.ContinueOnError)
	portFlag := fs.Int("port", -1, "HTTP server port (overrides server.port in WORKFLOW.md)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	workflowPath := "./WORKFLOW.md"
	if fs.NArg() > 0 {
		workflowPath = fs.Arg(0)
	}

	// 2. Load .env if present (populates environment before config resolution)
	if err := config.LoadDotEnv(".env"); err != nil {
		return fmt.Errorf("failed to load .env: %w", err)
	}

	// 3. Load and validate WORKFLOW.md
	wf, err := config.LoadWorkflow(workflowPath)
	if err != nil {
		return fmt.Errorf("failed to load workflow: %w", err)
	}

	cfg, err := config.NewServiceConfig(wf.Config)
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if err := config.ValidateDispatchConfig(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Initialize logger
	logger := logging.NewLogger(slog.LevelInfo)

	// 3. Start file watcher (wired to orchestrator after step 8)
	var orch *orchestrator.Orchestrator // forward-declared for watcher closure
	watcher, err := config.NewWatcher(workflowPath,
		func(newWf *model.WorkflowDefinition) {
			newCfg, err := config.NewServiceConfig(newWf.Config)
			if err != nil {
				logger.Error("workflow reload: invalid config", "error", err)
				return
			}
			if err := config.ValidateDispatchConfig(newCfg); err != nil {
				logger.Error("workflow reload: config validation failed", "error", err)
				return
			}
			if orch != nil {
				orch.ReloadConfig(newCfg, newWf.PromptTemplate)
			}
			logger.Info("workflow reloaded", "path", workflowPath)
		},
		func(err error) {
			logger.Error("workflow reload failed", "error", err)
		},
		logger,
	)
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	if err := watcher.Start(); err != nil {
		return fmt.Errorf("failed to start file watcher: %w", err)
	}
	defer watcher.Stop()

	// 4. Initialize Sentry
	sentryClient, err := meshsentry.Init(cfg.SentryDSN)
	if err != nil {
		return fmt.Errorf("failed to init sentry: %w", err)
	}
	defer sentryClient.Flush(2 * time.Second)

	// 5. Initialize tracker client and token provider
	var trackerClient orchestrator.TrackerClient
	var githubTokenProvider func() (string, error)
	switch cfg.TrackerKind {
	case "jira":
		trackerClient = tracker.NewJiraClient(
			cfg.TrackerEndpoint,
			cfg.TrackerEmail,
			cfg.TrackerAPIToken,
			cfg.TrackerProjectKey,
			30000,
		)
	case "github":
		keyPEM, err := os.ReadFile(cfg.GitHubAppPrivateKey)
		if err != nil {
			return fmt.Errorf("reading GitHub App private key: %w", err)
		}
		tokenMgr := tracker.NewGitHubTokenManager(cfg.GitHubAppID, cfg.GitHubInstallationID, keyPEM)
		githubTokenProvider = tokenMgr.Token

		ghClient := tracker.NewGitHubClient(
			cfg.TrackerOwner,
			cfg.TrackerRepo,
			tokenMgr.Token,
			30000,
		)
		if cfg.TrackerLabel != "" {
			ghClient.SetLabel(cfg.TrackerLabel)
		}
		trackerClient = ghClient
	}

	// 6. Verify Docker daemon
	dockerRunner := runner.NewDockerRunner()
	if err := dockerRunner.IsAvailable(); err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}

	// 7. Workspace manager
	ws := workspace.NewManager(cfg.WorkspaceRoot)
	ws.RepoURL = cfg.WorkspaceRepoURL

	// Initialize bare clone for worktree operations if repo URL is configured.
	if ws.RepoURL != "" {
		if err := ws.EnsureBase(); err != nil {
			return fmt.Errorf("workspace base setup: %w", err)
		}
	}

	// 7b. Start credential proxy.
	proxyCfg := &proxy.Config{
		ListenPort:   cfg.ProxyListenPort,
		ClaudeAPIKey: cfg.ClaudeAPIKey,
		JiraEndpoint: cfg.TrackerEndpoint,
		JiraEmail:    cfg.TrackerEmail,
		JiraAPIToken: cfg.TrackerAPIToken,
		SentryDSN:    cfg.SentryDSN,
		PidFile:      filepath.Join(os.TempDir(), "mesh-proxy.pid"),
		ErrorLog:     filepath.Join(os.TempDir(), "mesh-proxy-error.log"),
	}

	proxyInstance, err := proxy.New(proxyCfg)
	if err != nil {
		return fmt.Errorf("failed to create proxy: %w", err)
	}
	if err := proxyInstance.Start(); err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}
	logger.Info("credential proxy started", "port", cfg.ProxyListenPort)
	defer proxyInstance.Stop()

	// 8. Create and start orchestrator
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orchOpts := []func(*orchestrator.Orchestrator){
		orchestrator.WithErrorReporter(sentryClient),
	}
	if githubTokenProvider != nil {
		orchOpts = append(orchOpts, orchestrator.WithGitHubTokenProvider(githubTokenProvider))
	}
	orch = orchestrator.New(cfg, wf.PromptTemplate, trackerClient, dockerRunner, ws, logger, orchOpts...)

	// Startup terminal workspace cleanup (non-fatal).
	orch.CleanupTerminalWorkspaces()

	// Start orchestrator in background goroutine
	orchErrCh := make(chan error, 1)
	go func() {
		orchErrCh <- orch.Start(ctx)
	}()

	// 9. Start HTTP server if configured.
	httpPort := cfg.ServerPort
	if *portFlag >= 0 {
		httpPort = *portFlag // CLI --port overrides server.port
	}
	if httpPort >= 0 {
		srv := server.New(httpPort, orch, logger)
		addr, err := srv.Start()
		if err != nil {
			return fmt.Errorf("failed to start HTTP server: %w", err)
		}
		logger.Info("HTTP server listening", "addr", addr)
		defer func() {
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutCancel()
			_ = srv.Stop(shutCtx)
		}()
	}

	// 10. Start TUI
	snapshotCh := make(chan tui.Snapshot)

	// Bridge orchestrator snapshots to TUI (in background)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				close(snapshotCh)
				return
			case <-ticker.C:
				snap := orch.Snapshot()
				tuiSnap := tui.Snapshot{
					Running:          snap.Running,
					RetryQueue:       snap.RetryQueue,
					Completed:        snap.Completed,
					CompletedHistory: snap.CompletedHistory,
					ActivityLog:      snap.ActivityLog,
					AgentTotals:      snap.AgentTotals,
					RateLimits:       snap.RateLimits,
				}
				select {
				case snapshotCh <- tuiSnap:
				default: // Don't block if TUI is busy
				}
			}
		}
	}()

	tuiModel := tui.NewModel(snapshotCh)
	p := tea.NewProgram(tuiModel, tea.WithAltScreen())

	// 11. Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
			logger.Info("shutting down...")
			cancel()
			orch.Stop()
			p.Quit()
		case <-ctx.Done():
		}
	}()

	// Run TUI (blocking)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// Cancel context and wait for orchestrator
	cancel()
	select {
	case err := <-orchErrCh:
		if err != nil && err != context.Canceled {
			return fmt.Errorf("orchestrator error: %w", err)
		}
	case <-time.After(10 * time.Second):
		logger.Warn("orchestrator shutdown timed out")
	}

	return nil
}
