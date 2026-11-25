package qwen

import (
	"encoding/json"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// QwenACPTCIDPrefixes maps Qwen's tool names to ClawBench canonical names.
//
// Qwen reports the name in `_meta.toolName` (`read_file`, `provenance:
// builtin`), NOT in the toolCallId — the id is whatever the upstream model
// produced (the capture shows `call_1` coming from a fake endpoint, so the shape
// is provider-defined and no id-prefix table could be trusted).
//
// Only read_file is verified (docs/dev/qwen_acp_probe_results.md). The rest are
// Qwen's own builtin tool names, and every value is a name the shared tables
// already resolve, so an entry that turns out wrong degrades to the generic path
// instead of mis-naming a card.
var QwenACPTCIDPrefixes = map[string]string{
	"read_file":         "Read",
	"write_file":        "Write",
	"edit":              "Edit",
	"run_shell_command": "Bash",
	"glob":              "Glob",
	"grep_search":       "Grep",
	"list_directory":    "LS",
	"web_fetch":         "WebFetch",
	"google_web_search": "WebSearch",
	"ask_user":          "AskUserQuestion",
}

// qwenToolName reads the tool name Qwen puts in `_meta.toolName`, or "" when the
// frame carries none. The frame that STARTS a call carries neither the name nor
// a usable rawInput (`rawInput: {}`); both arrive in the first update.
func qwenToolName(meta acp.Meta) string {
	var name string
	if raw, ok := meta["toolName"]; ok {
		_ = json.Unmarshal(raw, &name)
	}
	return name
}

// parseQwenToolCallUpdate is the stock generic update mapper plus one rule: a
// frame that carries `_meta.toolName` names the tool.
//
// The stock _meta parser only replaces a name that is still lowercase, and the
// generic kind path capitalises (`kind=read` → "Read"). Since the start frame is
// unnamed, a completed read would otherwise keep the kind-derived name and never
// reach qwen's own table — the per-backend mapping this plugin exists for.
func parseQwenToolCallUpdate(tcu acp.SessionToolCallUpdate) *ai.ToolCall {
	tool := ai.ParseGenericACPToolCallUpdate(tcu, "qwen")
	if meta, ok := tcu.Meta.Value(); ok {
		if name := qwenToolName(meta); name != "" {
			tool.Name = name
		}
	}
	return tool
}

func init() {
	// Qwen Code runs over ACP only: `qwen --acp` starts a standard ACP server on
	// stdio.
	backends.Register(&backends.BackendPlugin{
		ID: "qwen",
		Spec: model.BackendSpec{
			ID: "qwen", Backend: "qwen", DefaultCmd: "qwen", Name: "Qwen Code", Specialty: "通义千问编码助手",
			AcpCommand:     "qwen --acp",
			ACPLoadSession: true, // initialize advertises loadSession:true
			InstallCmd:     "npm install -g @qwen-code/qwen-code",
			// qwen's interactive /auth wizard picks the auth type and, for openai,
			// asks for the key/base URL/model. No flags are documented for it, so
			// the login hint is the plain command.
			LoginCmd: "qwen",
			// qwen loads its own skills into the model context: one probe turn of
			// `qwen --acp` carried an <available_skills> table whose 19 entries
			// include 6 of this user's own ~/.agents/skills (find-skills, grill-me,
			// guizang-ppt-skill, ...) with <location>user</location>. The format
			// string lives in the shipped package (getUserSkillsDirs +
			// SKILL_PROVIDER_CONFIG_DIRS [".qwen", ".agents"]) and the ACP agent is
			// the same module that builds it, so the ACP transport uses that code.
			//
			// Project level is NOT declared: a SKILL.md in <cwd>/.qwen/skills or
			// <cwd>/.agents/skills, and a new dir under ~/.qwen/skills, did not
			// reach the model in the same session where the user-level ones did.
			// Declaring an unproven directory would suppress ClawBench's injection
			// and could leave the agent with no project skills — the unsafe side.
			NativeSkillsDirs:      []string{".qwen/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			// ThinkingEffortLevels is deliberately nil. Effort is per-model inside
			// qwen (qwen3.8-* take low/medium/xhigh; qwen3.5/3.6/3.7 are thinking
			// on/off only), and with a bound provider the model is whatever the
			// endpoint serves. For the OpenAI-compatible model on 0.28.4,
			// session/new advertised NO thought_level option, so a static list
			// would surface a dead control; a level the agent omits or advertises
			// is taken from the session at runtime.
			SortOrder: 21,
		},
		ACP: &backends.ACPPlugin{
			ToolCallIDPrefixes: QwenACPTCIDPrefixes,
			// No InputRemaps: the one rawInput we could observe is already
			// canonical (`file_path`), and qwen's camelCase spellings are covered
			// by the shared generic set. A copy here would be an unverified second
			// place to keep in sync.
		},
		Behavior: &ai.ACPBehavior{
			ParseToolCallUpdate: parseQwenToolCallUpdate,
		},
		ModelSource: model.StaticSource("qwen", "qwen", QwenCatalog),
		ProviderEnv: &model.ProviderEnvSpec{
			// Qwen's only non-interactive credential path is OpenAI-compatible
			// (authType "openai": OPENAI_API_KEY / OPENAI_BASE_URL / OPENAI_MODEL).
			// Verified two ways: AUTH_ENV_MAPPINGS in the shipped package, and a
			// successful session/new with exactly these variables set. No Anthropic
			// row on purpose — qwen honors ANTHROPIC_* for authType "anthropic",
			// but that path was not verified over ACP.
			OpenAI: model.ProtoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
			// ModelEnv: with openai auth qwen refuses session/new with -32000
			// "Authentication required: ... configure a model first" unless a model
			// is known at STARTUP. The value is therefore the provider's default
			// model (or the agent's pinned one) — never a static name, since a
			// bound endpoint may not serve qwen3.8-max at all.
			//
			// KNOWN GAP (measured on 0.28.4): the request model is always that
			// startup one. An in-session switch does not reach the wire — the
			// generic MODEL variable and the "model" config option both left the
			// outgoing OPENAI_MODEL value unchanged — so changing models under BYOK
			// means editing the provider's default model.
			ModelEnv: "OPENAI_MODEL",
		},
		OrphanCmdline: ai.OrphanCmdlinePattern{Binary: "qwen", Arg: "--acp"},
	})
}
