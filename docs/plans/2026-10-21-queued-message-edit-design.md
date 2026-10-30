# 排队消息编辑（撤回编辑）设计

日期：2026-10-21
状态：**已被「原地编辑」方案取代（2026-10-31）——本文档保留决策轨迹，不要按它实施**

> ## 取代说明（2026-10-31）
>
> 本设计选择「撤回编辑」：点编辑 → 删除队列行 → 正文与附件灌回输入框 → 用户改完
> 自己重发。它把「复制粘贴」自动化了，但**没有消掉「再发过去」这一步**，而且更糟：
> 撤回后重发会排到**队尾**。
>
> 需求原话是「排队要排十几二十分钟，等待期间需求变了」。对一个排在第三、还要等
> 二十分钟的消息，撤回编辑等于让用户把这段等待重来一遍 —— 等于没解决。
>
> 实际实施的是**原地编辑**（PATCH /api/ai/queue 改 content）：
>
> | 维度 | 撤回编辑（本文档） | 原地编辑（已实施） |
> |---|---|---|
> | 消掉「重发」 | ❌ 仍需重发 | ✅ 改完即生效 |
> | 队列位置 | ❌ 排到队尾 | ✅ 不变（`id ASC` 不动） |
> | 附件可编辑 | ✅ | ❌（明确不做，换附件仍走取消重发） |
> | 多设备一致 | 天然 | 天然（复用 `queue_added` upsert） |
> | 后端改动 | ~15 行（仅修既有 bug） | ~90 行（新端点 + 409 + 广播） |
>
> 决定性理由：附件不可编辑是可以接受的（需求变化通常改的是文字要求，不是那张图），
> 而**丢失队列位置**直接摧毁了「排队十几分钟」这个场景本身。附件编辑需要队列行内嵌
> 文件选择器，移动端布局必炸，成本约为文字编辑的 4 倍，留作后续。
>
> 实现位置：`service.UpdateQueuedMessageContent`、`handler.handleQueueUpdate`、
> `web/src/composables/useSessionManager.ts` 的 `editQueuedMessage`、
> `QueuedMessageBar.vue` 的 `data-action="queue-edit"`。
>
> 本文档第 4 条指出的既有 bug（`CancelQueuedMessage` 不检查 `RowsAffected`）**仍然
> 存在且尚未修复**，与编辑功能无关，另行处理。

## 背景

发送消息时若当前回合仍在运行，消息进入队列（`queued_messages` 表），等本轮结束后
各自作为独立回合发出。队列能力目前已经相当完整：

| 能力 | 状态 | 实现 |
|---|---|---|
| 多条排队 | ✅ | `queued_messages` 表，索引 `(session_id, id)`（`database.go:644-655`） |
| 合并 | ✅ | `MergeQueuedMessages`（`queue_store.go:479`）：正文 `\n\n` 拼接、附件按 `fileEntryKey` 去重 |
| 单条删除 | ✅ | `CancelQueuedMessage`（`queue_store.go:387`） |
| 插话 / 中断 | ✅ | `/api/ai/queue/inject`、`/api/ai/queue/interrupt` |
| 编辑引用批注 | ✅ | `UpdateQueuedMessageQuoteNote`（`queue_store.go:586`） |
| **编辑正文 / 附件** | ❌ | 无端点、无入口 |

需求（用户原话）：*「支持合并，但是现在界面不支持修改在队列中的这个消息。你看一下这个特性
是否需要支持？」*

## 关键事实（已核实）

1. **「排队中的消息是可变的」已经是既有先例**。`UpdateQueuedMessageQuoteNote` 专门为
   「引用批注在消息还没发出去时被编辑」而写，`handler/chat.go:1172-1178` 按
   `req.MessageID <= 0 && req.QueueID != ""` 分流到队列表。所以本设计不是新概念，
   只是把这个先例推广到正文与附件。

2. **队列是唯一能安全编辑消息的窗口**。排队中的消息没有 `chat_history` 行、AI 从未读过；
   一旦 drain 成 `chat_history` 行，编辑就需要 rewind/truncate + 重启 AI 会话 + 处理下游
   消息，成本高一个数量级。本设计因此**只在队列窗口内提供编辑**。

3. **入队不幂等，靠 `queue_id` 唯一索引**。`AddQueuedMessage`（`queue_store.go:102`）是
   纯 INSERT，重复 `queueId` 由 `idx_queued_identity`（`database.go:655`）拒绝。drain 时
   `claimQueuedRowTx` 在事务内 SELECT + DELETE（`queue_store.go:165`），写锁串行化，
   两条并发 claim 不会拿到同一行。

