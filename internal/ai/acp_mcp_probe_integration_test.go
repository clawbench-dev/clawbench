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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// ACP MCP 注入探针（全部 ACP 后端）
// ===========================================================================
//
// 目的：在实现「统一 MCP 配置」（issue #558）之前，用真实 agent 进程实测
// `session/new` 的 `mcpServers` 字段到底被谁真正消费。仓库现状是四处
// `McpServers: []acp.McpServer{}` 硬编码空切片（acp_conn_lifecycle.go:308/441/501、
// acp_pool.go:1614），因此这条链路从未被验证过。
//
// 每个后端观测四件事：
//
//	Q1 initialize 广告的 mcpCapabilities（http/sse/acp）。stdio 是规范强制要求，
//	   无需广告；http/sse 必须按此位过滤，否则会给不支持的 agent 传非法类型
//	   （codex-acp 对 sse/acp 直接 throw invalidRequest，是硬失败而非降级）。
//	Q2 session/new 携带 stdio MCP server 是否被接受（不报错）。
//	Q3 该 server 是否真的连上：模型能否调用它的工具。
//	Q4 工具调用的输出里是否出现探针标记 —— 这是「真的执行了」的唯一铁证。
//
// 对照组：同一后端先用空 mcpServers 建一个会话，确认探针工具在对照会话里
// 不可用。否则「调用成功」可能来自 agent 自己的配置文件（Claude 的 .mcp.json、
// Codex 的 config.toml），而不是我们注入的字段 —— 那会把 workaround 误判成
// 协议支持。
//
// 运行：
//
//	go test -tags integration -v -run 'TestACP_MCPProbe' -timeout 2400s ./internal/ai/
//
// 只跑指定后端：
//
//	MCP_PROBE_BACKENDS=claude,codex go test -tags integration -v \
//	    -run 'TestACP_MCPProbe' -timeout 900s ./internal/ai/
//
// 需要本机安装对应 CLI 且已登录；缺失的自动 skip。探针会真的向模型发一轮请求，
// 因此会产生 token 消耗。

// The marker and tool name carry a per-run nonce.
//
// Several agents cache MCP server schemas ON DISK keyed by server name (hermes:
// ~/.hermes/cache/mcp_schema_cache.json). With a fixed name, a second run's
// CONTROL session (empty mcpServers) picked the tool up from that cache and the
// result was reported as "AMBIGUOUS — the backend reads its own config", which
// is wrong: nothing was configured, the previous run had simply poisoned the
// cache. A unique name per run keeps the control honest.
var probeNonce = func() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%1e9)
}()

var (
	mcpProbeMarker   = "CLAWBENCH_MCP_PROBE_OK_" + probeNonce
	mcpProbeToolName = "clawbench_mcp_probe_ping_" + probeNonce
)

// TestACP_MCPProbe is the entry point.
func TestACP_MCPProbe(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available — the probe MCP server needs it")
	}
	serverScript, err := filepath.Abs(filepath.Join("testdata", "mcp_probe_server.mjs"))
	require.NoError(t, err, "resolve probe MCP server path")
	require.FileExists(t, serverScript, "probe MCP server fixture is missing")

	only := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("MCP_PROBE_BACKENDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			only[id] = true
		}
	}

	for _, b := range mcpProbeBackends {
		b := b
		if len(only) > 0 && !only[b.ID] {
			continue
		}
		t.Run(b.ID, func(t *testing.T) {
			if _, err := exec.LookPath(b.Args[0]); err != nil {
				t.Skipf("%s not installed (%s)", b.Args[0], err)
			}
			if b.CLI != "" {
				if _, err := exec.LookPath(b.CLI); err != nil {
					t.Skipf("%s CLI not installed — adapter cannot reach its engine", b.CLI)
				}
			}

			ctx, cancel := contextWithTimeout(t, 420*time.Second)
			defer cancel()

			probeOneBackendForMCP(t, ctx, b, nodePath, serverScript)
		})
	}
}

// mcpProbeObservation 汇总一个后端的探测结果。
type mcpProbeObservation struct {
	AdvertisedHTTP bool
	AdvertisedSSE  bool
	AdvertisedACP  bool
	// SessionAccepted is true when session/new with a stdio server succeeded.
	SessionAccepted bool
	// ControlSawTool records whether the tool appeared WITHOUT our injection.
	ControlSawTool bool
	ProbeSawTool   bool
	// MarkerSeen is the decisive signal: the tool actually ran.
	MarkerSeen bool
	// Inconclusive explains why this run cannot answer the question at all
	// (agent not signed in, backend process dead). Empty means the result is
	// trustworthy. Without it, "no marker" would be reported as "the agent
	// ignores mcpServers" — a wrong conclusion that would shape the design.
	Inconclusive string
}

