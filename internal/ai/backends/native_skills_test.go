package backends_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/ai/backends"
	_ "clawbench/internal/ai/backends/antigravity"
	_ "clawbench/internal/ai/backends/claude"
	_ "clawbench/internal/ai/backends/codebuddy"
	_ "clawbench/internal/ai/backends/codex"
	_ "clawbench/internal/ai/backends/copilot"
	_ "clawbench/internal/ai/backends/deepseek"
	_ "clawbench/internal/ai/backends/dsh"
	_ "clawbench/internal/ai/backends/grok"
	_ "clawbench/internal/ai/backends/kimi"
	_ "clawbench/internal/ai/backends/mimo"
	_ "clawbench/internal/ai/backends/opencode"
	_ "clawbench/internal/ai/backends/pi"
	_ "clawbench/internal/ai/backends/qoder"
	_ "clawbench/internal/ai/backends/vecli"
	_ "clawbench/internal/ai/backends/zcode"
)

// skillAwareBackends is the EXPECTED mapping of backend → does it load its own
// skill directory. It is the reviewed source of truth, so a backend cannot
// change this behavior by accident.
//
// A missing entry fails the test in BOTH directions, which is the point: the
// dangerous mistake is silently flipping AutoLoadsNativeSkills to false for a
// skill-aware backend, because that makes ClawBench inject a second copy of the
// table the agent already built. The values were verified against the installed
// CLIs (grep their binary/package for "SKILL.md" / "available_skills"), not
// inferred from the vendor's marketing.
var skillAwareBackends = map[string]bool{
	// Load their own skills → ClawBench must NOT re-inject.
	"claude":   true, // .claude/skills (claude.exe)
	"codex":    true, // core-skills crate; .codex/skills + .agents/skills
	"copilot":  true, // .copilot/skills + .github/skills + .agents/skills
	"opencode": true, // .config/opencode/skill + .opencode/skills + .agents/skills
	"mimo":     true, // OpenCode fork: .mimocode/skills + .agents/skills
	"qoder":    true, // .qoder/skills + .agents/skills
	"dsh":      true, // .dsh/skills + .agents/skills
	"deepseek": true, // CodeWhale: .codewhale/skills + .agents/skills
	"pi":       true, // ~/.pi/agent/skills + <available_skills> injection

	// Do NOT load their own skills → ClawBench must inject.
	//
	// codebuddy is the important one: its TUI reads ~/.codebuddy/skills but its
	// ACP process does not, which is why ClawBench has always injected them.
	// Setting this true would make CodeBuddy agents lose every skill.
	"codebuddy": false,

	// No skill directory at all (nothing to load, nothing to inject).
	"antigravity": false, // skills bundled read-only inside the CLI
	"kimi":        false,
	"grok":        false,
	"zcode":       false,
	"vecli":       false,
}

// TestBackendSkillAwarenessMatchesReviewedTable fails if a backend's actual
// skill behavior differs from the reviewed table, in either direction.
func TestBackendSkillAwarenessMatchesReviewedTable(t *testing.T) {
	for _, p := range backends.All() {
		// Test-only plugins register with an empty Backend stub.
		if p.Spec.Backend == "" {
			continue
		}
		expected, reviewed := skillAwareBackends[p.ID]
		if !reviewed {
			t.Errorf("backend %q has no entry in skillAwareBackends; review whether it loads its own skills, then add it (guessing risks either a duplicated skill table or a backend with no skills at all)", p.ID)
			continue
		}
		assert.Equal(t, expected, p.Spec.AutoLoadsNativeSkills,
			"backend %q: AutoLoadsNativeSkills must be %v (see the comment on its entry)", p.ID, expected)
	}
}

// TestSkillAwareBackendsDeclareTheirDirectory pins the other half: a backend
// that claims to load skills must say which directory it loads, otherwise
// ClawBench cannot dedup against it (the skill would be injected twice under a
// different source).
func TestSkillAwareBackendsDeclareTheirDirectory(t *testing.T) {
	for _, p := range backends.All() {
		if p.Spec.Backend == "" {
			continue
		}
		if p.Spec.AutoLoadsNativeSkills {
			assert.NotEmpty(t, p.Spec.NativeSkillsDirs,
				"backend %q loads its own skills, so it must declare NativeSkillsDirs", p.ID)
		}
	}
}

// TestNativeSkillsDirsAreHomeRelative pins the contract that each value is
// relative to the user's home (it is joined with os.UserHomeDir), so an
// absolute path would be silently mis-resolved.
func TestNativeSkillsDirsAreHomeRelative(t *testing.T) {
	for _, p := range backends.All() {
		for _, dir := range p.Spec.NativeSkillsDirs {
			assert.NotContains(t, dir, "\\", "backend %q: use forward slashes", p.ID)
			assert.NotEqual(t, byte('/'), dir[0],
				"backend %q: NativeSkillsDirs entries must be relative to the home directory", p.ID)
		}
	}
}

// TestSharedSkillsDirDeclaredWhereReadable pins that every backend known to read
// the cross-tool ~/.agents/skills declares it. Without the declaration the
// backend's own copy wins dedup and ClawBench injects a second one.
func TestSharedSkillsDirDeclaredWhereReadable(t *testing.T) {
	// Verified by grepping each shipped CLI for ".agents/skills".
	readsShared := map[string]bool{
		"codex": true, "copilot": true, "opencode": true,
		"mimo": true, "qoder": true, "dsh": true, "deepseek": true,
	}
	for _, p := range backends.All() {
		if !readsShared[p.ID] {
			continue
		}
		assert.Contains(t, p.Spec.NativeSkillsDirs, ".agents/skills",
			"backend %q reads the shared .agents/skills, so it must declare it", p.ID)
	}
}
