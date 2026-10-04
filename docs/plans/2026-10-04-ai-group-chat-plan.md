# AI 群聊（Group Chat）实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

> ## ⚠️ 评审勘误（2026-10-04）— 实现前必读
>
> 本计划经 Superpower code-reviewer 评审，发现 **7 Critical / 10 Important**，已逐条核对代码属实。**本计划尚不可直接执行**，须先按设计文档 `2026-10-04-ai-group-chat-design.md` **§12 评审勘误**修订下列阶段：
>
> - **C1（致命）**：流式行按"会话内最新一条"定位（`chat.go:2512,2593`），同轮并行 N 成员会互相覆盖。Phase C 必须新增**按消息 id 的写入原语**；L0/L1 不解决此问题。
> - **C2**：`agent_id` 无写/读路径（`AddChatMessage` 无参数、`model.ChatMessage` 无字段）。Phase A/F/J 需贯穿。
> - **C3**：`session_type` 影响 17 处（含**参数化**查询 grep 不到）。Phase E2 的 grep 清单错误且不全。
> - **C4**：Phase C 漏了 `run_turn.go` 的 `failTurn`（`:196`）与 metadata（`:340`）。
> - **C5**：成员 resume 修法错误（ACP 不读 `Resume`；CLI 会传错 id；`HasConversationHistory` 同病）。Phase D 须重做。
> - **C6**：`activeStreams` **不得**改时间线 id（会毁优雅关停）。
> - **C7**：E2E（M1）缺 2 个 agent + acp-mock 路由标签，必失败。
> - **I1**：`setupTestEnv` 只在 handler 包且签名不同 → Phase A/E 测试编译不过。
> - **I2**：`internal/ai/stream_event.go` **不存在**（`StreamEvent` 在 `interface.go:414`）。
> - **I3**：改 `simpleTextPayload` 会打断 12 处 `map[string]string` 断言。
> - **I4**：runner/running-state 接线全缺 → **停止按钮是 no-op**。
>
> **建议**：v1 先做**顺序轮次**（放弃同轮并行），可整体规避 C1/C6/I6/I7（见勘误 M8）。

**Goal:** 让用户在 ClawBench 里创建一个"群"，指定一个智能体当主持人，之后手动加入其他智能体，形成"主持人控场 + AI 决定发言者 + 人类可随时介入"的多智能体辩论式群聊。

**Architecture:** 群是一条 `chat_sessions` 行（`session_type='group'`）并独占时间线；每个成员是一条隐藏的 `chat_sessions` 行（`session_type='group_member'`，`group_id` 指向群），只承载"连接绑定"（agent/transport/external_session_id）。消息**只存一份**，落在群时间线（`chat_history.agent_id` 标记发言人）。新增独立编排器 `internal/service/group_orchestrator.go` 驱动主循环；现有 `run_turn`/ACP 池/runner 只做一处"时间线解耦"（新增 `TimelineSessionID`）。

**Tech Stack:** Go（SQLite、net/http、WS StreamHub）、Vue 3 + TypeScript（Vitest）、Playwright（E2E + acp-mock）。

**设计文档：** `docs/plans/2026-10-04-ai-group-chat-design.md`（决策表 30 条，遇歧义先读它）。

**通用约定：**
- 每个 Task 结束必须 commit（独立小提交）。
- Go 测试命令：`go test ./internal/<pkg>/ -run <TestName> -v`。
- 前端测试：`npx vitest run <file>`。
- 改 HTTP 接口必须同步 `internal/api/openapi.yaml`。
- 新增列必须走 `database.go` 的迁移模式（`pragma_table_info` 检测 + `ALTER TABLE`，见 `database.go:254-276` 范例）。
- **绝不给 ACP `conn.Prompt()` 加超时**；**绝不擅自重启服务**（`./build.sh --restart*` 必须先问用户）。

---

## 阶段总览

| 阶段 | 内容 | 依赖 |
|---|---|---|
| A | Schema 迁移（`chat_history.agent_id`、`chat_sessions.group_id`） | — |
| B | 路由标签解析叶子包 `internal/grouprouting` | — |
| C | `run_turn`/executor 时间线解耦（`TimelineSessionID`） | — |
| D | 成员 resume 修正 | C |
| E | 群存储与 CRUD（创建群/加成员/删成员/列成员） | A |
| F | 编排器主循环 | B、C、D、E |
| G | HTTP API + OpenAPI | E、F |
| H | 前端：`AgentSelectorDrawer` 多选 | — |
| I | 前端：建群入口与主持人选择 | G、H |
| J | 前端：群时间线渲染（发言人气泡/主持人样式） | G |
| K | 前端：成员头像条 + 增删 | G、H |
| L | 前端：同轮并发气泡 | J |
| M | E2E | 全部 |

---

## 阶段 A：Schema 迁移

### Task A1: `chat_history` 增加 `agent_id` 列

**Files:**
- Modify: `internal/service/database.go`（`createTables` 内 `chat_history` 建表 + 迁移段）
- Test: `internal/service/database_group_test.go`（新建）

**Step 1: 写失败测试**

新建 `internal/service/database_group_test.go`：

```go
package service

import (
	"testing"
)

func TestChatHistoryHasAgentIDColumn(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	var n int
	err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='agent_id'",
	).Scan(&n)
	if err != nil {
		t.Fatalf("pragma query failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("chat_history.agent_id column missing (count=%d)", n)
	}
}
```

