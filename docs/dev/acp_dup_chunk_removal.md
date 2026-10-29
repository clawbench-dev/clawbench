# 移除 ACP chunk 去重（2026-10-29）

## 结论

ClawBench 曾对 CodeBuddy 的模型输出帧做客户端去重（`internal/ai/acp_dup_chunk.go`），
按 `sessionId + variant + messageId + timestamp + memberEvent + text` 判重。
**该机制已完全移除**，理由是它在逐字符流式输出下会删掉真实内容，且上游并不需要它。

## 现象

助手消息里的 Markdown 表格整块渲染成普通段落。原因是分隔行的列数比表头少 2：

```
表头  : | 段 | 幕 | 旁白窗口 | 该幕稳定区 | 判定 |     ← 5 列
分隔行: |---|---|---|                                    ← 只剩 3 列
```

marked 的 table tokenizer 要求 `header.length === delim.length`，不等就不认表格。

全库对账 CodeBuddy 原始 jsonl（`~/.codebuddy/projects/*/*.jsonl`）：**16 条助手消息各丢失
恰好一个 `|---|---|`（8 字节）**，全部是分隔行末尾的两格。受影响的不止历史数据——修复尝试
部署后仍持续产生新的畸形表格。

## 真正的机制

用真实 ACP 探针抓帧（`codebuddy --acp`，prompt 要求输出两张表），得到 133 个
`agent_message_chunk`：

```
帧 0:  text='|'      ts=...47.908Z
帧 7:  text='|'      ts=...47.940Z
帧 8:  text=' ---'   ts=...47.950Z
帧 9:  text=' |'     ts=...47.951Z
帧 10: text=' ---'   ts=...47.952Z
帧 11: text=' |'     ts=...47.952Z     ← 与上一帧同毫秒
```

**CodeBuddy 是逐字符推送的。** 一条消息里：

- `' |'` 出现 21 次
- `' '` 出现 16 次
- `'|'` 出现 8 次
- `' ---'` 出现 7 次

一个 `|---|---|` 分隔行由 `'|' ' ---' ' |' ' ---' ' |'` 组成，其中 `' ---'` 和 `' |'`
在同一毫秒内相邻出现。任何以 text 为键的去重都会删掉这些重复片段，分隔行因此被啃掉一块。

### 为什么 5 秒时间窗救不了

曾经尝试对齐上游的 5 秒窗口（`chunkDedupWindow`）。无效：重复片段的间隔是 **1 毫秒**，
本来就在窗口内。加窗口只会让真正的重试保护失效，不解决误删。

### 为什么不能照抄上游

上游 `dist/codebuddy.js` 的 `chunkPublicationKey` 确实是同一个键：

```js
JSON.stringify([sessionId, sessionUpdate, messageId, timestamp,
                _meta["codebuddy.ai/conversationRequestId"] ?? _meta["codebuddy.ai/requestId"],
                _meta["codebuddy.ai/parentToolCallId"],
                extractTextContent(content)])
```

但它只在**内部总线**上用（`publishSessionUpdateToBus`），服务于 WebUI 广播。
普通 ACP 客户端走的是 `sendToConnection` / `trySendToConnection`：

```js
async trySendToConnection(eA,el,ec,eu,ed=!0){
  let eu=eA.getConnection();
  ...
  return await Promise.race([eu.sessionUpdate({sessionId:el,update:eh}), eg]), !0
}
```

**没有任何去重。** 所以上游的 ACP 输出不会丢内容。

更关键的是，上游能区分「重试」和「合法的重复片段」，靠的是
`getInFlightTurnTracker().apply()` 返回的 **offset**：

```js
accumulate(eA, el, ec) {
  case "agent_message_chunk": {
    let ep = eA.assistantText.length;   // 累加前的长度 = 该 chunk 在消息中的偏移
    eA.assistantText += el;
    return ep;
  }
}
```

重试时 offset 不变（累加器没前进），合法的新片段 offset 会推后。**这个 offset 不在 ACP
wire 上**，客户端拿不到，因此无法复现上游那套判断。

## 其他后端

全库统计（`chat_history`，按 backend 分组）：

| 后端 | 消息数 | 表格分隔行 | 畸形 | 畸形消息 |
|---|---|---|---|---|
| **codebuddy** | 14929 | 8466 | **130** | **66** |
| opencode | 2559 | 149 | 0 | 0 |
| claude | 54 | 24 | 0 | 0 |
| pi | 79 | 32 | 0 | 0 |
| codex | 14 | 1 | 0 | 0 |
| qoder | 2 | 0 | 0 | 0 |
| commandcode | 2 | 0 | 0 | 0 |

只有 codebuddy 有问题，而它正是唯一开启去重的后端（`chunkDedupEnabled` 只对 codebuddy
返回 true）。其他后端从未开启，零畸形——这本身是反证。

## 移除内容

删除：

- `internal/ai/acp_dup_chunk.go`（`chunkIdentity` / `isDuplicateChunk` / `resetChunkDedup` /
  `chunkDedupEnabled` / `chunkDedupWindow` / `chunkDedupNow` / `pruneExpiredChunkKeys` /
  `maxDupChunkKeys` / `metaKeyCodeBuddyTimestamp`）
- `internal/ai/acp_dup_chunk_test.go`
- `internal/ai/acp_dup_chunk_mapper_test.go`
- `internal/ai/codebuddy_chunk_meta_probe_integration_test.go`（该探针只为验证去重键存在而写）

修改：

- `internal/ai/acp_events.go` — 两个分支（`agent_message_chunk` / `agent_thought_chunk`）
  移除去重调用，留注释说明为什么不做客户端去重
- `internal/ai/acp_pool.go` — 移除 `ACPConn.seenChunkKeys` 字段
- `internal/ai/acp_conn_prompt.go` — 移除 `resetChunkDedup()` 调用

## 回归测试

`internal/ai/acp_chunk_no_dedup_test.go`：

- `KeepsRepeatedShortChunks` — 一个五列表格的分隔行按真实帧序列（`'|' ' ---' ' |' …`）
  投递，断言全部到达
- `MultipleTablesKeepTheirDelimiterRows` — 三张表（两列表 / 六列表 / 两列表），
  断言每个分隔行都存活，且按整行精确计数
- `KeepsIdenticalConsecutiveChunks` — 三个相同字符不得被吞
- `KeepsRepeatedThoughtChunks` — thinking 分支同样不抑制

变异验证：重新加回任何基于 text 的去重，前三个测试立即转红。

## 遗留

1. **历史数据未修复**。已经写进 DB 的畸形分隔行不会自愈，需要单独的数据迁移把被删的
   `|---|---|` 补回去。
2. **原始重复问题**（2026-10-21「已已把报告把报告」）若复现，需要重新定位。当时的成因是
   agent-team 的 `sendSessionUpdateBounded` 超时重试；上游 2.177.0 / 2.177.1 仍未修改这段
   代码（`memberSessionUpdateTimeoutMs = 5e3` + 最多 3 次重试），所以理论上仍可能发生。
   取舍：**重复是可恢复的（用户能看出来），删除是不可恢复的（用户看不出来）**。
