# 单聊自动巡航（Single-chat Auto-Cruise）设计

日期：2026-10-15
状态：设计已定稿，待实现

## 背景

群聊已有「自动巡航」正交开关（design §15，见 `docs/plans/2026-10-04-ai-group-chat-design.md`）：把循环的终止从「主持人发结束标签 / 接力枯竭 / 交回用户」改成「成员明确确认任务已满足用户要求」。现在把它移植到**单聊**。

单聊与群聊的结构差异决定了移植不是复制粘贴：

- 群聊有主持人路由 / 自由接力 / @User / 提问四个「交回用户」点，单聊**唯一**的「交回用户」点就是**模型回复自然结束**。
- 群聊有多个成员可以投票 / 被点名，单聊只有一个 AI。

因此单聊巡航 = **每轮回复结束后，若未声明完成，就注入一次「完成确认」提示词让 AI 自评并继续，循环到发出 `<clawbench-goal-satisfied/>` 或撞上限**。

## 决策汇总

| 维度 | 决策 |
|---|---|
| 定位 | 正交开关（非第三种模式），只改写「何时收敛」，不改写单轮行为 |
| 结束判定 | **照搬群聊严格版**：普通回复里的 `goal-satisfied` 不算数；只有**系统注入的确认轮**里发出标签才结束 |
| 存储 | 复用 `chat_sessions.context_state` 的 `$.cruise`（与群聊同列同键） |
| 上限 | 复用 `chat.group_max_speeches`（默认 100），计**确认轮数** |
| 开关入口 | 会话信息栏（`ChatInputBar` 的 `.chat-session-info`，仅单聊）新增「巡航」chip |
| 过程可见性 | 写系统行（确认时 + 完成时），与群聊一致 |
| 提示词注入 | addendum 拼进**首轮 Prompt**（确保 resume 生效）；确认注入**只拼进确认轮 Prompt，不落库** |
| 协议标签 | 复用 `internal/grouprouting` 的 `<clawbench-goal-satisfied/>`（`Result.GoalSatisfied`），前后端双镜像 |

## 语义

### 终止语义

巡航下，单聊回合不再在「模型回复结束」时交回用户，而是进入**确认回合**：

1. **第 1 轮**：用户的正常消息。系统提示追加巡航 addendum（自主推进、不向用户提问、完成时声明标签）。它的输出**不用于终止**（严格：普通回复不算数），但若含标签，徽章照常渲染。
2. **确认轮（此后每一轮都是确认轮）**：隐藏注入确认提示词（不落库），AI 自评。
   - 确认轮里出现 `<clawbench-goal-satisfied/>` → **结束**，该轮回复即最终结论。
   - 未出现 → 再确认一轮。
3. **上限**：确认轮数达 `chat.group_max_speeches` → 写「已达自动续跑上限」系统行并结束（沿用现有上限收尾语义）。
4. **用户点停止** → 完全沿用现有取消路径，不触发确认。
5. **用户中途发消息** → 排队（沿用 `queued_messages`），巡航结束后处理。drain 循环每 dequeue 一条真实用户消息，巡航轮数**归零**（预算是「每个用户轮次」的，不是「每个会话」的——与 `autoContinueAttempts` 同构）。

### 系统行（可见痕迹）

- 每次确认前写：**「自动巡航：正在确认任务完成情况」**（每确认轮都写）。
- 确认完成写：**「自动巡航：任务已确认完成，共 N 轮」**（N = 确认轮数）。
- 撞上限写：**「自动巡航：已达自动续跑上限」**（新增独立 i18n 键，不复用群聊的「成员间讨论次数达到上限」）。

系统行复用 `AddSystemMessage` / `emitGroupSystemMessage`（已是 session 级通用）；前端 `useChatStream` 的 `system_message` 分支与 reducer 已通用，无需改动渲染路径。

## 提示词

### A. 巡航 addendum（开启巡航后拼进首轮 Prompt）

```
[自动巡航模式] 本会话已开启自动巡航：在你（AI）确认任务已完成之前，对话不会交回用户。
请持续自主推进用户的要求，直到确实完成，不要为了收工而过早收尾。
巡航期间不要向用户提问，也不要等待用户确认——所有不明确之处，请通过自主探索、实验与思考自行决定。
当系统向你发起「完成确认」时，若你认为已经满足用户的要求，输出结束信号 <clawbench-goal-satisfied/>，
并在同一条回复里写出最终结论；若尚未完成，就继续推进尚未完成的部分，不要输出该信号。
不要主动输出 <clawbench-goal-satisfied/>——只有系统向你确认时才判断（提前输出不算数）。
```

