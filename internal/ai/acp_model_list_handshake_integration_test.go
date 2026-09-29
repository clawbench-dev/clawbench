//go:build integration

package ai

// ===========================================================================
// ACP 模型列表来源 — 握手 vs. Prompt
// ===========================================================================
//
// 待验证的论断：
//
//	「ACP 模式下模型列表是随便发一条消息就能拿到的，哪怕这条消息对应的
//	  模型用不了，列表照样返回。」
//
// 代码上的预期（见 acp_backend.go ExecuteStream）：
//
//	Step 2  emitSessionAndCacheState → CacheNewSessionState → EmitSessionStateEvents
//	        这里就已经从 session/new 响应里抽出模型列表并下发 model_list_update
//	Step 3  conn.Prompt(...)   ← 用户消息到这一步才发出去
//
// 因此正确的说法是「模型列表来自握手，和 prompt 内容无关」，而不是
// 「发一条 prompt 才会返回列表」。本测试用三条独立证据钉死这个区别：
//
//	T1 零 prompt：只跑 Initialize + session/new，不发送任何 prompt，
//	   用生产代码 extractACPModelList / raw availableModels 提取，列表已经存在。
//	   —— 这是最强证据：没有 prompt，列表照样有。
//
//	T2 顺序：真实跑一轮时，model_list_update 必须排在第一个 content 事件之前
//	   （它由 Step 2 下发，不可能来自模型输出）。
//
//	T3 内容无关 + 失败无关：
//	   T3a 换一条内容完全不同的 prompt，得到的列表与握手列表一致；
//	   T3b 指定一个 agent 不可能运行的模型，prompt 可能失败/崩溃重试，
//	       但 model_list_update 仍然被下发。
//
// 运行：
//
//	go test -v -run 'TestACP_ModelList' -tags integration -timeout 900s ./internal/ai/
//
// 需要本机安装对应 CLI；未安装的后端自动 skip。未登录的 agent 可能返回空
// 列表（如 kimi），此时只记录不断言 —— 空列表无法反驳「不需要 prompt」。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// 零 prompt 握手探针
// ---------------------------------------------------------------------------

// acpHandshakeProbe 驱动一个真实 ACP agent 进程走完 Initialize + session/new
// 后立即停止。全程不发送任何 prompt —— 这正是探针的意义：凡是在这里观测到
// 的东西，都只能由握手产生。
func acpHandshakeProbe(t *testing.T, ctx context.Context, cmdParts []string) (*acpRawTraffic, *acp.InitializeResponse, *acp.NewSessionResponse, error) {
	t.Helper()

	cmd := exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

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

	conn := acp.NewClientSideConnection(NewClawBenchACPClient(), recIn, recOut)
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
		ClientInfo: &acp.Implementation{Name: "clawbench-model-list-probe", Version: "1.0.0"},
	})
	if err != nil {
		return rec, nil, nil, fmt.Errorf("initialize: %w", err)
	}

	workDir, _ := os.Getwd()
	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: workDir, McpServers: []acp.McpServer{}})
	if err != nil {
		return rec, &initResp, nil, fmt.Errorf("new_session: %w", err)
	}

	// 给 agent 一点时间把 session/new 之后的异步通知（某些 agent 会另发一条
	// 携带模型列表的 config_option_update）刷出来，再快照原始流量。
	time.Sleep(500 * time.Millisecond)

	rec.fromAgent = parseWireMessages(recOut.buf)
	rec.toAgent = parseWireMessages(recIn.buf)

	return rec, &initResp, &newResp, nil
}

// modelListFromHandshake 复刻生产的提取优先级：先 ACP v2 的 ConfigOptions
// （extractACPModelList），再是 SDK 会丢掉的 kimi 风格原始扩展
// result.models.availableModels。
func modelListFromHandshake(newResp *acp.NewSessionResponse, rec *acpRawTraffic) *ModelListState {
	if newResp != nil {
		if ml := extractACPModelList(newResp); ml != nil && len(ml.Models) > 0 {
			return ml
		}
	}
	return rawSessionModelState(rec)
}

