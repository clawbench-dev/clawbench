package vecli

import "clawbench/internal/ai"

func init() {
	ai.RegisterBackend("vecli", newVeCLIBackend, false)
}

// newVeCLIBackend returns a VeCLIBackend instance.
// VeCLI is a custom backend — it wraps CLIBackend with additional
// session-summary post-processing. AutoResume is not needed.
func newVeCLIBackend() ai.AIBackend {
	return ai.NewVeCLIBackend()
}
