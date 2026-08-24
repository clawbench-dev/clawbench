# 排队消息持久化方案（彻底简化版）

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 彻底重排排队消息的存储与流转——排队消息入队即持久化到 `chat_history`，用 `queued` 标记列替代内存队列，一个发送端点统一"直接开跑/排队"两条路径，删除 `needs_start` 竞态回退（改用后端自愈 double-check）、前端 `appendQueueItems`/ghost-pending 防御、`drainQueueMessage` 的 `_dbMessageId` no-op。**保留** `afterSort` 和 string-id 大基数偏移（`TRANSIENT_BASE` 改名），排序域收敛为"DB id + afterSort 锚定"。排队消息在会话/服务器重启后不丢失。

**Architecture:** 后端 `POST /api/ai/queue` 入队时写 `chat_history`（加 `queue_id` + `queued` 列）+ 携带完整启动参数（agentId/modelId/thinkingEffort/modeId/transport），若会话未运行则复用 service 层 `executeStreamRunShared`/`LaunchSessionExecution` 直接启动（删 `needs_start`，用 drain 退出前 double-check 自愈）。内存 `sessionQueues` 整个删除，队列即 `WHERE queued=1 ORDER BY id`。前端发送统一走一个端点，`drainQueueMessage` 直接采用 DB id，排序保留 afterSort + 大基数偏移。

**Tech Stack:** Go（后端）、Vue 3 + TypeScript（前端）、SQLite（chat_history）

---

## 评审修正记录（2026-08-24 二次评审）

设计初稿经 code-reviewer 子智能体审查后修正，以下决策是评审结论，实施时**不得回退**：

| 编号 | 问题 | 修正决策 |
|---|---|---|
| **B1** | 删 `TRANSIENT_BASE` 后 string-id 消息按裸 `seq` 排到 DB 消息前 | **改名 `LARGE_BASE` 保留**——跨设备 `_remote`（`remote-*` id 无 afterSort）依赖大基数偏移恒排 DB 消息后 |
| **B2** | 删 `needs_start` 后 drain 退出窗口消息卡死（信号超时后到达被丢弃） | **drain 退出前 double-check** `SELECT count(queued=1)`，非空不自愈退出继续 drain |
| **B3** | `AddQueuedMessage` 裸 INSERT 绕过标题生成 + `updated_at` 更新 | **复用 `AddChatMessage` 的 title/updated_at 逻辑** |
| **B4** | 文档引用了不存在的 `WriteQueryRow` + `UPDATE...RETURNING`；DB 错误被当"队列空" | **用 `WriteBegin` 事务** + 条件 UPDATE；**区分 `sql.ErrNoRows` 与真实错误**（后者返回 error，drain 不得退出） |
| **B5** | `cancelPendingMessages` 按 `id` 匹配，落库后 numeric id 匹配不上 queueId | **扩展匹配 `m.id === queueId \|\| m.queueId === queueId`**（ChatMessage 新增 `queueId` 字段） |
| **M1** | "排序零影响"结论错：存在"排队消息落库 id < 回复占位 id"窗口 | **保留 `afterSort`**——正确性依赖它锚定 streaming 占位，而非"回复 id 更大" |
| **M2** | totalCount 与 messages 数组口径矛盾导致 `hasMore` 误判 | **方案 C**：`GetChatHistoryPaged` 返回双 count（total + queuedCount），前端 hasMore 剔除排队消息 |
| **M3** | 崩溃后残留 queued=1 无执行策略 | **不做全局扫表自愈**；用户下次发消息触发消费或 DELETE 取消 |
| **M4** | 排队消息污染 RAG 索引 | `AddQueuedMessage` 设 `indexed=1` 跳过索引，drain 后 `FinalizeStreamingMessage` 置 `indexed=0` |
| **M5** | 入队启动 goroutine 需 i18n 上下文 | **复用 service 层 `executeStreamRunShared`/`LaunchSessionExecution`**（无 `r` 依赖），非 `chat.go` 版本 |
| **D1** | B2 自愈机制实现选择（2026-08-24 拍板） | **入队后延迟复查 goroutine**（改入队路径），不碰 `RunDrainLoop` 核心循环 |
| **D2** | 分页方案 C API（2026-08-24 拍板） | `GetChatHistoryPaged` 返回 `(messages, total, queuedCount, err)` |
| **D3** | `POST /api/ai/chat` 去留（2026-08-24 拍板） | **彻底合并**：`POST /api/ai/chat` 发送能力删除，统一走 `/api/ai/queue`；fallback 路径改走 queue + 补 queueId |

---

## 现状痛点

所有复杂度源于三个决策的叠加：

1. **排队消息不入库** → 无 DB id → 前端必须用 `seq`/`TRANSIENT_BASE`/`afterSort` 临时排序
2. **内存队列 + 前端补挂** → `appendQueueItems`、ghost-pending 清理、`_dbMessageId` no-op 三重防御
3. **两个发送端点**（`/api/ai/chat` 直接发、`/api/ai/queue` 排队）+ `needs_start` 竞态回退

**排序影响已验证**（6 场景实测，`sortMessages`）：排队消息无论 pending 还是落库、id 顺序如何，视觉顺序恒为 `问题 → 回复 → 排队B → 排队C`。方案对排序视觉零影响。**但排序机制不能全部删除**——`afterSort` 锚定和 string-id 大基数偏移是正确性依赖（见"前端改动点"的排序机制修正版），本次只删 `_dbMessageId` no-op、`appendQueueItems`、ghost-pending 等防御逻辑，不删排序基础设施。

---

## 存储方案：`chat_history` 两列，删除内存队列

### 表结构变更

`internal/service/database.go:222` 现有 `chat_history` 追加两列：

```sql
-- database.go 的 schema migrations 区块追加（沿用 :517 起的 ALTER TABLE 模式）：
-- Migrate: add queue columns to chat_history for queued-message persistence.
ALTER TABLE chat_history ADD COLUMN queue_id TEXT DEFAULT '';
ALTER TABLE chat_history ADD COLUMN queued INTEGER NOT NULL DEFAULT 0;
```

### 内存队列删除

`internal/service/queue.go` 的 `sessionQueues` sync.Map、`sessionDrainChans`、`getOrCreateEntry`/`EnqueueMessage`/`DequeueMessage`/`WaitForEnqueue`/`GetQueue`/`RemoveQueueItem*`/`ClearQueue` **全部删除**。

队列即查询：

```sql
-- 入队顺序（FIFO）
SELECT id, queue_id, content, files FROM chat_history WHERE session_id = ? AND queued = 1 ORDER BY id ASC
```

### 消费标记

drain 时 `UPDATE chat_history SET queued = 0 WHERE id = ?`（而非 DELETE——消息本身保留为正常对话记录）。`queue_id` 保留用于取消时的精确定位。

### 入队写库：复用 `AddChatMessage` 的标题/updated_at 逻辑（必须）

排队消息落库不能是裸 `INSERT`。`internal/service/chat.go:339-357` 的 `AddChatMessage` 在 `role=user && count==1` 时更新会话标题，并在 `:334` 更新 `chat_sessions.updated_at`（驱动会话列表排序）。新方案下所有消息先落库 queued=1 再 drain，若 `AddQueuedMessage` 只是 INSERT：
- 新会话第一条消息永远不触发标题生成（drain 时 count>1，标题分支不再触发）
- 会话 `updated_at` 不刷新，入队后会话不会浮到列表顶部

**必须**：`AddQueuedMessage` 复用 `AddChatMessage` 的标题生成 + `updated_at` 更新逻辑（可抽共享 helper），仅额外设 `queued=1` 和 `queue_id`。

---

## 一个发送端点（关键架构变更）

### 现状两条路径

| 路径 | 端点 | 场景 |
|---|---|---|
| 直接发 | `POST /api/ai/chat` | AI 空闲 |
| 排队 | `POST /api/ai/queue` | AI 生成中，`needs_start` 处理竞态 |

### 改后：`/api/ai/queue` 统一处理

`POST /api/ai/queue` 请求体补全启动参数（对齐 `/api/ai/chat`）：

```json
{
  "message": "...",
  "queueId": "pending-...",
  "filePaths": [],
  "files": [],
  "agentId": "...",
  "modelId": "...",
  "thinkingEffort": "...",
  "modeId": "...",
  "transport": "...",
  "clientId": "..."
}
```

后端逻辑：

```
1. 写 chat_history（queued=1）
2. TrySetSessionRunning
   ├─ 成功 → 启动 goroutine（同现有 /api/ai/chat 的启动路径，RunDrainLoop 消费队列）
   └─ 失败 → 会话已运行，goroutine 的 drain loop 会拾取该行（queued=1）
3. 返回 { ok: true }——不再有 needs_start
```

**`needs_start` 删除 + 自愈机制（必须）**：会话"恰好刚结束"的竞态不再需要前端 resubmit——后端检测到未运行就**自己启动**，消息永不失。前端 `enqueueAndMaybeStart` 的 `resubmit` 回调、`needsStart` 分支全部删除。

