package antigravity

import (
	"time"

	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Antigravity (agy) is ACP-only: no CLI factory is set (CLI == nil), so
	// chat always uses the ACP stdio transport.
	//
	// The ACP server is the one built into the CLI itself (`agy acp`), NOT the
	// third-party npm adapter `agy-acp`. Measured on agy 1.5.2: the native
	// subcommand answers `initialize` with loadSession + sessionCapabilities
	// {resume, load, list, close, additionalDirectories} and relays prompts and
	// quota errors correctly. The adapter is a Node wrapper that (a) never
	// launches agy for the prompt turn, so the conversation hangs, and (b)
	// polls agy's SQLite DB through a protobuf decoder that leaks until the JS
	// heap OOMs after ~15 minutes — which reaches the user as "AI backend
	// exited abnormally". Pinning `@latest` made that the default for every
	// user, so the command must stay a real subcommand of the CLI the user
	// installed; anyone who still wants the adapter can point a custom ACP
	// agent at it.
	//
	// Requires agy >= 1.5.0: `acp` only appears in `agy --help` from that
	// release. An older CLI answers the spawn with "unknown subcommand", which
	// surfaces as the same "AI backend exited abnormally" this change is meant
	// to remove — so a report of that symptom on an old install means "upgrade
	// agy", not "the backend regressed".
	backends.Register(&backends.BackendPlugin{
		ID: "antigravity",
		Spec: model.BackendSpec{
			ID: "antigravity", Backend: "antigravity", DefaultCmd: "agy", Name: "Antigravity", Specialty: "Google 编码代理",
			ThinkingEffortLevels: []string{"low", "medium", "high"},
			AcpCommand:           "agy acp",
			ACPLoadSession:       true,
			InstallCmd:           "curl -fsSL https://antigravity.google/cli/install.sh | bash",
			// `agy acp` refuses session/new with -32000 Authentication required
			// until the CLI has been signed in. `--login` is agy's own
			// "ACP terminal auth" flag: it signs in with Google and exits,
			// which is exactly what an editor-only (no TUI) user needs.
			// Without a LoginCmd the AUTH_REQUIRED error degrades to the generic
			// "sign in with its CLI" text instead of the command to run.
			LoginCmd:  "agy acp --login",
			SortOrder: 14,
		},
		ModelSource: model.NewCLISource("antigravity", model.CLIOptions{
			Command: "agy",
			Args:    []string{"models"},
			Parse:   parseAgyModels,
			// agy 未登录时会联网重试 ~16s 才输出错误（实测 16.0/16.5/16.0s），
			// 而结果永远是 Fallback 里的 AntigravityCatalog。默认 10s 会在每次
			// 启动白等 10s，这里收紧到 5s：登录用户的一次真实拉取足够，
			// 未登录用户少等一半。
			Timeout:  5 * time.Second,
			Fallback: AntigravityCatalog,
		}),
		ProviderEnv: &model.ProviderEnvSpec{
			Gemini:  model.ProtoEnv("GOOGLE_GEMINI_BASE_URL", "GEMINI_API_KEY", ""),
			KeyOnly: "GEMINI_API_KEY",
			// The private protocol is the account key with the endpoint fields
			// hidden: it always talks to Google's own service.
			OwnServiceEnv: "GEMINI_API_KEY",
		},
	})
}
