package copilot

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// CopilotACPTitlePrefixes maps the tool-call TITLE prefixes GitHub Copilot CLI
// puts on the wire to ClawBench's canonical tool names.
//
// Measured on copilot 1.0.115-8 (docs/dev/copilot_acp_probe_results.md). Copilot
// names a tool by its title and puts nothing resolvable in the toolCallId
// (`call_<what the model asked for>`), and several titles name no tool at all:
// "write_agent" is the agent-authoring tool, "Reading shell output" is
// `read_bash`, "Searching for 'x'" is `grep`. The shared alias table and the
// kind mapping both get those wrong (kind alone would make every search a
// Read), so the titles it emits are mapped here. Every entry was observed in a
// real frame.
var CopilotACPTitlePrefixes = map[string]string{
	// Titles Copilot renders itself ("Running: echo hi", "Searching for 'x'",
	// "Finding files matching **/*.go", "Reading shell output"). The string
	// before ": " there is a word, not a tool name, and these four collide with
	// nothing else, so they are listed as-is and the lookup adds ": " to them;
	// "Running command" in turn is the form shared by other agents.
	"Running":                "Bash",
	"Running command":        "Bash",
	"Searching for":          "Grep",
	"Finding files matching": "Glob",
	"Reading shell output":   "Read",
	"Reading file":           "Read",
	// Titles that are the bare tool name, passed through when the model calls
	// a tool Copilot has no rendering for.
	"bash":                 "Bash",
	"view":                 "Read",
	"create":               "Write",
	"edit":                 "Edit",
	"grep":                 "Grep",
	"glob":                 "Glob",
	"web_fetch":            "WebFetch",
	"task":                 "Agent",
	"read_agent":           "Agent",
	"list_agents":          "Agent",
	"write_agent":          "Agent",
	"semantic_code_search": "Grep",
	"read_bash":            "Bash",
	"stop_bash":            "Bash",
	"list_bash":            "Bash",
}

// ProviderTypeEnv names the variable that selects the wire family. It is set
// per protocol in the ProviderEnv rows below (ProtocolEnv.Extra), so the
// env-building path emits the value that matches the bound provider.
const ProviderTypeEnv = "COPILOT_PROVIDER_TYPE"

// ProviderType maps a ClawBench protocol to the COPILOT_PROVIDER_TYPE value the
// CLI accepts, and "" for a protocol it cannot express. The accepted set is the
// CLI's own: an unknown value is refused at startup with "Must be one of:
// openai, azure, anthropic, google". openai, anthropic and google were each
// measured to create and run a session.
//
// The ProviderEnv rows below are built from this function, so a new protocol
// cannot be added with a family that disagrees with it.
func ProviderType(protocol string) string {
	switch protocol {
	case model.ProviderProtocolOpenAI:
		return "openai"
	case model.ProviderProtocolAnthropic:
		return "anthropic"
	case model.ProviderProtocolGemini:
		return "google"
	}
	return ""
}