4. **`CancelQueuedMessage` 不报告是否真的删掉了**（`queue_store.go:387`）：

```go
func CancelQueuedMessage(sessionID, queueID string) error {
	_, err := store.WriteExec("DELETE FROM queued_messages WHERE session_id = ? AND queue_id = ?", sessionID, queueID)
	return err
}
```

   没有 `RowsAffected` 检查。`handleQueueDelete`（`queue.go:525-542`）因此**无条件**返回
   `{"ok": true}` 并无条件广播 `queue_cancel`。前端 `handleRemovePending`
   （`useSessionManager.ts:170-181`）拿到 2xx 后**无条件**本地删除。

   **这是一个既有 bug**：若 drain 在「渲染队列条」与「点击」之间取走了该行，DELETE 静默
   无操作，但前端仍把它从队列里移除了 —— 用户以为取消了，消息其实已经发出。

   今天影响有限（只是 UI 短暂不一致，`queue_drain` 随后会补上真实气泡）。但**撤回编辑
   会把它放大成重复发送**：消息已发出 + 输入框里还有一份 → 用户再发一次。所以本设计
   必须一并修掉它。

5. **输入框已有完整的「预填」原语**：`ChatInputBar` 的 `prefillInput(text)`
   （`ChatInputBar.vue:2155-2164`）会替换正文、写草稿（`setChatDraft`）、聚焦 textarea。
   rewind（回溯）走的就是它（`ChatPanelContent.vue:1597-1600`）。

6. **`useChatContext` 是模块级单例**：`attachedFiles` 等 ref 定义在模块作用域
   （`useChatContext.ts:65`），`ChatPanelContent`（`:1100`）与 `ChatInputBar`（`:1542`）
   调用 `useChatContext()` 拿到的是**同一批 ref**。因此从任意一侧写 `attachedFiles.value`
   都直接生效，不需要新的 prop/emit 通道。

   **注意两者的取用方式不同**：`ChatPanelContent` 是**按名解构**
   （`const { stagedQuotes, removeStagedQuote, … } = useChatContext()`，`:1100`），
   而 `ChatInputBar` 保留对象（`const chatContext = useChatContext()`，`:1542`）。
   新增的函数若要在 `ChatPanelContent` 里用，必须加进那行解构，不能写 `chatContext.xxx`。

7. **队列条目已携带完整 `FileEntry[]`**（`useMessageQueue.ts:103` 的 `normalizeFiles`），
   撤回时无需回服务端二次读取。

8. **`queue_added` 是 upsert 语义**（前端 `addQueued` 按 `queueId` 找到就替换，
   `useMessageQueue.ts:218-238`）。这使「原地编辑 + 广播」在协议层是可行的，
   但本设计**不采用**（见「为什么选撤回编辑」）。

9. **既有 `stageAttachmentIntoDraft` 不能直接复用**（`useChatContext.ts:473`）：它写的是
   **别的会话的草稿快照**（`ensureAttachmentDraft`），不是当前 live 的 `attachedFiles`。
   撤回要的是「立刻出现在眼前的附件条」，因此需要一个写 live ref 的新函数（见第 7 条改动）。

10. **队列文案集中在 `chat.pending.*`**（`web/src/i18n/locales/zh.ts:1016-1040`）。

## 决策汇总

| # | 维度 | 决策 |
|---|---|---|
| 1 | 交互形态 | **撤回编辑**：点「编辑」→ 删除队列行 → 正文 + 附件灌回输入框 → 用户改完自己重发 |
| 2 | 为什么不原地编辑 | 见「为什么选撤回编辑」——附件是核心痛点，原地编辑改不了 |
| 3 | 新端点 | **无**。复用 `DELETE /api/ai/queue?queueId=` |
| 4 | 新 WS 事件 | **无**。成功时沿用既有 `queue_cancel` |
| 5 | 新 DB 迁移 | **无** |
| 6 | 必须先修的 bug | `CancelQueuedMessage` 返回是否真的删掉；`handleQueueDelete` 在未删掉时返回 **409** 且**不广播** |
| 7 | 输入框已有内容时 | **追加**（空则替换）。不弹确认框——两个原语都已存在，追加最不破坏 |
| 8 | 顺序 | **不保证**。撤回非队首条目后重发会排到队尾；文案提示，不做复杂排序 |
| 9 | 多设备 | 撤回设备的输入框被填充；其他设备只看到该条目消失（与今天的「删除」一致） |
| 10 | 消息已发出时 | 409 `QueueAlreadyDrained` → 提示「已发出，无法撤回」，**且不清空/不改动输入框** |

