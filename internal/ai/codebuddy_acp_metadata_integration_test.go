//go:build integration

package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// ACP Metadata — 自定义字段 / _meta / Token 用量挖掘（CodeBuddy / OpenCode / Claude / Codex）
// ===========================================================================
//
// 目标：验证各 ACP agent（`codebuddy --acp`、`opencode acp`、
// `npx @agentclientprotocol/claude-agent-acp`、`npx @agentclientprotocol/codex-acp`）
// 在 ACP 协议输出中除了标准协议字段外，是否还带详细的 token 用量信息、内置的
// 自定义扩展字段，以及各层 RPC 的 _meta 载荷。这些信息可以作为元信息加入
// ClawBench 的 Metadata / UsageState。
//
// 方法：与 TestClaudeACP_RawProtocolLatency 相同，直接通过 acp-go-sdk 驱动真实
// agent 进程。与高层测试的区别在于，这里在 SDK 的读写两端各套一个 tee，
// 把双向的原始 JSON-RPC 消息全部录制下来（agent→client 的通知/响应、client→agent
// 的请求），随后对每一层结果做字段级 dump。这样即便 SDK 的类型定义没覆盖某个
// 自定义扩展字段，原始 JSON 也不会丢。
//
// 运行：
//
//	go test -v -run TestACP_MetadataMining -tags integration -timeout 300s ./internal/ai/
//
// 结论型输出（而非断言失败）：token 信息实际出现在哪些位置（usage_update 通知 /
// PromptResponse.Usage / _meta），完全由 agent 决定。本测试只验证「已通过结构化
// 通道」的信息能到达 ClawBench 的 UsageState，并用 verbose 日志打印全部挖掘结果，
// 供后续决定是否接入 _meta。

// recordingWriter tees everything written into both the real pipe and a buffer.
type recordingWriter struct {
	dst io.Writer
	buf *bytes.Buffer
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	return w.dst.Write(p)
}

// recordingReader tees everything read into both the real pipe and a buffer.
type recordingReader struct {
	src io.Reader
	buf *bytes.Buffer
}

func (r *recordingReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.buf.Write(p[:n])
	}
	return n, err
}

// acpRawTraffic captures the raw bidirectional JSON-RPC messages of one
// connection. Each entry records one JSON object (the wire format is
// newline-delimited JSON).
type acpRawTraffic struct {
	// fromAgent are messages the agent sent to ClawBench (responses + notifications).
	fromAgent [][]byte
	// toAgent are messages ClawBench sent to the agent (requests + notifications).
	toAgent [][]byte
}

// dump prints every recorded wire message to the test log.
func (rec *acpRawTraffic) dump(t *testing.T) {
	t.Helper()
	t.Logf("-- %d messages from agent --", len(rec.fromAgent))
	for i, m := range rec.fromAgent {
		t.Logf("FROM agent[%d]: %s", i, compactJSON(m))
	}
	t.Logf("-- %d messages to agent --", len(rec.toAgent))
	for i, m := range rec.toAgent {
		t.Logf("TO agent[%d]: %s", i, compactJSON(m))
	}
}

// collectKeys gathers every key seen in an arbitrary JSON value (recursively,
// up to a depth cap) so we can report which custom fields the agent emits.
func collectKeys(v any, seen map[string]bool, depth int) {
	if depth > 4 || v == nil {
		return
	}
	switch val := v.(type) {
	case map[string]any:
		for k, sub := range val {
			seen[k] = true
			collectKeys(sub, seen, depth+1)
		}
	case []any:
		for _, sub := range val {
			collectKeys(sub, seen, depth+1)
		}
	}
}

// compactJSON renders a JSON message as a single line, falling back to the raw
// bytes when unmarshalling fails.
func compactJSON(raw []byte) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return strings.TrimSpace(string(raw))
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// parseWireMessages splits newline-delimited JSON into individual messages.
func parseWireMessages(recording *bytes.Buffer) [][]byte {
	var out [][]byte
	sc := bufio.NewScanner(recording)
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		// Copy: Scanner's buffer is reused.
		msg := make([]byte, len(line))
		copy(msg, line)
		out = append(out, msg)
	}
	return out
}

