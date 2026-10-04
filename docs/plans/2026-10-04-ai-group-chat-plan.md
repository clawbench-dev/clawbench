# AI 群聊（Group Chat）实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

> ## ⚠️ 评审勘误（2026-10-04）— 实现前必读
>
> 本计划经 Superpower code-reviewer 评审，发现 **7 Critical / 10 Important**，已逐条核对代码属实。
>
> **✅ v1 决策：顺序轮次。** 放弃同轮并行（决策 #22 修订）。这直接消除 **C1/C6/I6/I7**（全是并行专属问题），并**移除 L0/L1（并发气泡）整个阶段**——被点名者按顺序依次发言，任一时刻只有一条流式行在写群时间线，`UpdateStreamingMessage` 的 `ORDER BY id DESC LIMIT 1` 恰好命中正确行。
>
> **实现前仍须修订的项**（详见设计文档 §12）：
> - **C2**：`agent_id` 无写/读路径（`AddChatMessage` 无参数、`model.ChatMessage` 无字段）。Phase A/F/J 需贯穿。
> - **C3**：`session_type` 影响 17 处（含**参数化**查询 grep 不到）。Phase E2 的 grep 清单错误且不全。
> - **C4**：Phase C 漏了 `run_turn.go` 的 `failTurn`（`:196`）与 metadata（`:340`）。
> - **C5**：成员 resume 修法错误（ACP 不读 `Resume`；CLI 会传错 id；`HasConversationHistory` 同病）。Phase D 须重做。
> - **C7**：E2E（M1）缺 2 个 agent + acp-mock 路由标签，必失败。
> - **I1**：`setupTestEnv` 只在 handler 包且签名不同 → Phase A/E 测试编译不过。
> - **I2**：`internal/ai/stream_event.go` **不存在**（`StreamEvent` 在 `interface.go:414`）。
> - **I4**：runner/running-state 接线全缺 → **停止按钮是 no-op**。
>
> **v2 候选**：同轮并行（须先按 §12 C1 增加按消息 id 的流式写入原语 + L0/L1）。

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
| F | 编排器主循环（**顺序轮次**） | B、C、D、E |
| G | HTTP API + OpenAPI | E、F |
| H | 前端：`AgentSelectorDrawer` 多选 | — |
| I | 前端：建群入口与主持人选择 | G、H |
| J | 前端：群时间线渲染（发言人气泡/主持人样式） | G |
| K | 前端：成员头像条 + 增删 | G、H |
| M | E2E | 全部 |
| ~~L~~ | ~~前端：同轮并发气泡~~ → **v2**（随并行一起做，见头部勘误） | — |

---

## 阶段 A：Schema 迁移

### Task A1: `chat_history` 增加 `agent_id` 列

**Files:**
- Modify: `internal/service/database.go`（`createTables` 内 `chat_history` 建表 + 迁移段）
- Test: `internal/service/database_group_test.go`（新建）

**Step 1: 写失败测试**

新建 `internal/service/database_group_test.go`。**⚠️ I1 勘误**：`setupTestEnv` 只存在于 `internal/handler` 包且签名是 `(*testEnv, func())`（无 `.Cleanup()`、字段是 `ProjectDir`）。**service 包**测试必须照 `internal/service/database_test.go:330-345` 的模式（`model.DataDir` 指向临时目录 + `store.SnapshotDBForTest()` + `InitDB()` + `store.UnsafeDBForTest()`）：

```go
package service

import (
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

func TestChatHistoryHasAgentIDColumn(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()

	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer store.Close()

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

### Task A3: 打通 `agent_id` 的写入与读取路径（评审 C2）

> **C2 勘误（Critical）**：`chat_history.agent_id` 目前**既无写入也无读取**——`AddChatMessage`（`chat.go:624`）无参数、`insertChatMessageTx`（`chat.go:764`）INSERT 无该列、`scanMessages`（`chat.go:131`）与所有 SELECT（`chat.go:70,343,361`）未选它、`model.ChatMessage`（`model/chat.go:124`）**无 `AgentID` 字段**。不打通则 Phase F/J 无法实现。

**Files:**
- Modify: `internal/model/chat.go`（`ChatMessage` 加 `AgentID string \`json:"agentId,omitempty"\``）
- Modify: `internal/service/chat.go`（`AddChatMessage`/`insertChatMessageTx` 贯穿 agentID；所有 SELECT + `scanMessages` 补列）
- Test: `internal/service/database_group_test.go`

