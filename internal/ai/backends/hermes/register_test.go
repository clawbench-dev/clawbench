package hermes

import (
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Backend plugin registration ---

func TestHermesBackendPlugin_RegisteredInBackends(t *testing.T) {
	plugin := backends.Lookup("hermes")
	require.NotNil(t, plugin, "hermes should be registered in backends registry")
	assert.Equal(t, "hermes", plugin.ID)
}

func TestHermesBackendPlugin_SpecFields(t *testing.T) {
	plugin := backends.Lookup("hermes")
	require.NotNil(t, plugin)

	assert.Equal(t, "hermes", plugin.Spec.ID)
	assert.Equal(t, "hermes", plugin.Spec.Backend)
	assert.Equal(t, "hermes", plugin.Spec.DefaultCmd)
	assert.Equal(t, "Hermes Agent", plugin.Spec.Name)
	assert.Equal(t, "hermes acp", plugin.Spec.AcpCommand)
	assert.Equal(t, "curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash", plugin.Spec.InstallCmd)
	assert.NotZero(t, plugin.Spec.SortOrder)
}

// Hermes' adapter advertises loadSession and implements session/load, so the
// explicit acp-load endpoint must attempt it (unlike dsh, which omits it).
func TestHermesBackendPlugin_AdvertisesLoadSession(t *testing.T) {
	plugin := backends.Lookup("hermes")
	require.NotNil(t, plugin)
	assert.True(t, plugin.Spec.ACPLoadSession, "hermes implements session/load")
}

// No CLI factory is registered: `hermes` has no stream-json CLI mode, so chat
// must always go through the ACP stdio transport.
func TestHermesBackendPlugin_IsACPIOnly(t *testing.T) {
	assert.False(t, ai.BackendSupportsCLI("hermes"), "hermes is ACP-only")
}

// Hermes loads its own skills from ~/.hermes/skills (the hermes-acp toolset
// enables skills_list / skill_view / skill_manage), so ClawBench must not inject
// a second copy. ~/.agents/skills is NOT declared: Hermes only reads it when it
// is listed in skills.external_dirs (default []), so claiming it would make
// dedupe suppress those skills and hide them from Hermes entirely.
func TestHermesBackendPlugin_LoadsNativeSkills(t *testing.T) {
	plugin := backends.Lookup("hermes")
	require.NotNil(t, plugin)
	assert.True(t, plugin.Spec.AutoLoadsNativeSkills)
	assert.Equal(t, []string{".hermes/skills"}, plugin.Spec.NativeSkillsDirs)
	assert.NotContains(t, plugin.Spec.NativeSkillsDirs, ".agents/skills",
		"Hermes does not read ~/.agents/skills by default; declaring it would hide those skills")
}

// Hermes advertises no static effort levels and no `thought_level` config
// option, so the spec must stay empty rather than invent levels the UI cannot
// actually apply.
func TestHermesBackendPlugin_NoStaticThinkingEffort(t *testing.T) {
	plugin := backends.Lookup("hermes")
	require.NotNil(t, plugin)
	assert.Empty(t, plugin.Spec.ThinkingEffortLevels)
}

// Hermes accepts a "model" config option but never acts on it, so it must
// declare the ACP-standard session/set_model; otherwise a model switch is a
// silent no-op (the agent keeps running its previous model).
func TestHermesBackendPlugin_DeclaresSetModelSwitchMethod(t *testing.T) {
	plugin := backends.Lookup("hermes")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.ACP)
	assert.Equal(t, backends.ACPModelSwitchSetModel, plugin.ACP.ModelSwitchMethod)
}

// --- ACP tool mapping ---

