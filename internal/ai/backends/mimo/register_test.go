package mimo

import (
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/ai/backends/opencode"
	"clawbench/internal/model"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MiMo-Code is an OpenCode fork, so the plugin must register OpenCode's ACP
// behavior rather than the generic one: without it the checklist bridge and the
// tool-call remaps are missing and every MiMo session shows raw tool names and
// an empty plan panel.
func TestMimoPlugin_RegistersOpenCodeACPBehavior(t *testing.T) {
	plugin := backends.Lookup("mimo")
	require.NotNil(t, plugin, "mimo must be registered in the backends registry")
	require.NotNil(t, plugin.Behavior, "mimo needs the OpenCode behavior hooks")

	// The tool-call mappers are the shared ones, not per-backend copies.
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("call_1"),
		Title:      "read",
		Kind:       acp.ToolKindRead,
		RawInput:   map[string]any{"filePath": "/tmp/a.txt"},
	}
	got := plugin.Behavior.ParseToolCall(tc)
	require.NotNil(t, got)
	want := opencode.ParseACPToolCall(tc)
	require.NotNil(t, want)
	assert.Equal(t, want.Name, got.Name)
	assert.Equal(t, want.Input, got.Input)

	// The update hook exists too, so StreamUpdate frames go through the fork's
	// own mapping.
	require.NotNil(t, plugin.Behavior.ParseToolCallUpdate)
	st := acp.ToolCallStatusCompleted
	title := "N todos"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_1"),
		Title:      &title,
		Status:     &st,
		RawOutput: map[string]any{
			"metadata": map[string]any{
				"todos": []any{
					map[string]any{"content": "step 1", "priority": "high", "status": "completed"},
				},
			},
		},
	}
	upd := plugin.Behavior.ParseToolCallUpdate(tcu)
	require.NotNil(t, upd)
	assert.Equal(t, "call_1", upd.ID)
}

// The OnToolCallUpdate hook is the todowrite → plan bridge. It must forward a
// plan_update for a terminal todo frame — that is what fills the checklist panel
// for MiMo (which, like OpenCode, emits no plan notifications).
func TestMimoPlugin_OnToolCallUpdateBridgesThePlan(t *testing.T) {
	plugin := backends.Lookup("mimo")
	require.NotNil(t, plugin)
	require.NotNil(t, plugin.Behavior)
	require.NotNil(t, plugin.Behavior.OnToolCallUpdate)

	conn := ai.NewACPConnForTest(&model.Agent{ID: "a1", Backend: "mimo"}, "session-mimo-plan")
	ch := make(chan ai.StreamEvent, 8)
	st := acp.ToolCallStatusCompleted
	plugin.Behavior.OnToolCallUpdate(conn, ch, acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_todo"),
		Status:     &st,
		RawOutput: map[string]any{
			"metadata": map[string]any{
				"todos": []any{
					map[string]any{"content": "write the report", "priority": "high", "status": "in_progress"},
				},
			},
		},
	})

	select {
	case event := <-ch:
		assert.Equal(t, "plan_update", event.Type)
		require.NotNil(t, event.Plan)
		require.Len(t, event.Plan.Entries, 1)
		assert.Equal(t, "write the report", event.Plan.Entries[0].Content)
		assert.Equal(t, "in_progress", event.Plan.Entries[0].Status)
		assert.Equal(t, "high", event.Plan.Entries[0].Priority)
	default:
		t.Fatal("a terminal todo frame must emit a plan_update")
	}

	cached := conn.GetCachedPlanState()
	require.NotNil(t, cached, "the plan is cached so a client that connects later sees it")
	require.Len(t, cached.Entries, 1)

	// A non-terminal frame is not bridged: it carries the requested list, and
	// emitting it twice would render the checklist twice per invocation.
	inProgress := acp.ToolCallStatusInProgress
	plugin.Behavior.OnToolCallUpdate(conn, ch, acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_todo"),
		Status:     &inProgress,
		RawInput: map[string]any{
			"todos": []any{map[string]any{"content": "write the report", "status": "pending"}},
		},
	})
	select {
	case event := <-ch:
		t.Fatalf("in_progress frame must not emit, got %q", event.Type)
	default:
	}
}

// MiMo is an OpenCode fork that loads its own skills, so ClawBench must not
// inject them a second time; and its model catalog is served statically.
func TestMimoPlugin_SpecReachesTheSharedRegistries(t *testing.T) {
	plugin := backends.Lookup("mimo")
	require.NotNil(t, plugin)

	assert.True(t, plugin.Spec.AutoLoadsNativeSkills,
		"mimo reads .mimocode/skills itself, so injecting them again duplicates every skill")
	assert.Contains(t, plugin.Spec.NativeSkillsDirs, ".mimocode/skills")
	assert.Equal(t, "mimo acp", plugin.Spec.AcpCommand)

	// The wildcard catch-all is not a real source: a spec is discoverable only
	// because the plugin registered one.
	require.NotNil(t, plugin.ModelSource)
	assert.True(t, model.CanDiscoverModels(plugin.Spec))

	// The provider env is what makes "use my own endpoint" work for this fork.
	// The assertion is on the declaration itself rather than on
	// ProviderEnvSupported, which is keyed by backend id and therefore reads
	// whatever another backend package in the same binary registered.
	require.NotNil(t, plugin.ProviderEnv)
	// Both protocol slots are declared; a missing one means an agent bound to
	// that protocol gets no environment at all.
	assert.Equal(t, "ANTHROPIC_BASE_URL", plugin.ProviderEnv.Anthropic.BaseURL)
	assert.Equal(t, "ANTHROPIC_API_KEY", plugin.ProviderEnv.Anthropic.APIKey)
	assert.Equal(t, "ANTHROPIC_AUTH_TOKEN", plugin.ProviderEnv.Anthropic.AuthToken,
		"bearer-mode providers are the norm for this fork, so the token variable must be declared")
	assert.Equal(t, "OPENAI_BASE_URL", plugin.ProviderEnv.OpenAI.BaseURL)
	assert.Equal(t, "OPENAI_API_KEY", plugin.ProviderEnv.OpenAI.APIKey)
}