**Step 1: 写失败测试**

```go
func TestAddChatMessagePersistsAgentID(t *testing.T) {
	// 用 setupDB(t)（chat_test.go:232 的内存 schema）或 InitDB 模式
	db := setupDB(t)
	_ = db
	project := "/tmp/grouptest"
	_ = InitDB // 视具体夹具而定

	sid := helperCreateSession(t, project, "codebuddy", "t")
	id, err := AddChatMessageWithAgent(project, "codebuddy", sid, "assistant", `{"blocks":[]}`, nil, false, "", "agent-x")
	if err != nil {
		t.Fatal(err)
	}
	msgs, _, err := GetChatHistoryPaged(project, "codebuddy", sid, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if m.ID == id {
			found = true
			if m.AgentID != "agent-x" {
				t.Fatalf("AgentID=%q, want agent-x", m.AgentID)
			}
		}
	}
	if !found {
		t.Fatal("message not returned")
	}
}
```

> 实现时按既有测试夹具（`chat_test.go` 的 `setupDB` + `helperCreateSession`）调整；关键断言是 **`ChatMessage.AgentID` 能写能读**。

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestAddChatMessagePersistsAgentID -v`
Expected: FAIL（字段/参数不存在）

**Step 3: 实现**

- `model.ChatMessage` 加 `AgentID string`。
- 新增 `AddChatMessageWithAgent(...agentID string)`，内部把 `agentID` 传给 `insertChatMessageTx`；`AddChatMessage` 保留原签名并转调 `AddChatMessageWithAgent(..., "")`（避免改动所有既有调用点）。
- `insertChatMessageTx` 的 INSERT 增加 `agent_id` 列与占位符。
- `GetChatHistoryPaged` 三条 SELECT、`GetMessagesBySessionID`/`Raw`（`chat.go:343,361`）、`scanMessages` 均补 `agent_id` 并写入 `ChatMessage.AgentID`。
- handler 返回 JSON 已由 `model.ChatMessage` 的 tag 自动带上。

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestAddChatMessagePersistsAgentID -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/chat.go internal/service/chat.go internal/service/database_group_test.go
git commit -m "feat(group): thread agent_id through message write and read paths"
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

`runTurnStart` 直接建真 backend，难单测。**本 Task 的失败测试放在 C3**（直接构造 executor 断言落库/广播目标）。本 Task 只做代码改动，由 C3 的行为测试覆盖。**不要**用"调用点是否用 `effectiveTimelineSessionID()`"的源码守卫（**I6**：会假绿）。

**Step 2: 运行 C3 测试确认失败**

Run: `go test ./internal/service/ -run TestExecutorTimeline -v`
Expected: FAIL（此时 C3 测试已存在）

**Step 3: 实现**

`run_turn.go` 改动**五处**（**C4 勘误**：原计划只列了两处，漏了 `failTurn` 与 metadata）：

```go
	streamingMsgID, err := AddChatMessageWithAgent(spec.ProjectPath, spec.BackendName, spec.effectiveTimelineSessionID(),
		roleAssistant, string(emptyContent), nil, true, "", agentID)  // C2: 带 agentID
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

**C4 补两处**（否则成员/主持人的早失败错误与 metadata 落进隐藏成员会话，用户看不到）：

```go
// failTurn (run_turn.go:196)：错误广播与警告行都用时间线 id
func (s TurnSpec) failTurn(err error, key string) TurnResult {
	...
	emitDrainEvent(s.effectiveTimelineSessionID(), ai.StreamEvent{Type: eventTypeError, Error: errMsg})
	...
	AddChatMessageWithAgent(s.ProjectPath, s.BackendName, s.effectiveTimelineSessionID(), roleAssistant, string(errContent), nil, false, "", "")
```