## 为什么选撤回编辑（而不是原地编辑）

| 维度 | 撤回编辑 | 原地编辑 |
|---|---|---|
| 附件可编辑 | ✅ 完全（走既有附件条） | ❌ 只能改正文 |
| 引用 / @ 成员卡 | ✅ 一并恢复 | ❌ 改不了 |
| 后端改动 | 仅修既有 bug（约 15 行） | 新端点 + 校验 + 广播（约 120 行） |
| 前端改动 | 队列行加按钮 + 撤回逻辑（约 40 行） | 行内 textarea + 保存/取消 + 并发（约 150 行） |
| 新 WS 事件 | 无 | 需要（或复用 `queue_added` upsert） |
| 顺序保持 | ❌ | ✅ |
| 多设备一致 | 天然（DELETE 走 `queue_cancel`） | 需处理 last-write-wins |
| 移动端 | 队列条保持紧凑列表 | 行内 textarea 破坏布局 |

**决定性理由是附件**：队列编辑的真实价值场景是「带附件 / 引用的长消息写错了」——
短消息打错一个字，删除重打就够了。原地编辑恰好解决不了这个场景，而撤回编辑靠复用
既有附件条**免费获得**它。

代价是顺序不保证。但撤回的常见对象是**队首**（下一条要发的），此时顺序无损；
非队首的情况用户通常本来就想改完再发，排到队尾是可接受的。

## 后端改动

### `internal/service/queue_store.go`

1. **`CancelQueuedMessage` 改为报告是否真的删除**（当前 `:387`）：

```go
// CancelQueuedMessage deletes one queued message and reports whether a row was
// actually removed.
//
// The bool is load-bearing, not informational: the drain loop may claim the row
// between the client rendering the queue and the user acting on it. A DELETE
// that matched nothing must NOT be reported as success — the caller (and the
// user) would conclude the message was cancelled while it had in fact already
// been sent. Returning only an error made that case indistinguishable from a
// real cancellation, because SQL DELETE matching zero rows is not an error.
func CancelQueuedMessage(sessionID, queueID string) (deleted bool, err error) {
	res, err := store.WriteExec(
		"DELETE FROM queued_messages WHERE session_id = ? AND queue_id = ?",
		sessionID, queueID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
```

   调用方只有 `handler/queue.go:526`（已核实），无其他生产路径依赖旧签名。

### `internal/handler/queue.go`

2. **`handleQueueDelete`（`:523-543`）区分「删掉了」与「已经不在队列」**：

```go
queueID := r.URL.Query().Get("queueId")
if queueID != "" {
	deleted, err := service.CancelQueuedMessage(sessionID, queueID)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "QueueDeleteFailed")
		return
	}
	if !deleted {
		// The row is gone: the drain loop claimed it (or another device already
		// cancelled it). Either way the message is NOT cancellable any more.
		// Deliberately NO queue_cancel broadcast: the entry is already absent
		// server-side, and broadcasting a cancel for a message that became a
		// real turn would make other devices drop a bubble the timeline is
		// about to render. The drained case is covered by queue_drain.
		writeLocalizedErrorf(w, r, http.StatusConflict, "QueueAlreadyDrained")
		return
	}
	ws.EmitToSession(sessionID, ai.StreamEvent{
		Type: "queue_cancel",
		QueueEvent: &ai.QueueEventData{
			SessionID: sessionID,
			QueueIDs:  []string{queueID},
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return
}
```

   > **为什么是 409 而不是 200 + `{cancelled:false}`**：调用方（前端）必须能用一个
   > 通用的 `resp.ok` 判断「有没有生效」。若返回 200，所有既有调用点都得改成解析 body，
   > 而「删除」这个动作在 4xx 时保持队列不动是更安全的默认（失败即不改本地状态）。
   > 这也与 `/inject`、`/interrupt` 已有的 409 语义一致。

3. **「清空全部」路径（`:553-570`）不变**。`ClearQueuedMessages` 是全清，无需
   RowsAffected —— 「队列本来就是空的」和「清空了」对用户是同一件事。

### `internal/i18n/locales/active.{zh,en}.yaml`

4. 新增 1 个键（加入 `internal/i18n/i18n_test.go` 的键清单）：

