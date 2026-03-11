package config

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"

	"github.com/kalvis/mesh/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeWorkflowFile is a test helper that writes valid WORKFLOW.md content.
func writeWorkflowFile(t *testing.T, path, yaml, body string) {
	t.Helper()
	content := "---\n" + yaml + "\n---\n" + body
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestWatcher_DetectsFileChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	writeWorkflowFile(t, path, "tracker:\n  kind: jira", "initial prompt")

	var mu sync.Mutex
	var received *model.WorkflowDefinition

	onChange := func(wf *model.WorkflowDefinition) {
		mu.Lock()
		defer mu.Unlock()
		received = wf
	}
	onError := func(err error) {
		t.Errorf("unexpected error callback: %v", err)
	}

	w, err := NewWatcher(path, onChange, onError, slog.Default())
	require.NoError(t, err)
	require.NoError(t, w.Start())
	defer w.Stop()

	// Give the watcher time to set up.
	time.Sleep(50 * time.Millisecond)

	// Modify the file.
	writeWorkflowFile(t, path, "tracker:\n  kind: linear", "updated prompt")

	// Wait for the callback.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return received != nil
	}, 2*time.Second, 20*time.Millisecond, "onChange was never called")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "linear", received.Config["tracker"].(map[string]any)["kind"])
	assert.Contains(t, received.PromptTemplate, "updated prompt")
}

func TestWatcher_InvalidReloadCallsOnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	writeWorkflowFile(t, path, "tracker:\n  kind: jira", "initial prompt")

	var mu sync.Mutex
	var receivedErr error

	onChange := func(wf *model.WorkflowDefinition) {
		t.Errorf("unexpected onChange callback for invalid file")
	}
	onError := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		receivedErr = err
	}

	w, err := NewWatcher(path, onChange, onError, slog.Default())
	require.NoError(t, err)
	require.NoError(t, w.Start())
	defer w.Stop()

	time.Sleep(50 * time.Millisecond)

	// Write invalid YAML front matter.
	invalidContent := "---\n[invalid yaml: {{{\n---\nbody"
	require.NoError(t, os.WriteFile(path, []byte(invalidContent), 0o644))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return receivedErr != nil
	}, 2*time.Second, 20*time.Millisecond, "onError was never called")

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, receivedErr.Error(), "workflow_parse_error")
}

func TestWatcher_StartStop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	writeWorkflowFile(t, path, "tracker:\n  kind: jira", "prompt")

	w, err := NewWatcher(path, func(*model.WorkflowDefinition) {}, func(error) {}, slog.Default())
	require.NoError(t, err)
	require.NoError(t, w.Start())

	// Stop should return promptly without deadlocking, proving the
	// goroutine exits cleanly. We use a timeout to catch leaks.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success: Stop returned, goroutine exited.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2s; goroutine likely leaked")
	}
}

func TestWatcher_DebounceRapidChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	writeWorkflowFile(t, path, "tracker:\n  kind: jira", "initial prompt")

	var callCount atomic.Int32

	onChange := func(wf *model.WorkflowDefinition) {
		callCount.Add(1)
	}
	onError := func(err error) {
		t.Errorf("unexpected error callback: %v", err)
	}

	w, err := NewWatcher(path, onChange, onError, slog.Default())
	require.NoError(t, err)
	require.NoError(t, w.Start())
	defer w.Stop()

	time.Sleep(50 * time.Millisecond)

	// Write rapidly 5 times within a short window.
	for i := 0; i < 5; i++ {
		writeWorkflowFile(t, path, "tracker:\n  kind: jira", "rapid update")
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debounce to settle and callback to fire.
	time.Sleep(500 * time.Millisecond)

	count := callCount.Load()
	// Debounce should collapse the rapid writes into 1 or at most 2 calls.
	assert.LessOrEqual(t, count, int32(2),
		"expected debounced calls <= 2, got %d", count)
	assert.GreaterOrEqual(t, count, int32(1),
		"expected at least 1 callback, got %d", count)
}
