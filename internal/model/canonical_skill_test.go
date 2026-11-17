package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCanonicalSkillName pins the ONE normalisation the registry, the settings
// handler and the injection filter share. If the writer and the reader
// canonicalise differently, a silenced skill keeps being injected — the user's
// choice silently has no effect.
func TestCanonicalSkillName_TrimsCaseAndOneSlash(t *testing.T) {
	cases := map[string]string{
		"  Git-Commit  ": "git-commit",
		"/my-skill":      "my-skill",
		"/My_Skill/":     "my_skill/", // only ONE leading slash and whitespace are trimmed
		"":               "",
		"   ":            "",
		"//double":       "/double",
		"UPPER":          "upper",
	}
	for in, want := range cases {
		require.Equal(t, want, CanonicalSkillName(in), in)
	}
}

// TestDisabledSkillSet covers the read side of the per-skill opt-out: an empty
// list is nil (callers use `if set == nil` to skip the lookup), names are
// canonicalised on read, duplicates collapse, and blank entries cannot produce a
// match-everything key.
func TestDisabledSkillSet(t *testing.T) {
	var cfg Config
	require.Nil(t, cfg.DisabledSkillSet(), "an empty list must be nil, not an empty map")

	cfg.Skills.Disabled = []string{"  Git-Commit ", "/my-skill", "MY-SKILL", "", "  "}
	set := cfg.DisabledSkillSet()
	require.Equal(t, map[string]bool{"git-commit": true, "my-skill": true}, set)
	require.True(t, set[CanonicalSkillName("/Git-Commit")],
		"a name written by the UI in any casing must match the stored one")
}
