# AI 群聊（Group Chat）实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

> ## 🚧 实施状态（2026-10-06 核实）
>
> | 阶段 | 状态 |
> |---|---|
> | **A–M**（v1 基线） | ✅ **已合入 main**（`aa88f921a` 起） |
> | **N**（#39–#44 成员可见性/描述） | ❌ **全部未实现**（N1–N5） |
> | **O**（#45–#70 排队/生命周期/降级） | ❌ **全部未实现**（O1–O15） |
>
> **已逐项 `grep` 核实**（证据见设计文档 §0「未实现清单」）。**未实现的 Task 一律不要当成已完成**；实施时从 N1 起按顺序做。

> ## ⚠️ 评审勘误（2026-10-04，五轮）— 实现前必读
>
> 本计划经 **五轮** Superpower code-reviewer 评审（以代码片段为唯一判据），均已逐条核对代码。
>
> **✅ v1 决策：顺序轮次。** 放弃同轮并行（决策 #22 修订）。移除 L0/L1（并发气泡）。
>
> **五轮 Critical（四轮修复暴露的"声明/文件清单滞后"，必须先修）**：
> - **R-1**：`SpeakerID` 在 **C1 的片段**里声明（C2 早于 F0b 用它，须先有声明）。**已改 C1。**
> - **R-2**：G1 的 Files/commit 须含 **`internal/handler/chat.go`**（`AIChat` 委派落点）。**已改 G1。**
> - **R-3**：A3 改了 `insertChatMessageTx` 签名，须同步 **`queue_store.go:217`**（第二调用者）。**已改 A3。**
>
> **五轮 Important**：R-4（设计 §3.1 `UpdateLastRead` 须保持成员 id）、R-5（设计 §8 补 `GET /api/group/members`）、R-6（`GroupMember` 类型须声明）、R-7（E2 须含 `handler/session_resume.go`）、R-8（fork/continue 的 INSERT 也须写归属）、R-9（parity 语料唯一 + both-tag 断言 instruction）、R-10（store/rag 夹具不能共享 service 常量，逐文件改）。
>
> **前四轮**：C2→A3、C3→E2、C4→C2、C5→D1、C7→M0；N1（ACP 池键）、N2（F0 ctx）、N3（F0b）、N4（成员行 id）、N5（A3 读路径）、N6（agentId 判主持人）；C-1（SpeakerID）、C-2（汇总两路径）、C-3（AIChat 委派）、C-4（A3 独立文件）、C-5（顺序）。
>
> **⚠️ 实现方式建议（五轮评审结论）**：五轮 Critical **无一来自架构或核心机制**，全是文档内部一致性。**以代码片段为准、逐 Task `go build`/`go test` 验证**——剩余问题会在编译期即时暴露，比再评审一轮更快更准。勘误文字仅作背景。
>
> **v2 候选**：同轮并行（须先按 §12 C1 增加按消息 id 的流式写入原语 + L0/L1）。

**Goal:** 让用户在 ClawBench 里创建一个"群"，指定一个智能体当主持人，之后手动加入其他智能体，形成"主持人控场 + AI 决定发言者 + 人类可随时介入"的多智能体辩论式群聊。

**Architecture:** 群是一条 `chat_sessions` 行（`session_type='group'`）并独占时间线；每个成员是一条隐藏的 `chat_sessions` 行（`session_type='group_member'`，`group_id` 指向群），只承载"连接绑定"（agent/transport/external_session_id）。消息**只存一份**，落在群时间线（`chat_history.agent_id` 标记发言人）。新增独立编排器 `internal/service/group_orchestrator.go` 驱动主循环；现有 `run_turn`/ACP 池/runner 只做一处"时间线解耦"（新增 `TimelineSessionID`）。

**Tech Stack:** Go（SQLite、net/http、WS StreamHub）、Vue 3 + TypeScript（Vitest）、Playwright（E2E + acp-mock）。

**设计文档：** `docs/plans/2026-10-04-ai-group-chat-design.md`（决策表 **73 条**，遇歧义先读它；评审勘误见 §12/§12.1/§12.2；**合入后增补见 §12.6/§12.7**）。

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
| D | 成员 resume 修正（CLI/ACP 分流） | C |
| E | 群存储与 CRUD（创建群/加成员/删成员/列成员） | A |
| F | 编排器主循环（**顺序轮次**，含 F0 运行态/F0b stream_start 发言人） | B、C、D、E |
| G | HTTP API + OpenAPI | E、F |
| H | 前端：`AgentSelectorDrawer` 多选 | — |
| I | 前端：建群入口与主持人选择 | G、H |
| J | 前端：群时间线渲染（发言人气泡/主持人样式/路由卡片 J2） | G |
| K | 前端：成员头像条 + 增删 | G、H |
| M | E2E（M0 前置 + M1） | 全部 |
| N | **增补（合入后）**：成员可见性、描述注入、系统事件（N1 role='system' 重建 / N2 描述三处注入 / N3 系统事件 / N4 前端渲染 / N5 拒绝移除主持人） | 全部 |
| O | **增补（二轮 grill）**：排队与生命周期（O1 群入队+drain / O2 级联删除+离群保留 / O3 成员失败复用 failTurn / O4 摘要推荐跳过 / O5 轮转+失败收尾 / O6 智能体重名收敛 / O7 归档关连接 / O8 隐藏回溯入口 / O9 发言中高亮 / O10 群 auto-approve 批量 / O11 isGroupSession 用类型 / O12 项目计数排除成员行 / O13 群消息附件注入 / O14 剥离主持人标签+去重 / O15 失败不推游标+warning 不注入 / O16 终态前兜孤儿流式行 / #72 群回合推送并入 O1 / O17 保护成员连接） | N |
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

在 `database_group_test.go` 追加（**N7 勘误**：同样用 `InitDB()` 模式，**不要**用 `setupTestEnv`——它只在 handler 包）：

