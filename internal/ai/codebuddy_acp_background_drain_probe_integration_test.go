//go:build integration

package ai

import (
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// CodeBuddy 后台续轮（background_drain）承接探针 — issue #644
// ===========================================================================
//
// 目的：验证「智能体自己发起的那一轮」真的能被宿主接住。
//
// 背景：后台任务（run_in_background 命令 / 分离的子智能体）结束时，CodeBuddy
// 会在**空闲会话**上自己开一轮：先发 session_info_update（_meta 带
// codebuddy.ai/unsolicitedTurn，reason=background_drain），随后发内容帧，最后
// 发非标准的 session_end。宿主若不接，这一整轮的所有帧都会命中
// acp_client.go 的「无路由即丢弃」，会话从此静默（issue #644 的现象）。
//
// 本探针走真实的 codebuddy --acp，并装上**生产用的那个回调**
// （SetBackgroundTurnCallback），因此验的是真实链路而不是模拟：
//  1. 让 agent 起一个后台命令并立刻结束回合；
//  2. 模拟生产：回合结束后注销路由（会话回到空闲）；
//  3. 等宿主收到公告并开轮 —— 公告出现却没开轮就是失败；
//  4. 校验这一轮的正文真的流到了消费者手里，且**没有被截断**（收到的正文条数
//     不少于 wire 上的 agent_message_chunk 条数）；
//  5. 校验终态帧（session_end）被识别成 done，且排在所有正文之后。
//
// 运行（需要本机安装 codebuddy CLI 且已登录）：
//
//	go test -v -run 'TestCodebuddyACP_BackgroundDrain_AdoptedAsTurn' -tags integration \
//	    -timeout 900s ./internal/ai/
//
// 唯一的特殊前置条件：**不广告 terminal 能力**。
// 广告了，CodeBuddy 会把 run_in_background 的命令交给宿主终端执行，它自己就没有
// 后台任务，自然也不会回来通知 —— 实测如此，这也解释了既有那个只观察公告的探针
// 为什么一直写着「本次未触发」。
//
// 不需要客户端 opt-in：对照实验（不声明 mainAgentSupport、不打开 multitask 开关）
// 同样观察到了 drain 轮。生产环境正是这个配置，所以本探针刻意与生产一致。
//
// 与既有探针的分工：
//   - codebuddy_acp_team_probe_integration_test.go 的
//     TestCodebuddyACP_TeamMode_UnsolicitedDrain 只**观察**公告是否存在；
//   - 本探针验证宿主**承接**了那一轮（路由、正文、终态）。
//
// 环境依赖：若 agent 没有把任务真正放后台（没有 drain 轮），探针按既有约定打
// NOTE 后跳过 —— 那不是宿主的缺陷。但**只要公告出现，后续断言就是硬失败**。

// backgroundDrainProbePrompt asks for a detached background command and an
// immediate end of the turn, which is what makes the CLI come back later.
const backgroundDrainProbePrompt = `请严格按顺序执行，只做这两步：
1. 用 Bash 工具运行命令，参数里必须带 run_in_background: true，命令内容是 ` +
	"`sleep 8 && echo probe-done`" + `。
2. 拿到后台任务 id 后，立刻结束你的回合，只回复一句"已派出后台任务"。
不要等待这个后台任务，不要调用 TaskOutput，不要轮询它的状态。`

// backgroundDrainRecorder stands in for the service-layer consumer: it records
// every frame the host routes into the adopted turn and stops on the terminal
// event.
type backgroundDrainRecorder struct {
	sid string

	mu       sync.Mutex
	events   []StreamEvent
	doneSeen bool

	finished chan struct{}
}

func (r *backgroundDrainRecorder) consume(events <-chan StreamEvent, timeout time.Duration) {
	defer close(r.finished)
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			r.mu.Lock()
			r.events = append(r.events, ev)
			terminal := ev.Type == "done"
			if terminal {
				r.doneSeen = true
			}
			r.mu.Unlock()
			if terminal {
				return
			}
		case <-deadline:
			return
		}
	}
}

func (r *backgroundDrainRecorder) snapshot() (events []StreamEvent, doneSeen bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]StreamEvent, len(r.events))
	copy(out, r.events)
	return out, r.doneSeen
}

// countContent counts the reply-content events the host produced for the
// adopted turn.
func countContent(events []StreamEvent) int {
	n := 0
	for _, ev := range events {
		if ev.Type == "content" {
			n++
		}
	}
	return n
}

// countWireAgentChunks counts the agent_message_chunk frames the agent actually
// sent after index `from` — the ground truth the host must not fall short of.
func countWireAgentChunks(notifs []acp.SessionNotification, from int) int {
	n := 0
	for i := from; i < len(notifs); i++ {
		if notifs[i].Update.AgentMessageChunk != nil {
			n++
		}
	}
	return n
}

