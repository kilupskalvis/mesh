package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createSourceRepo creates a git repo with one commit, returns its path.
func createSourceRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, string(out))
	}
	run("init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0o644))
	run("add", ".")
	run("commit", "-m", "initial commit")
	return dir
}

// --- EnsureBase tests ---

func TestEnsureBase_ClonesOnFirstRun(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}

	err := mgr.EnsureBase()
	require.NoError(t, err)

	// .base/HEAD should exist (bare repo marker)
	assert.FileExists(t, filepath.Join(root, ".base", "HEAD"))
}

func TestEnsureBase_Idempotent(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}

	require.NoError(t, mgr.EnsureBase())
	require.NoError(t, mgr.EnsureBase()) // second call is no-op
	assert.FileExists(t, filepath.Join(root, ".base", "HEAD"))
}

func TestEnsureBase_RecreatesCorrupted(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}

	// Create a corrupted .base (directory exists but no HEAD)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".base"), 0o755))

	err := mgr.EnsureBase()
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(root, ".base", "HEAD"))
}

// --- Fetch tests ---

func TestFetch_UpdatesRefs(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}

	require.NoError(t, mgr.EnsureBase())

	// Add a new commit to source
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "second")
	cmd.Dir = src
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	require.NoError(t, cmd.Run())

	err := mgr.Fetch("")
	require.NoError(t, err)
}

// --- CreateWorktree tests ---

func TestCreateWorktree_CreatesDirectoryAndBranch(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}
	require.NoError(t, mgr.EnsureBase())

	path, err := mgr.CreateWorktree("issue-1-test")
	require.NoError(t, err)
	assert.DirExists(t, path)
	assert.Equal(t, filepath.Join(root, "issue-1-test"), path)

	// Should have a .git file (not directory) pointing to .base
	gitFile := filepath.Join(path, ".git")
	info, err := os.Stat(gitFile)
	require.NoError(t, err)
	assert.False(t, info.IsDir(), ".git should be a file in a worktree, not a directory")

	// README.md from the source repo should be present
	assert.FileExists(t, filepath.Join(path, "README.md"))
}

func TestWorktreeExists_ReturnsTrueForExisting(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}
	require.NoError(t, mgr.EnsureBase())

	assert.False(t, mgr.WorktreeExists("issue-1-test"))

	_, err := mgr.CreateWorktree("issue-1-test")
	require.NoError(t, err)

	assert.True(t, mgr.WorktreeExists("issue-1-test"))
}

func TestWorktreePath_ReturnsCorrectPath(t *testing.T) {
	mgr := &Manager{Root: "/tmp/ws", GitBin: "git"}
	assert.Equal(t, "/tmp/ws/issue-42-fix", mgr.WorktreePath("issue-42-fix"))
}

func TestCreateWorktree_ReusesExisting(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}
	require.NoError(t, mgr.EnsureBase())

	path1, err := mgr.CreateWorktree("issue-1-test")
	require.NoError(t, err)

	// Creating again should return the same path without error.
	path2, err := mgr.CreateWorktree("issue-1-test")
	require.NoError(t, err)
	assert.Equal(t, path1, path2)
}

// --- ResetWorktree tests ---

func TestResetWorktree_CleansToOriginMain(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}
	require.NoError(t, mgr.EnsureBase())

	path, err := mgr.CreateWorktree("issue-1-test")
	require.NoError(t, err)

	// Dirty the worktree: add a file and modify existing.
	require.NoError(t, os.WriteFile(filepath.Join(path, "dirty.txt"), []byte("dirty"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(path, "README.md"), []byte("modified"), 0o644))

	err = mgr.ResetWorktree("issue-1-test")
	require.NoError(t, err)

	// dirty.txt should be gone (git clean -fd)
	_, err = os.Stat(filepath.Join(path, "dirty.txt"))
	assert.True(t, os.IsNotExist(err))

	// README.md should be restored to original
	content, err := os.ReadFile(filepath.Join(path, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "# test", string(content))
}

// --- RemoveWorktree tests ---

func TestRemoveWorktree_DeletesDirectoryAndBranch(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}
	require.NoError(t, mgr.EnsureBase())

	path, err := mgr.CreateWorktree("issue-1-test")
	require.NoError(t, err)
	assert.DirExists(t, path)

	err = mgr.RemoveWorktree("issue-1-test")
	require.NoError(t, err)

	// Directory should be gone.
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))

	// Branch should be gone from bare repo.
	out, err := mgr.runGit(mgr.basePath(), "branch", "--list", "issue-1-test")
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(out))
}

