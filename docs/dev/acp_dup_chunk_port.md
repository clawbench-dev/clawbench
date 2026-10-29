# CodeBuddy chunk 去重：按「紧邻」判重（2026-10-29）

## 结论

ClawBench 对 CodeBuddy 的模型输出帧做去重，判据是**紧邻**：一个 chunk 与
**上一帧被接受的 chunk** 完全相同才算重试。不用时间窗。

只对 `codebuddy` 生效；其他后端没有这个问题，不走这条路径。

## 三次迭代的错误

### 第一版（`0abbe8b0e`，2026-10-22）：键抄错

```
sessionId + variant + messageId + timestamp + memberEvent + text
```

上游 `chunkPublicationKey` 的第 5 个元素是 `conversationRequestId ?? requestId`、
第 6 个是 `parentToolCallId`，且 `messageId` 优先取顶层字段。这一版都不对。

### 第二版（`482e8763e`）：以为「键抄对」就能修好

照搬了上游键，但**保留了 5 秒窗口**。这个改动对实际缺陷无效，因为第一版和
第二版的键**都包含 `timestamp`**——两次实现都不是因为「丢了 timestamp」而出错。

### 第三版（本提交）：判据错了

时间窗本身就是错的。理由见下。

## 为什么时间窗是错的

CodeBuddy 逐字符推送模型输出。一个 `|---|---|` 分隔行到达时是：

```
'|'  ' ---'  ' |'  ' ---'  ' |'  '\n'
```

一条消息里 `' |'` 重复 21 次、`' '` 16 次、`' ---'` 7 次。任何「相同文本再次出现
就算重复」的规则都会删真实内容。

### 生产数据

统计 2026-10-22..10-29 的 `dropping duplicate` 日志（6436 次）：

| 指标 | 值 |
|---|---|
| 同 (session, messageId) 组内相邻丢弃总数 | 116 |
| 其中间隔 < 5 秒 | **106（91%）** |
| 丢弃 1 次的组 | **179** |
| 丢弃 2+ 次的组 | 62 |

**关键判据：179 组只丢了 1 次。** 真重试是
`sendSessionUpdateWithRetries` 连续重发（最多 3 次），必然连续丢 2-3 次。
只丢 1 次意味着：这是一个孤立帧，它与窗口内某个更早的帧文本相同，于是被误删。

## 为什么「紧邻」是对的

真重试是同步重发**同一个 update 对象**（`sendSessionUpdateBounded` 超时后，
`em.tail` 上重跑），所以它一定是**紧挨着的下一帧**。

合法重复一定隔着别的输出。

真实帧实测（2026-10-29，82 帧两表回复）：

| 指标 | 值 |
|---|---|
| 相邻帧同文本 | **0 / 81** |
| 非相邻帧同文本 | 55 对 |

所以：**相邻同文本 = 重试，非相邻同文本 = 内容。** 这是唯一能分开两者的判据，
时间做不到。

## 实现

```go
func (c *ACPConn) isDuplicateChunk(identity string) bool {
	if c == nil || identity == "" {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if identity == c.lastChunkKey {
		return true
	}
	c.lastChunkKey = identity
	return false
}
```

状态只有一个字段 `lastChunkKey`（上一帧被接受的 identity）。被丢弃的重试**不替换**
它，所以三份连续拷贝塌成一份。

`chunkIdentity` 仍照搬上游的元素列表：

```
sessionId + variant + messageId + timestamp + requestId + parentToolCallId + text
```

`timestamp` 保留在键里是必要的：真重试复用同一个对象，timestamp 相同；正常的新帧
timestamp 不同。但它不再用于「多久算重复」——那由邻接决定。

`messageId` 顶层优先、`requestId` 取 `conversationRequestId ?? requestId`，与上游一致。

## 与上游的关系

上游 `chunkPublicationKey` 配的是 5 秒窗口，用在它的 **WebUI 发布总线**
（`publishSessionUpdateToBus`）上，那里 chunk 已合并。ClawBench 消费的是 ACP
wire，粒度是字符级，所以上游的窗口策略不能直接套用。

元素列表照搬，判据换成邻接。

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
- `PrefersTopLevelMessageID` / `RequestIDPrecedence` — 与上游 `??` 链一致
- `DistinguishesEveryComponent` — 7 个元素各自参与
- `IsInjective` — JSON 编码不会因 NUL 字节伪造键

**判重**
- `DropsOnlyTheImmediatelyRepeatedChunk` — 连续 3 份只留 1 份
- `NonAdjacentRepeatIsContent` — **核心回归**：两个 `' ---'` 中间隔一帧，都要保留
- `RepeatMuchLaterIsContent` — 隔 200 帧的重复是内容
- `RetryDoesNotReplacePrevious` — 被丢的重试不成为新的「上一帧」
- `EmptyIdentityNeverDrops` / `NilConnNeverDrops` / `ResetChunkDedup_*`

**端到端**
- `RealWireSequenceIsLossless` — **回放真实抓取的 82 帧**，逐字节断言
- `TableDelimiterRowSurvives` / `MultipleTablesKeepTheirDelimiterRows` /
  `SameTextLaterInMessageIsKept` / `ImmediateRetryIsDropped` /
  `ThoughtChunkRetryIsDropped` / `DedupDoesNotApplyToOtherBackends` /
  `KeepsChunksWithoutIdentity`

## 遗留

已写入 DB 的畸形分隔行不会自愈，需单独迁移补回被删的 `|---|---|`。
