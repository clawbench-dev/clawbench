//go:build integration

package ai

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Hermes Agent (NousResearch) ACP 适配器 — 能力探测探针
// ===========================================================================
//
// 目的：在决定是否把 Hermes Agent 接入 ClawBench 的 ACP 传输之前，用真实
// `hermes acp` 进程实测其能力面与 wire 形状。全部为**观测型**探针：事实写入
// t.Logf，硬断言只覆盖"必须成立才能接入"的前提（响应可达、非 method-not-found、
// 基本回合能流式收尾）。
//
// 背景：Hermes 是 NousResearch 的通用 agent（90+ 工具、持久记忆、多平台），
// v0.21.7 起 `hermes acp` 走 acp_adapter/ 的 JSON-RPC stdio server。ACP 会话是
// **进程内**的（list/load/resume/fork 只对当前 server 进程可见）。
//
// 关键问题：
//
//	P1  initialize 广告了什么能力 / authMethods？（typed struct 会丢弃 _meta，
//	    故同时 dump 原始 wire）
//	P2  session/new 是否返回 models（SessionModelState）/ modes / configOptions？
//	    SDK v0.13.5 的 NewSessionResponse **没有 Models 字段** —— 必须走裸通道。
//	P3  基本回合：text delta / thinking / tool_call / usage / stopReason 是否
//	    按 ACP 标准 session/update 变体流出？
//	P4  轮中注入：session/prompt 在途时再发一条 prompt，走 redirect（"Redirected
//	    the active turn"）还是 queue（"Queued for the next turn"）？是否发回执帧？
//	P5  cancel：session/cancel 能否中断在途回合（stopReason=cancelled）？
//	P6  session/{list,fork,resume} + session/set_model 是否可用？（会话为进程内语义）
//
// 运行：
//
//	go test -tags integration -v -run 'TestHermesACPProbe' \
//	    -timeout 900s ./internal/ai/
//
// 需要本机安装 hermes（v0.21.7+）且已配置 provider（本机 minimax-cn）。
// 模型可用 HERMES_PROBE_MODEL 覆盖（形如 "minimax-cn:MiniMax-M2.7"）。

// hermesProbeModel 是探针默认使用的模型选择（provider:model 编码，见
// acp_adapter/model_catalog.py 的 encode_model_choice）。留空表示用 Hermes
// 当前配置的模型。
const hermesProbeModel = ""

// hermesProbeArgs 是启动 Hermes ACP server 的命令行。
var hermesProbeArgs = []string{"hermes", "acp"}

// requireHermesACP skips the test when the hermes CLI is missing.
func requireHermesACP(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("hermes"); err != nil {
		t.Skip("hermes CLI not installed, skipping Hermes ACP probe")
	}
}

// hermesProbeModelChoice returns the model id to pass to session/set_model.
func hermesProbeModelChoice() string {
	if v := os.Getenv("HERMES_PROBE_MODEL"); v != "" {
		return v
	}
	return hermesProbeModel
}

// startHermesProbe launches `hermes acp`, performs initialize + session/new, and
// returns the shared probe setup. It dumps the raw wire for initialize/session-new
// so fields the typed SDK struct drops are still visible.
func startHermesProbe(t *testing.T, ctx context.Context, clientName string) *extProbeSetup {
	t.Helper()

	setup := startSteeringProbe(t, ctx, hermesProbeArgs, clientName)

	// Raw wire: initialize response + session/new response. Hermes advertises
	// model state and modes that acp-go-sdk v0.13.5 cannot decode.
	t.Logf("P1 initialize RESPONSE (raw wire): %s", findRawResponse(t, setup.reader.snapshot(), "agentCapabilities"))
	t.Logf("P1 initialize REQUEST  (raw wire): %s", findRawRequest(t, setup.writer.buf.Bytes(), "initialize"))
	t.Logf("P2 session/new RESPONSE (raw wire): %s", findRawResponse(t, setup.reader.snapshot(), "models"))

	return setup
}

