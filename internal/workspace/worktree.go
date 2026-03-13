package workspace

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// basePath returns the path to the shared bare clone.
func (m *Manager) basePath() string {
	return filepath.Join(m.Root, ".base")
}

// runGit runs a git command and returns combined output.
// It strips GIT_DIR, GIT_INDEX_FILE, and GIT_WORK_TREE from the environment
// so that git uses cmd.Dir instead of inheriting stale values (e.g. from a
// pre-commit hook context).
func (m *Manager) runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command(m.GitBin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = CleanGitEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), string(out), err)
	}
	return string(out), nil
}

// CleanGitEnv returns env with git-specific variables removed so that
// subprocess git commands use their own working directory, not inherited paths.
func CleanGitEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_DIR=") ||
			strings.HasPrefix(e, "GIT_INDEX_FILE=") ||
			strings.HasPrefix(e, "GIT_WORK_TREE=") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// EnsureBase creates the shared bare clone if it doesn't exist.
// Idempotent: skips if .base/HEAD exists. Removes and re-clones if corrupted.
// Also runs `git worktree prune` to clean stale metadata from prior crashes.
func (m *Manager) EnsureBase() error {
	base := m.basePath()
	head := filepath.Join(base, "HEAD")

	// Already healthy.
	if _, err := os.Stat(head); err == nil {
		// Prune stale worktree metadata.
		_, _ = m.runGit(base, "worktree", "prune")
		return nil
	}

	// Corrupted or missing — remove and re-clone.
	if _, err := os.Stat(base); err == nil {
		if err := os.RemoveAll(base); err != nil {
			return fmt.Errorf("remove corrupted .base: %w", err)
		}
	}

	// Ensure root exists.
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return fmt.Errorf("create workspace root: %w", err)
	}

	_, err := m.runGit(m.Root, "clone", "--bare", m.RepoURL, ".base")
	if err != nil {
		return fmt.Errorf("bare clone: %w", err)
	}

	// Configure fetch refspec so `origin/main` etc. resolve.
	// Bare clones default to mapping remote refs to local heads, not remotes/origin/*.
	if _, err := m.runGit(base, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return fmt.Errorf("configure fetch refspec: %w", err)
	}
	if _, err := m.runGit(base, "fetch", "origin"); err != nil {
		return fmt.Errorf("initial fetch: %w", err)
	}

	return nil
}

// Fetch runs `git fetch origin` in the bare clone.
// If token is non-empty, it is injected into the remote URL for authentication.
func (m *Manager) Fetch(token string) error {
	base := m.basePath()

	if token != "" {
		// Temporarily set the remote URL with token for fetch.
		authURL, err := injectToken(m.RepoURL, token)
		if err != nil {
			return fmt.Errorf("inject token: %w", err)
		}
		if _, err := m.runGit(base, "remote", "set-url", "origin", authURL); err != nil {
			return err
		}
		defer func() {
			// Restore original URL (without token) to avoid persisting credentials.
			_, _ = m.runGit(base, "remote", "set-url", "origin", m.RepoURL)
		}()
	}

	_, err := m.runGit(base, "fetch", "origin")
	return err
}

// injectToken inserts a token into a git URL for authentication.
// Supports https://github.com/... → https://x-access-token:{token}@github.com/...
func injectToken(repoURL, token string) (string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String(), nil
}

// WorktreePath returns the path for a worktree with the given branch name.
func (m *Manager) WorktreePath(branch string) string {
	return filepath.Join(m.Root, branch)
}

