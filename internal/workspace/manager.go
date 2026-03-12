// Package workspace manages per-issue workspace directories and hook execution.
package workspace

// Manager handles workspace creation, validation, and cleanup.
type Manager struct {
	Root    string // Base directory for all workspaces
	RepoURL string // Git remote URL for bare clone
	GitBin  string // Path to git binary (default: "git")
}

// NewManager creates a workspace manager with the given root directory.
func NewManager(root string) *Manager {
	return &Manager{Root: root, GitBin: "git"}
}