// ---------------------------------------------------------------------------
// P1 — initialize capabilities + auth methods
// ---------------------------------------------------------------------------

func TestHermesACPProbe_Initialize(t *testing.T) {
	requireHermesACP(t)

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	setup := startHermesProbe(t, ctx, "clawbench-hermes-probe")

	wire := string(setup.reader.snapshot())
	for _, key := range []string{"loadSession", "sessionCapabilities", "promptCapabilities", "authMethods", "fork", "resume", "list"} {
		t.Logf("P1 wire contains %-20s : %v", key, strings.Contains(wire, key))
	}

	// Hard prerequisite: the agent must advertise loadSession support so ClawBench
	// can implement the acp-load / acp-sync endpoints.
	require.Contains(t, wire, "loadSession",
		"P1 FAILED: Hermes ACP initialize did not advertise loadSession; "+
			"ClawBench's ACP session recovery depends on it")
	require.Contains(t, wire, "authMethods",
		"P1 FAILED: no authMethods in initialize response")
	t.Log("P1 CONFIRMED: initialize advertises session capabilities + auth methods")
}

// ---------------------------------------------------------------------------
// P2 — session/new model state + modes (raw channel; SDK drops these)
// ---------------------------------------------------------------------------

func TestHermesACPProbe_NewSessionState(t *testing.T) {
	requireHermesACP(t)

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	setup := startHermesProbe(t, ctx, "clawbench-hermes-probe")

	// Ask session/new again over the raw channel so we can decode the full result
	// (typed NewSessionResponse has no Models field in v0.13.5).
	callCtx, callCancel := context.WithTimeout(ctx, 60*time.Second)
	defer callCancel()
	resp, err := setup.raw.callRaw(callCtx, setup.writer, "session/new", map[string]any{
		"cwd":        acpTestWorkDir(),
		"mcpServers": []any{},
	})
	require.NoError(t, err, "P2 FAILED: no response to session/new over raw JSON-RPC")

	body, _ := json.Marshal(resp)
	t.Logf("P2 session/new raw response: %s", truncate(string(body), 4000))

	res, _ := resp["result"].(map[string]any)
	require.NotNil(t, res, "P2 FAILED: session/new returned no result object: %s", body)

	if models, ok := res["models"].(map[string]any); ok {
		cur, _ := models["currentModelId"].(string)
		avail, _ := models["availableModels"].([]any)
		t.Logf("P2 models: currentModelId=%q availableModels=%d", cur, len(avail))
		for i, m := range avail {
			if i >= 5 {
				t.Logf("P2   ... (%d more)", len(avail)-5)
				break
			}
			t.Logf("P2   model[%d]: %s", i, jsonStr(m))
		}
	} else {
		t.Log("P2 NOTE: no models object in session/new result")
	}
	if modes, ok := res["modes"].(map[string]any); ok {
		t.Logf("P2 modes: %s", jsonStr(modes))
	}
	if cfg, ok := res["configOptions"]; ok {
		t.Logf("P2 configOptions: %s", truncate(jsonStr(cfg), 1500))
	}

	// The session id is the handle every later probe needs.
	if sid, ok := res["sessionId"].(string); ok && sid != "" {
		setup.session = sid
	}
	require.NotEmpty(t, setup.session, "P2 FAILED: session/new returned no sessionId")
	t.Log("P2 CONFIRMED: session/new returns model/mode state over the raw channel")
}

// ---------------------------------------------------------------------------
// P3 — basic streaming turn (the core viability check)
// ---------------------------------------------------------------------------

// hermesProbeBasicPrompt is deliberately tiny: one Bash tool call plus a short
// answer, so the turn exercises tool + text streaming without a long wait.
const hermesProbeBasicPrompt = "Run the shell command `echo clawbench-hermes-probe` with your terminal tool, " +
	"then reply with exactly this token and nothing else: HERMES-PROBE-OK"

