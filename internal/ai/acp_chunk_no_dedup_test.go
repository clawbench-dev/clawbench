package ai

import (
	"context"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// drainContentText concatenates every content/thinking payload the mapper
// emitted, so a test can assert on the assembled text exactly as the executor's
// accumulator would.
func drainContentText(t *testing.T, ch chan StreamEvent) string {
	t.Helper()
	var out string
	for {
		select {
		case ev := <-ch:
			if ev.Type == "content" || ev.Type == "thinking" {
				out += ev.Content
			}
		default:
			return out
		}
	}
}

// codeBuddyChunk builds the _meta a real CodeBuddy agent_message_chunk carries.
// Verified against the wire (2026-10-29 probe): messageId and timestamp are
// both present, and `timestamp` is message-level — every chunk of one assistant
// message shares it.
func codeBuddyChunk(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta: map[string]any{
				metaKeyCodeBuddyMessageID: "01a18599-bc3d-779a-b099-09060857be98",
				"timestamp":               "2026-10-29T03:52:47.908Z",
			},
			Content: acp.TextBlock(text),
		},
	}
}

// TestMapACPSessionUpdate_KeepsRepeatedShortChunks pins that ClawBench performs
// NO client-side duplicate-chunk suppression.
//
// CodeBuddy streams model output character by character. On the wire a table's
// delimiter row arrives as separate frames whose text repeats constantly:
//
//	'|'  ' ---'  ' |'  ' ---'  ' |'  '\n'
//
// A guard keyed on text therefore deletes real content. Production evidence
// (2026-10-29): 16 assistant messages each lost exactly one `|---|---|`, which
// left the delimiter row two cells short of its header; marked refuses such a
// table and renders the whole block as a paragraph.
//
// CodeBuddy de-duplicates inside its own publish path (chunkPublicationKey),
// which also keys on a per-message offset the ACP wire does not carry — so the
// client cannot reproduce it and must not try.
func TestMapACPSessionUpdate_KeepsRepeatedShortChunks(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 256)

	// One five-column table's delimiter row, exactly as the agent emits it.
	frames := []string{"|", " ---", " |", " ---", " |", " ---", " |", " ---", " |", "\n"}
	for _, text := range frames {
		mapACPSessionUpdate(codeBuddyChunk(text), ch, context.Background(), conn, nil)
	}

	assert.Equal(t, strings.Join(frames, ""), drainContentText(t, ch),
		"every frame must reach the client; a text-keyed guard would drop the repeats")
}

// The concrete regression: a message with three GFM tables. Two of them use the
// same two-column delimiter row, and the six-column one contains that row as a
// prefix. All of them must survive intact, or the tables stop rendering.
func TestMapACPSessionUpdate_MultipleTablesKeepTheirDelimiterRows(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 512)

	chunks := []string{
		"| 他的判断 | 核实结果 |\n",
		"|---|---|\n",
		"| 06 下没有 tools | ✅ |\n\n",
		"| 段 | 分镜稿 | 实际段长 | 配音 | 差额 | 来源 |\n",
		"|---|---|---|---|---|---|\n",
		"| 钩子 | 3s | 4.0s | 1.512s | +1.0 | 常量 |\n\n",
		"| 已发布 | 讲了什么 |\n",
		"|---|---|\n",
		"| EP04 | 同一份素材 |\n",
	}
	for _, text := range chunks {
		mapACPSessionUpdate(codeBuddyChunk(text), ch, context.Background(), conn, nil)
	}

	got := drainContentText(t, ch)
	assert.Equal(t, strings.Join(chunks, ""), got,
		"no delimiter row may be dropped")

	// Count whole lines, not substrings: "|---|---|" is also a prefix of
	// "|---|---|---|---|---|---|", so a substring count would over-count.
	var twoCol, sixCol int
	for _, line := range strings.Split(got, "\n") {
		switch line {
		case "|---|---|":
			twoCol++
		case "|---|---|---|---|---|---|":
			sixCol++
		}
	}
	assert.Equal(t, 2, twoCol, "both two-column delimiter rows must survive")
	assert.Equal(t, 1, sixCol, "the six-column delimiter row must survive")
}

// Identical consecutive frames are normal model output (the same word twice, a
// repeated separator), not a retry. Nothing may be dropped.
func TestMapACPSessionUpdate_KeepsIdenticalConsecutiveChunks(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)

	for range 3 {
		mapACPSessionUpdate(codeBuddyChunk("哈"), ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "哈哈哈", drainContentText(t, ch),
		"three identical chunks are three characters of content")
}

// Thinking chunks take the same path: no suppression.
func TestMapACPSessionUpdate_KeepsRepeatedThoughtChunks(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)

	for range 2 {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
				Meta:    map[string]any{metaKeyCodeBuddyMessageID: "m1", "timestamp": "t1"},
				Content: acp.TextBlock("嗯"),
			},
		}, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "嗯嗯", drainContentText(t, ch))
}