但删 `needs_start` 后存在一个**消息卡死窗口**（B2，需替代机制）：
```
1. drain loop: WaitForEnqueue 超时 → 准备发 done
2. 用户入队: AddQueuedMessage(queued=1) → TrySetSessionRunning 失败(仍 running) → 信号到达
3. drain loop: MarkDoneAndSendFinal → SetSessionRunning(false) → return（信号被丢弃）
4. 排队消息永久卡在 queued=1，无人消费
```
现状 `needs_start`（queue.go:81-95）恰好兜住这个窗口。改后必须用**后端自愈**替代：`RunDrainLoop` 退出前做队列 double-check——`MarkDoneAndSendFinal` 之前再查一次 `SELECT count(*) FROM chat_history WHERE session_id=? AND queued=1`，非空则不自愈退出而是继续 drain；或入队接口在 `TrySetSessionRunning` 失败时启动"延迟复查 goroutine"（100ms 后若仍无运行则自己接管启动）。**文档必须包含此机制，否则排队消息在窗口内静默丢失。**

### 前端发送路径合流

```
sendMessage / handleToolSendMessage
  └─ 统一 enqueueAndMaybeStart
       ├─ pushMessage（乐观 pending 气泡）
       └─ POST /api/ai/queue（带全部启动参数）
```

不再有"空闲走 chat、生成中走 queue"的双路径判断。

---

## 消息流转（改后）

```
用户输入
  ├─ 前端：push pending 气泡（id=queueId）
  ├─ POST /api/ai/queue → 后端写 chat_history(queued=1)
  │     ├─ 会话未运行 → 后端直接启动 goroutine
  │     └─ 会话运行中 → drain loop 稍后拾取
  │
  ├─ 后端 drain loop：SELECT queued=1 ORDER BY id → 取第一条
  ├─ 后端：UPDATE queued=0（保留为正常消息）
  ├─ 后端：emit queue_drain（含真实 db_id + queueId）
  ├─ 前端：drainQueueMessage
  │     ├─ finalize 当前 streaming 回复
  │     ├─ 按 queueId 找到 pending 气泡 → 采用 db_id，去 pending
  │     └─ push 新 streaming 占位（用 db_id 排序，天然锚定在问题后）
  └─ 后端：executeStreamRun(该消息) → 继续流式
```

**loadHistory 天然返回排队消息**（`GetChatHistoryPaged` `SELECT ... WHERE session_id=? ORDER BY id` 不分排队状态），`appendQueueItems` 删除。

---

## 前端改动点

### 排序机制（修正版——保留 afterSort，TRANSIENT_BASE 改名保留）

**不能删 `afterSort`**（M1）：正确性依赖它把 streaming 占位锚定在问题后，而非"回复 db_id 更大"。存在窗口：drain `DequeueQueuedMessage` 之后、`executeStreamRun` 创建 streaming 占位（`chat.go:681`）**之前**，新排队消息落库 id 可能**小于**当前回复占位 id。纯 `id ASC` 排序会把排队消息排到正在生成的回复之前。`messageSortValue`（`chatStreamUtils.ts:344`）的 `afterSort` 分支（:353）必须保留。

**不能删 `TRANSIENT_BASE`**（B1）：`seqCounter` 从 1 递增（:311），删掉大基数偏移后，跨设备 `_remote` 消息（`useChatStream.ts:669` `remote-*` string id，无 afterSort）会按裸 `seq=3` 排到 `db_id=5000` 的历史消息**前面**。**必须保留大基数偏移**（可改名，语义不变）：string-id 消息按 `LARGE_BASE + seq` 恒排 DB 消息之后。

**简化后的 `messageSortValue`**：
```ts
function messageSortValue(m: ChatMessage): number {
  if (typeof m.afterSort === 'number') return m.afterSort   // 保留
  if (typeof m.id === 'number') return m.id                  // DB 消息
  return LARGE_BASE + (m.seq ?? 0)                           // 保留大基数（改名）
}
```

排队消息落库后走 `typeof m.id === 'number'` 分支，用 DB id 排序；`pending` 气泡（string id）仍走大基数分支——两者视觉位置天然一致（与现状实测相同）。**现状的 6 场景排序回归测试全部保留，作为正确性护栏。**

### 删除

| 组件 | 删除内容 |
|---|---|
| `chatStreamUtils.ts` | `_dbMessageId` no-op 参数（`drainQueueMessage` 直接采用 DB id）、string-id 保持逻辑、防御性 fallback 匹配（:448-522） |
| `useChatSession.ts` | `appendQueueItems`（:105）、ghost-pending 清理（:267-295） |
| `chatQueueSend.ts` | `needsStart`/`resubmit` 分支（配合后端自愈，见上） |
| `useChatStream.ts` | `queue_cancel` 处理逻辑扩展（见下） |

### 简化与保留边界

- **`cancelPendingMessages` 必须扩展匹配**（B5）：现状按 `m.id` 匹配（`chatStreamUtils.ts:558`）。排队消息被 loadHistory 落库成 DB 行后，id 从 string（queueId）变 numeric，`queue_cancel` 携带的 queueId 匹配不上 → pending 气泡残留。改为同时按 `m.id === queueId || m.queueId === queueId` 匹配（ChatMessage 新增 `queueId` 字段）。现有 6 个用例保留，补 DB 行匹配用例。
- **`syncSessionState` 保留一个防御**：loadHistory 响应带 `queue_id` 字段，前端发现"有 queue_id 的消息"且"无对应乐观气泡"时补 pending 标记（只匹配不重建，替代 appendQueueItems）。
- **`user_message` 事件**（`chat.go:480`/`:507`）入队分支带真实 msgID（>0），`_remote`/`_remoteQueueId` 机制简化或删除。

---

## 后端改动点

| 文件 | 改动 |
|---|---|
| `internal/service/database.go` | 加 `queue_id` + `queued` 列迁移 |
| `internal/service/queue.go` | **整个删除**（内存队列代码） |
| `internal/service/drain.go` | `RunDrainLoop`：队列改为 `SELECT queued=1 ORDER BY id`；drain 时 `UPDATE queued=0`；`queue_drain` 事件带真实 id；删 `PersistUser`（行已存在） |
| `internal/handler/queue.go` | `handleQueueEnqueue`：写 chat_history + 补启动参数 + 未运行则启动 goroutine；删 `needs_start` 分支 |
| `internal/handler/chat.go` | `:462` 入队分支改为同一落库逻辑；删 `needs_start` 相关；`GET /api/ai/chat` 响应删 `queue` 字段（需确认 Android/Electron 无外部依赖，见风险 m7） |
| `internal/service/session_runtime.go` | `CancelSession`（:446）：删 `ClearQueue`，改 `UPDATE chat_history SET queued=0 WHERE session_id=?`；`ForceCancelSession`（:496）同理 |
| `internal/service/chat.go` | `GetChatHistoryPaged`/`GetChatHistory`/`GetMessagesBySessionID`/`GetMessageByID` 共 6+ 处 SELECT 加 `queue_id`/`queued` 列；`scanMessages` 映射到 `msg.QueueID`/`msg.Queued` |
| RAG 索引策略 | **排队消息设 `indexed=1` 跳过 RAG 索引**（M4）：`GetUnindexedMessages`（chat.go:1496）查 `indexed=0 AND streaming=0`，若 `AddQueuedMessage` 设 `indexed=0`，排队消息（含用户取消的）会被索引为"无回复的问题"污染会话搜索。入队时 `indexed=1`，drain 后由 `FinalizeStreamingMessage` 置 `indexed=0` 再索引 |

### 入队即启动 goroutine 的复用（修正版——用 executeStreamRunShared，非 chat.go）

`chat.go:528` 起的 goroutine 启动逻辑强依赖 `r *http.Request` 做 i18n（`T(r, "BackendCreateFailed")` 等，chat.go:656/674），且 `chat.go:441-459` 的 `UpdateSessionModel`/`UpdateSessionTransport`（含 `CloseConn`）/ACP `SetAutoApprove` 前置处理都在 handler。

**已有无 `r` 的等价实现**：`internal/service/executeStreamRunShared`（`session_command.go:571`）+ `LaunchSessionExecution`，供钉钉/飞书推送路径复用。**queue handler 应复用 service 层版本**，而非从 `chat.go` 抽——避免把 i18n 上下文带入 service 层。若两者行为有差异，实施时先对齐。

**缺的检查**：queue handler 需在启动前验证 session 存在（`GetSessionBackend` 空 → 404 `SessionNotFound`），与 `chat.go` 的 session 解析对齐。

---

## 竞态与边界

### 1. 取消语义（保留，行为对齐）

现状：取消 = 内存队列移除 + `queue_cancel` 删前端气泡。
改后：取消 = `UPDATE chat_history SET queued=0 WHERE queue_id=?` + `queue_cancel` 事件（仍保留，前端删 pending 气泡）。已取消消息变成"无回复的用户消息"保留在 DB——与普通已发消息一致，是**行为改进**（现状重启会丢）。

### 2. 排队消息被 loadHistory 提前看到

排队消息落库后，loadHistory 会把它当普通 user 消息返回（无 pending 标记、有 db_id）。前端需在 `syncSessionState` 中识别"queued 但未 drain"的消息并补 pending 标记——或**接受**：前端乐观气泡（queueId）与 DB 行（db_id）短暂并存，靠 `drainQueueMessage` 的 queueId 匹配合并。

