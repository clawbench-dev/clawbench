package commandcode

import (
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestCommandCodeBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("commandcode")
	require.NotNil(t, plugin, "commandcode should be registered in backends registry")
	assert.Equal(t, "commandcode", plugin.ID)
}

func TestCommandCodeBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("commandcode")
	require.NotNil(t, plugin)

	assert.Equal(t, "commandcode", plugin.Spec.ID)
	assert.Equal(t, "commandcode", plugin.Spec.Backend)
	assert.Equal(t, "Command Code", plugin.Spec.Name)
	assert.NotEmpty(t, plugin.Spec.Specialty)
	assert.Equal(t, "cmdc acp", plugin.Spec.AcpCommand)
	assert.True(t, plugin.Spec.ACPLoadSession, "initialize advertises loadSession:true")
	assert.Equal(t, "npm install -g command-code", plugin.Spec.InstallCmd)
	assert.Equal(t, 18, plugin.Spec.SortOrder)
	// LoginCmd drives the AUTH_REQUIRED message. Without it the user gets the
	// generic fallback instead of the command they actually have to run.
	assert.Equal(t, "cmdc login", plugin.Spec.LoginCmd)
}

// DefaultCmd must never be the bare `cmd`: on Windows that name always resolves
// to the system shell, so probeBackendPresence would create a phantom agent for
// every Windows user and then spawn `cmd.exe acp`. `cmdc` is a shipped alias
// that works on all platforms.
func TestCommandCodeBackendPlugin_NeverUsesBareCmd(t *testing.T) {
	plugin := backends.Lookup("commandcode")
	require.NotNil(t, plugin)

	assert.NotEqual(t, "cmd", plugin.Spec.DefaultCmd,
		"`cmd` always resolves on Windows (system shell) and would create a phantom agent")
	assert.NotEqual(t, "cmd", plugin.Spec.AltCmd)
	assert.Equal(t, "cmdc", plugin.Spec.DefaultCmd)
	assert.False(t, strings.HasPrefix(plugin.Spec.AcpCommand, "cmd "),
		"AcpCommand must not invoke the bare `cmd` name")
}

// The ACP command must spawn a binary that detection also checks, or a user
// passing detection would fail to spawn.
func TestCommandCodeBackendPlugin_AcpCommandMatchesDefaultCmd(t *testing.T) {
	plugin := backends.Lookup("commandcode")
	require.NotNil(t, plugin)
	assert.True(t, strings.HasPrefix(plugin.Spec.AcpCommand, plugin.Spec.DefaultCmd),
		"AcpCommand %q must invoke DefaultCmd %q", plugin.Spec.AcpCommand, plugin.Spec.DefaultCmd)
}

// initialize advertises loadSession:true (verified on the wire), so the
// explicit acp-load / acp-sync endpoints may attempt session/load.
func TestCommandCodeBackendPlugin_AdvertisesLoadSession(t *testing.T) {
	plugin := backends.Lookup("commandcode")
	require.NotNil(t, plugin)
	assert.True(t, plugin.Spec.ACPLoadSession, "Command Code advertises loadSession:true")
}

// No CLI factory is registered: chat must always go through the ACP stdio
// transport (`cmdc acp`).
func TestCommandCodeBackendPlugin_IsACPIOnly(t *testing.T) {
	assert.False(t, ai.BackendSupportsCLI("commandcode"), "commandcode is ACP-only")
}

// Command Code reads ~/.commandcode/skills AND ~/.agents/skills at the user
// tier (cli.mjs resolveSkillLoadPlan), so both must be declared or ClawBench
// injects a duplicate of a skill the agent already loads.
func TestCommandCodeBackendPlugin_DeclaresItsNativeSkillDirs(t *testing.T) {
	plugin := backends.Lookup("commandcode")
	require.NotNil(t, plugin)
	assert.True(t, plugin.Spec.AutoLoadsNativeSkills, "Command Code loads its own skills")
	assert.ElementsMatch(t, []string{".commandcode/skills", ".agents/skills"},
		plugin.Spec.NativeSkillsDirs)
}

// Effort is a model-dependent ACP config option (category "thought_level"), so a
// static list would surface a control that is dead for the default model.
func TestCommandCodeBackendPlugin_HasNoStaticThinkingEffort(t *testing.T) {
	plugin := backends.Lookup("commandcode")
	require.NotNil(t, plugin)
	assert.Empty(t, plugin.Spec.ThinkingEffortLevels,
		"effort comes from the ACP config option; a static list would be dead for most models")
}

// --- ACP mapping data ---

func TestCommandCodeBackendPlugin_RegistersTitlePrefixesOnly(t *testing.T) {
	plugin := backends.Lookup("commandcode")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP)

	// toolCallID is "call_function_<hex>_<n>" — no tool name, so a prefix map
	// would never match.
	assert.Nil(t, plugin.ACP.ToolCallIDPrefixes,
		"Command Code toolCallIDs carry no tool name; a prefix map cannot work")
	assert.NotEmpty(t, plugin.ACP.ToolTitlePrefixes)
	assert.NotEmpty(t, plugin.ACP.InputRemaps)

	// Both switch paths are equivalent (handleSetConfigOption routes
	// configId="model" into switchModel), so the default path is correct.
	assert.Empty(t, plugin.ACP.ModelSwitchMethod,
		"config-option and session/set_model are the same function in Command Code")
}

