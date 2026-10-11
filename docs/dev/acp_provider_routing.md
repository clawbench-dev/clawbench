# 跨厂商模型切换（issue #340）— 能力探测与方案（未实现，issue 已关闭）

> **状态：不实现（closed as not planned）。** 保留本文件作为调研记录。
>
> **决策理由**：ClawBench 里**其他智能体的模型 Provider 配置都与 ClawBench 解耦**
> （各自读自己的全局配置：claude 读 `~/.claude/settings.json`、codex 读
> `~/.codex/config.toml`、qoder 读 `~/.qoder/...`）。issue #340 想让 claude / codex
> 变成「由 ClawBench 运行时托管 provider 路由」，这与其余所有后端的设计**不一致**。
> 为保持统一，决定不做。issue #340 关闭。
>
> 本文件记录：为什么老方案过时、运行时扩展的实测结论、若将来要做该怎么落地。

---

## 1. 问题（issue #340 的原始诉求）

issue 标题：「real per-session model/provider switching for codex & claude agents」。

- **同厂商内换模型**：**一直是真换**。每回合 `Prompt` 前发
  `session/set_config_option(model=...)`（`internal/ai/acp_conn_prompt.go:92`），
  适配器会真的改模型，还有 `lastSetModel` 去重（`acp_pool.go:1972`）。
- **跨厂商换**（如 claude agent 切到 DeepSeek）：是「假换」。适配器的 `base_url` /
  key / model alias 在**进程启动时**从全局配置读，`set_config_option(model=...)`
  改不到它 ⇒ UI 显示新模型，底层仍调老厂商。

所以问题**只影响 codex + claude 两个后端**，且只在「跨厂商」时暴露。

---

## 2. 老方案（issue 提出，现已过时）

issue 当时建议：

1. per-service 独立配置目录（`CODEX_HOME` / `CLAUDE_CONFIG_DIR`），切换时改写
   目录内的 `config.toml` / `settings.json`；
2. 建 `agent_api_keys` 表存密钥；
3. 追踪「当前生效模型」；
4. 自动补 `[model_providers.X]` 块。

**为什么过时**：提 issue 时（2026-08-04）适配器确实只能靠改全局文件；但**现在两个
桥接适配器都已内置运行时路由扩展**，无需碰任何磁盘文件。且 `agent_api_keys` 表已在
PR #346 被移除（`docs/spec/infra/config-and-discovery.md:98`）。

---

## 3. 实测结论（能力探测）

探针：`internal/ai/acp_provider_routing_probe_integration_test.go`（`-tags integration`）。

```bash
go test -tags integration -v -run 'TestACPProviderRoutingProbe' -timeout 900s ./internal/ai/
```

需要本机安装 claude / codex。**实测全部通过**（2026-10-11）。

### 3.1 两条机制（机制不同，按后端分派）

| 后端 | 机制 | 载荷 | 前置条件 |
|---|---|---|---|
| claude (0.89.1) | `providers/set` | `{providerId:"main", apiType:"anthropic", baseUrl, headers}` | 无（无条件广告 `agentCapabilities.providers`） |
| codex (2.2.2) | `authenticate` + `methodId:"gateway"` | `{methodId:"gateway", _meta:{gateway:{baseUrl, providerName, headers}}}` | Initialize 必须广告 `clientCapabilities.auth._meta.gateway=true`，否则 authMethods 不含 `gateway` |

**决定性证据**（本地 mock 收到真实请求）：

- claude → `POST /v1/messages`，`Authorization: Bearer acp-proxy` +
  `x-api-key: <我们传的 key>`（适配器 `createEnvForProvider()` 把 headers 变成
  `ANTHROPIC_CUSTOM_HEADERS`，base_url 变成 `ANTHROPIC_BASE_URL`）。
- codex → `POST /responses`，`Authorization: Bearer <我们传的 key>`
  （适配器把 gateway 合并进 `[model_providers.custom-gateway]`）。

