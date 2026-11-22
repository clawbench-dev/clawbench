package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai/acptest"
	"clawbench/internal/model"
)

// A backend that declares no attribution hook must still group ordinary
// sub-agent content — through the standard parentToolCallId convention every
// plain-ACP agent uses. Losing this fallback would drop every sub-agent's
// output into the main timeline.
func TestFrameAttribution_FallsBackToTheStandardParentLink(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "x", Backend: "plain-acp"}, "s1")

	attr := frameAttribution("plain-acp", conn, map[string]any{
		metaKeyClawBenchParentToolCallID: "call_parent",
	})
	require.NotNil(t, attr)
	assert.Equal(t, "call_parent", attr.ParentToolCallID)
	assert.Equal(t, "", attr.MemberName, "a plain backend has no team members to attribute")
}

// No hook and no link means top-level content: nil, not a zero-valued struct,
// so a branch cannot accidentally render an empty member chip.
func TestFrameAttribution_NoHookNoLinkIsNil(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "x", Backend: "plain-acp"}, "s1")
	assert.Nil(t, frameAttribution("plain-acp", conn, map[string]any{"unrelated": 1}))
	assert.Nil(t, frameAttribution("plain-acp", conn, nil))
}

// The compaction signals are backend hooks. An unregistered backend must not
// latch on a foreign key: a false positive silently re-injects the system
// prompt for the rest of the session.
func TestCompactionMeta_OnlyDeclaredBackendsContribute(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
	}{
		{"codebuddy flag", map[string]any{"codebuddy.ai/isCompactInternal": true}},
		{"codebuddy type", map[string]any{"codebuddy.ai/compactType": "auto"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, isCompactionMeta("plain-acp", tc.meta),
				"a backend that declares no compaction hook must not read another backend's key")
			assert.False(t, isCompactionCancelledMeta("plain-acp", tc.meta))
		})
	}
}

// The steer boundary is the backend's echo of an id THIS host issued. An
// unregistered backend contributes no id, so the shared loop cannot split a
// reply on a foreign frame.
func TestSteerBoundaryID_OnlyDeclaredBackendsContribute(t *testing.T) {
	assert.Equal(t, "", steerBoundaryID("plain-acp", map[string]any{
		"codebuddy.ai/messageId": "abc",
	}))
	assert.Equal(t, "", steerBoundaryID("plain-acp", nil))
}

// Wiring: a declared hook's answer must actually reach the emitted events —
// naming and colouring the member, and folding its content under the Agent
// card. This is the test that fails if the loop keeps calling a helper directly
// instead of going through the hook.
func TestFrameAttribution_HookResultReachesTheEvents(t *testing.T) {
	const backend = "attr-test-backend"
	const member = "probe-member"

	restore := LookupACPBehaviorFn
	LookupACPBehaviorFn = func(id string) *ACPBehavior {
		if id != backend {
			return nil
		}
		return &ACPBehavior{
			AttributeFrame: func(conn *ACPConn, meta map[string]any) *FrameAttribution {
				if meta["member"] == nil {
					return nil
				}
				return &FrameAttribution{
					MemberName:       member,
					MemberColor:      "blue",
					ParentToolCallID: "call_spawn",
				}
			},
		}
	}
	t.Cleanup(func() { LookupACPBehaviorFn = restore })

	ch := make(chan StreamEvent, 16)
	conn := newACPConn(&model.Agent{ID: "a", Backend: backend}, "s1")

	// A member text chunk: the content event must carry all three fields.
	frame := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta:    acptest.Meta(map[string]any{"member": member}),
			Content: acp.TextBlock("hello"),
		},
	}
	mapACPSessionUpdate(frame, ch, nil, conn, nil)

	var content *StreamEvent
	for len(ch) > 0 {
		ev := <-ch
		if ev.Type == "content" {
			content = &ev
		}
	}
	require.NotNil(t, content, "the member's text must still be forwarded")
	assert.Equal(t, member, content.MemberName)
	assert.Equal(t, "blue", content.MemberColor)
	assert.Equal(t, "call_spawn", content.ParentToolCallID)
}

