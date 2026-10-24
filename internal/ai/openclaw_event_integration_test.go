package ai

import (
	"context"
	"testing"

	"clawbench/internal/model"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Wrapper tool_call frames (openclaw's "tool_call: id: X, args: {...}")
// ---------------------------------------------------------------------------

// withWrapperPrefixes installs a wrapper-prefix table through the same function
// variable the backends package sets, so the real lookup path is exercised.
func withWrapperPrefixes(t *testing.T, backendID string, prefixes []string) {
	t.Helper()
	prev := LookupACPToolCallWrapperPrefixesFn
	LookupACPToolCallWrapperPrefixesFn = func(id string) []string {
		if id == backendID {
			return prefixes
		}
		return nil
	}
	t.Cleanup(func() { LookupACPToolCallWrapperPrefixesFn = prev })
}

// TestMapACPSessionUpdate_DropsWrapperToolCall pins that an envelope frame
// produces NO tool_use event. Forwarding it draws a card literally named
// "tool_call" next to the real card, and the two frames carry different
// toolCallIds so nothing downstream can collapse them.
func TestMapACPSessionUpdate_DropsWrapperToolCall(t *testing.T) {
	withWrapperPrefixes(t, "probe", []string{"tool_call: id: "})

	conn := newACPConn(&model.Agent{ID: "probe-agent", Backend: "probe"}, "sid")
	ch := make(chan StreamEvent, 20)
	update := acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "call_wrapper",
			Title:      `tool_call: id: web_fetch, args: {"url":"https://example.com"}`,
			Kind:       acp.ToolKindOther,
			RawInput:   map[string]any{"id": "web_fetch", "args": map[string]any{"url": "https://example.com"}},
		},
	}

	mapACPSessionUpdate(update, ch, context.Background(), conn, nil)

	for len(ch) > 0 {
		evt := <-ch
		assert.NotEqual(t, "tool_use", evt.Type,
			"the wrapper frame must not become a tool card (got name %q)",
			func() string {
				if evt.Tool != nil {
					return evt.Tool.Name
				}
				return ""
			}())
	}
}

// TestMapACPSessionUpdate_KeepsRealFrameAfterWrapper pins the other half: the
// frame that follows the wrapper must still render. A filter that dropped both
// would pass the test above but hide every plugin/MCP tool.
func TestMapACPSessionUpdate_KeepsRealFrameAfterWrapper(t *testing.T) {
	withWrapperPrefixes(t, "probe", []string{"tool_call: id: "})

	conn := newACPConn(&model.Agent{ID: "probe-agent", Backend: "probe"}, "sid")
	ch := make(chan StreamEvent, 20)
	update := acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "tool_call:call_wrapper:web_fetch:1",
			Title:      "web_fetch: url: https://example.com",
			Kind:       acp.ToolKindFetch,
			RawInput:   map[string]any{"url": "https://example.com"},
		},
	}

	mapACPSessionUpdate(update, ch, context.Background(), conn, nil)

	var found *ToolCall
	for len(ch) > 0 {
		evt := <-ch
		if evt.Type == "tool_use" && evt.Tool != nil {
			found = evt.Tool
		}
	}
	require.NotNil(t, found, "the real frame must still be rendered")
	assert.Equal(t, "WebFetch", found.Name)
	assert.Equal(t, "tool_call:call_wrapper:web_fetch:1", found.ID)
}

// TestMapACPSessionUpdate_WrapperFilterIsBackendScoped pins that the filter does
// not touch other backends: a title that happens to start with "tool_call: id: "
// on claude must render normally.
func TestMapACPSessionUpdate_WrapperFilterIsBackendScoped(t *testing.T) {
	withWrapperPrefixes(t, "probe", []string{"tool_call: id: "})

	ch := make(chan StreamEvent, 20)
	update := acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "call_other",
			Title:      "tool_call: id: something",
			Kind:       acp.ToolKindOther,
			RawInput:   map[string]any{},
		},
	}

	// backendID comes from conn, and a nil conn yields "" — which must not match.
	mapACPSessionUpdate(update, ch, context.Background(), nil, nil)

	var toolEvents int
	for len(ch) > 0 {
		if evt := <-ch; evt.Type == "tool_use" {
			toolEvents++
		}
	}
	assert.Equal(t, 1, toolEvents,
		"with no backend id the filter must not apply (it is keyed by backend)")
}

// ---------------------------------------------------------------------------
// Modes suppression (openclaw's modes == thought_level)
// ---------------------------------------------------------------------------

// TestApplyExtractedState_SuppressesMirroredModes pins that a backend whose
// modes duplicate its thought_level option ends up with NO mode surface.
// Both would otherwise appear as separate pickers, and the mode one is dead
// (session/set_config_option(configId="mode") is refused).
func TestApplyExtractedState_SuppressesMirroredModes(t *testing.T) {
	prev := LookupACPModesMirrorThoughtLevelFn
	LookupACPModesMirrorThoughtLevelFn = func(id string) bool { return id == "mirrored" }
	t.Cleanup(func() { LookupACPModesMirrorThoughtLevelFn = prev })

	conn := newACPConn(&model.Agent{ID: "m1", Backend: "mirrored"}, "sid")
	ext := sessionStateExtracted{
		modes: []ModeDef{
			{ID: "off", Name: "Off"},
			{ID: "high", Name: "High"},
		},
		modeCurrentID: "high",
		efforts: []ThinkingEffortDef{
			{ID: "off", Name: "Off"},
			{ID: "high", Name: "High"},
		},
		effortCurrentID: "high",
	}

	conn.applyExtractedState(ext)

	assert.Empty(t, conn.GetCurrentSelection("mode"),
		"the mirrored mode selection must be dropped")
	assert.Equal(t, "high", conn.GetCurrentSelection("thought_level"),
		"the thought_level selection must survive — it is the writable one")

	state := GetAgentCapabilityRegistry().GetModeState("m1", "high")
	assert.Nil(t, state, "no modes may be registered for a mirrored-modes backend")
}

// TestApplyExtractedState_KeepsModesForOtherBackends is the control: the
// suppression must not leak to backends that have a real mode surface.
func TestApplyExtractedState_KeepsModesForOtherBackends(t *testing.T) {
	prev := LookupACPModesMirrorThoughtLevelFn
	LookupACPModesMirrorThoughtLevelFn = func(string) bool { return false }
	t.Cleanup(func() { LookupACPModesMirrorThoughtLevelFn = prev })

	conn := newACPConn(&model.Agent{ID: "m2", Backend: "normal"}, "sid")
	conn.applyExtractedState(sessionStateExtracted{
		modes: []ModeDef{
			{ID: "code", Name: "Code"},
			{ID: "ask", Name: "Ask"},
		},
		modeCurrentID: "code",
	})

	assert.Equal(t, "code", conn.GetCurrentSelection("mode"))
	state := GetAgentCapabilityRegistry().GetModeState("m2", "code")
	require.NotNil(t, state, "a normal backend's modes must still be registered")
	assert.Len(t, state.AvailableModes, 2)
}
