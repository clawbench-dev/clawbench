package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// collectEvents drains a stream into a slice.
func collectEvents(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var out []Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// TestOpenAI_StreamsTextAndToolCalls pins the SSE parsing, which is the part
// that silently produces an empty answer when it is wrong.
func TestOpenAI_StreamsTextAndToolCalls(t *testing.T) {
	var gotBody oaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read","arguments":"{\"path\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.txt\"}"}}]}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`,
			`[DONE]`,
		}
		for _, f := range frames {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, err := New(Config{
		Protocol: ProtocolOpenAI, BaseURL: srv.URL, APIKey: "k", Model: "m1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := c.Stream(context.Background(), Request{
		System:   "be brief",
		Messages: []Message{TextMessage(RoleUser, "hi")},
		Tools: []ToolSpec{{
			Name: "read", Description: "read a file",
			Parameters: map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := collectEvents(t, ch)

	var text strings.Builder
	var args strings.Builder
	var usage *Usage
	var terminal EventKind
	for _, ev := range events {
		switch ev.Kind {
		case KindText:
			text.WriteString(ev.Text)
		case KindToolCallStart:
			if ev.ToolCall.Name != "read" {
				t.Fatalf("expected the tool name, got %q", ev.ToolCall.Name)
			}
		case KindToolCallDelta:
			args.WriteString(ev.ToolCall.Arguments)
		case KindUsage:
			usage = ev.Usage
		case KindDone, KindError:
			terminal = ev.Kind
		}
	}
	if text.String() != "Hello" {
		t.Fatalf("expected the streamed text, got %q", text.String())
	}
	if args.String() != `{"path":"a.txt"}` {
		t.Fatalf("tool arguments must be reassembled across deltas, got %q", args.String())
	}
	if usage == nil || usage.InputTokens != 11 || usage.OutputTokens != 7 {
		t.Fatalf("usage must be parsed, got %+v", usage)
	}
	if terminal != KindDone {
		t.Fatalf("expected a done terminal, got %v", terminal)
	}

	// The request must carry the system prompt, the tools and stream_options.
	if gotBody.Model != "m1" {
		t.Fatalf("model must be sent, got %q", gotBody.Model)
	}
	if len(gotBody.Tools) != 1 || gotBody.Tools[0].Function.Name != "read" {
		t.Fatalf("tools must be declared, got %+v", gotBody.Tools)
	}
	if gotBody.StreamOpts == nil || !gotBody.StreamOpts.IncludeUsage {
		t.Fatal("stream_options.include_usage must be set or usage is never reported")
	}
	if gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != "be brief" {
		t.Fatalf("the system prompt must be the first message, got %+v", gotBody.Messages[0])
	}
}

// TestOpenAI_HTTPErrorIsTyped pins that a non-2xx response carries its status
// so the caller can distinguish retryable from terminal.
func TestOpenAI_HTTPErrorIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	_, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err == nil {
		t.Fatal("a 401 must be an error")
	}
	he, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected an HTTPError, got %T", err)
	}
	if he.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", he.Status)
	}
	if he.Retryable() {
		t.Fatal("401 must not be retryable")
	}
	if !(&HTTPError{Status: 429}).Retryable() || !(&HTTPError{Status: 503}).Retryable() {
		t.Fatal("429 and 5xx must be retryable")
	}
}

// TestOpenAI_EmptyStreamIsAnError pins that a well-formed but empty response is
// reported as such instead of rendering an empty bubble.
func TestOpenAI_EmptyStreamIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	events := collectEvents(t, ch)
	last := events[len(events)-1]
	if last.Kind != KindError || last.Err != ErrNoOutput {
		t.Fatalf("an empty stream must be reported, got %v / %v", last.Kind, last.Err)
	}
}

// TestOpenAI_ToolResultWithImageBecomesTrailingUserMessage pins the workaround
// this protocol needs: role="tool" content is a string, so images must ride a
// following user message.
func TestOpenAI_ToolResultWithImageBecomesTrailingUserMessage(t *testing.T) {
	var gotBody oaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{
		Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m", SupportsVision: true,
	})
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{
			TextMessage(RoleUser, "look"),
			{Role: RoleTool, Parts: []Part{{ToolResult: &ToolResult{
				CallID: "c1",
				Parts: []Part{
					{Text: "here is the picture"},
					{Image: &Image{MediaType: "image/png", Data: []byte{1, 2, 3}}},
				},
			}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ch)

	var sawTool, sawUserImage bool
	for _, m := range gotBody.Messages {
		if m.Role == "tool" {
			if m.ToolCallID != "c1" {
				t.Fatalf("the tool message must reference the call id, got %q", m.ToolCallID)
			}
			if _, isString := m.Content.(string); !isString {
				t.Fatalf("role=tool content must be a string, got %T", m.Content)
			}
			sawTool = true
		}
		if m.Role == "user" {
			if parts, ok := m.Content.([]any); ok {
				for _, p := range parts {
					pm, _ := p.(map[string]any)
					if pm["type"] == "image_url" {
						sawUserImage = true
					}
				}
			}
		}
	}
	if !sawTool {
		t.Fatal("the tool result text must be sent as a tool message")
	}
	if !sawUserImage {
		t.Fatal("the image must be sent as a trailing user message")
	}
}

// TestOpenAI_RefusesImageWithoutVisionSupport pins the fail-fast behaviour: an
// opaque 400 from the endpoint is worse than a clear local error.
func TestOpenAI_RefusesImageWithoutVisionSupport(t *testing.T) {
	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: "http://127.0.0.1:1", Model: "m"})
	_, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Parts: []Part{
			{Text: "look"},
			{Image: &Image{MediaType: "image/png", Data: []byte{1}}},
		}}},
	})
	if err == nil {
		t.Fatal("an image sent to a text-only model must fail locally")
	}
}

// TestAnthropic_PutsToolResultsInUserMessage pins the structural difference
// from OpenAI: tool results are USER content blocks and consecutive ones merge
// into one message (the API requires alternating roles).
func TestAnthropic_PutsToolResultsInUserMessage(t *testing.T) {
	var gotBody anRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":9}}}`,
			`{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`,
			`{"type":"message_delta","usage":{"output_tokens":3}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, APIKey: "k", Model: "claude-x"})
	ch, err := c.Stream(context.Background(), Request{
		System: "be brief",
		Messages: []Message{
			TextMessage(RoleUser, "read two files"),
			{Role: RoleTool, Parts: []Part{
				{ToolResult: &ToolResult{CallID: "c1", Parts: []Part{{Text: "one"}}}},
				{ToolResult: &ToolResult{CallID: "c2", Parts: []Part{{Text: "two"}}}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ch)

	if gotBody.System != "be brief" {
		t.Fatalf("the system prompt must be a top-level field, got %q", gotBody.System)
	}
	if gotBody.MaxTokens == 0 {
		t.Fatal("max_tokens is required by this API and must be set")
	}
	// The tool results must be in a user message, and they must come BEFORE
	// any text in that message: this API rejects a tool_result that is not the
	// first block of the turn answering it.
	last := gotBody.Messages[len(gotBody.Messages)-1]
	if last.Role != "user" {
		t.Fatalf("tool results must be a user message, got %q", last.Role)
	}
	if len(last.Content) < 2 {
		t.Fatalf("both tool results must be present, got %d blocks", len(last.Content))
	}
	for i := 0; i < 2; i++ {
		if last.Content[i].Type != "tool_result" {
			t.Fatalf("block %d must be tool_result (they must lead the message), got %q", i, last.Content[i].Type)
		}
	}
	if last.Content[0].ToolUseID != "c1" || last.Content[1].ToolUseID != "c2" {
		t.Fatal("each tool_result must reference its call id")
	}
	// The preceding user text must survive, merged into the same turn rather
	// than sent as a second consecutive user message (which the API rejects).
	var sawText bool
	for _, b := range last.Content {
		if b.Type == "text" && b.Text == "read two files" {
			sawText = true
		}
	}
	if !sawText {
		t.Fatal("the user's own text must be preserved in the same turn")
	}
	if len(gotBody.Messages) != 1 {
		t.Fatalf("consecutive user-side turns must merge into one message, got %d", len(gotBody.Messages))
	}
}

// TestAnthropic_ImageRidesInsideToolResult pins the one place this protocol is
// easier than OpenAI's.
func TestAnthropic_ImageRidesInsideToolResult(t *testing.T) {
	var gotBody anRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{
		Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "claude-x", SupportsVision: true,
	})
	ch, _ := c.Stream(context.Background(), Request{
		Messages: []Message{
			{Role: RoleTool, Parts: []Part{{ToolResult: &ToolResult{
				CallID: "c1",
				Parts: []Part{
					{Text: "picture"},
					{Image: &Image{MediaType: "image/png", Data: []byte{9, 9}}},
				},
			}}}},
		},
	})
	collectEvents(t, ch)

	last := gotBody.Messages[len(gotBody.Messages)-1]
	block := last.Content[0]
	if block.Type != "tool_result" {
		t.Fatalf("expected a tool_result block, got %q", block.Type)
	}
	var sawImage bool
	for _, b := range block.Content {
		if b.Type == "image" {
			sawImage = true
			if b.Source == nil || b.Source.Type != "base64" {
				t.Fatal("the image source must be base64")
			}
			if b.Source.MediaType != "image/png" {
				t.Fatalf("the media type must be carried, got %q", b.Source.MediaType)
			}
		}
	}
	if !sawImage {
		t.Fatal("the image must ride inside the tool_result content")
	}
}

// TestAnthropic_StreamsToolUseArguments pins that partial_json fragments are
// concatenated, and that a tool_use block start announces the call.
func TestAnthropic_StreamsToolUseArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu1","name":"bash"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{
		Messages: []Message{TextMessage(RoleUser, "list")},
		Tools:    []ToolSpec{{Name: "bash", Parameters: map[string]any{"type": "object"}}},
	})
	events := collectEvents(t, ch)

	var name, args string
	for _, ev := range events {
		switch ev.Kind {
		case KindToolCallStart:
			name = ev.ToolCall.Name
		case KindToolCallDelta:
			args += ev.ToolCall.Arguments
		}
	}
	if name != "bash" {
		t.Fatalf("the tool name must be announced, got %q", name)
	}
	if args != `{"command":"ls"}` {
		t.Fatalf("arguments must be reassembled, got %q", args)
	}
}

// TestAnthropic_ErrorEventIsTerminal pins that an in-stream error ends the run
// instead of hanging on a channel that never closes.
func TestAnthropic_ErrorEventIsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"try later\"}}\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	events := collectEvents(t, ch)
	last := events[len(events)-1]
	if last.Kind != KindError || !strings.Contains(last.Err.Error(), "try later") {
		t.Fatalf("the in-stream error must be terminal and carry the message, got %v / %v", last.Kind, last.Err)
	}
}

// TestOpenAI_FinishReasonLengthIsTruncated pins the signal behind "the answer
// stopped mid-sentence and looked finished": the endpoint reports the cap with
// finish_reason=length, which must reach the caller instead of being parsed and
// ignored.
func TestOpenAI_FinishReasonLengthIsTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"choices":[{"delta":{"content":"The config lives in"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
			`[DONE]`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "go")}})
	events := collectEvents(t, ch)

	last := events[len(events)-1]
	if last.Kind != KindDone {
		t.Fatalf("truncation is not a failure, expected done, got %v (%v)", last.Kind, last.Err)
	}
	if !last.Truncated {
		t.Fatal("finish_reason=length must be reported as truncated")
	}
}

// TestOpenAI_TruncatedBeforeEmptyStream pins the ordering: a reply cut off
// during reasoning has no content at all, and "incomplete" is the useful
// diagnosis — reporting ErrNoOutput would send the caller into a retry that hits
// the same cap.
func TestOpenAI_TruncatedBeforeEmptyStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"choices":[{"delta":{"reasoning_content":"thinking..."},"finish_reason":"length"}]}`,
			`[DONE]`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "go")}})
	events := collectEvents(t, ch)

	last := events[len(events)-1]
	if last.Kind != KindDone || !last.Truncated {
		t.Fatalf("expected a truncated done, got %v (truncated=%v, err=%v)", last.Kind, last.Truncated, last.Err)
	}
}