func probeOneBackendForMCP(t *testing.T, ctx context.Context, b mcpProbeBackend, nodePath, serverScript string) {
	t.Helper()
	obs := &mcpProbeObservation{}

	// Each phase gets its OWN agent process.
	//
	// Sharing one process for the control and probe sessions was tried first and
	// failed on claude: the second session/new hung until the 120s deadline
	// (`-32603 context deadline exceeded`) because the adapter keeps per-process
	// session state and the first session was still alive. A fresh process also
	// matches production — ClawBench spawns one ACP process per chat session —
	// so the probe measures the same conditions the feature will run under.
	// Auto-approve permissions for the whole run. Without it the probe hangs on
	// any agent that gates MCP tool calls behind an approval prompt (observed:
	// qoder blocked until the prompt deadline), which would be misreported as
	// "the tool never ran".
	SetAutoApproveGetter(func(string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(string) bool { return false }) })

	t.Log("──────── PHASE 1: initialize + control session (mcpServers: []) ────────")
	controlSetup := startMCPProbe(t, ctx, b)
	obs.AdvertisedHTTP, obs.AdvertisedSSE, obs.AdvertisedACP = logMCPCapabilities(t, controlSetup.extProbeSetup)

	workDir := t.TempDir()
	controlID, controlErr := newProbeSession(t, ctx, controlSetup.extProbeSetup, workDir, []acp.McpServer{})
	if controlErr == nil {
		obs.ControlSawTool, _ = runMCPProbeTurn(t, ctx, controlSetup.extProbeSetup, controlID, "control")
	} else {
		obs.Inconclusive = classifyProbeBlock(controlErr)
		t.Logf("NOTE: control session could not be created (%s); result attribution is weaker this run", obs.Inconclusive)
	}
	controlSetup.kill()

	t.Log("──────── PHASE 2: fresh process + one stdio MCP server ────────")
	probeSetup := startMCPProbe(t, ctx, b)
	defer probeSetup.kill()

	server := probeStdioServer(nodePath, serverScript)
	probeID, sessionErr := newProbeSession(t, ctx, probeSetup.extProbeSetup, workDir, []acp.McpServer{server})
	if sessionErr != nil {
		t.Logf("Q2 RESULT[%s]: session/new FAILED", b.ID)
		// A sign-in gate fires BEFORE the MCP field is ever looked at, so it
		// says nothing about MCP support. Distinguish it from a real rejection.
		if obs.Inconclusive == "" {
			obs.Inconclusive = classifyProbeBlock(sessionErr)
		}
		dumpAgentStderr(t, probeSetup)
		obs.report(t, b)
		return
	}
	obs.SessionAccepted = true
	t.Logf("Q2 RESULT[%s]: session/new accepted the stdio MCP server", b.ID)

	// Optional warm-up: some agents attach MCP servers asynchronously AFTER
	// session/new returns, so a prompt sent immediately may run before the tool
	// list is complete. Set MCP_PROBE_WARMUP_SECONDS to give them time and see
	// whether the verdict changes — the difference between "never" and "late"
	// matters for the design (one needs a retry policy, the other doesn't).
	if warm := probeWarmupDuration(); warm > 0 {
		t.Logf("waiting %s for asynchronous MCP startup before the first prompt", warm)
		time.Sleep(warm)
	}

	// Two turns on the SAME session.
	//
	// MCP server startup is asynchronous in several agents (hermes spawns the
	// stdio server after the first prompt returns — its own log shows
	// "starting MCP server" in the same second the turn ended). A single turn
	// therefore cannot distinguish "not supported" from "not up yet", and the
	// first version of this probe reported hermes as IGNORED for exactly that
	// reason. The second turn gives a slow starter a fair chance.
	//
	// sawTool/marker are OR-ed: either turn proving the tool exists is enough.
	for attempt := 1; attempt <= 2; attempt++ {
		phase := fmt.Sprintf("probe#%d", attempt)
		sawTool, sawMarker, turnErr, turnInconclusive := runMCPProbeTurnDetailed(t, ctx, probeSetup.extProbeSetup, probeID, phase)

		obs.ProbeSawTool = obs.ProbeSawTool || sawTool
		obs.MarkerSeen = obs.MarkerSeen || sawMarker
		if obs.Inconclusive == "" {
			if turnErr != nil {
				obs.Inconclusive = classifyProbeBlock(turnErr)
			} else if !sawMarker {
				obs.Inconclusive = turnInconclusive
			}
		}
		if sawMarker {
			break
		}
		if attempt == 1 {
			t.Log("NOTE: first turn saw no marker; retrying once in case the MCP server was still starting")
			time.Sleep(5 * time.Second)
		}
	}

	if !obs.MarkerSeen {
		dumpAgentStderr(t, probeSetup)
	}
	obs.report(t, b)
}

