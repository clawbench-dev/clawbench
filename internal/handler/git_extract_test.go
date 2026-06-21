package handler

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitHeaderAndShortstat(t *testing.T) {
	tests := []struct {
		name             string
		rec              string
		wantHeader       string
		wantShortstatLen int
	}{
		{
			name:             "header only",
			rec:              "abc123||fix bug|2026-01-01|Author",
			wantHeader:       "abc123||fix bug|2026-01-01|Author",
			wantShortstatLen: 0,
		},
		{
			name:             "header followed by shortstat",
			rec:              "abc123||fix bug|2026-01-01|Author\n\n 2 files changed, 10 insertions(+), 5 deletions(-)",
			wantHeader:       "abc123||fix bug|2026-01-01|Author",
			wantShortstatLen: 1,
		},
		{
			name:             "shortstat before header",
			rec:              " 3 files changed, 5 insertions(+)\nabc123||fix bug|2026-01-01|Author",
			wantHeader:       "abc123||fix bug|2026-01-01|Author",
			wantShortstatLen: 1,
		},
		{
			name:             "shortstat only, no header",
			rec:              " 2 files changed, 10 insertions(+), 5 deletions(-)",
			wantHeader:       "",
			wantShortstatLen: 1,
		},
		{
			name:             "empty record",
			rec:              "",
			wantHeader:       "",
			wantShortstatLen: 0,
		},
		{
			name:             "multiple shortstat lines",
			rec:              "abc123||fix bug|2026-01-01|Author\n 2 files changed\n 1 file changed, 3 insertions(+)",
			wantHeader:       "abc123||fix bug|2026-01-01|Author",
			wantShortstatLen: 2,
		},
		{
			name:             "whitespace-only lines ignored",
			rec:              "  \n  abc123||fix bug|2026-01-01|Author  \n  \n 2 files changed  \n",
			wantHeader:       "abc123||fix bug|2026-01-01|Author",
			wantShortstatLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, shortstatLines := splitHeaderAndShortstat(tt.rec)
			assert.Equal(t, tt.wantHeader, header)
			assert.Len(t, shortstatLines, tt.wantShortstatLen)
		})
	}
}

func TestIsShortstatLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{" 2 files changed, 10 insertions(+), 5 deletions(-)", true},
		{" 1 file changed, 3 insertions(+)", true},
		{"15 files changed", true},
		{"1 file changed", true},
		{"abc123||fix bug|2026-01-01|Author", false},
		{"", false},
		{"some random text", false},
		{"Merge branch 'feature' into main", false},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			assert.Equal(t, tt.want, isShortstatLine(tt.line))
		})
	}
}

func TestExtractFileCountFromShortstatLines(t *testing.T) {
	tests := []struct {
		name          string
		lines         []string
		wantFileCount int
	}{
		{
			name:          "single line with file count",
			lines:         []string{" 2 files changed, 10 insertions(+), 5 deletions(-)"},
			wantFileCount: 2,
		},
		{
			name:          "multiple lines, first has count",
			lines:         []string{" 3 files changed, 5 insertions(+)", " 1 file changed, 2 deletions(-)"},
			wantFileCount: 3,
		},
		{
			name:          "first line no match, second has count",
			lines:         []string{"not a shortstat", " 1 file changed, 3 insertions(+)"},
			wantFileCount: 1,
		},
		{
			name:          "empty slice",
			lines:         []string{},
			wantFileCount: 0,
		},
		{
			name:          "no matching lines",
			lines:         []string{"not shortstat", "also not"},
			wantFileCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantFileCount, extractFileCountFromShortstatLines(tt.lines))
		})
	}
}

func TestValidateWorktreePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Resolve the project dir path (macOS /var → /private/var) so it matches
	// git worktree list --porcelain output which also resolves symlinks.
	resolvedProjectDir := resolveSymlinkPath(env.ProjectDir)

	t.Run("valid worktree path but is current", func(t *testing.T) {
		w := httptest.NewRecorder()
		result := validateWorktreePath(w, resolvedProjectDir, resolvedProjectDir)
		assert.False(t, result, "current worktree should fail validation")

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "cannot_delete_current", resp["error"])
	})

	t.Run("non-existent worktree path", func(t *testing.T) {
		w := httptest.NewRecorder()
		result := validateWorktreePath(w, resolvedProjectDir, "/tmp/nonexistent-path-xyz")
		assert.False(t, result, "non-existent path should fail validation")

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "path_not_allowed", resp["error"])
	})

	t.Run("non-current worktree path passes", func(t *testing.T) {
		run := func(name string, args ...string) {
			cmd := exec.Command(name, args...)
			cmd.Dir = resolvedProjectDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
			}
		}
		wtPath := filepath.Join(filepath.Dir(resolvedProjectDir), "wt-validate-ok")
		run("git", "worktree", "add", wtPath, "-b", "wt-validate-ok-branch")

		// Resolve the worktree path for consistent comparison
		resolvedWtPath := resolveSymlinkPath(wtPath)

		w := httptest.NewRecorder()
		result := validateWorktreePath(w, resolvedProjectDir, resolvedWtPath)
		assert.True(t, result, "non-current worktree path should pass validation")
	})
}

func TestRemoveWorktree(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	// resolvePath resolves symlinks for macOS compatibility (/var → /private/var)
	resolvePath := func(p string) string {
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return p
		}
		return resolved
	}

	t.Run("clean worktree removal", func(t *testing.T) {
		wtPath := filepath.Join(filepath.Dir(env.ProjectDir), "wt-remove-clean")
		run("git", "worktree", "add", wtPath, "-b", "wt-remove-clean-branch")

		w := httptest.NewRecorder()
		result := removeWorktree(w, env.ProjectDir, resolvePath(wtPath), false)
		assert.True(t, result, "clean worktree should be removed successfully")

		if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
			t.Error("worktree directory should be removed")
		}
	})

	t.Run("dirty worktree without force returns error", func(t *testing.T) {
		wtPath := filepath.Join(filepath.Dir(env.ProjectDir), "wt-remove-dirty")
		run("git", "worktree", "add", wtPath, "-b", "wt-remove-dirty-branch")
		require.NoError(t, os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("uncommitted"), 0o644))

		w := httptest.NewRecorder()
		result := removeWorktree(w, env.ProjectDir, resolvePath(wtPath), false)
		assert.False(t, result, "dirty worktree should not be removed without force")

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "dirty_worktree", resp["error"])
	})

	t.Run("dirty worktree with force succeeds", func(t *testing.T) {
		wtPath := filepath.Join(filepath.Dir(env.ProjectDir), "wt-remove-dirty-force")
		run("git", "worktree", "add", wtPath, "-b", "wt-remove-dirty-force-branch")
		require.NoError(t, os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("uncommitted"), 0o644))

		w := httptest.NewRecorder()
		result := removeWorktree(w, env.ProjectDir, resolvePath(wtPath), true)
		assert.True(t, result, "dirty worktree should be removed with force")

		if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
			t.Error("dirty worktree should be removed with force")
		}
	})

	t.Run("non-existent worktree returns delete_failed", func(t *testing.T) {
		w := httptest.NewRecorder()
		result := removeWorktree(w, env.ProjectDir, "/tmp/nonexistent-wt-path-xyz", false)
		assert.False(t, result)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "delete_failed", resp["error"])
	})
}
