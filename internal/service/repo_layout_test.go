package service_test

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canon resolves a test path the way production does (DetectRepoLayout applies
// filepath.Abs + Clean, then canonicalize = EvalSymlinks falling back to Clean).
// Assertions must compare through the same pipeline: on macOS every t.TempDir()
// lives under /var, a symlink to /private/var, so the raw test path and the
// production value never match verbatim. Linux hides this because /tmp has no
// symlinked ancestor.
func canon(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// A path that does not exist (e.g. a dangling gitdir target) is
		// returned cleaned by production; mirror that.
		return abs
	}
	return resolved
}

// makeRepo creates a normal clone layout: <dir>/.git as a directory.
func makeRepo(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
}

// makeLinkedWorktree creates a linked worktree layout under repoRoot, mirroring
// what `git worktree add` writes: the worktree root holds a `.git` FILE with a
// `gitdir:` pointer, and the pointed-at git dir holds a `commondir` file.
// `commonRel` is the commondir contents relative to the worktree git dir
// (git writes "../.." for the standard layout).
func makeLinkedWorktree(t *testing.T, repoRoot, wtPath, commonRel string) {
	t.Helper()
	wtGit := filepath.Join(repoRoot, ".git", "worktrees", filepath.Base(wtPath))
	require.NoError(t, os.MkdirAll(wtGit, 0o755))
	require.NoError(t, os.MkdirAll(wtPath, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(wtPath, ".git"),
		[]byte("gitdir: "+wtGit+"\n"), 0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(wtGit, "commondir"),
		[]byte(commonRel+"\n"), 0o644,
	))
}

func TestDetectRepoLayout_MainWorktree(t *testing.T) {
	repo := t.TempDir()
	makeRepo(t, repo)

	layout := service.DetectRepoLayout(repo)

	assert.True(t, layout.IsRepo())
	assert.Equal(t, canon(t, repo), layout.Root)
	assert.Equal(t, canon(t, filepath.Join(repo, ".git")), layout.CommonDir)
	assert.Equal(t, service.RepoKindMain, layout.Kind)
}

func TestDetectRepoLayout_LinkedWorktree(t *testing.T) {
	repo := t.TempDir()
	makeRepo(t, repo)
	wt := filepath.Join(repo, ".worktrees", "doc-sync")
	makeLinkedWorktree(t, repo, wt, "../..")

	layout := service.DetectRepoLayout(wt)

	assert.True(t, layout.IsRepo())
	assert.Equal(t, canon(t, wt), layout.Root, "a linked worktree is its own repository root")
	assert.Equal(t, canon(t, filepath.Join(repo, ".git")), layout.CommonDir,
		"commondir must resolve to the shared git dir, which is the grouping key")
	assert.Equal(t, service.RepoKindWorktree, layout.Kind)
}

func TestDetectRepoLayout_Subdir(t *testing.T) {
	repo := t.TempDir()
	makeRepo(t, repo)
	sub := filepath.Join(repo, "android")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	layout := service.DetectRepoLayout(sub)

	assert.True(t, layout.IsRepo())
	assert.Equal(t, canon(t, repo), layout.Root, "the root is the ancestor holding .git")
	assert.Equal(t, canon(t, filepath.Join(repo, ".git")), layout.CommonDir)
	assert.Equal(t, service.RepoKindSubdir, layout.Kind)
}

func TestDetectRepoLayout_NestedSubdir(t *testing.T) {
	repo := t.TempDir()
	makeRepo(t, repo)
	deep := filepath.Join(repo, "a", "b", "c")
	require.NoError(t, os.MkdirAll(deep, 0o755))

	assert.Equal(t, service.RepoKindSubdir, service.DetectRepoLayout(deep).Kind)
}

func TestDetectRepoLayout_NotARepository(t *testing.T) {
	layout := service.DetectRepoLayout(t.TempDir())

	assert.False(t, layout.IsRepo())
	assert.Empty(t, layout.Root)
	assert.Empty(t, layout.CommonDir)
	assert.Equal(t, service.RepoKindPlain, layout.Kind)
}