func TestHermesACPProbe_BasicStreamingTurn(t *testing.T) {
	requireHermesACP(t)

	ctx, cancel := contextWithTimeout(t, 480*time.Second)
	defer cancel()

	setup := startHermesProbe(t, ctx, "clawbench-hermes-probe")

	// Auto-approve so a terminal permission request never stalls the turn.
	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	streamCh := make(chan StreamEvent, 8192)
	registerProbeSession(t, setup, streamCh)

	promptCtx, promptCancel := context.WithTimeout(ctx, 300*time.Second)
	defer promptCancel()
	resp, err := setup.conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: acp.SessionId(setup.session),
		Prompt:    []acp.ContentBlock{acp.TextBlock(hermesProbeBasicPrompt)},
	})
	require.NoError(t, err, "P3 FAILED: session/prompt returned an error")
	t.Logf("P3 stopReason=%q usage=%s", resp.StopReason, jsonStr(resp.Usage))

	events := drainProbeStream(streamCh, 3*time.Second)
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Type]++
	}
	t.Logf("P3 event counts: %v", counts)

	content := concatACPContent(events)
	t.Logf("P3 content (%d chars): %s", len(content), truncate(content, 1200))

	require.NotEmpty(t, content,
		"P3 FAILED: no content streamed for a basic prompt — Hermes ACP is not producing "+
			"standard agent_message_chunk updates; events=%v", counts)

	if strings.Contains(content, "HERMES-PROBE-OK") {
		t.Log("P3 CONFIRMED: the model's exact reply token arrived via streaming content")
	} else {
		t.Log("P3 NOTE: reply token absent (model may have paraphrased); streaming itself worked")
	}
	if counts["tool_use"] > 0 || counts["tool_result"] > 0 {
		t.Logf("P3 CONFIRMED: tool events streamed (tool_use=%d tool_result=%d)",
			counts["tool_use"], counts["tool_result"])
	} else {
		t.Log("P3 NOTE: no tool events observed (model may have skipped the shell call)")
	}
}

// ---------------------------------------------------------------------------
// P4 — mid-turn injection (redirect vs queue)
// ---------------------------------------------------------------------------

func TestHermesACPProbe_MidTurnInjection(t *testing.T) {
	requireHermesACP(t)

	ctx, cancel := contextWithTimeout(t, 600*time.Second)
	defer cancel()

	setup := startHermesProbe(t, ctx, "clawbench-hermes-probe")

	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	streamCh := make(chan StreamEvent, 8192)
	registerProbeSession(t, setup, streamCh)

	// A deliberately slow turn: three real shell sleeps, so the injection lands
	// mid-turn rather than after completion.
	promptDone := make(chan struct{})
	var promptResp acp.PromptResponse
	var promptErr error
	go func() {
		promptResp, promptErr = setup.conn.Prompt(ctx, acp.PromptRequest{
			SessionId: acp.SessionId(setup.session),
			Prompt: []acp.ContentBlock{acp.TextBlock(
				"请分三步，每步之间必须真的调用终端工具执行命令：" +
					"1) 运行 `sleep 3` 并说明这是第一步；" +
					"2) 运行 `sleep 3` 并说明这是第二步；" +
					"3) 运行 `sleep 3` 并说明这是第三步。")},
		})
		close(promptDone)
	}()

	// Let the turn actually start before injecting.
	select {
	case <-promptDone:
		t.Log("P4 NOTE: prompt finished before injection — turn too fast; " +
			"the redirect-vs-queue branch could not be observed this run")
		require.NoError(t, promptErr, "prompt error")
		return
	case <-time.After(6 * time.Second):
	}

	const injectMarker = "CLAWBENCH-HERMES-INJECT-MARKER"
	injectStart := time.Now()
	injectCtx, injectCancel := context.WithTimeout(ctx, 60*time.Second)
	defer injectCancel()
	injectResp, err := setup.conn.Prompt(injectCtx, acp.PromptRequest{
		SessionId: acp.SessionId(setup.session),
		Prompt:    []acp.ContentBlock{acp.TextBlock("请立刻回复这句话，一个字都不要多：" + injectMarker)},
	})
	injectElapsed := time.Since(injectStart)
	require.NoError(t, err, "P4 FAILED: mid-turn session/prompt returned an error")

	body, _ := json.Marshal(injectResp)
	t.Logf("P4 mid-turn prompt response (%.2fs): %s", injectElapsed.Seconds(), body)

	// The injected prompt is answered immediately by _claim_turn_or_queue (either
	// "Redirected the active turn..." or "Queued for the next turn..."), which is
	// why it returns far sooner than the running turn.
	injectEvents := drainProbeStream(streamCh, 1500*time.Millisecond)
	injectText := concatACPContent(injectEvents)
	switch {
	case strings.Contains(injectText, "Redirected the active turn"):
		t.Log("P4 CONFIRMED: Hermes redirected the live turn (agent.redirect) — " +
			"mid-turn injection is supported without queueing")
	case strings.Contains(injectText, "Queued for the next turn"):
		t.Log("P4 CONFIRMED: Hermes queued the message for the next turn")
	default:
		t.Logf("P4 NOTE: neither redirect nor queue ack observed in stream (%.2fs, resp=%s)",
			injectElapsed.Seconds(), truncate(string(body), 400))
	}

	// Let the running turn finish cleanly.
	select {
	case <-promptDone:
		require.NoError(t, promptErr, "the running turn must complete after injection")
		t.Logf("P4 turn completed; stopReason=%q", promptResp.StopReason)
	case <-time.After(300 * time.Second):
		t.Fatal("P4 FAILED: running turn did not complete within 300s after injection")
	}

	all := concatACPContent(drainProbeStream(streamCh, 3*time.Second))
	t.Logf("P4 injection marker present in final output: %v", strings.Contains(all, injectMarker))
}

