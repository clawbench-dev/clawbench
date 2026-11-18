package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests pin the failure and edge paths of the two protocol clients. They
// are separate from client_test.go, which pins the shapes the happy path sends:
// what is tested here is what happens when the endpoint misbehaves — a refused
// connection, a truncated body, a frame that does not parse, a gateway that
// sends no terminator — because those are the states a user reports as "the
// agent said nothing".

// sseServer serves frames as an SSE body and closes when they run out.
func sseServer(t *testing.T, frames []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// lastEvent returns the terminal event of a drained stream.
func lastEvent(t *testing.T, events []Event) Event {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("the stream produced no events at all")
	}
	return events[len(events)-1]
}

// TestOpenAI_ConnectionRefusedIsAnError pins that a dead endpoint returns the
// error from Stream instead of a channel that never produces anything.
func TestOpenAI_ConnectionRefusedIsAnError(t *testing.T) {
	c, err := New(Config{Protocol: ProtocolOpenAI, BaseURL: "http://127.0.0.1:1", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err == nil {
		t.Fatal("a refused connection must be an error")
	}
	if !strings.Contains(err.Error(), "openai: request") {
		t.Fatalf("the error must name the protocol and stage, got %v", err)
	}
}

// TestAnthropic_ConnectionRefusedIsAnError is the other half.
func TestAnthropic_ConnectionRefusedIsAnError(t *testing.T) {
	c, err := New(Config{Protocol: ProtocolAnthropic, BaseURL: "http://127.0.0.1:1", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err == nil {
		t.Fatal("a refused connection must be an error")
	}
	if !strings.Contains(err.Error(), "anthropic: request") {
		t.Fatalf("the error must name the protocol and stage, got %v", err)
	}
}

// TestOpenAI_AuthModes pins which header the key rides. A gateway that only
// accepts one of the two is the reason the setting exists, so sending the wrong
// one is a 401 the user cannot diagnose.
func TestOpenAI_AuthModes(t *testing.T) {
	cases := []struct {
		mode      string
		wantAPI   bool
		wantAuthz bool
	}{
		{AuthAPIKey, true, false},
		{"", false, true},
		{AuthBearer, false, true},
		{AuthAuto, false, true},
	}
	for _, tc := range cases {
		t.Run("openai/"+tc.mode, func(t *testing.T) {
			var hdr http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hdr = r.Header.Clone()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()

			c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, APIKey: "secret", AuthMode: tc.mode, Model: "m"})
			ch, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
			if err != nil {
				t.Fatal(err)
			}
			collectEvents(t, ch)

			if got := hdr.Get("x-api-key") != ""; got != tc.wantAPI {
				t.Fatalf("x-api-key present = %v, want %v", got, tc.wantAPI)
			}
			if got := hdr.Get("Authorization") != ""; got != tc.wantAuthz {
				t.Fatalf("Authorization present = %v, want %v", got, tc.wantAuthz)
			}
		})
	}
}

// TestAnthropic_AuthModes pins the protocol's own default (x-api-key) and that an
// explicit bearer is honored, which is the whole point of the override.
func TestAnthropic_AuthModes(t *testing.T) {
	cases := []struct {
		mode    string
		wantAPI bool
	}{
		{"", true},
		{AuthAuto, true},
		{AuthAPIKey, true},
		{AuthBearer, false},
	}
	for _, tc := range cases {
		t.Run("anthropic/"+tc.mode, func(t *testing.T) {
			var hdr http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hdr = r.Header.Clone()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
			}))
			defer srv.Close()

			c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, APIKey: "secret", AuthMode: tc.mode, Model: "m"})
			ch, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
			if err != nil {
				t.Fatal(err)
			}
			collectEvents(t, ch)

			if got := hdr.Get("x-api-key") != ""; got != tc.wantAPI {
				t.Fatalf("x-api-key present = %v, want %v", got, tc.wantAPI)
			}
			if !tc.wantAPI && !strings.HasPrefix(hdr.Get("Authorization"), "Bearer ") {
				t.Fatalf("the bearer mode must send an Authorization header, got %q", hdr.Get("Authorization"))
			}
			if hdr.Get("anthropic-version") == "" {
				t.Fatal("the anthropic-version header is required and was not sent")
			}
		})
	}
}

