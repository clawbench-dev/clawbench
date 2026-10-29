# CodeBuddy chunk 去重：照搬上游权威实现（2026-10-29）

## 结论

ClawBench 对 CodeBuddy 的模型输出帧做去重，**原样移植上游的 `chunkPublicationKey`
与其消费逻辑**。只对 `codebuddy` 生效；其他后端没有这个问题，不走这条路径。

## 之前的两次错误

### 错误一：键抄错了

ClawBench 旧实现（2026-10-22 引入）的键是：

```
sessionId + variant + messageId + timestamp + memberEvent + text
```

上游（`dist/codebuddy.js`）的键是：

```js
JSON.stringify([sessionId, sessionUpdate, messageId, timestamp,
                _meta["codebuddy.ai/conversationRequestId"] ?? _meta["codebuddy.ai/requestId"],
                _meta["codebuddy.ai/parentToolCallId"],
                extractTextContent(content)])
```

第 5 个元素是 **requestId**（旧实现错用 memberEvent），第 6 个是 **parentToolCallId**
（旧实现完全没有）。更关键的是 `messageId` 上游优先取**顶层** `update.messageId`，
旧实现只看 `_meta`。

### 错误二：误以为上游 ACP 路径没有去重

我曾据此完全删掉去重（提交 `3533f953a`）。判断依据是：`chunkPublicationKey` 只在
`publishSessionUpdateToBus` 里被调用，而普通 ACP 客户端走 `sendToConnection`。
这个观察本身没错，但结论错了——**上游的去重方案是对的，应该移植，而不是放弃**。

## 真实帧验证

探针抓取 `codebuddy --acp` 的 82 个 `agent_message_chunk`（prompt 要求输出两张表）：

- 按上游键分组：**0 个重复**
- 同一 text 出现多次的情况：`' |'` 21 次、`' '` 16 次、`'|'` 8 次、`' ---'` 7 次
- 但这些重复帧的 `timestamp` **各不相同**（21 次 `' |'` 对应 21 个不同 timestamp）

所以上游键能正确区分「重试」与「合法的重复片段」：

| 情形 | timestamp | 键 | 结果 |
|---|---|---|---|
| 重试（同一 update 再发一次） | 相同 | 相同 | 丢弃 |
| 逐字符流式中的重复片段 | 不同 | 不同 | 保留 |

旧实现丢掉了 timestamp 的区分力（或者说，误以为 message 级 timestamp 是常量），
所以把合法重复也删了。

## 实现

`internal/ai/acp_dup_chunk.go` 逐元素移植：

```go
func chunkIdentity(sessionID, variant, topLevelMessageID string, meta map[string]any, text string) string {
    if !volatileSessionUpdate(variant) { return "" }
    ts := metaString(meta[metaKeyCodeBuddyTimestamp])
    msgID := topLevelMessageID
    if msgID == "" { msgID = metaString(meta[metaKeyCodeBuddyMessageID]) }
    if ts == "" || msgID == "" { return "" }
    requestID := metaString(meta[metaKeyCodeBuddyConversationRequestID])
    if requestID == "" { requestID = metaString(meta[metaKeyCodeBuddyRequestID]) }
    parentID := metaString(meta[metaKeyCodeBuddyParentToolCallID])
    parts, _ := json.Marshal([]string{sessionID, variant, msgID, ts, requestID, parentID, text})
    return string(parts)
}
```

窗口与容量也照搬：

- `chunkDedupWindow = 5 * time.Second`（上游 `eQ = 5e3`）
- 窗口从**被接受**的投递起算，被丢弃的重复不刷新（上游只在 accept 时写 map）
- 满 `8192` 条时先剪掉过期项，而非整体清空（上游 `eS.size > 8192` 分支）

三条不变量：

1. `timestamp` 与 `messageId` **缺一不可**（上游 `if (ts && msgId)`）
2. 只有 volatile 变体参与（`agent_message_chunk` / `agent_thought_chunk` /
   `user_message_chunk`）
3. `messageId` 优先取顶层字段

## 生效范围

`chunkDedupEnabled` 只对 `codebuddy` 返回 true。全库统计（`chat_history` 按 backend）：

| 后端 | 消息数 | 畸形表格分隔行 |
|---|---|---|
| codebuddy | 14929 | 130 |
| opencode | 2559 | 0 |
| claude | 54 | 0 |
| pi | 79 | 0 |
| codex | 14 | 0 |

其他后端零畸形，不接这条路径。

## 测试

`internal/ai/acp_dup_chunk_test.go`：

**键构造**
- `RequiresTimestampAndMessageID` — 缺一不可
- `PrefersTopLevelMessageID` — 顶层优先于 `_meta`
- `RequestIDPrecedence` — conversationRequestId 优先于 requestId
- `DistinguishesEveryComponent` — 7 个元素各自参与
- `NonVolatileVariantHasNoKey` — tool_call / plan 等不参与

**窗口**
- `DropsRepeatInsideWindow` / `WindowMeasuredFromAcceptedDelivery` /
  `EmptyIdentityNeverDrops` / `NilConnNeverDrops` / `ResetChunkDedup_*` /
  `OverflowPrunesExpired`

**端到端（真实映射路径）**
- `TableDelimiterRowsSurvive` — 按真实帧序列（`'|' ' ---' ' |' ' ---' ' |'`，
  各带独立 timestamp）投递，断言整行到达
- `MultipleTablesKeepTheirDelimiterRows` — 三张表，按整行精确计数
- `ImmediateRetryIsDropped` — 同帧重发 3 次只留 1 份
- `ThoughtChunkRetryIsDropped` / `DedupDoesNotApplyToOtherBackends` /
  `KeepsChunksWithoutIdentity`

**变异验证（双向）**

| 变异 | 结果 |
|---|---|
| 从键里去掉 `timestamp`（复现生产 bug） | 3 个测试转红，含两个表格回归 |
| 去掉时间窗 | `WindowMeasuredFromAcceptedDelivery` 转红 |

## 遗留

已写入 DB 的畸形分隔行不会自愈，需单独迁移补回被删的 `|---|---|`。