```go
func TestChatSessionsHasGroupIDColumn(t *testing.T) {
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
- Modify: **`internal/service/queue_store.go`**（**R-3**：`insertChatMessageTx` 的**第二个调用者**在 `:217`（`materializeQueuedRowTx`）；Go 无默认参数，改了签名不同步这里必编译失败）
- Modify: **`internal/service/continue_conversation.go`**（**R-8**：fork/continue 的 **INSERT** 路径 `:244,:479` 也须写 `agent_id`——只改 SELECT 不够，否则 fork 出的群丢失归属）
- Test: **`internal/service/chat_agent_id_test.go`（独立文件，`package service_test`）** —— **C-4 勘误**：A1 的 `database_group_test.go` 是 `package service`，A3 需要 `helperCreateSession` + `service.*` 故必须 `package service_test`；**一个文件不能既是 `service` 又是 `service_test`**，故 A3 用独立文件。

**Step 1: 写失败测试**

**N8 勘误**：`setupDB(t)`（`chat_test.go:232`）用的是 `chat_test.go:29-42` 的 `schema` 常量，**其中没有 `agent_id` 列**——须先给该内存 schema 加 `agent_id TEXT DEFAULT ''`（与真实迁移一致），否则测试会因缺列失败而非因功能缺失。测试文件用 `package service_test`（与 `chat_test.go` 一致）。

```go
// 先在 chat_test.go 的 schema 常量 chat_history 定义里加：agent_id TEXT DEFAULT '',
func TestAddChatMessagePersistsAgentID(t *testing.T) {
	setupDB(t) // chat_test.go:232，已含 agent_id 列
	project := "/tmp/grouptest"
	sid := helperCreateSession(t, project, "codebuddy", "t")
	id, err := service.AddChatMessageWithAgent(project, "codebuddy", sid, "assistant", `{"blocks":[]}`, nil, false, "", "member-row-1")
	if err != nil {
		t.Fatal(err)
	}
	msgs, _, err := service.GetChatHistoryPaged(project, "codebuddy", sid, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if m.ID == id {
			found = true
			if m.AgentID != "member-row-1" {
				t.Fatalf("AgentID=%q, want member-row-1", m.AgentID)
			}
		}
	}
	if !found {
		t.Fatal("message not returned")
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestAddChatMessagePersistsAgentID -v`
Expected: FAIL（字段/参数不存在）

**Step 3: 实现**

- `model.ChatMessage` 加 `AgentID string`（**语义 = 发言人成员行 id**，见设计 §4.3/N4）。
- 新增 `AddChatMessageWithAgent(...agentID string)`，内部把 `agentID` 传给 `insertChatMessageTx`；`AddChatMessage` 保留原签名并转调 `AddChatMessageWithAgent(..., "")`（避免改动所有既有调用点）。
- `insertChatMessageTx` 的 INSERT 增加 `agent_id` 列与占位符。

**N5 勘误（读路径必须补全，否则群分享/fork/TTS 静默丢归属）**：除 `GetChatHistoryPaged` 三条 SELECT（`chat.go:89,105,119`）、`GetMessagesBySessionID`/`Raw`（`chat.go:343,361`）、`scanMessages`（`chat.go:131`）外，**还须补**：
- `GetChatHistory`（`chat.go:34`，fork/RAG 用）
- `GetMessageByID`（`chat.go:304`，TTS/summary/RAG 用；`chat.go:260` 是 `GetMessageContent`）
- `GetSessionMessagesForSelection`（`session_share_payload.go:129`，**群分享**）
- `continue_conversation.go:215,446`（fork/continue 复制消息）
- `chat.go:2116`（preview 查询）
- `GetAssistantRawContents`（`chat.go:399`，fork 上下文 raw 文本）

逐条 `SELECT ... agent_id ...` 并写入 `ChatMessage.AgentID`。**验证方法**：`grep -n "SELECT id, role, content" internal/service/` 找所有列清单，逐个确认已含 `agent_id`。

**⚠️ N-3 勘误（四轮，Critical）——改 SELECT 会连带打挂全部测试夹具。** A3 给上述 reader 的 SELECT 增加 `agent_id` 列后，**所有用内存 DDL 建 `chat_history` 表却没加该列的测试**都会因 "no such column: agent_id" 失败。仓库内有 **25 个测试文件**自带 `chat_history` 建表语句（`grep -rln "CREATE TABLE.*chat_history" internal/ --include=*_test.go`），其中至少这些**必须**补列（否则首跑即挂）：
- `internal/handler/testutil_test.go`（`setupTestEnv`，约 1575 个调用者，且 `service.GetChatHistory` 被大量调用）
- `internal/service/chat_test.go`（`schema` 常量，`setupDB`）
- `internal/service/session_command_test.go`
- `internal/service/chat_summary_test.go`、`chat_summary_trigger_test.go`、`summary_test.go`
- `internal/service/session_share_payload_test.go`、`session_share_helpers_test.go`
- `internal/service/session_runtime_test.go`、`drain_test.go`、`scheduler_test.go`、`scheduler_executor_test.go`、`scheduler_script_phase_test.go`、`session_cleanup_test.go`、`pending_events_test.go`、`queue_reaper_test.go`、`tool_calls_test.go`、`thinking_migrate_test.go`、`chat_metadata_ledger_migrate_test.go`、`projects_migrate_test.go`、`database_test.go`
- `internal/rag/indexer_failure_test.go`、`cluster_worker_test.go`
- `internal/store/store_test_helpers_test.go`、`perf_instrumentation_test.go`

**做法（推荐）**：`internal/service` 内的夹具可抽**包内共享 DDL 常量**让各文件引用；或统一在每处 DDL 加 `agent_id TEXT DEFAULT ''`。**R-10 勘误**：**不能**用"从 `service` 导出的共享常量"给 `internal/store`/`internal/rag` 用——`internal/service` **imports** `internal/store`，反向导入成环。**`internal/store/*_test.go` 与 `internal/rag/*_test.go` 必须逐文件各自加列**（这两个包只有 `store_test_helpers_test.go`/`perf_instrumentation_test.go` 与 `indexer_failure_test.go`/`cluster_worker_test.go` 四个文件）。**另**：部分文件（如 `database_test.go:820,861` 的旧 schema 夹具、`projects_migrate_test.go`）走 `InitDB()`，迁移会自动 `ALTER` 加列，**无需改**。**验收**：`go vet ./...` 后全量 `go test ./internal/...`（隔离跑）确认无 "no such column"。

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestAddChatMessagePersistsAgentID -v`
Expected: PASS

**Step 4b: 补一条"群分享保留归属"测试**（否则 N5 的分享路径遗漏会绿着上线）：建群 → 成员发言 → 生成分享 payload → 断言含 `agent_id`。

**Step 5: Commit**

```bash
git add internal/model/chat.go internal/service/chat.go internal/service/session_share_payload.go internal/service/continue_conversation.go internal/service/chat_test.go internal/service/chat_agent_id_test.go
git commit -m "feat(group): thread agent_id through all message read/write paths"
```

---

## 阶段 B：路由标签解析叶子包

### Task B1: 创建 `internal/grouprouting` 包与解析函数

**Files:**
- Create: `internal/grouprouting/grouprouting.go`
- Create: `internal/grouprouting/testdata/parity_corpus.json`（**N-6**：parity 语料，Go 与 TS 双向固化；J2 依赖它）
- Create: `internal/grouprouting/parity_test.go`（**N-6**：Go 侧 parity 测试，读语料断言 `Parse` 输出；照 `internal/askquestion/parity_test.go`）
- Test: `internal/grouprouting/grouprouting_test.go`

**契约（照 `internal/askquestion` 的哲学）**：检测即解析；不可解析时**不剥离**标签（保留原文，绝不丢内容）；解析失败回退轮转。标签名固定 `clawbench-speaker`，结束信号固定 `clawbench-group-end`。**本包不 import 任何 internal 包**（叶子包）。

> **N-6 勘误**：J2（前端路由卡片）要求"复用或新建 parity corpus + 双向 parity 测试"，但**没有任何 Task 创建它**。B1 须一并建 `testdata/parity_corpus.json` + `parity_test.go`（Go 侧）；J2 再补 TS 侧读取同一语料。语料至少覆盖：单发言人、多发言人、结束信号、**两标签同现（N-4）**、畸形（空 payload）、无标签。

**Step 1: 写失败测试**

```go
package grouprouting

import (
	"reflect"
	"strings"
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

// N-4：一条消息同时含路由标签与结束标签。End 必须为真（编排器优先处理结束），
// 且结束标签不得泄漏进 Instruction。
func TestParseBothTags(t *testing.T) {
	r := Parse(`<clawbench-speaker>B</clawbench-speaker> 请回应<clawbench-group-end/>`)
	if !r.End {
		t.Fatal("expected End=true (end wins)")
	}
	if strings.Contains(r.Instruction, "clawbench-group-end") {
		t.Fatalf("end tag leaked into instruction: %q", r.Instruction)
	}
	if r.Instruction != "请回应" {
		t.Fatalf("instruction=%q", r.Instruction)
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

	// Instruction = text after the closing speaker tag, trimmed, with any end
	// tag removed (N-4: a message may carry both tags; the end tag must not
	// leak into the instruction handed to a member or rendered by the card).
	after := text[loc[1]:]
	after = reEnd.ReplaceAllString(after, "")
	res.Instruction = strings.TrimSpace(after)
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
Expected: PASS（6 个测试）

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
	if !strings.Contains(p, "结论") {
		t.Fatal("must ask for summary after end tag")
	}
	for _, m := range members {
		if !strings.Contains(p, m) {
			t.Fatalf("member %q missing from prompt", m)
		}
	}
}

func TestBuildHostSummaryPromptHasNoRoutingTag(t *testing.T) {
	p := BuildHostSummaryPrompt([]string{"A", "B"})
	if strings.Contains(p, "<clawbench-speaker>") || strings.Contains(p, "<clawbench-group-end/>") {
		t.Fatal("summary prompt must not ask for routing/end tags (would re-enter loop)")
	}
	if !strings.Contains(p, "结论") {
		t.Fatal("summary prompt must ask for a conclusion")
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
	b.WriteString("可一次点名多个成员（逗号分隔），他们将按顺序依次发言：\n")
	b.WriteString("  <clawbench-speaker>A,B</clawbench-speaker> 请分别表态\n")
	b.WriteString("当讨论已充分、可以收敛时，输出结束标签：\n")
	b.WriteString("  <clawbench-group-end/>\n")
	b.WriteString("在结束标签之后，必须再写一段简短的讨论结论（最终汇总），供用户阅读。\n") // 决策 #32：结束信号轮即汇总
	b.WriteString("可选的成员名：")
	b.WriteString(strings.Join(members, "、"))
	b.WriteString("。\n")
	return b.String()
}

// BuildHostSummaryPrompt is the prompt variant for the max-rounds fallback:
// the loop ran out of rounds before the host chose to end, so we run the host
// once more purely to produce the final summary. It MUST NOT ask for a routing
// tag — the orchestrator ignores any tag in this turn to avoid re-entering the
// loop (review round-3 C-2).
func BuildHostSummaryPrompt(members []string) string {
	var b strings.Builder
	b.WriteString("\n\n[群聊主持人] 讨论轮数已达上限，现在只做收尾。\n")
	b.WriteString("请直接写一段简短的讨论结论（最终汇总），总结各成员观点与你的判断，供用户阅读。\n")
	b.WriteString("不要再输出任何路由标签或结束标签。\n")
	b.WriteString("参与成员：")
	b.WriteString(strings.Join(members, "、"))
	b.WriteString("。\n")
	return b.String()
}
```

> **C-2 勘误**：两个函数分别对应两条路径——正常结束用 `BuildHostSystemPrompt`（结束标签轮自带汇总）；达上限用 `BuildHostSummaryPrompt`（额外一轮，只写结论）。**不得**在结束信号轮之后再跑主持人（会重复汇总）。

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

// R-1：SpeakerID 与 AgentID 是不同字段，编译期须能分别设置。
func TestTurnSpecSpeakerIDIsSeparateFromAgentID(t *testing.T) {
	spec := TurnSpec{SessionID: "member-1", AgentID: "agent-real", SpeakerID: "member-1"}
	if spec.SpeakerID != "member-1" || spec.AgentID != "agent-real" {
		t.Fatalf("SpeakerID=%q AgentID=%q", spec.SpeakerID, spec.AgentID)
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestTurnSpecEffectiveTimeline -v`
Expected: FAIL（方法未定义）

**Step 3: 实现**

在 `TurnSpec` 的 `SessionID` 字段后加（**R-1：`SpeakerID` 声明必须在这里，不能拖到 F0b**——C2 的片段读 `spec.SpeakerID`，而 Phase C 早于 Phase F）：

```go
	// TimelineSessionID, when non-empty, redirects message persistence and WS
	// broadcast to a different session than the one owning the connection.
	// Group chat sets it to the group session while SessionID is the member
	// row (whose backend connection and external_session_id are used).
	//
	// Empty means "same as SessionID" — every pre-existing caller keeps its
	// exact behavior.
	TimelineSessionID string

	// SpeakerID is the member session row id of whoever produced this turn's
	// output. It is written to chat_history.agent_id (speaker attribution) and
	// broadcast on stream_start. Empty for ordinary single-agent turns.
	//
	// It is DISTINCT from AgentID (the real agent id used for backend
	// resolution): never pass SpeakerID where AgentID is expected.
	SpeakerID string
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

在 `RunConfig` 的 `SessionID` 后加（**注意：`RunConfig` 在 `session_executor.go`，不是 `run_turn.go`**）：

```go
	// TimelineSessionID mirrors TurnSpec.TimelineSessionID: the session whose
	// timeline (chat_history rows, WS broadcast) this run writes to. Empty
	// means SessionID.
	TimelineSessionID string

	// SpeakerID mirrors TurnSpec.SpeakerID (member row id) for attribution.
	SpeakerID string
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

`run_turn.go` 改动**五处**（**C4 勘误**：原计划只列了两处，漏了 `failTurn` 与 metadata；**N-1 勘误**：消息行的归属必须写 `SpeakerID` 而非 `agentID`）：

```go
	// N-1：占位行的 agent_id = 发言人成员行 id（SpeakerID），不是真实 agent id。
	streamingMsgID, err := AddChatMessageWithAgent(spec.ProjectPath, spec.BackendName, spec.effectiveTimelineSessionID(),
		roleAssistant, string(emptyContent), nil, true, "", spec.SpeakerID)  // 成员行 id
```

```go
	ws.EmitToSession(spec.effectiveTimelineSessionID(), ai.StreamEvent{
		Type:        "stream_start",
		StreamStart: &ai.StreamStartData{MessageID: streamingMsgID, SpeakerID: spec.SpeakerID}, // N-1/C-1：填 SpeakerID
	})
```

```go
	execCfg := RunConfig{
		Mode:               spec.Mode,
		ProjectPath:        spec.ProjectPath,
		BackendName:        spec.BackendName,
		SessionID:          spec.SessionID,
		TimelineSessionID:  spec.effectiveTimelineSessionID(),
		AgentID:            agentID,       // 真实 agent id（连接解析用）
		SpeakerID:          spec.SpeakerID, // 成员行 id（归属用，N-1）
		...
	}
```

> **N-1（四轮，Critical，三轮 C-1 修复引入）**：`chat_history.agent_id` 的**唯一写入点**是 `runTurnStart` 的 `AddChatMessageWithAgent`（`UpdateStreamingMessage`/`FinalizeStreamingMessage` 只改 `content`/`streaming`/`completed_at`，**不碰 `agent_id`**）。若这里传 `agentID`（真实 agent id），则 F1 的 `agent_id != self` 自排除、前端行 id→agent 映射、N4 全部失效，而 §9.4 守卫（"agent_id 非空"）**照样通过**。**必须传 `spec.SpeakerID`**，并加测试断言"占位行 `agent_id == 成员行 id`"。

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
- `CreateStreamingMessage`（`:1183,1193`）→ 时间线 id。**N-7 勘误**：该函数（`chat.go:2571`）内部 `AddChatMessage(..., "", "")` ⇒ `agent_id=''`。群成员回合若发生 mid-turn 注入的 split，会产出一条**无归属**的 "after" 行（气泡中途丢失发言人）。须给 `CreateStreamingMessage` 增加 `agentID` 参数并传 `cfg.SpeakerID`（与 N-1 同源）。
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

> ⚠️ 见设计文档 §4.2 与 §12 **C5** / §12.1 **N1（Critical，上轮修订新引入）**。原计划"成员回合强制置 `resume=true`"是**错的**；上一轮改成"覆盖 SessionID=extID"也**错**（会毁 ACP 池键）。按下面重做。

### Task D1: 成员回合的请求构造（CLI/ACP 分流）

**C5 + N1 勘误（必读）**：
1. **ACP 的 `req.SessionID` 是连接池键**（`acp_backend.go:61` `GetOrCreateConn(ctx, b.agent, req.SessionID, ...)`；`:126` `getSessionAutoApprove(req.SessionID)`；`acp_pool.go:529` 预填 `getExternalSessionID(clawbenchSID)`）。**绝不能**把它覆盖成 ACP session id——否则池里多一个错误条目、auto-approve 丢失、resume 失效。ACP 的 resume 由成员行 `external_session_id` 经池自动完成（Phase F 的连接语义即覆盖）。
2. **CLI 的 `req.SessionID` 才是传给 `--resume` 的 id**（`common_stream.go:21`）。成员行无 `chat_history` ⇒ `SessionHasAssistant` 恒 false ⇒ `resolveResumeSessionID`（`chat_request.go:267`）返回 `SessionID=memberUUID, Resume=false`；须改成 `SessionID=extID, Resume=(extID!="")`。
3. **`HasConversationHistory`/`AssistantMessageCount` 同病**（`chat_request.go:136,154` 由 `chat_history` 推出，成员恒 false/0）⇒ `shouldNewSessionFallback(false)=true`（`acp_backend.go:335`）⇒ ACP 瞬时断连静默新建会话。成员回合须覆盖为 `extID != ""`（两者都覆盖；`AssistantMessageCount` 用 1 或真实值均可，仅需 > 0 表达"有历史"）。

**Files:**
- Create: `internal/service/group_member_request.go`
- Test: `internal/service/group_member_request_test.go`

**Step 1: 写失败测试**

```go
package service

import "testing"

func TestApplyMemberResumeOverrides_CLI(t *testing.T) {
	req := ai.ChatRequest{SessionID: "member-1", Resume: false, HasConversationHistory: false}
	applyMemberResumeOverrides(&req, "member-1", "ext-9", false /*isACP*/)
	if req.SessionID != "ext-9" || !req.Resume || !req.HasConversationHistory {
		t.Fatalf("cli: %+v", req)
	}
	// No ext id -> fresh.
	req = ai.ChatRequest{SessionID: "member-1"}
	applyMemberResumeOverrides(&req, "member-1", "", false)
	if req.SessionID != "member-1" || req.Resume || req.HasConversationHistory {
		t.Fatalf("cli fresh: %+v", req)
	}
}