// dumpAgentStderr prints the agent's stderr, which is where MCP startup failures
// surface. Truncated to keep the log readable.
func dumpAgentStderr(t *testing.T, setup *mcpProbeProc) {
	t.Helper()
	if setup.stderr == nil || setup.stderr.Len() == 0 {
		t.Log("agent stderr: (empty)")
		return
	}
	t.Logf("agent stderr (truncated): %s", truncate(setup.stderr.String(), 3000))
}

// probeStdioServer builds the ACP stdio MCP server descriptor.
//
// Args/Env are initialized to empty slices, never nil: acp-go-sdk marshals both
// without omitempty, so nil becomes JSON null and strict consumers (codex-acp's
// zod schema validates `args`/`env` as arrays) reject the whole request.
func probeStdioServer(nodePath, serverScript string) acp.McpServer {
	return acp.McpServer{
		Stdio: &acp.McpServerStdio{
			Name:    "clawbench-probe-" + probeNonce,
			Command: nodePath,
			Args:    []string{serverScript},
			Env: []acp.EnvVariable{
				{Name: "MCP_PROBE_MARKER", Value: mcpProbeMarker},
				{Name: "MCP_PROBE_TOOL", Value: mcpProbeToolName},
			},
		},
	}
}

// mcpProbeProc owns one spawned agent process plus its captured stderr.
//
// Kept separate from extProbeSetup (shared with other probes) so this file can
// own process lifetime: the control and probe phases each need their own
// process, and MCP startup failures only show up on stderr.
type mcpProbeProc struct {
	*extProbeSetup
	cmd     *exec.Cmd
	stderr  *lockedBuffer
	killed  bool
	killMux sync.Mutex
}

// kill terminates the agent process. Safe to call more than once.
func (p *mcpProbeProc) kill() {
	p.killMux.Lock()
	defer p.killMux.Unlock()
	if p.killed || p.cmd == nil || p.cmd.Process == nil {
		p.killed = true
		return
	}
	p.killed = true
	_ = p.cmd.Process.Kill()
	_, _ = p.cmd.Process.Wait()
}

// lockedBuffer is a goroutine-safe bytes.Buffer for capturing stderr.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// startMCPProbe spawns the agent and runs initialize. Mirrors startExtProbe
// (which is hardcoded to `codebuddy --acp`) but takes an arbitrary command.
func startMCPProbe(t *testing.T, ctx context.Context, b mcpProbeBackend) *mcpProbeProc {
	t.Helper()

	cmd := exec.CommandContext(ctx, b.Args[0], b.Args[1:]...)
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	stdoutPipe, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")

	// stderr is where an agent reports "failed to start MCP server"; without it
	// a rejected/ignored server looks identical to a working one.
	stderrBuf := &lockedBuffer{}
	cmd.Stderr = stderrBuf

	writer := &syncWriter{dst: stdinPipe, buf: &bytes.Buffer{}}
	reader := &demuxReader{src: stdoutPipe}

	client := NewClawBenchACPClient()
	conn := acp.NewClientSideConnection(client, writer, reader)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, cmd.Start(), "spawn %s", strings.Join(b.Args, " "))
	proc := &mcpProbeProc{cmd: cmd, stderr: stderrBuf}
	t.Cleanup(proc.kill)

	// connRef must be wired for the client's auto-approve branch: without it
	// IsAutoApprove() is never consulted and every permission request blocks.
	connRef := newACPConn(&model.Agent{ID: b.ID, Backend: b.ID}, "mcp-probe-"+b.ID)
	connRef.SetAutoApprove(true)
	client.connRef = connRef

	raw := &extProbeRawConn{pending: map[string]chan map[string]any{}}
	proc.extProbeSetup = &extProbeSetup{conn: conn, client: client, raw: raw, writer: writer, reader: reader}
	proc.pumpProbeResponses(ctx)

	initCtx, initCancel := context.WithTimeout(ctx, 120*time.Second)
	defer initCancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{Name: "clawbench-mcp-probe", Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	return proc
}

