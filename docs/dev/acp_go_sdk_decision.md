# ACP Go SDK 选型与旁路策略（决策记录）

> 记录日期：2026-10-02
> 状态：**决定维持现状**（`github.com/coder/acp-go-sdk v0.13.5` + 自研旁路）
> 相关实现：`internal/ai/acp_raw_notification.go`、`internal/ai/acp_stdout_filter.go`
> 相关实测：`internal/ai/acp_raw_notification_probe_integration_test.go`

---

## 1. 背景与问题

ClawBench 通过 ACP（Agent Client Protocol）stdio 与各 CLI 智能体通信，使用
`github.com/coder/acp-go-sdk`。使用中遇到两类上游缺陷，且观察到上游可能停维：

1. **未知 `sessionUpdate` 变体被错分类**：SDK 的 `SessionUpdate.UnmarshalJSON`
   对规范外的 discriminator 不报错，而是按「字段存在性」兜底匹配，把扩展变体塞进
   某个已知结构（如 `subagent_spawned` → `SessionInfoUpdate`，`tool_call_pending`
   带 `toolCallId`+`title` → `ToolCall`），**payload 丢失或错位**。
2. **自定义通知方法不 dispatch**：`_codebuddy.ai/checkpoint` 等扩展方法不在 SDK 的
   方法分发表内，被直接丢弃。

同时调查了上游维护状态与替代库，判断是否应 fork 或迁移。

## 2. 上游维护状态（2026-10-02 核查）

| 项 | 值 |
|----|----|
| 最新 tag | **v0.13.5**（2026-06-02） |
| `main` HEAD SHA | `0845a3bb…`，与 v0.13.5 tag **完全相同** |
| `v0.13.5...main` | `ahead_by: 0`（无未打标签提交） |
| 最后提交 | 2026-06-05（`pushed_at`） |
| 停更时长 | **约 4 个月** |
| 仓库状态 | 未 archived |
| Star / 依赖 | 239★ / **零外部依赖**（纯标准库） |

**结论**：已停更约 4 个月，但代码完整、无破坏性问题。

## 3. 代码规模（fork 可行性评估）

| 部分 | 行数 | 大小 | 是否必需 |
|------|------|------|----------|
| 根包 `acp`（运行时） | **11,988** | 424 KB | ✅ 必须 |
| ├─ `*_gen.go`（**机器生成**） | 10,532 | — | ✅（非人手代码） |
| └─ 手写（connection/helpers/errors/…） | **1,456** | — | ✅ |
| 根包测试 | 3,396 | — | ⬜ fork 后是资产 |
| `example/`（4 个示例） | 1,262 | 72 KB | ❌ |
| `schema/`（spec JSON） | — | 488 KB | ❌（除非重新生成） |
| `cmd/generate/`（代码生成器） | — | 172 KB | ❌（除非重新生成） |
| `.github/` `mise*` `Makefile` | — | 20 KB | ❌ |

**关键事实**：12K 行中 10.5K 是 `// Code generated … DO NOT EDIT`，从
`schema.json` 生成；**真正人写的只有约 1,456 行**。依赖为零。

**License**：Apache-2.0（无 NOTICE 文件，fork 时需自行添加修改声明）。

## 4. 替代方案调研

### 4.1 ACP 官方没有 Go SDK

官方仓库 `agentclientprotocol/agent-client-protocol` 的 README 列出的**官方库**为：
Kotlin、Java、Python、Rust、TypeScript。**Go 不在其中**，只有「社区库」。
因此 `coder/acp-go-sdk` 是 Go 生态的事实标准，而非官方实现。

### 4.2 候选库对比（2026-10-02 实测）

| 仓库 | ★ | 最后提交 | Go 要求 | 行数 | License | Release |
|------|---|----------|---------|------|---------|---------|
| **coder/acp-go-sdk**（现状） | 239 | 2026-06-05 | **1.21** | 12K | Apache-2.0 | v0.13.5 |
| ironpark/acp-go | 32 | **2026-10-02** | **1.27** ⚠️ | **46K** | MIT | v0.1.0 |
| Tangerg/acp | 4 | 2026-10-01 | — | — | Apache-2.0 | 无 |
| caelis-labs/acp-go-sdk | 2 | 2026-09-29 | — | — | Apache-2.0 | 无 |
| joshgarnett/agent-client-protocol-go | 2 | 2025-11-06 | — | — | MIT | 停滞 |

### 4.3 唯一有竞争力的替代：`ironpark/acp-go`

