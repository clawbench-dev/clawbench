//go:build integration

package ai

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// ACP MCP 补充探针：load/resume 路径 + HTTP/SSE 传输
// ===========================================================================
//
// 主探针（acp_mcp_probe_integration_test.go）只验证了 `session/new` + stdio。
// 但生产环境里另外两条路径同样会注入 mcpServers，而且它们才是常态：
//
//	acp_conn_lifecycle.go:441  session/load    （显式 acp-load / 崩溃恢复回退）
//	acp_conn_lifecycle.go:501  session/resume  （ensureAliveWithSession 主力路径）
//	acp_pool.go:1614           SyncLoadSession （显式回放）
//
// 若某个后端只在 `new` 时读 mcpServers、在 resume 时忽略，症状就是
// 「新会话能用 MCP，进程重启后工具消失」—— 这类「用着用着功能没了」的缺陷
// 在本仓已有多次先例，必须在开工前测掉。
//
// 另外 http/sse 传输完全没测。远程 MCP 基本都是 HTTP/SSE 型，而各后端的能力位
// 差异很大（codex 广告 http 但不广告 sse，且对 sse 直接报错），不实测就实现，
// 很可能做出「面板能填 HTTP 地址但只有 claude 能用」。
//
// 运行：
//
//	go test -tags integration -v -run 'TestACP_MCPProbeResume' -timeout 1800s ./internal/ai/
//	go test -tags integration -v -run 'TestACP_MCPProbeHTTP'   -timeout 1800s ./internal/ai/
//
// 两个探针都复用 MCP_PROBE_BACKENDS 过滤。

// resumeProbeResult 记录 load/resume 路径的观测结果。
type resumeProbeResult struct {
	// NewMarkerSeen: the session created with MCP had the tool (baseline).
	NewMarkerSeen bool
	// ResumeSupported: the agent implements session/resume at all.
	ResumeSupported bool
	ResumeMarker    bool
	LoadSupported   bool
	LoadMarker      bool
	// Notes explains why a path could not be exercised.
	Notes []string
}

// TestACP_MCPProbeResume checks whether mcpServers survives session resume/load.
//
// Method: create a session WITH the probe server, prove the tool works, then —
// in a FRESH process — resume that session ID with the same mcpServers and ask
// the model to call the tool again. A fresh process is required because an
// in-process resume would hit the adapter's own session cache and prove nothing
// about what the request carried.
func TestACP_MCPProbeResume(t *testing.T) {
	nodePath, serverScript, backendList := probePrereqs(t)

	for _, b := range backendList {
		b := b
		t.Run(b.ID, func(t *testing.T) {
			skipIfBackendMissing(t, b)

			ctx, cancel := contextWithTimeout(t, 480*time.Second)
			defer cancel()

			SetAutoApproveGetter(func(string) bool { return true })
			t.Cleanup(func() { SetAutoApproveGetter(func(string) bool { return false }) })

			workDir := t.TempDir()
			server := probeStdioServer(nodePath, serverScript)
			res := &resumeProbeResult{}

			// ── Step 1: create the session with MCP, prove the tool works ──
			t.Log("──── STEP 1: session/new with MCP (baseline) ────")
			proc := startMCPProbe(t, ctx, b)
			sid, err := newProbeSession(t, ctx, proc.extProbeSetup, workDir, []acp.McpServer{server})
			if err != nil {
				t.Logf("session/new failed: %v — cannot probe resume", err)
				proc.kill()
				return
			}
			if warm := probeWarmupDuration(); warm > 0 {
				time.Sleep(warm)
			}
			_, res.NewMarkerSeen = probeTurnWithRetry(t, ctx, proc.extProbeSetup, sid, "new")
			t.Logf("baseline markerSeen=%v (if false, a resume result below proves nothing)", res.NewMarkerSeen)
			// Release the session cleanly BEFORE killing the process: a SIGKILL
			// leaves the agent's per-thread writer lock behind, and the resume
			// step below would then fail with "already has an active writer"
			// (observed on codex) — which looks like "no resume support".
			proc.closeSession(ctx, sid)
			if proc.closeErr != nil {
				t.Logf("session/close error: %v", proc.closeErr)
			} else {
				t.Log("session/close ok")
			}
			proc.kill()

			if !res.NewMarkerSeen {
				t.Log("SKIP resume/load: the baseline session could not call the tool, so " +
					"a negative resume result would be uninterpretable")
				return
			}

			// ── Step 2: resume the same session in a fresh process ─────────
			t.Log("──── STEP 2: session/resume in a fresh process ────")
			resumeProc := startMCPProbe(t, ctx, b)
			res.ResumeSupported, res.ResumeMarker = probeResumePath(t, ctx, resumeProc, sid, workDir, server, res)
			resumeProc.closeSession(ctx, sid)
			resumeProc.kill()

			// ── Step 3: load the same session in a fresh process ───────────
			t.Log("──── STEP 3: session/load in a fresh process ────")
			loadProc := startMCPProbe(t, ctx, b)
			res.LoadSupported, res.LoadMarker = probeLoadPath(t, ctx, loadProc, sid, workDir, server, res)
			loadProc.closeSession(ctx, sid)
			loadProc.kill()

			res.report(t, b)
		})
	}
}

