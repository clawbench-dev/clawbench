package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// headerRecorder captures the request headers of each stream call.
type headerRecorder struct {
	mu   sync.Mutex
	seen []http.Header
}

func (h *headerRecorder) record(r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, r.Header.Clone())
}

func (h *headerRecorder) headers() []http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]http.Header(nil), h.seen...)
}

func sseHeaderServer(t *testing.T, rec *headerRecorder) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
}

// TestOpenAI_SendsOpenCodeHeaders pins the Go requirement: the endpoint rejects
// every request without x-opencode-session, so the client must send it (and its
// own User-Agent) even though the host is a local test server here.
func TestOpenAI_SendsOpenCodeHeaders(t *testing.T) {
	rec := &headerRecorder{}
	srv := sseHeaderServer(t, rec)
	defer srv.Close()

	c, err := New(Config{
		Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m",
		PresetID: "opencode-go", SessionID: "conv-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ch)

	headers := rec.headers()
	if len(headers) != 1 {
		t.Fatalf("expected one request, got %d", len(headers))
	}
	if got := headers[0].Get("x-opencode-session"); got != "conv-7" {
		t.Fatalf("x-opencode-session = %q, want the conversation id", got)
	}
	if ua := headers[0].Get("User-Agent"); !strings.HasPrefix(ua, "clawbench/") {
		t.Fatalf("User-Agent = %q, want our own client name", ua)
	}
}

// TestOpenAI_KeepsOneGeneratedSessionPerClient pins that a one-off caller (no
// conversation id) still gets ONE id for the whole client, not a fresh id per
// request — otherwise routing and prompt caching never line up.
func TestOpenAI_KeepsOneGeneratedSessionPerClient(t *testing.T) {
	rec := &headerRecorder{}
	srv := sseHeaderServer(t, rec)
	defer srv.Close()

	c, _ := New(Config{
		Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m", PresetID: "opencode",
	})
	for range 2 {
		ch, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
		if err != nil {
			t.Fatal(err)
		}
		collectEvents(t, ch)
	}

	headers := rec.headers()
	if len(headers) != 2 {
		t.Fatalf("expected two requests, got %d", len(headers))
	}
	a, b := headers[0].Get("x-opencode-session"), headers[1].Get("x-opencode-session")
	if a == "" || a != b {
		t.Fatalf("session id must be stable across requests, got %q then %q", a, b)
	}
}

// TestOpenAI_DoesNotLeakSessionToOtherProviders pins the other half: a normal
// gateway must not receive a header it never asked for.
func TestOpenAI_DoesNotLeakSessionToOtherProviders(t *testing.T) {
	rec := &headerRecorder{}
	srv := sseHeaderServer(t, rec)
	defer srv.Close()

	c, _ := New(Config{
		Protocol: ProtocolOpenAI, BaseURL: srv.URL, Model: "m",
		PresetID: "deepseek", SessionID: "conv-7",
	})
	ch, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ch)

	if got := rec.headers()[0].Get("x-opencode-session"); got != "" {
		t.Fatalf("an unrelated provider received x-opencode-session = %q", got)
	}
}

func TestAnthropic_SendsOpenCodeHeaders(t *testing.T) {
	rec := &headerRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}`+"\n\n")
	}))
	defer srv.Close()

	c, _ := New(Config{
		Protocol: ProtocolAnthropic, BaseURL: srv.URL, Model: "m",
		PresetID: "opencode-go", SessionID: "conv-9",
	})
	ch, err := c.Stream(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	collectEvents(t, ch)

	if got := rec.headers()[0].Get("x-opencode-session"); got != "conv-9" {
		t.Fatalf("x-opencode-session = %q, want the conversation id", got)
	}
}