```yaml
# zh
QueueAlreadyDrained: 这条消息已经发出，无法再撤回
```

```yaml
# en
QueueAlreadyDrained: This message has already been sent and can no longer be recalled
```

## API

| 方法 | 路径 | 变化 |
|---|---|---|
| `DELETE` | `/api/ai/queue?session_id=&queueId=` | **契约变更**：目标行已不存在时由 `200 {ok:true}` 改为 **409 `QueueAlreadyDrained`**，且不再广播 `queue_cancel` |

无新增端点。必须同步 `internal/api/openapi.yaml`（`/api/ai/queue` 定义在 `:2235`）：
在 `delete` 的 responses 里补 409 说明，并更新描述文字（「取消一条排队消息；
若该消息已被 drain 取走，返回 409」）。

## 前端改动

### `web/src/composables/useSessionManager.ts`

5. **`handleRemovePending`（`:170-181`）改为一律以服务端结果为准**：

```ts
/** Remove a queued message by its queueId.
 *
 *  The local entry is dropped ONLY when the server confirms a row was deleted.
 *  A 409 means the drain loop already claimed it (the message is now a real
 *  turn) — dropping it locally would tell the user "cancelled" about a message
 *  that was in fact sent. */
async function handleRemovePending(queueId: string): Promise<boolean> {
  if (!queueId) return false
  const sessionId = identity.currentSessionId.value
  try {
    const resp = await fetch(
      `/api/ai/queue?session_id=${encodeURIComponent(sessionId)}&queueId=${encodeURIComponent(queueId)}`,
      { method: 'DELETE' },
    )
    if (resp.status === 409) {
      // Already drained. Drop it locally too — the timeline will show it as a
      // real message, so keeping it in the queue panel would be a phantom.
      removeQueued(sessionId, queueId)
      toast.show(gt('chat.pending.alreadySent'), { icon: 'warning', type: 'info' })
      return false
    }
    if (!resp.ok) throw new Error(`delete failed: ${resp.status}`)
    removeQueued(sessionId, queueId)
    return true
  } catch {
    toast.show(gt('session.removeFailed'), { icon: 'warning', type: 'error' })
    return false
  }
}
```

   > 409 时**仍然** `removeQueued`：那条消息已经变成真实气泡，留在队列面板是幽灵条目。
   > 与「撤回编辑」的 409 处理不同（后者**必须不动输入框**），因为两者目标不同：
   > 删除只需让本地与事实一致，撤回还要决定是否把内容搬进输入框。

6. **新增 `recallQueuedMessage`**（紧邻 `handleRemovePending`）：

```ts
/**
 * Recall a queued message back into the input box for editing.
 *
 * Deletes the queue row first, then hands the caller the payload to inject.
 * Ordering matters: if the delete fails (409 = already drained) the caller must
 * NOT touch the input — otherwise the text would be duplicated when the user
 * sends it again, which is exactly the double-send this ordering prevents.
 *
 * Returns the recalled entry, or null when it could not be recalled.
 */
async function recallQueuedMessage(queueId: string): Promise<QueuedMessage | null> {
  if (!queueId) return null
  const sessionId = identity.currentSessionId.value
  const entry = getQueue(sessionId).find(m => m.queueId === queueId)
  if (!entry) return null

  const deleted = await handleRemovePending(queueId)   // 含 409 与网络错误的全部处理
  if (!deleted) return null
  return entry
}
```

   > 复用 `handleRemovePending` 而不是再写一遍 DELETE：两条路径的失败处理必须一致，
   > 而 409 的两种后果（本地清掉 + 提示）已经在第 5 条里定义好了。

### `web/src/composables/useChatContext.ts`

7. **新增 `addFileEntry`**：队列条目的 `files` 是**完整 `FileEntry[]`**（可能含
   `kind: 'url'`、引用条目、行范围），现有的 `addAttachedFile(path, isDir, ...)` /
   `addUrlAttachment` 都构造不出这些形状，必须能按原样注入。

   **不能复用 `stageAttachmentIntoDraft`**（`:473`）：它写的是**指定会话的草稿快照**
   （`ensureAttachmentDraft`），撤回需要的是**当前 live 的附件条**立即出现。

