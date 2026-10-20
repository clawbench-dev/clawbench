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

三个探针的分工：

| 探针 | 覆盖 | 命令 |
|---|---|---|
| `TestACP_MCPProbe` | `session/new` + stdio 传输 | `-run 'TestACP_MCPProbe$'` |
| `TestACP_MCPProbeResume` | `session/resume` / `session/load` 恢复路径 | `-run 'TestACP_MCPProbeResume'` |
| `TestACP_MCPProbeHTTP` | http / sse 传输 | `-run 'TestACP_MCPProbeHTTP'` |

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

## 恢复路径（`session/resume` / `session/load`）

生产环境里绝大多数「非首次」连接走的是 resume：`ensureAliveWithSession` **始终用
ResumeSession**（见 `docs/spec/core/ai-backend.md`）。如果某个后端只在 `session/new`
时读 `mcpServers`，症状就是「新会话能用 MCP，进程重启后工具消失」。

探针 `TestACP_MCPProbeResume` 的做法：先用 `session/new` 建会话并证明工具可用，
然后在**全新进程**里对同一 session ID 分别调 `session/resume` 与 `session/load`
（都带同一个 MCP server），再让模型调一次工具。

| 后端 | baseline | session/resume | session/load | 结论 |
|---|---|---|---|---|
| claude | ✅ | ✅ | ✅ | OK |
| codex | ✅ | ✅ | ✅ | OK |
| codebuddy | ✅ | ✅ | ✅ | OK |
| qoder | ✅ | ✅ | ✅ | OK |
| opencode | ✅ | ✅ | ✅ | OK |
| commandcode | ✅ | ✅ | ✅ | OK |
| hermes | ❌ | — | — | 跳过（baseline 本身拿不到工具，见下） |

**结论：6 个已验证后端在恢复路径上同样保留 MCP 配置**，不存在「重启后工具消失」的缺陷。

hermes 无法判定：它的 baseline 会话本身就时好时坏（异步挂载），而 baseline 不成立时
任何 resume 结果都无法归因，探针选择跳过而不是给出错误结论。

> 踩坑记录：探针最初用 `cmd.Process.Kill()` 结束进程，只杀掉 adapter，留下 codex-acp
> 派生的 codex app-server 子进程继续持有 per-thread writer 锁，于是下一个进程的
> resume 报 `thread ... already has an active writer`，看起来像「不支持 resume」。
> 改成 `killProcessGroup`（生产用的同一套）后 6/6 全部通过。**这类假阴性只能靠
> 「先证明 baseline 成立」+「进程组级清理」两条一起挡住。**

## HTTP / SSE 传输

远程 MCP 基本都是 HTTP/SSE 型。探针 `TestACP_MCPProbeHTTP` 起一个本地 fixture
（同时提供 Streamable HTTP 的 `POST /mcp` 与旧式 HTTP+SSE 的 `GET /sse` +
`POST /messages`），按 agent 广告的能力位分别尝试。

| 后端 | http 广告 | http 实测 | sse 广告 | sse 实测 |
|---|---|---|---|---|
| claude | ✅ | ✅ OK | ✅ | ✅ OK |
| codex | ✅ | ✅ OK | ❌ | 跳过（未广告） |
| codebuddy | ✅ | ✅ OK | ✅ | ✅ OK |
| qoder | ✅ | ✅ OK | ✅ | ✅ OK |
| opencode | ✅ | ✅ OK | ✅ | ✅ OK |
| commandcode | ✅ | ✅ OK | ❌ | 跳过（未广告） |
| hermes | ❌ | 跳过 | ❌ | 跳过 |

**关键结论：能力位广告与实际行为一致**——广告了的都能用，没广告的（codex/commandcode
的 sse、hermes 的 http/sse）探针不发送。这验证了「按能力位过滤」的策略是正确的，
不会误伤可用后端。

同时确认：**字段映射由各适配器自行处理**，ClawBench 只需按 ACP 标准结构填
`name` / `url` / `headers`（codex-acp 内部把 `headers` 转成 `http_headers`），
不需要为每个后端生成不同形状。

## 未覆盖的部分

- **未登录的五个后端**（kimi / grok / copilot / antigravity / mimo）需要登录后重跑。
  命令：
  ```bash
  MCP_PROBE_BACKENDS=kimi,grok,copilot,antigravity,mimo \
    go test -tags integration -v -run 'TestACP_MCPProbe' -timeout 1200s ./internal/ai/
  ```
