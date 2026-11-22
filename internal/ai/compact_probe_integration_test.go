//go:build integration

package ai

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 压缩信号全后端探针（compact signal probe — every backend）
// ===========================================================================
//
// 目的：把"上下文被压缩了"这个事实的**发现能力**逐后端钉死。ClawBench 用
// `compact_detected` 内部事件驱动两件事：① 下一轮重注入系统提示
// （chat_request.go 的 ConsumeSessionCompacted）；② 将来的"压缩可见"UI。
// 目前只有三个后端有检测（CodeBuddy / Claude / Grok），其余后端即便真的
// 压缩了也无从得知。本探针用**真实 agent 进程**实测每个后端的触发形态。
//
// 探测方式（唯一的生产路径：ACP）：
//
//	直连 `session/new` 后发送 `/compact`，同时
//	① tee 原始 JSON-RPC wire（看 agent 到底发了什么帧），
//	② 走生产 acpStdoutFilter + ClawBenchACPClient，
//	   由 mapACPSessionUpdate 产出 `compact_detected`（看现有检测是否命中）。
//
// 每个后端记录：
//   - 是否在 available_commands 里广告了 compact 命令（决定 `/compact` 是否可用）；
//   - `/compact` 之后 `compact_detected` 命中次数与来源（acp_meta / acp_text）；
//   - 原始 wire 里含压缩标记的行（判断"信号存在但没被识别"）；
//   - 压缩前后的 usage（used/size，判断"是否真的压缩了"）。
//
// 运行（全部后端，耗时较长）：
//
//	go test -v -run 'TestCompactSignalProbe' -tags integration \
//	    -timeout 2400s ./internal/ai/
//
// 只跑某个后端：
//
//	go test -v -run 'TestCompactSignalProbe_ACP/codebuddy' -tags integration \
//	    -timeout 600s ./internal/ai/
//
// 结论型探针：观测到的事实写 t.Logf，硬断言只覆盖"探针本身能跑通"
// （进程起得来、session 建得起来）。检测能力的有无由汇总表说明。

// compactWireMarkers are the substrings that identify a compaction signal on the
// raw wire. They cover every shape ClawBench knows about plus the ones it does
// NOT yet recognise, so an unmatched-but-present signal shows up in the report
// as "signal exists, detection missing" rather than being invisible.
var compactWireMarkers = []string{
	"isCompactInternal",
	"compactType",
	"compact_boundary",
	"compact-cancelled",
	"compact-limit-reached",
	"Compacting",
	"compaction",
	"Compaction",
}

// compactWireNoise are substrings that make a line a FALSE positive: the
// agent's command catalogue lists a command named "compact", which has nothing
// to do with a compaction happening. Without this filter every
// available_commands_update frame would be reported as a compaction signal.
var compactWireNoise = []string{
	`"availableCommands"`,
	`"available_commands"`,
}

// compactProbeResult is one backend's outcome.
type compactProbeResult struct {
	ID string

	Skipped    string // non-empty ⇒ backend unavailable, no facts gathered
	PromptErr  string // `/compact` prompt returned an error
	StopReason string

	// CompactCommandAdvertised is true when the agent listed a `compact`
	// slash command in available_commands_update (ACP only).
	CompactCommandAdvertised bool

	// Detected counts `compact_detected` events produced by the PRODUCTION
	// mapper/parser. Sources lists their Content field.
	Detected int
	Sources  []string

	// RawCompactLines are the raw wire/stdout lines that mention a compaction
	// marker. Present-but-undetected shows up here.
	RawCompactLines []string

	// UsageBefore / UsageAfter are "used/size" snapshots around the command.
	UsageBefore string
	UsageAfter  string

	// EventTypes is the ordered list of StreamEvent types, for context.
	EventTypes []string

	// ReplyText is the assistant's own answer to `/compact`. Agents explain
	// their refusal here ("nothing to compact", "not supported"), which is the
	// difference between "signal missing" and "compaction never ran".
	ReplyText string
}

