package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/skill"
)

func TestSkillCommands(t *testing.T) {
	t.Run("strips a leading slash and keeps order", func(t *testing.T) {
		got := skillCommands([]skill.Skill{
			{Name: "mmx-cli", Description: "a"},
			{Name: "/other", Description: "b"},
		})
		assert.Equal(t, []AvailableCommandInfo{
			{Name: "mmx-cli", Description: "a"},
			{Name: "other", Description: "b"},
		}, got)
	})

	// A name that disagrees with its directory must NOT become a menu entry:
	// an agent resolves a skill by directory, so the frontmatter name would be
	// an entry that does nothing when picked. The skill is still injected into
	// the system prompt, which carries an explicit path.
	t.Run("skips name-mismatched skills", func(t *testing.T) {
		got := skillCommands([]skill.Skill{
			{Name: "good", Description: "ok"},
			{Name: "wrong-name", Description: "bad", NameMismatch: true},
		})
		assert.Equal(t, []AvailableCommandInfo{{Name: "good", Description: "ok"}}, got)
	})

	t.Run("all mismatched yields no commands", func(t *testing.T) {
		got := skillCommands([]skill.Skill{{Name: "x", NameMismatch: true}})
		assert.Empty(t, got)
	})

	t.Run("empty input", func(t *testing.T) {
		assert.Empty(t, skillCommands(nil))
	})
}
