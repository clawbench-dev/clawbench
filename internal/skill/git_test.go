package skill

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// gitAvailable reports whether a real git binary is usable, skipping the test
// otherwise. The manager shells out to git (like the rest of the repo), so the
// tests exercise a real local repository rather than a mock.
func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// initRepo creates a local repository with one commit containing a skill.
func initRepo(t *testing.T, dir, skillName string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-q", "-b", "main")
	writeSkill(t, dir, skillName, "---\nname: "+skillName+"\ndescription: from repo\n---\n")
	run("add", "-A")
	run("commit", "-q", "-m", "init")
}

func TestSlugForRepoIsSafeAndCollisionResistant(t *testing.T) {
	a := model.SkillSlug("https://github.com/org/skills.git")
	b := model.SkillSlug("https://github.com/team/skills.git")

	assert.NotEqual(t, a, b, "different repos with the same name must not collide")
	assert.Contains(t, a, "skills")
	// No separators or traversal, so it is safe as a single path segment.
	for _, s := range []string{a, b, model.SkillSlug("git@github.com:a/b.git"), model.SkillSlug("")} {
		assert.NotContains(t, s, "/")
		assert.NotContains(t, s, `\`)
		assert.NotEqual(t, "..", s)
		assert.NotEqual(t, ".", s)
	}
	// Deterministic.
	assert.Equal(t, a, model.SkillSlug("https://github.com/org/skills.git"))
	// The trailing slash and .git suffix do not change the result.
	assert.Equal(t, a, model.SkillSlug("https://github.com/org/skills"))
}

func TestEnsureRepoClonesThenPulls(t *testing.T) {
	gitAvailable(t)

	origDataDir := model.DataDir
	origCfg := model.ConfigInstance
	t.Cleanup(func() {
		model.DataDir = origDataDir
		model.ConfigInstance = origCfg
	})
	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{}

	upstream := filepath.Join(t.TempDir(), "upstream")
	initRepo(t, upstream, "repo-skill")

	repo := model.SkillRepo{URL: "file://" + upstream, Slug: "test-repo"}
	dir, err := EnsureRepo(repo)
	require.NoError(t, err)

	// First call cloned: the skill is present.
	skills := ScanDir(dir, Source{Kind: SourceGit, Dir: dir})
	require.Len(t, skills, 1)
	assert.Equal(t, "repo-skill", skills[0].Name)

	// Add a commit upstream, then pull.
	writeSkill(t, upstream, "second", "---\nname: second\ndescription: added upstream\n---\n")
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = upstream
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-q", "-m", "second")
	cmd.Dir = upstream
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	require.NoError(t, cmd.Run())

	dir2, err := EnsureRepo(repo)
	require.NoError(t, err)
	assert.Equal(t, dir, dir2)
	assert.Len(t, ScanDir(dir, Source{}), 2, "pull must bring in the new skill")
}

func TestPullRepoFailureKeepsLastGoodCheckout(t *testing.T) {
	gitAvailable(t)

	origDataDir := model.DataDir
	origCfg := model.ConfigInstance
	t.Cleanup(func() {
		model.DataDir = origDataDir
		model.ConfigInstance = origCfg
	})
	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{}

	upstream := filepath.Join(t.TempDir(), "upstream")
	initRepo(t, upstream, "stable")

	repo := model.SkillRepo{URL: "file://" + upstream, Slug: "keep-good"}
	dir, err := EnsureRepo(repo)
	require.NoError(t, err)
	require.Len(t, ScanDir(dir, Source{}), 1)

	// Point the repo at an unreachable remote and pull: the failure must not
	// destroy the working tree.
	broken := model.SkillRepo{URL: "file:///nonexistent/repo", Slug: "keep-good"}
	err = PullRepo(broken)
	require.Error(t, err)

	skills := ScanDir(dir, Source{})
	require.Len(t, skills, 1, "a failed pull must leave the last good checkout intact")
	assert.Equal(t, "stable", skills[0].Name)
}

func TestEnsureRepoRejectsBadSlug(t *testing.T) {
	origDataDir := model.DataDir
	t.Cleanup(func() { model.DataDir = origDataDir })
	model.DataDir = t.TempDir()

	_, err := EnsureRepo(model.SkillRepo{URL: "https://example.com/a.git", Slug: "../escape"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slug")
}

func TestAuthURL(t *testing.T) {
	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })

	t.Run("no token leaves the URL untouched", func(t *testing.T) {
		model.ConfigInstance = model.Config{}
		assert.Equal(t, "https://github.com/a/b.git", authURL(model.SkillRepo{URL: "https://github.com/a/b.git"}))
	})

	t.Run("explicit token is injected", func(t *testing.T) {
		model.ConfigInstance = model.Config{}
		got := authURL(model.SkillRepo{URL: "https://github.com/a/b.git", Token: "secret"})
		assert.Contains(t, got, "x-access-token:secret@github.com")
		// The path is preserved.
		assert.Contains(t, got, "/a/b.git")
	})

	t.Run("token with special characters is URL-encoded", func(t *testing.T) {
		model.ConfigInstance = model.Config{}
		got := authURL(model.SkillRepo{URL: "https://github.com/a/b.git", Token: "p@ss:word"})
		// A raw "@" or ":" in the userinfo would corrupt parsing; url.URL encodes it.
		assert.NotContains(t, got, "p@ss:word@")
		assert.Contains(t, got, "github.com/a/b.git")
	})

	t.Run("forge credential is used as fallback", func(t *testing.T) {
		model.ConfigInstance = model.Config{
			Forge: model.ForgeConfig{Credentials: map[string]string{"github.com": "forge-token"}},
		}
		got := authURL(model.SkillRepo{URL: "https://github.com/a/b.git"})
		assert.Contains(t, got, "forge-token")
	})

	t.Run("ssh remotes are not rewritten", func(t *testing.T) {
		model.ConfigInstance = model.Config{
			Forge: model.ForgeConfig{Credentials: map[string]string{"github.com": "forge-token"}},
		}
		// An ssh remote relies on the host's own SSH agent; injecting a token
		// into it would produce an invalid URL.
		assert.Equal(t, "git@github.com:a/b.git", authURL(model.SkillRepo{URL: "git@github.com:a/b.git"}))
		assert.Equal(t, "ssh://git@github.com/a/b.git", authURL(model.SkillRepo{URL: "ssh://git@github.com/a/b.git"}))
	})
}

func TestCheckRepoSizeRejectsHugeTree(t *testing.T) {
	dir := t.TempDir()
	// One sparse file just over the cap.
	f, err := os.Create(filepath.Join(dir, "big"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(maxRepoBytes+1))
	require.NoError(t, f.Close())

	err = checkRepoSize(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

// TestCloneReplacesExistingCheckout verifies the swap path: a re-clone over an
// existing directory must end with the fresh content and no leftover backup.
func TestCloneReplacesExistingCheckout(t *testing.T) {
	gitAvailable(t)

	origDataDir := model.DataDir
	origCfg := model.ConfigInstance
	t.Cleanup(func() {
		model.DataDir = origDataDir
		model.ConfigInstance = origCfg
	})
	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{}

	upstream := filepath.Join(t.TempDir(), "upstream")
	initRepo(t, upstream, "first")

	repo := model.SkillRepo{URL: "file://" + upstream, Slug: "swap-test"}
	dir, err := EnsureRepo(repo)
	require.NoError(t, err)
	require.Len(t, ScanDir(dir, Source{}), 1)

	// Wipe the checkout (keeping no .git) so EnsureRepo takes the clone path
	// again over an existing directory.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, ".git")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stale-marker"), []byte("x"), 0o644))

	dir2, err := EnsureRepo(repo)
	require.NoError(t, err)
	assert.Equal(t, dir, dir2)

	// The fresh clone replaced the old contents...
	_, statErr := os.Stat(filepath.Join(dir, "stale-marker"))
	assert.True(t, os.IsNotExist(statErr), "the old checkout must be gone")
	// ...and the backup directory was cleaned up.
	_, bakErr := os.Stat(dir + ".old")
	assert.True(t, os.IsNotExist(bakErr), "no leftover .old backup")
	assert.Len(t, ScanDir(dir, Source{}), 1)
}
