package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/kalvis/mesh/internal/model"
)

// Runner launches and manages agent containers.
type Runner interface {
	// Run starts an agent container, writes the payload to stdin, and streams events.
	// Returns an event channel, a result channel (sends one RunResult when done), and an error.
	Run(ctx context.Context, params RunParams) (<-chan model.AgentEvent, <-chan RunResult, error)

	// Stop stops a running container (SIGTERM then SIGKILL after grace period).
	Stop(containerID string) error

	// IsAvailable checks if the Docker daemon is reachable.
	IsAvailable() error
}

// RunParams holds configuration for launching an agent container.
type RunParams struct {
	Image            string
	WorkspaceRoot    string // host path to workspace root (mounted as /workspaces)
	ContainerWorkDir string // working directory inside container (e.g. /workspaces/issue-42-fix)
	StdinPayload     model.StdinPayload
	EnvVars          map[string]string
	Memory           string // e.g. "2g"
	CPUs             string // e.g. "2"
	Network          string // e.g. "none"
	ReadTimeoutMs    int
}

// RunResult holds the outcome of a completed container run.
type RunResult struct {
	ExitCode int
	Stderr   string
	Error    error
}

// Compile-time check that DockerRunner satisfies the Runner interface.
var _ Runner = (*DockerRunner)(nil)

// DockerRunner implements Runner by shelling out to the docker CLI.
type DockerRunner struct {
	// DockerBin is the path to the docker binary (default: "docker").
	DockerBin string
}

// NewDockerRunner creates a DockerRunner with the default docker binary path.
func NewDockerRunner() *DockerRunner {
	return &DockerRunner{DockerBin: "docker"}
}

// ContainerWorkspacesRoot is the mount point for the workspace root inside the container.
const ContainerWorkspacesRoot = "/workspaces"

// unsafeContainerChars matches characters not allowed in Docker container names.
var unsafeContainerChars = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

// ContainerName generates a Docker container name from a session ID.
// Container names are prefixed with "mesh-" and sanitized to contain only [a-zA-Z0-9_.-].
// Spaces are replaced with hyphens; other unsafe characters with underscores.
func ContainerName(sessionID string) string {
	// Replace spaces with hyphens first for readability.
	sanitized := strings.ReplaceAll(sessionID, " ", "-")
	sanitized = unsafeContainerChars.ReplaceAllString(sanitized, "_")
	return "mesh-" + sanitized
}

// IsAvailable checks if the Docker daemon is reachable by running `docker info`.
func (d *DockerRunner) IsAvailable() error {
	cmd := exec.Command(d.DockerBin, "info")
	if err := cmd.Run(); err != nil {
		return model.NewMeshError(model.ErrDockerDaemonUnavailable,
			"docker daemon is not reachable", err)
	}
	return nil
}

// buildDockerArgs constructs the docker run CLI arguments.
func (d *DockerRunner) buildDockerArgs(params RunParams, containerName string) []string {
	args := []string{"run", "-i", "--rm", "--name", containerName}

	// Volume mount: host workspace root -> container /workspaces.
	args = append(args, "-v", params.WorkspaceRoot+":"+ContainerWorkspacesRoot)

	// Working directory inside container (specific worktree).
	args = append(args, "-w", params.ContainerWorkDir)

	// Environment variables.
	for k, v := range params.EnvVars {
		args = append(args, "-e", k+"="+v)
	}

	// Resource limits.
	if params.Memory != "" {
		args = append(args, "--memory", params.Memory)
	}
	if params.CPUs != "" {
		args = append(args, "--cpus", params.CPUs)
	}

	// Network mode.
	if params.Network != "" {
		args = append(args, "--network", params.Network)
	}

	// Run as non-root user (required by Claude CLI bypassPermissions).
	args = append(args, "--user", "1000:1000")

	// Image is always last.
	args = append(args, params.Image)
	return args
}

// initWorkspaceOwnership runs a short-lived container to chown the specific
// worktree directory so the non-root agent user can write to it.
// Only targets ContainerWorkDir (not the entire workspace root) to avoid
// recursively chowning the bare clone and sibling worktrees on every launch.
func (d *DockerRunner) initWorkspaceOwnership(params RunParams) error {
	cmd := exec.Command(d.DockerBin, "run", "--rm",
		"-v", params.WorkspaceRoot+":"+ContainerWorkspacesRoot,
		"--entrypoint", "chown",
		params.Image,
		"-R", "1000:1000", params.ContainerWorkDir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", string(out), err)
	}
	return nil
}