**决策**：loadHistory 响应带 `queue_id` 字段，前端在 `syncSessionState` 中若发现"有 queue_id 的消息"且"无对应乐观气泡"，补一个 pending 标记（替换 appendQueueItems，但只匹配不重建）。这是唯一保留的防御。

### 3. 入队落库失败

写 DB 失败 → 返回错误，前端已有处理（删 pending 气泡 + toast）。

### 4. 会话关闭/崩溃（残留排队消息的执行策略，必须明确）

排队行已落库（queued=1）但从未消费。`queued=1` 行只在"会话恢复运行"时才被消费，而会话只有用户**再次发消息**才恢复运行——崩溃时当前流 + 排队消息全部卡在 queued=1。

**决策**：服务器启动时不做全局扫表自愈（避免误启动会话）。残留排队消息的处理策略：
- 用户下次打开该会话 → loadHistory 显示为 pending 气泡（有 queue_id）
- 用户下次发消息 → 会话恢复运行 → drain loop 拾取消费
- 用户可用 DELETE 取消

需补测试：`TestDequeueQueuedMessage_AfterRestart`（重启后 queued=1 行仍可被消费）、`TestSessionList_ShowsQueuedPending`（会话列表正确显示 pending 状态）。**不能**只测"重启后 drain loop 消费"，必须测"会话未运行的残留"。

### 5. 跨设备

`user_message` 带真实 msgID，其他设备按 DB id 显示。`_remote` 简化。

### 6. 分页口径（hasMore 矛盾，方案 C 解决）

`GetChatMessageCount`（`chat.go:134`）当前 `COUNT(*)` 含 streaming。排队消息落库后 totalCount 会含 queued 行，`GetChatHistoryPaged`（`chat.go:59-104`）的 messages 数组也含 queued 行。问题在 `hasMore = messages.length < totalMessages`（`useChatSession.ts:467`）会把排队消息计入 messages.length，导致历史未加载完就 `hasMore=false`。

**决策（方案 C）**：`GetChatHistoryPaged` 返回两个 count——`total`（含排队）+ `queuedCount`（仅 queued=1）。前端 `hasMore` 剔除排队消息后比较（见"设计点 3"）。`GetChatMessageCount` 保持 `COUNT(*)` 不变。

---

## 测试计划（详细）

### 后端 Go 测试

#### 1. 迁移层测试（`database_test.go` 追加）

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestMigrate_QueueColumns_NewDB` | 新建数据库含 `queue_id`/`queued` 列 | `pragma_table_info` 返回两列，默认值正确 |
| `TestMigrate_QueueColumns_ExistingDB` | 旧数据库 ALTER TABLE 加列 | 迁移前后 `chat_history` 行数据不变，新列默认值 `''` / `0` |
| `TestMigrate_QueueColumns_Idempotent` | 重复迁移不报错 | `hasQueueID == 1` 仍成立，无 duplicate column 错误 |

#### 2. DB 队列函数测试（新增 `chat_queue_test.go`）

**`AddQueuedMessage`**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestAddQueuedMessage_Basic` | 写入 chat_history queued=1 | 返回 id > 0；DB 行 `queued=1`, `queue_id` 非空 |
| `TestAddQueuedMessage_WithFiles` | 带 files 入队 | files JSON 正确保存 |
| `TestAddQueuedMessage_QueueID` | 自定义 queueId | `queue_id` 列值 = 传入的 queueId |
| `TestAddQueuedMessage_AutoQueueID` | 空 queueId 自动生成 | `queue_id` 列值为 `q-` 前缀自动生成值 |
| `TestAddQueuedMessage_MultipleSessions` | 多会话入队不交叉污染 | 各会话各自 `queued=1` 行数正确 |

**`DequeueQueuedMessage`**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestDequeueQueuedMessage_FIFO` | 多条排队消息按 id 顺序出队 | 第一次出队 id 最小，第二次 id 次小 |
| `TestDequeueQueuedMessage_Empty` | 无排队消息 | 返回 `(msg, false)` |
| `TestDequeueQueuedMessage_SetsQueuedZero` | 出队后 queued 变为 0 | DB 行 `queued=0`，消息保留 |
| `TestDequeueQueuedMessage_Atomic_NoDoubleConsume` | 两个 goroutine 并发出队同一条 | 恰好一个成功，另一个返回 false（用 `WriteBegin` 事务 + 条件 UPDATE，**不是 `UPDATE...RETURNING`——`internal/service/` 无此 helper，只有 `WriteExec`/`WriteBegin`（database.go:51/74），需手动查 id 再 UPDATE，事务内保证原子**） |
| `TestDequeueQueuedMessage_SkipsNonQueued` | 混合 queued=0 和 queued=1 行 | 只出队 queued=1 的行 |
| `TestDequeueQueuedMessage_WithFiles` | 带文件的出队 | files 正确反序列化到 `msg.Files` |
| `TestDequeueQueuedMessage_PreservesQueueID` | 出队消息携带 queue_id | `msg.QueueID` 正确 |
| `TestDequeueQueuedMessage_DBError` | **DB 错误（非空队列）** | 返回 error（不是 false），drain loop 不得当"队列空"退出——否则静默丢消息。须区分 `sql.ErrNoRows`（真空）与其他错误 |

**`ClearQueuedMessages`**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestClearQueuedMessages_All` | 清空会话所有排队消息 | `queued=1` 行数变为 0；`queued=0` 行不受影响 |
| `TestClearQueuedMessages_OtherSessionUntouched` | 清空 A 不影响 B | B 的 queued=1 行数不变 |

**`CancelQueuedMessage`**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestCancelQueuedMessage_ByQueueID` | 按 queueId 取消单条 | 该条 `queued=0`，其余 `queued=1` 不变 |
| `TestCancelQueuedMessage_NotFound` | 不存在的 queueId | 无行被修改，不报错 |
| `TestCancelQueuedMessage_EmptyQueueID` | 空 queueId | 无行被修改（需显式调用 ClearQueuedMessages） |

**`GetQueuedQueueIDs`**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestGetQueuedQueueIDs_WithMessages` | 有排队消息 | 返回所有非空 queue_id |
| `TestGetQueuedQueueIDs_Empty` | 无排队消息 | 返回空切片 |
| `TestGetQueuedQueueIDs_SkipsEmptyQueueID` | 有空 queue_id 的排队消息 | 空字符串不在结果中 |

**`GetQueuedCount`**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestGetQueuedCount_WithMessages` | 有排队消息 | 返回正确数量 |
| `TestGetQueuedCount_NoMessages` | 无排队消息 | 返回 0 |
| `TestGetQueuedCount_ExcludesConsumed` | queued=0 行不计 | 仅计 queued=1 |

**`GetQueuedMessages`**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestGetQueuedMessages_Order` | 按 id ASC 返回 | 顺序正确 |
| `TestGetQueuedMessages_Empty` | 无排队消息 | 返回 nil 或空切片 |

#### 3. `GetChatMessageCount` 更新测试（`chat_test.go` 追加）

**决策对齐**（见竞态 6）：`GetChatMessageCount` 保持 `COUNT(*)` 含 queued 行，与 `GetChatHistoryPaged` 的 messages 数组口径一致。前端 `hasMore` 计算在 `syncSessionState` 中剔除排队消息。

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestGetChatMessageCount_IncludesQueued` | 有 queued=1 行时计入 totalCount | count = 全部行数（含 queued） |
| `TestGetChatMessageCount_QueuedBecomesConsumed` | queued=1 变为 0 后仍计入 | count 不变（drain 不删行） |
| `TestGetChatMessageCount_NonExistent` | 不存在的会话 | 返回 0 |
| `TestGetChatHistoryPaged_HasMoreWithQueued` | 历史 50、limit=40、排队 15 | 返回 messages=55、total=55，前端 hasMore 剔除排队后正确=false |

#### 4. `scanMessages` 更新测试（`chat_test.go` 追加）

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestScanMessages_QueueFields` | 返回 queueId/queued 字段 | `msg.QueueID` / `msg.Queued` 正确 |
| `TestScanMessages_QueuedMessage` | 排队消息返回 queued=true | `msg.Queued == true` |
| `TestScanMessages_NormalMessage` | 普通消息返回 queued=false | `msg.Queued == false`，`msg.QueueID == ""` |

#### 5. Drain loop 测试（改写 `drain_test.go`）

**删除**：`PersistUser` 相关 mock 逻辑（drain 不再写 DB，入队时已写）

