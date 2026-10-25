package ai

import (
	"context"
	"strconv"
	"testing"

	"clawbench/internal/model"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Wrapper tool_call frames (openclaw's "tool_call: id: X, args: {...}")
// ---------------------------------------------------------------------------

// withWrapperPrefixes installs a wrapper-prefix table for the "probe" backend
// through the same function variable the backends package sets, so the real
// lookup path is exercised. Every case here drives the same backend id (the one
// newACPConn is given below); the backend-scoping case covers the nil-conn path.
func withWrapperPrefixes(t *testing.T, prefixes []string) {
	t.Helper()
	prev := LookupACPToolCallWrapperPrefixesFn
	LookupACPToolCallWrapperPrefixesFn = func(id string) []string {
		if id == "probe" {
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
	withWrapperPrefixes(t, []string{"tool_call: id: "})

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
	withWrapperPrefixes(t, []string{"tool_call: id: "})

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
	withWrapperPrefixes(t, []string{"tool_call: id: "})

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
// Wrapper tool_call_UPDATE frames (the same id, no title)
// ---------------------------------------------------------------------------

// wrapperOpeningFrame is the envelope the openclaw bridge sends first: a
// "tool_call: id: <tool>, args: {...}" title under its own toolCallId.
func wrapperOpeningFrame(id string) acp.SessionUpdate {
	return acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: acp.ToolCallId(id),
			Title:      `tool_call: id: web_fetch, args: {"url":"https://example.com"}`,
			Kind:       acp.ToolKindOther,
			RawInput:   map[string]any{"id": "web_fetch"},
		},
	}
}

// collectEvents drains ch and returns every event it saw.
func drainACPTestEvents(ch chan StreamEvent) []StreamEvent {
	var out []StreamEvent
	for {
		select {
		case evt := <-ch:
			out = append(out, evt)
		default:
			return out
		}
	}
}

// TestMapACPSessionUpdate_DropsWrapperToolCallUpdate_ByID pins the id half of
// the wrapper filter. The bridge keeps sending tool_call_update frames under
// the WRAPPER's id, and those frames carry no title — so without an id table the
// update re-materializes the dropped envelope as a card named after the kind
// fallback ("Bash") which never closes (nothing ever completes that id, because
// the real call reports under a different one).
func TestMapACPSessionUpdate_DropsWrapperToolCallUpdate_ByID(t *testing.T) {
	withWrapperPrefixes(t, []string{"tool_call: id: "})

	conn := newACPConn(&model.Agent{ID: "probe-agent", Backend: "probe"}, "sid")
	ch := make(chan StreamEvent, 20)

	// 1. The wrapper opening frame — dropped, id remembered.
	mapACPSessionUpdate(wrapperOpeningFrame("call_wrapper"), ch, context.Background(), conn, nil)
	for _, evt := range drainACPTestEvents(ch) {
		require.NotEqual(t, "tool_use", evt.Type, "the wrapper frame must not become a card")
	}

	// 2. Updates under the same id with NO title (the shape the bridge emits).
	inProgress := acp.ToolCallStatusInProgress
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: "call_wrapper",
			Status:     &inProgress,
		},
	}, ch, context.Background(), conn, nil)

	for _, evt := range drainACPTestEvents(ch) {
		assert.NotEqual(t, "tool_use", evt.Type,
			"a wrapper update must not create a ghost card (got name %q)", toolNameOf(evt))
		assert.NotEqual(t, "tool_result", evt.Type,
			"a wrapper update must not produce a result for a card that was never created")
	}

	// 3. The stall watchdog must still consider the tool in flight: a wrapper
	//    means the real call is imminent.
	assert.True(t, conn.toolInFlight.Load(),
		"the wrapper's in-flight bookkeeping must survive the drop")
}

// TestMapACPSessionUpdate_DropsWrapperToolCallUpdate_ByTitle pins the title
// half: an update that DOES repeat the wrapper title is dropped even when the
// opening frame was never seen (e.g. the connection started mid-turn).
func TestMapACPSessionUpdate_DropsWrapperToolCallUpdate_ByTitle(t *testing.T) {
	withWrapperPrefixes(t, []string{"tool_call: id: "})

	conn := newACPConn(&model.Agent{ID: "probe-agent", Backend: "probe"}, "sid")
	ch := make(chan StreamEvent, 20)

	title := "tool_call: id: web_fetch, args: {}"
	inProgress := acp.ToolCallStatusInProgress
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: "call_never_seen",
			Status:     &inProgress,
			Title:      &title,
		},
	}, ch, context.Background(), conn, nil)

	for _, evt := range drainACPTestEvents(ch) {
		assert.NotEqual(t, "tool_use", evt.Type, "a wrapper update must not become a card")
	}
}