// ---------------------------------------------------------------------------
// P5 — cancel an in-flight turn
// ---------------------------------------------------------------------------

func TestHermesACPProbe_Cancel(t *testing.T) {
	requireHermesACP(t)

	ctx, cancel := contextWithTimeout(t, 480*time.Second)
	defer cancel()

	setup := startHermesProbe(t, ctx, "clawbench-hermes-probe")

	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	streamCh := make(chan StreamEvent, 8192)
	registerProbeSession(t, setup, streamCh)

	promptDone := make(chan struct{})
	var promptResp acp.PromptResponse
	var promptErr error
	go func() {
		promptResp, promptErr = setup.conn.Prompt(ctx, acp.PromptRequest{
			SessionId: acp.SessionId(setup.session),
			Prompt: []acp.ContentBlock{acp.TextBlock(
				"请运行 `sleep 30`，然后说明你完成了。")},
		})
		close(promptDone)
	}()

	select {
	case <-promptDone:
		t.Log("P5 NOTE: prompt finished before cancel could be sent — too fast")
		return
	case <-time.After(6 * time.Second):
	}

	// cancel is a notification (no response expected).
	require.NoError(t, setup.conn.Cancel(ctx, acp.CancelNotification{
		SessionId: acp.SessionId(setup.session),
	}), "P5 FAILED: session/cancel returned an error")

	// Hermes needs a settlement window before the cancelled turn resolves.
	select {
	case <-promptDone:
		require.NoError(t, promptErr, "cancelled prompt should resolve, not error")
		t.Logf("P5 CONFIRMED: cancel resolved the turn; stopReason=%q (want %q)",
			promptResp.StopReason, acp.StopReasonCancelled)
	case <-time.After(120 * time.Second):
		t.Fatal("P5 FAILED: turn did not resolve within 120s after session/cancel")
	}
}

// ---------------------------------------------------------------------------
// P6 — session/{list,fork,resume} availability
// ---------------------------------------------------------------------------

