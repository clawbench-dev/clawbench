package qwen

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustPlugin(t *testing.T) *backends.BackendPlugin {
	t.Helper()
	p := backends.Lookup("qwen")
	require.NotNil(t, p, "qwen must be registered by its init()")
	return p
}

// The spawn facts: its own binary, the ACP flag qwen documents in --help, and a
// loadSession support bit that matches the handshake (initialize reports
// loadSession:true, so a session/load is real and must not 501).
func TestQwenSpec_SpawnAndLoadSession(t *testing.T) {
	spec := mustPlugin(t).Spec
	assert.Equal(t, "qwen", spec.ID)
	assert.Equal(t, "qwen", spec.Backend)
	assert.Equal(t, "qwen", spec.DefaultCmd)
	assert.Equal(t, "qwen --acp", spec.AcpCommand)
	assert.True(t, spec.ACPLoadSession, "initialize advertises loadSession:true")
	assert.Equal(t, 21, spec.SortOrder)
}

// A lingering `qwen --acp` after a crash can only be recognized by its command
// line, so the pattern must name both halves.
func TestQwenSpec_OrphanCmdline(t *testing.T) {
	p := mustPlugin(t)
	assert.Equal(t, ai.OrphanCmdlinePattern{Binary: "qwen", Arg: "--acp"}, p.OrphanCmdline)
}

// The only credential path verified over ACP is the OpenAI one: qwen has no
// non-interactive auth besides OPENAI_API_KEY/OPENAI_BASE_URL/OPENAI_MODEL, so an
// Anthropic row would accept a binding qwen then never reads.
func TestQwenProviderEnv_IsOpenAIOnly(t *testing.T) {
	spec := mustPlugin(t).ProviderEnv
	require.NotNil(t, spec)
	require.NotNil(t, spec.OpenAI, "qwen reads OPENAI_*")
	assert.Equal(t, "OPENAI_BASE_URL", spec.OpenAI.BaseURL)
	assert.Equal(t, "OPENAI_API_KEY", spec.OpenAI.APIKey)
	assert.Nil(t, spec.Anthropic)
	assert.Nil(t, spec.Gemini)
	// The model variable is the difference between a binding and every turn
	// failing on "configure a model first": openai auth with only a key and an
	// endpoint has no model. It must be the PROVIDER's model, never a static name
	// (a bound endpoint may not serve qwen3.8-max at all).
	assert.Empty(t, spec.OpenAI.Extra, "no static model may be baked into the environment")
	assert.Equal(t, "OPENAI_MODEL", spec.ModelEnv)

	env, err := withProvider(t, "m2", "")
	require.NoError(t, err)
	assert.Contains(t, env, "OPENAI_MODEL=m2", "the provider's default model must fill the startup variable")
	assert.Contains(t, env, "OPENAI_BASE_URL=http://h/v1")
	assert.Contains(t, env, "OPENAI_API_KEY=k")
	env, err = withProvider(t, "m2", "m1")
	require.NoError(t, err)
	assert.Contains(t, env, "OPENAI_MODEL=m1", "the agent's pinned model wins over the default")

	// A provider that declares no model injects nothing: qwen's own startup
	// error names the missing model, which beats asking for one never served.
	env, err = model.BuildProviderEnv("qwen", &model.Provider{
		Name: "p", Protocol: model.ProviderProtocolOpenAI, APIKey: "k", BaseURL: "http://h/v1",
	}, "")
	require.NoError(t, err)
	for _, e := range env {
		assert.NotContains(t, e, "OPENAI_MODEL=")
	}

	assert.True(t, model.ProviderEnvSupported("qwen", model.ProviderProtocolOpenAI))
	assert.False(t, model.ProviderEnvSupported("qwen", model.ProviderProtocolAnthropic),
		"an anthropic binding would be accepted and then never read")
}

// The tool table must be consulted, not shadowed by the generic path: a frame
// whose _meta names the tool must resolve through it.
func TestQwenToolTable_IsRegistered(t *testing.T) {
	p := mustPlugin(t)
	require.NotNil(t, p.ACP)
	assert.Equal(t, "Read", p.ACP.ToolCallIDPrefixes["read_file"])
	assert.Equal(t, "Bash", p.ACP.ToolCallIDPrefixes["run_shell_command"])
	assert.Nil(t, p.ACP.InputRemaps, "qwen's one observed rawInput is already canonical")
}

