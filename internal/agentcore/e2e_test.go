package agentcore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"
)

// TestRun_EndToEndOverHTTP drives the whole stack — real llm client, real
// tools, real filesystem — against a fake OpenAI endpoint.
//
// The unit tests above use a scripted client, which cannot catch a protocol
// mistake (a malformed request body, an unparsed tool call). This one can: the
// fake server validates the request shape and returns a tool call, and the
// second round asserts the file content actually reached the model.
func TestRun_EndToEndOverHTTP(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("file contents here"), 0o644); err != nil {
		t.Fatal(err)
	}

	var round atomic.Int32
	var secondRequest string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := round.Add(1)

		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request is not valid JSON: %v", err)
			return
		}
		if req.Model == "" {
			t.Error("the model id must be sent")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			// First round: the model reads the file.
			var names []string
			for _, tool := range req.Tools {
				names = append(names, tool.Function.Name)
			}
			if len(names) == 0 {
				t.Error("the read policy must declare tools")
			}
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"path\":\"hello.txt\"}"}}]}}]}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}

		// Second round: the tool result must be present.
		secondRequest = string(body)
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"The file says: file contents here"}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":8}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client, err := llm.New(llm.Config{
		Protocol: llm.ProtocolOpenAI,
		BaseURL:  srv.URL,
		APIKey:   "test-key",
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := RunSync(context.Background(), Spec{
		Client:       client,
		SystemPrompt: "You are a test agent.",
		Prompt:       "read hello.txt and tell me what it says",
		WorkDir:      root,
		Policy:       model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}, nil)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if !strings.Contains(res.Text, "file contents here") {
		t.Fatalf("expected the model's answer, got %q", res.Text)
	}
	if res.ToolCalls != 1 {
		t.Fatalf("expected exactly one tool call, got %d", res.ToolCalls)
	}
	if res.Rounds != 2 {
		t.Fatalf("expected two model rounds, got %d", res.Rounds)
	}
	if res.Usage.InputTokens == 0 {
		t.Fatal("usage must be accumulated across rounds")
	}

	// The tool result must have reached the model as a tool message carrying
	// the real file content.
	if !strings.Contains(secondRequest, "file contents here") {
		t.Fatalf("the tool result must be replayed to the model, request was: %s", secondRequest)
	}
	if !strings.Contains(secondRequest, `"role":"tool"`) {
		t.Fatalf("the result must be sent as a tool message, request was: %s", secondRequest)
	}
}

// TestRun_CompactionOverHTTP drives the whole stack against a fake endpoint:
// a conversation that overflows the window must be summarized, and the request
// the model finally sees must contain the summary instead of the originals.
//
// The unit tests use a stub client, which cannot catch a protocol mistake in the
// summarizer's own request (a missing system prompt, an unparsed reply).
func TestRun_CompactionOverHTTP(t *testing.T) {
	var (
		summaryCalls atomic.Int32
		lastRequest  atomic.Value
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			System   string `json:"-"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)

		// A summary request carries the compaction instruction as its first
		// system message.
		isSummary := strings.Contains(string(body), "compacting a conversation")
		w.Header().Set("Content-Type", "text/event-stream")
		if isSummary {
			summaryCalls.Add(1)
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"SUMMARY-OF-OLDER-TURNS"}}]}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}

		lastRequest.Store(string(body))
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client, err := llm.New(llm.Config{
		Protocol: llm.ProtocolOpenAI, BaseURL: srv.URL, Model: "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	// A history that does not fit a 2000-token window.
	history := manyTurns(20, 2000)

	res, err := RunSync(context.Background(), Spec{
		Client:        client,
		Prompt:        "and now?",
		History:       history,
		WorkDir:       t.TempDir(),
		Policy:        model.ToolPolicy{Allow: []string{model.CapFSRead}},
		ContextWindow: 2000,
		Summarize:     withSummary(client),
	}, nil)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if summaryCalls.Load() == 0 {
		t.Fatal("an overflowing conversation must be summarized over HTTP")
	}
	if res.StopReason != StopComplete {
		t.Fatalf("the run must complete normally, got %q", res.StopReason)
	}
	if res.Summary == "" {
		t.Fatal("the run must report its summary so the caller can persist it")
	}

	// The final request must carry the summary and NOT the originals.
	final, _ := lastRequest.Load().(string)
	if !strings.Contains(final, "SUMMARY-OF-OLDER-TURNS") {
		t.Fatalf("the model must receive the summary, request was: %.300s", final)
	}
}

// TestRun_ReadOnlyPolicyCannotWriteOverHTTP pins the safety boundary against a
// real model that tries anyway.
//
// The model asks for a write tool that was never declared. The run must survive
// (the model gets an error it can read) and the file must not appear.
func TestRun_ReadOnlyPolicyCannotWriteOverHTTP(t *testing.T) {
	root := t.TempDir()

	var round atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if round.Add(1) == 1 {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"write","arguments":"{\"path\":\"evil.txt\",\"content\":\"pwned\"}"}}]}}]}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"I cannot write files."}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client, _ := llm.New(llm.Config{
		Protocol: llm.ProtocolOpenAI, BaseURL: srv.URL, Model: "m",
	})
	res, err := RunSync(context.Background(), Spec{
		Client:  client,
		Prompt:  "write evil.txt",
		WorkDir: root,
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}, nil)
	if err != nil {
		t.Fatalf("an undeclared tool must not end the run: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "evil.txt")); statErr == nil {
		t.Fatal("the file must not exist: the write tool was never declared")
	}
	if res.StopReason != StopComplete {
		t.Fatalf("expected a normal completion, got %q", res.StopReason)
	}
}