func TestHermesACPProbe_SessionManagement(t *testing.T) {
	requireHermesACP(t)

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	setup := startHermesProbe(t, ctx, "clawbench-hermes-probe")

	// list
	listCtx, listCancel := context.WithTimeout(ctx, 30*time.Second)
	defer listCancel()
	listResp, err := setup.conn.ListSessions(listCtx, acp.ListSessionsRequest{})
	if err != nil {
		t.Logf("P6 list: error %v", err)
	} else {
		t.Logf("P6 list: %d session(s) — %s", len(listResp.Sessions), truncate(jsonStr(listResp.Sessions), 600))
	}

	// fork (unstable method in v0.13.5)
	forkCtx, forkCancel := context.WithTimeout(ctx, 30*time.Second)
	defer forkCancel()
	forkResp, err := setup.conn.UnstableForkSession(forkCtx, acp.UnstableForkSessionRequest{
		SessionId: acp.SessionId(setup.session),
		Cwd:       acpTestWorkDir(),
	})
	if err != nil {
		t.Logf("P6 fork: error %v", err)
	} else {
		t.Logf("P6 fork: new session %q", forkResp.SessionId)
	}

	// resume
	resumeCtx, resumeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer resumeCancel()
	_, err = setup.conn.ResumeSession(resumeCtx, acp.ResumeSessionRequest{
		SessionId: acp.SessionId(setup.session),
		Cwd:       acpTestWorkDir(),
	})
	if err != nil {
		t.Logf("P6 resume: error %v", err)
	} else {
		t.Log("P6 resume: accepted")
	}

	// model switch — SDK v0.13.5 has no typed SetSessionModel, so use the raw
	// channel. Switch to a REAL advertised model (a bogus id is also accepted,
	// see below — Hermes does not validate model ids here).
	modelID := hermesProbeModelChoice()
	if modelID == "" {
		modelID = "minimax-cn:MiniMax-M3"
	}
	switchCtx, switchCancel := context.WithTimeout(ctx, 60*time.Second)
	defer switchCancel()
	switchResp, err := setup.raw.callRaw(switchCtx, setup.writer, "session/set_model", map[string]any{
		"sessionId": setup.session,
		"modelId":   modelID,
	})
	if err != nil {
		t.Logf("P6 set_model: no response (%v)", err)
	} else if code, msg, isErr := extractJSONRPCError(switchResp); isErr {
		if code == -32601 {
			t.Errorf("P6 FAILED: session/set_model returned Method not found (-32601); "+
				"model switching is unavailable. resp=%s", jsonStr(switchResp))
		} else {
			t.Logf("P6 set_model: dispatched (business error code=%v msg=%q) — method exists", code, msg)
		}
	} else {
		t.Logf("P6 set_model: accepted modelId=%q resp=%s", modelID, jsonStr(switchResp))
	}

	// Leniency probe: an unknown method returns -32601 (baseline), but set_model
	// returns {} for a nonexistent model. ClawBench must validate model ids
	// against the advertised list itself, since the agent will not.
	unknownCtx, unknownCancel := context.WithTimeout(ctx, 30*time.Second)
	defer unknownCancel()
	unknownResp, _ := setup.raw.callRaw(unknownCtx, setup.writer, "session/__no_such_method__", map[string]any{
		"sessionId": setup.session,
	})
	if code, _, isErr := extractJSONRPCError(unknownResp); isErr && code == -32601 {
		t.Log("P6 baseline CONFIRMED: unknown methods return -32601 (the raw channel reaches the dispatcher)")
	}
	bogusCtx, bogusCancel := context.WithTimeout(ctx, 30*time.Second)
	defer bogusCancel()
	bogusResp, err := setup.raw.callRaw(bogusCtx, setup.writer, "session/set_model", map[string]any{
		"sessionId": setup.session,
		"modelId":   "clawbench-hermes-probe-nonexistent-model",
	})
	if err == nil {
		if _, _, isErr := extractJSONRPCError(bogusResp); isErr {
			t.Logf("P6 set_model validates ids (bogus rejected): %s", jsonStr(bogusResp))
		} else {
			t.Log("P6 NOTE: set_model accepted a nonexistent model id ({} result) — Hermes does " +
				"NOT validate model ids; ClawBench must validate against the advertised list")
		}
	}

	t.Log("P6 NOTE: Hermes ACP sessions are process-local — list/load/resume/fork only " +
		"see sessions created by this same server process")
}