func TestApplyMemberResumeOverrides_ACPKeepsPoolKey(t *testing.T) {
	req := ai.ChatRequest{SessionID: "member-1", Resume: false, HasConversationHistory: false}
	applyMemberResumeOverrides(&req, "member-1", "acp-sid-7", true /*isACP*/)
	if req.SessionID != "member-1" {
		t.Fatalf("acp must keep pool key, got %q", req.SessionID)
	}
	if !req.Resume || !req.HasConversationHistory {
		t.Fatalf("acp resume/history must be true: %+v", req)
	}
}
```

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestApplyMemberResumeOverrides -v`
Expected: FAIL

**Step 3: 实现**

新建 `internal/service/group_member_request.go`：

```go
package service

import "clawbench/internal/ai"

// applyMemberResumeOverrides fixes the chat_history-derived resume signals on a
// group member's ChatRequest.
//
// Member session rows carry NO chat_history rows (all messages live in the
// group timeline), so BuildChatRequest derives the wrong values:
//   - SessionHasAssistant -> false  => Resume=false
//   - GetChatMessageCount -> 0      => HasConversationHistory=false
//
// The member's real memory signal is its own external_session_id (written by
// captureExternalSessionID on the member row, connection semantics).
//
// CRITICAL (review N1): req.SessionID is the ACP connection-pool key. For ACP
// it MUST stay the member row id — overwriting it with the ACP session id would
// create a second, wrong pool entry, lose auto-approve, and break resume. Only
// CLI backends need SessionID switched to the external id (that is the value
// passed to --resume).
func applyMemberResumeOverrides(req *ai.ChatRequest, memberRowID, externalSessionID string, isACP bool) {
	hasMemory := externalSessionID != ""
	req.Resume = hasMemory
	req.HasConversationHistory = hasMemory
	if hasMemory {
		req.AssistantMessageCount = 1 // >0 => "has history" for system-prompt re-injection
	}
	if !isACP && hasMemory {
		req.SessionID = externalSessionID
	}
	// ACP: leave req.SessionID == memberRowID (pool key).
	_ = memberRowID
}
```

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestApplyMemberResumeOverrides -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/group_member_request.go internal/service/group_member_request_test.go
git commit -m "feat(group): apply member resume overrides (CLI/ACP split)"
```

> **接线（Phase F）**：成员与主持人回合构造 `ai.ChatRequest` 后（`BuildChatRequest` 之后、`ExecuteStream` 之前），调用 `applyMemberResumeOverrides(&req, memberRowID, GetExternalSessionID(memberRowID), resolveIsACP(agentID, transport))`。`resolveIsACP` 已存在于 `chat_request.go`。

---

## 阶段 E：群存储与 CRUD

### Task E1: 群与成员的数据访问

**Files:**
- Create: `internal/service/group_store.go`
- Test: `internal/service/group_store_test.go`

**Step 1: 写失败测试**

覆盖：建群返回群 id + 主持人成员行；加成员返回成员行；列成员按顺序；删成员把成员行 `archived=1`（保留行与历史）；查群的主持人。

```go
// N7 勘误：用 InitDB() 模式，不要用 setupTestEnv（只在 handler 包）。
func TestCreateGroupAndMembers(t *testing.T) {
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

	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil { // 建 project 行
		t.Fatal(err)
	}

	groupID, hostMemberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if groupID == "" || hostMemberID == "" {
		t.Fatal("empty ids")
	}

	m, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// I-1 勘误：ListGroupMembers 默认含 archived=1（已离场），故删后仍应返回 2。
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
	if len(members) != 2 {
		t.Fatalf("removal keeps the row (已离场) -> still 2, got %d", len(members))
	}
	// 但活跃成员应只剩 1（离场的不算活跃）。
	if active := countActiveMembers(members); active != 1 {
		t.Fatalf("want 1 active member, got %d", active)
	}
}
```

> **I-1 勘误**：`ListGroupMembers` 默认**含** `archived=1`（决策 #29"保留并标记已离场"），故删成员后行数不变；测试须按"总行数含离场 / 活跃数排除离场"两条断言（或给 `ListGroupMembers` 加 `includeArchived bool` 参数，二者择一并在实现里固定）。

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestCreateGroupAndMembers -v`
Expected: FAIL

**Step 3: 实现**

新建 `internal/service/group_store.go`。复用 `CreateSession`（`chat.go:1822`，`sessionType` 参数可传 `"group"` / `"group_member"`）。要点：
- **R-6 勘误**：须定义 **`type GroupMember struct { ID, AgentID, Name, Backend string; Left bool }`**（`ListGroupMembers` 的返回元素类型）——测试里的 `countActiveMembers(members []GroupMember)` 依赖它，但全仓/计划此前都没声明过。
- **N-8 勘误**：**`SetSessionGroupID` 在仓库中不存在**——须一并实现（`UPDATE chat_sessions SET group_id=? WHERE id=?`）并列入本 Task 交付物；或直接内联 UPDATE。
- `CreateGroup(project, title, backend, hostAgentID)` → `CreateSession(..., "group")`，然后为主持人建成员行 `CreateSession(..., "group_member")`（`title` = 主持人 agent 名），`SetSessionGroupID(hostMemberID, groupID)`，写 `host_member_id` 到群行 `context_state`，返回 `(groupID, hostMemberID, err)`。
- `AddGroupMember(project, groupID, backend, agentID, displayName)` —— **I-7**：加 `displayName` 参数写入成员行 `title`（决策 #36）。
- `ListGroupMembers(groupID) []GroupMember` → `SELECT id, agent_id, backend, title, archived FROM chat_sessions WHERE group_id=? AND session_type='group_member' ORDER BY created_at ASC`（**含 archived=1**，映射为 `GroupMember{..., Left: archived==1}`）。
- `RemoveGroupMember(groupID, memberID)` → `UPDATE chat_sessions SET archived=1 WHERE id=? AND group_id=?`（**保留行**，历史发言在群里不受影响）。
- **N-9 勘误**：**只保留 `GetGroupHostMember(groupID) string` / `SetGroupHostMember(groupID, memberID)`**（返回/写入**成员行 id**）——删掉旧草稿里的 `GetGroupHost(groupID)`（它描述为"返回 agent id"，与 §4.3/N4/I-5 矛盾，且与 `GetGroupHostMember` 重叠）。读用 `json_extract(context_state,'$.host_member_id')`，写用 `PatchContextStateMerge`。
- `GetGroupMaxRounds(groupID) int` —— `json_extract(context_state,'$.maxRounds')`，缺省/空返回 **10**（**I-10**，决策 #31）。
- `SetGroupMaxRounds(groupID, n)` —— `PatchContextStateMerge`（值须为合法 JSON 数字）。
- `GetSessionType(sessionID) string` —— **N-5**：新增（供 G1 判断群会话）；`SELECT session_type FROM chat_sessions WHERE id=?`。
- `countActiveMembers(members []GroupMember) int` —— 测试辅助（活跃 = `!archived`）；M-5 指出它被用但未定义。

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
- Modify: `internal/store/session_queries.go`（参数化查询 `:133,:250`）
- Modify: `internal/service/continue_conversation.go`（计数 `:161,:430`）
- Modify: **`internal/handler/session_resume.go`**（**R-7**：计数 `:358` 是 #37"4 处"之一，此前 Files/commit 漏列）
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

逐处判断"该处是否应显示群"，已知需处理点（不止这三条）：`chat.go:1478,1641,2007,2014,2046,2068`、`continue_conversation.go:48,141,161,430`、`session_command.go:46,71`、`handler/session_resume.go:358,459`、`store/session_queries.go:133,250`。

**会话上限（决策 #37：群计入、成员不计）**：**计数类**查询共 **4 处**（`GetSessionCount`（`chat.go:2046`）、`continue_conversation.go:161,430`、`handler/session_resume.go:358`）改为 `session_type IN ('chat','group')`，使群占 1 个额度、成员不占。**列表/搜索类**查询同样改 `IN ('chat','group')`（群可见）。**关键**：`POST /api/group/create` 也必须自己过这道上限门（`handler/chat_session.go:174` 同款检查），否则新建群绕过上限。补测试：达到 `SessionMaxCount` 时建群返回 409。

> **I-2 勘误（谓词类别）**：`continue_conversation.go:48,141` 与 `session_resume.go:459` 是 **`source_session_id = ?` 查找**（fork/continue/ACP 来源），**不是列表/搜索**——**不要**把它们改成 `IN ('chat','group')`（类别错误）。群不会有 `source_session_id`，这些查询保持原样即可。

**Step 4: 运行确认通过**

Run: `go test ./internal/service/ -run TestGroupMembersHiddenFromSessionList -v`
Expected: PASS

**Step 4b: 补 search/browse/overview 守卫测试**

新增测试断言群**出现**在 `GetRecentSessions`/`SearchSessionsByTitle`/`GetOverviewSessions`，成员**不出现**。仅测 `GetSessions` 会漏掉参数化查询。

**Step 5: Commit**

```bash
git add internal/service/chat.go internal/store/session_queries.go internal/service/continue_conversation.go internal/handler/session_resume.go internal/service/group_store_test.go
git commit -m "feat(group): show groups and hide members across list/search/browse/overview"
```