// TestCommandCodeToolTitlesResolveCanonically is the regression guard for the
// 8 tools whose titles do NOT resolve through the shared tables.
//
// Why it matters: those titles are a single token with no ".", "/" or ":", so
// extractToolName's lookupByTitle short-circuits ("single token → return the
// title verbatim") and never reaches the kind → canonical mapping. Without the
// ToolTitlePrefixes entries the raw native name leaks into the UI
// ("enter_plan_mode", "shell_tasks") or lands in the wrong bucket
// ("Updating todos" → DeepThink).
//
// The "already correct" rows are asserted too: they pin the claim that those
// entries are deliberately omitted, so a future change to the shared tables
// that breaks one of them fails here instead of silently degrading the UI.
func TestCommandCodeToolTitlesResolveCanonically(t *testing.T) {
	const backendID = "commandcode"
	const opaqueToolCallID = "call_function_2ft7niegnoyv_1"

	cases := []struct {
		name  string
		title string
		kind  acp.ToolKind
		want  string
	}{
		// --- Covered by ToolTitlePrefixes (would otherwise leak/misbucket) ---
		{"enter_plan_mode", "enter_plan_mode", acp.ToolKindSwitchMode, "EnterPlanMode"},
		{"exit_plan_mode", "exit_plan_mode", acp.ToolKindSwitchMode, "ExitPlanMode"},
		{"plan_review", "plan_review", acp.ToolKindThink, "DeepThink"},
		{"ask_user_question", "ask_user_question", acp.ToolKindThink, "AskUserQuestion"},
		{"monitor_events", "monitor_events", acp.ToolKindExecute, "Bash"},
		{"shell_tasks", "shell_tasks", acp.ToolKindExecute, "Bash"},
		{"bash_output", "bash_output", acp.ToolKindExecute, "Bash"},
		{"todo_write", "Updating todos", acp.ToolKindThink, "TodoWrite"},

		// --- Deliberately NOT in the table: shared tables already resolve ---
		{"shell_command", "Shell: echo clawbench-cc-probe", acp.ToolKindExecute, "Bash"},
		{"read_file", "Read: /tmp/a.go", acp.ToolKindRead, "Read"},
		{"read_multiple_files", "Read: a.go, b.go", acp.ToolKindRead, "Read"},
		{"read_directory", "List: /tmp", acp.ToolKindRead, "LS"},
		{"edit_file", "Edit: /tmp/a.go", acp.ToolKindEdit, "Edit"},
		{"write_file", "Write: /tmp/a.go", acp.ToolKindEdit, "Write"},
		{"glob", "Glob: **/*.go", acp.ToolKindSearch, "Glob"},
		{"web_fetch", "Fetch: https://example.com", acp.ToolKindFetch, "WebFetch"},
		{"monitor_command", "Monitor: sleep 1", acp.ToolKindExecute, "Monitor"},
		{"run_command", "Command: ls", acp.ToolKindExecute, "Bash"},
		{"kill_shell", "Kill: t1", acp.ToolKindExecute, "Bash"},
		{"get_diagnostics", "Diagnostics", acp.ToolKindRead, "Diagnostics"},
		{"task", "Agent: desc", acp.ToolKindOther, "Agent"},

		// --- Known ambiguity: grep and web_search share the "Search" label ---
		// Both must land on Grep. Mapping "Search" → WebSearch would mislabel
		// every grep, and title+kind cannot tell them apart.
		{"grep", "Search: func main", acp.ToolKindSearch, "Grep"},
		{"web_search", "Search: golang release notes", acp.ToolKindSearch, "Grep"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ai.ExtractToolNameForTest(tc.title, tc.kind, backendID, opaqueToolCallID)
			assert.Equal(t, tc.want, got,
				"title %q (kind=%s) resolved to %q, want %q", tc.title, tc.kind, got, tc.want)
		})
	}
}

// TestCommandCodeTitlePrefixesCoverLeakingTools pins the table's contents so a
// removal is caught even if the shared tables change in a way that happens to
// hide the regression in the resolve test above.
func TestCommandCodeTitlePrefixesCoverLeakingTools(t *testing.T) {
	want := map[string]string{
		"enter_plan_mode":   "EnterPlanMode",
		"exit_plan_mode":    "ExitPlanMode",
		"ask_user_question": "AskUserQuestion",
		"monitor_events":    "Bash",
		"shell_tasks":       "Bash",
		"bash_output":       "Bash",
		"Updating todos":    "TodoWrite",
		"plan_review":       "DeepThink",
	}
	assert.Equal(t, want, CommandCodeACPTitlePrefixes)

	// A "Search" entry would be a regression: it would relabel every grep.
	assert.NotContains(t, CommandCodeACPTitlePrefixes, "Search",
		"grep and web_search share the 'Search' label; mapping it would mislabel grep")
}

// TestCommandCodeRemapsDoNotRenameCanonicalFields guards against adding a remap
// that would corrupt Command Code's already-canonical rawInput.
func TestCommandCodeRemapsDoNotRenameCanonicalFields(t *testing.T) {
	for _, canonical := range []string{"file_path", "path", "old_string", "new_string", "command", "pattern"} {
		assert.NotContains(t, CommandCodeACPInputRemaps, canonical,
			"%q is already canonical in Command Code's rawInput; remapping it would break the tool card", canonical)
	}
}