// TestOpenAI_NoAPIKeySendsNoAuthHeader pins that an unauthenticated local
// gateway still works: a blank key must not produce "Bearer ".
func TestOpenAI_NoAPIKeySendsNoAuthHeader(t *testing.T) {
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ch)
	if hdr.Get("Authorization") != "" || hdr.Get("x-api-key") != "" {
		t.Fatalf("no key configured, so no auth header may be sent, got %v", hdr)
	}
}

// TestAnthropic_HTTPErrorIsTyped pins that the Anthropic path reports the status
// the same way the OpenAI path does.
func TestAnthropic_HTTPErrorIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad"}}`)
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	_, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusBadRequest {
		t.Fatalf("expected a typed 400, got %v", err)
	}
	if !strings.Contains(he.Error(), "bad") {
		t.Fatalf("the body must be part of the message, got %q", he.Error())
	}
}

// TestHTTPError_ErrorOmitsEmptyBody pins the two message shapes: a bodyless
// failure must not end in a dangling colon.
func TestHTTPError_ErrorOmitsEmptyBody(t *testing.T) {
	if got := (&HTTPError{Status: 502}).Error(); got != "endpoint returned status 502" {
		t.Fatalf("got %q", got)
	}
	if got := (&HTTPError{Status: 502, Body: "upstream down"}).Error(); got != "endpoint returned status 502: upstream down" {
		t.Fatalf("got %q", got)
	}
}

// TestOpenAI_SkipsFramesItCannotUse pins the two skips that keep a gateway's
// keepalives from ending a turn: comments, blank lines, non-data lines and
// frames whose JSON does not parse are all ignored, and the answer still arrives.
func TestOpenAI_SkipsFramesItCannotUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": keepalive\n\n")
		_, _ = io.WriteString(w, "event: ping\n\n")
		_, _ = io.WriteString(w, "data: {not json\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"still here\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	events := collectEvents(t, ch)

	var text string
	for _, ev := range events {
		if ev.Kind == KindText {
			text += ev.Text
		}
	}
	if text != "still here" {
		t.Fatalf("unusable frames must be skipped, got %q", text)
	}
	if got := lastEvent(t, events).Kind; got != KindDone {
		t.Fatalf("the stream must still terminate cleanly, got %v", got)
	}
}

// TestAnthropic_SkipsFramesItCannotUse is the same contract for the other
// protocol.
func TestAnthropic_SkipsFramesItCannotUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": keepalive\n\n")
		_, _ = io.WriteString(w, "data: {not json\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"still here\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	events := collectEvents(t, ch)

	var text string
	for _, ev := range events {
		if ev.Kind == KindText {
			text += ev.Text
		}
	}
	if text != "still here" {
		t.Fatalf("unusable frames must be skipped, got %q", text)
	}
	if got := lastEvent(t, events).Kind; got != KindDone {
		t.Fatalf("the stream must still terminate cleanly, got %v", got)
	}
}

// TestOpenAI_InStreamErrorIsTerminal pins that a chunk carrying an `error`
// object ends the turn with the provider's message, before any [DONE].
func TestOpenAI_InStreamErrorIsTerminal(t *testing.T) {
	srv := sseServer(t, []string{
		`{"choices":[{"delta":{"content":"partial"}}]}`,
		`{"error":{"message":"rate limited","type":"rate_limit"}}`,
		`{"choices":[{"delta":{"content":"never"}}]}`,
	})
	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	events := collectEvents(t, ch)

	last := lastEvent(t, events)
	if last.Kind != KindError || !strings.Contains(last.Err.Error(), "rate limited") {
		t.Fatalf("expected a terminal error carrying the message, got %v / %v", last.Kind, last.Err)
	}
	if !strings.Contains(last.Err.Error(), "openai:") {
		t.Fatalf("the error must name the protocol, got %v", last.Err)
	}
	for _, ev := range events {
		if ev.Kind == KindText && ev.Text == "never" {
			t.Fatal("nothing after the error frame may be emitted")
		}
	}
}