**改写**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestDrainLoop_DequeueFromDB` | drain loop 从 DB 出队 | `DequeueQueuedMessage` 被调用；返回的消息 `queued=0` |
| `TestDrainLoop_FIFO_MultipleMessages` | 多条排队消息按 FIFO 消费 | 执行顺序 = 入队顺序 |
| `TestDrainLoop_QueueEmpty_EmitsDone` | DB 无 queued=1 行 | 发射 `done` 事件 |
| `TestDrainLoop_UserCancel_MarksQueuedZero_EmitsCancel` | 取消时 `ClearQueuedMessages` + 发射 `queue_cancel` | DB 所有 `queued=0`；`queue_cancel` 含正确 queueIDs |
| `TestDrainLoop_UserCancel_NoQueueIDs_NoEvent` | 无排队消息时取消 | 不发 `queue_cancel` |
| `TestDrainLoop_WaitForEnqueue_SignaledBySignalDrain` | 入队信号唤醒 | `WaitForEnqueue` 在 `SignalDrain` 后返回 true |
| `TestDrainLoop_WaitForEnqueue_Timeout` | 无信号超时 | `WaitForEnqueue` 返回 false |
| `TestDrainLoop_DoesNotCallPersistUser` | drain 不再调用 PersistUser | PersistUser callback 不存在于 DrainConfig |
| `TestDrainLoop_ExitWindow_SelfHeal` | **B2 自愈**：drain 超时准备发 done 时，排队消息恰好到达 | drain loop 不退出，先消费完排队消息再发 done（double-check 机制生效） |

#### 6. Handler 测试（改写 `queue_test.go` + `chat_test.go`）

**`queue_test.go` 改写**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestQueueHandler_Enqueue_PersistsToDB` | 入队写 DB | DB 有 `queued=1` 行；返回 `{ok: true}` |
| `TestQueueHandler_Enqueue_WithFilePaths` | 带 filePaths 入队 | DB 行 files 正确 |
| `TestQueueHandler_Enqueue_WithFiles` | 带 files 入队 | DB 行 files JSON 正确 |
| `TestQueueHandler_Enqueue_MissingSessionID` | 缺 session_id | 400 |
| `TestQueueHandler_Enqueue_InvalidJSON` | 非法 JSON | 400 |
| `TestQueueHandler_Enqueue_EmptyMessage` | 空消息 | 400 |
| `TestQueueHandler_Enqueue_SessionRunning_DoesNotStartGoroutine` | 会话已运行 | 返回 `{ok: true}`，`TrySetSessionRunning` 不再被调用 |
| `TestQueueHandler_Enqueue_SessionNotRunning_StartsGoroutine` | 会话未运行 | 后端启动 goroutine；返回 `{ok: true}` |
| `TestQueueHandler_Enqueue_NoNeedsStart` | 永不返回 needs_start | 响应无 `needs_start` 字段 |
| `TestQueueHandler_Enqueue_SignalsDrain` | 入队后发信号 | `SignalDrain` 被调用（或 drain loop 唤醒验证） |
| `TestQueueHandler_Get_ReturnsQueuedMessages` | GET 返回 DB 排队消息 | 返回的 queue 内容 = DB queued=1 行 |
| `TestQueueHandler_Get_EmptyQueue` | 无排队消息 | 返回 `{queue: []}` |
| `TestQueueHandler_Delete_ByQueueID` | 按 queueId 取消 | 该条 `queued=0`；返回剩余队列 |
| `TestQueueHandler_Delete_ClearAll` | 清空全部 | 所有 `queued=0`；返回 `{ok: true}` |
| `TestQueueHandler_Delete_InvalidIndex` | 非法 index | 400 |
| `TestQueueHandler_MethodNotAllowed` | PUT 返回 405 | 405 |
| `TestQueueHandler_Enqueue_CrossProject_403` | 跨项目入队 | 403 |
| `TestQueueHandler_Get_CrossProject_403` | 跨项目 GET | 403 |
| `TestQueueHandler_Delete_CrossProject_403` | 跨项目 DELETE | 403 |
| `TestQueueHandler_Enqueue_FilesNoDuplicate` | filePaths + files 无重复 | DB 行 files 合并后无重复 |
| `TestQueueHandler_Delete_MissingSessionID` | 缺 session_id | 400 |

**`chat_test.go` 入队分支改写**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestAIChat_EnqueuePath_PersistsToDB` | 入队路径 **写入** DB（反转原 NoDBPersist） | `GetChatHistory` 有该消息；`queued=1` |
| `TestAIChat_EnqueuePath_UserMessageEmit_WithMsgID` | 入队路径 `user_message` 事件带真实 msgID | `MessageID > 0`（不再为 0） |
| `TestAIChat_EnqueuePath_FilesNoDuplicate` | 入队带文件无重复 | DB 行 files 正确 |
| `TestAIChat_EnqueuePath_MultipleSessionsNoCrossContamination` | 多会话入队无交叉 | 各会话 DB 各自独立 |
| `TestAIChat_EnqueueThenDrain_SinglePersist` | 入队时已落库，drain 不重复写入 | 入队 + drain 后 DB 仅一条记录 |
| `TestAIChat_DirectPath_PersistsWithQueuedZero` | 直接发送（非入队）写 DB queued=0 | `queued=0` |

#### 7. Session 运行时 / 取消测试（改写 `session_runtime_test.go` + `session_command_test.go`）

**`session_runtime_test.go` 追加**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestCancelSession_ClearsQueuedMessages_DB` | 取消时 DB 所有 queued=1 → 0 | `GetQueuedCount == 0` |
| `TestCancelSession_EmitsQueueCancel_WithQueueIDs` | 取消有排队消息时发射 queue_cancel | WS 事件含 queueIDs |
| `TestCancelSession_NoQueue_NoQueueCancelEvent` | 取消无排队消息时不发射 | 无 queue_cancel 事件 |
| `TestForceCancelSession_ClearsQueuedMessages_DB` | 强制取消时 DB 清除 | `GetQueuedCount == 0` |
| `TestForceCancelSession_NoQueueCancelEvent` | 强制取消不发 WS 事件 | 无 queue_cancel（客户端已断开） |

**`session_command_test.go` 改写**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestRunDrainLoop_UserCancel_DB` | cancel 分支用 DB 队列 | `ClearQueuedMessages` 被调用；queue_cancel 含 queueIDs |
| `TestRunDrainLoop_DrainQueue_DB` | drain 从 DB 消费 | `DequeueQueuedMessage` 被调用；执行顺序正确 |
| `TestRunDrainLoop_DoneNoQueue_DB` | DB 无排队消息时 done | 正常结束 |
| `TestSendMessageToSessionFromPush_AlreadyRunning_EnqueuesToDB` | 推送入队写 DB | DB 有 queued=1 行 |
| `TestSendMessageToSessionFromPush_NotRunning_StartsGoroutine` | 推送入队启动 goroutine | `TrySetSessionRunning` 成功 |
| `TestSendMessageToSessionFromPush_AlreadyRunning_SignalsDrain` | 推送入队信号 drain | `SignalDrain` 被调用 |

#### 8. SessionMessenger 接口测试（`cmd/server/main_test.go` 或独立）

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestDingtalkSessionMessenger_EnqueueMessage_PersistsToDB` | 钉钉入队写 DB | DB 有 queued=1 行 |
| `TestDingtalkSessionMessenger_ClearQueuedMessages_DB` | 钉钉清除排队 | DB queued=0 |
| `TestFeishuSessionMessenger_EnqueueMessage_PersistsToDB` | 飞书入队写 DB | DB 有 queued=1 行 |
| `TestFeishuSessionMessenger_ClearQueuedMessages_DB` | 飞书清除排队 | DB queued=0 |

