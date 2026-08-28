# 推送权威消息同步 + 断线补偿方案

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 解决多端同时操作同一会话时，「A 发消息 B 不实时可见」的根因。核心思路：**所有消息气泡的权威身份以后端 WS 推送事件为准**（不再依赖本地乐观气泡猜身份），WS 断线期间漏掉的事件通过**增量拉取（事件游标）**补偿，把「每个事件都要靠 DB 全量收敛兜底」降级为「仅断线时一次增量补偿」。同时把「会话订阅」从「流式状态」中解耦——**打开会话即持续订阅**，消除「订阅晚于事件广播」的竞态窗口。

**Architecture:** 三层改动：
1. **订阅生命周期解耦**：打开会话即 `subscribe`，切换/关闭退订，WS 断线重连后重订阅。订阅不再与 `isStreaming` 绑定。
2. **断线补偿**：复用现有 `pending_events` 机制（`GET /api/ai/events/pending?after=evt_xxx`），扩展为存储 `user_message` 事件；前端 WS 断线重连后拉取漏掉的 `user_message`。
3. **乐观气泡简化**：气泡身份统一以后端推送为准——发送方收到自己 `user_message` 事件后原地 adopt 真实 id；接收方 `_remote` 气泡直接从事件拿真实 id + queueId。

**Tech Stack:** Go（后端）、Vue 3 + TypeScript（前端）、SQLite（chat_history + pending_events）

---

## 评审修正记录（2026-08-28 架构评审）

设计初稿经 architect-review 子智能体审查后修正，以下决策是评审结论，实施时**不得回退**：

| 编号 | 问题 | 修正决策 |
|---|---|---|
| **F1** | `pendingEventExpiresAt` 无需新增默认分支 | `user_message` 的 `event="chat_stream"` + `status=""` 自动走 24h 默认 TTL——**删除原文"需增加默认分支"说明** |
| **F2** | "write-ahead"描述暗示 `user_message` 已采用 | **限定为**「现有 `session_update`/`task_update` 的 `emitSessionEvent` 采用 write-ahead；`user_message` 广播点没有先存——需补上」 |
| **F3** | 遗漏 `useTaskExecStream.ts` 的订阅逻辑 | **补充**：任务执行会话订阅独立于聊天会话（不同 session_id），`useTaskExecStream` 的 done 后 unsubscribe 保持不变 |
| **F4** | `chat.go:475-484` 入队分支 `MessageID: 0` 导致 B 端 `_remote` 气泡简化前提不成立 | **前置条件**：实施前修复为 `MessageID: msgID`（`AddQueuedMessage` 返回值），与排队消息持久化方案 B5 一致 |
| **F5** | `LAST_SEEN_KEY` 游标只对 terminal 事件更新，断线拉取可能重复处理旧 `user_message` | **补充**：扩展游标更新策略，`user_message` 事件也更新 `LAST_SEEN_KEY` |
| **F6** | `onSessionEvent` running 分支删 `onConnectStream` 后替代方案不明 | **明确**：改为 `loading=true` + `ensureStreamingPlaceholder()`（创建占位符，不涉及订阅） |
| **F7** | 实施阶段 1 与阶段 2 需原子完成（删 streamTimeout 是订阅解耦的一部分） | **合并**阶段 1+2 为一个原子阶段；阶段 3 降级与阶段 6 合并 |

---

## 前置条件

1. **`chat.go` 入队分支 `MessageID: 0` 修复**：`internal/handler/chat.go:475-484` 会话运行时入队分支的 `user_message` 广播 `MessageID: 0`（未携带 DB id），导致 B 端 `_remote` 气泡只能用 `remote-*` 随机 id，第三部分「乐观气泡简化」的前提不成立。**需在实施前修复**：`AddQueuedMessage` 返回 msgID 后，将 `MessageID: 0` 改为 `MessageID: msgID`。此修复与排队消息持久化方案（`2026-08-24-queued-message-persistence.md`）的 B5 修正一致。

---

## 现状根因

### 为什么不实时同步

