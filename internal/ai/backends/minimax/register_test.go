package minimax

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// --- Registration ---

func TestMiniMaxBackendPlugin_Registered(t *testing.T) {
	plugin := backends.Lookup("minimax")
	require.NotNil(t, plugin, "minimax should be registered in the backends registry")
	assert.Equal(t, "minimax", plugin.ID)
}

// The facts the UI and the spawn path read, pinned against the measured
// handshake (docs/dev/minimax_acp_probe_results.md).
func TestMiniMaxBackendPlugin_SpecFields(t *testing.T) {
	spec := backends.Lookup("minimax").Spec

	assert.Equal(t, "minimax", spec.ID)
	assert.Equal(t, "minimax", spec.Backend)
	// The npm package @minimax-ai/code installs the bin `mcode`, not `minimax`.
	assert.Equal(t, "mcode", spec.DefaultCmd)
	assert.Equal(t, "mcode acp", spec.AcpCommand)
	assert.Equal(t, "MiniMax Code", spec.Name)
	assert.Equal(t, "npm install -g @minimax-ai/code", spec.InstallCmd)
	// initialize advertises loadSession:true.
	assert.True(t, spec.ACPLoadSession)
	// session/new without a login / BYOK provider answers -32000 with "Run
	// `mcode login`", so the error can be rendered as that instruction.
	assert.Equal(t, "mcode login", spec.LoginCmd)
	assert.Equal(t, 24, spec.SortOrder)

	// Effort is a per-model ACP config option, not a fixed scale.
	assert.Nil(t, spec.ThinkingEffortLevels)

	// MCP was measured working (injected stdio server called in the first turn,
	// absent without it), so no restriction flags may be set.
	assert.False(t, spec.MCPUnsupported)
	assert.False(t, spec.MCPRejectsSSE)
}

// ACP must carry no mapping data: the measured frames are already canonical.
func TestMiniMaxBackendPlugin_NoToolMapping(t *testing.T) {
	plugin := backends.Lookup("minimax")
	assert.Nil(t, plugin.ACP, "MiniMax tool names/inputs need no backend-specific mapping")
	assert.Nil(t, plugin.Behavior, "no private wire shape was observed")
	assert.Nil(t, plugin.MidTurn, "the private mcode/session/steer extension is not wired")
}

// Measured tool frames (mcode 0.7.9): name and title are the lowercase tool
// name, so the shared alias table must be what resolves them. This is the
// regression guard that keeps a future "minimax looks unresolved" report from
// being answered by adding a redundant backend map.
func TestMiniMaxToolFramesResolveThroughSharedTable(t *testing.T) {
	cases := []struct {
		title     string
		kind      acp.ToolKind
		toolCall  string
		canonical string
	}{
		{"read", acp.ToolKindRead, "call_01a20e048aab7020a5cfb9df", "Read"},
		{"write", acp.ToolKindEdit, "call_01a20e048ae0742c99e47076", "Write"},
		{"bash", acp.ToolKindExecute, "call_01a20e048b4d773abb51345d", "Bash"},
		// Injected MCP tools arrive as mcp__<server>__<tool> with kind "other";
		// they must stay visible as themselves rather than collapsing to a kind.
		{"mcp__probe__clawbench_mcp_probe_ping", acp.ToolKindOther, "call_01a20e0935cd746f9b58d415", "mcp__probe__clawbench_mcp_probe_ping"},
	}
	for _, tc := range cases {
		got := ai.ExtractToolNameForTest(tc.title, tc.kind, "minimax", tc.toolCall)
		assert.Equal(t, tc.canonical, got, "title %q", tc.title)
	}
}

// --- ProviderEnv ---

// An EMPTY spec is the deliberate answer, not an omission: mcode reads its
// provider from its own config file only, so any env row here would offer a
// binding that silently keeps using MiniMax's endpoint.
func TestMiniMaxCannotTakeAProvider(t *testing.T) {
	plugin := backends.Lookup("minimax")
	require.NotNil(t, plugin.ProviderEnv, "an absent entry fails the coverage test; an empty one is the decision")

	assert.Empty(t, plugin.ProviderEnv.Anthropic)
	assert.Empty(t, plugin.ProviderEnv.OpenAI)
	assert.Empty(t, plugin.ProviderEnv.Gemini)
	assert.Empty(t, plugin.ProviderEnv.KeyOnly)
	assert.False(t, plugin.ProviderEnv.ConfigOnly)
	assert.Empty(t, plugin.ProviderEnv.ConfigDirEnv)

	assert.False(t, model.ProviderEnvSupported("minimax", model.ProviderProtocolOpenAI),
		"the UI must not offer an OpenAI-protocol binding")
	assert.False(t, model.ProviderEnvSupported("minimax", model.ProviderProtocolAnthropic),
		"the UI must not offer an Anthropic-protocol binding")
}

// A crash must not leave `mcode acp` running: the cmdline fallback has to name
// this backend's own ACP shape, not another tool that takes `acp` as an arg.
func TestMiniMaxOrphanCmdline(t *testing.T) {
	plugin := backends.Lookup("minimax")
	assert.Equal(t, ai.OrphanCmdlinePattern{Binary: "mcode", Arg: "acp"}, plugin.OrphanCmdline)

	// The pattern must be the one the cleanup matcher actually sees, and it must
	// describe the real argv shape (AcpCommand, NUL-separated by the kernel).
	//
	// The NEGATIVE cases are the real risk here: a bare "acp" substring is wide
	// enough that the wrong pairing would turn startup cleanup into "kill any
	// process that mentions acp". Each one below is a shape that must NOT be
	// killed.
	cases := []struct {
		cmdline []byte
		match   bool
		why     string
	}{
		{[]byte("mcode\x00acp\x00"), true, "the shape we spawn"},
		{[]byte("mcode\x00exec\x00"), false, "`mcode exec` is not an ACP child"},
		{[]byte("othertool\x00acp\x00"), false, "another tool taking an acp argument"},
		{[]byte("mcode\x00--version\x00"), false, "the binary alone is not enough"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.match, ai.OrphanCmdlineMatches(tc.cmdline),
			"cmdline %q: %s", string(tc.cmdline), tc.why)
	}
}

func TestMiniMaxModelSource(t *testing.T) {
	src, ok := model.LookupModelSource("minimax")
	require.True(t, ok, "minimax must register a model source")
	assert.Equal(t, "minimax", src.Backend())
	assert.Equal(t, model.SourceKindStatic, src.Kind())
}

func TestMiniMaxCatalog(t *testing.T) {
	require.NotEmpty(t, MiniMaxCatalog)
	ids := make(map[string]bool, len(MiniMaxCatalog))
	for _, m := range MiniMaxCatalog {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
		assert.False(t, m.Default, "StaticSource marks the default itself")
		ids[m.ID] = true
	}
	assert.True(t, ids["MiniMax-M3.1-Flash"], "M3.1-Flash is the shipped default")
}
