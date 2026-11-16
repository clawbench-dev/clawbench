package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"clawbench/internal/model"
)

// TestRunBuiltinAgentTask_CallsToolsEndToEnd pins the property the meeting
// agent mode exists for: the agent can READ project files.
//
// The whole point of running minutes on the built-in agent instead of the
// summary path is that it can look things up. A test that only checked "it
// returns text" would pass for a summary call too, so this asserts the tool
// round actually happened and the file's content reached the model.
func TestRunBuiltinAgentTask_CallsToolsEndToEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("PROJECT-SAYS-HI"), 0o644); err != nil {
		t.Fatal(err)
	}

	var round atomic.Int32
	var secondRequest atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := round.Add(1)

		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			// The model must be offered the read tool, otherwise the agent has
			// no way to look anything up and this test would be vacuous.
			if !strings.Contains(string(body), `"name":"read"`) {
				t.Errorf("the read tool must be declared, body: %.400s", body)
			}
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"path\":\"marker.txt\"}"}}]}}]}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		secondRequest.Store(string(body))
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"## 概要\n\n纪要完成"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	withBuiltinAgent(t, srv.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	got, err := RunBuiltinAgentTask(context.Background(), BuiltinAgentTask{
		Prompt:       "写一份纪要",
		SystemPrompt: "你是会议记录员",
		WorkDir:      root,
		Policy:       model.ToolPolicy{Allow: []string{model.CapFSRead}},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(got, "纪要完成") {
		t.Fatalf("expected the model's answer, got %q", got)
	}
	if round.Load() < 2 {
		t.Fatalf("the agent must have made a second round after the tool call, got %d rounds", round.Load())
	}

	// The file's content must have reached the model as a tool result.
	second, _ := secondRequest.Load().(string)
	if !strings.Contains(second, "PROJECT-SAYS-HI") {
		t.Fatalf("the tool result must carry the file content, request: %.400s", second)
	}
}

// TestRunBuiltinAgentTask_ReadOnlyPolicyCannotWrite pins the tool grant.
//
// Minutes generation is transcription and lookup. A background job that can
// write files edits the project while nobody is watching, and nothing in the
// task needs it.
func TestRunBuiltinAgentTask_ReadOnlyPolicyCannotWrite(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// The write tool must never be declared under a read-only grant.
		if strings.Contains(string(body), `"name":"write"`) {
			t.Errorf("the write tool must not be declared, body: %.300s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	withBuiltinAgent(t, srv.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	if _, err := RunBuiltinAgentTask(context.Background(), BuiltinAgentTask{
		Prompt:  "x",
		WorkDir: root,
		Policy:  model.ToolPolicy{Allow: []string{model.CapFSRead}},
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
}

// withBuiltinAgent installs a built-in agent bound to a test endpoint.
func withBuiltinAgent(t *testing.T, baseURL string, policy model.ToolPolicy) {
	t.Helper()
	withAgentMap(t)

	provider := &model.Provider{
		ID: 1, Name: "test", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: baseURL, APIKey: "k", Models: []string{"test-model"}, DefaultModel: "test-model",
	}
	prevProvider := model.GetProvider(1)
	model.ReplaceProviders([]*model.Provider{provider})
	t.Cleanup(func() {
		if prevProvider != nil {
			model.ReplaceProviders([]*model.Provider{prevProvider})
			return
		}
		model.ReplaceProviders(nil)
	})

	pid := int64(1)
	model.Agents[BuiltinAgentID] = &model.Agent{
		ID:             BuiltinAgentID,
		Name:           "ClawBench Agent",
		Backend:        builtinAgentBackendID,
		ProviderID:     &pid,
		PreferredModel: "test-model",
		ToolPolicy:     policy,
	}
}