// probeResumePath calls session/resume with the probe server and runs a turn.
func probeResumePath(t *testing.T, ctx context.Context, proc *mcpProbeProc, sid, workDir string, server acp.McpServer, res *resumeProbeResult) (supported, marker bool) {
	t.Helper()

	callCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	_, err := resumeWithRetry(callCtx, proc.conn, sid, workDir, server)
	if err != nil {
		if isMethodNotFound(err) {
			res.Notes = append(res.Notes, "session/resume: method not found")
			t.Logf("session/resume NOT IMPLEMENTED: %v", err)
			return false, false
		}
		res.Notes = append(res.Notes, "session/resume: "+classifyProbeBlock(err))
		t.Logf("session/resume error: %v", err)
		return false, false
	}
	t.Log("session/resume accepted")

	if warm := probeWarmupDuration(); warm > 0 {
		time.Sleep(warm)
	}
	_, marker = probeTurnWithRetry(t, ctx, proc.extProbeSetup, sid, "resume")
	t.Logf("after resume: markerSeen=%v", marker)
	return true, marker
}

// probeLoadPath calls session/load with the probe server and runs a turn.
func probeLoadPath(t *testing.T, ctx context.Context, proc *mcpProbeProc, sid, workDir string, server acp.McpServer, res *resumeProbeResult) (supported, marker bool) {
	t.Helper()

	callCtx, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()

	_, err := loadWithRetry(callCtx, proc.conn, sid, workDir, server)
	if err != nil {
		if isMethodNotFound(err) {
			res.Notes = append(res.Notes, "session/load: method not found")
			t.Logf("session/load NOT IMPLEMENTED: %v", err)
			return false, false
		}
		res.Notes = append(res.Notes, "session/load: "+classifyProbeBlock(err))
		t.Logf("session/load error: %v", err)
		return false, false
	}
	t.Log("session/load accepted")

	if warm := probeWarmupDuration(); warm > 0 {
		time.Sleep(warm)
	}
	_, marker = probeTurnWithRetry(t, ctx, proc.extProbeSetup, sid, "load")
	t.Logf("after load: markerSeen=%v", marker)
	return true, marker
}

func (r *resumeProbeResult) report(t *testing.T, b mcpProbeBackend) {
	t.Helper()
	t.Logf("════ RESUME SUMMARY[%s] ════", b.ID)
	t.Logf("  baseline (session/new): marker=%v", r.NewMarkerSeen)
	t.Logf("  session/resume: supported=%v marker=%v", r.ResumeSupported, r.ResumeMarker)
	t.Logf("  session/load:   supported=%v marker=%v", r.LoadSupported, r.LoadMarker)

	switch {
	case r.ResumeMarker || r.LoadMarker:
		t.Logf("  VERDICT: OK — mcpServers survives the recovery path")
	case !r.ResumeSupported && !r.LoadSupported:
		t.Logf("  VERDICT: N/A — this agent implements neither session/resume nor session/load")
	default:
		t.Logf("  VERDICT: DROPPED — the recovery path accepted mcpServers but the tool " +
			"was gone. A user would see MCP tools disappear after a process restart.")
	}
	for _, n := range r.Notes {
		t.Logf("  note: %s", n)
	}
}

// ===========================================================================
// HTTP / SSE 传输
// ===========================================================================

