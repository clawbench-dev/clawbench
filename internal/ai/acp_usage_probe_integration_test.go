//go:build integration

package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===========================================================================
// ACP usage probe — does this agent report token usage at all?
// ===========================================================================
//
// PURPOSE: chat_metadata has rows for several ACP backends whose token columns
// are ALL ZERO (cursor, dsh, qoder, pi-over-acp). Before changing any parser we
// need to know WHICH of these is true:
//
//	A. the agent never sends usage_update and never sets PromptResponse.Usage
//	   → nothing to parse; the gap is upstream and unfixable here
//	B. the agent DOES send usage, but under a key our adapters do not read
//	   → a parser bug we can fix
//	C. the agent sends it only on PromptResponse (not as a notification)
//	   → a wiring bug in emitPromptResponseUsage
//
// This probe answers that by capturing the RAW wire, bypassing every adapter.
//
// RUN:
//
//	go test -tags integration -v -timeout 900s \
//	    -run 'TestACPUsageProbe' ./internal/ai/
//
// It spawns each agent's ACP server, does initialize → session/new →
// session/prompt, and prints every session/update notification plus the
// PromptResponse, with usage_update / _meta payloads called out explicitly.
func TestACPUsageProbe(t *testing.T) {
	agents := []struct {
		name    string
		command string
		args    []string
	}{
		// pi's ACP bridge: registered as "npx -y pi-acp@latest" in backends/pi.
		{"pi", "npx", []string{"-y", "pi-acp@latest"}},
		{"cursor", "cursor-agent", []string{"acp"}},
		{"dsh", "dsh", []string{"--profile", "acp"}},
		{"qoder", "qodercli", []string{"--acp"}},
		{"qodercn", "qoderclicn", []string{"--acp"}},
	}

	for _, a := range agents {
		a := a
		t.Run(a.name, func(t *testing.T) {
			probeOneAgent(t, a.name, a.command, a.args)
		})
	}
}

