package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// RunHook executes a shell hook script with workspace path as the working directory.
// Returns error if the hook fails or times out.
// Parameters:
//   - name: hook name for logging (e.g. "after_create")
//   - script: shell command to execute
//   - workspacePath: working directory for the hook
//   - timeoutMs: maximum execution time in milliseconds
func RunHook(name, script, workspacePath string, timeoutMs int) error {
	if script == "" {
		return nil
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", script)
	cmd.Dir = workspacePath

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hook %q timed out after %dms", name, timeoutMs)
		}
		return fmt.Errorf("hook %q failed: %w\noutput: %s", name, err, string(output))
	}

	return nil
}