// TestACP_MCPProbeHTTP checks whether http and sse MCP servers work, and whether
// the agent's advertised mcpCapabilities actually predict the outcome.
//
// Each transport is attempted only when the agent advertises it. That is not
// just politeness: codex-acp throws invalidRequest for an sse server, so sending
// one to a backend that does not advertise sse would fail the whole session/new
// and tell us nothing about the http case.
func TestACP_MCPProbeHTTP(t *testing.T) {
	nodePath, serverScript, backendList := probePrereqs(t)

	for _, b := range backendList {
		b := b
		t.Run(b.ID, func(t *testing.T) {
			skipIfBackendMissing(t, b)

			ctx, cancel := contextWithTimeout(t, 480*time.Second)
			defer cancel()

			SetAutoApproveGetter(func(string) bool { return true })
			t.Cleanup(func() { SetAutoApproveGetter(func(string) bool { return false }) })

			workDir := t.TempDir()

			// Capabilities come from the control process (empty mcpServers).
			capProc := startMCPProbe(t, ctx, b)
			httpCap, sseCap, _ := logMCPCapabilities(t, capProc.extProbeSetup)
			capProc.kill()

			for _, transport := range []string{"http", "sse"} {
				advertised := httpCap
				if transport == "sse" {
					advertised = sseCap
				}
				if !advertised {
					t.Logf("SKIP %s: the agent does not advertise mcpCapabilities.%s", transport, transport)
					continue
				}

				t.Logf("──────── %s transport (advertised) ────────", strings.ToUpper(transport))
				runHTTPTransportProbe(t, ctx, b, nodePath, serverScript, workDir, transport)
			}

			if !httpCap && !sseCap {
				t.Logf("VERDICT[%s]: N/A — the agent advertises neither http nor sse", b.ID)
			}
		})
	}
}

// runHTTPTransportProbe starts the fixture over HTTP, builds the matching ACP
// descriptor, and runs one turn.
func runHTTPTransportProbe(t *testing.T, ctx context.Context, b mcpProbeBackend, nodePath, serverScript, workDir, transport string) {
	t.Helper()

	srv, err := startProbeHTTPServer(t, ctx, nodePath, serverScript)
	if err != nil {
		t.Logf("%s: could not start the fixture HTTP server: %v", transport, err)
		return
	}
	defer srv.stop()

	var server acp.McpServer
	url := fmt.Sprintf("http://127.0.0.1:%d", srv.port)
	headers := []acp.HttpHeader{
		{Name: "X-Clawbench-Probe", Value: transport},
	}
	if transport == "http" {
		server = acp.McpServer{
			Http: &acp.McpServerHttpInline{
				Name:    "clawbench-probe-" + probeNonce,
				Type:    "http",
				Url:     url + "/mcp",
				Headers: headers,
			},
		}
	} else {
		server = acp.McpServer{
			Sse: &acp.McpServerSseInline{
				Name:    "clawbench-probe-" + probeNonce,
				Type:    "sse",
				Url:     url + "/sse",
				Headers: headers,
			},
		}
	}

	proc := startMCPProbe(t, ctx, b)
	defer proc.kill()

	sid, err := newProbeSession(t, ctx, proc.extProbeSetup, workDir, []acp.McpServer{server})
	if err != nil {
		t.Logf("%s: session/new REJECTED the server: %v", transport, err)
		t.Logf("%s VERDICT[%s]: REJECTED", transport, b.ID)
		dumpAgentStderr(t, proc)
		return
	}
	t.Logf("%s: session/new accepted", transport)

	if warm := probeWarmupDuration(); warm > 0 {
		time.Sleep(warm)
	}
	sawTool, marker := probeTurnWithRetry(t, ctx, proc.extProbeSetup, sid, transport)

	switch {
	case marker:
		t.Logf("%s VERDICT[%s]: OK — the %s transport works end-to-end", transport, b.ID, transport)
	case sawTool:
		t.Logf("%s VERDICT[%s]: ATTACHED — tool registered but never called", transport, b.ID)
	default:
		t.Logf("%s VERDICT[%s]: IGNORED — session accepted but the tool never appeared", transport, b.ID)
		dumpAgentStderr(t, proc)
	}
}

// probeHTTPServer is the fixture process running in --http mode.
type probeHTTPServer struct {
	cmd  *exec.Cmd
	port int
}

func (s *probeHTTPServer) stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
}

// startProbeHTTPServer launches the fixture and waits for its "PORT=<n>" line.
func startProbeHTTPServer(t *testing.T, ctx context.Context, nodePath, serverScript string) (*probeHTTPServer, error) {
	t.Helper()

	cmd := exec.CommandContext(ctx, nodePath, serverScript, "--http")
	cmd.Env = append(os.Environ(),
		"MCP_PROBE_MARKER="+mcpProbeMarker,
		"MCP_PROBE_TOOL="+mcpProbeToolName,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Return the error instead of failing the test: the caller already treats a
	// fixture failure as "this transport could not be measured", and a fatal
	// here aborts the whole backend subtest (observed: qoder's sse phase hit the
	// outer deadline and took the http result down with it).
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start fixture: %w", err)
	}

	srv := &probeHTTPServer{cmd: cmd}

	// The port line is the only thing the fixture writes to stdout before it
	// starts serving, so a bounded read is enough.
	portCh := make(chan int, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if m := regexp.MustCompile(`^PORT=(\d+)$`).FindStringSubmatch(line); m != nil {
				var p int
				fmt.Sscanf(m[1], "%d", &p)
				portCh <- p
				// Keep draining so the pipe never fills.
				for sc.Scan() {
				}
				return
			}
		}
	}()

	select {
	case port := <-portCh:
		srv.port = port
		t.Logf("fixture HTTP server listening on 127.0.0.1:%d", port)
		return srv, nil
	case <-time.After(20 * time.Second):
		srv.stop()
		return nil, fmt.Errorf("fixture did not report a port within 20s")
	case <-ctx.Done():
		srv.stop()
		return nil, ctx.Err()
	}
}