// probeOneAgent runs the full handshake against one agent and dumps the wire.
func probeOneAgent(t *testing.T, name, command string, args []string) {
	t.Helper()

	if _, err := exec.LookPath(command); err != nil {
		t.Skipf("%s not installed: %v", command, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	var writeMu sync.Mutex
	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = stdin.Write(append(b, '\n'))
		return err
	}

	// reader collects every inbound line and routes responses to waiters.
	type inbound struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	lines := make(chan inbound, 256)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
		for sc.Scan() {
			var in inbound
			if json.Unmarshal(sc.Bytes(), &in) == nil {
				lines <- in
			}
		}
		close(lines)
	}()

	// collect runs the reader loop, answering RPCs we do not care about with
	// null and recording every notification.
	var (
		notifications []string
		usageFrames   []string
		nextID        int64
		pending       = map[int64]chan inbound{}
		pendingMu     sync.Mutex
	)
	call := func(method string, params any) (json.RawMessage, error) {
		pendingMu.Lock()
		nextID++
		id := nextID
		ch := make(chan inbound, 1)
		pending[id] = ch
		pendingMu.Unlock()

		if err := send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			return nil, err
		}
		select {
		case in := <-ch:
			if len(in.Error) > 0 && string(in.Error) != "null" {
				return nil, fmt.Errorf("%s: %s", method, string(in.Error))
			}
			return in.Result, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// reader pump
	go func() {
		for in := range lines {
			if in.ID != nil && in.Method == "" {
				pendingMu.Lock()
				ch := pending[*in.ID]
				delete(pending, *in.ID)
				pendingMu.Unlock()
				if ch != nil {
					ch <- in
				}
				continue
			}
			raw, _ := json.Marshal(in)
			notifications = append(notifications, string(raw))
			if isUsageBearingFrame(raw) {
				usageFrames = append(usageFrames, string(raw))
			}
		}
	}()

	// ---- handshake ----
	if _, err := call("initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "clawbench-usage-probe", "version": "0"},
	}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	cwd := t.TempDir()
	sessRaw, err := call("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		t.Fatalf("session/new: %v", err)
	}
	var sess struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(sessRaw, &sess); err != nil || sess.SessionID == "" {
		t.Fatalf("session/new returned no sessionId: %s", string(sessRaw))
	}
	t.Logf("[%s] sessionId=%s", name, sess.SessionID)

	// ---- one prompt ----
	notifications = nil
	usageFrames = nil
	respRaw, err := call("session/prompt", map[string]any{
		"sessionId": sess.SessionID,
		"prompt": []any{
			map[string]any{"type": "text", "text": "Reply with exactly: ok"},
		},
	})
	if err != nil {
		t.Logf("[%s] session/prompt failed: %v", name, err)
	}

	// Give late notifications a moment to land.
	time.Sleep(2 * time.Second)

	// ---- report ----
	t.Logf("[%s] === PromptResponse ===", name)
	if len(respRaw) > 0 {
		t.Logf("[%s]   %s", name, truncateForProbe(string(respRaw), 1500))
	} else {
		t.Logf("[%s]   <none>", name)
	}

	t.Logf("[%s] === usage-bearing notifications (%d) ===", name, len(usageFrames))
	for i, f := range usageFrames {
		if i >= 5 {
			t.Logf("[%s]   ... %d more", name, len(usageFrames)-5)
			break
		}
		t.Logf("[%s]   %s", name, truncateForProbe(f, 1200))
	}

	t.Logf("[%s] === all notification methods (%d) ===", name, len(notifications))
	methods := map[string]int{}
	for _, n := range notifications {
		var in struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal([]byte(n), &in)
		methods[in.Method]++
	}
	for m, c := range methods {
		t.Logf("[%s]   %-40s x%d", name, m, c)
	}

	// The verdict this probe exists to produce.
	//
	// A usage_update carrying only used/size is NOT a billable usage report: it
	// describes the context window, which is what the composer ring renders. An
	// agent that sends only that (dsh, pi, and — with no frame at all — cursor,
	// qoder) leaves the ledger with zero tokens, and no parser change can fix
	// it because the numbers were never on the wire.
	hasBillable := false
	for _, f := range usageFrames {
		if usageFrameHasTokenCounters(f) {
			hasBillable = true
			break
		}
	}
	hasResponseUsage := len(respRaw) > 0 && strings.Contains(string(respRaw), "usage")

	switch {
	case hasBillable || hasResponseUsage:
		t.Logf("[%s] VERDICT: agent reports BILLABLE usage (parsable)", name)
	case len(usageFrames) > 0:
		t.Logf("[%s] VERDICT: agent reports only context occupancy (used/size), "+
			"NO token counters — ledger will stay zero and this is an upstream limitation", name)
	default:
		t.Logf("[%s] VERDICT: agent reports NO usage at all — upstream limitation", name)
	}
}

// isUsageBearingFrame reports whether a wire line mentions usage at all.
func isUsageBearingFrame(raw []byte) bool {
	s := string(raw)
	return strings.Contains(s, "usage_update") || strings.Contains(s, "\"usage\"")
}

// usageFrameHasTokenCounters reports whether a usage_update actually carries
// billable token counters.
//
// The ACP-standard usage_update only defines used/size (context occupancy).
// Token counters arrive either as extra fields on the update or inside
// update._meta, so both are inspected. A frame with just used/size is NOT
// billable.
func usageFrameHasTokenCounters(frame string) bool {
	var msg struct {
		Params struct {
			Update map[string]any `json:"update"`
		} `json:"params"`
	}
	if json.Unmarshal([]byte(frame), &msg) != nil {
		return false
	}
	update := msg.Params.Update
	if update == nil {
		return false
	}
	// Direct token fields on the update.
	for _, k := range []string{"inputTokens", "outputTokens", "totalTokens", "cost"} {
		if _, ok := update[k]; ok {
			return true
		}
	}
	// Nested under _meta (CodeBuddy/Claude style).
	if meta, ok := update["_meta"].(map[string]any); ok && len(meta) > 0 {
		blob, _ := json.Marshal(meta)
		for _, k := range []string{
			"prompt_tokens", "inputTokens", "completion_tokens", "outputTokens",
			"totalTokens", "token_count", "credit", "usage",
		} {
			if strings.Contains(string(blob), "\""+k+"\"") {
				return true
			}
		}
	}
	return false
}

// truncateForProbe shortens a wire line for readable test output.
func truncateForProbe(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
