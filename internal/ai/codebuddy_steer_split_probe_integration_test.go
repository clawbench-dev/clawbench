//go:build integration

package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// 预研：能否在"插入处"把助手消息截断为两条
// ===========================================================================
//
// 背景：steer 接入后，运行中插入的用户消息排在**正在流式输出的助手回复之后**，
// 呈现 `[Q1] [助手仍在流式] [Q2]`，Q2 后面空无一物，看着像"发了没人理"。目前的
// 缓解是给 Q2 加"已并入当前回复"提示。更符合直觉的做法是**在插入处把助手回复
// 切成两条**：
//
//	[Q1] [助手回复·前段] [Q2] [助手回复·后段]
//
// 这需要一个精确的"插入边界"信号：宿主必须知道 steer 之后 agent 新产生的内容
// 属于"后段"。本探针**只做观测**，回答三个问题：
//
//	P4a  steer 之后，agent 是否在 wire 上发出可识别的边界信号？
//	     （重点：sessionUpdate=user_message_chunk —— 文档 §5 称其为"steer 注入回执"）
//	P4b  该信号是否带足够的信息定位边界（clientUserMessageId / messageId / _meta）？
//	P4c  边界信号在时间线上是否恰好落在 steer 前后（前段/后段之间）？
//
// 结论只以 t.Logf 记录（观测型，不断言业务结论）—— 这类私有接口无版本承诺，
// 断言会随 agent 版本漂移而误报。唯一的硬断言是"探针本身跑通了"。

// steerBoundaryFrame 记录 wire 上一帧的类型与关键字段，用于时间线分析。
type steerBoundaryFrame struct {
	At     time.Duration // 相对 steer 发起时刻
	Kind   string        // "session_update" | "response" | "request" | "other"
	Update string        // sessionUpdate 值（若有）
	Detail string        // 截断后的原始 JSON
}

// steerBoundaryRecorder 持续快照 agent 的原始 stdout，把每一帧解析成
// steerBoundaryFrame，并打上相对 steer 时刻的时间戳。
type steerBoundaryRecorder struct {
	mu        sync.Mutex
	frames    []steerBoundaryFrame
	seen      int
	steerAt   time.Time
	haveSteer bool
}

func (r *steerBoundaryRecorder) markSteer() {
	r.mu.Lock()
	r.steerAt = time.Now()
	r.haveSteer = true
	r.mu.Unlock()
}

// ingest 解析 reader 中尚未消费的完整行。
func (r *steerBoundaryRecorder) ingest(reader *demuxReader) {
	snap := reader.snapshot()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(snap) <= r.seen {
		return
	}
	tail := snap[r.seen:]
	idx := bytes.LastIndexByte(tail, '\n')
	if idx < 0 {
		return
	}
	chunk := tail[:idx+1]
	r.seen += idx + 1

	sc := bufio.NewScanner(bytes.NewReader(chunk))
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		f := steerBoundaryFrame{Kind: "other", Detail: compactJSON(line)}
		if r.haveSteer {
			f.At = time.Since(r.steerAt)
		}
		// Classification order matters. A JSON-RPC REQUEST has method AND id;
		// a NOTIFICATION has method but NO id; a RESPONSE has id but no method.
		// Checking `method` alone would misfile every notification as a request.
		_, hasMethod := m["method"]
		_, hasID := m["id"]
		switch {
		case hasMethod && hasID:
			f.Kind = "request"
		case hasID:
			f.Kind = "response"
		case hasMethod:
			// ACP notifications: {"method":"session/update","params":{"update":{...}}}
			f.Kind = "notification"
			if params, ok := m["params"].(map[string]any); ok {
				if upd, ok := params["update"].(map[string]any); ok {
					f.Kind = "session_update"
					if s, ok := upd["sessionUpdate"].(string); ok {
						f.Update = s
					}
				}
			}
		}
		r.frames = append(r.frames, f)
	}
}