---

## 阶段 F：编排器主循环（顺序轮次）

> **C-5 / N-2 勘误（执行顺序）**：`group_orchestrator.go` 由 **F2 创建**；F0/F3 是**修改**它；而 F2 的测试依赖 F1 的 `buildInjectionText`，F2 广播 `stream_start` 又依赖 F0b 的 `SpeakerID` 字段。故**实际执行顺序为**：
>
> **F0b（后端 SpeakerID）→ F1（`group_inject.go` + `buildInjectionText`）→ F2（创建 `group_orchestrator.go` 骨架 + 主循环）→ F0（接线运行态/取消）→ F3（抢占）**
>
> 下文 Task 编号保留但**按此顺序执行**。（三轮曾误写为 F2→F0→F1→F3，那会让 F2 早于它依赖的 F1，四轮 N-2 已纠正。）

### Task F0: 运行态与取消接线（评审 I4/I5）

> **依赖**：在 **F2 之后**执行（F2 先创建 `group_orchestrator.go`）。
>
> **I4 勘误（Critical）**：`runTurn` 自身**不**注册运行态——`TryClaimSessionRun`/`SetSessionRunning` 在 `handler/chat.go:469,527`。若不接线：(a) `IsSessionRunning(memberID)` 恒 false ⇒ ACP 空闲回收（`acp_pool.go:481`）可能杀掉长辩论中的成员连接；(b) 群未注册 running ⇒ 前端对 `groupID` 发 cancel 时 `CancelSession(groupID)` 找不到 runner，**静默 no-op**（停止按钮失效）；(c) F3 的 `CancelSession(memberRowID)` 同样需要成员已注册。

**Files:**
- Modify: `internal/service/group_orchestrator.go`（编排器进入成员回合前 `RegisterExternalExecution` / `SetSessionRunning`）—— **须在 F2 之后**
- Test: `internal/service/group_orchestrator_test.go`

**Step 1: 写失败测试**：启动群回合后断言 `IsSessionRunning(groupID)==true`；回合结束后为 false；**对 groupID 调 `CancelSession(groupID)` 能真正停住正在跑的成员回合**（不只停群占位 runner）。

**Step 3: 实现**：
- 群回合开始：为 `groupID` 注册运行态（参考 `scheduler.go:1086` 的 `RegisterExternalExecution` + `SetSessionRunning`），结束/异常时 `SetSessionRunning(groupID, false, true)`。
- 每个成员回合前同样为 `memberRowID` 注册运行态（供 ACP 空闲回收豁免 + F3 取消）。
- **N2（停止传播，Critical-adjacent）**：每个成员回合的 `TurnSpec.Ctx` 必须**派生自编排器自己的可取消 ctx**（`groupCtx, groupCancel := context.WithCancel(...)`），这样 `CancelSession(groupID)`（经 `RegisterExternalExecution` 挂到群 runner）能**传播到当前成员回合**。否则停止只取消了群占位 runner，成员 `runTurn` 照跑。F0 的测试（"真正停住"）会抓住这点。
- **I5**：取消隐藏成员时**不要**走会广播终态 `session_update` + push 的 `CancelSession`（`session_runtime.go:832`）——那会给隐藏成员发多余通知并 finalize 其（不存在的）历史。新增 `cancelMemberTurn(memberRowID)`，只取消该成员的 turn cancel、不发终态事件。

**Step 5: Commit**

```bash
git add internal/service/group_orchestrator.go internal/service/group_orchestrator_test.go
git commit -m "feat(group): wire running-state, ctx-derived cancel, member-aware cancel"
```

---

### Task F0b: `stream_start` 携带发言人（评审 N3）

> **N3**：`stream_start` payload 只有 `message_id`（`ws/stream_hub.go:373`），流式中前端无法知道气泡属于哪个成员。v1 在 `stream_start` 增加发言人（成员行 id）。

**Files:**
- Modify: `internal/ai/interface.go`（`StreamStartData`（`:479`）加 **`SpeakerID string`**，**不是 `AgentID`**——C-1 勘误，避免与真实 agent id 混淆）
- Modify: `internal/ai/interface.go`（`StreamEvent`（`:414`）如需在事件上携带发言人）
- Modify: `internal/service/run_turn.go`（`TurnSpec`/`RunConfig` 加 `SpeakerID`；广播 `stream_start` 时从它填）
- Modify: `internal/ws/stream_hub.go`（`streamStartPayload` 加 `agent_id`，值取 `SpeakerID`）
- Test: `internal/ws/stream_hub_test.go`

> **F0b 在阶段顺序中的位置（N-2 勘误）**：F0b 是**后端小改**，应在 **F2 之前**完成（F2 广播 `stream_start` 时依赖 `SpeakerID` 字段已存在）。故完整执行顺序为 **F0b → F1 → F2 → F0 → F3**。

**Step 1: 写失败测试**

```go
func TestStreamStartPayloadCarriesAgentID(t *testing.T) {
	p := streamStartPayload(ai.StreamEvent{StreamStart: &ai.StreamStartData{MessageID: 7, SpeakerID: "member-1"}})
	m, ok := p.(map[string]any)
	if !ok {
		t.Fatalf("type %T", p)
	}
	if m["message_id"] != int64(7) || m["agent_id"] != "member-1" {
		t.Fatalf("payload=%v", m)
	}
}
```

**Step 2-4:** 实现后 `go test ./internal/ws/ -run TestStreamStartPayloadCarriesAgentID -v` 须 PASS。

> **C-1 勘误（Critical）**：**不要**复用 `TurnSpec.AgentID` 传成员行 id——`run_turn.go:234` 的 `ResolveAgentID(spec.SessionID, spec.AgentID)` 会**原样返回**它，再喂给 `NewBackendForAgentWithTransport`（`:245`），对 ACP-only 后端会失败（`model.GetAgent(成员行id)` 为 nil）。**须新增独立字段 `SpeakerID`**（成员行 id）：
> - `StreamStartData`（`internal/ai/interface.go:479`）加 **`SpeakerID string`**（不是 `AgentID`，避免与真实 agent id 混淆）；`streamStartPayload` 输出键名仍用 `agent_id`（前端语义 = 发言人成员行 id）。
> - `TurnSpec` / `RunConfig` 加 `SpeakerID string`，`runTurnStart` 广播 `stream_start` 时从它填；**`TurnSpec.AgentID` 保持真实 agent id（或留空）**。
>
> **I-3 勘误**：`stream_hub_test.go:1100` 是 `stream_start` 的**整 map 相等**断言（`assert.Equal(t, map[string]any{"message_id": ...}, payload)`）。**`agent_id`（= SpeakerID）为空时必须省略该键**（同 `simpleTextPayload` 对 `think_id` 的处理），否则既有断言失败。plan 原话"加字段不会破坏既有断言"**为假**。**M-1 勘误**：`:1066` 其实是 `TestStreamSplitPayload`（`streamSplitPayload`，不被本 Task 改动），无需处理；只 `:1100` 相关。另**须新增一条"`SpeakerID == ""` 时 `agent_id` 键不存在"的测试**。

**Step 5: Commit**

```bash
git add internal/ai/interface.go internal/service/run_turn.go internal/ws/stream_hub.go internal/ws/stream_hub_test.go
git commit -m "feat(group): carry speaker id on stream_start"
```

---

### Task F1: 增量注入文本构造

**Files:**
- Create: `internal/service/group_inject.go`
- Test: `internal/service/group_inject_test.go`

**Step 1: 写失败测试**

给定群时间线（含 user、成员 A、成员 B、自己 的消息），断言：只含 `id > cursor` 且 `agent_id != self` 的消息；渲染为带发言人名字的文本；主持人指令单独成段。**N4**：`self` 与消息的 `agent_id` 都是**成员行 id**（不是 agent id），确保同一 agent 的两个成员也能互相看见。

**I-4 勘误**：夹具**必须给每条消息填 `AgentID`（成员行 id）**，且 `self` 也传成员行 id（原夹具用显示名 `"B"` 作 self 且消息无 `AgentID` ⇒ 自排除断言无法成立）。发言人**名字**通过 `names`（成员行 id → 显示名）映射，不再是 `map[消息 id]名字`。

```go
func TestBuildMemberInjection(t *testing.T) {
	msgs := []model.ChatMessage{
		{ID: 1, Role: "user", Content: `{"blocks":[{"type":"text","text":"问题Q"}]}`},
		{ID: 2, Role: "assistant", AgentID: "row-a", Content: `{"blocks":[{"type":"text","text":"A1"}]}`},
		{ID: 3, Role: "assistant", AgentID: "row-b", Content: `{"blocks":[{"type":"text","text":"B1"}]}`},
	}
	names := map[string]string{"row-a": "A", "row-b": "B"} // 成员行 id -> 显示名
	got := buildInjectionText(msgs, 1, "row-b" /*self*/, names, "请回应 A")
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

**Step 2-4:** TDD 循环实现 `buildInjectionText(msgs []model.ChatMessage, cursor int64, selfMemberRowID string, names map[string]string, instruction string) string`。文本抽取复用 `ExtractPlainText`（`chat.go` 已有）。**`names` 以成员行 id（`msg.AgentID`）为键**，`self` 也是成员行 id。

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
**M-3' 勘误**：`groupMemberTurn` / `groupMemberResult` 类型须在本 Task 声明（此前被引用却无定义）：

```go
type groupMemberTurn struct {
	MemberRowID string
	Prompt      string
}
type groupMemberResult struct {
	Err error
}

// groupTurnRunner runs one member's turn. Production wires it to runTurn with
// a TurnSpec whose SessionID is the member row and TimelineSessionID the group.
type groupTurnRunner func(spec groupMemberTurn) groupMemberResult
```

**Step 1: 写失败测试**

覆盖：主持人输出路由标签 → 选人 → 成员发言 → 落群时间线；**被点名多个成员时按标签顺序依次发言、后者能看到前者本轮发言**；结束信号停止；解析失败回退轮转；**最大轮数读群设置、达上限强制停**（决策 #31）；**结束后主持人产出汇总**（决策 #32）；成员失败不终止群。

**Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestGroupOrchestrator -v`
Expected: FAIL

**Step 3: 实现**

`group_orchestrator.go` 要点（伪代码见设计文档 §5.1，**v1 顺序轮次**）：
- `RunGroupTurn(ctx, groupID, userMessage)`：写用户消息到群 → 循环。
- 每轮：主持人发言（注入 + 解析）；解析失败回退轮转；被点名成员**按标签顺序依次**发言（**v1 顺序，非并发**）；每个成员发言前记录高水位 `H`，发言后 `seen_cursor = H`；检查结束/最大轮数/抢占。
- **最大轮数（决策 #31）**：默认 10，读群行 `context_state` 的配置值。**I-10 勘误**：`ContextState`（`chat.go:2200`）只认 `Mode`/`ThinkingEffort`/`Usage`，**不含 `maxRounds`**——须新增 reader `GetGroupMaxRounds(groupID) int`（用 `json_extract(context_state,'$.maxRounds')`，缺省/空返回 10）。E1 一并实现。
- **最终汇总（决策 #32，C-2 勘误）**：**两条路径各产出一次**，不得重复：
  - 正常结束信号：**发 `<clawbench-group-end/>` 那一轮的主持人输出即汇总**（`BuildHostSystemPrompt` 已要求），**不再另跑主持人**。
  - 达上限退出：**额外跑一次**主持人，系统提示用 **`BuildHostSummaryPrompt`**（只写结论、不输出路由/结束标签）；**该轮不解析路由标签**（防重入循环）。
  - 汇总作为主持人发言（`SpeakerID=主持人成员行 id`）写入群时间线。