```ts
/**
 * Attach a fully-formed FileEntry to the LIVE input (queue recall, …).
 *
 * Distinct from stageAttachmentIntoDraft, which writes another session's draft
 * snapshot: a recall must show up in the attachment bar the user is looking at
 * right now.
 *
 * Dedupes with the same identity rule as the rest of this module, so recalling a
 * message whose attachment is already staged does not create a duplicate chip.
 * Returns whether it was added.
 */
function addFileEntry(entry: FileEntry): boolean {
  if (!entry) return false
  if (attachedFiles.value.some(f => sameEntry(f, entry))) return false
  attachedFiles.value.push({ ...entry })
  return true
}
```

   `sameEntry` 已存在于模块内（`:152`），直接复用。新函数需加入
   `useChatContext()` 的返回对象（`:497`）与 `ChatPanelContent` 的解构行（`:1100`）。

### `web/src/components/chat/QueuedMessageBar.vue`

8. 每个展开行在「插话」与「删除」之间插入「编辑」按钮（`Pencil`，`lucide-vue-next`）：

```
[插话] [编辑] [×]
```

   - 与既有按钮同款 `:disabled="busy === msg.queueId"`。
   - `title` 用 `t('chat.pending.editHint')`（「把这条消息撤回输入框修改」）。
   - 收起态**不提供**：收起态只有一个作用于队首的「插话」按钮，加编辑会让那一行过挤；
     展开后每条都有，代价是一次点击。
   - 新增 emit `edit`。

### `web/src/components/chat/ChatPanelContent.vue`

9. 接线 `@edit="handleEditPending"`，新增：

```ts
async function handleEditPending(queueId) {
  if (!queueId || editBusy.value) return
  editBusy.value = true
  try {
    const entry = await manager.recallQueuedMessage(queueId)
    if (!entry) return                       // 409 / 网络错误：输入框保持原样
    applyRecalledEntry(entry)
  } finally {
    editBusy.value = false
  }
}
```

10. **`applyRecalledEntry`** 负责把内容放进输入框（`chatContext` 与输入框共享同一批 ref）：

```ts
function applyRecalledEntry(entry) {
  const bar = inputBarRef.value
  if (!bar) return
  // 正文：输入框为空则替换，否则追加（不弹确认框，两个原语都已存在）。
  const existing = bar.inputText?.trim() || ''
  if (existing) {
    bar.injectToInput(entry.text)
  } else {
    bar.prefillInput(entry.text)      // 同时写草稿 + 聚焦
  }
  // 附件：逐条按原样注入，sameEntry 去重。
  for (const f of entry.files || []) chatContext.addFileEntry(f)
}
```

    > **注意**：`prefillInput` 在空输入时已负责写草稿；`injectToInput` 不写草稿，
    > 因此追加分支需要显式 `bar.saveDraft()`，否则切走会话后草稿丢失。实现时补上。

11. `editBusy` 是独立 ref（不与 `pendingActionBusy` / `mergeBusy` 复用）：
    三者作用于不同按钮，复用一个 ref 会让「编辑中」把其他行的插话按钮也禁用。

### i18n（`web/src/i18n/locales/{zh,en}.ts`，`chat.pending` 段）

12. 新增 3 个键：

```ts
edit: '编辑',
editHint: '把这条消息撤回输入框修改',
alreadySent: '这条消息已经发出，无法再撤回',
```

## 测试

### Go

| 测试 | 断言 |
|---|---|
| `TestCancelQueuedMessage_ReportsDeleted` | 行存在 → `deleted == true` 且行已消失 |
| `TestCancelQueuedMessage_AlreadyGoneReportsFalse` | 不存在的 queueId → `deleted == false`、`err == nil`（不是错误） |
| `TestCancelQueuedMessage_WrongSessionReportsFalse` | 别的 session 的 queueId → `deleted == false`，**且不误删** |
| `TestQueueDelete_AlreadyDrainedIs409` | 已 drain 的 queueId → 409 `QueueAlreadyDrained` |
| `TestQueueDelete_AlreadyDrainedEmitsNoCancel` | 同上场景**不**广播 `queue_cancel`（订阅方收不到该事件） |
| `TestQueueDelete_SuccessEmitsCancel` | 正常删除 → 200 + 广播 `queue_cancel` 含该 queueId |
| `TestI18nKeys_QueueAlreadyDrained` | 新键中英双语可解析 |

### 前端