// Run starts an agent container, writes the JSON payload to stdin, and streams
// parsed events from stdout. It returns an event channel, a result channel
// (which receives exactly one RunResult when the container exits), and a
// startup error if the container cannot be launched.
//
// The caller should select on the event channel for streaming updates and the
// result channel for the final outcome. Cancelling ctx will stop the container.
func (d *DockerRunner) Run(ctx context.Context, params RunParams) (<-chan model.AgentEvent, <-chan RunResult, error) {
	// Init step: chown workspace so the non-root container user can write.
	if err := d.initWorkspaceOwnership(params); err != nil {
		return nil, nil, fmt.Errorf("init workspace ownership: %w", err)
	}

	containerName := ContainerName(fmt.Sprintf("%d", time.Now().UnixNano()))

	args := d.buildDockerArgs(params, containerName)
	cmd := exec.CommandContext(ctx, d.DockerBin, args...)

	// Set up stdin pipe for payload delivery.
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	// Set up stdout pipe for event streaming.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Capture stderr for diagnostics.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	// Start the container process.
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("starting docker container: %w", err)
	}

	// Write JSON payload to stdin and close.
	go func() {
		defer stdinPipe.Close()
		payload, err := json.Marshal(params.StdinPayload)
		if err != nil {
			return
		}
		_, _ = stdinPipe.Write(payload)
		_, _ = stdinPipe.Write([]byte("\n"))
	}()

	eventCh := make(chan model.AgentEvent, 64)
	resultCh := make(chan RunResult, 1)

	// Read timeout for individual line reads.
	readTimeout := time.Duration(params.ReadTimeoutMs) * time.Millisecond
	if readTimeout <= 0 {
		readTimeout = 60 * time.Second // default 60s
	}

	// lineCh receives lines from a dedicated scanner goroutine so we can
	// enforce per-line read timeouts without blocking the main select loop.
	type scanLine struct {
		text string
		err  error // non-nil only when the scanner stops (EOF / error)
	}
	lineCh := make(chan scanLine, 64)

	// Scanner goroutine: reads stdout until EOF or error, then closes lineCh.
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(stdoutPipe)
		// Allow up to 10 MB per line per spec.
		scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			lineCh <- scanLine{text: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			lineCh <- scanLine{err: err}
		}
	}()

	// Main event-loop goroutine: enforces read timeout and forwards parsed events.
	go func() {
		defer close(eventCh)
		defer close(resultCh)

		timer := time.NewTimer(readTimeout)
		defer timer.Stop()

		var timedOut bool
		for {
			select {
			case sl, ok := <-lineCh:
				if !ok {
					// Scanner finished (stdout closed).
					goto waitProc
				}
				if sl.err != nil {
					goto waitProc
				}

				// Reset the read-timeout timer on each received line.
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(readTimeout)

				line := sl.text
				if strings.TrimSpace(line) == "" {
					continue
				}

				ev, parseErr := ParseEvent(line)
				if parseErr != nil {
					// Emit a malformed event so the orchestrator can log it.
					ev = model.AgentEvent{
						Event:   "malformed",
						Message: line,
					}
				}

				select {
				case eventCh <- ev:
				case <-ctx.Done():
					goto waitProc
				}

			case <-timer.C:
				timedOut = true
				goto waitProc

			case <-ctx.Done():
				goto waitProc
			}
		}

	waitProc:
		// Wait for the process to finish.
		waitErr := cmd.Wait()

		exitCode := 0
		if waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}

		result := RunResult{
			ExitCode: exitCode,
			Stderr:   stderrBuf.String(),
		}

		if timedOut {
			result.Error = model.NewMeshError(model.ErrReadTimeout,
				fmt.Sprintf("no output received within %v", readTimeout), nil)
		} else if exitCode != 0 && ctx.Err() == nil {
			result.Error = model.NewMeshError(model.ErrContainerExit,
				fmt.Sprintf("container exited with code %d", exitCode), waitErr)
		} else if ctx.Err() != nil {
			result.Error = ctx.Err()
		}

		resultCh <- result
	}()

	return eventCh, resultCh, nil
}

// Stop stops a running container by name with SIGTERM, then SIGKILL after a 10-second grace period.
func (d *DockerRunner) Stop(containerID string) error {
	// docker stop sends SIGTERM and waits the grace period, then SIGKILL.
	cmd := exec.Command(d.DockerBin, "stop", "-t", "10", containerID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stopping container %s: %w", containerID, err)
	}
	return nil
}