前端订阅与流式状态耦合：**只在会话 running/replay 时订阅**，idle 会话打开时只 `loadHistory` 不订阅。事件广播（`user_message`/`queue_drain`）只发给**当时已订阅**的客户端（`StreamHub.Emit` 按订阅者集合扇出），因此：

```
B 打开 idle 会话 → 不订阅（无流可收）
A 发消息 → 后端广播 user_message（B 未订阅，收不到）
        → 广播 session_update running hasNewMessages=FALSE（B 收到但无刷新信号）
B 收到 running → 现在才 subscribe ← 订阅晚于广播，事件已丢
B 只能等 queue_drain 防御性 content 重建 / 会话完成后的 loadHistory 兜底
```

### 为什么「只要 WS 不断就不丢」不可行

后端 WS 推送有三处可靠性边界：

| 边界 | 位置 | 后果 |
|---|---|---|
| `sendQueue` 满（256 条）→ **主动掐断连接** | `internal/ws/manager.go:268-285` | 慢客户端（移动端后台/网络差）消费不及时 → 队列满 → 后端强制断线。WS 断之前未发出的 256 条可能丢 |
| 断线缓冲窗口仅 **10s / 50 条** | `internal/ws/manager.go:58,62` | 断线超过窗口的事件直接丢弃 |
| ack 不触发重发 | 事件系统设计要点 | 客户端 ack 只是确认，后端不会补发漏掉的事件 |

结论：**「WS 不断」这个前提在慢客户端场景下后端自己会破坏**，纯依赖推送在工程上不可行。必须有断线补偿。

---

## 方案总览

```
发送方：
  点击发送 → 乐观气泡立即显示（queueId 占位，UI 即时响应）
  后端处理 → 广播 user_message（真实 messageId + queueId + senderClientId）
  发送方收到自己事件 → 原地 adopt 真实 id（保持 DOM 对象，无闪烁）

接收方（B）：
  WS 正常 → user_message 事件 → 插入气泡（真实 id + queueId）→ 后续流式事件实时
  WS 断线 → 重连后增量拉取（pending 游标）补齐 user_message → 流式事件从订阅点跟进
  兜底   → session_update completed hasNewMessages=true → loadHistory 全量收敛
```

**关键原则**：
- 消息气泡的**权威身份来自后端推送事件**，本地乐观气泡只是 UI 占位
- 最终一致性由「断线后增量补偿」保证，而非每个事件都要靠 DB 全量收敛
- `queueId` 保留，但角色从「多通道匹配谜题」简化为「占位 → 事件对号」的单一用途

---

## 第一部分：订阅生命周期解耦（打开会话即持续订阅）

### 现状问题

`useChatStream.ts` 用 `isStreaming` 一个标志管两件事：是否订阅 + 是否有活跃流。订阅只在 running/replay/发送时建立，`done`/`error`/`replay_done`/`cancelled` 时拆除。由此产生一整套「订阅晚于广播」的补救链：

```
streamTimeout (30s 无事件 → disconnect + reload)        ← 补救链起点
  → loadHistory 的 isRunning 分支 re-connectStream
    → reuseExistingStreaming / subscribeOnly 选项
      → forceNotRunning 参数
        → syncSessionOnReconnect 的 running/finished 分支
          → done/error/replay_done 里 disconnectStream
```

### 改后模型

两个独立概念：

| 概念 | 生命周期 | 作用 |
|---|---|---|
| `isSubscribed` | **打开会话时订阅，切换/关闭时退订，WS 断线时重订阅**（常驻） | 收发 WS 事件 |
| `isStreaming` | 每回合独立：有活跃 AI 流时为 true，`done`/`error`/`cancelled` 时 false | 决定 `loading`、流式占位符 |

### 前端改动

**`useChatStream.ts`**：

```typescript
// 拆分：订阅与流式解耦
function subscribe(sessionId: string) {
  // 去重：已订阅同一会话则跳过（避免 OnSubscribe 重复重放 ACP 状态）
  if (isSubscribed && subscribedSessionId === sessionId) return
  sendWsMessage({ type: 'subscribe', session_id: sessionId })
  subscribedSessionId = sessionId
  isSubscribed = true
}

function unsubscribe() {
  if (isSubscribed && subscribedSessionId) {
    sendWsMessage({ type: 'unsubscribe', session_id: subscribedSessionId })
  }
  isSubscribed = false
  subscribedSessionId = null
}

function ensureStreamingPlaceholder() {
  // 原 connectStream 的占位符创建逻辑（finalize stale + push new streaming）
}

function stopStreaming() {
  // 清流式状态：streamTimeout、toolUseWatchdog、thinkingBlockCounter
  // 但不 unsubscribe
}
```

