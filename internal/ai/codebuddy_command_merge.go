package ai

import "strings"

// canonicalCommandName normalizes a command name for identity comparison by
// stripping a single leading "/". Command producers disagree on the slash
// prefix: CodeBuddy's own AvailableCommandsUpdate strips it (skills arrive as
// "mmx-cli"), while our pre-scan of ~/.codebuddy/skills/ prefixes it
// (SkillsToCommands produces "/mmx-cli"). Comparing raw names makes the same
// skill appear twice in the slash menu (double "/" in the UI). The canonical
// form is used only as a dedupe key; the original Name is kept for display and
// execution.
func canonicalCommandName(name string) string {
	return strings.TrimPrefix(name, "/")
}

// MergeCommands merges two command lists, deduplicating by command name
// (canonical — leading "/" ignored, see canonicalCommandName).
// acpCommands take precedence — they may carry InputHint from the ACP protocol
// that pre-scanned commands cannot provide.
// Returns the merged list in stable order: ACP commands first, then new plugin commands.
func MergeCommands(acpCommands, pluginCommands []AvailableCommandInfo) []AvailableCommandInfo {
	if len(acpCommands) == 0 {
		return pluginCommands
	}
	if len(pluginCommands) == 0 {
		return acpCommands
	}

	// Build set of canonical names already present in ACP commands
	seen := make(map[string]struct{}, len(acpCommands))
	result := make([]AvailableCommandInfo, 0, len(acpCommands)+len(pluginCommands))
	for _, cmd := range acpCommands {
		seen[canonicalCommandName(cmd.Name)] = struct{}{}
		result = append(result, cmd)
	}

	// Add plugin commands not already present
	for _, cmd := range pluginCommands {
		if _, exists := seen[canonicalCommandName(cmd.Name)]; !exists {
			result = append(result, cmd)
		}
	}

	return result
}