// TestAnthropic_ErrorWithoutMessage pins the fallback text: an error frame with
// no message still has to produce a readable failure, not an empty one.
func TestAnthropic_ErrorWithoutMessage(t *testing.T) {
	srv := sseServer(t, []string{`{"type":"error","error":{}}`})
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	last := lastEvent(t, collectEvents(t, ch))
	if last.Kind != KindError || !strings.Contains(last.Err.Error(), "stream error") {
		t.Fatalf("expected the fallback text, got %v / %v", last.Kind, last.Err)
	}
}

// TestAnthropic_BodyWithoutTerminatorIsSuccess pins the gateway quirk this client
// tolerates: some send no message_stop and just close the connection. Anything
// that arrived is a complete-enough answer.
func TestAnthropic_BodyWithoutTerminatorIsSuccess(t *testing.T) {
	srv := sseServer(t, []string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"cut"}}`,
	})
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	events := collectEvents(t, ch)
	if got := lastEvent(t, events).Kind; got != KindDone {
		t.Fatalf("a body that ended after content must terminate as done, got %v", got)
	}
}

// TestBothProtocols_EmptyStreamIsErrNoOutput pins that a body with nothing in it
// is a distinct failure on both protocols.
func TestBothProtocols_EmptyStreamIsErrNoOutput(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		srv := sseServer(t, nil)
		c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
		ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
		last := lastEvent(t, collectEvents(t, ch))
		if last.Kind != KindError || !errors.Is(last.Err, ErrNoOutput) {
			t.Fatalf("got %v / %v", last.Kind, last.Err)
		}
	})
	t.Run("anthropic", func(t *testing.T) {
		srv := sseServer(t, nil)
		c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
		ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
		last := lastEvent(t, collectEvents(t, ch))
		if last.Kind != KindError || !errors.Is(last.Err, ErrNoOutput) {
			t.Fatalf("got %v / %v", last.Kind, last.Err)
		}
	})
}

// TestAnthropic_UsageArrivesOnMessageDelta pins the split accounting: input
// tokens come from message_start and output tokens from message_delta.
func TestAnthropic_UsageArrivesOnMessageDelta(t *testing.T) {
	srv := sseServer(t, []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":42}}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`,
		`{"type":"message_delta","usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	})
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})

	var usage *Usage
	for _, ev := range collectEvents(t, ch) {
		if ev.Kind == KindUsage {
			usage = ev.Usage
		}
	}
	if usage == nil || usage.InputTokens != 42 || usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v, want 42/7", usage)
	}
}

// TestAnthropic_SkipsEmptyDeltas pins that a delta event carrying nothing emits
// no content event (an empty text event would render an empty bubble), and that
// a stream made only of those is reported as no output rather than as an answer.
func TestAnthropic_SkipsEmptyDeltas(t *testing.T) {
	srv := sseServer(t, []string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":""}}`,
		`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":""}}`,
		`{"type":"content_block_delta","delta":{"type":"signature_delta","signature":""}}`,
		`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":""}}`,
		`{"type":"content_block_delta"}`,
		`{"type":"message_stop"}`,
	})
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	events := collectEvents(t, ch)
	for _, ev := range events[:len(events)-1] {
		t.Fatalf("no content event may be emitted for empty deltas, got %v", ev.Kind)
	}
	last := lastEvent(t, events)
	if last.Kind != KindError || !errors.Is(last.Err, ErrNoOutput) {
		t.Fatalf("a stream of empty deltas must be ErrNoOutput, got %v / %v", last.Kind, last.Err)
	}
}

// TestAnthropic_InputJSONDeltaWithoutAnOpenBlockIsDropped pins the gateway quirk
// guard: a fragment with no tool_use block to belong to must not be emitted,
// because the consumer would append it to an unrelated call.
func TestAnthropic_InputJSONDeltaWithoutAnOpenBlockIsDropped(t *testing.T) {
	srv := sseServer(t, []string{
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"done"}}`,
		`{"type":"message_stop"}`,
	})
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	for _, ev := range collectEvents(t, ch) {
		if ev.Kind == KindToolCallDelta {
			t.Fatalf("an orphan fragment must be dropped, got %+v", ev.ToolCall)
		}
	}
}

// TestAnthropic_NonToolBlockStartClosesTheOpenCall pins that a text block opening
// after a tool_use closes the argument window: a later fragment must not be
// attributed to the earlier call.
func TestAnthropic_NonToolBlockStartClosesTheOpenCall(t *testing.T) {
	srv := sseServer(t, []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu1","name":"read"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"STRAY"}}`,
		`{"type":"message_stop"}`,
	})
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})

	var args []string
	for _, ev := range collectEvents(t, ch) {
		if ev.Kind == KindToolCallDelta {
			args = append(args, ev.ToolCall.Arguments)
		}
	}
	if len(args) != 1 || args[0] != `{"path":` {
		t.Fatalf("only the fragment inside the tool_use block may be forwarded, got %q", args)
	}
}