- `connectStream(sessionId, opts)` 重构为 `subscribe(sessionId)` + `ensureStreamingPlaceholder()`
- `disconnectStream()` 拆成 `unsubscribe()` 与 `stopStreaming()`
- `done`/`error`/`replay_done`/`cancelled` 分支：只调 `stopStreaming()`，**不再 unsubscribe**
- `watch(connected)`：改为 `if (isConnected && isSubscribed) subscribe(subscribedSessionId)`（WS 断线后 backend `UnsubscribeAll` 清了订阅，重连必须重订阅；配合 `subscribedSessionId` 去重，只发一次）
- 删 `streamTimeout` 整个机制（30s 无事件强制 reload）。idle 会话长时间无事件是正常现象。兜底由 `useGlobalEvents` 心跳（60s 无消息强制重连）承担

**`useChatSession.ts`**：

- `switchSession(sessionId)`：先 `onDisconnectStream()`（内部改为 unsubscribe 旧会话），`loadHistory` 成功后 `onConnectStream(subscribeOnly)`（内部改为 subscribe 新会话）
- `createSession`：创建成功后 subscribe
- `loadHistory` 的 `isRunning`/`isReplayPending` 分支：**删 `onConnectStream` 调用**，只保留 `loading.value = true` + `onScrollBottom`（订阅已在会话打开时建立）
- `onSessionEvent` running 分支：删 `onConnectStream` 恢复逻辑；`loading=false` 时改为 `loading=true` + `ensureStreamingPlaceholder()`（创建流式占位符，不涉及订阅——订阅已常驻）
- 删 `forceNotRunning` 参数、`reuseExistingStreaming`/`subscribeOnly` 选项
- `syncSessionOnReconnect` 简化为纯 `loadHistory(skipIfUnchanged=true)` + `watch(connected)` 无条件重订阅
- `skipIfUnchanged` 的 `!isRunning` 条件删除（running 期间由事件驱动增量渲染）

**`ChatPanelContent.vue`**：

- 删 `sendMessageNow` 里两处 `stream.connectStream`（851/858 行）——发送后订阅已存在，只需 `ensureStreamingPlaceholder`
- `onUnmounted` 的 `disconnectStream` 改为 `unsubscribe`

### useTaskExecStream 订阅协调

`useTaskExecStream.ts` 也使用 `sendWsMessage({ type: 'subscribe' })` 订阅任务执行会话，且有独立的 `done` 后 unsubscribe 逻辑。本次改动需确保：

- 任务执行会话的订阅**独立于**聊天会话的订阅（不同 session_id，互不干扰）
- `useTaskExecStream` 的 `done` 事件 unsubscribe **保持不变**（任务执行会话不需要常驻订阅——执行完即结束）
- 不会出现同一 session_id 被 `useChatStream` 和 `useTaskExecStream` 双重 subscribe（正常使用中不会出现：任务会话 ≠ 聊天会话）

### 保留清单（不是补救，是正常机制）

| 逻辑 | 位置 | 原因 |
|---|---|---|
| `rebuildFromDb` live placeholder 三通道匹配 | `chatStreamUtils.ts:750-797` | 流进行中 loadHistory 不打断渲染 |
| `_remote` 插入 + rebuildFromDb 收养 | `chatStreamUtils.ts:998-1028, 844-857` | 跨设备 user 消息主路径 |
| `drainQueueMessage` 防御性 content 重建 | `chatStreamUtils.ts:577-594` | loadHistory 丢 bubble 的兜底（REST/WS 交错竞态，与订阅无关） |
| `skipIfUnchanged` / `lastMessageSnapshot` | `useChatSession.ts:141-145` | reconnect/事件驱动的防 churn |
| `onSessionEvent` completed/cancelled 兜底 | `useChatSession.ts:884-899` | done 事件错过的最终安全网 |
| `has_new_messages` → loadHistory | `useChatSession.ts:904-906` | 消息同步机制（替代旧轮询） |