### B. 确认注入（每个确认轮，隐藏，不落库）

```
[自动巡航·完成确认] 当前没有新的用户消息，请不要向用户提问，也不要等待用户确认。
所有不明确之处，请通过自主探索、实验、思考自行决定。
请评估：当前任务是否已经满足了用户最初的要求？
若确认已完成：输出 <clawbench-goal-satisfied/>，并在同一条回复里直接写出最终结论（供用户阅读，不要再提问）。
例外：若你上一条回复已经写出了完整的最终结论，本轮只需输出 <clawbench-goal-satisfied/> 并加一句确认（如「结论同上」），不要重复展开。
若尚未完成：继续推进尚未完成的部分，不要输出 goal-satisfied。
```

`[自动巡航·完成确认]` 标记照搬群聊，`cmd/acp-mock` 的 `cruiseConfirmReply` 零改动即可命中单聊 E2E。

### 注入方式（关键实现细节）

- **A 段**：`BuildChatRequest` 在 `GetSessionCruise(sessionID)` 为真时把 A 段拼进 `ChatRequest.Prompt`（**不是** `SystemPrompt`）。理由：CLI resume 时 `SystemPrompt` 未必重新发送（`ShouldInjectSystemPrompt` 在 resume + interval=0 时返回 false），而 `Prompt` 每次都发。群聊成员提示词正因如此拼进 prompt。
  - `ChatRequest.Prompt` 是**瞬态字段**——持久化的是 `req.Message`（`AddChatMessage` 用的是 `req.Message`），所以把 addendum 拼进 prompt 不会污染用户气泡或历史。
  - 拼接点：`BuildChatRequest` 末尾，`prompt = BuildCruiseSingleAddendum() + "\n\n" + prompt`（仅在巡航开启时）。这样 direct-send 与 queue-drain 两条路径都经同一个 builder，不会漂移。
- **B 段**：巡航 runner 在确认轮把 B 段**前置到该轮的 `Prompt`**，不持久化、不发 `user_message`。用户只在时间线上看到系统行 + AI 回复。

## 后端实现

### 存储（`group_store.go`）

`GetGroupCruise` / `SetGroupCruise` 的底层本就是**按 session id 读 `context_state` 的 `$.cruise`**，与群/单聊无关。更名以便单聊复用（群聊调用点同步改名）：

- `GetGroupCruise` → `GetSessionCruise`
- `SetGroupCruise` → `SetSessionCruise`

默认关；SQLite 的 `json_extract` 把 JSON true 渲染为 `1`，`"1"`/`"true"` 判真。

### 解析（`run_turn.go` / `drain.go`）

- `TurnResult` 加 `GoalSatisfied bool`；`runTurnFinalize` 从最终输出解析（`grouprouting.Parse(ExtractPlainText(finalized content)).GoalSatisfied`）。
- `streamRunResult`（`handler/chat.go`）与 `streamRunResultShared`（`session_command.go`）透传；`DrainResult` 加 `GoalSatisfied bool`。

> 注意：`GoalSatisfied` 只用于**确认轮**的终止判定。首轮（普通回复）的标签只驱动徽章，不终止。

### 巡航 runner（新增 `internal/service/cruise.go`）

与 `auto_continue.go` 的 `NewAutoContinueRunner` 对称：

```go
type CruiseRunnerConfig struct {
    Ctx         context.Context
    SessionID   string
    ProjectPath string
    BackendName string
    // RunTurn executes one confirmation turn and returns its outcome.
    RunTurn func(prompt string) DrainResult
}

func NewCruiseRunner(cfg CruiseRunnerConfig) func(attempt int, prev DrainResult) (DrainResult, bool)
```

`NewCruiseRunner` 返回的钩子逻辑：

1. `!GetSessionCruise(sessionID)` → `(prev, false)`（未开启）。
2. `prev.GoalSatisfied` → `(prev, false)`：**上一轮（确认轮）已声明完成** → 结束；由 drain 循环收尾（写「任务已确认完成」系统行）。首轮的 `GoalSatisfied` 由调用方**不**透传（见下）。
3. `attempt-1 >= GetGroupMaxSpeeches()` → 写上限系统行 → `(prev, false)`。
4. `cfg.Ctx.Err() != nil` / `!IsSessionRunning` → `(prev, false)`。
5. 写「正在确认」系统行；`emitTurnFinalize(sessionID, prev.MsgID)` 收口上一气泡；`RunTurn(BuildCruiseSingleConfirmPrompt())`；返回 `(next, true)`。