// Every content/thinking/tool branch must ask the hook, not just the text one.
// A branch left calling a removed helper would silently stop attributing — the
// member's output would render as top-level content in the main timeline, with
// no error anywhere.
//
// The branches walked here are the four that CARRY attribution: agent_message,
// agent_thought, tool_call and tool_call_update. user_message_chunk is
// deliberately absent — that branch reads a steer-boundary id through its own
// hook (SteerBoundaryID) and forwards no content of its own, so it has no
// member to attribute.
func TestFrameAttribution_EveryBranchAsksTheHook(t *testing.T) {
	const backend = "attr-branch-backend"
	restore := LookupACPBehaviorFn
	LookupACPBehaviorFn = func(id string) *ACPBehavior {
		if id != backend {
			return nil
		}
		return &ACPBehavior{
			AttributeFrame: func(_ *ACPConn, meta map[string]any) *FrameAttribution {
				if meta["m"] == nil {
					return nil
				}
				return &FrameAttribution{MemberName: "mem", MemberColor: "teal", ParentToolCallID: "call_x"}
			},
		}
	}
	t.Cleanup(func() { LookupACPBehaviorFn = restore })

	conn := newACPConn(&model.Agent{ID: "a", Backend: backend}, "s1")
	meta := acptest.Meta(map[string]any{"m": true})

	drain := func(ch chan StreamEvent, want string) StreamEvent {
		t.Helper()
		for len(ch) > 0 {
			if ev := <-ch; ev.Type == want {
				return ev
			}
		}
		t.Fatalf("no %s event was forwarded", want)
		return StreamEvent{}
	}
	assertAttributed := func(ev StreamEvent, branch string) {
		t.Helper()
		assert.Equal(t, "mem", ev.MemberName, "%s: member name must reach the event", branch)
		assert.Equal(t, "teal", ev.MemberColor, "%s: member colour must reach the event", branch)
	}

	// text
	ch := make(chan StreamEvent, 32)
	mapACPSessionUpdate(acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
		Meta: meta, Content: acp.TextBlock("hi"),
	}}, ch, nil, conn, nil)
	assertAttributed(drain(ch, "content"), "agent_message_chunk")

	// thinking
	ch = make(chan StreamEvent, 32)
	mapACPSessionUpdate(acp.SessionUpdate{AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
		Meta: meta, Content: acp.TextBlock("hmm"),
	}}, ch, nil, conn, nil)
	assertAttributed(drain(ch, "thinking"), "agent_thought_chunk")

	// tool call start
	ch = make(chan StreamEvent, 32)
	mapACPSessionUpdate(acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{
		Meta: meta, ToolCallId: "tc-1", Title: "Read",
	}}, ch, nil, conn, nil)
	tool := drain(ch, "tool_use")
	assertAttributed(tool, "tool_call")
	require.NotNil(t, tool.Tool)
	assert.Equal(t, "call_x", tool.Tool.ParentToolCallID,
		"the tool call must be folded under the Agent card, not the main timeline")

	// tool call update (the RESULT frame). This branch is the easiest one to
	// break silently: it flows through mapACPToolCallUpdate, and losing the
	// attribution there leaves the result card without its parent link — it
	// pops out of the Agent card and lands in the main timeline as if the
	// assistant had said something unrelated. Nothing errors.
	ch = make(chan StreamEvent, 32)
	mapACPSessionUpdate(acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
		Meta:       acp.PatchSet(meta),
		ToolCallId: "tc-1",
		Status:     acptest.Patch(acp.ToolCallStatusCompleted),
	}}, ch, nil, conn, nil)
	result := drain(ch, "tool_result")
	assertAttributed(result, "tool_call_update")
	require.NotNil(t, result.Tool)
	assert.Equal(t, "call_x", result.Tool.ParentToolCallID,
		"the tool result must inherit the member's parent link, or it leaves the Agent card")
}
