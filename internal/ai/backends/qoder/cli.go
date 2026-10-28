package qoder

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("qoder", newQoderBackend)
	backends.Register(&backends.BackendPlugin{
		ID: "qoder",
		Spec: model.BackendSpec{
			ID: "qoder", Backend: "qoder", DefaultCmd: "qodercli", Name: "Qoder", Specialty: "AI 编码助手",
			AcpCommand:     "qodercli --acp",
			ACPLoadSession: true,
			InstallCmd:     "curl -fsSL https://qoder.com/install | bash",
			SortOrder:      5,
			// Qoder loads its own skills (verified in the shipped CLI: it
			// references ".qoder/skills", ".agents/skills" and "SKILL.md", and
			// has an "Agent skills reloaded successfully." path), so ClawBench
			// must not inject them again.
			NativeSkillsDirs:      []string{".qoder/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			// Sessions are gated on a signed-in account: without one,
			// session/new answers -32000 AUTH_REQUIRED. Naming the login
			// command here renders that as an actionable instruction instead
			// of a raw JSON-RPC blob. The CN build needs its own (qoderclicn
			// login) — see backends/qodercn.
			LoginCmd: "qodercli login",
		},
	})
}

// newQoderBackend returns a CLIBackend instance for Qoder CLI.
func newQoderBackend() ai.AIBackend {
	return NewCLIBackend("qoder", "qodercli")
}
