package openclaw

import (
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestOpenClawBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("openclaw")
	require.NotNil(t, plugin, "openclaw should be registered in backends registry")
	assert.Equal(t, "openclaw", plugin.ID)
}

func TestOpenClawBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("openclaw")
	require.NotNil(t, plugin)

	assert.Equal(t, "openclaw", plugin.Spec.ID)
	assert.Equal(t, "openclaw", plugin.Spec.Backend)
	assert.Equal(t, "OpenClaw", plugin.Spec.Name)
	assert.NotEmpty(t, plugin.Spec.Specialty)
	assert.Equal(t, "openclaw acp", plugin.Spec.AcpCommand)
	assert.Equal(t, "npm install -g openclaw", plugin.Spec.InstallCmd)
	assert.Equal(t, 19, plugin.Spec.SortOrder)
	// LoginCmd is what turns the otherwise opaque "initialize timed out" into an
	// actionable instruction: `openclaw acp` is only a bridge, and with the
	// Gateway down the process exits before answering initialize.
	assert.Equal(t, "openclaw gateway start", plugin.Spec.LoginCmd)
}

// The ACP command must spawn a binary that detection also checks, or a user
// passing detection would fail to spawn.
func TestOpenClawBackendPlugin_AcpCommandMatchesDefaultCmd(t *testing.T) {
	plugin := backends.Lookup("openclaw")
	require.NotNil(t, plugin)

	assert.Equal(t, plugin.Spec.DefaultCmd, strings.Fields(plugin.Spec.AcpCommand)[0],
		"AcpCommand must start with DefaultCmd, or detection and spawn disagree")
}

// session/resume is scoped to the bridge process, so recovery MUST go through
// session/load. Without ACPResumeIsProcessLocal the generic recovery path tries
// resume, gets -32603 "Session ... not found" (not -32601), and reports the
// session unrecoverable — stranding the conversation on every restart.
func TestOpenClawBackendPlugin_RecoversViaLoadSession(t *testing.T) {
	plugin := backends.Lookup("openclaw")
	require.NotNil(t, plugin)

	assert.True(t, plugin.Spec.ACPLoadSession,
		"the bridge implements session/load and it restores history")
	assert.True(t, plugin.Spec.ACPResumeIsProcessLocal,
		"session/resume only resolves ids created by the current bridge process")
}

// The bridge refuses per-session mcpServers, so the model layer must filter
// them out. This is a cross-package invariant: a regression here makes every
// openclaw turn fail at session/new.
func TestOpenClawBackendPlugin_MCPIsExcluded(t *testing.T) {
	assert.False(t, model.BackendSupportsMCP("openclaw"),
		"the bridge rejects per-session mcpServers (session/new fails)")
}

func TestOpenClawBackendPlugin_ACPPlugin(t *testing.T) {
	plugin := backends.Lookup("openclaw")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP)

	// modes duplicate the thought_level option value-for-value, and writing a
	// mode is refused. Suppressing the mode surface is what keeps the UI from
	// showing two identical pickers (one of them dead).
	assert.True(t, plugin.ACP.ModesMirrorThoughtLevel,
		"session/new advertises modes == thought_level, and set_config_option(mode) is refused")

	// Wrapper frames are the only way to identify the "tool_call: id: ..."
	// envelope (its toolCallId differs from the real frame's).
	assert.Contains(t, plugin.ACP.ToolCallWrapperTitlePrefixes, "tool_call: id: ",
		"plugin/MCP tools arrive as a wrapper frame followed by the real one")

	// No model switching over ACP: the bridge exposes no model option and
	// session/set_model answers -32601. Declaring a method here would send RPCs
	// that always fail.
	assert.Empty(t, plugin.ACP.ModelSwitchMethod,
		"the bridge has no model-selection surface; do not add a raw-RPC switch")
}

// --- Tool title prefixes ---