// Real frames from the capture (qwen 0.28.4, docs/dev/qwen_acp_probe_results.md).
// The start frame has no name and empty rawInput; the first update carries
// `_meta.toolName` plus the real rawInput. The completed card must end up named
// from _meta, keeping the canonical file_path input.
func TestParseToolCallUpdate_RealFrames(t *testing.T) {
	p := mustPlugin(t)
	require.NotNil(t, p.Behavior)
	require.NotNil(t, p.Behavior.ParseToolCallUpdate, "qwen needs the update hook: the name only arrives there")

	inProgress := mustUpdateForTest(t, `{
		"sessionUpdate": "tool_call_update",
		"toolCallId": "call_1",
		"status": "in_progress",
		"title": "ReadFile: hello.txt",
		"kind": "read",
		"rawInput": {"file_path": "/tmp/probe-qwen/hello.txt"},
		"_meta": {"toolName": "read_file", "provenance": "builtin"}
	}`)
	tool := p.Behavior.ParseToolCallUpdate(inProgress)
	require.NotNil(t, tool)
	assert.Equal(t, "call_1", tool.ID)
	assert.Equal(t, "read_file", tool.Name, "the _meta name must win over the kind-derived \"Read\"")
	assert.False(t, tool.Done)
	assert.Contains(t, tool.Input, "/tmp/probe-qwen/hello.txt")
	assert.Contains(t, tool.Input, "file_path")

	failed := mustUpdateForTest(t, `{
		"sessionUpdate": "tool_call_update",
		"toolCallId": "call_1",
		"status": "failed",
		"content": [{"type": "content", "content": {"type": "text", "text": "params must have required property 'file_path'"}}],
		"_meta": {"toolName": "read_file", "provenance": "builtin"}
	}`)
	tool = p.Behavior.ParseToolCallUpdate(failed)
	require.NotNil(t, tool)
	assert.True(t, tool.Done)
	assert.Equal(t, "read_file", tool.Name)
	assert.NotEmpty(t, tool.Output)
}

// A frame with no _meta.toolName must not blank the name or panic — the generic
// path stays in charge.
func TestParseToolCallUpdate_WithoutMetaNameFallsBack(t *testing.T) {
	p := mustPlugin(t)
	upd := mustUpdateForTest(t, `{
		"sessionUpdate": "tool_call_update",
		"toolCallId": "call_2",
		"status": "in_progress",
		"title": "ReadFile: hello.txt",
		"kind": "read"
	}`)
	tool := p.Behavior.ParseToolCallUpdate(upd)
	require.NotNil(t, tool)
	assert.Equal(t, "Read", tool.Name, "without _meta the kind decides, as for every backend")
}

// The start frame carries `rawInput: {}` and no name; the generic path must
// still produce an identifiable tool (kind=read), and it must not overwrite the
// name that the following update will set.
func TestParseToolCall_StartFrameOfRealCapture(t *testing.T) {
	start := mustToolCallForTest(t, `{
		"sessionUpdate": "tool_call",
		"toolCallId": "call_1",
		"status": "pending",
		"title": "ReadFile",
		"content": [],
		"locations": [],
		"kind": "read",
		"rawInput": {},
		"_meta": {"toolName": "read_file", "provenance": "builtin", "phase": "preparing"}
	}`)
	tool := ai.ParseGenericACPToolCall(start, "qwen")
	require.NotNil(t, tool)
	assert.Equal(t, "call_1", tool.ID)
	assert.NotEmpty(t, tool.Name)
	assert.False(t, tool.Done)
}

func TestQwenCatalog_Shape(t *testing.T) {
	require.NotEmpty(t, QwenCatalog)
	seen := make(map[string]bool, len(QwenCatalog))
	defaults := 0
	for _, m := range QwenCatalog {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
		assert.False(t, seen[m.ID], "duplicate id %q", m.ID)
		seen[m.ID] = true
		if m.Default {
			defaults++
		}
	}
	assert.Equal(t, 1, defaults, "exactly one default")
	assert.True(t, QwenCatalog[0].Default)
	assert.True(t, seen["qwen3.8-max"], "the current flagship must be in the list")
}

// The catalog is published through the plugin's ModelSource, so a model in the
// picker and a spawnable CLI stay one fact.
func TestQwenModelSource_IsThePluginsOwnCatalog(t *testing.T) {
	src, ok := model.LookupModelSource("qwen")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindStatic, src.Kind())
	assert.Equal(t, "qwen", src.Backend())
}

func mustUpdateForTest(t *testing.T, raw string) acp.SessionToolCallUpdate {
	t.Helper()
	var upd acp.SessionToolCallUpdate
	require.NoError(t, json.Unmarshal([]byte(raw), &upd))
	return upd
}

func mustToolCallForTest(t *testing.T, raw string) acp.SessionUpdateToolCall {
	t.Helper()
	var tc acp.SessionUpdateToolCall
	require.NoError(t, json.Unmarshal([]byte(raw), &tc))
	return tc
}

// qwen injects its own skill directories into the model request: a probe turn of
// `qwen --acp` carried 6 of this user's ~/.agents/skills inside <available_skills>
// (location "user"). Declaring them without the auto-load bit would make ClawBench
// inject a second copy of every skill; the bit without the directories would leave
// them unshadowed.
func TestQwenSpec_LoadsItsOwnSkills(t *testing.T) {
	spec := mustPlugin(t).Spec
	assert.True(t, spec.AutoLoadsNativeSkills)
	assert.ElementsMatch(t, []string{".qwen/skills", ".agents/skills"}, spec.NativeSkillsDirs)
}

// withProvider renders qwen's provider environment for a provider whose default
// model is "m2" (models m1, m2), with the given pinned model.
func withProvider(t *testing.T, defaultModel, pin string) ([]string, error) {
	t.Helper()
	env, err := model.BuildProviderEnv("qwen", &model.Provider{
		Name: "p", Protocol: model.ProviderProtocolOpenAI, APIKey: "k", BaseURL: "http://h/v1",
		Models: []string{"m1", "m2"}, DefaultModel: defaultModel,
	}, pin)
	require.NoError(t, err)
	return env, err
}
