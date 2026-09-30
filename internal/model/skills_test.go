package model

import (
	"path/filepath"
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