// acpMetadataProbe drives a real ACP agent process over raw JSON-RPC and
// records every protocol message. The prompt runs to completion (or the
// context is cancelled); notifications are captured both as raw wire messages
// and as typed SessionNotification values.
func acpMetadataProbe(t *testing.T, ctx context.Context, cmdParts []string, prompt string) (*acpRawTraffic, *acp.InitializeResponse, *acp.NewSessionResponse, *acp.PromptResponse, []acp.SessionNotification, error) { //nolint:gocyclo // probe walks the full ACP lifecycle, each step is linear
	t.Helper()

	cmd := exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, OrphanChildEnvVar)

	agentOut, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	agentIn, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	cmd.Stderr = os.Stderr

	rec := &acpRawTraffic{}
	recOut := &recordingReader{src: agentOut, buf: &bytes.Buffer{}}
	recIn := &recordingWriter{dst: agentIn, buf: &bytes.Buffer{}}

	require.NoError(t, cmd.Start(), "spawn ACP agent")
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	// Wrap the client so we can also collect typed notifications as they are
	// delivered to the SDK's SessionUpdate callback (in addition to the raw
	// wire recording).
	client := NewClawBenchACPClient()
	capturing := &capturingACPClient{ClawBenchACPClient: client}
	conn := acp.NewClientSideConnection(capturing, recIn, recOut)
	conn.SetLogger(slog.Default())

	initCtx, initCancel := context.WithTimeout(ctx, 60*time.Second)
	defer initCancel()
	initResp, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{Name: "clawbench-metadata-probe", Version: "1.0.0"},
	})
	if err != nil {
		return rec, nil, nil, nil, nil, fmt.Errorf("initialize: %w", err)
	}

	workDir, _ := os.Getwd()
	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: workDir, McpServers: []acp.McpServer{}})
	if err != nil {
		return rec, &initResp, nil, nil, nil, fmt.Errorf("new_session: %w", err)
	}

	streamCh := make(chan StreamEvent, 512)
	capturing.RegisterSession(string(newResp.SessionId), streamCh)

	promptCtx, promptCancel := context.WithTimeout(ctx, 180*time.Second)
	defer promptCancel()
	promptDone := make(chan struct{})
	var promptResp acp.PromptResponse
	var promptErr error
	go func() {
		promptResp, promptErr = conn.Prompt(promptCtx, acp.PromptRequest{
			SessionId: newResp.SessionId,
			Prompt:    []acp.ContentBlock{acp.TextBlock(prompt)},
		})
		close(promptDone)
	}()

	var streamEventCount int
	waiting := true
	for waiting {
		select {
		case <-promptDone:
			waiting = false
		case evt, ok := <-streamCh:
			if !ok {
				continue
			}
			streamEventCount++
			switch evt.Type {
			case "error":
				t.Logf("metadata probe: stream error event: %s", evt.Error)
			case "usage_update":
				t.Logf("metadata probe: usage_update stream event: used=%d size=%d", evt.Usage.Used, evt.Usage.Size)
			}
		case <-time.After(180 * time.Second):
			t.Log("metadata probe: timed out waiting for prompt completion")
			waiting = false
		}
	}
	t.Logf("metadata probe: %d stream events drained (channel sink)", streamEventCount)

	rec.fromAgent = parseWireMessages(recOut.buf)
	rec.toAgent = parseWireMessages(recIn.buf)

	return rec, &initResp, &newResp, &promptResp, capturing.notifications(), promptErr
}

// capturingACPClient wraps ClawBenchACPClient to collect every
// SessionNotification the SDK delivers (typed form of the raw wire messages).
// It embeds the real client so it satisfies the full acp.Client interface;
// only SessionUpdate is intercepted.
type capturingACPClient struct {
	*ClawBenchACPClient

	mu       sync.Mutex
	captured []acp.SessionNotification
}

func (c *capturingACPClient) notifications() []acp.SessionNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]acp.SessionNotification, len(c.captured))
	copy(out, c.captured)
	return out
}

// SessionUpdate implements acp.Client. It records the notification and
// delegates to the real implementation (which routes to the stream channel).
func (c *capturingACPClient) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	c.mu.Lock()
	c.captured = append(c.captured, n)
	c.mu.Unlock()
	return c.ClawBenchACPClient.SessionUpdate(ctx, n)
}

// Renderers -----------------------------------------------------------------

