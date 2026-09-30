package model

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillSlug(t *testing.T) {
	a := SkillSlug("https://github.com/org/skills.git")
	b := SkillSlug("https://github.com/team/skills.git")
	assert.NotEqual(t, a, b, "different repos with the same name must not collide")
	assert.Contains(t, a, "skills")
	for _, s := range []string{a, b, SkillSlug("git@github.com:a/b.git"), SkillSlug("")} {
		assert.NotContains(t, s, "/")
		assert.NotContains(t, s, `\`)
		assert.NotEqual(t, "..", s)
		assert.NotEqual(t, ".", s)
	}
	assert.Equal(t, a, SkillSlug("https://github.com/org/skills.git"), "must be deterministic")
	assert.Equal(t, a, SkillSlug("https://github.com/org/skills"), "trailing slash / .git must not matter")
}

// TestSkillSlug_SanitizesUnsafeCharacters pins that every character outside
// [a-z0-9._-] is replaced (not dropped) and that a name with nothing usable
// still yields a valid "repo-<hash>" slug rather than an empty prefix.
func TestSkillSlug_SanitizesUnsafeCharacters(t *testing.T) {
	// Uppercase, spaces and unicode all collapse to '-'.
	got := SkillSlug("https://example.com/My Skills/Ünïcode Repo")
	assert.NotContains(t, got, " ")
	assert.NotContains(t, got, "/")
	assert.NotContains(t, got, "Ü")
	assert.Contains(t, got, "repo", "the readable prefix must survive sanitization")

	// A name that sanitizes to nothing falls back to "repo".
	empty := SkillSlug("https://example.com/！！！")
	assert.Contains(t, empty, "repo", "an unusable name must fall back to the 'repo' prefix")
	assert.NotContains(t, empty, "！")
}

// TestSkillSlug_TruncatesLongName pins the length bound: the readable prefix is
// capped at maxSkillSlugLen, and the hash suffix (the collision guard) is
// appended AFTER truncation so it always survives.
func TestSkillSlug_TruncatesLongName(t *testing.T) {
	long := strings.Repeat("a", maxSkillSlugLen+50)
	got := SkillSlug("https://example.com/" + long)
	// prefix (<=64) + "-" + 8 hex chars.
	assert.LessOrEqual(t, len(got), maxSkillSlugLen+1+8)
	assert.NotEqual(t, SkillSlug("https://example.com/"+long+"x"), got,
		"the hash must still distinguish names that truncate to the same prefix")
}

// TestResolveSkillsDirs pins the resolution order: configured entries win,
// blank entries are dropped (an empty string would otherwise resolve to the
// scan root), and an all-blank/empty list falls back to the default dir.
func TestResolveSkillsDirs(t *testing.T) {
	origDataDir := DataDir
	t.Cleanup(func() { DataDir = origDataDir })
	DataDir = t.TempDir()
	defaultDir := filepath.Join(DataDir, "skills-user")

	t.Run("configured dirs are returned in order", func(t *testing.T) {
		cfg := Config{Skills: SkillsConfig{Dirs: []string{"/a", "/b"}}}
		assert.Equal(t, []string{"/a", "/b"}, cfg.ResolveSkillsDirs())
	})

	t.Run("blank entries are dropped", func(t *testing.T) {
		cfg := Config{Skills: SkillsConfig{Dirs: []string{"/a", "", "/b"}}}
		assert.Equal(t, []string{"/a", "/b"}, cfg.ResolveSkillsDirs())
	})

	t.Run("only blanks falls back to the default", func(t *testing.T) {
		cfg := Config{Skills: SkillsConfig{Dirs: []string{"", ""}}}
		assert.Equal(t, []string{defaultDir}, cfg.ResolveSkillsDirs())
	})

	t.Run("empty list falls back to the default", func(t *testing.T) {
		cfg := Config{}
		assert.Equal(t, []string{defaultDir}, cfg.ResolveSkillsDirs())
	})

	t.Run("no default available returns empty", func(t *testing.T) {
		saved := DataDir
		DataDir = ""
		defer func() { DataDir = saved }()
		cfg := Config{}
		assert.Empty(t, cfg.ResolveSkillsDirs())
	})
}

// TestApplyDefaults_SkillsMigratesLegacyDir pins the migration from the
// pre-multi-directory `dir` key to `dirs`. Without it, upgrading silently drops
// the user's configured skill directory.
func TestApplyDefaults_SkillsMigratesLegacyDir(t *testing.T) {
	origDataDir := DataDir
	t.Cleanup(func() { DataDir = origDataDir })
	DataDir = t.TempDir()

	t.Run("legacy dir becomes the single entry in dirs", func(t *testing.T) {
		cfg := Config{Skills: SkillsConfig{LegacyDir: "/legacy/skills"}}
		ApplyDefaults(&cfg, nil)
		assert.Equal(t, []string{"/legacy/skills"}, cfg.Skills.Dirs)
		assert.Empty(t, cfg.Skills.LegacyDir, "the legacy key must be cleared so it is not written back")
	})

	t.Run("dirs wins when both are present", func(t *testing.T) {
		cfg := Config{Skills: SkillsConfig{Dirs: []string{"/new/a"}, LegacyDir: "/legacy"}}
		ApplyDefaults(&cfg, nil)
		assert.Equal(t, []string{"/new/a"}, cfg.Skills.Dirs)
		assert.Empty(t, cfg.Skills.LegacyDir)
	})

	t.Run("neither present falls back to the default dir", func(t *testing.T) {
		cfg := Config{}
		ApplyDefaults(&cfg, nil)
		assert.Equal(t, []string{filepath.Join(DataDir, "skills-user")}, cfg.Skills.Dirs)
	})

	t.Run("enabled defaults to true", func(t *testing.T) {
		cfg := Config{}
		ApplyDefaults(&cfg, nil)
		assert.True(t, cfg.Skills.Enabled)
	})

	t.Run("an explicit disable is respected", func(t *testing.T) {
		cfg := Config{}
		ApplyDefaults(&cfg, map[string]bool{"skills.enabled": true})
		assert.False(t, cfg.Skills.Enabled)
	})

	t.Run("refresh_hours defaults to 6 but 0 is expressible", func(t *testing.T) {
		cfg := Config{}
		ApplyDefaults(&cfg, nil)
		assert.Equal(t, 6, cfg.Skills.RefreshHours)

		cfg = Config{}
		ApplyDefaults(&cfg, map[string]bool{"skills.refresh_hours": true})
		assert.Equal(t, 0, cfg.Skills.RefreshHours, "an explicit 0 (startup-only) must survive")

		cfg = Config{Skills: SkillsConfig{RefreshHours: -1}}
		ApplyDefaults(&cfg, map[string]bool{"skills.refresh_hours": true})
		assert.Equal(t, 0, cfg.Skills.RefreshHours, "a negative value clamps to 0")
	})

	t.Run("repo slugs are derived", func(t *testing.T) {
		cfg := Config{Skills: SkillsConfig{Repos: []SkillRepo{{URL: "https://github.com/a/b.git"}}}}
		ApplyDefaults(&cfg, nil)
		require.Len(t, cfg.Skills.Repos, 1)
		assert.NotEmpty(t, cfg.Skills.Repos[0].Slug)
	})
}