---

## 第二部分：断线补偿（增量拉取）

### 复用现有机制

后端已有：
- `pending_events` 表（`internal/service/pending_events.go`）
- `GET /api/ai/events/pending?after=evt_xxx`（`internal/handler/pending_events.go:10`）
- 条件存储：`HasDisconnectedClients()` 为 true 时才写 DB（`pending_events.go:184`）。注意：`HasDisconnectedClients()` 按**全局**而非 per-session 检查——当所有客户端都未 WS 连接时也返回 true。Android 原生 WS 断线可能导致误判（详见风险清单）
- 现有 `session_update`/`task_update` 的 `emitSessionEvent` 采用 write-ahead 模式（先 `StoreNotifiableEvent` 后 `BroadcastEvent`）。但 `user_message` 的广播点（handler 层）直接 `ws.EmitToSession`，**没有先存**——需补上

**现状局限**：`IsNotifiableEvent` 只放行 terminal 状态事件（completed/cancelled/permission_pending 等），**不含 `chat_stream` 事件**。断线窗口内漏掉的 `user_message` 无法补回。

### 后端改动

**扩展 `user_message` 事件入 `pending_events`**：

`internal/service/pending_events.go` 的 `IsNotifiableEvent` 增加对 `chat_stream` + `user_message` 的判断：

```go
// IsNotifiableEvent 扩展：user_message 事件也需要离线补偿
func IsNotifiableEvent(event string, data any) bool {
    // 现有 terminal 状态事件逻辑保留
    // 新增：
    if event == "chat_stream" {
        if cs, ok := data.(ws.ChatStreamData); ok {
            if cs.EventType == "user_message" {
                return true
            }
        }
    }
    return false
}
```

注意：`StoreNotifiableEvent` 存的是 `msg`（`ws.ServerMessage`），`msg.Data` 是 `ChatStreamData`，JSON 序列化后完整保留 `session_id`/`event_type`/`payload`。前端拿到后按现有 `chat_stream` 处理路径 dispatch 即可（`useChatStream` 的事件分发天然支持）。

**`user_message` 的广播点补 `StoreNotifiableEvent`**：`user_message` 广播在 handler 层（`queue.go:110`、`chat.go:475/503`）直接 `ws.EmitToSession`，**没有先存**。需改为 write-ahead 模式：

```go
// handler 层广播前先尝试存储（条件存储内部有 HasDisconnectedClients 守卫）
msg := ws.ServerMessage{ Type: "event", ID: ws.GenerateEventID(), Event: "chat_stream", Data: ... }
service.StoreNotifiableEvent(msg)
ws.GetManager().BroadcastEvent(msg)  // 或 EmitToSession
```

**`expires_at` 处理**：`StoreNotifiableEvent` 内部调用 `pendingEventExpiresAt(event, status)` 计算 TTL。`user_message` 事件的 `event="chat_stream"`，`status=""`（空），不匹配任何特殊条件，自动走 24h 默认 TTL——**无需新增分支**。

### 前端改动

**断线重连后拉取漏掉的 `user_message`**：

`useGlobalEvents.ts` 已有 `fetchPendingEvents()`（`useGlobalEvents.ts:141`）——但它目前只拉 `session_update` 补 unread 状态。扩展为：

1. WS 重连成功（`onopen`/`connected` 变 true）后，用本地 `last_seen_event_id` 游标调 `GET /api/ai/events/pending?after=...`
2. 返回的事件若是 `chat_stream` + `user_message`，且 `session_id` 是当前打开的会话 → 走 `useChatStream` 的 `user_message` 分发路径（插入 `_remote` 气泡或 adopt id）
3. 更新本地 `last_seen_event_id` 游标（**扩展**：对 `user_message` 事件也更新游标，避免下次重连重复拉取已处理的旧事件。现有 `useGlobalEvents.ts:266-277` 只在 terminal 事件时更新 `LAST_SEEN_KEY`，需扩展为 `user_message` 也更新）

