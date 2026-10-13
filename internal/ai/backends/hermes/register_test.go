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

// Hermes loads its own skills (the hermes-acp toolset enables skills_list /
// skill_view / skill_manage), so ClawBench must not inject a second copy.
func TestHermesBackendPlugin_LoadsNativeSkills(t *testing.T) {
	plugin := backends.Lookup("hermes")
	require.NotNil(t, plugin)
	assert.True(t, plugin.Spec.AutoLoadsNativeSkills)
	assert.Equal(t, []string{".hermes/skills", ".agents/skills"}, plugin.Spec.NativeSkillsDirs)
}

// Hermes advertises no static effort levels and no `thought_level` config
// option, so the spec must stay empty rather than invent levels the UI cannot
// actually apply.
func TestHermesBackendPlugin_NoStaticThinkingEffort(t *testing.T) {
	plugin := backends.Lookup("hermes")
	require.NotNil(t, plugin)
	assert.Empty(t, plugin.Spec.ThinkingEffortLevels)
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
