package ai

// kimiBackend returns a CLIBackend instance for Kimi CLI.
// Kimi uses Gemini-family stream-json output format (--print --output-format stream-json).
func kimiBackend() *CLIBackend {
	return &CLIBackend{
		name:           "kimi",
		defaultCommand: "kimi",
		buildArgs:      buildKimiStreamArgs,
		newParser:      func() LineParser { return &GeminiStreamParser{} },
		filterLine:     filterSkipNonJSON(),
		preStart:       nil,
	}
}