// TestAnthropic_BlockStartWithoutContentBlock pins the defensive branch: a
// content_block_start with no block object must not panic and must not announce
// a call.
func TestAnthropic_BlockStartWithoutContentBlock(t *testing.T) {
	srv := sseServer(t, []string{
		`{"type":"content_block_start","index":0}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`{"type":"message_stop"}`,
	})
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	for _, ev := range collectEvents(t, ch) {
		if ev.Kind == KindToolCallStart {
			t.Fatal("a non-tool block start must not announce a call")
		}
	}
}

// TestOpenAI_ToolCallWithoutIndexDefaultsToSlotZero pins the gateway quirk: a
// single call may omit index, and the consumer still needs one to key on.
func TestOpenAI_ToolCallWithoutIndexDefaultsToSlotZero(t *testing.T) {
	srv := sseServer(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"id":"c0","function":{"name":"read","arguments":"{}"}}]}}]}`,
		`[DONE]`,
	})
	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	for _, ev := range collectEvents(t, ch) {
		if ev.ToolCall != nil && (ev.ToolCall.Index == nil || *ev.ToolCall.Index != 0) {
			t.Fatalf("a missing index must default to slot 0, got %+v", ev.ToolCall)
		}
	}
}

// TestOpenAI_ToolCallStartIsAnnouncedOnce pins that argument fragments do not
// re-open the card for every chunk.
func TestOpenAI_ToolCallStartIsAnnouncedOnce(t *testing.T) {
	srv := sseServer(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c0","function":{"name":"read","arguments":"{\"a\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}`,
		`[DONE]`,
	})
	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	starts := 0
	for _, ev := range collectEvents(t, ch) {
		if ev.Kind == KindToolCallStart {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("the call card must open exactly once, got %d starts", starts)
	}
}

// TestOpenAI_ReasoningFieldAlternatives pins the OpenRouter spelling of the
// reasoning stream, which the DeepSeek spelling would otherwise shadow.
func TestOpenAI_ReasoningFieldAlternatives(t *testing.T) {
	if evs := reasoningEvents(oaDelta{Reasoning: "r"}); len(evs) != 1 || evs[0].Reasoning != "r" {
		t.Fatalf("the `reasoning` field must be read, got %+v", evs)
	}
	if evs := reasoningEvents(oaDelta{ReasoningContent: "a", Reasoning: "b"}); len(evs) != 1 || evs[0].Reasoning != "a" {
		t.Fatalf("reasoning_content must win when both are present, got %+v", evs)
	}
	if evs := reasoningEvents(oaDelta{}); evs != nil {
		t.Fatalf("no reasoning means no event, got %+v", evs)
	}
}

// TestOpenAI_ReasoningOnlyStreamIsNoOutput pins what the empty-stream check
// currently does with a reply that produced ONLY reasoning: sawContent tracks
// answer text, so a reasoning-only stream is reported as ErrNoOutput even though
// a thinking block was streamed.
func TestOpenAI_ReasoningOnlyStreamIsNoOutput(t *testing.T) {
	srv := sseServer(t, []string{
		`{"choices":[{"delta":{"reasoning_content":"pondering"}}]}`,
	})
	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	events := collectEvents(t, ch)
	if events[0].Kind != KindReasoning {
		t.Fatalf("the reasoning must still be streamed, got %v", events[0].Kind)
	}
	last := lastEvent(t, events)
	if last.Kind != KindError || !errors.Is(last.Err, ErrNoOutput) {
		t.Fatalf("a reasoning-only reply is currently ErrNoOutput, got %v / %v", last.Kind, last.Err)
	}
}

// TestOpenAI_UsageOnlyStreamIsNotNoOutput pins that a gateway reporting usage and
// nothing else is treated as a (poor) success rather than ErrNoOutput — the
// distinction drives different caller messages.
func TestOpenAI_UsageOnlyStreamIsNotNoOutput(t *testing.T) {
	srv := sseServer(t, []string{`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":0}}`})
	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	last := lastEvent(t, collectEvents(t, ch))
	if last.Kind != KindDone {
		t.Fatalf("a usage-bearing stream must terminate as done, got %v / %v", last.Kind, last.Err)
	}
}

// TestOpenAI_ScannerFailureIsReported pins that a body whose line exceeds the SSE
// cap ends the turn with a read error instead of a silent, empty answer.
func TestOpenAI_ScannerFailureIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: "+strings.Repeat("x", maxSSELineBytes+1)+"\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	last := lastEvent(t, collectEvents(t, ch))
	if last.Kind != KindError || !strings.Contains(last.Err.Error(), "read stream") {
		t.Fatalf("an over-long line must be reported as a read failure, got %v / %v", last.Kind, last.Err)
	}
}

