package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAI_InterleavedToolCallDeltasCarryTheirIndex pins the fact the engine
// depends on: OpenAI streams parallel calls INTERLEAVED, so every fragment must
// say which call it belongs to. Without the index the consumer can only guess
// "the last call", which splices two argument objects into one invalid blob.
func TestOpenAI_InterleavedToolCallDeltasCarryTheirIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c1","function":{"name":"bash","arguments":"{\"command\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.txt\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"ls\"}"}}]}}]}`,
			`[DONE]`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, err := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "go")}})
	if err != nil {
		t.Fatal(err)
	}

	starts := map[int]string{}
	args := map[int]string{}
	var sawUnindexed int
	for _, ev := range collectEvents(t, ch) {
		if ev.ToolCall == nil {
			continue
		}
		if ev.ToolCall.Index == nil {
			sawUnindexed++
			continue
		}
		switch ev.Kind {
		case KindToolCallStart:
			starts[*ev.ToolCall.Index] = ev.ToolCall.Name
		case KindToolCallDelta:
			args[*ev.ToolCall.Index] += ev.ToolCall.Arguments
		}
	}
	if sawUnindexed > 0 {
		t.Fatalf("%d tool-call events carried no index; interleaved fragments cannot be reassembled without it", sawUnindexed)
	}
	if starts[0] != "read" || starts[1] != "bash" {
		t.Fatalf("starts must be keyed by the provider index, got %+v", starts)
	}
	for idx, want := range map[int]string{0: `{"path":"a.txt"}`, 1: `{"command":"ls"}`} {
		if args[idx] != want {
			t.Fatalf("call %d arguments = %q, want %q", idx, args[idx], want)
		}
		if !json.Valid([]byte(args[idx])) {
			t.Fatalf("call %d arguments are not valid JSON: %q", idx, args[idx])
		}
	}
}

// TestAnthropic_ToolCallDeltasCarryTheOpenBlockIndex pins the same contract for
// the other protocol: an input_json_delta has no id of its own, so it must carry
// the index of the content_block_start (the tool_use block) it belongs to.
func TestAnthropic_ToolCallDeltasCarryTheOpenBlockIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu1","name":"read"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"a.txt\"}"}}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tu2","name":"bash"}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "go")}})

	starts := map[int]string{}
	args := map[int]string{}
	for _, ev := range collectEvents(t, ch) {
		if ev.ToolCall == nil {
			continue
		}
		if ev.ToolCall.Index == nil {
			t.Fatalf("%v event carries no index", ev.Kind)
		}
		switch ev.Kind {
		case KindToolCallStart:
			starts[*ev.ToolCall.Index] = ev.ToolCall.Name
		case KindToolCallDelta:
			args[*ev.ToolCall.Index] += ev.ToolCall.Arguments
		}
	}
	if starts[1] != "read" || starts[2] != "bash" {
		t.Fatalf("starts must carry the content-block index, got %+v", starts)
	}
	if args[1] != `{"path":"a.txt"}` || args[2] != `{"command":"ls"}` {
		t.Fatalf("fragments must be keyed by their block, got %+v", args)
	}
}
