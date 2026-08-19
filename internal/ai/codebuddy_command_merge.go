package ai

// MergeCommands merges two command lists, deduplicating by command name.
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

	// Build set of names already present in ACP commands
	seen := make(map[string]struct{}, len(acpCommands))
	result := make([]AvailableCommandInfo, 0, len(acpCommands)+len(pluginCommands))
	for _, cmd := range acpCommands {
		seen[cmd.Name] = struct{}{}
		result = append(result, cmd)
	}

	// Add plugin commands not already present
	for _, cmd := range pluginCommands {
		if _, exists := seen[cmd.Name]; !exists {
			result = append(result, cmd)
		}
	}

	return result
}