// TestAnthropic_ScannerFailureIsReported is the other half.
func TestAnthropic_ScannerFailureIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = io.WriteString(w, "data: "+strings.Repeat("x", maxSSELineBytes+1)+"\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	last := lastEvent(t, collectEvents(t, ch))
	if last.Kind != KindError || !strings.Contains(last.Err.Error(), "read stream") {
		t.Fatalf("expected a read failure, got %v / %v", last.Kind, last.Err)
	}
}

// TestOpenAI_AssistantMessageWithImageIsRefused pins the contract that the loop
// never produces this: if it does, the request fails locally instead of silently
// dropping the picture.
func TestOpenAI_AssistantMessageWithImageIsRefused(t *testing.T) {
	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: "http://127.0.0.1:1", Model: "m"})
	_, err := c.Stream(context.Background(), Request{Messages: []Message{
		{Role: RoleAssistant, Parts: []Part{{Image: &Image{MediaType: "image/png", Data: []byte{1}}}}},
	}})
	if err == nil || !strings.Contains(err.Error(), "cannot carry images") {
		t.Fatalf("expected a local refusal, got %v", err)
	}
}

// TestAnthropic_AssistantMessageWithImageIsRefused is the other half.
func TestAnthropic_AssistantMessageWithImageIsRefused(t *testing.T) {
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: "http://127.0.0.1:1", Model: "m"})
	_, err := c.Stream(context.Background(), Request{Messages: []Message{
		{Role: RoleAssistant, Parts: []Part{{Image: &Image{MediaType: "image/png", Data: []byte{1}}}}},
	}})
	if err == nil || !strings.Contains(err.Error(), "cannot carry images") {
		t.Fatalf("expected a local refusal, got %v", err)
	}
}

// TestAnthropic_RefusesImageWithoutVisionSupport pins that a user image reaches
// the caller as a local error, not as an opaque provider 400.
func TestAnthropic_RefusesImageWithoutVisionSupport(t *testing.T) {
	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: "http://127.0.0.1:1", Model: "m"})
	_, err := c.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Parts: []Part{
		{Text: "look"},
		{Image: &Image{MediaType: "image/png", Data: []byte{1}}},
	}}}})
	if !errors.Is(err, ErrVisionUnsupported) {
		t.Fatalf("expected ErrVisionUnsupported, got %v", err)
	}
}

// TestAnthropic_ToolResultImageIsSubstitutedForTextOnlyModels pins the difference
// from user images: a tool result's image cannot fail the request (the tool
// already ran), so it is replaced by a note the model can act on.
func TestAnthropic_ToolResultImageIsSubstitutedForTextOnlyModels(t *testing.T) {
	c := newAnthropicClient(Config{Protocol: ProtocolAnthropic, Model: "m"})
	blocks := c.toolResultContent(&ToolResult{CallID: "c1", Parts: []Part{
		{Image: &Image{MediaType: "image/png", Data: []byte{1}}},
	}})
	if len(blocks) != 1 || blocks[0].Type != anBlockText || !strings.Contains(blocks[0].Text, "cannot read images") {
		t.Fatalf("the image must be replaced by an explanation, got %+v", blocks)
	}
}

