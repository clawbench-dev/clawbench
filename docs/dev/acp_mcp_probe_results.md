# ACP `mcpServers` 注入实测结论（2026-10-19）

> 关联 issue：[#558](https://github.com/clawbench-dev/clawbench/issues/558)（设置里加 MCP 配置：统一管理 + 可按智能体配置）
> 探针：`internal/ai/acp_mcp_probe_integration_test.go` + `internal/ai/testdata/mcp_probe_server.mjs`
> 漂移守卫：`internal/ai/mcp_probe_table_test.go`（无 build tag，CI 常规 `go test ./internal/ai/` 即跑）

## 为什么先做这个探针

仓库现状是四处硬编码的空切片，`mcpServers` 这条链路从未被验证过：

- `internal/ai/acp_conn_lifecycle.go:308`（`session/new`）
- `internal/ai/acp_conn_lifecycle.go:441`（`session/load`，崩溃恢复）
- `internal/ai/acp_conn_lifecycle.go:501`（`session/resume`，`ensureAliveWithSession` 主力路径）
- `internal/ai/acp_pool.go:1614`（`SyncLoadSession`）

不先确认真实行为就动工，会出现「做完发现一半后端不支持」或「支持但首轮拿不到工具」。

## 探针怎么判定

每个后端跑两个**独立进程**（生产也是一会话一进程）：

1. **对照组**：`session/new` 带 `mcpServers: []`，发一轮 prompt 要求调用探针工具。
   目的：证明标记不可能来自该 agent 自己的配置文件（Claude 的 `.mcp.json`、Codex 的 `config.toml`）。
2. **探针组**：`session/new` 带一个 stdio MCP server（`node testdata/mcp_probe_server.mjs`），
   该 server 只有一个无副作用工具，输出固定标记串。

判定只看**工具输出里的标记**，不看模型是否复述了工具名（模型找不到工具时也会说出名字）。

## 实测结果（17 个声明了 `AcpCommand` 的后端）

| 后端 | 广告 http | 广告 sse | `session/new` 接受 | 对照组有工具 | 标记出现 | 结论 |
|---|---|---|---|---|---|---|
| claude | ✅ | ✅ | ✅ | ❌ | ✅ | **OK** |
| codex | ✅ | ❌ | ✅ | ❌ | ✅ | **OK** |
| codebuddy | ✅ | ✅ | ✅ | ❌ | ✅ | **OK** |
| qoder | ✅ | ✅ | ✅ | ❌ | ✅ | **OK**（需自动批准权限） |
| opencode | ✅ | ✅ | ✅ | ❌ | ✅ | **OK** |
| commandcode | ✅ | ❌ | ✅ | ❌ | ✅ | **OK** |
| hermes | ❌ | ❌ | ✅ | ❌ | 不稳定 | **异步挂载**（见下） |
| kimi | ✅ | ❌ | ✅ | ❌ | — | 未登录，无法判定 |
| mimo | ✅ | ✅ | ✅ | ❌ | — | 空回合，无法判定 |
| grok | ✅ | ✅ | ❌ | — | — | 未登录，无法判定 |
| cursor | ✅ | ✅ | ✅ | ❌ | — | 套餐限制（"Upgrade your plan"） |
| copilot | ✅ | ✅ | ❌ | — | — | 未登录，无法判定 |
| antigravity | ❌ | ❌ | ❌ | — | — | 未登录，无法判定 |
| zcode | ✅ | ❌ | ✅ | ❌ | — | 后端进程崩溃，无法判定 |
| dsh | ✅ | ❌ | ✅ | ❌ | ❌ | 接受但工具不存在 |
| deepseek | ❌ | ❌ | ✅ | ❌ | ❌ | 接受但工具不存在 |
| pi | ❌ | ❌ | ✅ | ❌ | ❌ | 接受但工具不存在 |

## 对设计的直接影响

### 1. 七个后端确认可用，其中四个是主力

claude / codex / codebuddy / qoder / opencode / commandcode / hermes 都能真正把 MCP 工具交给模型。
CodeBuddy 尤其重要：它**原生支持协议字段**，`--mcp-config` 那个 workaround（`acp_conn_lifecycle.go:697-712`）
可以退役。

### 2. 必须按能力位过滤 HTTP/SSE

stdio 是 ACP 规范强制要求（所有后端都支持），但 http/sse 不是：

- codex 广告 `http: true, sse: false`，且其适配器对 `sse` 类型直接 `throw RequestError.invalidRequest`
- hermes / deepseek / pi / antigravity 三者全 false（`mcpCapabilities` 未广告）

不做过滤就是**硬失败**（整个 `session/new` 报错），不是降级。当前
`acp_conn_lifecycle.go:846-857` 只提取 `listSessions`/`deleteSession`，
`McpCapabilities` 被直接丢弃 —— 实现时必须补上。

### 3. hermes 是异步挂载，首轮拿不到工具

实测：`session/new` 返回后立刻发 prompt，模型看不到工具；等一轮之后（同一 session）
就能看到。其 `~/.hermes/logs/mcp-stderr.log` 显示 MCP server 的启动时刻与首轮结束时刻相同。

这对产品的影响：
- 用户第一次提问时 MCP 工具可能不可用，第二次才生效 —— 需要 UI 提示或首轮重试策略
- 不能假设「`session/new` 成功 = 工具就绪」

其余六个后端在 `session/new` 返回时工具已就绪（首轮即可用）。

### 4. 部分后端会卡在权限审批上

qoder 的 MCP 工具调用需要用户批准；探针不自动批准时整轮挂到超时。
实现时必须确认：自动批准（`autoApprove`）是否覆盖 MCP 工具调用，否则任务场景会挂死。

### 5. 三个后端接受字段但完全忽略

dsh / deepseek / pi：`session/new` 不报错，但工具从未出现（两轮都试过）。
对这三个后端注入 `mcpServers` 是**静默无效**的，UI 上应明确标注「该智能体不支持」，
而不是让用户以为配置生效了。

## 未覆盖的部分

- **http / sse 型 MCP server 未实测**：本次只测 stdio（规范强制、覆盖面最广）。
  实现 http/sse 前需要再补一轮探针，重点验证 codex 的 `http_headers` 映射和
  claude 的 `headers` 映射是否一致。
- **未登录的五个后端**（kimi / grok / copilot / antigravity / mimo）需要登录后重跑。
  命令：
  ```bash
  MCP_PROBE_BACKENDS=kimi,grok,copilot,antigravity,mimo \
    go test -tags integration -v -run 'TestACP_MCPProbe' -timeout 1200s ./internal/ai/
  ```
- **zcode** 后端进程在探针环境下崩溃（`zcode backend reader exited`），
  可能是 Electron 引擎在无头环境的限制，需要单独排查。

## 复现

```bash
# 全量（约 20 分钟，会真实调用模型）
go test -tags integration -v -run 'TestACP_MCPProbe' -timeout 2300s ./internal/ai/

# 指定后端
MCP_PROBE_BACKENDS=claude,codex go test -tags integration -v \
    -run 'TestACP_MCPProbe' -timeout 900s ./internal/ai/

# 给异步挂载的后端留出启动时间（秒）
MCP_PROBE_WARMUP_SECONDS=20 MCP_PROBE_BACKENDS=hermes go test -tags integration -v \
    -run 'TestACP_MCPProbe' -timeout 400s ./internal/ai/
```

探针会在 agent 的本地状态里留下会话记录（`~/.claude/projects/`、`~/.codex/sessions/` 等），
**不会**写入任何 MCP 配置文件。hermes 会把探针 server 名缓存到
`~/.hermes/cache/mcp_schema_cache.json`，探针用每次运行唯一的 server 名避免污染下一轮的对照组；
清理时删掉所有以 `clawbench-probe` 开头的键即可。