// framesSince returns frames recorded after the steer mark (At >= 0), plus all
// frames when no steer was marked.
func (r *steerBoundaryRecorder) framesSince() []steerBoundaryFrame {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]steerBoundaryFrame, len(r.frames))
	copy(out, r.frames)
	return out
}

// pump 是 recorder 的采集循环（20ms 粒度，与探针的 response pump 同频）。
func (r *steerBoundaryRecorder) pump(ctx context.Context, reader *demuxReader) {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.ingest(reader)
		}
	}
}

// TestCodebuddyACP_ExtProbe_SteerSplitBoundary 预研"插入处截断"的可行性。
//
// 观测目标（P4a/P4b/P4c 见文件头注释）：
//   - steer 之后 wire 上出现哪些 sessionUpdate 类型；
//   - 是否有 user_message_chunk（文档 §5 称其为 steer 回执）；
//   - 该帧是否携带 clientUserMessageId / messageId / _meta 等定位信息；
//   - 它在时间线上的位置（是否夹在前段与后段内容之间）。
//
// 运行：
//
//	go test -tags integration -v -run 'TestCodebuddyACP_ExtProbe_SteerSplitBoundary' \
//	    -timeout 400s ./internal/ai/
func TestCodebuddyACP_ExtProbe_SteerSplitBoundary(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 360*time.Second)
	defer cancel()

	setup := startExtProbe(t, ctx, acp.ClientCapabilities{
		Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		Terminal: true,
	}, "clawbench-steer-split-probe")

	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	streamCh := make(chan StreamEvent, 2048)
	registerProbeSession(t, setup, streamCh)

	// 独立采集器：记录每一帧及其相对 steer 时刻的偏移。
	rec := &steerBoundaryRecorder{}
	go rec.pump(ctx, setup.reader)

	// 让本轮足够长，保证有稳定的"前段 / 后段"内容可以分辨。
	promptDone := make(chan struct{})
	var promptResp acp.PromptResponse
	var promptErr error
	go func() {
		promptResp, promptErr = setup.conn.Prompt(ctx, acp.PromptRequest{
			SessionId: acp.SessionId(setup.session),
			Prompt: []acp.ContentBlock{acp.TextBlock(
				"请分三步，每步之间必须真的调用 Bash 工具执行命令：" +
					"1) 运行 `sleep 3` 并说明这是第一步；" +
					"2) 运行 `sleep 3` 并说明这是第二步；" +
					"3) 运行 `sleep 3` 并说明这是第三步。")},
		})
		close(promptDone)
	}()

	// 等本轮真正开始（前段已产出一些内容）再 steer，确保 steer 落在轮中。
	select {
	case <-promptDone:
		t.Skip("prompt finished before we could steer — too fast for this probe")
	case <-time.After(4 * time.Second):
	}

	const steerMarker = "CLAWBENCH-SPLIT-BOUNDARY-MARKER"
	rec.markSteer()
	steerStart := time.Now()

	steerCtx, steerCancel := context.WithTimeout(ctx, 30*time.Second)
	defer steerCancel()
	steerResp, err := setup.raw.callRaw(steerCtx, setup.writer, "session/steer", map[string]any{
		"sessionId": setup.session,
		"contentBlocks": []map[string]any{
			{"type": "text", "text": "请立刻回复这句话，一个字都不要多：" + steerMarker},
		},
		"clientUserMessageId": "clawbench-split-probe-1",
	})
	steerElapsed := time.Since(steerStart)
	require.NoError(t, err, "session/steer must respond while a prompt is in flight")

	steerBody, _ := json.Marshal(steerResp)
	t.Logf("P4 steer response (%.2fs): %s", steerElapsed.Seconds(), steerBody)

	steered := false
	ownerRequestID := ""
	if res, ok := steerResp["result"].(map[string]any); ok {
		if v, ok := res["steered"].(bool); ok {
			steered = v
		}
		if v, ok := res["ownerRequestId"].(string); ok {
			ownerRequestID = v
		}
	}
	t.Logf("P4a steer accepted=%v ownerRequestId=%q", steered, ownerRequestID)
	if !steered {
		t.Logf("NOTE: steer was declined (%s) — the session was not injectable, so the "+
			"boundary signal (if any) cannot be observed. Re-run with a longer turn.",
			steerBody)
	}

	// 等本轮结束，让采集器拿到全部帧。
	select {
	case <-promptDone:
		require.NoError(t, promptErr, "prompt must complete cleanly after steer")
		t.Logf("P4 turn completed; stopReason=%q", promptResp.StopReason)
	case <-time.After(180 * time.Second):
		t.Fatal("prompt did not complete within 180s after steer")
	}

	// 给采集器最后一个 tick 收尾。
	time.Sleep(500 * time.Millisecond)
	frames := rec.framesSince()

	// ── 时间线：steer 前后的 sessionUpdate 序列 ──
	var updates []string
	var userMessageChunkFrames []steerBoundaryFrame
	seenTypes := map[string]int{}
	for _, f := range frames {
		if f.Kind != "session_update" {
			continue
		}
		updates = append(updates, f.Update)
		seenTypes[f.Update]++
		if f.Update == "user_message_chunk" {
			userMessageChunkFrames = append(userMessageChunkFrames, f)
		}
	}
	t.Logf("P4a sessionUpdate sequence after start (%d frames): %s",
		len(updates), strings.Join(updates, " → "))
	t.Logf("P4a sessionUpdate type counts: %v", seenTypes)
	t.Logf("P4a all frame kinds: %d frames total", len(frames))
	kindCounts := map[string]int{}
	for _, f := range frames {
		kindCounts[f.Kind]++
	}
	t.Logf("P4a frame kind counts: %v", kindCounts)

	// ── 关键问题：user_message_chunk 是否是 steer 回执？ ──
	if len(userMessageChunkFrames) == 0 {
		t.Logf("P4b NO user_message_chunk observed. The docs call it the steer receipt, " +
			"but this run did not emit one — either the doc is wrong, the agent version " +
			"changed, or it only fires under conditions this probe did not hit.")
	} else {
		t.Logf("P4b OBSERVED %d user_message_chunk frame(s):", len(userMessageChunkFrames))
		for i, f := range userMessageChunkFrames {
			t.Logf("    [%d] at steer%+.2fs: %s", i, f.At.Seconds(), f.Detail)
		}
		// 定位信息：clientUserMessageId / messageId / _meta 是边界可用的前提。
		for i, f := range userMessageChunkFrames {
			var probe struct {
				Params struct {
					Update struct {
						SessionUpdate string         `json:"sessionUpdate"`
						MessageID     string         `json:"messageId"`
						Meta          map[string]any `json:"_meta"`
						Content       map[string]any `json:"content"`
					} `json:"update"`
				} `json:"params"`
			}
			if err := json.Unmarshal([]byte(f.Detail), &probe); err != nil {
				t.Logf("    [%d] parse failed: %v", i, err)
				continue
			}
			u := probe.Params.Update
			metaKeySet := map[string]bool{}
			collectKeys(u.Meta, metaKeySet, 0)
			t.Logf("    [%d] messageId=%q content.type=%v _meta.keys=%v",
				i, u.MessageID, u.Content["type"], metaKeySet)

			// 关键关联：messageId 是否等于我们发送的 clientUserMessageId？
			// 这决定了宿主能否用自己生成的 id 精确定位边界（而非解析 agent 内部 id）。
			metaMessageID, _ := u.Meta["codebuddy.ai/messageId"].(string)
			metaConvReqID, _ := u.Meta["codebuddy.ai/conversationRequestId"].(string)
			t.Logf("    [%d] CORRELATION: _meta.messageId=%q (we sent %q → match=%v)",
				i, metaMessageID, "clawbench-split-probe-1", metaMessageID == "clawbench-split-probe-1")
			t.Logf("    [%d] CORRELATION: _meta.conversationRequestId=%q (steer ownerRequestId=%q → match=%v)",
				i, metaConvReqID, ownerRequestID, metaConvReqID == ownerRequestID)
		}
	}

	// ── 前后段是否可分：看 steer 之后是否仍有 agent_message_chunk ──
	afterSteer := 0
	for _, f := range frames {
		if f.Kind == "session_update" && f.Update == "agent_message_chunk" && f.At >= 0 {
			afterSteer++
		}
	}
	t.Logf("P4c agent_message_chunk AFTER steer: %d (non-zero means the assistant kept "+
		"producing content after injection — i.e. there IS a 'part 2' to split into)", afterSteer)

	// ── 决定性证据：边界帧是否恰好夹在"前段内容"与"后段内容"之间？ ──
	// 这是"截断为两条"能否成立的唯一充分条件。若边界帧之前和之后都有
	// agent_message_chunk，则宿主可以按此帧的位置切分，无需任何推断。
	boundaryIdx := -1
	for i, f := range frames {
		if f.Kind == "session_update" && f.Update == "user_message_chunk" {
			boundaryIdx = i
			break
		}
	}
	if boundaryIdx < 0 {
		t.Logf("P4c NO boundary index — cannot assess interleaving")
	} else {
		beforeContent, afterContent := 0, 0
		for i := 0; i < boundaryIdx; i++ {
			if frames[i].Kind == "session_update" && frames[i].Update == "agent_message_chunk" {
				beforeContent++
			}
		}
		for i := boundaryIdx + 1; i < len(frames); i++ {
			if frames[i].Kind == "session_update" && frames[i].Update == "agent_message_chunk" {
				afterContent++
			}
		}
		t.Logf("P4c DECISIVE: boundary frame at index %d/%d; "+
			"agent_message_chunk before=%d after=%d",
			boundaryIdx, len(frames), beforeContent, afterContent)
		if beforeContent > 0 && afterContent > 0 {
			t.Logf("P4c *** SPLIT IS FEASIBLE *** — the boundary frame is interleaved between " +
				"part-1 and part-2 content chunks. A host can split exactly here with no guessing.")
		} else {
			t.Logf("P4c SPLIT NOT SUPPORTED by this run — content chunks exist on only one "+
				"side of the boundary (before=%d after=%d), so the split point is ambiguous.",
				beforeContent, afterContent)
		}

		// 边界邻域：打印前后各 6 帧，人工可核。
		lo := boundaryIdx - 6
		if lo < 0 {
			lo = 0
		}
		hi := boundaryIdx + 7
		if hi > len(frames) {
			hi = len(frames)
		}
		t.Logf("P4c boundary neighbourhood (index: kind/update @ steer offset):")
		for i := lo; i < hi; i++ {
			marker := "  "
			if i == boundaryIdx {
				marker = ">>"
			}
			t.Logf("    %s [%d] %s/%s @ steer%+.2fs",
				marker, i, frames[i].Kind, frames[i].Update, frames[i].At.Seconds())
		}
	}

	// ── marker 是否出现在输出里（证明 steer 真的影响了模型输出）──
	events := drainProbeStream(streamCh, 3*time.Second)
	content := concatACPContent(events)
	t.Logf("P4 steer marker present in final output: %v", strings.Contains(content, steerMarker))
	if !strings.Contains(content, steerMarker) {
		t.Logf("NOTE: marker absent — the model may have ignored the injected instruction. " +
			"This does not disprove steer; steered=true + ownerRequestId is the authoritative signal.")
	}

	// ── 汇总：对"截断为两条"的可行性判断依据 ──
	t.Logf("──────── P4 SUMMARY ────────")
	t.Logf("boundary signal (user_message_chunk): %v", len(userMessageChunkFrames) > 0)
	t.Logf("content continues after steer:        %v", afterSteer > 0)
	t.Logf("steer accepted:                       %v", steered)
	t.Logf("A split is possible ONLY if a boundary signal exists AND content continues " +
		"after it; otherwise the host would have to infer the split point, which is unsafe.")
}
