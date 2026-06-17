package codex

import (
	"clawbench/internal/ai"
)

func init() {
	ai.RegisterBackend("codex", newCodexBackend, false)
}

// newCodexBackend returns a CodexBackend instance.
// Codex is a custom backend — it directly implements AIBackend,
// not using the CLIBackend skeleton. AutoResume is not needed.
func newCodexBackend() ai.AIBackend {
	return &ai.CodexBackend{}
}