两者都是**进程内**生效，不写磁盘。

### 3.2 关键陷阱

1. **Go SDK 字段名不一致（决定性）**：`acp-go-sdk v0.13.5` 的
   `UnstableSetProviderRequest.Id` 序列化成 `json:"id"`，但 claude 适配器（及 JS SDK
   v1.8.0 `zSetProviderRequest`）读的是 **`providerId`**。
   - 传 `providerId` → ACCEPTED
   - 传 `id` → `-32602 providerId: Invalid input: expected string, received undefined`
   - **`conn.UnstableSetProvider(...)` typed 方法直接失败** ⇒ 接 claude 必须走
     `ACPConn.CallRaw`（生产代码已有，`internal/ai/acp_raw_rpc.go`）。
2. **codex `wire_api` 写死 `"responses"`** ⇒ 只接 Responses API 厂商。DeepSeek 这类
   只有 OpenAI-chat 端点的接不了；claude 走 Anthropic 兼容端点可接。
3. **claude `providers/set` 是进程级**：只对「调用之后创建或加载的会话」生效 ⇒
   顺序必须是 spawn → set provider → NewSession/LoadSession。换 provider 要重启进程
   （换模型不用）。
4. **claude 的 model 名仍来自全局 settings**：路由只改 base_url，实测 body 里 model
   仍是全局配置的值 ⇒ 跨厂商切换时 model 名仍需另经 `set_config_option` 设。

---

## 4. 若将来要做：落地方案（记录，不实现）

### 4.1 数据模型

`internal/model/agent.go:9` 的 `AgentModel` 加 `Provider string`（provider id，空=原生）。
provider 元数据复用 `internal/model/provider_registry.go`。凭证跟随现有 `APIConfig`
模式存 config.yaml（不建表），走 settings 敏感字段掩码。

### 4.2 接缝（照搬 mid-turn 模式）

新增 `internal/ai/provider_routing.go`：

```go
type ProviderRoutingRequest struct { SessionID, ApiType, BaseURL string; Headers map[string]string }
type ProviderRouter interface {
    Apply(ctx context.Context, transport RawRPCTransport, req ProviderRoutingRequest) error
}
var LookupProviderRouterFn func(backendID string) ProviderRouter
```

- 复用现有 `RawRPCTransport`（`CallRaw` + `AcpSessionID`）——两条机制都能走裸通道。
- 注册：`internal/ai/backends/plugin.go` 的 `BackendPlugin` 加 `ProviderRouter` 字段 +
  `registry.go` 的 `LookupProviderRouter` + `acp_wire.go` 接线。
- claude 实现发 `providers/set`（字段名 `providerId`！）；codex 实现发
  `authenticate(gateway)`。

### 4.3 生命周期

在 `ensureAliveWithSession`（`acp_conn_lifecycle.go:224`）的 `spawnLocked` 之后、
建/恢复会话之前插入 `applyProviderRouting`。`ACPConn` 记 `currentRouting` 指纹：

- 指纹相同 → 只发 `set_config_option(model=...)`（现状）。
- 指纹不同 → 重启进程再 apply。
- respawn（空闲/崩溃恢复）→ 从期望 provider 重新 apply，**不**当成用户切换。

`buildInitializeRequest`（`acp_conn_lifecycle.go:165`）按 backend 门控
`Auth.Meta={"gateway":true}`（只给 codex）。

### 4.4 降级

两个扩展都是 UNSTABLE。实现须运行时探测：`providers/set` 返回 -32601/-32602 时记
warning 并禁用该 agent 的跨厂商能力，**不影响同厂商换模型**。

---

## 5. 附：探测文件

- 探针：`internal/ai/acp_provider_routing_probe_integration_test.go`
- 相关：`docs/dev/claude_acp_extensions.md`（claude ACP 扩展面）、
  `docs/dev/acp_go_sdk_decision.md`（Go SDK 选型）