**去重**：`useGlobalEvents` 已有 `processedEventIds`（`useGlobalEvents.ts:98-113`）按事件 id 去重，WS 实时收到 + pending 拉取双通道不会重复。

---

## 第三部分：乐观气泡简化（身份以后端推送为准）

### 现状复杂度来源

发送方乐观气泡（string id = queueId）→ 收到自己 `user_message` 事件 → `optimistic_adopt_id` 匹配 string id adopt DB id；接收方 `_remote` 气泡（随机 string id）→ 靠 `_remoteQueueId` 三通道匹配被 `queue_drain` 升级。两条路径都靠「string id ↔ DB id」的桥接猜谜。

### 简化后

**发送方**：

```
点击发送 → push 乐观气泡 { id: pendingId(string), queueId, pending: true }
收到自己 user_message（senderClientId === 本机）
  → dispatch optimistic_adopt_id（保留现有逻辑——它已足够简单：按 queueId/string id 匹配，原地 adopt DB id）
```

发送方改动最小：`optimistic_adopt_id` 已按 string id（=queueId）匹配，事件到达即 adopt。**无行为变化**，只是现在订阅常驻后事件到达率接近 100%，adopt 更可靠。

**接收方（B）**：

```
收到 user_message（senderClientId ≠ 本机）
  → dispatch ws_user_message
    → 气泡直接带真实 messageId + queueId
    → _remote 标记 + _remoteQueueId 保留（queue_drain 三通道匹配仍需）
```

`ws_user_message`（`chatStreamUtils.ts:998-1028`）保留。**唯一可简化的点**：`user_message` 事件在排队路径（`queue.go:110`）已带真实 `msgID > 0`（`MessageID: msgID`），B 端气泡 `id: msgId > 0 ? msgId : remote-*` 直接走数字 id 分支——随机 id 兜底分支在「user_message 必达」假设下退化为纯防御。**保留随机 id 兜底**（WS 断线 + pending 补偿失败的极端情况），但不再是主路径。

### queueId 三通道匹配（必须保留）

`drainQueueMessage` 的三通道 OR 匹配（`chatStreamUtils.ts:542-547`：`m.id === queueId || m.queueId === queueId || _remoteQueueId === queueId`）是防重复的核心——它处理的是「乐观气泡 → DB 行」的身份收敛，与订阅时机无关。**保留**。

---

## 与排队消息逻辑的关系（结论：几乎全部保留）

深入分析后确认：**排队消息逻辑的复杂度不是来自订阅时机，而是来自「WS 实时 + REST loadHistory 两通道对同一气泡生命周期的协调」**。持续订阅只消除「订阅晚于广播」这一种竞态，排队逻辑的两通道协调竞态（REST 响应与 WS 事件交错、loadHistory 数组重建与 queue_drain 消费交错）依然存在。因此：

| 逻辑 | 命运 | 原因 |
|---|---|---|
| `queueId` 生成/传递/回显 | **保留** | 持久化 DB 身份，多通道匹配键 |
| `queue_drain` 三通道匹配 + adopt | **保留**（主路径） | 防重复核心 |
| 防御性 content 重建分支 | **保留** | REST/WS 交错竞态兜底，与订阅无关 |
| `rebuildFromDb` queued 行处理 | **保留**（降为兜底） | 切换/刷新/重连窗口恢复 pending 气泡 |
| `_remote` + `_remoteQueueId` | **保留**（B 端主路径） | 让 queue_drain 升级 B 端气泡 |
| `queue_cancel` / `cancelPendingMessages` | **保留** | 逻辑不变 |
| `needs_start` 重提交 | **已删除**（后端 B2 self-heal） | 无操作 |
| `queuedCount` 维护 | **可选简化** | 收益小，不做 |

---

## 测试计划

### 后端 Go 测试