func TestDetectRepoLayout_SeparateReposDoNotShareCommonDir(t *testing.T) {
	// Two independent clones whose directory names look alike must NOT be
	// grouped together: grouping keys on git facts, not on names.
	parent := t.TempDir()
	repoA := filepath.Join(parent, "clawbench")
	repoB := filepath.Join(parent, "clawbench-master-backup")
	require.NoError(t, os.MkdirAll(repoA, 0o755))
	require.NoError(t, os.MkdirAll(repoB, 0o755))
	makeRepo(t, repoA)
	makeRepo(t, repoB)

	layoutA := service.DetectRepoLayout(repoA)
	layoutB := service.DetectRepoLayout(repoB)

	assert.Equal(t, service.RepoKindMain, layoutA.Kind)
	assert.Equal(t, service.RepoKindMain, layoutB.Kind)
	assert.NotEqual(t, layoutA.CommonDir, layoutB.CommonDir,
		"independent repositories have distinct common dirs")
}

func TestDetectRepoLayout_BrokenGitFile(t *testing.T) {
	t.Run("empty gitdir pointer is not a repository", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir:\n"), 0o644))

		layout := service.DetectRepoLayout(dir)

		assert.Equal(t, service.RepoKindPlain, layout.Kind)
		assert.False(t, layout.IsRepo())
	})

	t.Run("dangling gitdir pointer stays a linked worktree", func(t *testing.T) {
		// A worktree whose git dir was deleted still reports the worktree kind:
		// linked-ness comes from .git being a file, not from the target being
		// readable. Misreporting it as the main worktree would put the wrong
		// label on the group header.
		//
		// The dangling target is built from t.TempDir() rather than a POSIX
		// literal: "/nonexistent" is not absolute on Windows, so production
		// would resolve it against the worktree root and the expected value
		// below would never match.
		dir := t.TempDir()
		dangling := filepath.Join(t.TempDir(), "deleted-git-dir")
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, ".git"),
			[]byte("gitdir: "+dangling+"\n"), 0o644,
		))

		layout := service.DetectRepoLayout(dir)

		assert.Equal(t, service.RepoKindWorktree, layout.Kind)
		assert.Equal(t, canon(t, dangling), layout.CommonDir)
	})
}

func TestDetectRepoLayout_SymlinkedPathSharesCommonDir(t *testing.T) {
	// A path reached through a symlink must group with the same repository as
	// the real path; otherwise one repository splits into two groups (this is
	// what macOS /var -> /private/var does on every temp dir).
	parent := t.TempDir()
	repo := filepath.Join(parent, "real-repo")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	makeRepo(t, repo)

	link := filepath.Join(parent, "link-to-repo")
	if err := os.Symlink(repo, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	viaLink := service.DetectRepoLayout(link)
	viaReal := service.DetectRepoLayout(repo)

	assert.Equal(t, service.RepoKindMain, viaLink.Kind,
		"a symlink to a repo root is still the main worktree, not a subdir")
	assert.Equal(t, viaReal.CommonDir, viaLink.CommonDir,
		"both routes must resolve to one grouping key")
}

func TestDetectRepoLayout_RelativeGitDirPointer(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "real-git"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".git"), []byte("gitdir: real-git\n"), 0o644,
	))

	layout := service.DetectRepoLayout(dir)

	assert.Equal(t, canon(t, filepath.Join(dir, "real-git")), layout.CommonDir)
	assert.Equal(t, service.RepoKindWorktree, layout.Kind)
}

func TestDetectRepoLayout_AbsoluteCommondir(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	shared := filepath.Join(parent, "shared.git")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	require.NoError(t, os.MkdirAll(shared, 0o755))

	wtGit := filepath.Join(shared, "worktrees", "wt")
	require.NoError(t, os.MkdirAll(wtGit, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(repo, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(wtGit, "commondir"), []byte(shared+"\n"), 0o644,
	))

	assert.Equal(t, canon(t, shared), service.DetectRepoLayout(repo).CommonDir)
}
