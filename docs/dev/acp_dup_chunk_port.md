# CodeBuddy chunk 去重：三代尝试与最终移除（2026-10-22 → 2026-10-30）

## 最终结论

**已移除。ACP wire 层一律透传，不做任何帧级内容去重。**

`internal/ai/acp_dup_chunk.go` 与其测试已删除，`acp_events.go` 两处接入、
`acp_pool.go` 的 `lastChunkKey` 字段、`acp_conn_prompt.go` 的每回合清空一并移除。

原因不是「某一版判据没调好」，而是**这个判据在 ACP 客户端这一层不存在**：
序列化之后，「重试」与「模型确实又输出了相同文本」在字节上完全一致
（同 `messageId`、同 `_meta.timestamp`、同 `text`）。三代实现都在文本维度上找判据，
所以三代都在删真实内容。

## 为什么上游能去重，客户端不能

CodeBuddy 的 `publishSessionUpdateToBus`（`dist/codebuddy.js`）有两层：

```js
let eB = new WeakMap, eS = new Map, eQ = 5e3;
function wasPublishedRecently(u){ const t = eB.get(u); return t !== undefined && Date.now()-t < eQ }
function chunkPublicationKey(sid, u){ /* sid + variant + messageId + ts + reqId + parentId + text */ }
function publishSessionUpdateToBus(sid, u){
  if (wasPublishedRecently(u)) return false;      // ① WeakMap 按「JS 对象身份」
  eB.set(u, Date.now());
  const k = chunkPublicationKey(sid, u);
  if (k) { const t = eS.get(k); if (t !== undefined && now - t < eQ) return false; eS.set(k, now); }  // ② 内容键 + 5s
  ...
}
```

- **① 是真正挡住重试的那层**，判据是 **JS 对象身份**。重试路径
  `sendSessionUpdateBounded` 把 `conn.sessionUpdate(notification)` 与 5 秒定时器赛跑，
  超时抛错后 `sendSessionUpdateWithRetries` **拿同一个 `notification` 对象**再发一次
  （最多 3 次）。同一个对象 → WeakMap 必命中。
  这个信号**不跨越 wire**：客户端只收到重新序列化的 JSON。
- **② 挂在 WebUI 发布总线上**，订阅者（它的 WebUI）在进程内、chunk 已被合并，
  粒度是「一段 update」而不是 4 个字符。

所以「照抄上游」只可能抄到键的元素列表，抄不到判据。第二版（`482e8763e`）就是这么错的。

## 重复的触发条件（决定概率，而不是文本）

只有团队成员帧会走重试：

- `AcpTeamBridge.subscribeMemberSession(session)` 第一行 `if (!session.teamContext?.memberName) return`；
- `sendSessionUpdateBounded` / `sendSessionUpdateWithRetries` 全仓 4 个调用点都在
  `AcpTeamBridge` 内（成员帧转发、成员 tool 终态、`flushPendingTeamUpdates`）；
- 主持人自己的正文、普通单聊的正文走 `publishSessionUpdateToBus` → 总线 → 连接，
  **没有任何超时重试**。

触发链条：成员帧写入卡住 ≥5 秒（消费端背压：通知 goroutine 被锁/DB/WS 阻塞、
进程被挂起、resume 重放洪峰）→ 超时抛错但**那次 write 并未取消** → 重试再写一遍
→ 两帧都落地。停顿期间排队的**每一帧**都会重复，所以症状是整段文字翻倍
（`已已把报告把报告发给发给 team team-le-leadad…`），不是个别字符。

结论：**重复概率 = P(团队回合) × P(≥5 秒写入停顿)**，与文本是否重复零相关；
而文本重复（`|---|---|` 连写、`\n\n`、缩进空格）在正常输出里是常态。

## 三代实现与各自的误删

### 第一版（`0abbe8b0e`，2026-10-22）：键抄错

```
sessionId + variant + messageId + timestamp + memberEvent + text
```

上游第 5 个元素是 `conversationRequestId ?? requestId`、第 6 个是 `parentToolCallId`，
`messageId` 优先取顶层字段。这一版都不对。

### 第二版（`482e8763e`）：键抄对，判据仍是内容

照搬上游键 + 5 秒窗口。生产一周内 6436 次丢弃里，**179 组只丢了 1 次**——
真重试是连续重发，必然连续丢 2–3 次；只丢 1 次说明是与窗口内某个更早帧文本相同的
孤立帧，即误删。

### 第三版（`605285f1c`，2026-10-29）：改成「紧邻」仍然错

判据改成「与上一帧被接受的 chunk 完全相同」，理由是「合法重复一定隔着别的输出」。
该结论来自一份 82 帧样本（相邻同文本 0/81），在更宽的表上不成立：

- 2026-10-30 msg 67355（单聊，`session_type=chat`，结构上不可能重复）：
  原生 `~/.codebuddy/projects/*/*.jsonl` 的分隔行是 `|---|---|---|---|---|`，
  ClawBench 落库为 `|---|---|---|`，正文其余 1414 字符逐字节相同；
  该回合日志只有 1 条 `dropping duplicate agent_message_chunk`，丢的正是 8 字符
  = 一个 `|---|---|` 帧。**8 字符 = 1 帧，与「真重试至少连续丢 2 次」直接矛盾。**
- 同一天 msg 67237：`192.168.x.x:20000` → `192.168.x:20000`（丢 `.x`）。
- 表格损坏统计（`分隔行列数 < 表头列数`）：10-20/10-21 去重上线前 144 张表 **0 损坏**；
  上线后 10-22 起每天 2–13 张，三天 44 张，全库 codebuddy 累计 130 张。

关键：`timestamp` 也在键里，但两帧**合法**内容可以共享 `messageId + timestamp + text`
（67355 即是），所以键里带 timestamp 不构成任何判别。

## 如果重复真的复现

不要回到逐帧文本比对。可行方向只有两个：

1. **消息级密度门控**：一条消息里相邻重复帧占比极高（真重试风暴 ≈ 每个 delta 都重复）
   才启用丢弃；孤立重复一律放行。代价是风暴最前面几帧仍会漏过。
2. **上游修**：对象身份在 CodeBuddy 进程内是可得的，让 `sendSessionUpdateBounded`
   在重试前检查 `wasPublishedRecently` 语义（或干脆不重试已被消费的帧）。

判断是否复现的最快办法：日志里按 `messageId` 统计相邻同文本帧的数量——
真风暴是「一条消息几百次」，零星几次是内容重复。

## 遗留

已写入 DB 的畸形分隔行不会自愈（用户 2026-10-30 决定先不修）。若要修，
原生 `~/.codebuddy/projects/<slug>/<acp_sid>.jsonl` 的 `output_text` 是权威副本，
可按逐字 diff 回填。