// Every entry must actually be reachable: the prefix has to be the exact leading
// token of a title OpenClaw emits, otherwise the map is dead weight and the tool
// leaks its raw name into the UI.
func TestOpenClawTitlePrefixes_MatchObservedTitles(t *testing.T) {
	// Titles + kinds captured by TestOpenClawACPProbe_L4_ToolFrameShapes.
	cases := []struct {
		title string
		kind  acp.ToolKind
		want  string
	}{
		// Resolve through the shared tables.
		{"read: path: /a/b.go", acp.ToolKindRead, "Read"},
		{"exec: command: echo hi", acp.ToolKindExecute, "Bash"},
		{"edit: path: /x, edits: [...]", acp.ToolKindEdit, "Edit"},
		{"write: path: /x, content: probe", acp.ToolKindEdit, "Edit"},
		{"web_fetch: url: https://example.com", acp.ToolKindFetch, "WebFetch"},
		{"web_search: query: OpenClaw ACP", acp.ToolKindSearch, "Grep"},
		// Resolve only through this backend's title prefixes.
		{"ls: path: /tmp/x", acp.ToolKindOther, "LS"},
		{"tool_search: query: memory search", acp.ToolKindSearch, "Grep"},
		{"memory_search: query: openclaw", acp.ToolKindSearch, "Grep"},
		{"apply_patch: input: *** Begin Patch", acp.ToolKindOther, "Edit"},
		{"progress_card: markdown: x", acp.ToolKindOther, "DeepThink"},
		{"sessions_list", acp.ToolKindOther, "DeepThink"},
		{"tool_describe: id: x", acp.ToolKindOther, "DeepThink"},
	}

	for _, c := range cases {
		got := ai.ExtractToolNameForTest(c.title, c.kind, "openclaw")
		assert.Equal(t, c.want, got, "title %q (kind=%s)", c.title, c.kind)
		assert.NotContains(t, got, ":",
			"title %q leaked a colon into the tool name (%q)", c.title, got)
		assert.NotContains(t, got, " ",
			"title %q leaked a phrase into the tool name (%q)", c.title, got)
	}
}

// Titles that already resolve through the shared tables must NOT be overridden —
// a prefix entry for them would be redundant and could mask a shared-table fix.
func TestOpenClawTitlePrefixes_DoNotShadowSharedTables(t *testing.T) {
	for _, key := range []string{"read", "write", "edit", "exec", "web_fetch", "web_search"} {
		_, exists := OpenClawACPTitlePrefixes[key]
		assert.False(t, exists,
			"title prefix %q already resolves via the shared tables; adding it here hides that", key)
	}
}

// --- Wrapper frame filter ---

func TestOpenClawWrapperFramesAreDetected(t *testing.T) {
	// Wired through backends.init() → ai.LookupACPToolCallWrapperPrefixesFn.
	assert.True(t, ai.IsACPToolCallWrapperForTest("tool_call: id: web_fetch, args: {}", "openclaw"),
		"the envelope frame must be dropped")
	assert.False(t, ai.IsACPToolCallWrapperForTest("web_fetch: url: https://example.com", "openclaw"),
		"the real frame must survive")
	assert.False(t, ai.IsACPToolCallWrapperForTest("read: path: /a/b.go", "openclaw"))
	// Other backends must be unaffected.
	assert.False(t, ai.IsACPToolCallWrapperForTest("tool_call: id: web_fetch, args: {}", "claude"),
		"the filter is backend-scoped")
}

// --- Modes suppression ---

func TestOpenClawModesMirrorThoughtLevelIsWired(t *testing.T) {
	assert.True(t, ai.ACPModesMirrorThoughtLevelForTest("openclaw"))
	assert.False(t, ai.ACPModesMirrorThoughtLevelForTest("claude"),
		"only openclaw's modes duplicate its thought_level option")
}

// --- Input normalization ---