| 测试 | 断言 |
|---|---|
| `useSessionManager` 单测 | `handleRemovePending` 收到 200 → 本地移除、返回 true |
| `useSessionManager` 单测 | 收到 409 → 本地**仍移除**（避免幽灵条目）、提示「已发出」、返回 false |
| `useSessionManager` 单测 | 收到 500 → 本地**不移除**、提示失败、返回 false |
| `useSessionManager` 单测 | `recallQueuedMessage` 在 409 时返回 `null`（调用方据此**不动输入框**） |
| `QueuedMessageBar` 单测 | 展开态每行都有「编辑」按钮；点击 emit `edit` 携带 queueId；`busy` 时禁用 |
| `ChatPanelContent` 单测 | 撤回成功 → 输入框正文 = 原 text、附件条含原附件 |
| `ChatPanelContent` 单测 | 输入框非空时撤回 → **追加**而非覆盖 |
| `ChatPanelContent` 单测 | 撤回 409 → 输入框**完全不变**（防重复发送的核心断言） |
| `useChatContext` 单测 | `addFileEntry` 注入含 `kind:'url'` 的条目后形状完整；重复注入同一 entry 不产生第二条 |

### E2E

在既有 `e2e/specs/queued-messages.spec.ts` 补用例（不新建 spec —— 队列已有专属 spec，
拆开会让"排队行为"分散在两处）：

1. 运行中连发两条 → 展开队列 → 点第一条「编辑」→ 队列剩一条、输入框含第一条正文与附件
2. 改完重发 → 队列变为两条（顺序：原第二条在前、改后的一条在后）——**钉住顺序不保证**
3. 让本轮结束、第一条已 drain 后再点「编辑」→ 出现「已发出」提示、**输入框未被填充**

用 `data-action` / `data-queue-id` 定位，不用位置索引。

## 文档同步

- `docs/spec/features/attachments-and-share.md` 或队列所在 spec：补「撤回编辑」条目
- `docs/spec/core/chat-flow.md` 的队列章节：补 409 `QueueAlreadyDrained` 语义
- `internal/api/openapi.yaml`：`/api/ai/queue` 的 DELETE 补 409
- `docs/user-guide/`：排队消息的操作说明（可选）

## 不做的事

| 不做 | 原因 |
|---|---|
| **原地编辑**（队列行内 textarea） | 改不了附件与引用，而附件正是编辑需求的主要场景；成本约为撤回编辑的 4 倍 |
| 保证撤回后的**顺序** | 需要「插入到原位置」的排序语义，而队列只有 `id ASC` 一个序。撤回队首时天然无损，非队首排到队尾是可接受的 |
| 撤回时**自动重发** | 撤回的目的是修改，自动重发等于没改。用户必须显式再发一次 |
| 撤回时弹**确认框** | 撤回可逆（内容进了输入框，可再发也可丢弃），且 `prefillInput` 已写草稿。加确认框会让轻量操作变重 |
| 把正文草稿**落 localStorage** | `chatDraftStore` 是内存态，刷新即丢。这是独立议题（影响所有草稿，不止撤回） |
| 在**收起态**也提供编辑 | 收起态只有一个作用于队首的按钮，加编辑会挤爆该行；展开后每条都有 |
| 撤回时**同步填充其他设备**的输入框 | 撤回是单设备的本地动作；其他设备只看到条目消失（与今天的删除一致）。跨设备搬内容需要新的广播事件，收益不足 |

## 风险与已接受的取舍

1. **409 契约变更影响所有 `DELETE /api/ai/queue` 调用方**：已核实生产调用方只有
   `handleRemovePending` 一处（`CancelQueuedMessage` 亦只有 `handler/queue.go:526` 一个调用点）。
   若有外部脚本依赖「总是 200」，会开始收到 409 —— 但那本来就是错误语义。
   **接受**，并在 OpenAPI 里写明。
2. **撤回即删除**：用户点「编辑」后直接关页面，内容只在内存草稿里，刷新即丢。
   内容在输入框可见，属于主动放弃。**接受**（与「不做的事」最后一条同源）。
3. **顺序不保证**：非队首撤回后重发排到队尾。文案提示 + E2E 钉住实际行为，
   不让它成为"有时灵有时不灵"的隐性契约。**接受**。
4. **附件去重依赖 `sameEntry`**：撤回的附件若已在输入框里（用户先手动挂了同一个文件），
   不会重复。这是正确行为，但意味着「撤回后附件数」可能少于原消息 —— 需在测试里
   明确断言去重，避免被误判为丢失。
5. **`editBusy` 与 `pendingActionBusy` 分开**：若实现时图省事复用，会让「编辑某一行」
   把其他行的插话按钮一起禁用。这是 UI 层的静默退化，无报错，需靠单测钉住。