// TestHermesToolTitlesResolveCanonically is the point of the ACPPlugin: Hermes
// titles are "<native_name>: <preview>" and its toolCallID carries no tool name,
// so the title-prefix table is the only thing that keeps these tools off the
// generic "Bash" / raw-name fallback. kind values are taken from a live session
// (TOOL_KIND_MAP in acp_adapter/tools.py).
func TestHermesToolTitlesResolveCanonically(t *testing.T) {
	cases := []struct {
		title string
		kind  string
		want  string
	}{
		// kind=other → raw native name without the table.
		{"todo_list: planning 1 task(s)", "other", "TodoWrite"},
		{"memory: Memory manage (memory)", "other", "save_memory"},
		{"session_search: recall: \"foo\"", "other", "Grep"},
		{"process_manage: x", "other", "Bash"},
		{"browser_console: x", "other", "Bash"},
		{"browser_cdp: x", "other", "Bash"},
		{"browser_dialog: x", "other", "Bash"},
		{"browser_exec: x", "other", "Bash"},
		{"browser_vault: x", "other", "Bash"},
		{"manage_connections: x", "other", "Skill"},
		// kind=read/edit → dedicated Skill entries.
		{"skill_view: Loading skill 'x'", "read", "Skill"},
		{"skills_list: x", "read", "Skill"},
		{"skill_manage: x", "edit", "SkillManage"},
		// kind=execute → Agent, not Bash.
		{"delegate_task: x", "execute", "Agent"},
		// kind=fetch → WebSearch, not WebFetch.
		{"web_search: Searching the web for: x", "fetch", "WebSearch"},
	}
	for _, c := range cases {
		got := ai.ExtractToolNameForTest(c.title, acp.ToolKind(c.kind), "hermes")
		assert.Equal(t, c.want, got, "title %q (kind=%s)", c.title, c.kind)
	}
}

// TestHermesAllEnabledToolsResolveCanonically is the guard for the whole
// `hermes-acp` toolset (toolsets.py: _CODING_TOOLS minus clarify — 32 tools).
// Every tool must resolve to a name that is EITHER already canonical
// (kind-mapped, e.g. read_file→Read) OR explicitly in the title-prefix table;
// none may fall through to its raw native name (which renders as a generic
// wrench + snake_case label). The list is the reviewed source of truth, so a
// Hermes version that adds a tool — or a table edit that drops one — fails here
// instead of silently degrading a tool card.
func TestHermesAllEnabledToolsResolveCanonically(t *testing.T) {
	// name → the ACP kind Hermes reports (TOOL_KIND_MAP in acp_adapter/tools.py;
	// tools absent from that map are "other").
	tools := map[string]string{
		"web_search": "fetch", "web_extract": "fetch",
		"terminal": "execute", "process_manage": "other",
		"read_file": "read", "write_file": "edit", "patch": "edit", "search_files": "search",
		"vision_analyze": "read",
		"skills_list":    "read", "skill_view": "read", "skill_manage": "edit",
		"browser_navigate": "fetch", "browser_snapshot": "read", "browser_click": "execute",
		"browser_type": "execute", "browser_scroll": "execute", "browser_back": "execute",
		"browser_press": "execute", "browser_get_images": "read", "browser_vision": "read",
		"browser_console": "other", "browser_cdp": "other", "browser_dialog": "other",
		"browser_vault": "other", "browser_exec": "other",
		"todo_list": "other", "memory": "other", "session_search": "other",
		"execute_code": "execute", "delegate_task": "execute",
		"manage_connections": "other",
	}
	for name, kind := range tools {
		// Hermes titles are "<name>: <preview>"; a bare name also occurs.
		got := ai.ExtractToolNameForTest(name+": preview", acp.ToolKind(kind), "hermes")
		assert.NotEqual(t, name+": preview", got,
			"tool %q (kind=%s) leaked its raw native title; add it to HermesACPTitlePrefixes", name, kind)
		assert.NotContains(t, got, ":",
			"tool %q resolved to a non-canonical name %q", name, got)
	}
}

// TestHermesTitlePrefixDoesNotOvermatch pins the "<name>:" boundary: a title
// that merely starts with a key's characters must not match. Without the colon
// boundary "todo_lists: x" would resolve as "todo_list".
func TestHermesTitlePrefixDoesNotOvermatch(t *testing.T) {
	assert.Equal(t, "todo_lists: x", ai.ExtractToolNameForTest("todo_lists: x", "other", "hermes"),
		"a longer native name must not match the shorter prefix key")
}

// TestHermesToolsKindResolvesWithoutTable documents that the table is a
// correction layer, not the primary resolver: tools whose ACP kind already maps
// correctly keep working with no entry (read_file→Read, terminal→Bash, …).
func TestHermesToolsKindResolvesWithoutTable(t *testing.T) {
	cases := []struct {
		title string
		kind  string
		want  string
	}{
		{"read_file: foo.go", "read", "Read"},
		{"terminal: echo hi", "execute", "Bash"},
		{"search_files: Searching for 'x' (content)", "search", "Grep"},
		{"browser_navigate: x", "fetch", "WebFetch"},
		{"write_file: Preparing write to foo.go.", "edit", "Edit"},
	}
	for _, c := range cases {
		got := ai.ExtractToolNameForTest(c.title, acp.ToolKind(c.kind), "hermes")
		assert.Equal(t, c.want, got, "title %q (kind=%s)", c.title, c.kind)
	}
}