> `attempt` 语义：drain 循环维护一个 `cruiseRounds` 计数，dequeue 真实用户消息时归零。首轮不计入 `attempt`（它是用户轮，不是确认轮）。

### drain 钩子（`drain.go`）

`DrainConfig` 加：

```go
// CruiseContinue, when non-nil, is called after a turn finished CLEANLY and
// the session has cruise enabled, to run the next confirmation round instead
// of terminating. Returns the resumed turn's result, or ok=false to stop.
CruiseContinue func(attempt int, prev DrainResult) (DrainResult, bool)
```

`RunDrainLoop` 里，在 `shouldAutoContinue` 之后、`drainHandleTerminal` 之前插入：

```go
if shouldCruiseContinue(cfg, result, cruiseRounds) {
    cruiseRounds++
    if next, ok := runCruiseContinue(cfg, cruiseRounds, result); ok {
        result = next
        continue
    }
}
```

`shouldCruiseContinue` 的守卫（与 `shouldAutoContinue` 同构）：

- `cfg.CruiseContinue == nil` → false。
- 回合**必须干净**（`CancelReason == "" && Err == "" && !Empty && AbnormalReason == ""`）——巡航只在正常结束时接管，取消/错误照常终止。
- 用户有排队消息（`GetQueuedCount > 0`）→ false（用户显式消息优先）。
- `!IsSessionRunning` → false。

`autoContinueAttempts` 与 `cruiseRounds` 都在 dequeue 成功时归零。

> 与 `AutoContinue` 的顺序：先 `AutoContinue`（异常恢复）再 `CruiseContinue`（正常续跑）。二者互斥：`AbnormalReason != ""` 时巡航守卫为假，`AbnormalReason == ""` 时自动续跑守卫为假。

### 收尾

确认完成时由 drain 循环/runner 写「任务已确认完成，共 N 轮」系统行。**不额外跑总结轮**——确认轮的回复已含最终结论（提示词要求）。

### 两条接线路径

- `handler/chat.go` 的 `go func()`：`DrainConfig` 加 `CruiseContinue: service.NewCruiseRunner(...)`，`RunTurn` 闭包用 `buildChatRequest` 重建请求（确认提示词作为 prompt 传入）。
- `service/session_command.go` 的 `LaunchSessionExecution`：同样加 `CruiseContinue`，`RunTurn` 用 `executeStreamRunShared`。

## HTTP / OpenAPI

- `PATCH /api/ai/session/update`（`handler/chat_session.go` 的 `ServeAISessionUpdate`）加 `Cruise *bool`；`Cruise != nil` 时 `service.SetSessionCruise(sessionID, *req.Cruise)`。
- `GET /api/ai/chat`（`handler/chat.go`）响应加 `"cruise": service.GetSessionCruise(sessionID)`（两处 `writeJSON`，即 err 与正常分支）。
- `internal/api/openapi.yaml` 同步：`/api/ai/chat` 的 `cruise` 响应字段、`/api/ai/session/update` 的 `cruise` 请求字段。

## 前端

### 开关 chip（`ChatInputBar.vue`）

`.chat-session-info`（`v-if="!isGroupSession"`）内新增「巡航」chip，与模型/模式/用量同排：

- 点击切换 `cruise`，toast 提示开启/关闭。
- 开启时 chip 高亮 + 显示「巡航中」（复用 `group.cruiseActive` 文案或新增 `chat.cruise.active`）。
- 图标用 lucide `Navigation`（与群聊 pill 一致）。

### 状态（`useSessionIdentity.ts`）

加 `cruise` ref + `toggleCruise(enabled)`，照抄 `autoApprove` / `toggleAutoApprove`（PATCH `/api/ai/session/update`），并在 `loadHistory` 从 GET 响应的 `data.cruise` 初始化。

### 徽章（`ChatMessageItem.vue` + `groupRouting.ts`）