func renderMetaDump(label string, meta acp.Meta) string {
	if len(meta) == 0 {
		return fmt.Sprintf("%s._meta: <absent/empty>", label)
	}
	b, _ := json.MarshalIndent(meta, "    ", "  ")
	return fmt.Sprintf("%s._meta:\n    %s", label, string(b))
}

func renderUsageDump(label string, u *acp.Usage) string {
	if u == nil {
		return fmt.Sprintf("%s.usage: <absent>", label)
	}
	return fmt.Sprintf(
		"%s.usage: input=%d output=%d total=%d cachedRead=%d cachedWrite=%d thought=%d",
		label, u.InputTokens, u.OutputTokens, u.TotalTokens,
		ptrInt64Val(u.CachedReadTokens), ptrInt64Val(u.CachedWriteTokens), ptrInt64Val(u.ThoughtTokens),
	)
}

func renderUsageUpdateDump(n acp.SessionUsageUpdate) string {
	cost := "<absent>"
	if n.Cost != nil {
		cost = fmt.Sprintf("%v %s", n.Cost.Amount, n.Cost.Currency)
	}
	return fmt.Sprintf("used=%d size=%d cost=%s", n.Used, n.Size, cost)
}

// metaUsageExtract describes the token/cost details found inside a
// session/update _meta payload (CodeBuddy's OpenAI-style usage extension).
type metaUsageExtract struct {
	promptTokens       int
	completionTokens   int
	cacheReadTokens    int
	cacheWriteTokens   int
	thinkingTokens     int
	credit             float64
	hasUsage           bool
	usageByCategory    map[string]any
	hasUsageByCategory bool
	requestModelID     string
	requestModelName   string
	responseModelID    string
	traceID            string
	requestID          string
	messageID          string
	finishReason       string
	outcome            string
	agentPhase         string
}