// logResult prints one backend's result in a stable, greppable shape.
func (r compactProbeResult) logResult(t *testing.T) {
	t.Helper()
	if r.Skipped != "" {
		t.Logf("PROBE %-12s SKIP  %s", r.ID, r.Skipped)
		return
	}
	t.Logf("PROBE %-12s detected=%d sources=%v cmd_advertised=%v usage=%s->%s stop=%s",
		r.ID, r.Detected, r.Sources, r.CompactCommandAdvertised,
		orDash(r.UsageBefore), orDash(r.UsageAfter), orDash(r.StopReason))
	if r.PromptErr != "" {
		t.Logf("PROBE %-12s   prompt error: %s", r.ID, r.PromptErr)
	}
	if r.ReplyText != "" {
		t.Logf("PROBE %-12s   reply: %s", r.ID, truncate(strings.TrimSpace(r.ReplyText), 300))
	}
	for _, line := range r.RawCompactLines {
		t.Logf("PROBE %-12s   wire: %s", r.ID, truncate(line, 400))
	}
	if r.Detected == 0 && len(r.RawCompactLines) > 0 {
		t.Logf("PROBE %-12s   ⚠ signal present on the wire but NOT detected by the production mapper", r.ID)
	}
	if r.Detected == 0 && len(r.RawCompactLines) == 0 {
		t.Logf("PROBE %-12s   no compaction signal seen (command unsupported, or agent compacts silently)", r.ID)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// compactMarkerLines filters raw lines down to those that mention a compaction
// marker, de-duplicated and capped so a chatty agent cannot flood the log.
func compactMarkerLines(lines [][]byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range lines {
		s := string(raw)
		noisy := false
		for _, n := range compactWireNoise {
			if strings.Contains(s, n) {
				noisy = true
				break
			}
		}
		if noisy {
			continue
		}
		hit := ""
		for _, m := range compactWireMarkers {
			if strings.Contains(s, m) {
				hit = m
				break
			}
		}
		if hit == "" {
			continue
		}
		c := "[" + hit + "] " + compactJSON(raw)
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

// usageSnapshot renders a UsageState as "used/size" (empty when unknown).
func usageSnapshot(u *UsageState) string {
	if u == nil || u.Size <= 0 {
		return ""
	}
	return strconv.Itoa(u.Used) + "/" + strconv.Itoa(u.Size)
}

// ---------------------------------------------------------------------------
// ACP probe
// ---------------------------------------------------------------------------

// compactProbeACP drives one ACP backend through a real `/compact`.
func compactProbeACP(t *testing.T, ctx context.Context, cfg acpTestConfig) compactProbeResult {
	t.Helper()
	res := compactProbeResult{ID: cfg.ID}

	agent := buildACPAgent(cfg)
	args := strings.Fields(cfg.AcpCommand)
	require.NotEmpty(t, args, "acp command must not be empty")

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	stdoutPipe, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	cmd.Stderr = nil // keep test output readable

	outBuf := &bytes.Buffer{}
	inBuf := &bytes.Buffer{}
	recOut := &recordingReader{src: stdoutPipe, buf: outBuf}
	recIn := &recordingWriter{dst: stdinPipe, buf: inBuf}
	filter := newACPStdoutFilter(recOut)
	t.Cleanup(filter.Close)

	client := NewClawBenchACPClient()
	conn := acp.NewClientSideConnection(client, recIn, filter)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	conn.SetUnhandledHandler(client.handleUnhandledNotification)

	require.NoError(t, cmd.Start(), "spawn %s", cfg.AcpCommand)
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	// Mirror the production back-reference so mapACPSessionUpdate sees the real
	// backend id (backend-gated branches are skipped without it — a harness gap,
	// not a product path).
	client.connRef = newACPConn(agent, cfg.ID+"-compact-probe")

	initCtx, initCancel := context.WithTimeout(ctx, 90*time.Second)
	defer initCancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{Name: "clawbench-compact-probe", Version: "1.0.0"},
	})
	if err != nil {
		res.PromptErr = "initialize: " + err.Error()
		return res
	}

	newCtx, newCancel := context.WithTimeout(ctx, 90*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: acpTestWorkDir(), McpServers: []acp.McpServer{}})
	if err != nil {
		res.PromptErr = "session/new: " + err.Error()
		return res
	}
	sessionID := string(newResp.SessionId)

	streamCh := make(chan StreamEvent, 16384)
	client.RegisterSession(sessionID, streamCh)

	// Warm the session so there is something to compact. Some agents refuse
	// `/compact` on an empty session ("nothing to compact"), and an empty
	// context also makes a successful compaction indistinguishable from a
	// no-op. This is deliberately short: the probe measures the SIGNAL, not the
	// compaction algorithm.
	warmCtx, warmCancel := context.WithTimeout(ctx, cfg.Timeout)
	warmResp, warmErr := conn.Prompt(warmCtx, acp.PromptRequest{
		SessionId: acp.SessionId(sessionID),
		Prompt:    []acp.ContentBlock{acp.TextBlock("Reply with the single word READY.")},
	})
	warmCancel()
	if warmErr != nil {
		res.PromptErr = "warm-up prompt: " + warmErr.Error()
	} else {
		res.StopReason = string(warmResp.StopReason)
	}
	drainProbeEvents(t, streamCh, 2*time.Second, &res, false)
	// Only the compact turn's own reply is interesting from here on.
	res.ReplyText = ""

	// Ask for a compaction through the production slash-command channel.
	promptCtx, promptCancel := context.WithTimeout(ctx, cfg.Timeout)
	compactResp, compactErr := conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: acp.SessionId(sessionID),
		Prompt:    []acp.ContentBlock{acp.TextBlock("/compact")},
	})
	promptCancel()
	if compactErr != nil {
		res.PromptErr = "compact prompt: " + compactErr.Error()
	} else {
		res.StopReason = string(compactResp.StopReason)
	}
	drainProbeEvents(t, streamCh, 3*time.Second, &res, true)

	// Let any trailing notifications land before reading the wire.
	time.Sleep(500 * time.Millisecond)
	// Snapshot BEFORE parsing: parseWireMessages runs a bufio.Scanner over the
	// buffer, which advances its read offset, so a dump taken afterwards would
	// be empty.
	rawWire := append([]byte(nil), outBuf.Bytes()...)
	res.RawCompactLines = compactMarkerLines(parseWireMessages(outBuf))
	dumpWireIfRequested(t, cfg.ID, rawWire)
	return res
}

