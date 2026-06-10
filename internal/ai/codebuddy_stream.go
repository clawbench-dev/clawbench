package ai

// buildCodebuddyStreamArgs constructs the CLI arguments for Codebuddy streaming
func buildCodebuddyStreamArgs(req ChatRequest) []string {
	return buildBaseStreamArgs(req, nil)
}