- 主持人/成员回合都构造 `TurnSpec{SessionID: memberRowID, TimelineSessionID: groupID, SpeakerID: memberRowID, ChatReq: ...}`；`ChatReq` 用 **D1 的 `applyMemberResumeOverrides`** 修正（**C5/N1**）。**C-1 勘误**：发言人用**独立的 `TurnSpec.SpeakerID`**（成员行 id），**`TurnSpec.AgentID` 保持真实 agent id（或留空让 `ResolveAgentID` 从成员行推出）**——否则 `ResolveAgentID` 原样返回成员行 id 会毁 ACP 后端解析。
- 主持人回合的系统提示 = 成员系统提示 + `BuildHostSystemPrompt(memberNames)`（收尾轮用 `BuildHostSummaryPrompt`）。
- **N6（主持人标记机制）**：`content` 由 executor 的 `buildContentJSON`（`session_executor.go:1430`）构建，**编排器无注入 content 顶层键的钩子**；"在 content 加 `meta.role`"**不可行**。v1 靠 **`agent_id == 主持人成员行 id`** 区分主持人发言，前端据此渲染居中样式 + 解析路由卡片（决策 #35）。**不引入 content 信封标记**。
- **I9（游标语义）**：主持人也用 `seen_cursor`，语义与成员一致——**本次发言前**的群时间线高水位；发言后更新为发言前的高水位（不是发言后的最大 id）。F1 的 `buildInjectionText` 对主持人和成员是同一个函数，`self` 传各自**成员行 id**。
- **顺序不变量（C1 残留）**：串行 `await` 每个成员回合完全结束（含 Finalize）再启动下一个；群回合开始/结束时清理群时间线孤儿 `streaming=1` 行（见设计 §5.6）。
- **解析失败 2 次收尾（设计 §5.1）**：连续 2 轮路由解析失败 → 走达上限路径（额外跑收尾主持人）。

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
- Modify: **`internal/handler/chat.go`**（**R-2**：`AIChat` 在 `:30`；POST 分支加群会话委派，见下 C-3/N-5。此前 Files/commit 漏列此文件，导致 C-3 无落点）
- Modify: `internal/handler/handler.go`（`RegisterRoutes`）
- Modify: `internal/api/openapi.yaml`
- Test: `internal/handler/group_test.go`

**端点**（字段名从 handler 代码抄，勿望文生义）：
- `POST /api/group/create` — `{hostAgentId, memberAgentIds: []}` → `{ok, groupId, hostMemberId}`（**决策 #25 一步建群**：`memberAgentIds` 含全部成员**及主持人**，与群行**同一事务**创建；标题不接收，后端用主持人名占位，决策 #28；**须过会话上限门**，决策 #37）
- `POST /api/group/members` — `{groupId, agentIds: []}` → `{ok, memberIds: []}`（**I-6 勘误**：批量，body 收数组；名称由 agent 名派生写成员行 `title`，决策 #36/I-7）
- `DELETE /api/group/members` — `{groupId, memberId}` → `{ok}`
- `GET /api/group/members?groupId=` → `{ok, members:[{id, agentId, name, backend, left, **isHost**}]}`（**I-5 勘误**：须含 `isHost`，J1/K1 靠它识别主持人）
- `PATCH /api/group/settings` — `{groupId, maxRounds}` → `{ok}`（决策 #31）
- （**删除** `POST /api/group/chat`；群发送统一走 `AIChat` 的委派，见下 C-3/N-5）

**C-3 勘误（Critical，群发送链路必须接通）**：仅有 `POST /api/group/chat` 端点**不够**——前端 `sendMessage` 现在发的是 `/api/ai/chat`（`useChatSession.ts`），无人会调新端点。**采用方案 A（推荐）**：在 `AIChat`（`handler/chat.go:30`）的 **POST 分支**判断该 session 的 `session_type == 'group'` → 委派 `service.RunGroupTurn(...)`，**前端不改**。须在 G1 显式实现并加测试（往群 session POST → 走编排器而非普通单智能体回合）。

**N-5 勘误（四轮，C-3 的两个未定义依赖）**：
1. **须新增 session 类型 reader**：判断 `session_type == 'group'` 需要读 `chat_sessions.session_type`，但**全仓没有这个 reader**（只有 `store.NormalizeSessionTypeFilter`/`SessionTypeDBValue` 两个**过滤器映射**，非读取）。**M-2' 勘误**：`GetSessionFullInfo`（`chat.go:2453`）的 `SessionInfo` **不含 `session_type`**（只有 Title/Backend/AgentID/Model/Transport/AutoApprove/ProjectPath），**不是死路复用**——G1 须**新增** `service.GetSessionType(sessionID) string`（E1 已列）。
2. **委派点必须明确在 `TryClaimSessionRun` 之前**：`AIChat` POST 现有顺序是 文件校验 → `TryClaimSessionRun`（`chat.go:469`）→ `AddChatMessage`（用户消息，`:525`）→ `user_message` WS 广播（`:535`）→ 起 goroutine。而 F2 的 `RunGroupTurn` **自己也写用户消息**。故必须**在 ownership 检查之后、`TryClaimSessionRun`/`AddChatMessage` 之前**委派，否则用户消息写两次、或编排器发现会话已被 claim。须在 G1 显式列出"编排器接管哪几项职责"（文件校验 / claim / 用户消息 insert / `user_message` 广播 / running-state）。
3. **群 POST body 语义**：前端 POST 带 `agentId/modelId/transport/thinkingEffort`（`chat.go:293-304`），在群里这些是**每成员**的、非每群——须明确**忽略**。`RunGroupTurn` 的签名与 `userMessage` 类型（string？含 files 的结构体？）须定义。

`POST /api/group/chat` **删除**（避免两个入口；M-7）。

**Step 1-5:** TDD：先写 handler 测试（`httptest`），确认 404，再注册路由实现。OpenAPI 同步 `internal/api/openapi.yaml`，跑 `go test ./internal/handler/ -run TestOpenAPIDrift`。

> **注意**：`maxRounds` 存群行 `context_state` JSON，写入用 `PatchContextStateMerge`（值须为合法 JSON 数字，见设计 §12 M1）；读取用 E1 的 `GetGroupMaxRounds`（I-10）。

**Commit:**

```bash
git add internal/handler/group.go internal/handler/chat.go internal/handler/handler.go internal/api/openapi.yaml internal/handler/group_test.go
git commit -m "feat(group): add group HTTP endpoints, AIChat delegation and OpenAPI docs"
```

---

## 阶段 H：前端 `AgentSelectorDrawer` 多选

### Task H1: 加 `multiple` 模式 + 建群模式（行内指定主持人）

**Files:**
- Modify: `web/src/components/common/AgentSelectorDrawer.vue`
- Test: `web/src/components/common/__tests__/AgentSelectorDrawer.test.ts`

**Step 1: 写失败测试**：
- `multiple=true` 时渲染 checkbox、点击不关闭、`update:modelValue` 发出数组、可勾多个。
- **建群模式**（`groupMode=true`）：勾选后行右侧出现「主持」圆点；未勾选行**不出现**圆点；点圆点 `emit('update:hostId', ...)` 且**不切换勾选**（`@click.stop`）；同一时刻只有一个主持人；取消勾选主持人 → `hostId` 清空。
- 未指定主持人时确认按钮**禁用**；指定后启用，文案为「创建群聊」。
- 「加成员」模式（`multiple=true, groupMode=false`）**不渲染**主持圆点。

**Step 3: 实现**：加 `multiple?: boolean`、`groupMode?: boolean`、`hostId?: string` props；`modelValue` 类型放宽为 `string | string[]`；多选时行前渲染 checkbox，`handleSelect` 切换数组项且不关闭；建群模式下已勾选行右侧渲染「主持」单选圆点（`@click.stop` 只改主持人）；`confirmDisabled` 计算属性在建群模式且无 `hostId` 时为 true；单选路径行为完全不变（回归测试钉住）。

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

**Step 3: 实现**：加 `data-action="create-group"` 按钮，emit `create-group`；宿主转发到 App，打开 **建群模式的 `AgentSelectorDrawer`**（`multiple + groupMode`）：成员与主持人**在同一列表一起选**，点「创建群聊」一次性建群。**不再有"先建群后加成员"两步**。

**Commit:**

```bash
git add web/src/components/session/ web/src/App.vue
git commit -m "feat(group): add create-group entry point"
```

### Task I2: 建群调用与跳转（一步到位）

**Files:**
- Modify: `web/src/composables/useChatSession.ts`（加 `createGroup(hostAgentId, memberAgentIds)`）
- Test: `web/src/composables/__tests__/useChatSession.group.test.ts`

**Step 3: 实现**：`POST /api/group/create {hostAgentId, memberAgentIds}` → 成功后 `switchSession(groupId)`。标题不传（后端用主持人名占位，决策 #28）。**成员已在建群时一并创建**，前端不再追加调用 `addGroupMembers`。

**Commit:**

```bash
git add web/src/composables/useChatSession.ts web/src/composables/__tests__/useChatSession.group.test.ts
git commit -m "feat(group): create group with members in one call and switch into it"
```

---

## 阶段 J：前端群时间线渲染

### Task J1: 消息类型加 `agentId`，气泡显示发言人

**Files:**
- Modify: `web/src/utils/chatStreamUtils.ts`（`ChatMessage` 加 `agentId?`）
- Modify: `web/src/components/chat/ChatMessageItem.vue`
- Modify: `web/src/components/chat/ChatMessageList.vue`
- Modify: `web/src/composables/useChatStream.ts`（消费 `stream_start.agent_id`）
- Test: 组件测试

**Step 3: 实现**：
- `agent_id` 非空时气泡显示 `AgentIcon` + 名字（复用 `useAgents` 的 `getAgentName`）。**注意 N4**：`agent_id` 是**成员行 id**，须先用群成员列表（从 `GET /api/group/members` 加载）把行 id 映射为 agent 头像/名字；映射缺失时降级为通用样式。
- **I-5**：主持人成员行 id 从 `GET /api/group/members` 的 **`isHost` 标志**得到（**不要**假设"第一个成员即主持人"）；主持人特殊样式靠 **`agentId === hostMemberRowId`** 判断（**不要**读 `meta.role`——该机制不存在，N6）。
- **N3**：流式中的发言人来自 F0b 的 `stream_start.agent_id`（不是 DB `agentId`）；落库后来自 `ChatMessage.agentId`。两条来源都指向成员行 id。

**Commit:**

```bash
git add web/src/utils/chatStreamUtils.ts web/src/components/chat/ web/src/composables/useChatStream.ts
git commit -m "feat(group): render speaker attribution and host style in timeline"
```

---

### Task J2: 前端路由卡片解析（决策 #35）

> **决策 #35**：后端**保留**路由标签（不剥离），前端解析成"主持人 → A、B"卡片。与 Go `internal/grouprouting` **镜像**，由 parity corpus 固化（同 askquestion 约定）。

**Files:**
- Create: `web/src/utils/groupRouting.ts`（`parseGroupRouting(text)` 镜像 Go）
- Create/Modify: parity corpus（复用 `internal/grouprouting/testdata/` 或新建，双向固化）
- Modify: `web/src/components/chat/ChatMessageItem.vue`（主持人气泡内渲染路由卡片）
- Test: `web/src/utils/__tests__/groupRouting.test.ts` + parity 测试

