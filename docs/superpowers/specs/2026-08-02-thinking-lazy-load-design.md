# 深度思考（thinking）独立存储与懒加载设计

## 背景

助手消息的深度思考（thinking）文本目前**内嵌在** `chat_history.content` 的 blocks JSON 里（`model/chat.go:132` 的 `ContentBlock{Type:"thinking", Text}`）。打开会话时 `/api/ai/chat` 把整条 content 原样返回（`handler/chat.go:254`），thinking 文本越大、消息越多，加载越慢。而前端 thinking 块默认折叠成 chip（`ContentBlocks.vue` `isThinkingCollapsed`），文本其实早已传过来了——属于无效开销。

工具调用已有成熟先例：input/output 拆到 `chat_tool_calls` 表，content 里留 slim 块，前端点击时才经 `GET /api/ai/chat/tool-call` 懒加载。本设计把 thinking 照此模式改造。

## 设计决策（已确认）

| 决策 | 选择 |
| --- | --- |
| 存量数据 | **全量迁移**：启动时仿 `MigrateToolCallsFromContent` 分批抽取改写 |
| Fork/续会话 | **顺带复制 `chat_tool_calls` + `chat_thinking`**，一并修掉 fork 工具详情 404 的存量 bug |

## 核心映射机制

slim 块留在 content JSON 的 `blocks` 数组**原位**（数组顺序即渲染顺序），`think_id` 指向 `chat_thinking` 表中的文本。与 tool_use 同构：

```json
{"blocks":[
  {"type":"text","text":"开始回答..."},
  {"type":"thinking","think_id":"th_a1b2","done":true},
  {"type":"tool_use","id":"toolu_x","name":"Bash","done":true},
  {"type":"text","text":"结论"}
]}
```

**WS 流式路径完全不改**（仍全量，live 块有 `text`），只改 DB 写入与历史 API 返回。

## 后端

### 1. Schema（`database.go` createTables，仿 `chat_tool_calls`）

```sql
CREATE TABLE IF NOT EXISTS chat_thinking (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    think_id TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(think_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_thinking_message ON chat_thinking(message_id);
CREATE INDEX IF NOT EXISTS idx_thinking_session ON chat_thinking(session_id, created_at DESC);
```

### 2. 写入路径（`SessionExecutor.Finalize`，`session_executor.go:496`）

- 照旧 `buildContentJSON` 产出**全量** content，`result.Blocks` 保持不变（供 WS 终端事件）
- 新函数 `slimThinkingInContent(content string) (string, []ThinkingRecord, error)`：字符串级变换——解析 content JSON，给每个 thinking 块生成 `think_id`、去掉 `text`，重新 marshal（保留 `metadata` 等未知字段）；收集 `(think_id, text)` 记录
- 若有记录：`DELETE FROM chat_thinking WHERE message_id = ?`（幂等）→ `INSERT` 记录 → `FinalizeStreamingMessage` 用 **slim** content
- 边界：无活跃 streaming 行时（极端情况）直接 finalize 全量 content，降级为现状
- `think_id` 格式 `th_` + 随机（仿 `toolu_`）

### 3. 迁移（`MigrateThinkingFromContent`，仿 `database.go:996`）

- 检测：`content LIKE '%"type":"thinking"%' AND content NOT LIKE '%think_id%' AND streaming = 0 AND NOT EXISTS (chat_thinking 行)`
- 分批（200）+ offset 分页，逐行调 `slimThinkingInContent` 复用同一逻辑：写表 + 改写 content
- `InitDB` 中在 `MigrateToolCallsFromContent` 之后调用

### 4. Service + API

- 新文件 `internal/service/thinking.go`：`ThinkingRecord`、`UpsertThinking`、`GetThinking(thinkID, messageID)`、`GetThinkingBySession(thinkID, sessionID)`——全部仿 `tool_calls.go`
- 新端点 `GET /api/ai/chat/thinking?think_id=&message_id=&session_id=`，仿 `ServeToolCallDetail`（`chat_history.go:216`）：GET 校验、`requireProject`、参数校验、按 message_id 主查 → session_id 兜底、归档 + 项目归属检查、`writeJSON` 返回记录；路由注册于 `handler.go` 的 `register()` 段（仿 `register("/api/ai/chat/tool-call", ...)`）

### 5. Fork / 续会话复制（修存量 bug）