// rawSessionModelState 从原始 JSON-RPC 流量里捞出 SessionModelState 扩展。
// 逻辑与 acpStdoutFilter.cacheModelsFromResponse 对齐，但直接在测试里解析，
// 避免依赖 filter 实例。
func rawSessionModelState(rec *acpRawTraffic) *ModelListState {
	if rec == nil {
		return nil
	}
	for _, line := range rec.fromAgent {
		if !bytes.Contains(line, []byte("availableModels")) {
			continue
		}
		var msg struct {
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(line, &msg) != nil || len(msg.Result) == 0 {
			continue
		}
		var result struct {
			Models json.RawMessage `json:"models"`
		}
		if json.Unmarshal(msg.Result, &result) != nil || len(result.Models) == 0 {
			continue
		}
		var state acpSessionModelState
		if json.Unmarshal(result.Models, &state) != nil {
			continue
		}
		if len(state.AvailableModels) == 0 && state.CurrentModelID == "" {
			continue
		}
		ml := &ModelListState{CurrentModelID: state.CurrentModelID}
		for _, m := range state.AvailableModels {
			ml.Models = append(ml.Models, model.AgentModel{ID: m.ModelID, Name: m.Name})
		}
		return ml
	}
	return nil
}

// sortedModelIDs 返回排序后的模型 ID 列表，便于做顺序无关的集合比较。
func sortedModelIDs(models []model.AgentModel) []string {
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ids
}

// firstEventIndex 返回第一个匹配给定类型的事件下标，找不到返回 -1。
func firstEventIndex(events []StreamEvent, eventType string) int {
	for i, e := range events {
		if e.Type == eventType {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// T1 + T2 + T3a：一次子测试覆盖三条证据
// ---------------------------------------------------------------------------

func TestACP_ModelListIsHandshakeDerived(t *testing.T) {
	for _, cfg := range acpBackends {
		t.Run(cfg.ID, func(t *testing.T) {
			requireACPBackendAvailable(t, cfg)

			// ── T1：零 prompt 握手 ──────────────────────────────────
			// 不发送任何 prompt，仅 Initialize + session/new。
			probeCtx, probeCancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer probeCancel()

			rec, _, newResp, err := acpHandshakeProbe(t, probeCtx, strings.Fields(cfg.AcpCommand))
			if err != nil {
				// 本探针直接驱动 SDK，观测的是 agent 本身而非 ClawBench 的连接
				// 管理代码，所以握手失败只说明环境不具备（最常见是未登录，例如
				// copilot 返回 "Authentication required"）。会话能否建立由主
				// ACP 集成套件负责断言，这里跳过而不是失败。
				t.Skipf("agent %s: 无法建立会话，跳过（环境前置条件不满足）: %v", cfg.ID, err)
			}
			require.NotNil(t, newResp, "session/new must return a response")

			t.Logf("session/new: sessionId=%s configOptions=%d",
				newResp.SessionId, len(newResp.ConfigOptions))
			for i, opt := range newResp.ConfigOptions {
				t.Logf("  config_option[%d]: %s", i, describeConfigOption(opt))
			}

			handshakeList := modelListFromHandshake(newResp, rec)
			if handshakeList == nil || len(handshakeList.Models) == 0 {
				// 不是每个 agent 都通过 ACP 广告模型列表；未登录时也可能返回空。
				// 空列表无法反驳「不需要 prompt」，所以只记录不失败。
				t.Logf("agent %s: 握手中没有模型列表（可能未登录或该 agent 不上报模型）", cfg.ID)
				t.Logf("agent %s: 零 prompt 已证明握手可独立完成，列表缺失与 prompt 无关", cfg.ID)
				return
			}

			t.Logf("T1 零 prompt 握手即拿到模型列表: current=%q, %d models",
				handshakeList.CurrentModelID, len(handshakeList.Models))
			for _, m := range handshakeList.Models {
				t.Logf("  model id=%q name=%q", m.ID, m.Name)
			}

			// ── T2 + T3a：真实跑一轮，用诗词 prompt ────────────────
			// 直接对应用户的说法：「随便发一个诗词过去」。
			agent := buildACPAgent(cfg)
			setupACPTestEnvForAgent(t, agent)
			backend, err := NewACPBackend(agent)
			require.NoError(t, err)

			// 让注册表对本次进程实例重新做一次全量刷新，避免上一条子测试的
			// refreshedInProcess 标记让 ForceUpdate 被跳过。
			GetAgentCapabilityRegistry().MarkStale(agent.ID)

			sessionID := acpSessionID()
			cleanupConn(t, sessionID)

			events := sendACPPrompt(t, backend, sessionID,
				"写一首关于秋天的五言绝句，只要诗本身。", cfg.Timeout)
			requireDoneEvent(t, events)

			modelUpdates := findModelListUpdateEvents(events)
			// 走到这里已经证明握手能拿到列表，因此本轮若一个 model_list_update
			// 都没有，就是真的丢了事件（例如回归删掉了 EmitSessionStateEvents
			// 里的 model 分支）——不能当成「该 agent 不上报模型」放过。
			require.NotEmpty(t, modelUpdates,
				"agent %s: 握手已证明有模型列表（%d 个），本轮却未下发 model_list_update —— 事件链断裂",
				cfg.ID, len(handshakeList.Models))

			// T2：model_list_update 必须早于第一个 content —— 它来自 Step 2，
			// 不可能由模型输出产生。
			mlIdx := firstEventIndex(events, "model_list_update")
			contentIdx := firstEventIndex(events, "content")
			t.Logf("T2 事件顺序: model_list_update@%d, first content@%d", mlIdx, contentIdx)
			if contentIdx >= 0 {
				assert.Less(t, mlIdx, contentIdx,
					"agent %s: model_list_update 必须先于第一个 content 事件 —— 列表由 session/new 握手下发，与模型输出无关", cfg.ID)
			}

			// T3a：prompt 内容无关 —— 诗词 prompt 拿到的列表 == 零 prompt 握手列表。
			turnList := modelUpdates[0].ModelList
			require.NotNil(t, turnList, "agent %s: model_list_update 应携带 ModelList", cfg.ID)

			assert.Equal(t, sortedModelIDs(handshakeList.Models), sortedModelIDs(turnList.Models),
				"agent %s: 诗词 prompt 得到的模型列表应与零 prompt 握手列表一致（列表与 prompt 内容无关）", cfg.ID)
			t.Logf("T3a prompt 内容无关: 诗词 prompt 列表 == 握手列表 (%d models)", len(turnList.Models))
			t.Logf("state after prompt: %s", fmtACPStateSummary(sessionID))
		})
	}
}

// ---------------------------------------------------------------------------
// T3b：模型不可用时仍返回列表
// ---------------------------------------------------------------------------

// TestACP_ModelListEmittedWhenModelUnusable 验证用户论断里最反直觉的一半：
// 即使指定的模型 agent 根本跑不了（prompt 会失败甚至把进程打崩触发重试），
// 模型列表依然会被下发 —— 因为它在 Prompt 之前就已经发完了。
//
// 只对已在 acpBackends 中声明支持 model 配置的后端运行（SupportsConfig），
// 否则「切换模型」这一步本身就没有意义。
func TestACP_ModelListEmittedWhenModelUnusable(t *testing.T) {
	const bogusModel = "clawbench-nonexistent-model-xyz"

	for _, cfg := range acpBackends {
		if !cfg.SupportsConfig {
			continue
		}
		t.Run(cfg.ID, func(t *testing.T) {
			requireACPBackendAvailable(t, cfg)

			agent := buildACPAgent(cfg)
			setupACPTestEnvForAgent(t, agent)
			backend, err := NewACPBackend(agent)
			require.NoError(t, err)

			GetAgentCapabilityRegistry().MarkStale(agent.ID)

			sessionID := acpSessionID()
			cleanupConn(t, sessionID)

			ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()

			ch, err := backend.ExecuteStream(ctx, ChatRequest{
				Prompt:    "写一首关于秋天的五言绝句。",
				SessionID: sessionID,
				WorkDir:   acpTestWorkDir(),
				Model:     bogusModel, // 一个 agent 不可能运行的模型
			})
			require.NoError(t, err, "ExecuteStream 不应返回错误（失败会走事件流）")

			events := collectACPEvents(t, ch, cfg.Timeout)

			// 关键断言：无论这一轮最终成功、失败还是崩溃重试，模型列表都已经
			// 在 Prompt 之前下发过了。
			modelUpdates := findModelListUpdateEvents(events)
			if len(modelUpdates) == 0 {
				t.Logf("agent %s: 本轮没有 model_list_update（该 agent 可能不通过 ACP 上报模型）", cfg.ID)
				t.Logf("agent %s: 事件类型: %v", cfg.ID, acpEventTypes(events))
				return
			}

			mlIdx := firstEventIndex(events, "model_list_update")
			contentIdx := firstEventIndex(events, "content")
			t.Logf("指定不可用模型 %q 后: model_list_update@%d, first content@%d, 事件类型=%v",
				bogusModel, mlIdx, contentIdx, acpEventTypes(events))
			if contentIdx >= 0 {
				assert.Less(t, mlIdx, contentIdx,
					"agent %s: 即便模型不可用，model_list_update 仍应先于模型输出出现", cfg.ID)
			}

			assert.NotEmpty(t, modelUpdates[0].ModelList.Models,
				"agent %s: 模型不可用不应阻止模型列表下发", cfg.ID)
		})
	}
}