#### 9. 会话重启恢复测试（新增 `chat_queue_test.go`）

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestQueuedMessage_PersistsAcrossRestart` | 入队 → 模拟重启（清除内存） → 重新查询 | DB 仍有 queued=1 行 |
| `TestQueuedMessage_DrainedAfterRestart` | 重启后 drain loop 消费遗留排队消息 | 消息被正常消费，queued=0 |

#### 10. 整体删除的测试文件/用例

| 文件 | 操作 | 理由 |
|---|---|---|
| `internal/service/queue_test.go` | **整个删除** | 内存队列 API 全部删除，19+ 用例不再适用 |
| `drain_test.go` 中 `PersistUser` 相关 mock | **删除** | drain 不再写 DB |
| `chat_test.go` 的 `TestAIChat_EnqueuePath_NoDBPersist` | **删除** | 行为反转，改写为新用例 |
| `chat_test.go` 的 `TestAIChat_UserMessageEmit_EnqueuePath` | **改写** | MessageID 从 0 变为 >0 |
| `queue_test.go` 的 `TestQueueHandler_Enqueue_NeedsStartWhenSessionNotRunning` | **删除** | needs_start 机制删除 |
| `queue_test.go` 的 `TestQueueHandler_Enqueue_NoNeedsStartWhenSessionRunning` | **删除** | needs_start 机制删除 |

---

### 前端 Vitest 测试

#### 1. `chatStreamUtils.test.ts` 改写

**`drainQueueMessage` describe 块**：

**删除的用例**：
- `keeps the found pending message transient (string id)` — pending 消息现在有 DB id
- `uses a string drain id (transient) even when dbMessageId is provided` — 现在直接采用 db_id
- `assigns stable drain ID to the pushed user message` — 不再生成 drain id，用 db_id
- `_drain marker enables loadHistory self-cleaning` — `_drain` 标记删除
- `drain ID does not collide with DB numeric IDs` — 不再使用 drain id
- `drain ID does not collide with optimistic push local- IDs` — 不再使用 drain id
- `auto-generates drain ID when not provided` — 不再生成 drain id
- `assigns drain ID to the pushed user message` — 不再使用 drain id
- `falls back to push when no matching pending message found` — 防御性 fallback 删除
- `finds _remote message by content and clears flag` — `_remote` 简化
- `preserves a numeric id on a drained cross-device _remote message` — `_remote` 简化
- `prefers pending match over _remote match` — 不再需要 `_remote` 匹配
- `falls back to content match when queueId not provided` — queueId 始终存在

**改写的用例**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `finalizes streaming assistant and pushes new streaming placeholder` | 保留，验证 finalize + new streaming | streaming 气泡被 finalize，新 streaming 占位被 push |
| `finds pending message by queueId and adopts db_id` | 按 queueId 找到 pending 气泡，采用 db_id | `msg.id === dbMessageId`；`pending` 被删除 |
| `queueId match resolves even when content differs` | queueId 匹配不受 content 影响 | queueId 匹配优先 |
| `FIFO: first drain clears first pending by queueId` | 多条排队消息 FIFO drain | 先入队的先被 drain |
| `inserts streaming assistant AFTER the drained user message` | streaming 占位在用户消息后 | sortMessages 后位置正确 |
| `streaming placeholder uses afterSort anchored to parent` | 占位用 afterSort 锚定 | `afterSort` = parent sortValue + 0.5 |
| `streaming placeholder gets numeric id from stream_start` | stream_start 后 id 变 numeric | `typeof msg.id === 'number'`；afterSort 仍保留（M1：占位 anchor 需持续到最终归位） |
| `single queued message: updates the queued bubble in place` | 单条排队消息原地更新 | id 从 queueId 变为 db_id |
| `multiple queued messages: each reply stays between its own question and the next` | 多条排队消息排序 | 视觉顺序 = 问题→回复→问题→回复 |
| `finalizes unfinished tool_use blocks` | 保留，工具块 finalize | done 标记正确 |
| `does NOT mark PermissionApproval blocks as done` | 保留 | PermissionApproval 不标记 done |
| `clears garbage output from finalized tool_use blocks` | 保留 | garbage 清理 |
| `deduplicates user message by db_id (not content text)` | db_id 去重 | 相同 db_id 不重复 push |
| `no message has undefined id after drain` | 完整性检查 | 所有消息有 id |
| `cancel while queued: removes the queued messages from the array` | 保留，queue_cancel 仍工作 | pending 气泡被移除 |

**`sortMessages` describe 块改写**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `sorts DB-backed messages by numeric id ascending` | 保留 | numeric id 升序 |
| `streaming reply anchored below its question via afterSort` | 改写：afterSort 仍有效 | streaming 占位紧跟问题后 |
| `afterSort overrides numeric id for streaming placeholder` | 新增：afterSort 优先于 numeric id | streaming 有 numeric id 仍用 afterSort |
| `string-id message sorts by seq after numeric ids` | 保留（B1）：大基数偏移保留 | string-id 按 `LARGE_BASE + seq` 恒在 numeric id 后 |
| `_remote string-id never jumps above DB messages` | 新增（B1 护栏）：跨设备 remote-* 无 afterSort | remote 消息恒在 db_id=5000 历史消息后 |
| `never shows a new reply above an older reply` | 保留 | 顺序稳定 |
| `is idempotent` | 保留 | 排序幂等 |

**`isTransientMessage` 改写**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `streaming message is transient` | `streaming === true` | 返回 true |
| `string-id message is transient` | `typeof id !== 'number'` | 返回 true |
| `pending message with numeric id is NOT transient` | 新：pending + numeric id → 非瞬态 | 返回 false |
| `numeric id non-streaming is NOT transient` | 普通消息 | 返回 false |

**`messageSortValue` 改写**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `afterSort takes highest priority` | 有 afterSort | 返回 afterSort 值 |
| `numeric id returns id value` | 无 afterSort，有 numeric id | 返回 id |
| `string id returns LARGE_BASE + seq` | 无 afterSort，string id（B1 修正：保留大基数） | 返回 `LARGE_BASE + seq`，恒在 numeric id 后 |
| `string id without seq returns LARGE_BASE` | 边界 | 返回 `LARGE_BASE` |

**`cancelPendingMessages` describe 块**（B5 扩展）：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| 全部 6 个现有用例 | **保留**——乐观气泡（string id = queueId）按 id 匹配仍工作 | 行为不变 |
| `matches queued DB row by queueId field` | 新增（B5）：排队消息已落库，id 为 numeric，queue_cancel 携带 string queueId | 通过 `m.queueId === queueId` 匹配并删除，pending 气泡不残留 |

#### 2. `useChatSession.test.ts` 改写

**删除的用例**：
- `restores queued messages from backend queue field after switchSession` — `queue` 字段删除
- `restores queued messages from backend queue field in recovery path` — `queue` 字段删除

**改写的用例**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `loadHistory returns queued messages with queueId and queued=true` | DB 返回排队消息带 queueId | 消息列表含 queued=true 的行 |
| `syncSessionState marks DB-returned queued message as pending` | 有 queueId 的 DB 消息补 pending 标记 | `msg.pending === true` |
| `syncSessionState merges optimistic bubble with DB row by queueId` | 乐观气泡 + DB 行合并 | 合并后只有一条消息，id = db_id |
| `syncSessionState does not double-count merged queued message` | 合并后不重复 | messages 长度 = DB 行数 |
| `preserves in-flight streaming message on reload` | 保留：streaming 消息不被 evict | streaming 消息仍在 |
| `cancelled queued message is removed by queue_cancel event` | 改写：由 queue_cancel 事件移除 | pending 气泡消失 |
| `does NOT carry old session queued messages into new session` | 保留：跨会话隔离 | 新会话无旧排队消息 |

**新增的用例**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `pending bubble with same queueId as DB row gets db_id` | 乐观气泡获得 DB id | id 从 queueId 变为 numeric db_id |
| `loadHistory without queue field still works` | 响应无 queue 字段 | 不崩溃（appendQueueItems 已删） |
| `queued=true message without matching optimistic bubble gets pending marker` | 跨设备：DB 有排队消息但本地无气泡 | 补 pending 标记 |

#### 3. `chatQueueSend.test.ts` 改写

**删除的用例**：
- `resubmits with the backend-returned message and files when needsStart is true` — needsStart 删除
- `falls back to original text and computed files when needsStart result lacks them` — needsStart 删除
- `does NOT resubmit when needsStart is false` — needsStart 删除

**改写的用例**：

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `produces a pending- prefixed unique id` | 保留：generateQueueId 仍生成 `pending-*` | 格式正确 |
| `pushes a pending user message with a generated queueId` | 保留：pushMessage 仍有 pending + queueId | `pending: true`, `id: pending-*` |
| `assigns a monotonic seq to the pushed pending message` | 保留 | `msg.seq` 为数字 |
| `dedupes pending and attached files` | 保留 | 文件去重 |
| `calls enqueue with sessionId, text, attachments and queueId` | 保留 | enqueue 被正确调用 |
| `enqueue returns {ok: true} without needsStart` | 改写：EnqueueResult 无 needsStart 字段 | `result.needsStart` 不存在 |
| `no resubmit callback is ever called` | 新增：resubmit 永远不被调用 | resubmit mock 未被调用 |
| `honors a caller-provided queueId` | 保留 | 自定义 queueId |
| `calls onPendingRendered after pushing the message` | 保留 | 回调时机 |

**`EnqueueResult` 接口改写**：

```typescript
export interface EnqueueResult {
  ok: boolean
  // needsStart: boolean  ← 删除
  // message?: string     ← 删除
  // filePaths?: string[] ← 删除
  // files?: FileEntry[]  ← 删除
}
```

#### 4. `useChatStream.test.ts` 改写

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `queue_drain event: drainQueueMessage called with db_id and queueId` | queue_drain 带真实 db_id | `drainQueueMessage(messages, queueId, ..., dbMessageId)` |
| `queue_drain event: adopts db_id on pending message` | pending 气泡获 db_id | 找到的 pending 消息 id = db_id |
| `queue_cancel event: removes pending messages by queueIds` | 保留 | pending 气泡被移除 |
| `stream_start assigns numeric id to streaming placeholder` | 保留 | `sm.id = payload.message_id` |
| `stream_start removes afterSort from streaming placeholder` | 新增 | `afterSort` 被删除（id 已 numeric） |

#### 5. 新增：排序回归测试（`chatStreamUtils.test.ts` 追加）

6 场景视觉顺序验证（与方案"排序影响已验证"对齐）：

| 场景 | 初始状态 | 操作 | 期望视觉顺序 |
|---|---|---|---|
| 1. 首次发送 | 空 | 发送 Q1 | Q1 → A1(streaming) |
| 2. 发送时 AI 空闲 | 空 | 发送 Q1, Q1 drain, A1 done | Q1 → A1 |
| 3. 发送时 AI 忙 | Q1 → A1(streaming) | 排队 Q2(pending) | Q1 → A1(streaming) → Q2(pending) |
| 4. 排队后 drain | Q1 → A1(streaming) → Q2(pending) | A1 done → Q2 drain | Q1 → A1 → Q2 → A2(streaming) |
| 5. 多条排队 | Q1 → A1(streaming) | 排队 Q2(pending) + Q3(pending) | Q1 → A1(streaming) → Q2(pending) → Q3(pending) |
| 6. 多条 drain 逐条 | Q1 → A1(streaming) → Q2(pending) → Q3(pending) | A1 done → Q2 drain → A2 done → Q3 drain | Q1 → A1 → Q2 → A2 → Q3 → A3(streaming) |

每个场景验证：`sortMessages(messages)` 后，视觉顺序 = 问题→回复→下一问题→下一回复。

#### 6. 整体删除的前端测试用例

| 文件 | 用例 | 理由 |
|---|---|---|
| `chatStreamUtils.test.ts` | `_drain marker` 相关全部 | `_drain` 标记删除 |
| `chatStreamUtils.test.ts` | `drain ID` 格式/碰撞/自动生成 | 不再使用 drain id |
| `chatStreamUtils.test.ts` | `string id kept transient` 系列 | pending 消息现在有 DB id |
| `chatStreamUtils.test.ts` | `_remoteQueueId` 匹配 | `_remote` 简化 |
| `chatStreamUtils.test.ts` | content-based fallback 匹配 | 防御性 fallback 删除 |
| `chatStreamUtils.test.ts` | `TRANSIENT_BASE` 常量名 | 改名 `LARGE_BASE`，**行为不变**（B1：大基数偏移保留） |
| `chatStreamUtils.test.ts` | `isTransientMessage.pending` 分支 | pending 消息有 numeric id |
| `useChatSession.test.ts` | `appendQueueItems` / `queue` 字段恢复 | appendQueueItems 删除 |
| `useChatSession.test.ts` | ghost-pending 清理系列 | ghost 机制删除 |
| `chatQueueSend.test.ts` | `needsStart` / `resubmit` 系列 | needsStart 删除 |

---

### 测试执行验证

| 阶段 | 命令 | 通过标准 |
|---|---|---|
| 后端单元测试 | `go test ./internal/service/... ./internal/handler/... ./cmd/server/...` | 全部通过，覆盖率 ≥ 基线 |
| 前端单元测试 | `npm test` | 全部通过 |
| 类型检查 | `vue-tsc --noEmit` | 无错误 |
| 全量回归 | `./scripts/pre-push-checks.sh` | 全部通过 |

---

## 风险清单

| 风险 | 等级 | 缓解 |
|---|---|---|
| loadHistory 与乐观气泡并存 | 高 | `syncSessionState` 按 `queue_id` 补 pending 标记（唯一保留防御） |
| **B2：drain 退出窗口消息卡死** | 高 | drain 退出前 double-check `SELECT count(queued=1)`，非空不自愈退出 |
| **M2：totalCount 与 messages 口径矛盾** | 中 | `GetChatMessageCount` 保持 `COUNT(*)` 含 queued；前端 `hasMore` 剔除排队消息后比较 |
| **M3：崩溃后残留 queued=1 的执行策略** | 中 | 不做全局扫表自愈；残留消息由用户下次发消息触发消费，或 DELETE 取消（竞态 4） |
| **M4：排队消息污染 RAG 索引** | 中 | `AddQueuedMessage` 设 `indexed=1` 跳过索引，drain 后 `FinalizeStreamingMessage` 置 `indexed=0` |
| 入队即启动 goroutine 的并发安全 | 中 | 复用 `TrySetSessionRunning` 原子守卫 |
| **m6：`CancelSession` 三处 `ClearQueue` 都要改 DB + 补发 `queue_cancel`** | 中 | `session_runtime.go:460`（stuck 分支）/`:475`（正常）/`:502`（ForceCancel），每处收集 queueIDs + emit |
| **m7：`GET /api/ai/chat` 删 `queue` 字段的外部依赖** | 中 | 实施前 grep Android/Electron/其他客户端对 `queue` 字段的引用 |
| 取消消息残留为无回复 user 消息 | 低 | 设计上接受（行为改进） |
| 测试改写量大（队列相关全删全改） | 中 | 分批：先迁移 → 再 queue/drain → 再 handler → 再前端 |

---

## 审查补充方案（6 个阻断性问题 + 4 个设计点）

### 阻断 1：Drain loop 唤醒机制

**问题**：删除 `sessionDrainChans` 后，drain loop 在 `SELECT queued=1` 返回空后如何被新入队消息唤醒？当前 `WaitForEnqueue` 依赖 channel 信号实现 100ms 内即时响应。

**方案**：保留 `sessionDrainChans` sync.Map（仅做信号，不存数据），从 `queue.go` 移至 `drain.go`。入队 handler 写 DB 后 `select { case ch <- struct{}{}: default: }` 发信号，drain loop 等待信号再查询 DB。`queue.go` 其余代码（sessionQueues、EnqueueMessage 数据逻辑等）全部删除。

```go
// drain.go 保留
var sessionDrainChans sync.Map // map[string]chan struct{}