// findUnsolicitedTurnIndex returns the index of the drain announcement in the
// captured notifications, or -1.
func findUnsolicitedTurnIndex(notifs []acp.SessionNotification) int {
	for i, n := range notifs {
		if n.Update.SessionInfoUpdate == nil {
			continue
		}
		if _, ok := codeBuddyUnsolicitedTurn(n.Update.SessionInfoUpdate.Meta); ok {
			return i
		}
	}
	return -1
}

// TestCodebuddyACP_BackgroundDrain_AdoptedAsTurn is the end-to-end probe for
// issue #644: an agent-initiated turn must be adopted and fully delivered.
func TestCodebuddyACP_BackgroundDrain_AdoptedAsTurn(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 600*time.Second)
	defer cancel()

	setup := startTeamProbeWith(t, ctx, teamProbeOptions{
		// Terminal must stay off: advertising it makes the CLI delegate
		// background commands to the host, so it never has a background task of
		// its own to notify about.
		advertiseTerminal: false,
	})
	defer setup.stopEventPump()

	// The harness client is not bound to an ACPConn (no clawbench session id),
	// and adoption is keyed on it. Bind a stand-in so the production path runs.
	setup.client.connRef = &ACPConn{clawbenchSID: "probe-bg-drain"}

	// Install the PRODUCTION consumer. The probe records instead of persisting,
	// but everything up to the callback (announcement parsing, route opening,
	// frame routing, session_end detection) is the real code path.
	adopted := make(chan *backgroundDrainRecorder, 4)
	SetBackgroundTurnCallback(func(sid string, events <-chan StreamEvent, end func()) {
		rec := &backgroundDrainRecorder{sid: sid, finished: make(chan struct{})}
		select {
		case adopted <- rec:
		default:
		}
		rec.consume(events, 240*time.Second)
		end()
	})
	t.Cleanup(func() { SetBackgroundTurnCallback(nil) })

	// 1) Dispatch a detached background task and end the turn immediately.
	promptResp, err := setup.prompt(ctx, 180*time.Second, backgroundDrainProbePrompt)
	if err != nil {
		t.Logf("NOTE: dispatch prompt failed (%v); the drain turn cannot be exercised", err)
		return
	}
	t.Logf("dispatch stopReason=%s", promptResp.StopReason)

	// 2) Mirror production: the user's turn is over, so its route is gone and
	//    the session is idle when the agent comes back.
	setup.client.UnregisterSession(setup.session)

	// 3) Wait for the host to adopt the agent-initiated turn.
	var rec *backgroundDrainRecorder
	select {
	case rec = <-adopted:
	case <-time.After(150 * time.Second):
		notifs := setup.client.notifications()
		if findUnsolicitedTurnIndex(notifs) < 0 {
			t.Log("NOTE: no unsolicitedTurn observed — the task may not have been detached, " +
				"or the drain turn did not run. Nothing to assert about adoption.")
			return
		}
		t.Fatal("the agent announced a background drain turn but the host never adopted it")
	}
	require.Equal(t, "probe-bg-drain", rec.sid, "adopted under the ClawBench session id")

	// 4) Wait for the turn to end (terminal event) or time out.
	select {
	case <-rec.finished:
	case <-time.After(240 * time.Second):
	}

	events, doneSeen := rec.snapshot()
	t.Logf("adopted turn: %d frames, terminal=%v", len(events), doneSeen)

	// 5) The turn's output must have reached the consumer.
	require.NotEmpty(t, events, "adopted turn delivered no frames — the whole turn was dropped")
	require.GreaterOrEqual(t, countContent(events), 1,
		"adopted turn delivered no content — the reply would be invisible")

	// 6) No truncation: every agent_message_chunk the wire carried after the
	//    announcement must have produced a content event. This is the regression
	//    the first review round caught (a terminal sent off the stdout tee could
	//    overtake frames still queued in the SDK).
	notifs := setup.client.notifications()
	announceAt := findUnsolicitedTurnIndex(notifs)
	require.GreaterOrEqual(t, announceAt, 0, "announcement vanished from the captured notifications")
	wireChunks := countWireAgentChunks(notifs, announceAt)
	t.Logf("wire agent_message_chunk after announcement=%d, host content events=%d",
		wireChunks, countContent(events))
	require.GreaterOrEqual(t, countContent(events), wireChunks,
		"reply truncated: host produced fewer content events than the agent sent")

	// 7) The terminal frame must have been recognised, and must be last.
	require.True(t, doneSeen,
		"no terminal event: session_end was not recognised, the bubble would spin until the backstop timeout")
	require.Equal(t, "done", events[len(events)-1].Type,
		"terminal event is not last — content arrived after the turn was closed")

	t.Logf("CONFIRMED: agent-initiated background turn adopted and delivered in full (%d content frames)",
		countContent(events))
}