// The flat ACP remap table cannot express these: `path` must become `file_path`
// for read/write/edit but must stay `path` for ls, and edit's arguments live in a
// nested edits[] array.
func TestOpenClawInputNormalization(t *testing.T) {
	t.Run("read_path_becomes_file_path", func(t *testing.T) {
		got := ai.NormalizeOpenClawACPInputForTest("read", `{"path":"/a/b.go"}`)
		assert.JSONEq(t, `{"file_path":"/a/b.go"}`, got)
	})

	t.Run("ls_path_stays_path", func(t *testing.T) {
		got := ai.NormalizeOpenClawACPInputForTest("ls", `{"path":"/tmp/x"}`)
		assert.JSONEq(t, `{"path":"/tmp/x"}`, got)
	})

	t.Run("write_path_becomes_file_path", func(t *testing.T) {
		got := ai.NormalizeOpenClawACPInputForTest("write", `{"path":"/x","content":"hi"}`)
		assert.JSONEq(t, `{"file_path":"/x","content":"hi"}`, got)
	})

	t.Run("edit_promotes_first_edit", func(t *testing.T) {
		raw := `{"path":"/x","edits":[{"oldText":"aaa","newText":"bbb"}]}`
		got := ai.NormalizeOpenClawACPInputForTest("edit", raw)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &parsed))
		assert.Equal(t, "/x", parsed["file_path"], "path must be remapped for the edit card")
		assert.Equal(t, "aaa", parsed["old_string"],
			"the diff renderer reads only the flat fields, so edits[0] must be promoted")
		assert.Equal(t, "bbb", parsed["new_string"])
	})

	t.Run("exec_command_untouched", func(t *testing.T) {
		got := ai.NormalizeOpenClawACPInputForTest("exec", `{"command":"echo hi","workdir":"/tmp"}`)
		assert.JSONEq(t, `{"command":"echo hi","workdir":"/tmp"}`, got)
	})

	t.Run("empty_input_is_empty_object", func(t *testing.T) {
		assert.Equal(t, "{}", ai.NormalizeOpenClawACPInputForTest("read", ""))
	})
}

// --- Tool parsing ---

func TestParseOpenClawToolCall(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: "call_abc",
		Title:      "read: path: /a/b.go",
		Kind:       acp.ToolKindRead,
		RawInput:   map[string]any{"path": "/a/b.go"},
	}
	tool := ai.ParseOpenClawToolCallForTest(tc)
	require.NotNil(t, tool)
	assert.Equal(t, "Read", tool.Name)
	assert.Equal(t, "call_abc", tool.ID)
	assert.False(t, tool.Done)
	assert.JSONEq(t, `{"file_path":"/a/b.go"}`, tool.Input)
}

func TestParseOpenClawToolCallUpdate_Completed(t *testing.T) {
	status := acp.ToolCallStatusCompleted
	title := "exec: command: echo hi"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_abc",
		Status:     &status,
		Title:      &title,
		RawInput:   map[string]any{"command": "echo hi"},
	}
	tool := ai.ParseOpenClawToolCallUpdateForTest(tcu)
	require.NotNil(t, tool)
	assert.True(t, tool.Done)
	// mapToolCallName deliberately skips name extraction for a Done update: the
	// name is carried by the opening frame, and a completed frame's title can be
	// a progress phrase rather than the tool. So the update alone has no name —
	// what matters here is that it is marked Done and keeps the id the executor
	// uses to merge it into the opening frame's block.
	assert.Equal(t, "call_abc", tool.ID)
	assert.Equal(t, `{"command":"echo hi"}`, tool.Input)
}

// A Done update must not invent a name from a title that is a progress phrase —
// that is how a completed Bash call would turn into a card named after the
// agent's narration.
func TestParseOpenClawToolCallUpdate_DoesNotRenameOnCompletion(t *testing.T) {
	status := acp.ToolCallStatusCompleted
	title := "Reading the file now"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_abc",
		Status:     &status,
		Title:      &title,
	}
	tool := ai.ParseOpenClawToolCallUpdateForTest(tcu)
	require.NotNil(t, tool)
	assert.Empty(t, tool.Name,
		"a completed update must not derive a tool name from its title")
}
