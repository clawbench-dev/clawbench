# 会话自动命名（AI 重命名）设计

日期：2026-09-26
状态：已确认，实施中

## 背景与目标

现有会话命名有两条独立链路：

1. **本地标题**（`internal/service/chat.go` 的 `maybeAutoTitleSessionTx`）：用户消息落库时，若会话仍是占位标题（`title_source='placeholder'`），直接用该消息的纯文本（截断 50 字）作为标题，并写入 `title_source='auto'`。此后不再改写。
2. **手动生成标题**（`internal/handler/session_title.go` 的 `ServeGenerateSessionTitle` → `summarize.GenerateSessionTitle`）：用户点「生成标题」按钮，把会话全部用户消息交给共享的 `ai_summary` 模型摘要成候选标题，**不落库**，由用户在改名对话框里确认后经正常改名端点保存。

目标：新增一个**全局开关**，开启后在上述「本地标题」的**同一时机**，自动用 AI 摘要覆盖标题。用户要求：

- 触发时机与现有本地命名**完全一致**（首条用户消息 / Fork 后首条消息 / 继续会话后首条消息）。
- Fork 分支：必须把**原有用户消息 + 最新用户消息**都给 AI 总结。
- 定时任务继续会话：把**任务中的用户消息 + 继续后的第一条消息**给 AI 总结。
- AI 调用失败或未配置摘要模型时，**回退到现有本地标题**（界面立刻有名字，零回归）。
- **必须复用现有手动生成标题的代码**，不得另写一套 AI 调用。
- **手动重命名入口原样保留**，用户任意时刻都能改名。

## 关键设计决策

### 决策 1：复用共享核心，不写第二套 AI 调用

在 `internal/service` 新增共享函数（`session_auto_title.go`）：

```go
// CollectSessionUserMessages 读取会话全部用户消息（纯文本），并在末尾追加
// extraText（若与最后一条不同）。extraText 用于排队首条消息——它还在
// queued_messages 里、不在 chat_history 中。
func CollectSessionUserMessages(sessionID, extraText string) []string

// GenerateSessionTitleFromMessages 是手动与自动两条路径唯一的 AI 调用点。
func GenerateSessionTitleFromMessages(ctx context.Context, sessionID, extraText string) (string, error)
```

`handler/session_title.go`（手动端点）改为调用 `GenerateSessionTitleFromMessages`，删除其中重复的「收集用户消息 + 构造 summarizer + 调用 + 语言解析」逻辑。自动路径调用同一函数。这样 prompt、模型选择、截断策略只有一份。

### 决策 2：同步本地标题 + 异步 AI 覆盖（失败回退）

`maybeAutoTitleSessionTx` 仍同步写入本地标题（保证界面立刻有名字），但新增返回 `titled bool` 表示「本次是否真的写了标题」。调用方（`AddChatMessage` / `AddQueuedMessage`）在事务提交后：

```
if titled && model.ChatAutoRenameEnabled && ai_summary 已配置 {
    go autoRenameSession(sessionID, triggerText)
}
```

`autoRenameSession` 仿 `triggerChatRecommendation`：`recover()` 包裹、`context.WithoutCancel`（避免会话 cancel 掐断）、60s 超时。AI 成功则覆盖标题；失败/未配置直接返回，本地标题保持不变。

### 决策 3：覆盖守卫（防止覆盖用户中途改名）

AI 调用耗时数秒，期间用户可能手动改名（写 `title_source='custom'`）。覆盖必须是**原子 SQL 守卫**：

```sql
UPDATE chat_sessions SET title = ?, title_source = 'auto'
WHERE id = ? AND COALESCE(title_source, '') <> 'custom'
```

若 `RowsAffected == 0`，说明标题已被用户锁定 → 不覆盖、不广播。

### 决策 4：fork / 继续 / 任务继续天然满足「新旧消息都给 AI」

这三条路径都会把源会话历史**复制进新会话**（`ForkSession` / `ContinueFromExecution` 的 `copySessionMessages`），新会话的 `title_source` 被置为 `placeholder`。因此当用户在 fork/继续会话里发出第一条消息时：