> 若 `setupTestEnv` 签名不同，照抄同目录既有测试（如 `database_test.go`）的初始化方式。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/service/ -run TestChatHistoryHasAgentIDColumn -v`
Expected: FAIL（count=0）

**Step 3: 实现**

在 `database.go` 的 `CREATE TABLE IF NOT EXISTS chat_history (...)` 中，`backend` 行后加：

```sql
			agent_id TEXT DEFAULT '',
```

并在 `createTables` 的迁移区（`chat_metadata` 迁移段之后）加同样的 `pragma_table_info` 检测 + `ALTER TABLE`：

```go
	// Pre-migration: chat_history.agent_id (group chat speaker attribution).
	// On an existing database the CREATE TABLE above is a no-op, so add the
	// column explicitly. Default '' preserves all existing rows.
	var hasHistoryAgentID int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_history') WHERE name='agent_id'").Scan(&hasHistoryAgentID)
	if hasHistoryAgentID == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_history ADD COLUMN agent_id TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add chat_history.agent_id: %w", err)
		}
	}
```

**Step 4: 运行测试确认通过**

Run: `go test ./internal/service/ -run TestChatHistoryHasAgentIDColumn -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/database.go internal/service/database_group_test.go
git commit -m "feat(group): add chat_history.agent_id column for speaker attribution"
```

---

### Task A2: `chat_sessions` 增加 `group_id` 列

**Files:**
- Modify: `internal/service/database.go`
- Test: `internal/service/database_group_test.go`

**Step 1: 写失败测试**

在 `database_group_test.go` 追加：

```go
func TestChatSessionsHasGroupIDColumn(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	var n int
	err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='group_id'",
	).Scan(&n)
	if err != nil {
		t.Fatalf("pragma query failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("chat_sessions.group_id column missing (count=%d)", n)
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestChatSessionsHasGroupIDColumn -v`
Expected: FAIL

**Step 3: 实现**

`CREATE TABLE IF NOT EXISTS chat_sessions (...)` 中 `session_type` 行后加：

```sql
			group_id TEXT DEFAULT '',
```

迁移段追加：

```go
	var hasSessionGroupID int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='group_id'").Scan(&hasSessionGroupID)
	if hasSessionGroupID == 0 {
		if _, err := store.WriteExec("ALTER TABLE chat_sessions ADD COLUMN group_id TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add chat_sessions.group_id: %w", err)
		}
	}
```

并在建表语句的索引区加成员查询索引：

```sql
		CREATE INDEX IF NOT EXISTS idx_sessions_group ON chat_sessions(group_id, session_type);
```

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestChatSessionsHasGroupIDColumn -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/database.go internal/service/database_group_test.go
git commit -m "feat(group): add chat_sessions.group_id column and index"
```

---

## 阶段 B：路由标签解析叶子包

### Task B1: 创建 `internal/grouprouting` 包与解析函数

**Files:**
- Create: `internal/grouprouting/grouprouting.go`
- Test: `internal/grouprouting/grouprouting_test.go`

**契约（照 `internal/askquestion` 的哲学）**：检测即解析；不可解析时**不剥离**标签（保留原文，绝不丢内容）；解析失败回退轮转。标签名固定 `clawbench-speaker`，结束信号固定 `clawbench-group-end`。**本包不 import 任何 internal 包**（叶子包）。

**Step 1: 写失败测试**

```go
package grouprouting

import (
	"reflect"
	"testing"
)

func TestParseSingleSpeaker(t *testing.T) {
	r := Parse(`<clawbench-speaker>B</clawbench-speaker> 请回应 A 的质疑`)
	if !r.Found {
		t.Fatal("expected Found=true")
	}
	if r.End {
		t.Fatal("expected End=false")
	}
	if !reflect.DeepEqual(r.Speakers, []string{"B"}) {
		t.Fatalf("speakers=%v", r.Speakers)
	}
	if r.Instruction != "请回应 A 的质疑" {
		t.Fatalf("instruction=%q", r.Instruction)
	}
}

func TestParseMultipleSpeakers(t *testing.T) {
	r := Parse(`<clawbench-speaker>A, B ,C</clawbench-speaker> 各自表态`)
	if !reflect.DeepEqual(r.Speakers, []string{"A", "B", "C"}) {
		t.Fatalf("speakers=%v", r.Speakers)
	}
}

func TestParseEndSignal(t *testing.T) {
	r := Parse(`讨论已充分。<clawbench-group-end/>`)
	if !r.End {
		t.Fatal("expected End=true")
	}
}

func TestParseMalformedKeepsRaw(t *testing.T) {
	in := `<clawbench-speaker></clawbench-speaker>`
	r := Parse(in)
	if r.Found {
		t.Fatal("empty payload must not be Found")
	}
	if r.Raw != in {
		t.Fatalf("Raw must equal source, got %q", r.Raw)
	}
}

func TestParseNoTag(t *testing.T) {
	r := Parse("普通发言，没有标签")
	if r.Found || r.End {
		t.Fatal("expected no tag")
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/grouprouting/ -run TestParse -v`
Expected: FAIL（包不存在）

**Step 3: 实现**

```go
// Package grouprouting parses the host agent's routing decision from its
// assistant text in an AI group chat.
//
// The host is an ordinary agent, so its routing decision is expressed as a
// structured tag inside its natural-language output:
//
//	<clawbench-speaker>A,B</clawbench-speaker> 请 B 回应 A 的质疑
//	<clawbench-group-end/> 讨论已充分
//
// Contract (mirrors internal/askquestion): detect-then-parse; an unparseable
// tag is NEVER stripped — Raw is returned so the caller keeps the original
// text visible. Parse failure degrades to round-robin at the orchestrator, it
// never loses content.
//
// The package imports nothing from this module so every layer can depend on it.
package grouprouting

import (
	"regexp"
	"strings"
)

// Result is the outcome of parsing a host message.
type Result struct {
	// Found reports whether a speaker tag was located and understood.
	Found bool
	// Speakers is the ordered list of named members the host wants to speak.
	// Non-empty only when Found is true.
	Speakers []string
	// Instruction is the text following the speaker tag (the host's directive
	// to those members). Empty when absent.
	Instruction string
	// End reports whether the host signalled the discussion is over.
	End bool
	// Raw is the matched tag text, retained for logging. Callers keep the
	// original message text regardless (this package never mutates input).
	Raw string
}

var (
	reSpeaker = regexp.MustCompile(`(?s)<clawbench-speaker>(.*?)</clawbench-speaker>`)
	reEnd     = regexp.MustCompile(`<clawbench-group-end\s*/>`)
)

// Parse locates the host's routing decision in an assistant message.
func Parse(text string) Result {
	var res Result

	if m := reEnd.FindString(text); m != "" {
		res.End = true
		res.Raw = m
	}

	loc := reSpeaker.FindStringSubmatchIndex(text)
	if loc == nil {
		return res
	}
	inner := text[loc[2]:loc[3]]
	res.Raw = text[loc[0]:loc[1]]

	speakers := splitSpeakers(inner)
	if len(speakers) == 0 {
		// Malformed (empty payload): do not claim Found, keep Raw for logs.
		res.End = reEnd.MatchString(text)
		return res
	}
	res.Found = true
	res.Speakers = speakers

	// Instruction = text after the closing tag, trimmed.
	after := strings.TrimSpace(text[loc[1]:])
	res.Instruction = after
	return res
}

// splitSpeakers splits a comma-separated speaker list, trimming each name and
// dropping empties. Order is preserved.
func splitSpeakers(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if name := strings.TrimSpace(p); name != "" {
			out = append(out, name)
		}
	}
	return out
}
```

**Step 4: 运行确认通过**

Run: `go test ./internal/grouprouting/ -v`
Expected: PASS（5 个测试）

**Step 5: Commit**

```bash
git add internal/grouprouting/
git commit -m "feat(group): add grouprouting parser for host routing tags"
```

---

### Task B2: 主持人系统提示词片段

**Files:**
- Modify: `internal/service/chat_request.go`（或新增 `internal/service/group_prompt.go`）
- Test: `internal/service/group_prompt_test.go`

**Step 1: 写失败测试**

```go
package service

import (
	"strings"
	"testing"
)

func TestBuildHostSystemPrompt(t *testing.T) {
	members := []string{"A", "B", "C"}
	p := BuildHostSystemPrompt(members)
	if !strings.Contains(p, "<clawbench-speaker>") {
		t.Fatal("must document speaker tag")
	}
	if !strings.Contains(p, "<clawbench-group-end/>") {
		t.Fatal("must document end signal")
	}
	for _, m := range members {
		if !strings.Contains(p, m) {
			t.Fatalf("member %q missing from prompt", m)
		}
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestBuildHostSystemPrompt -v`
Expected: FAIL

**Step 3: 实现**

新建 `internal/service/group_prompt.go`：

```go
package service

import "strings"

// BuildHostSystemPrompt returns the instruction appended to the host agent's
// system prompt so it emits a parseable routing decision. Members are the
// display names the host may address.
func BuildHostSystemPrompt(members []string) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 你是本次多智能体讨论的主持人。你的职责是控场，而不是代替成员回答问题。\n")
	b.WriteString("每次发言必须包含一个路由标签，指定接下来该谁发言：\n")
	b.WriteString("  <clawbench-speaker>成员名</clawbench-speaker> 给该成员的指令\n")
	b.WriteString("可同时点名多个成员（逗号分隔），他们将并行发言：\n")
	b.WriteString("  <clawbench-speaker>A,B</clawbench-speaker> 请分别表态\n")
	b.WriteString("当讨论已充分、可以收敛时，输出结束标签：\n")
	b.WriteString("  <clawbench-group-end/>\n")
	b.WriteString("可选的成员名：")
	b.WriteString(strings.Join(members, "、"))
	b.WriteString("。\n")
	return b.String()
}
```

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestBuildHostSystemPrompt -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/group_prompt.go internal/service/group_prompt_test.go
git commit -m "feat(group): add host system prompt builder"
```

---

## 阶段 C：`run_turn`/executor 时间线解耦

> ⚠️ 本阶段是**最易出错**的一处。先读设计文档 §3.1。核心：`SessionID` 身兼"连接"与"时间线"两职，必须引入 `TimelineSessionID` 分离，且**逐一核对** `session_executor.go` 每个 `e.cfg.SessionID` 的归属。

### Task C1: `TurnSpec` 与 `RunConfig` 增加 `TimelineSessionID` + 访问器

**Files:**
- Modify: `internal/service/run_turn.go`（`TurnSpec`、`runTurnStart`）
- Modify: `internal/service/session_executor.go`（`RunConfig`、访问器）
- Test: `internal/service/timeline_session_test.go`

**Step 1: 写失败测试**

```go
package service

import "testing"

func TestTurnSpecEffectiveTimelineDefaultsToSessionID(t *testing.T) {
	spec := TurnSpec{SessionID: "sess-1"}
	if got := spec.effectiveTimelineSessionID(); got != "sess-1" {
		t.Fatalf("got %q, want sess-1", got)
	}
}

func TestTurnSpecEffectiveTimelineUsesOverride(t *testing.T) {
	spec := TurnSpec{SessionID: "member-1", TimelineSessionID: "group-1"}
	if got := spec.effectiveTimelineSessionID(); got != "group-1" {
		t.Fatalf("got %q, want group-1", got)
	}
}

func TestRunConfigEffectiveTimeline(t *testing.T) {
	c := RunConfig{SessionID: "m", TimelineSessionID: "g"}
	if got := c.effectiveTimelineSessionID(); got != "g" {
		t.Fatalf("got %q, want g", got)
	}
	c2 := RunConfig{SessionID: "s"}
	if got := c2.effectiveTimelineSessionID(); got != "s" {
		t.Fatalf("got %q, want s", got)
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestTurnSpecEffectiveTimeline -v`
Expected: FAIL（方法未定义）

**Step 3: 实现**

在 `TurnSpec` 的 `SessionID` 字段后加：

```go
	// TimelineSessionID, when non-empty, redirects message persistence and WS
	// broadcast to a different session than the one owning the connection.
	// Group chat sets it to the group session while SessionID is the member
	// row (whose backend connection and external_session_id are used).
	//
	// Empty means "same as SessionID" — every pre-existing caller keeps its
	// exact behavior.
	TimelineSessionID string
```

在 `run_turn.go` 加：

```go
// effectiveTimelineSessionID returns TimelineSessionID when set, else
// SessionID. See TurnSpec.TimelineSessionID.
func (s TurnSpec) effectiveTimelineSessionID() string {
	if s.TimelineSessionID != "" {
		return s.TimelineSessionID
	}
	return s.SessionID
}
```

在 `RunConfig` 的 `SessionID` 后加：

```go
	// TimelineSessionID mirrors TurnSpec.TimelineSessionID: the session whose
	// timeline (chat_history rows, WS broadcast) this run writes to. Empty
	// means SessionID.
	TimelineSessionID string
```

在 `session_executor.go` 加：

```go
// effectiveTimelineSessionID returns TimelineSessionID when set, else
// SessionID. Persistence and WS broadcast use this; connection state
// (external_session_id, transport, model, cancel reason) uses SessionID.
func (c RunConfig) effectiveTimelineSessionID() string {
	if c.TimelineSessionID != "" {
		return c.TimelineSessionID
	}
	return c.SessionID
}
```

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestTurnSpecEffectiveTimeline -v && go test ./internal/service/ -run TestRunConfigEffectiveTimeline -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/run_turn.go internal/service/session_executor.go internal/service/timeline_session_test.go
git commit -m "feat(group): add TimelineSessionID to TurnSpec/RunConfig with accessors"
```

---

### Task C2: `runTurnStart` 使用时间线 id 落库与广播，并透传

**Files:**
- Modify: `internal/service/run_turn.go:277,300,309`
- Test: `internal/service/run_turn_timeline_test.go`

**Step 1: 写失败测试**

用注入式假后端较难在 `runTurnStart` 层面测（它直接建真 backend）。改为**源码级 + 行为级**混合：
- 行为级：验证 `RunConfig` 被正确透传（通过下面 Task C3 的 executor 测试）。
- 此处先加一个**透传断言**：用一个可替换的 backend factory 或直接测 `runTurnStart` 的落库目标（需 mock backend）。

若 mock backend 成本高，改为在 Task C3 用 `RunConfig` 直接构造 executor 测落库目标，本 Task 只做代码改动 + 由 C3 覆盖。**优先 C3 的行为测试。**

**Step 2-4:** 见 C3。

**Step 3: 实现**

`run_turn.go` 改动三处：

```go
	streamingMsgID, err := AddChatMessage(spec.ProjectPath, spec.BackendName, spec.effectiveTimelineSessionID(),
		roleAssistant, string(emptyContent), nil, true, "")
```

```go
	ws.EmitToSession(spec.effectiveTimelineSessionID(), ai.StreamEvent{
		Type:        "stream_start",
		StreamStart: &ai.StreamStartData{MessageID: streamingMsgID},
	})
```

```go
	execCfg := RunConfig{
		Mode:               spec.Mode,
		ProjectPath:        spec.ProjectPath,
		BackendName:        spec.BackendName,
		SessionID:          spec.SessionID,
		TimelineSessionID:  spec.effectiveTimelineSessionID(),
		AgentID:            agentID,
		...
	}
```

**Step 5: Commit**

```bash
git add internal/service/run_turn.go
git commit -m "feat(group): route run_turn persistence and broadcast through timeline id"
```

---

### Task C3: executor 区分时间线与连接语义

**Files:**
- Modify: `internal/service/session_executor.go`（逐一改时间线语义的调用点）
- Test: `internal/service/session_executor_timeline_test.go`

**Step 1: 写失败测试**

测试核心行为：给定 `RunConfig{SessionID:"m", TimelineSessionID:"g"}`，消息落库到 `g`、连接状态读写 `m`。用既有的 executor 测试夹具（参考 `session_executor_batching_test.go` 的初始化）。至少断言：
- `activeStreams` 的键是 `g`（广播目标）。
- `AddChatMessage` / `FinalizeStreamingMessage` 的目标会话是 `g`。

（若既有夹具难以驱动完整流，退一步：把"时间线 vs 连接"的字段选择抽成小函数并单测，例如 `func (e *SessionExecutor) timelineSID() string { return e.cfg.effectiveTimelineSessionID() }`，然后加**源码守卫测试**断言这些调用点用 `timelineSID()`。）

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestExecutorTimeline -v`
Expected: FAIL

**Step 3: 实现**

在 `SessionExecutor` 加辅助方法：

```go
// timelineSID is the session this run writes to (chat_history, WS). It equals
// cfg.SessionID unless a group turn redirected it.
func (e *SessionExecutor) timelineSID() string {
	return e.cfg.effectiveTimelineSessionID()
}
```

逐一修改**时间线语义**调用点，把 `e.cfg.SessionID` 换成 `e.timelineSID()`：

- `activeStreams.Store/Delete`（`:349,369`）→ `e.timelineSID()`（前端订阅群）
- `emitStreamEvent`（`:357`）→ `e.timelineSID()`
- `UpdateStreamingMessage`（`:514,1379,1392`）→ `e.timelineSID()`
- `FinalizeStreamingMessage` / `FinalizeCancelledStreamingMessage`（`:1168,1626,1628`）→ `e.timelineSID()`
- `persistThinkingToDB` / `AppendThinkingSegment`（`:1054,1249,1609`）→ 时间线 id（这些函数按 messageID+sessionID 落库，sessionID 用时间线）
- tool-call 持久化（`:914,968`）→ 时间线 id
- `CreateStreamingMessage`（`:1183,1193`）→ 时间线 id
- `triggerChatSummarization`（`:1644`）→ 时间线 id（摘要属于群会话）
- `UpdateLastRead`（`:1143`）→ 时间线 id
- `newFinalizeTimer`（`:1550`）→ 仅日志，用时间线 id 便于诊断
- `MarkSessionCompacted`（`:565`）→ **连接语义**（压缩是 agent 会话状态）→ 保持 `cfg.SessionID`
- `PatchContextStateMerge`（`:986`）→ **连接语义**（成员自己的 context_state）→ 保持 `cfg.SessionID`

**连接语义保持 `e.cfg.SessionID` 不动**：
- `captureExternalSessionID`（`:851-853`）
- `GetCachedStateByClawbenchSID`（`:1403`）
- `GetSessionTransport`（`:1412`）
- `GetSessionModel`（`:1419`）
- `GetExternalSessionID`（`:1423`）
- `GetAndClearCancelReason`（`:830`）
- `PatchContextStateMerge`（`:986`）
- `MarkSessionCompacted`（`:565`）

> **核对方法**：`grep -n "e.cfg.SessionID" internal/service/session_executor.go`，逐行按上表归类，漏改会导致"消息写进成员会话（用户看不到）"或"连接状态写到群会话（成员 resume 失效）"。

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestExecutorTimeline -v`
Expected: PASS

**Step 5: 全量回归（隔离跑）**

Run: `go test ./internal/service/ -run TestSessionExecutor -v`
Expected: PASS（既有 executor 测试不受影响，因默认 `TimelineSessionID==SessionID`）

**Step 6: Commit**

```bash
git add internal/service/session_executor.go internal/service/session_executor_timeline_test.go
git commit -m "feat(group): split timeline vs connection semantics in SessionExecutor"
```

---

## 阶段 D：成员 resume 修正

> ⚠️ 见设计文档 §4.2 的警示：成员会话无 `chat_history` 行 → `SessionHasAssistant` 恒 false → 成员永不 resume。

### Task D1: 成员回合强制 resume

**Files:**
- Modify: `internal/service/group_orchestrator.go`（成员回合构造请求处，阶段 F 建；本 Task 先做可单测的纯函数）
- Create: `internal/service/group_member_request.go`
- Test: `internal/service/group_member_request_test.go`

**Step 1: 写失败测试**

```go
package service

import "testing"

func TestShouldResumeMember(t *testing.T) {
	// First turn: no ext id, no assistant rows -> no resume.
	if shouldResumeMember("", false) {
		t.Fatal("first turn must not resume")
	}
	// Ext id present -> resume.
	if !shouldResumeMember("ext-123", false) {
		t.Fatal("ext id present must resume")
	}
	// Has assistant rows -> resume.
	if !shouldResumeMember("", true) {
		t.Fatal("assistant rows present must resume")
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestShouldResumeMember -v`
Expected: FAIL

**Step 3: 实现**

新建 `internal/service/group_member_request.go`：

```go
package service

// shouldResumeMember decides whether a group member's turn should resume its
// persistent backend session.
//
// Member session rows carry NO chat_history rows (all messages live in the
// group timeline), so BuildChatRequest's SessionHasAssistant check is always
// false for them. Without this override a member would be treated as a brand
// new session every turn and could never build native memory — defeating the
// "independent persistent connection" design.
//
// A member resumes when it has a captured external_session_id OR the caller
// reports it already produced assistant output. Both empty/false on the first
// turn, which is correct (nothing to resume yet).
func shouldResumeMember(externalSessionID string, hasAssistant bool) bool {
	return externalSessionID != "" || hasAssistant
}
```

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestShouldResumeMember -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/group_member_request.go internal/service/group_member_request_test.go
git commit -m "feat(group): add member resume decision helper"
```

---

## 阶段 E：群存储与 CRUD

### Task E1: 群与成员的数据访问

**Files:**
- Create: `internal/service/group_store.go`
- Test: `internal/service/group_store_test.go`

**Step 1: 写失败测试**

覆盖：建群返回群 id + 主持人成员行；加成员返回成员行；列成员按顺序；删成员把成员行 `archived=1`（保留行与历史）；查群的主持人。

```go
func TestCreateGroupAndMembers(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	project := env.ProjectPath

	groupID, hostMemberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if groupID == "" || hostMemberID == "" {
		t.Fatal("empty ids")
	}

	m, err := AddGroupMember(project, groupID, "claude", "agent-a")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("want 2 members, got %d", len(members))
	}

	if err := RemoveGroupMember(groupID, m); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	members, _ = ListGroupMembers(groupID)
	if len(members) != 1 {
		t.Fatalf("want 1 member after removal, got %d", len(members))
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestCreateGroupAndMembers -v`
Expected: FAIL

**Step 3: 实现**

新建 `internal/service/group_store.go`。复用 `CreateSession`（`chat.go:1822`，`sessionType` 参数可传 `"group"` / `"group_member"`）与 `SetSessionGroupID`。要点：
- `CreateGroup` → `CreateSession(project, backend, title, hostAgentID, "", "default", "group")`，然后为主持人建成员行 `CreateSession(..., "group_member")` 并 `UPDATE chat_sessions SET group_id=?`，返回两个 id。
- `AddGroupMember` 同上建成员行。
- `ListGroupMembers(groupID)` → `SELECT id, agent_id, backend, title, archived FROM chat_sessions WHERE group_id=? AND session_type='group_member' ORDER BY created_at ASC`（含 archived=1 的"已离场"）。
- `RemoveGroupMember` → `UPDATE chat_sessions SET archived=1 WHERE id=? AND group_id=?`（**保留行**，历史发言在群里不受影响）。
- `GetGroupHost(groupID)` → 主持人 agent id（存哪见下）。

**主持人存储**：群行的 `context_state` JSON 加 `host_member_id`。实现 `SetGroupHostMember(groupID, memberID)` / `GetGroupHostMember(groupID)`，用 `PatchContextStateMerge`（`session_executor.go:986` 同款）。

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestCreateGroupAndMembers -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/group_store.go internal/service/group_store_test.go
git commit -m "feat(group): add group and member store CRUD"
```

---

### Task E2: 会话列表过滤成员行

**Files:**
- Modify: `internal/service/chat.go`（`sessionsQueryBase:1201`、`overviewSessionsQuery:1209`、`pagedSessionsQueryBase:1218`）
- Test: `internal/service/group_store_test.go`

**Step 1: 写失败测试**

```go
func TestGroupMembersHiddenFromSessionList(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	project := env.ProjectPath

	_, hostMember, err := CreateGroup(project, "g", "codebuddy", "agent-host")
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := GetSessions(project, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s.ID == hostMember {
			t.Fatal("group member must not appear in session list")
		}
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestGroupMembersHiddenFromSessionList -v`
Expected: FAIL（成员泄漏）

**Step 3: 实现**

把三条 query 的 `AND s.session_type = 'chat'` 改为：

```sql
AND s.session_type IN ('chat', 'group')
```

（群要出现在列表，成员不出现。）同时检查 `internal/store/session_queries.go:126` 的 `GetRecentSessions` 等硬编码 `session_type = 'chat'` 处，按需同样放宽为 `IN ('chat','group')`。用 `grep -rn "session_type = 'chat'" internal/` 找全部，逐一判断"该处是否应显示群"。

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestGroupMembersHiddenFromSessionList -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/chat.go internal/store/session_queries.go internal/service/group_store_test.go
git commit -m "feat(group): hide group members from session list, show groups"
```

---

## 阶段 F：编排器主循环

### Task F1: 增量注入文本构造

**Files:**
- Create: `internal/service/group_inject.go`
- Test: `internal/service/group_inject_test.go`

**Step 1: 写失败测试**

给定群时间线（含 user、成员 A、成员 B、自己 的消息），断言：只含 `id > cursor` 且 `agent_id != self` 的消息；渲染为带发言人名字的文本；主持人指令单独成段。

```go
func TestBuildMemberInjection(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 1, Role: "user", Content: `{"blocks":[{"type":"text","text":"问题Q"}]}`},
		{ID: 2, Role: "assistant", Content: `{"blocks":[{"type":"text","text":"A1"}]}`}, // A
		{ID: 3, Role: "assistant", Content: `{"blocks":[{"type":"text","text":"B1"}]}`}, // B
	}
	names := map[int64]string{2: "A", 3: "B"}
	got := buildInjectionText(msgs, 1, "B", names, "请回应 A")
	if !strings.Contains(got, "A: A1") {
		t.Fatalf("missing A's speech: %q", got)
	}
	if strings.Contains(got, "B: B1") {
		t.Fatalf("must exclude self speech: %q", got)
	}
	if !strings.Contains(got, "请回应 A") {
		t.Fatalf("missing host instruction")
	}
}
```

**Step 2-4:** TDD 循环实现 `buildInjectionText(msgs []model.ChatMessage, cursor int64, selfAgentID string, names map[int64]string, instruction string) string`。文本抽取复用 `ExtractPlainText`（`chat.go` 已有）。消息按 `agent_id` 映射发言人名（`names` 以消息 id 为键或 agent_id 为键——实现时统一）。

**Step 5: Commit**

```bash
git add internal/service/group_inject.go internal/service/group_inject_test.go
git commit -m "feat(group): add incremental context injection builder"
```

---

### Task F2: 编排器骨架 + 单轮（注入式假 run_turn）

**Files:**
- Create: `internal/service/group_orchestrator.go`
- Test: `internal/service/group_orchestrator_test.go`

**设计**：编排器通过一个**可注入的 run 函数**执行成员回合，便于单测：

```go
// groupTurnRunner runs one member's turn. Production wires it to runTurn with
// a TurnSpec whose SessionID is the member row and TimelineSessionID the group.
type groupTurnRunner func(spec groupMemberTurn) groupMemberResult
```

**Step 1: 写失败测试**

覆盖：主持人输出路由标签 → 选人 → 成员发言 → 落群时间线；结束信号停止；解析失败回退轮转；最大轮数兜底；成员失败不终止群。

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestGroupOrchestrator -v`
Expected: FAIL

**Step 3: 实现**

`group_orchestrator.go` 要点（伪代码见设计文档 §5.1）：
- `RunGroupTurn(ctx, groupID, userMessage)`：写用户消息到群 → 循环。
- 每轮：记录高水位 `H`；主持人发言（注入 + 解析）；解析失败回退轮转；被点名成员**同轮并发**（`errgroup` 或 `sync.WaitGroup`）；各自 `seen_cursor = H`；检查结束/最大轮数/抢占。
- 主持人/成员回合都构造 `TurnSpec{SessionID: memberRowID, TimelineSessionID: groupID, ChatReq: ...}`，`ChatReq` 里 `Resume` 用 `shouldResumeMember`。
- 主持人回合的系统提示 = 成员系统提示 + `BuildHostSystemPrompt(memberNames)`。
- 主持人发言的消息级标记：在写群消息时把 `agent_id=hostAgentID` 并在 `content` 信封加 `"meta":{"role":"host"}`（见 J 阶段读取）。

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestGroupOrchestrator -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/group_orchestrator.go internal/service/group_orchestrator_test.go
git commit -m "feat(group): add group orchestrator main loop"
```

---

### Task F3: 抢占与取消

**Files:**
- Modify: `internal/service/group_orchestrator.go`
- Test: `internal/service/group_orchestrator_test.go`

**Step 1: 写失败测试**：模拟用户在成员发言中途发消息，断言当前成员 runner 被取消、用户消息进时间线、主持人重新选人。

**Step 3: 实现**：复用 `CancelSession(memberRowID)`（`session_runtime.go:770`）取消当前成员；用户消息经 `AddChatMessage(groupID)` 落库；循环检测到新用户消息即重新进入主持人阶段。

**Step 5: Commit**

```bash
git add internal/service/group_orchestrator.go internal/service/group_orchestrator_test.go
git commit -m "feat(group): add preemption on human interjection"
```

---

## 阶段 G：HTTP API + OpenAPI

### Task G1: 群端点

**Files:**
- Create: `internal/handler/group.go`
- Modify: `internal/handler/handler.go`（`RegisterRoutes`）
- Modify: `internal/api/openapi.yaml`
- Test: `internal/handler/group_test.go`

**端点**（字段名从 handler 代码抄，勿望文生义）：
- `POST /api/group/create` — `{title, hostAgentId}` → `{ok, groupId, hostMemberId}`
- `POST /api/group/members` — `{groupId, agentId}` → `{ok, memberId}`
- `DELETE /api/group/members` — `{groupId, memberId}` → `{ok}`
- `GET /api/group/members?groupId=` → `{ok, members:[{id, agentId, name, backend, left}]}`
- `POST /api/group/chat` — `{groupId, message, ...}` → 触发编排器（可复用 `/api/ai/chat` 语义）。

**Step 1-5:** TDD：先写 handler 测试（`httptest`），确认 404，再注册路由实现。OpenAPI 同步 `internal/api/openapi.yaml`，跑 `go test ./internal/handler/ -run TestOpenAPIDrift`。

**Commit:**

```bash
git add internal/handler/group.go internal/handler/handler.go internal/api/openapi.yaml internal/handler/group_test.go
git commit -m "feat(group): add group HTTP endpoints and OpenAPI docs"
```

---

## 阶段 H：前端 `AgentSelectorDrawer` 多选

### Task H1: 加 `multiple` 模式

**Files:**
- Modify: `web/src/components/common/AgentSelectorDrawer.vue`
- Test: `web/src/components/common/__tests__/AgentSelectorDrawer.test.ts`

**Step 1: 写失败测试**：`multiple=true` 时渲染 checkbox、点击不关闭、`update:modelValue` 发出数组、可勾多个。

**Step 3: 实现**：加 `multiple?: boolean` prop；`modelValue` 类型放宽为 `string | string[]`；多选时行前渲染 checkbox，`handleSelect` 切换数组项且不关闭；单选路径行为完全不变（回归测试钉住）。

**Step 5: Commit**

```bash
git add web/src/components/common/AgentSelectorDrawer.vue web/src/components/common/__tests__/AgentSelectorDrawer.test.ts
git commit -m "feat(group): support multi-select in AgentSelectorDrawer"
```

---

## 阶段 I：前端建群入口与主持人选择

### Task I1: 列表头"建群"按钮

**Files:**
- Modify: `web/src/components/session/SessionListHeader.vue`（`data-action="create"` 旁）
- Modify: `web/src/components/session/SessionDrawer.vue`、`SessionSidebar.vue`（转发事件）
- Modify: `web/src/App.vue`（处理建群事件）
- Test: 组件测试 + 后续 E2E

**Step 3: 实现**：加 `data-action="create-group"` 按钮，emit `create-group`；宿主转发到 App，打开"选主持人"抽屉（复用 H1 的多选组件，此处单选）。

**Commit:**

```bash
git add web/src/components/session/ web/src/App.vue
git commit -m "feat(group): add create-group entry point"
```

### Task I2: 建群调用与跳转

**Files:**
- Modify: `web/src/composables/useChatSession.ts`（加 `createGroup(hostAgentId, title?)`）
- Test: `web/src/composables/__tests__/useChatSession.group.test.ts`

**Step 3: 实现**：`POST /api/group/create` → 成功后 `switchSession(groupId)`；标题留空时前端用主持人名拼接占位（`<hostName> 的群聊`）。

**Commit:**

```bash
git add web/src/composables/useChatSession.ts web/src/composables/__tests__/useChatSession.group.test.ts
git commit -m "feat(group): create group and switch into it"
```

---

## 阶段 J：前端群时间线渲染

### Task J1: 消息类型加 `agentId`，气泡显示发言人

**Files:**
- Modify: `web/src/utils/chatStreamUtils.ts`（`ChatMessage` 加 `agentId?`）
- Modify: `web/src/components/chat/ChatMessageItem.vue`
- Modify: `web/src/components/chat/ChatMessageList.vue`
- Test: 组件测试

**Step 3: 实现**：`agent_id` 非空时气泡显示 `AgentIcon` + 名字（复用 `useAgents` 的 `getAgentName`）；`meta.role==="host"` 时用居中特殊样式。

**Commit:**

```bash
git add web/src/utils/chatStreamUtils.ts web/src/components/chat/ web/src/composables/useChatStream.ts
git commit -m "feat(group): render speaker attribution and host style in timeline"
```

---

## 阶段 K：前端成员头像条 + 增删

### Task K1: 头像条与加成员

**Files:**
- Create: `web/src/components/chat/GroupMemberBar.vue`
- Modify: `web/src/components/chat/ChatPanelContent.vue`
- Test: 组件测试

**Step 3: 实现**：头部横向头像条；尾部 `+` 打开 H1 多选抽屉批量加成员；成员管理面板可移除（调 `DELETE /api/group/members`）；"已离场"成员灰显。

**Commit:**

```bash
git add web/src/components/chat/GroupMemberBar.vue web/src/components/chat/ChatPanelContent.vue
git commit -m "feat(group): add member avatar bar with add/remove"
```

---

## 阶段 L：同轮并发气泡

### Task L0: 流式事件携带 message_id（后端）

> **已核实**：`content`/`thinking` 事件当前**不带** `message_id`——`simpleTextPayload`（`internal/ws/stream_hub.go:345-369`）只有 `content`/`text` + `parent_tool_call_id`/`member_name`/`member_color`。同轮 N 个成员并发流式时，前端无法把 delta 路由到各自的占位气泡。**必须先补这个字段。**

**Files:**
- Modify: `internal/ai/stream_event.go`（`StreamEvent` 加 `MessageID int64`）
- Modify: `internal/service/session_executor.go`（emit 前填入 `e.cfg.StreamingMessageID`）
- Modify: `internal/ws/stream_hub.go`（`simpleTextPayload` 加 `message_id`，用 `map[string]any` 而非 `map[string]string`）
- Test: `internal/ws/stream_hub_test.go`

**Step 1: 写失败测试**

```go
func TestSimpleTextPayloadCarriesMessageID(t *testing.T) {
	p := simpleTextPayload(ai.StreamEvent{Type: "content", Content: "hi", MessageID: 42})
	m, ok := p.(map[string]any)
	if !ok {
		t.Fatalf("payload type %T", p)
	}
	if m["message_id"] != int64(42) {
		t.Fatalf("message_id=%v", m["message_id"])
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/ws/ -run TestSimpleTextPayloadCarriesMessageID -v`
Expected: FAIL

**Step 3: 实现**

- `StreamEvent` 加 `MessageID int64`（字段注释说明：群聊同轮并发时用于把 delta 路由到对应占位气泡；单会话路径为 0，前端回退到"唯一 streaming 行"逻辑，行为不变）。
- `simpleTextPayload` 改为 `map[string]any`，`MessageID > 0` 时写 `payload["message_id"] = event.MessageID`。
- executor 在 emit content/thinking 事件前设 `event.MessageID = e.cfg.StreamingMessageID`（或由 coalescer 注入——选一处，保持单一来源）。

**Step 4: 运行确认通过**

Run: `go test ./internal/ws/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ai/stream_event.go internal/service/session_executor.go internal/ws/stream_hub.go internal/ws/stream_hub_test.go
git commit -m "feat(group): carry message_id on content/thinking stream events"
```

### Task L1: 支持多路并发流式占位

**Files:**
- Modify: `web/src/composables/useChatStream.ts`（`findStreamingMsg` 单一假设 → 按 `message_id` 维护多锚点）
- Test: `web/src/composables/__tests__/useChatStream.concurrent.test.ts`

**Step 3: 实现**：`stream_start` 的 `message_id` 是群时间线里每个并发发言各自的占位行；前端按 `message_id` 维护 `Map<number, placeholder>`；content/thinking 事件带 `message_id`（L0 已补）时按该 id 路由；`message_id` 缺失（单会话旧路径）时回退到"唯一 streaming 行"逻辑，行为不变。

**Commit:**

```bash
git add web/src/composables/useChatStream.ts web/src/composables/__tests__/useChatStream.concurrent.test.ts
git commit -m "feat(group): render concurrent member streams"
```

---

## 阶段 M：E2E

### Task M1: 群聊端到端

**Files:**
- Create: `e2e/specs/group-chat.spec.ts`

**前置：** `go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock`

**场景：** 建群（选主持人）→ 加 2 个成员 → 发一条消息 → 断言多成员气泡按序出现 + 主持人消息可见 + 人类抢占生效。

Run: `npx playwright test --config e2e/playwright.config.ts --project=chromium-coverage e2e/specs/group-chat.spec.ts`
Expected: PASS

**Commit:**

```bash
git add e2e/specs/group-chat.spec.ts
git commit -m "test(group): add group chat e2e spec"
```

---

## 完成标准

- 所有阶段 Task 完成且各自 commit。
- `go test ./internal/...` 全绿（隔离跑，避免与并发 agent 争抢）。
- `npx vitest run` 全绿。
- `./scripts/pre-push-checks.sh` 通过。
- 纯前端改动后 `npm run build` 供用户即时测试。
- OpenAPI 与 `internal/api/openapi.yaml` 同步，`TestOpenAPIDrift` 通过。