// TestAnthropic_MaxTokensIsTruncated pins the Anthropic spelling of the same
// signal: stop_reason=max_tokens arrives on message_delta, one event before
// message_stop.
func TestAnthropic_MaxTokensIsTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":99}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "go")}})
	events := collectEvents(t, ch)

	last := events[len(events)-1]
	if last.Kind != KindDone || !last.Truncated {
		t.Fatalf("expected a truncated done, got %v (truncated=%v, err=%v)", last.Kind, last.Truncated, last.Err)
	}
}

// TestAnthropic_EndTurnIsNotTruncated pins the negative: a normal stop must not
// be reported as truncated, or every answer would be continued.
func TestAnthropic_EndTurnIsNotTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m"})
	ch, _ := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "go")}})
	events := collectEvents(t, ch)

	last := events[len(events)-1]
	if last.Kind != KindDone || last.Truncated {
		t.Fatalf("a normal end_turn must not be truncated, got %v (truncated=%v)", last.Kind, last.Truncated)
	}
}

func TestNew_RejectsUnsupportedProtocol(t *testing.T) {
	if _, err := New(Config{Protocol: "gemini"}); err == nil {
		t.Fatal("an unsupported protocol must fail loudly rather than silently doing nothing")
	}
	if _, err := New(Config{Protocol: ""}); err == nil {
		t.Fatal("an empty protocol must fail")
	}
}