func SignalDrain(sessionID string) {
    ch, _ := sessionDrainChans.LoadOrStore(sessionID, make(chan struct{}, 1))
    select {
    case ch.(chan struct{}) <- struct{}{}:
    default:
    }
}

func WaitForEnqueue(sessionID string, timeout time.Duration) bool {
    ch, _ := sessionDrainChans.LoadOrStore(sessionID, make(chan struct{}, 1))
    select {
    case <-ch.(chan struct{}):
        return true
    case <-time.After(timeout):
        return false
    }
}
```

入队 handler（`handleQueueEnqueue`、`chat.go` 入队分支、`sendMessageToSessionFromPush`）在 `AddChatMessage` 后调用 `SignalDrain(sessionID)`。

**优点**：与当前行为完全等价，drain loop 延迟 ≤100ms；信号 channel 仅 1 buffer，零内存负担。

---

### 阻断 2：Streaming 占位符排序空窗

**问题**：`drainQueueMessage` 创建的 streaming 占位消息在 `stream_start` 赋予 numeric id 之前仍是 string id（`generateDrainId()` 生成），纯 numeric id 排序无法覆盖此窗口期。当前代码用 `afterSort = parentSortValue + 0.5` 锚定在问题之后。

**方案**：保留 `afterSort` + `computeAfterSort`，但收窄使用场景——仅用于 streaming 占位符（`drainQueueMessage` 步骤 3 和首次发送的 `handleSendNew`），其余全部删除：

| 机制 | 决策 |
|---|---|
| `afterSort` | **保留**，仅 streaming 占位符使用（`drainQueueMessage` 和 `handleSendNew`） |
| `computeAfterSort` | **保留** |
| `TRANSIENT_BASE` | **改名 `LARGE_BASE` 保留**（B1 修正）：string-id 消息（跨设备 `_remote` 的 `remote-*`、stream_start 前的占位）无 afterSort，删大基数会按裸 `seq` 排到 DB 消息之前 |
| `isTransientMessage` | 简化为 `m.streaming === true \|\| typeof m.id !== 'number'`（删 `pending` 分支，因 pending 消息现在有 DB id） |
| `messageSortValue` | 简化：`afterSort > numeric id > LARGE_BASE + seq`（大基数保留，仅改名） |
| `seq` | **保留**，用于 string-id 消息（`_remote`、stream_start 前占位）在 transient 域内的相对排序 |
| `nextClientSeq` | **保留** |
| `generateDrainId` | **保留**，仍为 streaming 占位符生成临时 string id |

**排序优先级改后**：
1. 有 `afterSort` → 用 `afterSort`（streaming 占位符锚定在问题后）
2. 有 numeric id → 用 `id`（DB-backed 消息，包括已落库的排队消息）
3. string id → 用 `LARGE_BASE + seq`（`_remote`、stream_start 前占位，恒排 DB 消息后）

**验证**：`afterSort` 的必要性在于——streaming 占位符创建时 string id，问题已是 numeric id，若无 `afterSort`，占位符按 `LARGE_BASE+seq` 排序会落到所有 numeric id 之后（`LARGE_BASE` 远大于 DB id）。`afterSort` 确保占位符紧跟其父问题，直到 `stream_start` 赋予 numeric id 后自然归位。

---

### 阻断 3：SessionMessenger 接口迁移

**问题**：`internal/push/common/interfaces.go:37-38` 的 `EnqueueMessage(sessionID, message string) error` 和 `ClearQueue(sessionID string)` 被 DingTalk/Feishu 推送后端使用，`cmd/server/main.go` 有两个实现（`dingtalkSessionMessenger`、`feishuSessionMessenger`），`session_command.go:161` 也直接调用 `EnqueueMessage`。

**方案**：

1. **`SessionMessenger` 接口改签名**：

```go
// push/common/interfaces.go
type SessionMessenger interface {
    FindSessionsByPrefix(prefix string, runningOnly bool) ([]SessionInfo, error)
    ListRecentSessions(limit int) ([]SessionInfo, error)
    IsSessionRunning(sessionID string) bool
    // EnqueueMessage persists the message to DB (queued=1) and signals drain loop.
    // If the session is not running, starts a goroutine to process it.
    EnqueueMessage(sessionID, message string) error
    // ClearQueuedMessages marks all queued messages as consumed (queued=0).
    ClearQueuedMessages(sessionID string)
    SendMessageToSession(sessionID, message string) error
}
```

2. **`cmd/server/main.go` 实现改写**：

```go
func (dingtalkSessionMessenger) EnqueueMessage(sessionID, message string) error {
    info := service.GetSessionFullInfo(sessionID)
    if info == nil {
        return fmt.Errorf("session %s not found", sessionID)
    }
    // 持久化到 DB（queued=1）
    _, err := service.AddQueuedMessage(info.ProjectPath, info.Backend, sessionID, message)
    if err != nil {
        return err
    }
    // 信号 drain loop 或启动 goroutine
    if service.TrySetSessionRunning(sessionID) {
        go service.StartSessionRun(sessionID, info.ProjectPath, info.Backend)
    } else {
        service.SignalDrain(sessionID)
    }
    return nil
}