**Step 1: 写失败测试**

```ts
import { parseGroupRouting } from '../groupRouting'

test('parses speakers and instruction', () => {
  const r = parseGroupRouting('<clawbench-speaker>A, B</clawbench-speaker> 请表态')
  expect(r.found).toBe(true)
  expect(r.speakers).toEqual(['A', 'B'])
  expect(r.instruction).toBe('请表态')
})

test('parses end signal', () => {
  expect(parseGroupRouting('充分了<clawbench-group-end/>').end).toBe(true)
})
```

**Step 3: 实现**：镜像 `internal/grouprouting/grouprouting.go` 的 `Parse`（同样的正则、同样的"不可解析不剥离"契约）。**两个分支（设计 §5.3）**：
- **可解析**：主持人气泡把**标签 span** 替换为卡片（发言人 chips）；**指令文本只渲染一次**——要么卡片含指令、正文剔除该段；要么卡片不含指令、正文保留。**不得两处都显示**。
- **不可解析**：**原样显示**（含标签），与后端"绝不丢内容"契约一致。

**parity corpus 双向测试**：同一输入 Go 与 TS 产出同结构（照 `internal/askquestion/parity_test.go` 与 `web/src/utils/askQuestion` 的先例）。

**Commit:**

```bash
git add web/src/utils/groupRouting.ts web/src/utils/__tests__/groupRouting.test.ts internal/grouprouting/testdata/
git commit -m "feat(group): frontend routing-card parser mirroring Go"
```

---

## 阶段 K：前端成员头像条 + 增删

### Task K1: 头像条与加成员

**Files:**
- Create: `web/src/components/chat/GroupMemberBar.vue`
- Create: `web/src/components/chat/GroupMemberSheet.vue`（成员管理 + 群设置，`BottomSheet`，决策 #34）
- Modify: `web/src/components/chat/ChatPanelContent.vue`
- Test: 组件测试

**Step 3: 实现**：
- 头部横向头像条（决策 #19）；尾部 `+` 打开 **`BottomSheet` 抽屉**（决策 #34）批量加成员（H1 多选，**一次提交 `{groupId, agentIds:[]}`**，I-6）。
- 抽屉内：成员列表（可移除，调 `DELETE /api/group/members`；"已离场"成员**灰显 + 标注"已离场"**，决策 #29/#34）+ **群设置（最大轮数，决策 #31）**，改后调 `PATCH /api/group/settings`。
- 成员展示名取自成员行 `title`（决策 #36）。**主持人**靠 `GET /api/group/members` 的 **`isHost`** 标注（I-5），头像条上给主持人一个可辨识标记。

**Commit:**

