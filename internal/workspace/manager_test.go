package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateForIssue_CreatesDirectory(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)

	path, created, err := mgr.CreateForIssue("PROJ-42")
	require.NoError(t, err)
	assert.True(t, created)
	assert.DirExists(t, path)
	assert.Equal(t, filepath.Join(root, "PROJ-42"), path)
}

func TestCreateForIssue_ReusesExisting(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)

	path1, created1, err := mgr.CreateForIssue("PROJ-42")
	require.NoError(t, err)
	assert.True(t, created1)

	path2, created2, err := mgr.CreateForIssue("PROJ-42")
	require.NoError(t, err)
	assert.False(t, created2)
	assert.Equal(t, path1, path2)
}

func TestCreateForIssue_SanitizesIdentifier(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)

	path, created, err := mgr.CreateForIssue("PROJ/123")
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, filepath.Join(root, "PROJ_123"), path)
	assert.DirExists(t, path)
}

func TestWorkspacePath_ReturnsCorrectPath(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)

	path := mgr.WorkspacePath("PROJ-42")
	assert.Equal(t, filepath.Join(root, "PROJ-42"), path)

	// Sanitized identifier
	path = mgr.WorkspacePath("PROJ/123")
	assert.Equal(t, filepath.Join(root, "PROJ_123"), path)
}

func TestValidateContainment_ValidPath(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)

	err := mgr.ValidateContainment(filepath.Join(root, "some-issue"))
	assert.NoError(t, err)
}

func TestValidateContainment_EscapedPath(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)

	err := mgr.ValidateContainment(filepath.Join(root, "..", "escaped"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "outside workspace root")
}

func TestCleanWorkspace_RemovesDirectory(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)

	path, _, err := mgr.CreateForIssue("PROJ-42")
	require.NoError(t, err)
	assert.DirExists(t, path)

	err = mgr.CleanWorkspace("PROJ-42", "", 0)
	require.NoError(t, err)

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRunHook_ExecutesScript(t *testing.T) {
	dir := t.TempDir()

	err := RunHook("test_hook", "echo hello > out.txt", dir, 5000)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "hello")
}

func TestRunHook_TimesOut(t *testing.T) {
	dir := t.TempDir()

	err := RunHook("slow_hook", "sleep 10", dir, 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestRunHook_EmptyScript(t *testing.T) {
	dir := t.TempDir()

	err := RunHook("noop", "", dir, 5000)
	assert.NoError(t, err)
}