- 新函数 `copySessionDetailTables(idMap map[int64]int64, sourceSessionID, newSessionID string) error`：把源会话的 `chat_tool_calls` 和 `chat_thinking` 复制到新会话，`message_id` 用 `idMap` 重映射（`UNIQUE(tool_id, message_id)` / `UNIQUE(think_id, message_id)` 在新 message_id 下仍唯一）
- **按 `idMap` 迭代复制**（仿 `copySessionSummaries`）：只复制源 `message_id` 在 `idMap` 中的行；fork 带 `beforeMessageID` 截断时，截断点之后的消息不在 `idMap` 里，其工具/思考行直接跳过——避免孤儿行或无法映射的行
- 调用点（两条独立复制路径都补）：
  - `ForkSession` → `copySessionMessages` 之后（`continue_conversation.go:345`）
  - `ContinueFromExecution` → 复制 summaries 之后（`continue_conversation.go:236`）

### 6. 删除 / 清理

- `PurgeArchivedData`（`chat.go:1446`）+ `HardDeleteSession`（`chat.go:1508`）各加 `DELETE FROM chat_thinking WHERE session_id = ...`
- `session_cleanup.go` purge 路径核对并补上（如走 `HardDeleteSession` 则已覆盖）

## 前端

### 7. 解析与稳定 key

- `chatBlocks.ts` `parseAssistantContent`：兼容 slim thinking 块（有 `think_id` 无 `text`），保留 `_key` 兜底赋值
- `ContentBlocks.vue` `stableBlockKey`（`:370`）：thinking 优先用 `think_id`（仿 tool_use 用 `id`），旧块回退 `_key`——`v-for :key`、展开动画状态、懒加载缓存共用同一 key

### 8. 懒加载 composable

- 新文件 `web/src/composables/useThinkingContent.ts`：
  - `thinkingTextCache: Map<think_id, text>` 模块级共享缓存（跨 ContentBlocks 实例复用，避免滚动重挂载时重复请求），随会话切换调 `clearThinkingCache()` 清空
  - 并发去重（in-flight Map）
  - `loadThinking(block, msgId)`：调 `GET /api/ai/chat/thinking?think_id=&message_id=&session_id=`，返回 `{text, loading, error}`
  - 缓存清空挂在会话切换处（与 `staticBlockCache.clear()` 同处）
- `ContentBlocks.vue` `getThinkingHtml`（`:550`）：`block.text` 存在 → 照旧渲染；否则有 `think_id` → 渲染 loading 占位（复用 `.placeholder-dots`）或错误重试态。**懒加载请求从展开事件触发**（`handleThinkingClick` 或对 expanded 状态的 `watch`），不在 `getThinkingHtml` 渲染 getter 内发请求（避免渲染期副作用）
- 流式不受影响（live 块始终有 `text`）

## 已知取舍（非本次范围）

- `rag message <id>` CLI 命令（`internal/cli/rag.go`）slim 后显示不到 thinking，可后续在 `GetMessageByID` 回填——低优先级
- RAG 索引 / 摘要不受影响（`summarize/task.go` 与 RAG 本就排除 thinking 块）

## 测试

**Go**（`*_test.go` 放对应包旁）：
- `thinking.go`：Upsert/Get/GetBySession 存取与兜底
- `slimThinkingInContent`：有/无 thinking 块、保留 metadata、think_id 生成
- `SessionExecutor.Finalize`：thinking 拆分 → `chat_thinking` 行 + DB slim content + WS 全量 blocks
- `MigrateThinkingFromContent`：老格式 → slim + 行；幂等（跳过已 slim 消息）
- `ForkSession` / `ContinueFromExecution`：`chat_tool_calls` + `chat_thinking` 均被复制；**fork 带 `beforeMessageID` 截断时**，截断点后的工具/思考行被跳过、截断点内的正确重映射
- `ServeThinkingDetail`：found / 404 / session 兜底 / 项目不匹配 / 方法校验
- Purge：`chat_thinking` 被清理

**前端**（Vitest `.test.ts` 放 composable 旁）：
- `chatBlocks`：slim thinking 解析、think_id 稳定 key
- `useThinkingContent`：fetch / 缓存命中 / 并发去重 / 错误重试
- `ContentBlocks`：展开触发懒加载、渲染缓存文本、loading/error 态