```bash
git add web/src/components/chat/GroupMemberBar.vue web/src/components/chat/GroupMemberSheet.vue web/src/components/chat/ChatPanelContent.vue
git commit -m "feat(group): add member bar, management sheet and group settings"
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

**场景（v1 顺序轮次）：** 建群（一步多选：成员 + 行内指定主持人）→ 发一条消息 → 断言成员气泡**按顺序**出现 + 主持人消息可见 + 人类抢占生效。既有的 host-only `POST /api/group/create`（省略 `memberAgentIds`）退化路径仍被 E2E 覆盖，后端不得移除该兼容。

Run: `npx playwright test --config e2e/playwright.config.ts --project=chromium-coverage e2e/specs/group-chat.spec.ts`
Expected: PASS

**Commit:**

```bash
git add e2e/specs/group-chat.spec.ts
git commit -m "test(group): add group chat e2e spec"
```

---

## 阶段 N：成员可见性、描述注入与系统事件（决策 #39–#44）

> **背景**：v1（A–M）已合入 main（`aa88f921a`）。本阶段是**合入后增补**——设计 §12.6 记录了三个确实存在的缺口（主持人只知道成员名字 / 增删成员不写时间线且离场者仍被点名 / 主持人被移除后仍控场）加系统事件落库的硬约束。**逐 Task TDD + 独立 commit。**

### Task N1: `chat_history` 放宽 role CHECK（新增 `role='system'`）

**Files:**
- Modify: `internal/service/database.go`（`chat_history` 建表 `:308` + 新增重建迁移函数）
- Modify: **约 20 个测试夹具**的 `CHECK(role IN ('user','assistant'))`（见设计 §12.6 清单）
- Test: `internal/service/database_group_test.go`（追加）

**Step 1: 写失败测试**：建一个含 `role='system'` 行的库，断言插入成功且 `GetChatHistoryPaged` 能读出该行。

**Step 3: 实现**：
- 建表语句的 CHECK 改为 `CHECK(role IN ('user','assistant','system'))`（新库直接生效）。
- 新增 `rebuildChatHistoryRoleCheckIfNeeded()`：用 `SELECT sql FROM sqlite_master WHERE name='chat_history'` 检测现有表是否**已含 `'system'`**，未含则整表重建（`CREATE chat_history_new → INSERT SELECT → DROP → RENAME`），**保留所有列与索引**（照抄 `rebuildChatMetadataWithoutFK` 的结构）。**必须在同一事务内**，防崩溃丢数据。
- **夹具同改**：`chat_test.go:32`、`handler/testutil_test.go`、`session_runtime_test.go:1370`、`database_test.go:61/2123/2510/3367/3724`、`drain_test.go:51`、`scheduler_test.go:26`、`scheduler_executor_test.go:28`、`scheduler_script_phase_test.go:31`、`summary_test.go:258`、`session_cleanup_test.go:351`、`chat_metadata_ledger_migrate_test.go:50`、`thinking_migrate_test.go:48`、`session_command_test.go:66`。**逐个改，不得共享常量**（R-10 先例）。

**Step 4: 运行确认通过**：`go test ./internal/service/ ./internal/handler/ -run 'Group|History|Database' -v`

**Commit:** `feat(group): allow role='system' in chat_history via table rebuild`

---

### Task N2: 成员描述取值 + 三处注入

**Files:**
- Create: `internal/service/group_agent_info.go`（`GetAgentSpecialty(agentID) string`）
- Modify: `internal/service/group_prompt.go`（`BuildHostSystemPrompt` 入参改结构体切片）
- Modify: `internal/service/group_orchestrator.go`（`activeMemberNamesExcept` → 带描述/离场状态）
- Modify: `internal/service/group_inject.go`（离场标注 + 头部参与者清单）
- Test: `internal/service/group_prompt_test.go`、`group_inject_test.go`（追加）

**Step 1: 写失败测试**：
- `BuildHostSystemPrompt` 断言渲染为 `Claude（代码编写与推理）`；离场者带 `（已离场）`；无描述时省略括号。
- `buildInjectionText` 断言离场成员名后带 `（已离场）`；头部含 `参与者：A（描述）、B（描述）`。

**Step 3: 实现**：
- `GetAgentSpecialty(agentID) string`：`model.GetAgent(agentID)` 取 `Specialty`，空则返回 `""`（调用方据此省略括号）。
- `BuildHostSystemPrompt` 入参改为 `[]HostMemberInfo{Name, Specialty string, Left bool}`；**同步改调用点** `group_orchestrator.go:200` 与测试 `group_prompt_test.go`。
- `buildInjectionText` 加 `leftIDs map[string]bool` 参数；渲染离场者名后加 `（已离场）`；头部插入参与者清单（**仅当有描述时**，避免噪音）。
- **决策 #41 明确不做**：不注入默认模型、不注入完整运行时提示词（见设计 §5.2）。

**Commit:** `feat(group): inject member specialty into host prompt, injection context`

---

### Task N3: 增删成员写系统事件

**Files:**
- Modify: `internal/service/group_store.go`（`AddGroupMember`/`RemoveGroupMember` 写系统事件）
- Modify: `internal/service/chat.go`（新增 `AddSystemMessage`，`role='system'`、`agent_id=''`）
- Modify: `internal/service/group_orchestrator.go`（系统事件广播到群）
- Test: `internal/service/group_store_test.go`（追加）

**Step 1: 写失败测试**：加成员后群时间线多一条 `role='system'` 行，内容含 `名字（描述）加入了讨论`；删成员后多一条 `XX 已离场`；**断言该行计入注入**（`id > cursor` 时出现在 `buildInjectionText`）且**不计入未读**（`role='assistant'` 过滤天然排除）。

**Step 3: 实现**：
- `AddSystemMessage(projectPath, sessionID, text) (int64, error)`：`role='system'`、`agent_id=''`、`streaming=0`，复用 `insertChatMessageTx`（N1 已放宽 CHECK）。
- `AddGroupMember` 成功后写 `"{名字}（{描述}）加入了讨论"`（**重新加入**写 `"{名字} 重新加入"`）；`RemoveGroupMember` 成功后写 `"{名字} 已离场"`。
- 广播：系统事件同样 `ws.EmitToSession(groupID, ...)`，前端按系统事件样式渲染（Task N4）。

**Commit:** `feat(group): write system events on member add/remove`

---

### Task N4: 前端系统事件渲染

**Files:**
- Modify: `web/src/utils/chatStreamUtils.ts`（`role` 已含 `'system'`，确认 `agentId` 语义）
- Modify: `web/src/components/chat/ChatMessageItem.vue`（系统事件分支）
- Test: 组件测试

**Step 3: 实现**：`role === 'system'` 渲染**居中细条**（非气泡、无头像、无 meta bar），样式走**全局 css**（`v-html` 陷阱不适用，但共享类基规则须在全局——design-guide 红线 2）。文案不解析 markdown，纯文本。

**Commit:** `feat(group): render system events as centered thin rows`

---

### Task N5: 拒绝移除主持人

**Files:**
- Modify: `internal/service/group_store.go`（`RemoveGroupMember` 加守卫）
- Modify: `internal/handler/group.go`（错误映射 409/400）
- Modify: `internal/api/openapi.yaml`（DELETE `/api/group/members` 新增 409）
- Test: `internal/service/group_store_test.go`、`internal/handler/group_test.go`（追加）

**Step 1: 写失败测试**：`RemoveGroupMember(groupID, hostMemberID)` 返回错误、主持人行 `archived` 仍为 0；handler 返回 409。

**Step 3: 实现**：`RemoveGroupMember` 先 `GetGroupHostMember(groupID)`，若 `memberID == hostID` 返回 `ErrCannotRemoveHost`；handler 映射为 409 并本地化提示"请先转移主持人"。

**Commit:** `feat(group): refuse to remove the host member`

---

## 阶段 O：排队、生命周期与降级（决策 #45–#59）

> **背景**：二轮 grill 逐分支核对代码后新增（设计 §12.7 记录了 10 个缺口）。**逐 Task TDD + 独立 commit。** 依赖阶段 N（N1 的 `role='system'` 是 O3 的前置，O2 复用 N1 的删除路径）。

### Task O1: 群消息入队 + 复用 drain loop（决策 #45/#46/#58/#72）

**Files:**
- Modify: `internal/handler/chat.go`（群委派移到 claim 之后）
- Modify: `internal/service/group_orchestrator.go`（`emitGroupTerminal` → `MarkDoneAndSendFinal`）
- Test: `internal/handler/chat_group_queue_test.go`（新建）、`internal/service/group_orchestrator_test.go`（追加）

**Step 1: 写失败测试**：群回合运行中发第二条消息 → 断言**入队**（不启动第二个编排器）、时间线只有一条用户消息；群回合结束 → drain loop 取队列继续；队列排空 → 恰好一次 `done`。

**Step 3: 实现**：
- `handler/chat.go` 的群分支移到 `TryClaimSessionRun` **之后**（与单聊同构）；忙碌时走 `AddQueuedMessage` + `queue_added`。
- 群回合的 run 包装成 `DrainConfig.ExecuteRunWithMessage`：`func(msgID int64, row service.QueuedRow) service.DrainResult { return service.RunGroupTurnForSession(...) }`。
- `emitGroupTerminal` 改为调用 `MarkDoneAndSendFinal`（群回合不自发 `done`）；`OnTurnAnswered` 复用 `EmitTurnAnsweredNotification`（逐条完成推送，不占名额）。
- **推送（决策 #72）**：终态走 `EmitSessionPushNotification(groupID,"completed")`（once-per-run 守卫），**与单聊同一函数**——现状群回合只发 WS、不推送，用户切走后收不到任何通知。测试须断言"群回合排空 → 恰好一次 push"。
- 前端队列条目的「加入本轮」按钮在群会话**不显示**（只留「插话」）；文案 key 复用/新增 i18n。

**Commit:** `feat(group): queue messages while a group turn runs and drain via RunDrainLoop`

---

### Task O2: 级联删除 + 归档保留 + 离群不误删（决策 #48）

**Files:**
- Modify: `internal/service/chat.go`（`HardDeleteSession` 删成员行）
- Modify: `internal/service/group_store.go`（`RemoveGroupMember` 刷 `updated_at`）
- Modify: `internal/store/session_queries.go`（`GetExpiredArchivedSessions` 排除 `group_member`）
- Test: `internal/service/group_store_test.go`、`internal/service/chat_test.go`（追加）

**Step 1: 写失败测试**：
- 销毁群 → 断言成员行**全部消失**。
- 归档群 → 断言成员行 `archived` **仍为 0**（传递性隐藏）。
- 移除成员 → 断言 `updated_at` 被刷新；把 `updated_at` 设为 100 天前再跑 `GetExpiredArchivedSessions` → 断言该成员行**不在**结果里。

**Step 3: 实现**：`HardDeleteSession` 在删群行前 `DELETE FROM chat_sessions WHERE group_id=? AND session_type='group_member'`（同一事务）；`RemoveGroupMember` 的 UPDATE 加 `updated_at = CURRENT_TIMESTAMP`；`GetExpiredArchivedSessions` 加 `AND session_type != 'group_member'`（双保险）。

**Commit:** `fix(group): cascade member rows on group delete, keep them on archive`

---

### Task O3: 成员失败复用 failTurn（决策 #51）

**Files:**
- Modify: `internal/service/group_orchestrator.go`（运行中失败也走 failTurn 形态）
- Test: `internal/service/group_orchestrator_test.go`（追加）

**Step 1: 写失败测试**：成员回合返回 `res.Err != ""` → 断言群时间线多一条 `role='assistant'` 的 warning block，且 `agent_id == 该成员行 id`。

**Step 3: 实现**：`defaultRunner` 在 `res.Err != ""` 时用与 `TurnSpec.failTurn`（`run_turn.go:214`）**相同的 block 结构**（`{type:'warning', text, reason: ai.ReasonBackendExit}`）写群时间线；**不新增** `role='system'` 错误消息。抽共享构造函数避免两处漂移。

**Commit:** `fix(group): surface member turn failures as warning blocks like single chat`

---

### Task O4: 群聊跳过摘要与推荐（决策 #55）

**Files:**
- Modify: `internal/service/session_executor.go`（Finalize 的 `triggerChatSummarization` 加群判断）
- Modify: `internal/service/group_orchestrator.go`（整轮结束时跑一次）
- Test: `internal/service/session_executor_test.go`、`group_orchestrator_test.go`（追加）

**Step 1: 写失败测试**：群成员回合 Finalize → 断言 `triggerChatSummarization` **未被调用**；群回合整轮结束 → 断言**调用一次**。

**Step 3: 实现**：`session_executor.go:1672` 改为 `if GetSessionType(e.timelineSID()) != "group" { triggerChatSummarization(...) }`（成员回合的 `timelineSID()` 是群行）；编排器在 `emitGroupTerminal` 前对群行调一次。

**Commit:** `perf(group): summarize once per group turn instead of per member`

---

### Task O5: 回退轮转指针 + 连续失败收尾（决策 #56）

**Files:**
- Modify: `internal/service/group_orchestrator.go`（`speakNextMember` 轮转 + 失败计数）
- Test: `internal/service/group_orchestrator_test.go`（追加）

**Step 1: 写失败测试**：连续两次路由解析失败 → 断言**两次点名不同成员**；第三次失败 → 断言**跳出循环**（不再跑满 `maxRounds`）。

**Step 3: 实现**：`RunGroupTurn` 维护 `lastFallbackIdx`（或轮转游标）与 `parseFailures`；`speakNextMember` 从游标之后取第一个未离场非主持人成员并推进游标；`parseFailures >= 2` → 走收尾路径。

**Commit:** `fix(group): round-robin fallback and abort after two parse failures`

---

### Task O6: 智能体重名收敛到单一函数（决策 #50）

**Files:**
- Modify: `internal/service/agent_store.go`（新增 `AgentNameTaken`；`SaveAgent`/`PatchAgentFields` 调用）
- Modify: `internal/handler/agent.go`（错误码映射）
- Test: `internal/service/agent_store_test.go`（追加）

**Step 1: 写失败测试**：
- `SaveAgent` 写入与**其他 id** 同名的 agent → 报错。
- `PatchAgentFields` 改名撞他人 → 报错。
- **同名但同 id**（幂等重存）→ **不报错**（否则内置后端重注册自我拒绝）。
- `DuplicateAgent` 用已存在名字 → 报错（预填是 `{源名} (复制)`，同源复制两次会撞）。

**Step 3: 实现**：`AgentNameTaken(name, excludeID string) bool`（查 `agents` 表 `name=? AND id!=?`，大小写/空白按现有约定处理）；`SaveAgent` 与 `PatchAgentFields` 在写入前调用；`DuplicateAgent` 无需单独改（走 `SaveAgent`）。handler 返回 409 + 本地化文案。**同步更新** `CopyAgentDialog.vue` 的错误显示（已有 `error` 位）。

**Commit:** `feat(agent): enforce globally unique agent names in one place`

---

### Task O7: 归档/销毁群关闭成员连接（决策 #57）

**Files:**
- Modify: `internal/handler/chat_session.go`（Archive/Destroy 遍历成员关连接）
- Test: `internal/handler/chat_session_group_test.go`（新建）

**Step 1: 写失败测试**：建群 + 2 个 ACP 成员 + 各自建连接 → 归档群 → 断言**两个成员连接都被关闭**。

**Step 3: 实现**：归档与销毁路径中，若 `GetSessionType(sessionID) == "group"`，取 `ListGroupMembers` 并逐个 `go CloseConn(member.ID)`（保持现有 goroutine 惯例，`CloseConn` 可能阻塞在 `cmd.Wait()`）。

**Commit:** `fix(group): close member connections when a group is archived or destroyed`

---

### Task O8: 群会话隐藏回溯入口（决策 #52）

**Files:**
- Modify: `web/src/components/chat/ChatMessageItem.vue`（或回溯入口所在组件）
- Test: 组件测试

**Step 3: 实现**：群会话（`isGroupSession`）不渲染「回溯到此处」入口。**后端不加守卫**（v1 前端隐藏即可；若日后开放需先解决游标回退，见设计 §12.7(10)）。

**Commit:** `feat(group): hide rewind entry for group sessions`

---

### Task O9: 头像条"发言中高亮"（决策 #59）

**Files:**
- Modify: `web/src/components/chat/GroupAvatarStack.vue`（消费 `activeSpeakerId`）
- Modify: `web/src/composables/useGroupMembers.ts` 或 `useChatStream.ts`（从 `stream_start.agent_id` / `stream_finalize` 派生）
- Test: 组件测试

**Step 3: 实现**：`stream_start.agent_id` 设为当前发言人（高亮），`stream_finalize` 或整轮 `done` 清除。**后端零改动**（`stream_hub.go:384/399` 已在线上）。

**Commit:** `feat(group): highlight the speaking member in the avatar strip`

---

### Task O10: 群 auto-approve 作用于全体成员（决策 #61）

**Files:**
- Modify: `internal/handler/chat_session.go`（`:583-590` 群分支批量写成员行）
- Modify: `internal/service/group_store.go`（新增 `SetGroupAutoApprove(groupID, enabled)`）
- Test: `internal/handler/chat_session_group_test.go`、`internal/service/group_store_test.go`（追加）

**Step 1: 写失败测试**：建群 + 2 个 ACP 成员 → PATCH `{sessionId: groupID, autoApprove: true}` → 断言**两个成员行的 `auto_approve` 均为 1**（群行可不变）；建了连接的成员 → 断言 `conn.GetAutoApprove()` 已同步。

**Step 3: 实现**：`chat_session.go:583` 的 `req.AutoApprove != nil` 分支里，若 `GetSessionType(sessionID) == "group"`，取 `ListGroupMembers` 逐个 `UpdateSessionAutoApprove(member.ID, enabled)` + `GetConn(member.ID)?.SetAutoApprove(...)`；否则保持现状。**注意**：群行自身无连接，`GetConn(sessionID)` 对群恒 nil，勿依赖它。

**Commit:** `fix(group): apply auto-approve toggle to every member row`

---

### Task O11: `isGroupSession` 改用会话类型（决策 #62）

**Files:**
- Modify: `internal/handler/chat.go`（`GET /api/ai/chat` 响应补 `sessionType`）
- Modify: `internal/api/openapi.yaml`
- Modify: `web/src/composables/useChatSession.ts`（暴露 `sessionType`）
- Modify: `web/src/components/chat/ChatPanelContent.vue`（`:350` 改判据）
- Modify: `web/src/App.vue`（把类型传下去，或经 useChatSession 共享）
- Test: `internal/handler/chat_test.go`、组件测试（追加）

**Step 1: 写失败测试**：
- 后端：`GET /api/ai/chat` 对群会话返回 `sessionType: "group"`。
- 前端：roster 为空但 `sessionType==='group'` → 断言 `isGroupSession` 为 **true**（控件仍隐藏）；roster 非空但类型是 `chat` → 为 false。

**Step 3: 实现**：后端读 `GetSessionType(sessionID)` 放进 GET 响应（与既有 `sessionBackend` 等并列）；前端 `isGroupSession` 改 `computed(() => sessionType.value === 'group')`，**名单只用于渲染成员**。注意 `useGroupMembers` 的 catch 置空行为**保持不变**（名单失败不影响类型判定）。

**Commit:** `fix(group): derive isGroupSession from session type, not roster presence`

---

### Task O12: 项目会话计数排除成员行（决策 #64）

**Files:**
- Modify: `internal/service/project_registry.go`（`ListAllProjects` 的 COUNT 加类型白名单）
- Modify: `internal/service/chat.go`（`GetConversationProjects` 的 COUNT 加类型白名单）
- Test: `internal/service/project_registry_test.go`、`internal/service/chat_test.go`（追加）

**Step 1: 写失败测试**：建 1 个群 + 3 个成员行 → 断言 `ListAllProjects` 里该项目的 `session_count` 只比建群前 **+1**（不是 +4）；`GetConversationProjects` 同理。

**Step 3: 实现**：两处子查询加 `AND session_type IN ('chat','group')`（与决策 #37 同一白名单，**不要**用 `!= 'group_member'` 黑名单，见决策 #63）。

**Commit:** `fix(group): exclude hidden member rows from project session counts`

---

### Task O13: 群消息支持附件（注入时渲染）（决策 #65/#66）

**Files:**
- Modify: `internal/handler/chat.go`（群分支放开附件校验）
- Modify: `internal/service/group_orchestrator.go`（`RunGroupTurnForSession` 接收 files）
- Modify: `internal/service/group_inject.go`（渲染用户消息时拼附件）
- Test: `internal/service/group_inject_test.go`、`internal/handler/chat_test.go`（追加）

**Step 1: 写失败测试**：
- 群会话发带附件消息 → **不再 400**；消息落库且 `files` 有值。
- `buildInjectionText` 渲染该用户消息 → 断言输出含 `[User uploaded 1 file: …]`（或引用/URL 对应前缀）。
- 断言 `content` **不含**附件标记（气泡与单聊一致）。

**Step 3: 实现**：
- `handler/chat.go:324-327` 删除群会话的 files 拒绝分支；`RunGroupTurnForSession` 增加 `files []model.FileEntry` 形参并透传到 `RunGroupTurn`，落库时传给 `AddChatMessageWithAgent`。
- `group_inject.go` 的 `buildInjectionText`：渲染 `role=='user'` 的消息时，用 `model.ClassifyAttachments(msg.Files, nil)` + `model.ApplyAttachmentPrefixes(text, ...)` 拼附件摘要（复用单聊同款函数，勿另写格式）。
- **`GetMessagesBySessionIDRaw` 已取 `files`**（`chat.go:361,379`），无需改。

**Commit:** `feat(group): carry message attachments into member injection`

---

### Task O14: 剥离主持人标签 + 去掉指令重复（决策 #67/#68）

**Files:**
- Modify: `internal/grouprouting/grouprouting.go`（`Result` 加"标签前背景"字段）
- Modify: `internal/grouprouting/testdata/parity_corpus.json`（语料补背景字段期望）
- Modify: `web/src/utils/groupRouting.ts`（**前端镜像同步**）
- Modify: `internal/service/group_inject.go`（渲染主持人发言时剥离标签、只用背景）
- Test: `internal/grouprouting/*_test.go`、`internal/service/group_inject_test.go`、`web/src/utils/__tests__/groupRouting.test.ts`（追加）

**Step 1: 写失败测试**：
- `Parse("<speaker>A</speaker> 请谈谈")` → 新字段 = `""`（标签前无内容）；`Parse("A 的观点不错 <speaker>B</speaker> 请回应")` → 新字段 = `"A 的观点不错"`。
- `buildInjectionText` 对主持人那条发言 → 断言输出**不含** `<clawbench-speaker>`，且**只出现一次**指令（末尾 `主持人要求你：…`）。
- **解析失败**（畸形标签）→ 断言**原样保留**（不剥离、不丢内容，同 askquestion 契约）。

**Step 3: 实现**：
- `grouprouting.Result` 加 `Before string`（标签前的文本，trim）；`Parse` 里 `text[:loc[0]]` 取得。**契约不变**：不可解析时仍不剥离。
- `buildInjectionText`：渲染主持人发言时，`Found` 则用 `Before`（指令不再从正文重复）；`!Found` 则原样输出全文。
- 前端 `groupRouting.ts` 镜像加同名字段；parity 语料补用例。

**Commit:** `fix(group): strip host routing tags from member injection and de-duplicate the directive`

---

### Task O15: 失败不推进游标 + warning block 不注入（决策 #69/#70）

**Files:**
- Modify: `internal/service/group_orchestrator.go`（游标推进移到成功分支）
- Modify: `internal/service/group_inject.go`（排除 warning block）
- Test: `internal/service/group_orchestrator_test.go`、`group_inject_test.go`（追加）

**Step 1: 写失败测试**：
- 成员回合返回 `Err != ""` → 断言 `GetMemberCursor(该成员)` **未变**（仍是发言前的值）。
- 主持人失败 → 同样断言游标未变。
- 时间线存在 `agent_id=X` 的 warning block → 断言 `buildInjectionText` 输出**不含**该 warning 的文本（对成员 Y 与 X 都断言）。

**Step 3: 实现**：
- `group_orchestrator.go:120` 与 `:157` 的 `SetMemberCursor` 移入 `Err == ""` 分支（主持人失败与成员失败都不推）。
- `group_inject.go`：跳过含 warning block 的消息（用与 `run_turn.go` 同源的块类型判定，勿用字符串匹配）。**注意**：失败残留仍留在时间线（决策 #69），只是不参与注入。

**Commit:** `fix(group): keep member cursor on failure and exclude warning blocks from injection`

---

### Task O16: 终态前兜住孤儿流式行（决策 #71）

**Files:**
- Modify: `internal/service/group_orchestrator.go`（`emitGroupTerminal` 内加孤儿清理）
- Test: `internal/service/group_orchestrator_test.go`（追加）

**Step 1: 写失败测试**：在群时间线手工插入一条 `streaming=1` 的 assistant 行 → 调 `emitGroupTerminal` → 断言该行变为 `streaming=0`（且 `done`/`completed` 仍各发一次）。

**Step 3: 实现**：`emitGroupTerminal` 的**第一行**（在 `SetSessionRunning(false)` 之前）调 `finalizeOrphanedStreamingMessages(groupID, "interrupt")`。**不引入等待**——调用点全在 `runner(...)` 返回之后（见设计 §12.7(19) 澄清）。幂等，重复调用安全。

**Commit:** `fix(group): finalize orphaned streaming rows before the terminal event`

---

### Task O17: 保护群讨论中的成员连接（决策 #73，细化 I4）

**Files:**
- Modify: `internal/service/group_orchestrator.go`（维护"本轮在用成员行"集合）
- Modify: `cmd/server/main.go`（`SetSessionRunningChecker` 改注入新查询）
- Test: `internal/service/group_orchestrator_test.go`（追加）

**Step 1: 写失败测试**：群回合运行中 → 断言该群**成员行**被新的检查函数判为"running"；**同时断言 `GetRunningSessionIDs()` 不含任何成员行**（会话列表/项目删除不受污染）。

**Step 3: 实现**：
- 编排器维护 `activeGroupMemberIDs`（或复用 `RegisterExternalExecution` 的机制但走独立注册表），群回合开始时登记本轮将发言的成员行、结束时清除。
- 新增 `service.IsSessionRunningForSweep(sessionID) bool` = `IsSessionRunning(sessionID) || activeGroupMember(sessionID)`；`main.go:863` 改注入它。
- **不要**把成员行塞进 `GetRunningSessionIDs`（`session_runtime.go:377`）——它喂 `chat_session.go:34,173`（列表 running 标记）与 `project_delete.go:198`（拒删运行中项目）。

**Commit:** `fix(group): keep member connections alive during a running group turn`

---

## 完成标准

- 所有阶段 Task 完成且各自 commit（**v1 不含 L0/L1，它们属 v2**）。
- **顺序轮次**：一轮内被点名成员依次发言，后者可见前者本轮发言（E2E 与单测钉住）。
- `go test ./internal/...` 全绿（隔离跑，避免与并发 agent 争抢）。
- `npx vitest run` 全绿。
- `./scripts/pre-push-checks.sh` 通过。
- 纯前端改动后 `npm run build` 供用户即时测试。
- OpenAPI 与 `internal/api/openapi.yaml` 同步，`TestOpenAPIDrift` 通过。
- **评审项验收（一轮）**：C2（`agent_id` 可写可读）、C3（群在 list/search/browse/overview 均可见、成员均隐藏）、C4（`failTurn`/metadata 落群）、C5（成员 resume 用 external_session_id）、I4（停止按钮生效）均有对应测试通过。
- **评审项验收（二轮）**：N1（ACP 成员 `SessionID` 保持池键，仅 CLI 换 extID）、N2（`CancelSession(groupID)` 能停住当前成员回合）、N3（`stream_start` 带发言人）、N4（同 agent 两成员互相可见）、N5（群分享保留归属）、N6（主持人样式按成员行 id）、N7/N8（脚手架与夹具）均有对应测试或明确实现。
- **决策验收（§10 已定）**：最大轮数默认 10 可配（#31）、结束时主持人汇总（#32）、标题占位用主持人名（#28）、**建群一步多选 + 行内指定主持人（#25/#33）**、成员管理 BottomSheet + 已离场灰显（#34）、前端路由卡片（#35）、成员名存 title（#36）、**群计入会话上限而成员不计、建群过上限门（#37）**、群设置在成员管理抽屉内（#38）均落地。
- **评审项验收（三轮）**：C-1（发言人用独立 `SpeakerID`，`TurnSpec.AgentID` 保持真实 agent id）、C-2（汇总两路径各一次）、C-3（`AIChat` 对群会话委派编排器）、C-4（A3 独立测试文件）、C-5（执行顺序 **F0b→F1→F2→F0→F3**）、I-1（E1 断言含离场）、I-2（#37 为 4 处且不误改 source 查询）、I-3（stream_start 空值省略键）、I-4（F1 夹具用成员行 id）、I-5（`GET /api/group/members` 含 `isHost`）、I-6（成员端点批量）、I-7（`AddGroupMember` 带 displayName）、I-10（`GetGroupMaxRounds` reader）均落地。
- **评审项验收（四/五轮）**：R-1（`SpeakerID` 在 C1 声明，先于 C2 使用）、R-2（G1 Files 含 `handler/chat.go`）、R-3（A3 含 `queue_store.go:217` 第二调用者）、R-4（设计 §3.1 `UpdateLastRead` 保持成员 id）、R-5（设计 §8 含 `GET /api/group/members`）、R-6（`GroupMember` 类型已声明）、R-7（E2 含 `session_resume.go`）、R-8（fork/continue INSERT 写归属）均落地。
- **增补验收（阶段 N，决策 #39–#44）**：离场成员注入标注（#39）、系统事件 `role='system'` 整表重建（#40/#43/#44）、描述注入三处（#41）、拒绝移除主持人（#42）均有对应测试通过。
- **增补验收（阶段 O，决策 #45–#66）**：群消息入队+复用 drain（#45/#46/#58）、级联删除/归档保留/离群不误删（#48）、成员失败复用 `failTurn`（#51）、群聊跳过摘要推荐（#55）、轮转+连续失败收尾（#56）、智能体重名单一函数（#50）、归档关成员连接（#57）、隐藏回溯入口（#52）、发言中高亮（#59）、群 auto-approve 批量写成员（#61）、`isGroupSession` 用会话类型（#62）、项目计数排除成员行（#64）、群消息附件注入（#65/#66）、剥离主持人标签+指令去重（#67/#68）、失败不推进游标+warning 不注入（#69/#70）、终态前兜孤儿流式行（#71）、群回合推送与单聊一致（#72）、群讨论中保护成员连接（#73，细化 I4）均有对应测试通过；**v1 明确不做**：@ 成员（#47）、离线状态（#54）、更换主持人（#53）；**已实现无需改**：群会话级设置隐藏（#60）；**已核实安全**：context_state / 未读 / activeStreams / auto-title / RAG（设计 §12.7(15)）。