// TestAnthropic_EmptyToolResultGetsAPlaceholder pins that an empty content array
// is never sent: the API rejects it.
func TestAnthropic_EmptyToolResultGetsAPlaceholder(t *testing.T) {
	c := newAnthropicClient(Config{Protocol: ProtocolAnthropic, Model: "m"})
	blocks := c.toolResultContent(&ToolResult{CallID: "c1"})
	if len(blocks) != 1 || blocks[0].Text != "(no output)" {
		t.Fatalf("expected the placeholder, got %+v", blocks)
	}
	// Empty text parts are dropped, so a result of only empty strings is empty.
	blocks = c.toolResultContent(&ToolResult{CallID: "c1", Parts: []Part{{Text: ""}}})
	if len(blocks) != 1 || blocks[0].Text != "(no output)" {
		t.Fatalf("expected the placeholder for blank text, got %+v", blocks)
	}
}

// TestAnthropic_EmptyUserMessageGetsAPlaceholder pins the same rule for a user
// turn: an empty content array is a 400.
func TestAnthropic_EmptyUserMessageGetsAPlaceholder(t *testing.T) {
	c := newAnthropicClient(Config{Protocol: ProtocolAnthropic, Model: "m"})
	blocks, err := c.userBlocks(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Text != "(empty)" {
		t.Fatalf("expected the placeholder, got %+v", blocks)
	}
}

// TestAnthropic_UserTurnWithToolResultAndText pins that a user turn that carries
// both renders both, in order.
func TestAnthropic_UserTurnWithToolResultAndText(t *testing.T) {
	c := newAnthropicClient(Config{Protocol: ProtocolAnthropic, Model: "m"})
	blocks, err := c.userBlocks([]Part{
		{Text: "and then"},
		{ToolResult: &ToolResult{CallID: "c1", Parts: []Part{{Text: "ok"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || blocks[0].Type != anBlockText || blocks[1].Type != anBlockToolResult {
		t.Fatalf("expected text then tool_result, got %+v", blocks)
	}
}

// TestAnthropic_MalformedToolArgumentsBecomeAnEmptyObject pins that the request is
// not rejected wholesale over one bad argument blob: the tool layer reports the
// missing arguments instead.
func TestAnthropic_MalformedToolArgumentsBecomeAnEmptyObject(t *testing.T) {
	for _, args := range []string{"", "{not json"} {
		blocks, err := assistantBlocks([]Part{{ToolCall: &ToolCall{ID: "c1", Name: "read", Arguments: args}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(blocks) != 1 || string(blocks[0].Input) != "{}" {
			t.Fatalf("arguments %q must become {}, got %+v", args, blocks)
		}
	}
	blocks, _ := assistantBlocks([]Part{{ToolCall: &ToolCall{ID: "c1", Name: "read", Arguments: `{"path":"a"}`}}})
	if string(blocks[0].Input) != `{"path":"a"}` {
		t.Fatalf("valid arguments must pass through, got %s", blocks[0].Input)
	}
}

// TestAnthropic_EmptyAssistantTextIsDropped pins that a text part with no content
// does not become an empty block (some providers reject one).
func TestAnthropic_EmptyAssistantTextIsDropped(t *testing.T) {
	blocks, err := assistantBlocks([]Part{{Text: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 0 {
		t.Fatalf("an empty text part must produce no block, got %+v", blocks)
	}
}

// TestAnthropic_SystemPartsMergeIntoOneTurn pins that a system message lands as a
// user turn and merges with an adjacent one.
func TestAnthropic_SystemPartsMergeIntoOneTurn(t *testing.T) {
	b := &anMessageBuilder{}
	b.appendSystem([]Part{{Text: "rule one"}, {Text: "; rule two"}})
	if len(b.out) != 1 || b.out[0].Role != anRoleUser {
		t.Fatalf("a system message must become a user turn, got %+v", b.out)
	}
	if b.out[0].Content[0].Text != "rule one; rule two" {
		t.Fatalf("all system parts must be joined, got %q", b.out[0].Content[0].Text)
	}
	// An empty system message adds nothing at all.
	b.appendSystem(nil)
	if len(b.out) != 1 {
		t.Fatalf("an empty system message must add no turn, got %d turns", len(b.out))
	}
}

// TestAnthropic_AppendBlocksMergesSameRoleOnly pins the alternating-role rule:
// same-role turns append, a different role starts a new turn.
func TestAnthropic_AppendBlocksMergesSameRoleOnly(t *testing.T) {
	b := &anMessageBuilder{}
	b.appendBlocks(anRoleAssistant, []anBlock{{Type: anBlockText, Text: "a"}})
	b.appendBlocks(anRoleAssistant, []anBlock{{Type: anBlockText, Text: "b"}})
	if len(b.out) != 1 || len(b.out[0].Content) != 2 {
		t.Fatalf("same-role turns must merge, got %+v", b.out)
	}
	b.appendBlocks(anRoleUser, []anBlock{{Type: anBlockText, Text: "u"}})
	if len(b.out) != 2 {
		t.Fatalf("a different role must start a new turn, got %+v", b.out)
	}
	b.appendBlocks(anRoleUser, nil)
	if len(b.out) != 2 {
		t.Fatalf("no blocks must add no turn, got %+v", b.out)
	}
}

// TestAnthropic_ResolveMaxTokens pins the required-field default: this API
// rejects a request without max_tokens.
func TestAnthropic_ResolveMaxTokens(t *testing.T) {
	if got := resolveMaxTokens(Request{}); got != 8192 {
		t.Fatalf("an unset cap must default to 8192, got %d", got)
	}
	if got := resolveMaxTokens(Request{MaxOutputTokens: 100}); got != 100 {
		t.Fatalf("an explicit cap must be honored, got %d", got)
	}
}

// TestAnthropic_MaxTokensGrowsPastTheThinkingBudget pins the constraint: a user
// cap below the budget would be rejected, so the request grows it.
func TestAnthropic_MaxTokensGrowsPastTheThinkingBudget(t *testing.T) {
	c := newAnthropicClient(Config{Model: "m"})
	body, err := c.buildBody(Request{
		Messages:        []Message{TextMessage(RoleUser, "hi")},
		MaxOutputTokens: 100,
		ReasoningEffort: EffortMedium,
	})
	if err != nil {
		t.Fatal(err)
	}
	var req anRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens <= req.Thinking.BudgetTokens {
		t.Fatalf("max_tokens %d must exceed the budget %d", req.MaxTokens, req.Thinking.BudgetTokens)
	}
	if req.MaxTokens != req.Thinking.BudgetTokens+4096 {
		t.Fatalf("max_tokens = %d, want budget+4096", req.MaxTokens)
	}
}

// TestNormalizeEffort pins the mapping: "no preference" becomes empty, everything
// else is normalized but preserved.
func TestNormalizeEffort(t *testing.T) {
	cases := map[string]string{
		"":        "",
		"  ":      "",
		"auto":    "",
		"AUTO":    "",
		"default": "",
		" High ":  "high",
		"MEDIUM":  "medium",
		"extreme": "extreme",
	}
	for in, want := range cases {
		if got := normalizeEffort(in); got != want {
			t.Errorf("normalizeEffort(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResolveTemperature pins the zero handling: zero means "the caller did not
// choose", not "deterministic".
func TestResolveTemperature(t *testing.T) {
	if got := resolveTemperature(Request{}); got != DefaultTemperature {
		t.Fatalf("got %v, want the default", got)
	}
	if got := resolveTemperature(Request{Temperature: 0.9}); got != 0.9 {
		t.Fatalf("got %v, want 0.9", got)
	}
}

// TestResolveEndpoint_EdgeShapes pins the shapes the settings page allows but the
// main table does not exercise: a bare host without a scheme, trailing slashes,
// an empty base, and a version segment that is not a valid one.
func TestResolveEndpoint_EdgeShapes(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"", "/v1/chat/completions", ""},
		{"   ", "/v1/messages", ""},
		{"api.example.com", "/v1/messages", "api.example.com/v1/messages"},
		{"https://api.example.com/v1beta", "/v1/messages", "https://api.example.com/v1beta/messages"},
		// A versionless path is a namespace the API is mounted under, not a
		// complete endpoint, so the protocol path is appended to it.
		{"https://x.example.com/chat", "/v1/messages", "https://x.example.com/chat/v1/messages"},
		// A versioned path was typed deliberately and is kept verbatim.
		{"https://x.example.com/v1/chat", "/v1/messages", "https://x.example.com/v1/chat"},
	}
	for _, tc := range cases {
		if got := resolveEndpoint(tc.base, tc.path); got != tc.want {
			t.Errorf("resolveEndpoint(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

// TestAuthHeaderIsAPIKey pins the comparison: the mode is user-editable text, so
// case and padding must not decide whether the key is sent.
func TestAuthHeaderIsAPIKey(t *testing.T) {
	if !authHeaderIsAPIKey(" X-API-KEY ") {
		t.Fatal("the mode must be matched case-insensitively and trimmed")
	}
	for _, mode := range []string{"", "auto", "bearer"} {
		if authHeaderIsAPIKey(mode) {
			t.Fatalf("%q must not select x-api-key", mode)
		}
	}
}

// TestEmitEach_StopsWhenTheConsumerIsGone pins the delivery contract: a cancelled
// context makes emitEach report false instead of blocking forever, which is what
// lets the producer goroutine exit.
func TestEmitEach_StopsWhenTheConsumerIsGone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan Event, 1)
	ch <- Event{Kind: KindText} // fill the buffer so the next send would block
	if emitEach(ctx, ch, []Event{{Kind: KindText}, {Kind: KindText}}) {
		t.Fatal("a cancelled context must report that delivery failed")
	}
}

// TestEmitTerminal_SendsExactlyOneEvent pins the terminal contract in both
// directions.
func TestEmitTerminal_SendsExactlyOneEvent(t *testing.T) {
	ctx := context.Background()

	ch := make(chan Event, 4)
	emitTerminal(ctx, ch, nil)
	close(ch)
	evs := collectEvents(t, ch)
	if len(evs) != 1 || evs[0].Kind != KindDone {
		t.Fatalf("a success must send exactly one done, got %+v", evs)
	}

	ch = make(chan Event, 4)
	boom := errors.New("boom")
	emitTerminal(ctx, ch, boom)
	close(ch)
	evs = collectEvents(t, ch)
	if len(evs) != 1 || evs[0].Kind != KindError || !errors.Is(evs[0].Err, boom) {
		t.Fatalf("a failure must send exactly one error carrying the cause, got %+v", evs)
	}

	ch = make(chan Event, 4)
	emitTruncated(ctx, ch)
	close(ch)
	evs = collectEvents(t, ch)
	if len(evs) != 1 || evs[0].Kind != KindDone || !evs[0].Truncated {
		t.Fatalf("truncation must be a done with the flag, got %+v", evs)
	}
}

// TestNextSSEPayload pins the framing rules directly, including the two shapes
// gateways send that carry no payload.
func TestNextSSEPayload(t *testing.T) {
	body := strings.Join([]string{
		": keepalive",
		"",
		"event: message",
		"data:first",
		"data: second ",
	}, "\n")
	scanner := newSSEScanner(strings.NewReader(body))
	got, ok := nextSSEPayload(scanner)
	if !ok || got != "first" {
		t.Fatalf("got %q %v, want \"first\" true", got, ok)
	}
	got, ok = nextSSEPayload(scanner)
	if !ok || got != "second" {
		t.Fatalf("got %q %v, want \"second\" true", got, ok)
	}
	if _, ok := nextSSEPayload(scanner); ok {
		t.Fatal("an exhausted body must report false")
	}
}

// TestNewSSEScanner_AllowsALongLine pins the buffer ceiling: a tool-call argument
// carrying a whole file must not overflow the default 64 KiB scanner limit.
func TestNewSSEScanner_AllowsALongLine(t *testing.T) {
	long := "data: " + strings.Repeat("a", 200*1024)
	scanner := newSSEScanner(strings.NewReader(long))
	got, ok := nextSSEPayload(scanner)
	if !ok {
		t.Fatalf("a 200 KiB line must be accepted: %v", scanner.Err())
	}
	if len(got) != 200*1024 {
		t.Fatalf("payload truncated to %d bytes", len(got))
	}
}