- **zcode** 后端进程在探针环境下崩溃（`zcode backend reader exited`），
  可能是 Electron 引擎在无头环境的限制，需要单独排查。
- **多 server 并发注入**（一次传 3~5 个）未测。**启动失败行为已测**，见下节。
- **CLI 传输侧完全没测**：CLI 后端没有 `mcpServers` 参数，只能改各自配置文件，
  同一个智能体在 CLI 模式下配置不生效，产品上需要说明。

## 启动失败行为（`TestACP_MCPProbeFailure`）

三种失败形态 + 一坏一好混合：

| 后端 | 缺失命令 | 立即退出 | 永不响应 | 混合 |
|---|---|---|---|---|
| claude | 跳过 0.6s | 跳过 0.6s | 跳过 0.5s | TOLERANT |
| codex | 跳过 0.2s | 跳过 0.1s | 跳过 0.1s | TOLERANT |
| codebuddy | 跳过 0.2s | 跳过 0.2s | 跳过 0.2s | TOLERANT |
| qoder | 跳过 2.9s | 跳过 2.6s | 跳过 32.5s | TOLERANT |
| opencode | 跳过 0.9s | 跳过 0.8s | 跳过 30.8s | TOLERANT |
| commandcode | 跳过 0.1s | 跳过 0.1s | 跳过 80.1s | TOLERANT |
| hermes | 跳过 4.7s | 跳过 8.3s | 跳过 64.1s | DEGRADED |

三条结论：

1. **没有后端因为坏 server 让 `session/new` 失败** —— 全部静默跳过。
   「一个写错的 command 锁死所有智能体」的担心不成立。
2. **坏 server 不毒化同一会话的好 server** —— 6/7 TOLERANT。hermes 的
   DEGRADED 与它已知的异步挂载不稳定一致。
3. **「永不响应」会拖慢建会话 30~80 秒，且无任何提示**。这是这次探针发现的
   真问题：用户看到「点发送后卡一分钟」，无法归因。需要在实现时补
   「连接后校验 + 超时放宽 + 慢启动日志」。

复现：

```bash
go test -tags integration -v -run 'TestACP_MCPProbeFailure' -timeout 1800s ./internal/ai/
```

## 复现

```bash
# 主探针：session/new + stdio（约 20 分钟，会真实调用模型）
go test -tags integration -v -run 'TestACP_MCPProbe$' -timeout 2300s ./internal/ai/

# 恢复路径：session/resume + session/load
go test -tags integration -v -run 'TestACP_MCPProbeResume' -timeout 2300s ./internal/ai/

# HTTP / SSE 传输
go test -tags integration -v -run 'TestACP_MCPProbeHTTP' -timeout 2300s ./internal/ai/

# 指定后端
MCP_PROBE_BACKENDS=claude,codex go test -tags integration -v \
    -run 'TestACP_MCPProbe$' -timeout 900s ./internal/ai/

# 给异步挂载的后端留出启动时间（秒）
MCP_PROBE_WARMUP_SECONDS=20 MCP_PROBE_BACKENDS=hermes go test -tags integration -v \
    -run 'TestACP_MCPProbe$' -timeout 400s ./internal/ai/
```

三个探针都读 `MCP_PROBE_BACKENDS`（逗号分隔）做过滤。

探针会在 agent 的本地状态里留下会话记录（`~/.claude/projects/`、`~/.codex/sessions/` 等），
**不会**写入任何 MCP 配置文件。

会话的 `cwd` 用 `t.TempDir()` 而不是仓库目录：qoder 会把项目级权限白名单写进
`<cwd>/.qoder/settings.local.json`（内容是探针 MCP server 的 allow 规则），zcode 会建
`<cwd>/.zcode/acp/sandbox.json` —— 早先版本指向 `internal/ai/` 时每次运行都会弄脏工作区。
临时目录同时加强了对照组：那里不可能存在项目级 MCP 配置。

hermes 会把探针 server 名缓存到 `~/.hermes/cache/mcp_schema_cache.json`，探针用每次运行
唯一的 server 名避免污染下一轮的对照组；清理时删掉所有以 `clawbench-probe` 开头的键即可。