#### 1. pending_events 扩展测试（`pending_events_test.go` 追加）

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestIsNotifiableEvent_UserMessage` | `chat_stream` + `user_message` 判定为可通知 | 返回 true |
| `TestStoreNotifiableEvent_UserMessage_Disconnected` | 有断线客户端时存 `user_message` | pending_events 表有该行，payload 完整（含 messageId/queueId） |
| `TestStoreNotifiableEvent_UserMessage_NoDisconnected` | 无断线客户端时不存 | 不写库 |
| `TestGetPendingEvents_ReturnsUserMessage` | `after=游标` 拉取含 `user_message` | 返回的 events 含 user_message，顺序按 id ASC |
| `TestPendingEventExpiresAt_UserMessage` | `user_message` 的 expires_at | 等于默认值（24h） |

#### 2. user_message 广播点 write-ahead 测试

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `TestQueueEnqueue_UserMessageWriteAhead` | 入队广播 `user_message` 前先 `StoreNotifiableEvent` | 有断线客户端时 pending_events 有行，且 id 与广播事件一致 |
| `TestChatDirectSend_UserMessageWriteAhead` | 直接发送广播 `user_message` 前先存 | 同上 |

### 前端 Vitest 测试

#### 3. 订阅解耦测试（`useChatStream.test.ts` / `useChatSession.test.ts` 改写）

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `subscribe on session open, unsubscribe on switch` | 打开会话 subscribe，切换会话 unsubscribe 旧 + subscribe 新 | `subscribe`/`unsubscribe` 消息按顺序发送 |
| `done event does NOT unsubscribe` | 会话 done 后订阅保持 | 无 `unsubscribe` 消息；`isSubscribed` 仍 true |
| `reconnect resubscribes when subscribed` | WS 断线重连，已订阅会话 | 恰好一次 `subscribe`（去重） |
| `reconnect does NOT resubscribe when not subscribed` | WS 断线重连，未打开会话 | 无 subscribe |
| `streamTimeout removed: idle session no forced reload` | idle 会话 30s 无事件 | 不触发 loadHistory/disconnect |
| `subscribe dedup: same session twice` | 重复 subscribe 同一会话 | 只发一次 |
| `onSessionEvent running: ensureStreamingPlaceholder when loading=false` | running 事件到达但 loading=false | `loading=true` + placeholder 存在，无 subscribe |

#### 4. 断线补偿测试（`useGlobalEvents.test.ts` 追加）

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `fetchPendingEvents dispatches user_message for current session` | pending 拉取返回 `chat_stream user_message`，session 匹配 | 走 `user_message` 分发，插入 `_remote` 气泡 |
| `fetchPendingEvents skips other sessions` | session 不匹配 | 不插入 |
| `pending user_message dedup by event id` | 同一事件 WS 已收到 + pending 又拉取 | 不重复插入 |
| `fetchPendingEvents user_message deferred until session loaded` | pending 拉取时 `currentSessionId` 未就绪 | 事件暂存或延迟 dispatch，不丢失 |

#### 5. rebuildFromDb 幂等性测试（`chatStreamUtils.test.ts` 追加）

| 用例名 | 测试内容 | 关键断言 |
|---|---|---|
| `rebuildFromDb adopts _remote bubble by numeric id` | 实时 `_remote` 气泡（msgId > 0）被 DB 行正确收养 | `_remote` 标记清除，对象身份保留 |
| `rebuildFromDb adopts _remote bubble by _remoteQueueId → DB queueId` | `_remoteQueueId` 匹配 DB 行 queueId | 收养成功，queueId 回填 |
| `断线期间 content 事件丢失后 loadHistory 收敛` | 断线 → 重连 → loadHistory | 消息内容与 DB 一致，无丢失 |

#### 6. 排队逻辑回归（保留全部现有用例）

现有 `chatStreamUtils.test.ts` 的 RC 系列、queue_drain 系列、`useChatSession.test.ts` 的 loadHistory 系列**全部保留**，作为两通道协调的回归护栏。

### 测试执行验证

| 阶段 | 命令 | 通过标准 |
|---|---|---|
| 后端单元测试 | `go test ./internal/service/... ./internal/handler/... ./internal/ws/...` | 全部通过 |
| 前端单元测试 | `npm test` | 全部通过 |
| 类型检查 | `vue-tsc --noEmit` | 无错误 |
| 全量回归 | `./scripts/pre-push-checks.sh` | 全部通过 |

---

## 实施顺序

| 阶段 | 内容 | 验证点 |
|---|---|---|
| 0 | **前置条件**：修复 `chat.go:475-484` 入队分支 `MessageID: 0` → 改为 `MessageID: msgID`（`AddQueuedMessage` 返回值） | B 端 `_remote` 气泡能带数字 id |
| 1 | **订阅解耦 + 删 streamTimeout**（原子操作）：`useChatStream` 拆 `subscribe`/`unsubscribe`/`ensureStreamingPlaceholder`/`stopStreaming`；`switchSession`/`createSession` 挂订阅；`watch(connected)` 改语义；删 `streamTimeout` 机制 | 无重复 subscribe；切会话不泄漏订阅；idle 不误 reload |
| 2 | 删 `loadHistory` 的 connectStream 分支 + `reuseExistingStreaming`/`subscribeOnly`/`forceNotRunning` 选项；删 `sendMessageNow` 的 `connectStream`；删 done/error/replay_done 的 `disconnectStream`（订阅常驻）；简化 `syncSessionOnReconnect` | 单条发送、队列、运行中刷新、连续多轮对话全部正常 |
| 3 | **后端**：`pending_events` 扩展存 `user_message` + 广播点 write-ahead | 断线窗口 user_message 可补偿 |
| 4 | **前端**：`fetchPendingEvents` 扩展处理 `user_message`；`LAST_SEEN_KEY` 游标对 `user_message` 也更新 | 断线重连后补回漏掉的消息 |
| 5 | 全量回归：`go test ./...` + `npm test` + `vue-tsc` + 手动多端验证 | 全部通过 |

每个阶段有独立验证点，风险从低到高。阶段 1 与阶段 3/4 相互独立，可并行。

---

## 风险清单

| 风险 | 等级 | 缓解 |
|---|---|---|
| 重复 subscribe → OnSubscribe 重复重放 ACP 状态 | 高 | `subscribedSessionId` 去重，只发一次 subscribe |
| 删 streamTimeout 后 backend 静默死亡无兜底 | 中 | `useGlobalEvents` 60s/90s 心跳强制重连已覆盖 |
| Android 原生 WS 断线导致 `HasDisconnectedClients()` 误判 | 中 | 原生层 client_id 不同于 WebView，断线时全局检查返回 true → 所有 user_message 都写入 pending_events。可考虑改为 per-session 检查（只检查订阅了该 session 的客户端断线状态），或忽略非 chat_stream 订阅者的断线状态 |
| 订阅常驻后非当前会话事件的空耗 | 低 | 用户切换会话后，旧会话已被 unsubscribe；但在同一会话内，来自其他设备的 mode_update/usage_update 等事件仍会到达（sessionChanged() guard 过滤），移动端后台可能增加 CPU 开销。可考虑检测大量被丢弃事件时自动 unsubscribe |
| pending_events 存 `user_message` 导致表增长 | 低 | 复用现有 `CleanupPendingEvents`（expires_at + 行数上限）；所有客户端在线时 `HasDisconnectedClients()` 返回 false，零写入 |
| 断线补偿只补 `user_message` 不补流式 content | 低 | 流式内容最终被 loadHistory/rebuildFromDb 收敛；`completed hasNewMessages` 兜底 |
| `_remote` 随机 id 兜底分支退化为防御路径后测试覆盖不足 | 低 | 保留现有 `_remote` 测试用例 |
| 测试改写量大（订阅相关全删全改） | 中 | 按阶段 1-7 分批，每阶段独立验证 |

---

## 不做的事（YAGNI）

- **不做**队列消息逻辑重构（queueId 三通道、rebuildFromDb、_remote 全部保留）——它们与订阅时机无关，是两通道协调的正常机制
- **不做** `hasNewMessages=true` 后端增强（`TrySetSessionRunning` 带标记）——订阅常驻后 idle 客户端也能收到实时事件，此增强成为纯兜底，收益低，暂缓
- **不做** `queuedCount` 实时计数改造——现有 `!m.queueId` 过滤已免疫快照漂移，收益小
- **不做** 纯推送（无乐观气泡）——用户点发送的即时响应是移动端核心体验，乐观气泡保留
