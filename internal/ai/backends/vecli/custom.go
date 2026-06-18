package vecli

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("vecli", newVeCLIBackend, false)
	backends.Register(&backends.BackendPlugin{
		ID: "vecli",
		Spec: model.BackendSpec{
			ID: "vecli", Backend: "vecli", DefaultCmd: "vecli", Name: "VeCLI", Icon: "🌿", Specialty: "字节跳动 AI 助手",
			SortOrder: 6,
		},
	})
}

// newVeCLIBackend returns a VeCLIBackend instance.
// VeCLI is a custom backend — it wraps CLIBackend with additional
// session-summary post-processing. AutoResume is not needed.
func newVeCLIBackend() ai.AIBackend {
	return ai.NewVeCLIBackend()
}