// TestResolveEndpoint pins the base-URL shapes the settings page accepts.
//
// This mirrors summarize.BuildEndpointURL for the cases that matter; getting it
// wrong is the bug that once made "test connection" pass while the real call
// 404'd.
func TestResolveEndpoint(t *testing.T) {
	tests := []struct {
		base, path, want string
	}{
		{"https://api.openai.com", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/chat/completions", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://gw.example.com/api/v1", "/v1/chat/completions", "https://gw.example.com/api/v1/chat/completions"},
		{"https://api.anthropic.com", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		// A provider-specific endpoint is used verbatim: appending would 404.
		{"https://api.minimax.cn/v1/text/chatcompletion_v2", "/v1/chat/completions", "https://api.minimax.cn/v1/text/chatcompletion_v2"},
	}
	for _, tc := range tests {
		if got := resolveEndpoint(tc.base, tc.path); got != tc.want {
			t.Errorf("resolveEndpoint(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

// TestOpenAI_ReasoningEffort pins the one field that carries the user's
// thinking level on this protocol. "auto" must send NOTHING: a plain chat model
// given reasoning_effort is a 400 the user cannot act on.
func TestOpenAI_ReasoningEffort(t *testing.T) {
	cases := []struct {
		name   string
		effort string
		want   string
	}{
		{"high", "high", "high"},
		{"auto", "auto", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody oaRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()

			c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
			ch, err := c.Stream(context.Background(), Request{
				Messages:        []Message{TextMessage(RoleUser, "hi")},
				ReasoningEffort: tc.effort,
			})
			if err != nil {
				t.Fatal(err)
			}
			collectEvents(t, ch)
			if gotBody.ReasoningEffort != tc.want {
				t.Fatalf("reasoning_effort = %q, want %q", gotBody.ReasoningEffort, tc.want)
			}
			// A reasoning request must NOT carry a temperature: o-series and
			// GPT-5 reject any explicit value, so the level would fail on a
			// parameter the user never set.
			if tc.want != "" && gotBody.Temperature != 0 {
				t.Fatalf("temperature = %v, want omitted while reasoning is on", gotBody.Temperature)
			}
			if tc.want == "" && gotBody.Temperature != DefaultTemperature {
				t.Fatalf("temperature = %v, want the default %v", gotBody.Temperature, DefaultTemperature)
			}
		})
	}
}

// TestAnthropic_ThinkingBudget pins the translation of a level into the
// extended-thinking request, including the two constraints the API enforces:
// max_tokens must exceed the budget, and temperature must be 1.
func TestAnthropic_ThinkingBudget(t *testing.T) {
	var gotBody anRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "claude-x"})
	ch, err := c.Stream(context.Background(), Request{
		Messages:        []Message{TextMessage(RoleUser, "hi")},
		ReasoningEffort: EffortHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ch)

	if gotBody.Thinking == nil {
		t.Fatal("high effort must enable extended thinking")
	}
	if gotBody.Thinking.Type != "enabled" || gotBody.Thinking.BudgetTokens != 8192 {
		t.Fatalf("thinking = %+v, want enabled/8192", *gotBody.Thinking)
	}
	if gotBody.MaxTokens <= gotBody.Thinking.BudgetTokens {
		t.Fatalf("max_tokens (%d) must exceed budget_tokens (%d)", gotBody.MaxTokens, gotBody.Thinking.BudgetTokens)
	}
	if gotBody.Temperature != 1 {
		t.Fatalf("temperature = %v, want 1 while thinking is enabled", gotBody.Temperature)
	}
}

// TestAnthropic_AutoEffortSendsNoThinking pins the absence direction: "auto"
// must not turn reasoning on behind the user's back.
func TestAnthropic_AutoEffortSendsNoThinking(t *testing.T) {
	var gotBody anRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "claude-x"})
	ch, _ := c.Stream(context.Background(), Request{
		Messages:        []Message{TextMessage(RoleUser, "hi")},
		ReasoningEffort: EffortAuto,
	})
	collectEvents(t, ch)

	if gotBody.Thinking != nil {
		t.Fatalf("auto must not enable thinking, got %+v", *gotBody.Thinking)
	}
	if gotBody.Temperature != DefaultTemperature {
		t.Fatalf("temperature = %v, want the default %v", gotBody.Temperature, DefaultTemperature)
	}
}

// TestOpenAI_ReasoningContentBecomesThinking pins the vendor reasoning field.
//
// A reasoning level on a DeepSeek/GLM/Qwen gateway produces the thinking stream
// in `reasoning_content` (OpenRouter: `reasoning`). Ignoring it made the level
// look like it did nothing: the turn took longer and showed no thinking block.
func TestOpenAI_ReasoningContentBecomesThinking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"choices":[{"delta":{"reasoning_content":"think "}}]}`,
			`{"choices":[{"delta":{"content":"answer"}}]}`,
			`[DONE]`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m"})
	ch, err := c.Stream(context.Background(), Request{
		Messages:        []Message{TextMessage(RoleUser, "hi")},
		ReasoningEffort: EffortHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := collectEvents(t, ch)

	var kinds []EventKind
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
	}
	if len(kinds) < 3 || kinds[0] != KindReasoning || kinds[1] != KindText {
		t.Fatalf("events = %v, want reasoning then text", kinds)
	}
	if events[0].Reasoning != "think " {
		t.Fatalf("reasoning = %q", events[0].Reasoning)
	}
}

// TestAnthropic_ReplaysThinkingBlock pins the wire shape required when an
// extended-thinking turn also calls a tool: the assistant message must carry
// the thinking block (with signature) before the tool_use block.
func TestAnthropic_ReplaysThinkingBlock(t *testing.T) {
	var gotBody anRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "claude-x"})
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{
			TextMessage(RoleUser, "read it"),
			{Role: RoleAssistant, Parts: []Part{
				{Thinking: &Thinking{Text: "I should read it", Signature: "sig-1"}},
				{ToolCall: &ToolCall{ID: "c1", Name: "read", Arguments: `{"path":"a.txt"}`}},
			}},
			{Role: RoleTool, Parts: []Part{{ToolResult: &ToolResult{CallID: "c1", Parts: []Part{{Text: "body"}}}}}},
		},
		ReasoningEffort: EffortHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ch)

	var assistant *anMessage
	for i := range gotBody.Messages {
		if gotBody.Messages[i].Role == "assistant" {
			assistant = &gotBody.Messages[i]
		}
	}
	if assistant == nil || len(assistant.Content) < 2 {
		t.Fatalf("assistant message = %+v, want thinking + tool_use", assistant)
	}
	first := assistant.Content[0]
	if first.Type != "thinking" || first.Thinking != "I should read it" || first.Signature != "sig-1" {
		t.Fatalf("first block = %+v, want the thinking block with its signature", first)
	}
	if assistant.Content[1].Type != "tool_use" {
		t.Fatalf("second block = %+v, want tool_use", assistant.Content[1])
	}
}

// TestAnthropic_SignatureDeltaBecomesAnEvent pins the capture half: the client
// must surface the signature so the loop can replay it.
func TestAnthropic_SignatureDeltaBecomesAnEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range []string{
			`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hm"}}`,
			`{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"sig-9"}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "claude-x"})
	ch, _ := c.Stream(context.Background(), Request{
		Messages:        []Message{TextMessage(RoleUser, "hi")},
		ReasoningEffort: EffortHigh,
	})
	events := collectEvents(t, ch)

	var signature string
	for _, ev := range events {
		if ev.Kind == KindReasoningSignature {
			signature = ev.Signature
		}
	}
	if signature != "sig-9" {
		t.Fatalf("signature = %q, want sig-9", signature)
	}
}