// dumpWireIfRequested writes the full agent→client wire to a file when
// CLAWBENCH_COMPACT_PROBE_DUMP is set. Needed to answer "did the agent send a
// signal we simply do not recognise?" — the marker scan can only find shapes it
// already knows about.
func dumpWireIfRequested(t *testing.T, id string, raw []byte) {
	t.Helper()
	if os.Getenv("CLAWBENCH_COMPACT_PROBE_DUMP") == "" {
		return
	}
	path := "/tmp/compact-probe-" + id + ".wire.jsonl"
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Logf("PROBE %-12s   wire dump failed: %v", id, err)
		return
	}
	t.Logf("PROBE %-12s   wire dumped to %s (%d bytes)", id, path, len(raw))
}

// drainProbeEvents consumes everything currently buffered on ch, recording
// compact_detected hits and usage snapshots. When countUsage is true the latest
// usage is recorded as UsageAfter, otherwise as UsageBefore.
func drainProbeEvents(t *testing.T, ch <-chan StreamEvent, grace time.Duration, res *compactProbeResult, countUsage bool) {
	t.Helper()
	deadline := time.After(grace)
	for {
		select {
		case ev := <-ch:
			res.EventTypes = append(res.EventTypes, ev.Type)
			switch ev.Type {
			case "content":
				res.ReplyText += ev.Content
			case "compact_detected":
				res.Detected++
				res.Sources = append(res.Sources, ev.Content)
			case "usage_update":
				if countUsage {
					if s := usageSnapshot(ev.Usage); s != "" {
						res.UsageAfter = s
					}
				} else {
					if s := usageSnapshot(ev.Usage); s != "" {
						res.UsageBefore = s
					}
				}
			case "commands_update":
				for _, c := range ev.Commands {
					if strings.EqualFold(c.Name, "compact") {
						res.CompactCommandAdvertised = true
					}
				}
			}
		case <-deadline:
			return
		}
	}
}

// TestCompactSignalProbe_ACP runs the probe against every ACP backend.
func TestCompactSignalProbe_ACP(t *testing.T) {
	for _, cfg := range acpBackends {
		cfg := cfg
		t.Run(cfg.ID, func(t *testing.T) {
			requireACPBackendAvailable(t, cfg)
			ctx, cancel := contextWithTimeout(t, 3*cfg.Timeout+120*time.Second)
			defer cancel()

			res := compactProbeACP(t, ctx, cfg)
			res.logResult(t)
		})
	}
}

// ---------------------------------------------------------------------------
// Report aggregation
// ---------------------------------------------------------------------------

// TestCompactSignalProbe_Summary re-runs nothing; it exists so a single grep of
// the test log can be pasted into the issue as the coverage table. Kept as a
// separate entry point because Go gives no cross-test shared state.
func TestCompactSignalProbe_Summary(t *testing.T) {
	t.Skip("informational: the per-backend PROBE lines above ARE the summary table")
}