```go
// runTurnFinalize (run_turn.go:340)：metadata 广播用时间线 id
emitDrainEvent(at.spec.effectiveTimelineSessionID(), ai.StreamEvent{Type: contentKeyMetadata, Meta: at.runResult.Metadata})
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
- `emitStreamEvent` 广播到 `g`。
- **`activeStreams` 的键是 `m`（成员行，非 `g`）** —— **C6 勘误**：`activeStreams` 是**服务端**注册表（供 `FlushStreamingNow`/`WaitStreamsDrained`），与前端订阅无关，必须保持 `cfg.SessionID`，否则并发成员互相覆盖、优雅关停丢尾部。
- `UpdateStreamingMessage` / `FinalizeStreamingMessage` 的目标会话是 `g`。

> **I6 勘误**：不要用"调用点是否用了 `timelineSID()`"的源码守卫——它在 C1 冲突仍在时照样绿（本仓有源码守卫被中转变量绕过的先例）。用**行为测试**：直接构造 executor（或最小驱动）断言落库/广播目标。

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

- `emitStreamEvent`（`:357`）→ `e.timelineSID()`
- `UpdateStreamingMessage`（`:514,1379,1392`）→ `e.timelineSID()`
- `FinalizeStreamingMessage` / `FinalizeCancelledStreamingMessage`（`:1168,1626,1628`）→ `e.timelineSID()`
- `persistThinkingToDB` / `AppendThinkingSegment`（`:1054,1249,1609`）→ 时间线 id（这些函数按 messageID+sessionID 落库，sessionID 用时间线）
- tool-call 持久化（`:914,968`）→ 时间线 id
- `CreateStreamingMessage`（`:1183,1193`）→ 时间线 id
- `triggerChatSummarization`（`:1644`）→ 时间线 id（摘要属于群会话）
- `newFinalizeTimer`（`:1550`）→ 仅日志，用时间线 id 便于诊断

**保持 `e.cfg.SessionID`（连接语义）不动**：

- **`activeStreams.Store/Delete`（`:349,369`）→ 保持 `cfg.SessionID`**（**C6 勘误**：服务端注册表，按正在执行的成员行键控）
- `captureExternalSessionID`（`:851-853`）
- `GetCachedStateByClawbenchSID`（`:1403`）
- `GetSessionTransport`（`:1412`）
- `GetSessionModel`（`:1419`）
- `GetExternalSessionID`（`:1423`）
- `GetAndClearCancelReason`（`:830`）
- `PatchContextStateMerge`（`:986`）
- `MarkSessionCompacted`（`:565`）
- **`UpdateLastRead`（`:1143`）→ 保持 `cfg.SessionID`**（**I10 勘误**：改时间线 id 会让任一成员回合把**群**标记已读，群徽章永不亮；v1 群未读按成员会话聚合，见设计 §12 I10）

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

> ⚠️ 见设计文档 §4.2 与 §12 **C5（Critical）**。原计划"成员回合强制置 `resume=true`"是**错的**，须按下述重做。

### Task D1: 成员回合的请求构造（resume + HasConversationHistory + AssistantMessageCount 三处）

**C5 勘误（必读）**：
1. **ACP 根本不读 `ChatRequest.Resume`** —— ACP 的 resume 由 DB `external_session_id` 经 `GetOrCreateConn`/`ensureAliveWithSession`（`acp_conn_lifecycle.go:280`）驱动。所以对 ACP 成员，**真正要做的是让成员行的 `external_session_id` 被正确写入/读取**（Phase F 的 `captureExternalSessionID` 走连接语义即自动完成），"强制 resume"是 no-op。
2. **对 CLI 成员，事后翻 `Resume=true` 会传错 id** —— `resolveResumeSessionID`（`chat_request.go:267`）在 `resume=false` 时返回 `SessionID=memberUUID, Resume=false`；`BuildBaseStreamArgs`（`common_stream.go:21`）会 `--resume <memberUUID>`（CLI 没见过）。**修法**：成员回合的 `ChatRequest` 必须让 `SessionID = GetExternalSessionID(memberRow)`（非空时）且 `Resume = (extID != "")`。
3. **`HasConversationHistory` 同病**（`chat_request.go:136` 由 `GetChatMessageCount` 推出，成员恒 false）⇒ `shouldNewSessionFallback(false)=true`（`acp_backend.go:335`）⇒ ACP 瞬时断连时静默新建会话丢上下文。成员回合须把 `HasConversationHistory` 置为 `extID != ""`。`AssistantMessageCount` 同理（影响系统提示重注入）。

**Files:**
- Create: `internal/service/group_member_request.go`
- Test: `internal/service/group_member_request_test.go`

**Step 1: 写失败测试**

```go
package service

import "testing"