> **已反转（2026-10-16）**：单聊只有一个人，完成徽章是冗余噪音，**已移除**。单聊 assistant 行不再渲染任何 `goal-satisfied` 徽章（`singleChatGoalSatisfied` 计算属性与 `.msg-goal-satisfied-standalone` 样式一并删除；`hasGoalSatisfied` 因无消费方而删除）。`stripGoalSatisfiedTag` **保留**——它仍负责把标签从正文/朗读/复制/引用中剥掉。以下为原始设计记录。

单聊也要显示完成徽章。当前徽章由 `groupRouting.goalSatisfied` 驱动，而 `groupRouting` 在非群聊时返回 `empty`（`speaker.value` 为 null 且非群聊用户行 → 空）。改法：

- `ChatMessageItem.vue`：单聊 assistant 行也解析 `goal-satisfied`（新增轻量 `hasGoalSatisfied(text)` 或让 `groupRouting` 在单聊 assistant 行也解析 `goalSatisfied` 字段，但**不**解析 mention）。
- **安全**：单聊**只**剥离/解析 `<clawbench-goal-satisfied/>`（自闭合、无内容），**绝不**启用 mention 剥离——那会重演 msg 58879（单聊正文里讨论标签语法被截断）。
- `groupRouting.ts` 加 `stripGoalSatisfiedTag(text)`（仅剥该标签，返回正文）与 `hasGoalSatisfied(text)`。
- 徽章样式复用现有 `.msg-goal-satisfied`（`--color-success`）。

### i18n（zh/en）

- `chat.cruise.label` = 自动巡航 / Auto-cruise
- `chat.cruise.active` = 巡航中 / Cruising
- `chat.cruise.enabled` / `chat.cruise.disabled` toast
- 系统行（后端 `active.{zh,en}.yaml`）：
  - `CruiseConfirming` = 自动巡航：正在确认任务完成情况
  - `CruiseSatisfied` = 自动巡航：任务已确认完成，共 {{.Rounds}} 轮
  - `CruiseMaxRounds` = 自动巡航：已达自动续跑上限

## 测试

- **Go**：
  - `cruise_test.go`：runner 四态（未开启 / 已声明完成 / 撞上限 / 取消）+ drain 循环集成（干净回合续跑、异常回合不续跑、排队消息优先）。
  - `group_store_test.go`：`GetSessionCruise`/`SetSessionCruise` round-trip（改名后）。
  - `group_prompt_test.go`：A/B 段文案含关键标记。
  - `run_turn_test.go`：`GoalSatisfied` 从输出解析。
  - handler：`ServeAISessionUpdate` 的 `cruise` 字段；`GET /api/ai/chat` 的 `cruise`。
- **前端**：`useSessionIdentity` toggle、`ChatInputBar` chip、`ChatMessageItem` 单聊徽章、`groupRouting` 的 `hasGoalSatisfied`/`stripGoalSatisfiedTag`。
- **E2E**：`e2e/specs/single-cruise.spec.ts`（mock 已认 `[自动巡航·完成确认]`，零改动）。改 Go 后必须 `go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock`。

## 边界

- 巡航进行中用户主动发消息 → 排队，本轮巡航结束后处理。
- 单聊无 host/free 之分，故无「host 只问一人 / free 群发半数」的分支——单聊确认轮只问唯一的 AI。
- 上限复用 `group_max_speeches`（默认 100）：单聊「确认轮」会持续消耗它，大任务可能几轮就撞；用户可调高。

## 关键文件

| 层 | 文件 |
|---|---|
| 存储 | `internal/service/group_store.go`（`GetSessionCruise`/`SetSessionCruise`） |
| 解析 | `internal/service/run_turn.go`、`internal/service/drain.go`、`internal/grouprouting/grouprouting.go` |
| 提示词 | `internal/service/group_prompt.go` |
| 循环 | `internal/service/cruise.go`（新增）、`internal/service/drain.go`、`internal/handler/chat.go`、`internal/service/session_command.go` |
| 请求 | `internal/service/chat_request.go` |
| HTTP | `internal/handler/chat_session.go`、`internal/handler/chat.go`、`internal/api/openapi.yaml` |
| i18n | `internal/i18n/locales/active.{zh,en}.yaml`、`web/src/i18n/locales/{zh,en}.ts` |
| 前端 | `web/src/composables/useSessionIdentity.ts`、`web/src/components/chat/ChatInputBar.vue`、`web/src/components/chat/ChatMessageItem.vue`、`web/src/utils/groupRouting.ts` |
| 测试 | `internal/service/cruise_test.go`、`e2e/specs/single-cruise.spec.ts` |
