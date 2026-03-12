package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
