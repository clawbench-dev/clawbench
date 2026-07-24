package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collectGrokEvents(parser *GrokStreamParser, lines ...string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	for _, line := range lines {
		parser.ParseLine(line, ch)
	}
	close(ch)
	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}
	return events
}

func TestGrokStream_ParseLine_Text(t *testing.T) {
	parser := &GrokStreamParser{}
	events := collectGrokEvents(parser,
		`{"type":"text","data":"Hello"}`,
		`{"type":"text","data":" world"}`,
	)
	require.Len(t, events, 2)
	assert.Equal(t, "content", events[0].Type)
	assert.Equal(t, "Hello", events[0].Content)
	assert.Equal(t, "content", events[1].Type)
	assert.Equal(t, " world", events[1].Content)
}

func TestGrokStream_ParseLine_Thought(t *testing.T) {
	parser := &GrokStreamParser{}
	events := collectGrokEvents(parser,
		`{"type":"thought","data":"Analyzing the directory structure..."}`,
	)
	require.Len(t, events, 1)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "Analyzing the directory structure...", events[0].Content)
}

func TestGrokStream_ParseLine_End_CapturesSessionAndDone(t *testing.T) {
	parser := &GrokStreamParser{}
	events := collectGrokEvents(parser,
		`{"type":"end","stopReason":"EndTurn","sessionId":"sess-abc-123","usage":{"input_tokens":10,"output_tokens":20,"cost_usd":0.01}}`,
	)

	assert.Equal(t, "sess-abc-123", parser.GetCapturedSessionID())

	var hasMeta, hasDone bool
	for _, e := range events {
		switch e.Type {
		case "metadata":
			hasMeta = true
			require.NotNil(t, e.Meta)
			assert.Equal(t, "sess-abc-123", e.Meta.SessionID)
			assert.Equal(t, "EndTurn", e.Meta.StopReason)
			assert.Equal(t, 10, e.Meta.InputTokens)
			assert.Equal(t, 20, e.Meta.OutputTokens)
			assert.InDelta(t, 0.01, e.Meta.CostUSD, 1e-9)
		case "done":
			hasDone = true
		}
	}
	assert.True(t, hasMeta, "expected metadata event")
	assert.True(t, hasDone, "expected done event")
}

func TestGrokStream_ParseLine_Error(t *testing.T) {
	parser := &GrokStreamParser{}
	events := collectGrokEvents(parser,
		`{"type":"error","message":"Couldn't start session: auth required"}`,
	)
	require.Len(t, events, 1)
	assert.Equal(t, "error", events[0].Type)
	assert.Contains(t, events[0].Error, "auth required")
}

func TestGrokStream_ParseLine_UnknownAndInvalid(t *testing.T) {
	parser := &GrokStreamParser{}
	events := collectGrokEvents(parser,
		`not-json`,
		`{"type":"auto_compact_started"}`,
		`{"type":"text","data":""}`, // empty data ignored
	)
	assert.Empty(t, events)
	assert.Empty(t, parser.GetCapturedSessionID())
}

func TestGrokStream_FullFlow(t *testing.T) {
	parser := &GrokStreamParser{}
	events := collectGrokEvents(parser,
		`{"type":"thought","data":"Planning"}`,
		`{"type":"text","data":"Done."}`,
		`{"type":"end","stopReason":"EndTurn","sessionId":"flow-1"}`,
	)

	require.GreaterOrEqual(t, len(events), 3)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "content", events[1].Type)
	assert.Equal(t, "flow-1", parser.GetCapturedSessionID())

	// last event should be done
	assert.Equal(t, "done", events[len(events)-1].Type)
}
