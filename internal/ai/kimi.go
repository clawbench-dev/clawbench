package ai

// kimiBackend returns a CLIBackend instance for Kimi CLI.
// Kimi uses stream-json output format (--print --output-format stream-json).
func kimiBackend() *CLIBackend {
	return &CLIBackend{
		name:           "kimi",
		defaultCommand: "kimi",
		buildArgs:      buildKimiStreamArgs,
		newParser:      func() LineParser { return &StreamJSONParser{} },
		filterLine:     filterSkipNonJSON(),
		preStart:       nil,
	}
}
