package qodercn

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/ai/backends/qoder"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("qodercn", newQoderCNBackend)
	backends.Register(&backends.BackendPlugin{
		ID: "qodercn",
		Spec: model.BackendSpec{
			ID: "qodercn", Backend: "qodercn",
			DefaultCmd: "qoderclicn",
			// `qodercn` is the second bin the CN package declares (both point
			// at the same entry); using it as the fallback lets detection
			// succeed for users who only have that symlink on PATH.
			AltCmd:     "qodercn",
			Name:       "Qoder CN",
			Specialty:  "Qoder 国内版 AI 编码助手",
			AcpCommand: "qoderclicn --acp",
			// initialize advertises loadSession:true and the same
			// sessionCapabilities set as the international build — the two are
			// one codebase with a different brand constant (see
			// docs/dev/qoder_cn_acp_probe_results.md).
			ACPLoadSession: true,
			InstallCmd:     "npm install -g @qodercn-ai/qoderclicn",
			// Sessions are gated on a signed-in account: session/new answers
			// -32000 AUTH_REQUIRED until `qoderclicn login` has run once.
			//
			// This MUST be the CN binary's own command. The two distributions
			// keep separate credentials, so telling a CN user to run
			// `qodercli login` would sign them into the wrong product — and the
			// reverse is worse: pointing the CN binary at the international
			// config root makes it DELETE that credential (it loads the token,
			// rejects it as foreign, then clears it; observed 2026-10-27).
			LoginCmd: "qoderclicn login",
			// SortOrder 20 places it right after OpenClaw (19) rather than next
			// to qoder (5): the picker groups by discovery order, and keeping
			// the two Qoder builds adjacent in the list would imply they are
			// interchangeable when they are separate installs with separate
			// accounts. Appending keeps existing agents' order untouched.
			SortOrder: 20,
			// Same skills subsystem as the international build, but the user
			// tier is the CN dot-directory: the shipped bundle's
			// `saa={user:".qoder-cn",project:".qoder"}` shows the USER skills
			// root follows the brand (`.qoder-cn/skills`) while the PROJECT
			// root stays `.qoder` (shared with the international build), and
			// `getUserAgentSkillsDir()` still resolves to `~/.agents/skills`.
			NativeSkillsDirs:      []string{".qoder-cn/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
		},
	})
}

// newQoderCNBackend returns a CLIBackend instance for Qoder CLI CN.
//
// It reuses the international build's argument builder and stdin-prompt
// prestart hook: the CLI surface is identical (probe L0 asserts all 59 flags
// match), only the binary name differs.
func newQoderCNBackend() ai.AIBackend {
	return qoder.NewCLIBackend("qodercn", "qoderclicn")
}