func init() {
	// GitHub Copilot CLI is ACP-only: `copilot --acp` runs a standard Agent
	// Client Protocol server over stdio (measured on 1.0.115-8; see
	// docs/dev/copilot_acp_probe_results.md).
	backends.Register(&backends.BackendPlugin{
		ID: "copilot",
		Spec: model.BackendSpec{
			ID: "copilot", Backend: "copilot", DefaultCmd: "copilot", Name: "GitHub Copilot",
			Specialty: "GitHub 官方编码智能体",
			// Measured: initialize advertises loadSession:true and
			// sessionCapabilities {close, list}, so the explicit acp-load /
			// acp-sync endpoints work and no on-disk session scanner is needed.
			AcpCommand:     "copilot --acp",
			ACPLoadSession: true,
			// Measured: an MCP server passed to session/new is attached, its tool
			// is declared to the model and CALLED in the first turn, so
			// MCPUnsupported / MCPRejectsSSE / MCPAttachesAsync all stay unset.
			//
			// ThinkingEffortLevels is intentionally nil: the CLI has a
			// --reasoning-effort flag, but the ACP session advertises no
			// `thought_level` config option (configOptions are mode / model /
			// allow_all), so a static list would render a control whose value
			// never reaches the agent (same posture as commandcode/minimax).
			InstallCmd: "npm install -g @github/copilot",
			LoginCmd:   "copilot login",
			SortOrder:  23,
			// Measured on 1.0.115-8: the ACP session advertises the user's
			// skills through availableCommands by itself — a skill dropped in
			// <HOME>/.copilot/skills/ or <HOME>/.agents/skills/ appeared at
			// session/new, and a project's .github/skills/ and .claude/skills/
			// appeared for a session whose cwd was that project. So ClawBench
			// must not inject them again.
			//
			// Both personal directories are declared: `copilot skill list` names
			// "Personal  ~/.copilot/skills/ or ~/.agents/skills/" and the ACP
			// session read from both in the probe. `.agents/skills` is also
			// declared by other backends, which is what makes a skill installed
			// through `npx skills` visible to every agent that reads it.
			NativeSkillsDirs:      []string{".copilot/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
		},
		ACP: &backends.ACPPlugin{
			ToolTitlePrefixes: CopilotACPTitlePrefixes,
			// No InputRemaps: observed rawInput is {command}, {path, content} or
			// {pattern, path} — canonical or covered by the shared
			// command/exec remap.
		},
		// No Behavior hooks: every frame observed was the standard ACP surface
		// (tool_call / tool_call_update with content + rawOutput, config option
		// updates, session_info_update, usage_update).
		ModelSource: model.PluginSource("copilot", discoverCopilotModels),
		ProviderEnv: &model.ProviderEnvSpec{
			// Copilot's own models need a GitHub Copilot subscription, and no
			// environment variable authenticates the ACP transport: session/new
			// answers -32000 "Authentication required" with GH_TOKEN /
			// GITHUB_TOKEN / COPILOT_GITHUB_TOKEN set (the ACP server logs "no
			// authentication available for this ACP connection" and rejects a
			// classic PAT outright), so only `copilot login` gets there.
			//
			// BYOK is the path that needs no GitHub account ("GitHub
			// authentication isn't required", `copilot help providers-env`), and
			// it is what all three rows below bind. Each row carries its own
			// wire family in Extra: COPILOT_PROVIDER_TYPE accepts openai / azure
			// / anthropic / google and REFUSES anything else at startup
			// ("Must be one of: ..."), so the value has to follow the protocol.
			// Copilot has no azure row to bind here because ClawBench has no
			// azure protocol — an Azure OpenAI deployment is reached as an
			// OpenAI-compatible endpoint.
			//
			// The model is a fourth variable, ModelEnv below. Worth knowing for
			// anyone touching this: the session's model candidate set is fixed
			// at session/new, so a switch can only reach a model the session
			// already advertises — measured, anything else answers -32602
			// "not one of this session's models", and "auto" answers -32603
			// "Auto is unavailable for this account". That is why the model has
			// to be a startup variable rather than a per-session choice.
			OpenAI: &model.ProtocolEnv{
				BaseURL: "COPILOT_PROVIDER_BASE_URL",
				APIKey:  "COPILOT_PROVIDER_API_KEY",
				Extra:   map[string]string{ProviderTypeEnv: ProviderType(model.ProviderProtocolOpenAI)},
			},
			Anthropic: &model.ProtocolEnv{
				BaseURL: "COPILOT_PROVIDER_BASE_URL",
				APIKey:  "COPILOT_PROVIDER_API_KEY",
				Extra:   map[string]string{ProviderTypeEnv: ProviderType(model.ProviderProtocolAnthropic)},
			},
			Gemini: &model.ProtocolEnv{
				BaseURL: "COPILOT_PROVIDER_BASE_URL",
				APIKey:  "COPILOT_PROVIDER_API_KEY",
				Extra:   map[string]string{ProviderTypeEnv: ProviderType(model.ProviderProtocolGemini)},
			},
			// BYOK refuses to start a session without an explicit model
			// ("BYOK providers require an explicit model"), and settings.json is
			// NOT a way to supply it — measured: with only "model" in the file
			// the CLI still refuses. The three variables that do work are
			// COPILOT_MODEL, COPILOT_PROVIDER_MODEL_ID and --model; this one is
			// chosen because it declares only the id and leaves the wire model
			// name alone (COPILOT_MODEL would also rewrite what is sent to the
			// endpoint).
			ModelEnv: "COPILOT_PROVIDER_MODEL_ID",
		},
		// Only reached when the CLAWBENCH_CHILD marker is unreadable.
		OrphanCmdline: ai.OrphanCmdlinePattern{Binary: "copilot", Arg: "--acp"},
	})
}