func (dingtalkSessionMessenger) ClearQueuedMessages(sessionID string) {
    service.ClearQueuedMessages(sessionID)
}
```

3. **`session_command.go:161` 改写**：`sendMessageToSessionFromPush` 的入队分支同步改为 DB 落库 + `SignalDrain`/启动 goroutine。

4. **DingTalk/Feishu 推送后端调用点**更新为 `ClearQueuedMessages`。

**新增 DB 函数**：
- `AddQueuedMessage(projectPath, backend, sessionID, message string, files []model.FileEntry, queueID string) (int64, error)` — 写 `chat_history` + `queued=1` + `queue_id`；**复用 `AddChatMessage` 的标题生成 + `updated_at` 更新逻辑（B3）**；**设 `indexed=1` 跳过 RAG 索引（M4），drain 后由 `FinalizeStreamingMessage` 置 `indexed=0`**
- `ClearQueuedMessages(sessionID string)` — `UPDATE chat_history SET queued=0 WHERE session_id=? AND queued=1`

---

### 阻断 4：GetChatHistoryPaged 返回新列

**问题**：`scanMessages` 只读 `id, role, content, files, backend, streaming, created_at, indexed`，不读 `queue_id`/`queued`。前端 `ChatMessage` 模型（Go: `model.ChatMessage`，TS: `ChatMessage`）均无这两个字段。

**方案**：

1. **Go `model.ChatMessage` 加字段**（`internal/model/chat.go`）：

```go
type ChatMessage struct {
    // ... 现有字段 ...
    QueueID string `json:"queueId,omitempty"`
    Queued  bool   `json:"queued,omitempty"`
}
```

2. **`scanMessages` 改写**（`internal/service/chat.go`）：

```go
var queueID string
var queued int
if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed, &queueID, &queued); err != nil {
    return nil, err
}
msg.QueueID = queueID
msg.Queued = queued != 0
```

3. **所有 SELECT 查询加列**——`GetChatHistoryPaged` 的两个 SELECT 子句追加 `, queue_id, queued`。

4. **前端 `ChatMessage` 加字段**（`chatStreamUtils.ts`）：

```typescript
export interface ChatMessage {
  // ... 现有字段 ...
  /** DB-assigned queue ID for matching optimistic pending bubbles to DB rows */
  queueId?: string
  /** True if this message is still queued (waiting for drain) */
  queued?: boolean
}
```

5. **`parseMessages` / `loadHistory` 无需额外处理**——`queueId` 和 `queued` 随 JSON 响应自动带入。

---

### 阻断 5：/api/ai/chat 端点去留（修订版——彻底合并）

**问题**：方案说"一个发送端点统一"但未说明 `POST /api/ai/chat` 的命运。它仍被前端 `useSessionIdentity.ts:656-691` 的 fallback 路径直接调用（ChatPanel 未挂载时，`useQuoteQuestion.ts:259` 引用提问条触发）。

**决策（2026-08-24 用户拍板）**：**彻底合并**——`POST /api/ai/chat` 的发送能力删除，统一走 `POST /api/ai/queue`。`/api/ai/chat` 仅保留 GET（loadHistory）。

```
POST /api/ai/queue → handleUnifiedEnqueue（唯一发送入口）
POST /api/ai/chat → 删除（发送能力移除，GET 保留）
```

**前端 fallback 路径改写**（`useSessionIdentity.ts:656-691`）：`identity.sendMessage` 的 fallback 从 `POST /api/ai/chat` 改为 `POST /api/ai/queue`，并生成 queueId：

```typescript
// useSessionIdentity.ts fallback 改写
const queueId = `pending-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
await fetch(`/api/ai/queue?session_id=${encodeURIComponent(sid)}`, {
    method: 'POST',
    body: JSON.stringify({
        message: text, queueId, filePaths: [],
        modelId: currentModelId.value || undefined,
        thinkingEffort: currentThinkingEffort.value || undefined,
        transport: currentTransport.value || undefined,
        clientId: localStorage.getItem('clawbench_client_id') || undefined,
    }),
})
```

**影响评估（破坏 POST /api/ai/chat 的代价）**：fallback 触发面窄（仅 ChatPanel 未挂载 + 引用提问），失败是"发送报错"而非数据损坏，且正常聊天（ChatPanel 挂载走 `_sendMessage`）完全不受影响。合并后后端只维护一个发送路径，无重复逻辑。

**后端 handler 变更**：`internal/handler/chat.go` 的 `POST` 分支删除（或改为 405），`ServeChatHistory` 仅剩 GET。需检查 `main.go` 路由注册中 `POST /api/ai/chat` 的处理。

---

### 阻断 6：DB 层原子消费（修正版——B4）

**问题**：drain loop 的 `SELECT queued=1 ORDER BY id` → `UPDATE queued=0 WHERE id=?` 两步非原子。并发入队可能在间隙插入新行（虽然 FIFO 顺序不会乱，但同一行可能被两个 drain loop 消费——如果会话重启后新 goroutine 与旧 goroutine 短暂并存）。

**方案**：SQLite 事务原子消费 + `writeMu` 互斥锁双重保障。**注意：`internal/service/` 没有 `WriteQueryRow`（B4 已验证，只有 `WriteExec`/`WriteBegin`，database.go:51/74），必须用事务实现**：

```go
// DequeueQueuedMessage atomically claims the next queued message for a session.
// Returns the message and true if one was found, false if queue empty.
// Must distinguish sql.ErrNoRows (truly empty) from real DB errors — a DB
// error must NOT be treated as "queue empty" or the drain loop exits and
// silently loses the message (B4).
func DequeueQueuedMessage(sessionID string) (model.ChatMessage, bool, error) {
    tx, err := WriteBegin()
    if err != nil {
        return model.ChatMessage{}, false, err
    }
    defer tx.Rollback()

    // Pick the oldest queued message under the write lock.
    var id int64
    var msg model.ChatMessage
    var filesJSON sql.NullString
    var queueID string
    var queued int
    err = tx.QueryRow(`
        SELECT id, role, content, files, backend, created_at, queue_id, queued
        FROM chat_history WHERE session_id = ? AND queued = 1
        ORDER BY id ASC LIMIT 1
    `, sessionID).Scan(&id, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &msg.CreatedAt, &queueID, &queued)
    if err == sql.ErrNoRows {
        return model.ChatMessage{}, false, nil // genuinely empty
    }
    if err != nil {
        return model.ChatMessage{}, false, err // real DB error — retry, don't exit
    }

    // Claim it: conditional UPDATE under the same tx.
    res, err := tx.Exec(`UPDATE chat_history SET queued = 0 WHERE id = ? AND queued = 1`, id)
    if err != nil {
        return model.ChatMessage{}, false, err
    }
    n, _ := res.RowsAffected()
    if n == 0 {
        // Already claimed by another drain loop — try the next one.
        return DequeueQueuedMessage(sessionID) // or return false + retry loop
    }
    if err := tx.Commit(); err != nil {
        return model.ChatMessage{}, false, err
    }

    msg.ID = id
    msg.SessionID = sessionID
    msg.QueueID = queueID
    msg.Queued = queued != 0
    if filesJSON.Valid && filesJSON.String != "" {
        msg.Files = unmarshalFilesJSON(filesJSON.String)
    }
    return msg, true, nil
}
```

**关键**：`UPDATE ... RETURNING` 是 SQLite 3.35+（现代版本全部支持）的原子操作——SELECT 和 UPDATE 在同一条语句内，不存在 TOCTOU 窗口。加上 `writeMu` 互斥，即使会话重启导致短暂的两个 goroutine 并存，也只有一个能成功 claim 某一行。

**`GetQueuedCount` 辅助函数**（取消路径需要知道有多少排队消息）：

```go
func GetQueuedCount(sessionID string) int {
    var count int
    dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND queued = 1", sessionID).Scan(&count)
    return count
}
```

**取消路径的 `queue_cancel` 收集**：

```go
func GetQueuedQueueIDs(sessionID string) []string {
    rows, err := dbRead.Query("SELECT queue_id FROM chat_history WHERE session_id = ? AND queued = 1 AND queue_id != ''", sessionID)
    // ... scan into []string
}
```

---

### 设计点 1：handleQueueGet / handleQueueDelete DB 替代

**`handleQueueGet`**：改为 DB 查询。

```go
func handleQueueGet(w http.ResponseWriter, r *http.Request) {
    // ... auth checks ...
    queue, err := service.GetQueuedMessages(sessionID)  // SELECT ... WHERE queued=1 ORDER BY id
    if queue == nil { queue = []model.QueuedMessage{} }
    writeJSON(w, http.StatusOK, map[string]any{"queue": queue})
}
```