**优点**
- 活跃（当日仍有提交），远超 coder 的 4 个月
- 支持 `acp1`（v1 协议）与 `acp2`（**v2 协议**，官方 spec 已有 `schema/v2`）
- 有 `TestExtensionMethodsFallThrough`，看似处理扩展方法
- MIT，有 `v0.1.0` release 与 CHANGELOG

**硬伤**
1. **要求 Go 1.27**；ClawBench 当前 `go 1.25`（工具链 1.26.1）。采用需整体升级 Go，
   连带影响 CI、交叉编译（`--windows/--linux-arm64/--darwin`）与 Android 构建链。
2. **46K 行**（4 倍），含 `acphttp` / `acpmcp` / `router` / `middleware` 等未使用的层。
3. **API 完全不同**：`NewClientSideConnection(func(c) Client, transport, opts...)`
   vs coder 的 `NewClientSideConnection(client, in, out)`。
   ClawBench 有 **69 个文件、157 个 `acp.*` 符号**需要重写。
4. 其扩展处理能力**未实测验证**（仅凭测试名推断）。

## 5. 决策：维持 coder + 旁路

### 5.1 为什么旁路优于 fork 或迁移

自研旁路（`acp_raw_notification.go`）在 stdout 层做 **tee**（与既有 `rawResponseSink`
同纪律：tee 不 steal），由客户端从原始行读取：

- 未知 `sessionUpdate` 变体 → 交 `SetExtensionUpdateHandler`
- 非规范通知方法 → 交 `SetExtensionNotificationHandler`
- typed 路径加 `sessionUpdateVariantMismatch` 护栏，避免为错分类帧产出错误事件

**通用性对比**：

| | Fork 改 SDK | 旁路 |
|---|---|---|
| 修未知变体 | 逐变体加 case，每加一个改一次 | **任何未来变体自动生效** |
| 修自定义方法 | 逐个加 dispatch | **任何方法自动生效** |
| 上游停更影响 | 自己维护 12K 行 | 无感（已 pin） |
| 侵入性 | 改 SDK 源码 | 不改 SDK，可随时关闭 |

SDK 的 `UnmarshalJSON` 是「按 discriminator 穷举」，每新增一个变体就要改一次；
而 tee 是「未知的一律交出来」，**永不需要跟进**。

### 5.2 迁移到 ironpark 的收益为负

换库的动机是「怕停维」与「修扩展协议」，但：
- 停维不可怕：12K 行、零依赖、纯 stdlib，需要时 fork 即可（424 KB）
- 扩展协议已解决：旁路比任何「穷举式」SDK 都更抗变化
- 迁移成本高（69 文件 + 升 Go 1.27），换来的是「另一个可能停维的社区库」

## 6. 重新评估的触发条件

出现以下任一情况时，重新评估 fork 或迁移：

1. **需要给 SDK 类型加字段**且 `Meta` 绕过开始变脏 —— 例如 Claude 的
   `clientCapabilities.subagents`（AIR 门控）不在 SDK 类型定义里。
2. **需要改 outbound 序列化** —— 某请求要携带 SDK 未建模的字段。
3. **ACP spec 新增变体且希望正确建模**（而非当未知处理）。
4. **必须使用 ACP v2 协议**（`acp1` 已不满足）。
5. **上游归档或出现破坏性安全通告**。

## 7. 若将来 fork 的正确姿势

**不要 `go mod vendor`**（会把 frp/sqlite/larksuite 等全部拖入，数十 MB）。
只拷单个包 + `replace`：

```
third_party/acp-go-sdk/     ← 仅根包 .go + go.mod + LICENSE + NOTICE
```

```go
// go.mod
replace github.com/coder/acp-go-sdk => ./third_party/acp-go-sdk
```

- 69 个文件的 import 路径**无需改动**（module 名保持不变）
- 删除 `example/` `schema/` `cmd/` `.github/` `mise*` `Makefile`，仅留根包
- 体积：424 KB（不含测试）/ 556 KB（含测试）
- **保留测试**是 fork 的关键价值——它们验证 patch 未破坏协议
- Apache-2.0：保留 `LICENSE`，新增 `NOTICE` 声明「本副本已修改」

## 8. 附：核查命令

```bash
# 上游最新版本
curl -s https://proxy.golang.org/github.com/coder/acp-go-sdk/@latest
go list -m -versions github.com/coder/acp-go-sdk

# 确认 main 与最新 tag 是否一致
curl -s https://api.github.com/repos/coder/acp-go-sdk/compare/v0.13.5...main

# 官方库清单（确认 Go 不在官方列表）
curl -s https://raw.githubusercontent.com/agentclientprotocol/agent-client-protocol/main/README.md
```