// ===========================================================================
// shared helpers
// ===========================================================================

// probePrereqs resolves node + the fixture and applies the backend filter.
func probePrereqs(t *testing.T) (nodePath, serverScript string, backends []mcpProbeBackend) {
	t.Helper()

	nodePath, err := exec.LookPath("node")
	require.NoError(t, err, "node is required by the probe fixtures")

	serverScript, err = filepath.Abs(filepath.Join("testdata", "mcp_probe_server.mjs"))
	require.NoError(t, err)
	require.FileExists(t, serverScript)

	only := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("MCP_PROBE_BACKENDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			only[id] = true
		}
	}
	for _, b := range mcpProbeBackends {
		if len(only) == 0 || only[b.ID] {
			backends = append(backends, b)
		}
	}
	return nodePath, serverScript, backends
}

func skipIfBackendMissing(t *testing.T, b mcpProbeBackend) {
	t.Helper()
	if _, err := exec.LookPath(b.Args[0]); err != nil {
		t.Skipf("%s not installed (%s)", b.Args[0], err)
	}
	if b.CLI != "" {
		if _, err := exec.LookPath(b.CLI); err != nil {
			t.Skipf("%s CLI not installed — adapter cannot reach its engine", b.CLI)
		}
	}
}

// probeTurnWithRetry runs up to two turns and reports whether the tool/marker
// was seen in either. Retrying matters because several agents attach MCP
// servers asynchronously (hermes starts the server after the first turn ends).
func probeTurnWithRetry(t *testing.T, ctx context.Context, setup *extProbeSetup, sessionID, phase string) (sawTool, sawMarker bool) {
	t.Helper()
	for attempt := 1; attempt <= 2; attempt++ {
		label := fmt.Sprintf("%s#%d", phase, attempt)
		tool, marker, _, _ := runMCPProbeTurnDetailed(t, ctx, setup, sessionID, label)
		sawTool = sawTool || tool
		sawMarker = sawMarker || marker
		if sawMarker {
			return sawTool, sawMarker
		}
		if attempt == 1 {
			time.Sleep(5 * time.Second)
		}
	}
	return sawTool, sawMarker
}

// resumeWithRetry retries while the agent reports a stale writer lock.
//
// Killing the previous adapter does not synchronously release the underlying
// engine's per-thread writer: codex-acp delegates to a codex app-server child
// that outlives the ACP process briefly. An immediate resume then fails with
// `thread ... already has an active writer` (-32600), which is a TIMING issue,
// not a missing capability — reporting it as "resume unsupported" would be a
// false negative. Production hits the same window when a session is restarted
// right after a crash, so this is worth measuring rather than papering over.
func resumeWithRetry(ctx context.Context, conn *acp.ClientSideConnection, sid, workDir string, server acp.McpServer) (acp.ResumeSessionResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= 6; attempt++ {
		resp, err := conn.ResumeSession(ctx, acp.ResumeSessionRequest{
			SessionId:  acp.SessionId(sid),
			Cwd:        workDir,
			McpServers: []acp.McpServer{server},
		})
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isStaleWriterLock(err) {
			return resp, err
		}
		select {
		case <-ctx.Done():
			return resp, lastErr
		case <-time.After(2 * time.Second):
		}
	}
	return acp.ResumeSessionResponse{}, lastErr
}

// loadWithRetry is resumeWithRetry for session/load.
func loadWithRetry(ctx context.Context, conn *acp.ClientSideConnection, sid, workDir string, server acp.McpServer) (acp.LoadSessionResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= 6; attempt++ {
		resp, err := conn.LoadSession(ctx, acp.LoadSessionRequest{
			SessionId:  acp.SessionId(sid),
			Cwd:        workDir,
			McpServers: []acp.McpServer{server},
		})
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isStaleWriterLock(err) {
			return resp, err
		}
		select {
		case <-ctx.Done():
			return resp, lastErr
		case <-time.After(2 * time.Second):
		}
	}
	return acp.LoadSessionResponse{}, lastErr
}

// isStaleWriterLock reports the transient "previous process still holds the
// session" error. Matched on the documented reason code, not on prose.
func isStaleWriterLock(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "thread_active_writer") ||
		strings.Contains(err.Error(), "already has an active writer")
}