func TestResolveMemberResume(t *testing.T) {
	// First turn: no ext id -> fresh, no resume, empty session id.
	sid, resume, hasHistory := resolveMemberResume("member-1", "", 0)
	if resume || hasHistory || sid != "member-1" {
		t.Fatalf("first turn: sid=%q resume=%v hasHistory=%v", sid, resume, hasHistory)
	}
	// Has ext id -> resume with ext id, history true.
	sid, resume, hasHistory = resolveMemberResume("member-1", "ext-9", 0)
	if !resume || !hasHistory || sid != "ext-9" {
		t.Fatalf("resume turn: sid=%q resume=%v hasHistory=%v", sid, resume, hasHistory)
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestResolveMemberResume -v`
Expected: FAIL

**Step 3: 实现**

新建 `internal/service/group_member_request.go`：

```go
package service

// resolveMemberResume computes the (SessionID, Resume, HasConversationHistory)
// triple for a group member's turn.
//
// Member session rows carry NO chat_history rows (all messages live in the
// group timeline), so every chat_history-derived signal BuildChatRequest uses
// is wrong for them:
//   - SessionHasAssistant -> false  => Resume=false
//   - GetChatMessageCount -> 0      => HasConversationHistory=false
//
// The member's real memory signal is its own external_session_id (written by
// captureExternalSessionID on the member row, which uses connection semantics).
//
// For ACP the returned SessionID is ignored (the pool owns mapping via the
// member row's ClawBench UUID), but Resume must still be truthful for the
// executor's bookkeeping. For CLI the SessionID MUST be the external id — an
// agent that has never seen memberUUID cannot --resume it.
//
// assistantCount is the caller's own count if it tracks one (0 for members).
func resolveMemberResume(memberRowID, externalSessionID string, assistantCount int) (sessionID string, resume, hasConversationHistory bool) {
	if externalSessionID == "" {
		return memberRowID, false, false
	}
	return externalSessionID, true, true
}
```

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestResolveMemberResume -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/group_member_request.go internal/service/group_member_request_test.go
git commit -m "feat(group): add member resume/history resolution"
```

> **接线（Phase F）**：成员回合构造 `ai.ChatRequest` 后，用 `resolveMemberResume` 的结果覆盖 `ChatRequest.SessionID`/`Resume`/`HasConversationHistory`。主持人回合同理。此覆盖**必须**在 `BuildChatRequest` 之后、`ExecuteStream` 之前。

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

**Step 1: 写失败测试**（**I1 勘误**：service 包用 `InitDB()`/`store.SnapshotDBForTest()` 模式，非 `setupTestEnv`）

```go
func TestGroupMembersHiddenFromSessionList(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir, model.DataDir = origBinDir, origDataDir }()

	restoreDB := store.SnapshotDBForTest()
	defer restoreDB()
	if err := InitDB(); err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	project := "/tmp/grouplist" // 需先 ProjectIDForPath 建 project
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

（群要出现在列表，成员不出现。）

**C3 勘误（Critical）**：**不要**只用 `grep -rn "session_type = 'chat'" internal/` 找——`GetRecentSessions`（`store/session_queries.go:133`）与 `SearchSessionsByTitle`（`:250`）是**参数化**的（`WHERE s.session_type = ?` 传 `"chat"`），字面量 grep **找不到**。必须用：

```bash
grep -rn "session_type" internal/ | grep -v "_test.go"
```

逐处判断"该处是否应显示群"，已知需处理点（不止这三条）：`chat.go:1478,1641,2007,2014,2046,2068`、`continue_conversation.go:48,141,161,430`、`session_command.go:46,71`、`handler/session_resume.go:358,459`、`store/session_queries.go:133,250`。**特别注意 `GetSessionCount`（`chat.go:2046`）喂会话上限门（`chat_session.go:174`）——群应计入还是不计入须决策，否则群绕过上限。**

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestGroupMembersHiddenFromSessionList -v`
Expected: PASS

**Step 4b: 补 search/browse/overview 守卫测试**

新增测试断言群**出现**在 `GetRecentSessions`/`SearchSessionsByTitle`/`GetOverviewSessions`，成员**不出现**。仅测 `GetSessions` 会漏掉参数化查询。

**Step 5: Commit**

```bash
git add internal/service/chat.go internal/store/session_queries.go internal/service/continue_conversation.go internal/service/group_store_test.go
git commit -m "feat(group): show groups and hide members across list/search/browse/overview"
```

---

## 阶段 F：编排器主循环（顺序轮次）

### Task F0: 运行态与取消接线（评审 I4/I5）

> **I4 勘误（Critical）**：`runTurn` 自身**不**注册运行态——`TryClaimSessionRun`/`SetSessionRunning` 在 `handler/chat.go:469,527`。若不接线：(a) `IsSessionRunning(memberID)` 恒 false ⇒ ACP 空闲回收（`acp_pool.go:481`）可能杀掉长辩论中的成员连接；(b) 群未注册 running ⇒ 前端对 `groupID` 发 cancel 时 `CancelSession(groupID)` 找不到 runner，**静默 no-op**（停止按钮失效）；(c) F3 的 `CancelSession(memberRowID)` 同样需要成员已注册。

**Files:**
- Modify: `internal/service/group_orchestrator.go`（编排器进入成员回合前 `RegisterExternalExecution` / `SetSessionRunning`）
- Test: `internal/service/group_orchestrator_test.go`

**Step 1: 写失败测试**：启动群回合后断言 `IsSessionRunning(groupID)==true`；回合结束后为 false；对 groupID 调 `CancelSession(groupID)` 返回 true 且真正停住。

**Step 3: 实现**：
- 群回合开始：为 `groupID` 注册运行态（参考 `scheduler.go:1086` 的 `RegisterExternalExecution` + `SetSessionRunning`），结束/异常时 `SetSessionRunning(groupID, false, true)`。
- 每个成员回合前同样为 `memberRowID` 注册运行态（供 ACP 空闲回收豁免 + F3 取消）。
- **I5**：取消隐藏成员时**不要**走会广播终态 `session_update` + push 的 `CancelSession`（`session_runtime.go:832`）——那会给隐藏成员发多余通知并 finalize 其（不存在的）历史。新增 `cancelMemberTurn(memberRowID)`，只取消该成员的 turn cancel、不发终态事件。

**Step 5: Commit**

```bash
git add internal/service/group_orchestrator.go internal/service/group_orchestrator_test.go
git commit -m "feat(group): wire running-state and member-aware cancel"
```

---

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

覆盖：主持人输出路由标签 → 选人 → 成员发言 → 落群时间线；**被点名多个成员时按标签顺序依次发言、后者能看到前者本轮发言**；结束信号停止；解析失败回退轮转；最大轮数兜底；成员失败不终止群。

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestGroupOrchestrator -v`
Expected: FAIL

**Step 3: 实现**

`group_orchestrator.go` 要点（伪代码见设计文档 §5.1，**v1 顺序轮次**）：
- `RunGroupTurn(ctx, groupID, userMessage)`：写用户消息到群 → 循环。
- 每轮：主持人发言（注入 + 解析）；解析失败回退轮转；被点名成员**按标签顺序依次**发言（**v1 顺序，非并发**）；每个成员发言前记录高水位 `H`，发言后 `seen_cursor = H`；检查结束/最大轮数/抢占。
- 主持人/成员回合都构造 `TurnSpec{SessionID: memberRowID, TimelineSessionID: groupID, ChatReq: ...}`；`ChatReq` 的 `SessionID`/`Resume`/`HasConversationHistory` 用 **D1 的 `resolveMemberResume`** 覆盖（**C5**）。
- 主持人回合的系统提示 = 成员系统提示 + `BuildHostSystemPrompt(memberNames)`。
- 主持人发言的消息级标记：在写群消息时把 `agent_id=hostAgentID` 并在 `content` 信封加 `"meta":{"role":"host"}`（见 J 阶段读取）。
- **I9（游标语义）**：主持人也用 `seen_cursor`，语义与成员一致——**本次发言前**的群时间线高水位；发言后更新为发言前的高水位（不是发言后的最大 id）。F1 的 `buildInjectionText` 对主持人和成员是同一个函数，`selfAgentID` 传各自 agent id。

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestGroupOrchestrator -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/group_orchestrator.go internal/service/group_orchestrator_test.go
git commit -m "feat(group): add group orchestrator main loop (sequential rounds)"
```

---

### Task F3: 抢占与取消

**Files:**
- Modify: `internal/service/group_orchestrator.go`
- Test: `internal/service/group_orchestrator_test.go`

**Step 1: 写失败测试**：模拟用户在成员发言中途发消息，断言当前成员 turn 被取消、用户消息进时间线、主持人重新选人。

**Step 3: 实现**：调用 **F0 的 `cancelMemberTurn(memberRowID)`**（成员感知，不发终态广播——**I5**）；用户消息经 `AddChatMessageWithAgent(groupID, ..., agentID="")` 落库；循环检测到新用户消息即重新进入主持人阶段。

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

## 阶段 L（v2）：同轮并发气泡 —— **v1 不做**

> v1 是顺序轮次，**整个 L 阶段移出 v1**。保留以下记录，供 v2（恢复同轮并行）时实施。届时**前置条件**：先完成设计文档 §12 C1 的**按消息 id 流式写入原语**，否则并发成员互相覆盖。

### Task L0（v2）: 流式事件携带 message_id（后端）

> `content`/`thinking` 事件当前**不带** `message_id`——`simpleTextPayload`（`internal/ws/stream_hub.go:345-369`）只有 `content`/`text` + `parent_tool_call_id`/`member_name`/`member_color`。

**Files（注意 I2 勘误）:**
- Modify: `internal/ai/interface.go`（`StreamEvent` 在 `:414`；**不是** `internal/ai/stream_event.go`，该文件不存在）
- Modify: `internal/service/session_executor.go`（emit 前填入 `e.cfg.StreamingMessageID`）
- Modify: `internal/ws/stream_hub.go`（`simpleTextPayload` 加 `message_id`，用 `map[string]any` 而非 `map[string]string`）
- Test: `internal/ws/stream_hub_test.go`

> **I3 勘误**：把 `simpleTextPayload` 从 `map[string]string` 改为 `map[string]any` 会打断 `stream_hub_test.go` 的 **12 处** `map[string]string` 断言（`:299,306,340,397,1008,1016,1024,1202,1214,1221,1252,1284`），必须一并更新，否则 "PASS" 是假的。

### Task L1（v2）: 支持多路并发流式占位

**Files:**
- Modify: `web/src/composables/useChatStream.ts`
- Test: `web/src/composables/__tests__/useChatStream.concurrent.test.ts`

> **I7 勘误**：`useChatStream.ts` 有 **20 处** `findStreamingMsg`（`.find()` 取第一个），且 `rebuildFromDb`/`loadHistory` 切回时要保留 N 个并发占位——不止 L1 描述的 Map 改动。

---

## 阶段 M：E2E

### Task M0: E2E 前置（2 个 agent + acp-mock 路由标签）

> **C7 勘误**：`e2e/helpers/server.ts:148` 只写 1 个 agent，`cmd/acp-mock/main.go` 不含任何路由标签（grep=0）。M1 断言"多成员气泡 + 主持人消息"按现状必失败。

**Files:**
- Modify: `cmd/acp-mock/main.go`（支持产出 `<clawbench-speaker>...</clawbench-speaker>` / `<clawbench-group-end/>`，可由环境变量或输入驱动）
- Modify: `e2e/helpers/server.ts`（注册 ≥2 个 agent YAML：一个当主持人、一个当成员）
- Test: `internal/...` 无需；由 M1 验证

**Commit:**

```bash
git add cmd/acp-mock/main.go e2e/helpers/server.ts
git commit -m "test(group): add second mock agent and routing-tag support for e2e"
```

### Task M1: 群聊端到端

**Files:**
- Create: `e2e/specs/group-chat.spec.ts`

**前置：** `go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock`

**场景（v1 顺序轮次）：** 建群（选主持人）→ 加 2 个成员 → 发一条消息 → 断言成员气泡**按顺序**出现 + 主持人消息可见 + 人类抢占生效。

Run: `npx playwright test --config e2e/playwright.config.ts --project=chromium-coverage e2e/specs/group-chat.spec.ts`
Expected: PASS

**Commit:**

```bash
git add e2e/specs/group-chat.spec.ts
git commit -m "test(group): add group chat e2e spec"
```

---

## 完成标准

- 所有阶段 Task 完成且各自 commit（**v1 不含 L0/L1，它们属 v2**）。
- **顺序轮次**：一轮内被点名成员依次发言，后者可见前者本轮发言（E2E 与单测钉住）。
- `go test ./internal/...` 全绿（隔离跑，避免与并发 agent 争抢）。
- `npx vitest run` 全绿。
- `./scripts/pre-push-checks.sh` 通过。
- 纯前端改动后 `npm run build` 供用户即时测试。
- OpenAPI 与 `internal/api/openapi.yaml` 同步，`TestOpenAPIDrift` 通过。
- **评审项验收**：C2（`agent_id` 可写可读）、C3（群在 list/search/browse/overview 均可见、成员均隐藏）、C4（`failTurn`/metadata 落群）、C5（成员 resume 用 external_session_id）、I4（停止按钮生效）均有对应测试通过。
