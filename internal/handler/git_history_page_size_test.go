package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// git_history.page_size must reach git log: it is a real page size, and hasMore
// must be derived from it rather than from a hard-coded 30.
func TestGetProjectHistory_UsesConfiguredPageSize(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	for _, name := range []string{"a", "b", "c"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".txt"), []byte(name), 0o644))
		run("add", name+".txt")
		run("commit", "-q", "-m", name)
	}

	orig := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = orig })

	model.ConfigInstance.GitHistory.PageSize = 2
	commits, hasMore := getProjectHistory(dir, 0, "")
	assert.Len(t, commits, 2, "page_size=2 must return two commits")
	assert.True(t, hasMore, "a full page must report more")

	model.ConfigInstance.GitHistory.PageSize = 5
	commits, hasMore = getProjectHistory(dir, 0, "")
	assert.Len(t, commits, 3, "page_size=5 must return all three commits")
	assert.False(t, hasMore, "a short page must report no more")
}