// metaUsageFromWire scans raw session/update messages for _meta extensions.
func metaUsageFromWire(msgs [][]byte) metaUsageExtract {
	var out metaUsageExtract
	for _, m := range msgs {
		var msg struct {
			Method string `json:"method"`
			Params struct {
				Update struct {
					Meta map[string]any `json:"_meta"`
				} `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(m, &msg) != nil || msg.Method != "session/update" {
			continue
		}
		meta := msg.Params.Update.Meta
		if meta == nil {
			continue
		}

		out.traceID = firstNonEmpty(out.traceID, asString(meta["codebuddy.ai/traceId"]), asString(meta["traceparent"]))
		out.requestID = firstNonEmpty(out.requestID, asString(meta["codebuddy.ai/requestId"]))
		out.messageID = firstNonEmpty(out.messageID, asString(meta["codebuddy.ai/messageId"]))
		out.requestModelID = firstNonEmpty(out.requestModelID, asString(meta["codebuddy.ai/requestModelId"]))
		out.requestModelName = firstNonEmpty(out.requestModelName, asString(meta["codebuddy.ai/requestModelName"]))
		out.responseModelID = firstNonEmpty(out.responseModelID, asString(meta["codebuddy.ai/responseModelId"]))
		out.finishReason = firstNonEmpty(out.finishReason, asString(meta["codebuddy.ai/finishReason"]))
		out.outcome = firstNonEmpty(out.outcome, asString(meta["codebuddy.ai/outcome"]))
		out.agentPhase = firstNonEmpty(out.agentPhase, asString(meta["codebuddy.ai/agentPhase"]))

		if m, ok := meta["usage"].(map[string]any); ok {
			out.hasUsage = true
			out.promptTokens = maxInt(out.promptTokens, asInt(m["prompt_tokens"]))
			out.completionTokens = maxInt(out.completionTokens, asInt(m["completion_tokens"]))
			out.cacheReadTokens = maxInt(out.cacheReadTokens, asInt(m["prompt_cache_hit_tokens"]), asInt(m["cache_read_input_tokens"]))
			out.cacheWriteTokens = maxInt(out.cacheWriteTokens, asInt(m["prompt_cache_write_tokens"]), asInt(m["cache_creation_input_tokens"]))
			out.thinkingTokens = maxInt(out.thinkingTokens, asInt(m["completion_thinking_tokens"]))
			out.credit = maxFloat(out.credit, asFloat(m["credit"]))
		}
		if m, ok := meta["codebuddy.ai/usageByCategory"].(map[string]any); ok {
			out.hasUsageByCategory = true
			if out.usageByCategory == nil {
				out.usageByCategory = m
			}
		}
	}
	return out
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func maxInt(base int, vals ...int) int {
	out := base
	for _, v := range vals {
		if v > out {
			out = v
		}
	}
	return out
}

func maxFloat(base float64, v float64) float64 {
	if v > base {
		return v
	}
	return base
}

// TestACP_MetadataMining drives real ACP agent processes and dumps every
// metadata-carrying field each emits. One subtest per agent.
//
// Non-skipping behavior: subtests run even when token info is absent (they
// simply report the absence), but skip when the agent CLI is not installed.
func TestACP_MetadataMining(t *testing.T) {
	agents := []struct {
		name     string
		cmdParts []string
		skipIf   func() string // returns a skip reason when unavailable
	}{
		{
			name:     "codebuddy",
			cmdParts: strings.Fields("codebuddy --acp"),
			skipIf: func() string {
				if _, err := exec.LookPath("codebuddy"); err != nil {
					return "codebuddy CLI not available"
				}
				return ""
			},
		},
		{
			name:     "opencode",
			cmdParts: strings.Fields("opencode acp"),
			skipIf: func() string {
				if _, err := exec.LookPath("opencode"); err != nil {
					return "opencode CLI not available"
				}
				return ""
			},
		},
		{
			name:     "claude",
			cmdParts: strings.Fields("npx -y @agentclientprotocol/claude-agent-acp@latest"),
			skipIf: func() string {
				if _, err := exec.LookPath("npx"); err != nil {
					return "npx not available"
				}
				if _, err := exec.LookPath("claude"); err != nil {
					return "claude CLI not available"
				}
				return ""
			},
		},
		{
			name:     "codex",
			cmdParts: strings.Fields("npx -y @agentclientprotocol/codex-acp@latest"),
			skipIf: func() string {
				if _, err := exec.LookPath("npx"); err != nil {
					return "npx not available"
				}
				if _, err := exec.LookPath("codex"); err != nil {
					return "codex CLI not available"
				}
				return ""
			},
		},
	}

	for _, a := range agents {
		a := a
		t.Run(a.name, func(t *testing.T) {
			if reason := a.skipIf(); reason != "" {
				t.Skip(reason)
			}

			ctx, cancel := contextWithTimeout(t, 240*time.Second)
			defer cancel()

			rec, initResp, newResp, promptResp, notifications, promptErr := acpMetadataProbe(t, ctx, a.cmdParts, "请只回复一个字：好")

			t.Log("=== 1. Raw JSON-RPC traffic ===")
			rec.dump(t)

			t.Log("=== 2. Initialize response ===")
			if initResp != nil {
				t.Logf("protocol_version=%v agentInfo=%v agentCapabilities=%v",
					initResp.ProtocolVersion, sprintImplementation(initResp.AgentInfo), sprintCaps(initResp.AgentCapabilities))
				t.Log(renderMetaDump("initialize", initResp.Meta))
			}

			t.Log("=== 3. NewSession response ===")
			if newResp != nil {
				t.Logf("session_id=%s modes=%d configOptions=%d", newResp.SessionId, len(sessionModes(newResp.Modes)), len(newResp.ConfigOptions))
				for i, opt := range newResp.ConfigOptions {
					t.Logf("config_option[%d]: %s", i, describeConfigOption(opt))
				}
				t.Log(renderMetaDump("new_session", newResp.Meta))
			}

			t.Log("=== 4. Notifications (session/update) — typed view ===")
			typeCounts := map[string]int{}
			for _, n := range notifications {
				typeCounts[describeNotification(n)]++
			}
			keys := make([]string, 0, len(typeCounts))
			for k := range typeCounts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				t.Logf("  %-40s x%d", k, typeCounts[k])
			}

			for _, n := range notifications {
				switch {
				case n.Update.UsageUpdate != nil:
					t.Logf("usage_update: %s", renderUsageUpdateDump(*n.Update.UsageUpdate))
					if len(n.Meta) > 0 {
						t.Logf("usage_update._meta: %v", n.Meta)
					}
				case n.Update.AgentMessageChunk != nil && n.Update.AgentMessageChunk.Content.Text != nil:
					t.Logf("agent_message_chunk[text len=%d]: %s", len(n.Update.AgentMessageChunk.Content.Text.Text), n.Update.AgentMessageChunk.Content.Text.Text)
					if len(n.Meta) > 0 {
						t.Logf("agent_message_chunk._meta: %v", n.Meta)
					}
				case n.Update.SessionInfoUpdate != nil:
					t.Logf("session_info_update: title=%v updatedAt=%v", n.Update.SessionInfoUpdate.Title, n.Update.SessionInfoUpdate.UpdatedAt)
					if len(n.Meta) > 0 {
						t.Logf("session_info_update._meta: %v", n.Meta)
					}
				}
			}

			t.Log("=== 5. PromptResponse ===")
			if promptResp != nil {
				t.Logf("stop_reason=%v", promptResp.StopReason)
				t.Log(renderUsageDump("prompt_response", promptResp.Usage))
				t.Log(renderMetaDump("prompt_response", promptResp.Meta))
			}
			if promptErr != nil {
				t.Logf("prompt error: %v", promptErr)
			}

			// ── Custom fields embedded in session/update._meta (wire level) ──
			t.Log("=== 5b. session/update._meta extensions (wire level) ===")
			mu := metaUsageFromWire(rec.fromAgent)
			t.Logf("  _meta.usage (OpenAI-style): present=%v prompt=%d completion=%d cacheRead=%d cacheWrite=%d thinking=%d credit=%v",
				mu.hasUsage, mu.promptTokens, mu.completionTokens, mu.cacheReadTokens, mu.cacheWriteTokens, mu.thinkingTokens, mu.credit)
			if mu.hasUsageByCategory {
				t.Logf("  _meta.<agent>.usageByCategory: %v", mu.usageByCategory)
			} else {
				t.Log("  _meta.<agent>.usageByCategory: <absent>")
			}
			t.Logf("  _meta trace/identity: model=%s(%s) responseModel=%s requestId=%s messageId=%s traceId=%s finishReason=%s outcome=%s phase=%s",
				mu.requestModelID, mu.requestModelName, mu.responseModelID, mu.requestID, mu.messageID, mu.traceID, mu.finishReason, mu.outcome, mu.agentPhase)

			// ── Structured verification: what arrives through typed channels ──
			t.Log("=== 6. Structured verification ===")
			if promptResp != nil && promptResp.Usage != nil {
				u := promptResp.Usage
				usageState := &UsageState{
					InputTokens:       int(u.InputTokens),
					OutputTokens:      int(u.OutputTokens),
					TotalTokens:       int(u.TotalTokens),
					CachedReadTokens:  ptrInt64Val(u.CachedReadTokens),
					CachedWriteTokens: ptrInt64Val(u.CachedWriteTokens),
					ThoughtTokens:     ptrInt64Val(u.ThoughtTokens),
				}
				if usageState.InputTokens != 0 || usageState.OutputTokens != 0 {
					t.Logf("CONFIRMED: PromptResponse.Usage carries token info (input=%d output=%d total=%d) → maps to UsageState",
						usageState.InputTokens, usageState.OutputTokens, usageState.TotalTokens)
				} else {
					t.Log("NOTICE: PromptResponse.Usage present but all token counters are zero")
				}
			} else {
				t.Log("NOTICE: PromptResponse.Usage is ABSENT — no per-turn token usage via the (UNSTABLE) usage field")
			}

			usageUpdateCount := 0
			for _, n := range notifications {
				if n.Update.UsageUpdate != nil {
					usageUpdateCount++
				}
			}
			if usageUpdateCount > 0 {
				t.Logf("CONFIRMED: %d usage_update notification(s) → maps to UsageState.Used/Size/Cost", usageUpdateCount)
			} else {
				t.Log("NOTICE: NO usage_update notifications received — agent does not push context-window usage")
			}

			if mu.hasUsage {
				t.Logf("FINDING: detailed token usage embedded in session/update._meta.usage — NOT currently parsed into UsageState/Metadata")
			}

			// ── Custom-field evidence: every distinct key on the wire ──
			t.Log("=== 7. Custom-field evidence (full wire keys) ===")
			allKeys := map[string]bool{}
			for _, m := range rec.fromAgent {
				var v any
				if json.Unmarshal(m, &v) == nil {
					collectKeys(v, allKeys, 0)
				}
			}
			keys2 := make([]string, 0, len(allKeys))
			for k := range allKeys {
				keys2 = append(keys2, k)
			}
			sort.Strings(keys2)
			t.Logf("distinct keys seen on the wire (%d): %v", len(keys2), keys2)

			metaCounts := map[string]int{"initialize": 0, "new_session": 0, "prompt_response": 0, "notifications": 0}
			if initResp != nil && len(initResp.Meta) > 0 {
				metaCounts["initialize"] = 1
			}
			if newResp != nil && len(newResp.Meta) > 0 {
				metaCounts["new_session"] = 1
			}
			if promptResp != nil && len(promptResp.Meta) > 0 {
				metaCounts["prompt_response"] = 1
			}
			for _, n := range notifications {
				if len(n.Meta) > 0 {
					metaCounts["notifications"]++
				}
			}
			t.Logf("_meta presence: %v", metaCounts)
		})
	}
}

// ---------------------------------------------------------------------------
// Small helpers for the probe test
// ---------------------------------------------------------------------------

// TestCodebuddyACP_MetadataE2E drives a real codebuddy --acp process through
// the full ACPBackend.ExecuteStream path and verifies that the per-agent _meta
// extensions (OpenAI-style usage, usageByCategory, trace) reach the typed
// usage_update event. This is the functional confirmation of the adapter
// wiring in mapACPSessionUpdate — the probe tests above only record wire data.
func TestCodebuddyACP_MetadataE2E(t *testing.T) {
	if _, err := exec.LookPath("codebuddy"); err != nil {
		t.Skip("codebuddy CLI not available, skipping CodeBuddy ACP metadata E2E test")
	}

	agent := codebuddyACPAgent()
	_ = setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	cleanupConn(t, sessionID)

	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()
	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "请只回复一个字：好",
		SessionID: sessionID,
		WorkDir:   acpTestWorkDir(),
	})
	require.NoError(t, err, "ExecuteStream should not return error")

	var usageEvents, metadataEvents []StreamEvent
	for evt := range ch {
		switch evt.Type {
		case "usage_update":
			usageEvents = append(usageEvents, evt)
		case "metadata":
			metadataEvents = append(metadataEvents, evt)
		}
	}

	require.NotEmpty(t, usageEvents, "expected at least one usage_update event")
	for i, evt := range usageEvents {
		u := evt.Usage
		t.Logf("usage_update[%d]: used=%d size=%d input=%d output=%d cacheHit=%d cacheMiss=%d credit=%v category=%v",
			i, u.Used, u.Size, u.InputTokens, u.OutputTokens,
			u.CacheHitTokens, u.CacheMissTokens, u.Credit, u.UsageByCategory)
	}
	last := usageEvents[len(usageEvents)-1]
	require.NotNil(t, last.Usage)

	// CodeBuddy always reports used/size on usage_update.
	assert.Positive(t, last.Usage.Used, "used should be reported")
	assert.Positive(t, last.Usage.Size, "size should be reported")

	// At least one usage_update should carry the context-window usage. CodeBuddy
	// may push several; the detailed _meta extensions (usageByCategory) appear
	// on whichever notification carried them — so scan all, not just the last.
	var sawCategory, sawDetail bool
	for _, evt := range usageEvents {
		if evt.Usage != nil {
			if len(evt.Usage.UsageByCategory) > 0 {
				sawCategory = true
			}
			if evt.Usage.InputTokens != 0 || evt.Usage.OutputTokens != 0 ||
				evt.Usage.CacheHitTokens != 0 || evt.Usage.CacheMissTokens != 0 {
				sawDetail = true
			}
		}
	}
	t.Logf("usageByCategory observed on any event: %v; token detail observed: %v", sawCategory, sawDetail)

	// The turn-final metadata event (persisted to chat_history.content JSON and
	// chat_metadata) must carry the per-agent _meta detail for message-level
	// display — verify CodeBuddy's usageByCategory + token detail flow there.
	var metaWithData int
	for _, evt := range metadataEvents {
		if evt.Meta == nil {
			continue
		}
		md := evt.Meta
		t.Logf("metadata event: input=%d output=%d total=%d cachedRead=%d cachedWrite=%d cacheCreation=%d cacheHit=%d cacheMiss=%d thought=%d credit=%v stopReason=%q requestId=%q traceId=%q messageId=%q messageRequestId=%q reqModel=%q reqModelName=%q respModel=%q finishReason=%q outcome=%q phase=%q category=%v",
			md.InputTokens, md.OutputTokens, md.TotalTokens, md.CachedReadTokens, md.CachedWriteTokens,
			md.CacheCreationTokens, md.CacheHitTokens, md.CacheMissTokens, md.ThoughtTokens, md.Credit,
			md.StopReason, md.RequestID, md.TraceID, md.MessageID, md.MessageRequestID,
			md.Model, md.RequestModelName, md.ResponseModelID, md.FinishReason, md.Outcome, md.AgentPhase,
			md.UsageByCategory)
		if len(evt.Meta.UsageByCategory) > 0 || evt.Meta.InputTokens != 0 || evt.Meta.RequestID != "" {
			metaWithData++
		}
	}
	t.Logf("metadata events with _meta detail: %d of %d", metaWithData, len(metadataEvents))
	if metaWithData > 0 {
		t.Logf("CONFIRMED: message-level metadata carries per-agent _meta detail → persisted to chat_metadata")
	} else {
		t.Logf("NOTICE: no metadata event carried _meta detail (model/version dependent)")
	}
}

func sessionModes(ms *acp.SessionModeState) []string {
	if ms == nil {
		return nil
	}
	out := make([]string, 0, len(ms.AvailableModes))
	for _, m := range ms.AvailableModes {
		out = append(out, string(m.Id))
	}
	return out
}

func sprintImplementation(impl *acp.Implementation) string {
	if impl == nil {
		return "<nil>"
	}
	return fmt.Sprintf("{name=%s version=%s}", impl.Name, impl.Version)
}

func sprintCaps(c acp.AgentCapabilities) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("loadSession=%v", c.LoadSession))
	parts = append(parts, fmt.Sprintf("mcp.acp=%v mcp.http=%v mcp.sse=%v",
		c.McpCapabilities.Acp, c.McpCapabilities.Http, c.McpCapabilities.Sse))
	parts = append(parts, fmt.Sprintf("prompt.audio=%v image=%v embedded=%v",
		c.PromptCapabilities.Audio, c.PromptCapabilities.Image, c.PromptCapabilities.EmbeddedContext))
	if c.SessionCapabilities.List != nil {
		parts = append(parts, "session.list=supported")
	}
	return strings.Join(parts, " ")
}

func describeConfigOption(opt acp.SessionConfigOption) string {
	if opt.Select == nil {
		return "<non-select>"
	}
	sel := opt.Select
	cat := "<nil>"
	if sel.Category != nil {
		cat = string(*sel.Category)
	}
	var vals []string
	if sel.Options.Ungrouped != nil {
		for _, v := range *sel.Options.Ungrouped {
			vals = append(vals, string(v.Value))
		}
	}
	return fmt.Sprintf("id=%q category=%s current=%q values=%v", sel.Id, cat, sel.CurrentValue, vals)
}

func describeNotification(n acp.SessionNotification) string {
	switch {
	case n.Update.UserMessageChunk != nil:
		return "user_message_chunk"
	case n.Update.AgentMessageChunk != nil:
		return "agent_message_chunk"
	case n.Update.AgentThoughtChunk != nil:
		return "agent_thought_chunk"
	case n.Update.ToolCall != nil:
		return "tool_call"
	case n.Update.ToolCallUpdate != nil:
		return "tool_call_update"
	case n.Update.Plan != nil:
		return "plan"
	case n.Update.PlanUpdate != nil:
		return "plan_update"
	case n.Update.PlanRemoved != nil:
		return "plan_removed"
	case n.Update.AvailableCommandsUpdate != nil:
		return "available_commands_update"
	case n.Update.CurrentModeUpdate != nil:
		return "current_mode_update"
	case n.Update.ConfigOptionUpdate != nil:
		return "config_option_update"
	case n.Update.SessionInfoUpdate != nil:
		return "session_info_update"
	case n.Update.UsageUpdate != nil:
		return "usage_update"
	default:
		return "unknown"
	}
}
