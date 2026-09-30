package ai

import (
	"strings"

	"clawbench/internal/skill"
)

// skillCommands converts an agent's own native skills into slash-command menu
// entries.
//
// Names are emitted WITHOUT a leading slash, matching the convention of the ACP
// protocol and CodeBuddy's own available_commands_update (which strips the "/"
// from every command name). The frontend adds the single "/" when it renders
// the slash menu. Keeping the stored form slashless also makes dedupe against
// the agent's own slashless skill commands exact (a "/mmx-cli" here would
// otherwise surface as a second, double-slash menu entry).
//
// Only the agent's own native skills belong here: skills injected from another
// source are not executable by this agent's slash mechanism.
//
// Skills whose frontmatter name disagrees with their directory are SKIPPED. An
// agent resolves a skill by directory, so advertising the frontmatter name
// would put an entry in the menu that does nothing when picked. Such a skill is
// still injected into the system prompt (which carries an explicit path).
func skillCommands(skills []skill.Skill) []AvailableCommandInfo {
	var cmds []AvailableCommandInfo
	for _, s := range skills {
		if s.NameMismatch {
			continue
		}
		cmds = append(cmds, AvailableCommandInfo{
			Name:        strings.TrimPrefix(s.Name, "/"),
			Description: s.Description,
		})
	}
	return cmds
}