// --- injectToken tests ---

func TestInjectToken(t *testing.T) {
	got, err := injectToken("https://github.com/owner/repo.git", "ghp_abc123")
	require.NoError(t, err)
	assert.Equal(t, "https://x-access-token:ghp_abc123@github.com/owner/repo.git", got)
}

func TestInjectToken_NonGitHub(t *testing.T) {
	got, err := injectToken("https://gitlab.com/owner/repo.git", "tok")
	require.NoError(t, err)
	assert.Equal(t, "https://x-access-token:tok@gitlab.com/owner/repo.git", got)
}

func TestFetch_WithToken_RestoresOriginalURL(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	// Use a file:// URL so injectToken produces a valid remote.
	fileURL := "file://" + src
	mgr := &Manager{Root: root, RepoURL: fileURL, GitBin: "git"}
	require.NoError(t, mgr.EnsureBase())

	err := mgr.Fetch("some-token")
	require.NoError(t, err)

	// Verify original remote URL is restored (token not persisted).
	out, err := mgr.runGit(mgr.basePath(), "remote", "get-url", "origin")
	require.NoError(t, err)
	assert.Equal(t, fileURL, strings.TrimSpace(out))
}

// --- End-to-end lifecycle test ---

func TestWorktreeLifecycle_EndToEnd(t *testing.T) {
	src := createSourceRepo(t)
	root := t.TempDir()
	mgr := &Manager{Root: root, RepoURL: src, GitBin: "git"}

	// 1. EnsureBase
	require.NoError(t, mgr.EnsureBase())
	assert.FileExists(t, filepath.Join(root, ".base", "HEAD"))

	// 2. Fetch
	require.NoError(t, mgr.Fetch(""))

	// 3. Create worktree
	branch := BranchName("42", "Fix Auth Bug")
	path, err := mgr.CreateWorktree(branch)
	require.NoError(t, err)
	assert.DirExists(t, path)
	assert.FileExists(t, filepath.Join(path, "README.md"))
	assert.True(t, mgr.WorktreeExists(branch))

	// 4. Dirty the workspace
	require.NoError(t, os.WriteFile(filepath.Join(path, "dirty.txt"), []byte("x"), 0o644))

	// 5. Reset (simulates error retry)
	require.NoError(t, mgr.ResetWorktree(branch))
	_, err = os.Stat(filepath.Join(path, "dirty.txt"))
	assert.True(t, os.IsNotExist(err), "dirty file should be cleaned")

	// 6. Create second worktree (concurrent agent)
	branch2 := BranchName("57", "Add Search")
	path2, err := mgr.CreateWorktree(branch2)
	require.NoError(t, err)
	assert.DirExists(t, path2)
	assert.NotEqual(t, path, path2)

	// 7. Remove first worktree (simulates terminal state)
	require.NoError(t, mgr.RemoveWorktree(branch))
	assert.False(t, mgr.WorktreeExists(branch))

	// 8. Second worktree should be unaffected
	assert.True(t, mgr.WorktreeExists(branch2))
	assert.FileExists(t, filepath.Join(path2, "README.md"))

	// 9. Cleanup
	require.NoError(t, mgr.RemoveWorktree(branch2))
}