**`handleQueueDelete`**：按 queueId 取消单条 / 清空全部。

```go
func handleQueueDelete(w http.ResponseWriter, r *http.Request) {
    // ... auth checks ...
    queueID := r.URL.Query().Get("queueId")
    if queueID != "" {
        // 取消单条：UPDATE queued=0 WHERE queue_id=?
        service.CancelQueuedMessage(sessionID, queueID)
        // 返回剩余队列
        queue, _ := service.GetQueuedMessages(sessionID)
        if queue == nil { queue = []model.QueuedMessage{} }
        writeJSON(w, http.StatusOK, map[string]any{"ok": true, "queue": queue})
        return
    }
    indexStr := r.URL.Query().Get("index")
    if indexStr == "" {
        // 清空全部
        service.ClearQueuedMessages(sessionID)  // UPDATE queued=0 WHERE session_id=?
        writeJSON(w, http.StatusOK, map[string]any{"ok": true})
        return
    }
    // 按 index 删除（legacy）：先查再按 id 取消
    // ... 查询第 N 条 queued=1 的 id，UPDATE queued=0 WHERE id=?
}
```

---

### 设计点 2：queue_cancel 确认保留

**明确决策**：`queue_cancel` 事件 **保留**，`cancelPendingMessages` **保留**。

取消会话时后端 `ClearQueuedMessages`（`UPDATE queued=0`）后仍需发 `queue_cancel` 事件，前端收到后移除乐观 pending 气泡。理由：
- pending 气泡由前端乐观创建（id=queueId），不在 DB 中
- 取消后 pending 气泡应立即消失，不能等 loadHistory
- `useChatStream.ts:724-732` 的 `queue_cancel` handler 和 `cancelPendingMessages` 函数保留，逻辑不变

**`CancelSession` 的 queue_cancel 发射**：当前 `queue_cancel` 仅在 `drain.go:RunDrainLoop` 的 cancel 分支内发射。但 `CancelSession` 可能通过 `cancel()` 直接杀掉 goroutine，此时 `RunDrainLoop` 的 cancel 分支可能未执行。需确保 `CancelSession` 自身也发射 `queue_cancel`：

```go
func CancelSession(sessionID string) bool {
    // ... 现有逻辑 ...
    // 在 ClearQueue 改为 ClearQueuedMessages 后，收集 queue IDs
    queueIDs := GetQueuedQueueIDs(sessionID)
    ClearQueuedMessages(sessionID)
    cancel()
    // ... 现有 emit 逻辑 ...
    // 补充：确保 queue_cancel 总是发射
    if len(queueIDs) > 0 {
        ws.EmitToSession(sessionID, ai.StreamEvent{
            Type: "queue_cancel",
            QueueEvent: &ai.QueueEventData{
                SessionID: sessionID,
                QueueIDs:  queueIDs,
            },
        })
    }
    // ... SetSessionRunning ...
}
```

`ForceCancelSession` 不需要发 `queue_cancel`（客户端已断开，看不到），但 `ClearQueuedMessages` 仍需调用。

---

### 设计点 3：GetChatMessageCount / 分页对策（修正版——两个 count，方案 C）

**问题（M2）**：单 count 方案有矛盾——
- 方案 A（`GetChatMessageCount` 加 `AND queued=0`）：total 排除排队，但 `GetChatHistoryPaged` 的 messages 数组**仍含**排队消息 → 历史 50、limit=40、排队 15 → total=50、messages=55 → `hasMore=false`，实际还有 10 条更老历史没加载。**误判**。
- 方案 B（保持 `COUNT(*)`）：total=65、messages=55 → `hasMore=true`，但排队消息被计入 messages.length，多加载一次返回空。轻微不精确。

**方案 C（采用）**：`GetChatHistoryPaged` 返回**两个 count**：

```go
// GetChatHistoryPaged 返回值扩展为 (messages, total, queuedCount, err)
// total      = COUNT(*) 含排队（与 messages 数组口径一致）
// queuedCount = COUNT(*) WHERE queued=1（前端 hasMore 剔除用）
func GetChatMessageCount(sessionID string) int {
    var count int
    dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sessionID).Scan(&count)
    return count
}
func GetQueuedCount(sessionID string) int {
    var count int
    dbRead.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND queued = 1", sessionID).Scan(&count)
    return count
}
```

**前端 `hasMore`**（`useChatSession.ts:467`）改为：

```typescript
// 剔除排队消息后再比较——排队消息是 pending 气泡，不算已加载的历史
const hasMore = computed(() => {
  const loaded = messages.value.filter(m => !m.queueId).length  // 或 !m.pending
  return loaded < totalMessages.value - queuedCount.value
})
```

**`GetUserMessageIndex`** 加 `AND queued = 0`（导航索引不应包含排队消息）。`GetFinalizedMessageCount` 不受影响（已有 `streaming = 0` 过滤）。

---

### 设计点 4：测试影响范围补充

**遗漏的测试文件**：

| 文件 | 需改动调用 |
|---|---|
| `internal/service/session_command_test.go` | `ClearQueue` → `ClearQueuedMessages`（约 3 处） |
| `internal/handler/queue_test.go` | `EnqueueMessage`/`DequeueMessage`/`GetQueue`/`RemoveQueueItem*`/`ClearQueue` 全部改写为 DB 操作 |
| `internal/handler/chat_test.go` | 入队分支测试全部改写（`EnqueueMessage` → `AddQueuedMessage`） |
| `internal/service/queue_test.go` | **整个删除**（内存队列测试） |
| `internal/service/drain_test.go` | `WaitForEnqueue`/`DequeueMessage`/`GetQueue`/`PersistUser` 改写 |
| `cmd/server/main_test.go` | `SessionMessenger` mock 签名更新（如有） |

**新增测试用例**：
- `DequeueQueuedMessage` 原子性（并发竞争）
- `GetChatMessageCount` 排除 queued=1 行
- `AddQueuedMessage` + `SignalDrain` + `TrySetSessionRunning` 联动
- `CancelQueuedMessage` 按 queueId 精确取消
- 会话重启后 queued=1 行被新 drain loop 消费
- `scanMessages` 返回 `queueId`/`queued` 字段

---

## 实施顺序（更新）

1. **Task 1**：后端——`database.go` 加 `queue_id`/`queued` 列迁移 + `model.ChatMessage` 加字段 + `scanMessages`/SELECT 加列 + 迁移测试
2. **Task 2**：后端——新增 `AddQueuedMessage`（复用 title/updated_at，B3；设 `indexed=1` 跳过 RAG，M4）/`DequeueQueuedMessage`（事务原子 + 区分 DB 错误，B4）/`ClearQueuedMessages`/`GetQueuedQueueIDs`/`GetQueuedMessages`/`GetQueuedCount` DB 函数 + 原子消费测试；`GetChatHistoryPaged` 返回双 count（方案 C）
3. **Task 3**：后端——抽 `startSessionRun` 共享 goroutine 启动 + 保留 `sessionDrainChans`/`SignalDrain`/`WaitForEnqueue` 移至 `drain.go` + `drain.go` 改用 DB 消费 + 删 `PersistUser` + **drain 退出前 double-check 自愈（B2）** + 测试
4. **Task 4**：后端——`queue.go` 整个删除；`handleQueueEnqueue`/`handleQueueGet`/`handleQueueDelete` 改用 DB；`handleUnifiedEnqueue` 抽取 + 删 `needs_start` + 测试
5. **Task 5**：后端——`chat.go` 入队分支改 `AddQueuedMessage` + `SignalDrain`；**`POST /api/ai/chat` 发送分支删除（彻底合并，仅留 GET）**；GET 响应删 `queue` 字段（先 grep Android/Electron 依赖，m7）+ 测试
6. **Task 6**：后端——`SessionMessenger` 接口改签名 + `main.go` 实现改写 + `session_command.go` 改写 + `CancelSession`（三处 ClearQueue，m6）/`ForceCancelSession` 改 `ClearQueuedMessages` + 补 `queue_cancel` 发射 + 测试
7. **Task 7**：前端——`ChatMessage` 加 `queueId`/`queued`；`chatStreamUtils` 的 `TRANSIENT_BASE` **改名 `LARGE_BASE` 保留（B1）**、`isTransientMessage` 删 `pending` 分支、保留 `afterSort`/`seq`/`generateDrainId`；`drainQueueMessage` 采用 db_id + 保留 queueId 匹配 + **`cancelPendingMessages` 扩展 queueId 匹配（B5）** + 测试
8. **Task 8**：前端——`useChatSession` 删 `appendQueueItems`/ghost + 补 `queue_id` 匹配 pending + **`hasMore` 按方案 C 剔除排队消息** + 测试
9. **Task 9**：前端——`chatQueueSend` 删 `needsStart`/`resubmit`；`useChatStream` 保留 `queue_cancel` handler；**`useSessionIdentity` fallback 从 `POST /api/ai/chat` 改走 `/api/ai/queue` + 生成 queueId** + 测试
10. **Task 10**：全量回归——`go test ./...` + `npm test` + `vue-tsc` + 排序场景 + 手动验证