// WorktreeExists returns true if a worktree directory exists for the given branch.
func (m *Manager) WorktreeExists(branch string) bool {
	path := m.WorktreePath(branch)
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// CreateWorktree creates a git worktree for the given branch name, branching
// from origin/main. If the worktree already exists, it is reused.
// Cleans up any stale local branch of the same name before creating.
func (m *Manager) CreateWorktree(branch string) (string, error) {
	path := m.WorktreePath(branch)

	// Already exists — reuse.
	if m.WorktreeExists(branch) {
		return path, nil
	}

	base := m.basePath()

	// Prune stale worktree entries, then delete the branch if it lingers.
	_, _ = m.runGit(base, "worktree", "prune")
	_, _ = m.runGit(base, "branch", "-D", branch)

	// Create the worktree.
	_, err := m.runGit(base, "worktree", "add", path, "-b", branch, "origin/main")
	if err != nil {
		return "", fmt.Errorf("create worktree %q: %w", branch, err)
	}

	// Rewrite the .git file to use a relative gitdir path so the worktree
	// works inside Docker containers where the mount point differs from the host.
	if err := relativizeGitdir(path); err != nil {
		return "", fmt.Errorf("relativize gitdir: %w", err)
	}

	// Ensure container user can write (Colima maps as root:root).
	if err := os.Chmod(path, 0o777); err != nil {
		return "", fmt.Errorf("chmod worktree: %w", err)
	}

	return path, nil
}

// ResetWorktree resets a worktree to a clean state on origin/main.
// Used for error retries to give the agent a fresh start.
// If the reset fails (corrupted state), falls back to remove + recreate.
func (m *Manager) ResetWorktree(branch string) error {
	path := m.WorktreePath(branch)

	// Try the fast path: reset + clean + checkout.
	if _, err := m.runGit(path, "reset", "--hard", "origin/main"); err != nil {
		return m.recreateWorktree(branch)
	}
	if _, err := m.runGit(path, "clean", "-fd"); err != nil {
		return m.recreateWorktree(branch)
	}
	if _, err := m.runGit(path, "checkout", "-B", branch, "origin/main"); err != nil {
		return m.recreateWorktree(branch)
	}

	return nil
}

// recreateWorktree forcibly removes and re-creates a corrupted worktree.
func (m *Manager) recreateWorktree(branch string) error {
	base := m.basePath()
	path := m.WorktreePath(branch)

	// Force remove worktree (ignore errors).
	_, _ = m.runGit(base, "worktree", "remove", "--force", path)
	// Fallback: rm -rf if worktree remove also failed.
	_ = os.RemoveAll(path)
	// Prune stale worktree entries so branch -D succeeds.
	_, _ = m.runGit(base, "worktree", "prune")
	// Delete branch.
	_, _ = m.runGit(base, "branch", "-D", branch)

	// Re-create.
	_, err := m.runGit(base, "worktree", "add", path, "-b", branch, "origin/main")
	if err != nil {
		return fmt.Errorf("recreate worktree %q: %w", branch, err)
	}

	if err := relativizeGitdir(path); err != nil {
		return fmt.Errorf("relativize gitdir: %w", err)
	}

	if err := os.Chmod(path, 0o777); err != nil {
		return fmt.Errorf("chmod worktree: %w", err)
	}

	return nil
}

// relativizeGitdir rewrites the `.git` file in a worktree directory and the
// reverse `gitdir` pointer in the bare repo so both use relative paths.
// This is necessary because `git worktree add` writes absolute host paths,
// which break when the worktree is mounted at a different path inside Docker.
func relativizeGitdir(worktreePath string) error {
	dotGit := filepath.Join(worktreePath, ".git")

	data, err := os.ReadFile(dotGit)
	if err != nil {
		return fmt.Errorf("read .git file: %w", err)
	}

	line := strings.TrimSpace(string(data))
	const prefix = "gitdir: "
	if !strings.HasPrefix(line, prefix) {
		return fmt.Errorf("unexpected .git content: %s", line)
	}
	absGitdir := strings.TrimPrefix(line, prefix)

	// Compute relative path from worktree to its gitdir metadata.
	relGitdir, err := filepath.Rel(worktreePath, absGitdir)
	if err != nil {
		return fmt.Errorf("compute relative gitdir: %w", err)
	}
	if err := os.WriteFile(dotGit, []byte("gitdir: "+relGitdir+"\n"), 0o644); err != nil {
		return fmt.Errorf("write .git file: %w", err)
	}

	// Fix the reverse pointer: .base/worktrees/<branch>/gitdir → worktree path.
	reverseFile := filepath.Join(absGitdir, "gitdir")
	revData, err := os.ReadFile(reverseFile)
	if err != nil {
		// Not fatal — some git versions may not create this file.
		return nil
	}
	absWorktree := strings.TrimSpace(string(revData))
	relWorktree, err := filepath.Rel(absGitdir, absWorktree)
	if err != nil {
		return fmt.Errorf("compute relative worktree path: %w", err)
	}
	if err := os.WriteFile(reverseFile, []byte(relWorktree+"\n"), 0o644); err != nil {
		return fmt.Errorf("write reverse gitdir: %w", err)
	}

	return nil
}

// RemoveWorktree removes a worktree and its local branch.
func (m *Manager) RemoveWorktree(branch string) error {
	base := m.basePath()
	path := m.WorktreePath(branch)

	// Remove worktree.
	if _, err := m.runGit(base, "worktree", "remove", "--force", path); err != nil {
		// Fallback: manual removal.
		_ = os.RemoveAll(path)
	}

	// Prune stale metadata so branch -D succeeds after manual removal.
	_, _ = m.runGit(base, "worktree", "prune")
	// Delete local branch.
	_, _ = m.runGit(base, "branch", "-D", branch)

	return nil
}