// TestMapACPSessionUpdate_WrapperIDIsReleasedOnTerminalStatus pins the lifecycle:
// the id is remembered for the whole update stream (pending → in_progress →
// completed) and released when that stream terminates, so the table cannot
// accumulate dead ids for the rest of a long session.
func TestMapACPSessionUpdate_WrapperIDIsReleasedOnTerminalStatus(t *testing.T) {
	withWrapperPrefixes(t, []string{"tool_call: id: "})

	conn := newACPConn(&model.Agent{ID: "probe-agent", Backend: "probe"}, "sid")
	ch := make(chan StreamEvent, 20)
	mapACPSessionUpdate(wrapperOpeningFrame("call_wrapper"), ch, context.Background(), conn, nil)
	drainACPTestEvents(ch)

	// Mid-stream updates keep being dropped...
	pending := acp.ToolCallStatusPending
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{ToolCallId: "call_wrapper", Status: &pending},
	}, ch, context.Background(), conn, nil)
	assert.Empty(t, drainACPTestEvents(ch), "a pending wrapper update must be dropped")

	// ...and the terminal one drops too, then releases the id.
	completed := acp.ToolCallStatusCompleted
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{ToolCallId: "call_wrapper", Status: &completed},
	}, ch, context.Background(), conn, nil)
	assert.Empty(t, drainACPTestEvents(ch), "the terminal wrapper update must be dropped too")
	assert.False(t, conn.isDroppedWrapperToolCallID("call_wrapper"),
		"the id must be released once the wrapper's update stream ended")

	// A later frame reusing that id is a genuine call and must render.
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: "call_wrapper",
			Title:      "web_fetch: url: https://example.com",
			Kind:       acp.ToolKindFetch,
			RawInput:   map[string]any{"url": "https://example.com"},
		},
	}, ch, context.Background(), conn, nil)
	var names []string
	for _, evt := range drainACPTestEvents(ch) {
		if evt.Type == "tool_use" {
			names = append(names, toolNameOf(evt))
		}
	}
	assert.Equal(t, []string{"WebFetch"}, names,
		"once released, the id must not suppress a genuine call")
}

// TestMapACPSessionUpdate_WrapperIDTableIsBounded pins the FIFO bound: a session
// that somehow sees an endless stream of wrappers must not grow the table.
func TestMapACPSessionUpdate_WrapperIDTableIsBounded(t *testing.T) {
	withWrapperPrefixes(t, []string{"tool_call: id: "})

	conn := newACPConn(&model.Agent{ID: "probe-agent", Backend: "probe"}, "sid")
	ch := make(chan StreamEvent, 4)
	total := wrapperToolCallIDCapacity + 5
	for i := range total {
		mapACPSessionUpdate(wrapperOpeningFrame("call_wrapper_"+strconv.Itoa(i)), ch, context.Background(), conn, nil)
		drainACPTestEvents(ch)
	}
	conn.stateMu.Lock()
	size := len(conn.wrapperToolCallIDs)
	order := len(conn.wrapperToolCallOrder)
	conn.stateMu.Unlock()

	assert.LessOrEqual(t, size, wrapperToolCallIDCapacity, "the id table must stay bounded")
	assert.Equal(t, size, order, "the eviction order must track the table exactly")
	assert.False(t, conn.isDroppedWrapperToolCallID("call_wrapper_0"),
		"the oldest ids are the ones evicted")
	assert.True(t, conn.isDroppedWrapperToolCallID("call_wrapper_"+strconv.Itoa(total-1)),
		"the newest id must still be remembered")
}

// toolNameOf returns the tool name of a tool_use/tool_result event, "" otherwise.
func toolNameOf(evt StreamEvent) string {
	if evt.Tool == nil {
		return ""
	}
	return evt.Tool.Name
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