// logMCPCapabilities prints the agent's advertised MCP capabilities.
//
// Read from the RAW wire rather than the typed struct: findRawResponse only
// matches top-level result keys, and mcpCapabilities is nested under
// agentCapabilities. Scanning raw lines keeps the evidence independent of SDK
// version drift too.
func logMCPCapabilities(t *testing.T, setup *extProbeSetup) (http, sse, acpCap bool) {
	t.Helper()

	var initLine string
	sc := bufio.NewScanner(bytes.NewReader(setup.reader.snapshot()))
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' || !bytes.Contains(line, []byte("mcpCapabilities")) {
			continue
		}
		initLine = string(line)
	}
	if initLine == "" {
		t.Log("Q1 mcpCapabilities: NOT ADVERTISED (stdio is still mandatory per spec)")
		return false, false, false
	}
	t.Logf("Q1 mcpCapabilities (raw wire): %s", truncate(initLine, 1200))

	var env struct {
		Result struct {
			AgentCapabilities struct {
				McpCapabilities struct {
					HTTP bool `json:"http"`
					SSE  bool `json:"sse"`
					ACP  bool `json:"acp"`
				} `json:"mcpCapabilities"`
			} `json:"agentCapabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(initLine), &env); err != nil {
		t.Logf("Q1 parse error: %v", err)
		return false, false, false
	}
	caps := env.Result.AgentCapabilities.McpCapabilities
	t.Logf("Q1 parsed: http=%v sse=%v acp=%v (stdio is mandatory per spec, never advertised)",
		caps.HTTP, caps.SSE, caps.ACP)
	return caps.HTTP, caps.SSE, caps.ACP
}

// newProbeSession opens a session on the probe connection. Returns ok=false when
// the agent refuses (some agents allow only one session per process).
//
// workDir must be a scratch directory, NOT the repo. Several agents persist
// project-scoped state next to the session cwd: qoder wrote
// `<cwd>/.qoder/settings.local.json` containing allow-rules for the probe MCP
// server, and zcode created `<cwd>/.zcode/acp/sandbox.json`. Pointing cwd at
// internal/ai/ therefore dirtied the working tree on every run. A temp dir also
// strengthens the control group: no project-level MCP config can exist there.
func newProbeSession(t *testing.T, ctx context.Context, setup *extProbeSetup, workDir string, servers []acp.McpServer) (string, error) {
	t.Helper()

	newCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	resp, err := setup.conn.NewSession(newCtx, acp.NewSessionRequest{
		Cwd:        workDir,
		McpServers: servers,
	})
	if err != nil {
		t.Logf("session/new error: %v", err)
		return "", err
	}
	return string(resp.SessionId), nil
}

// classifyProbeBlock turns an agent error into "why this run proves nothing".
//
// The distinction matters for the design: a sign-in gate or a dead backend
// process says nothing about whether the agent consumes mcpServers, while a
// clean "no such tool" from a signed-in agent does. Reporting the first as
// "IGNORED" would put a false negative in the compatibility table.
func classifyProbeBlock(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "authentication required"),
		strings.Contains(msg, "no auth method"),
		strings.Contains(msg, "-32000"):
		return "agent not signed in"
	case strings.Contains(msg, "backend dead"),
		strings.Contains(msg, "reader exited"),
		strings.Contains(msg, "backend reader"):
		return "agent backend process died"
	case strings.Contains(msg, "context deadline exceeded"):
		return "timed out (agent too slow or hung)"
	}
	return "agent error: " + truncate(err.Error(), 200)
}

// runMCPProbeTurn drives one turn and classifies what came back.
//
// The prompt asks for the tool's raw output verbatim, but the marker is ALSO
// searched inside tool_result events: the tool output is the authoritative copy
// and does not depend on the model echoing it.
func runMCPProbeTurn(t *testing.T, ctx context.Context, setup *extProbeSetup, sessionID, phase string) (sawTool, sawMarker bool) {
	t.Helper()
	sawTool, sawMarker, _, _ = runMCPProbeTurnDetailed(t, ctx, setup, sessionID, phase)
	return sawTool, sawMarker
}

// runMCPProbeTurnDetailed is runMCPProbeTurn plus the prompt error, so the
// caller can tell "the agent refused the turn" (not signed in) from "the agent
// answered but the tool was absent" (a real MCP verdict).
func runMCPProbeTurnDetailed(t *testing.T, ctx context.Context, setup *extProbeSetup, sessionID, phase string) (sawTool, sawMarker bool, promptErr error, inconclusive string) {
	t.Helper()

	ch := make(chan StreamEvent, 16384)
	setup.client.RegisterSession(sessionID, ch)

	// Drain in the background; the channel must never fill or the notification
	// goroutine stalls and the turn hangs.
	//
	// The drain is stopped with its OWN context, never the parent one: the
	// parent lives for the whole subtest, so waiting on it after the prompt
	// returns blocks until the outer deadline (observed: a 420s hang that
	// surfaced as "spawn ... context deadline exceeded" on the NEXT phase).
	drainCtx, stopDrain := context.WithCancel(ctx)
	defer stopDrain()

	var mu sync.Mutex
	var events []StreamEvent
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				mu.Lock()
				events = append(events, ev)
				mu.Unlock()
			case <-drainCtx.Done():
				return
			}
		}
	}()

	prompt := fmt.Sprintf(
		"Call the tool named %q exactly once. It takes no arguments. "+
			"Then reply with its output verbatim, character for character, and nothing else. "+
			"If the tool does not exist, reply exactly: NO_SUCH_TOOL",
		mcpProbeToolName,
	)

	promptCtx, cancel := context.WithTimeout(ctx, 240*time.Second)

	t.Logf("[%s] sending prompt (session=%s)", phase, sessionID)
	resp, err := setup.conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: acp.SessionId(sessionID),
		Prompt:    []acp.ContentBlock{acp.TextBlock(prompt)},
	})
	cancel()
	promptErr = err
	if err != nil {
		t.Logf("[%s] prompt error: %v", phase, err)
	} else {
		t.Logf("[%s] prompt finished, stopReason=%q", phase, resp.StopReason)
	}

	// Give the last notifications a moment to land before snapshotting, then
	// stop the drain with its own context (see the note where it is created).
	time.Sleep(1500 * time.Millisecond)
	stopDrain()
	<-drainDone

	mu.Lock()
	snapshot := append([]StreamEvent(nil), events...)
	mu.Unlock()

	var (
		toolNames []string
		content   strings.Builder
	)
	for _, ev := range snapshot {
		switch ev.Type {
		case "tool_use", "tool_result":
			if ev.Tool == nil {
				continue
			}
			toolNames = append(toolNames, ev.Tool.Name)
			if isMCPProbeToolName(ev.Tool.Name) {
				sawTool = true
			}
			if strings.Contains(ev.Tool.Output, mcpProbeMarker) {
				sawMarker = true
			}
		case "content":
			content.WriteString(ev.Content)
		}
	}

	text := content.String()
	// The assistant mentioning the tool NAME is not evidence it exists — a model
	// that could not find it will happily say so by name. Only the marker (or a
	// tool event whose Name carries the tool) counts.
	if strings.Contains(text, mcpProbeMarker) {
		sawMarker = true
	}

	t.Logf("[%s] events=%d tools=%v", phase, len(snapshot), sortedUnique(toolNames))
	t.Logf("[%s] assistant text (truncated): %s", phase, truncate(text, 600))
	t.Logf("[%s] sawTool=%v sawMarker=%v", phase, sawTool, sawMarker)

	// A turn that produced neither text nor tool events proves nothing about
	// MCP: the model never ran. Reporting it as "tool absent" would be a false
	// negative (observed: hermes replied with a provider-outage notice).
	if reason := classifyEmptyTurn(text, snapshot); reason != "" {
		t.Logf("[%s] INCONCLUSIVE: %s", phase, reason)
		inconclusive = reason
	}

	if strings.Contains(text, "NO_SUCH_TOOL") {
		t.Logf("[%s] the model explicitly reported NO_SUCH_TOOL — the server was not attached", phase)
	}

	return sawTool, sawMarker, promptErr, inconclusive
}

func (o *mcpProbeObservation) report(t *testing.T, b mcpProbeBackend) {
	t.Helper()
	t.Logf("════ SUMMARY[%s] ════", b.ID)
	t.Logf("  advertised:   http=%v sse=%v acp=%v", o.AdvertisedHTTP, o.AdvertisedSSE, o.AdvertisedACP)
	t.Logf("  session/new:  accepted=%v", o.SessionAccepted)
	t.Logf("  control turn: sawTool=%v (must be false — otherwise the tool came from the agent's own config)", o.ControlSawTool)
	t.Logf("  probe turn:   sawTool=%v markerSeen=%v", o.ProbeSawTool, o.MarkerSeen)

	// Inconclusive takes precedence over every other verdict. A sign-in gate or a
	// dead backend means the mcpServers field was never evaluated, so any
	// "IGNORED"/"REJECTED" label below would be a false negative that ends up in
	// the compatibility table the feature gets designed around.
	if o.Inconclusive != "" && !o.MarkerSeen {
		t.Logf("  VERDICT: INCONCLUSIVE — %s. Re-run after fixing this to get a real answer.", o.Inconclusive)
		return
	}

	switch {
	case o.MarkerSeen && !o.ControlSawTool:
		t.Logf("  VERDICT: OK — the mcpServers field is honored end-to-end")
	case o.MarkerSeen && o.ControlSawTool:
		t.Logf("  VERDICT: AMBIGUOUS — marker seen, but the CONTROL session also had the tool; " +
			"this backend reads its own config file, so the result cannot be attributed to mcpServers")
	case o.ProbeSawTool && !o.ControlSawTool:
		t.Logf("  VERDICT: ATTACHED — the tool was registered by our server (absent in the " +
			"control session) but the model never called it, so the marker is unproven. " +
			"Attachment is confirmed; execution is not.")
	case o.ProbeSawTool:
		t.Logf("  VERDICT: PARTIAL — tool appeared in both sessions; cannot attribute it to mcpServers")
	case o.SessionAccepted:
		t.Logf("  VERDICT: IGNORED — field accepted but the tool never existed")
	default:
		t.Logf("  VERDICT: REJECTED — session/new refused the server")
	}
}

// sortedUnique returns the sorted, de-duplicated input. Inline rather than a
// shared helper: the probe is the only caller.
func sortedUnique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// probeWarmupDuration reads MCP_PROBE_WARMUP_SECONDS (0 = no wait).
func probeWarmupDuration() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MCP_PROBE_WARMUP_SECONDS"))
	if raw == "" {
		return 0
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// classifyEmptyTurn reports why a turn that produced no marker proves nothing.
//
// Two shapes must be told apart:
//   - the model answered and said the tool is missing → a REAL negative, which
//     is the answer the probe is looking for;
//   - the model never answered (provider outage, empty turn, auth wall) → the
//     mcpServers field was never exercised, so labelling it "IGNORED" would put
//     a false negative into the compatibility table.
func classifyEmptyTurn(text string, events []StreamEvent) string {
	if strings.Contains(text, mcpProbeMarker) || strings.Contains(text, "NO_SUCH_TOOL") {
		return ""
	}
	// Some agents answer in a language of their own ("didn't answer after 3
	// attempts", "temporarily unavailable"); treat any non-empty prose that is
	// not a tool verdict as a provider-level failure.
	if strings.TrimSpace(text) != "" {
		return "the model produced prose but never addressed the tool: " + truncate(strings.TrimSpace(text), 160)
	}
	for _, ev := range events {
		if ev.Type == "error" {
			return "the agent reported an error: " + truncate(ev.Error, 160)
		}
	}
	return "the turn produced no assistant output at all"
}

// isMCPProbeToolName reports whether a tool event's Name is the probe tool
// itself, rather than some other tool that merely mentions it.
//
// Three false positives were observed before this guard:
//   - ToolSearch / DeferExecuteTool carry the sought name as an ARGUMENT;
//   - Hermes names the search tool after its query, producing the title
//     "tool_search: Searching tools · clawbench_mcp_probe_ping_…";
//   - a model that cannot find the tool still says its name in prose.
//
// The shapes that DO mean "the tool exists" are:
//   - the bare tool name (some agents);
//   - the canonical `mcp__<server>__<tool>` form (claude, hermes, opencode…);
//   - `<tool> (<server> MCP Server)` — Qoder's label.
//
// All three start with either the tool name or the `mcp__` namespace, so those
// two prefixes are the whole rule. Anything else (a search title, a prefix like
// "tool_call:") is not the call itself.
func isMCPProbeToolName(name string) bool {
	return strings.HasPrefix(name, mcpProbeToolName) ||
		(strings.HasPrefix(name, "mcp__") && strings.Contains(name, mcpProbeToolName))
}
