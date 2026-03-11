// Package workspace manages per-issue workspace directories and hook execution.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kalvis/mesh/internal/model"
)

// Manager handles workspace creation, validation, and cleanup.
type Manager struct {
	Root string // Base directory for all workspaces
}

// NewManager creates a workspace manager with the given root directory.
func NewManager(root string) *Manager {
	return &Manager{Root: root}
}

// CreateForIssue creates or reuses a workspace directory for the given issue identifier.
// Returns (path, createdNow, error).
//   - Sanitizes identifier using model.SanitizeWorkspaceKey
//   - Creates root directory if needed
//   - Creates issue subdirectory if it doesn't exist
//   - Validates path containment (workspace must be under root)
func (m *Manager) CreateForIssue(identifier string) (string, bool, error) {
	sanitized := model.SanitizeWorkspaceKey(identifier)
	wsPath := filepath.Join(m.Root, sanitized)

	if err := m.ValidateContainment(wsPath); err != nil {
		return "", false, err
	}

	// Check if the directory already exists.
	if info, err := os.Stat(wsPath); err == nil && info.IsDir() {
		return wsPath, false, nil
	}

	// Create root and subdirectory.
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		return "", false, fmt.Errorf("create workspace directory: %w", err)
	}

	return wsPath, true, nil
}

// WorkspacePath returns the sanitized path for an issue without creating it.
func (m *Manager) WorkspacePath(identifier string) string {
	sanitized := model.SanitizeWorkspaceKey(identifier)
	return filepath.Join(m.Root, sanitized)
}

// CleanWorkspace removes a workspace directory for the given identifier.
// It runs the beforeRemoveHook first if provided, then removes the directory.
func (m *Manager) CleanWorkspace(identifier string, beforeRemoveHook string, hookTimeoutMs int) error {
	wsPath := m.WorkspacePath(identifier)

	if err := m.ValidateContainment(wsPath); err != nil {
		return err
	}

	if beforeRemoveHook != "" {
		// before_remove failure is logged and ignored; cleanup still proceeds.
		_ = RunHook("before_remove", beforeRemoveHook, wsPath, hookTimeoutMs)
	}

	if err := os.RemoveAll(wsPath); err != nil {
		return fmt.Errorf("remove workspace directory: %w", err)
	}

	return nil
}

// ValidateContainment checks that a resolved path is under the workspace root.
// This prevents path traversal attacks via crafted identifiers.
func (m *Manager) ValidateContainment(path string) error {
	absRoot, err := filepath.Abs(m.Root)
	if err != nil {
		return fmt.Errorf("resolve root path: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}

	// The resolved path must be a proper child of the root.
	// filepath.Abs resolves ".." components, so this catches traversal.
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return fmt.Errorf("path outside workspace root: %w", err)
	}

	if rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path outside workspace root: %q is not under %q", absPath, absRoot)
	}

	return nil
}