- `maybeAutoTitleSessionTx` 触发（占位可改写）；
- `CollectSessionUserMessages` 读该会话 DB 里的**全部**用户消息 = 复制来的历史 + 刚发的新消息；
- 对定时任务继续会话同理：任务 prompt 作为用户消息已复制进来，加上继续后的第一条。

无需为这三种场景写特判。仅**排队首条消息**例外（消息尚未落库），故用 `extraText` 补上触发文本。

### 决策 5：截断保证最新消息一定进入

`summarize.joinUserMessages` 目前超 `maxTitlePayloadRunes`（8000）时直接丢弃尾部，会截掉 fork 后表达新分支意图的那条。改为：超限时保留**开头若干条 + 必定保留最后一条**，中间以省略标记衔接。

## 变更清单

### 后端

| 文件 | 改动 |
|---|---|
| `internal/model/config.go` | `Chat` 增 `AutoRenameEnabled bool`；全局变量区增 `ChatAutoRenameEnabled bool` |
| `internal/model/defaults.go` | presence-map 默认 false（同 `auto_continue_enabled`） |
| `internal/handler/settings.go` | `configChat` 字段、`configResponse` 赋值、`applyConfigPatch` 分支、`PatchableConfigPaths`、`hotReloadFields`、`applyHotReloadGlobals` |
| `cmd/server/main.go` | `model.ChatAutoRenameEnabled = cfg.Chat.AutoRenameEnabled` |
| `internal/summarize/title.go` | `joinUserMessages` 保留末条 |
| `internal/service/session_auto_title.go`（新） | `CollectSessionUserMessages`、`GenerateSessionTitleFromMessages`、`autoRenameSession`、覆盖守卫 |
| `internal/service/chat.go` | `maybeAutoTitleSessionTx` 返回 `titled bool`；`insertChatMessageTx` 透传；`AddChatMessage` 触发异步重命名 |
| `internal/service/queue_store.go` | `AddQueuedMessage` 用 `extraText=content` 触发异步重命名 |
| `internal/handler/session_title.go` | 改调共享核心 |
| `internal/ws/protocol.go` | 新增 `SessionTitleUpdateData`（`session_id`、`title`、`project_path`） |
| `internal/api/openapi.yaml` | 若端点/字段有变则同步（本次无新端点，仅确认 generate-title 描述不变） |

### 前端

| 文件 | 改动 |
|---|---|
| `web/src/composables/useSettingsConfig.ts` | `serverDefaults['chat.auto_rename_enabled'] = false` |
| `web/src/components/settings/settingsFieldMap.ts` | 聊天章节增开关 + 跳转「AI 摘要模型」action 行（参照推荐回复） |
| `web/src/i18n/locales/zh.ts` / `en.ts` | 开关标题/描述（描述注明需配置 AI 摘要模型）、跳转行文案 |
| `web/src/composables/useGlobalEvents.ts` | `session_title_update` → 派发 `clawbench-session-title-update` |
| `web/src/App.vue` | 监听该事件：更新 `currentSessionTitle` + bump `sessionListVersion` |

## 测试

- `internal/summarize/title_test.go`：超长时末条保留（含「末条本身超长」边界）。
- `internal/model/defaults_test.go`：`auto_rename_enabled` 缺省 false、presence true 生效。
- `internal/service/session_auto_title_test.go`（新）：
  - 开启 + 摘要服务可用 → 标题被 AI 结果覆盖；
  - 关闭 → 保持本地标题；
  - 摘要失败 → 保持本地标题；
  - AI 调用期间用户改名（`custom`）→ 不被覆盖（守卫生效）；
  - fork 会话 → payload 含复制来的历史用户消息 + 新消息。
- `internal/handler/settings_test.go`：patchAndRead 持久化 + 热重载。
- 前端：`settingsFieldMap.test.ts` 覆盖新条目；`useGlobalEvents.test.ts` 覆盖事件派发。

## 兼容性

- 开关默认关闭 → 现有行为完全不变。
- 手动「生成标题」入口保留，且与自动路径共用同一实现。
- 新增 `session_title_update` WS 事件为增量事件，旧客户端忽略未知事件，无破坏性。
