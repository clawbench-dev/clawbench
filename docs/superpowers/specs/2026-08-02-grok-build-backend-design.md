# Grok Build 智能体适配设计

## 背景

Grok Build（xAI 编码代理，本机 `grok` v0.2.112）需要作为 ClawBench 的新 AI 后端接入，复用现有双传输（CLI headless + ACP stdio）适配模式，与 claude / opencode / kimi 等后端保持一致。

## Grok Build 接口调研

### Headless CLI

```bash
grok -p "prompt" --output-format streaming-json
```

stdout 输出 NDJSON 事件（每行一个 JSON 对象）：

| `type`      | `data` 字段 | 语义                                        |
| ----------- | ----------- | ------------------------------------------- |
| `text`      | string      | 响应文本增量                                |
| `thought`   | string      | 内部推理（thinking tokens）                 |
| `end`       | —           | 结束事件，携带 `sessionId`/`stopReason` 等  |
| `error`     | —           | 错误，携带 `message`                        |

注意：headless CLI **不输出 tool_use 事件**，因此 CLI 模式无法可视化工具调用。

### ACP stdio

```bash
grok agent stdio
```

完整 ACP (JSON-RPC over stdio) 协议：`session/new`、`session/prompt`、`session/update`（`agent_message_chunk`/`agent_thought_chunk`/`tool_call`/`tool_call_update`/`plan`）、权限审批。支持 `-r/--resume <id>` 续聊。

### 其他 CLI 参数

| 参数 | 说明 |
| --- | --- |
| `-m, --model <ID>` | 模型 ID |
| `--cwd <PATH>` | 工作目录 |
| `-r, --resume <ID>` | 续聊会话 |
| `--reasoning-effort / --effort <LEVEL>` | 推理强度：`none`/`minimal`/`low`/`medium`/`high`/`xhigh`/`max` |
| `--always-approve / --yolo` | 自动批准工具执行（headless 非 TTY 必需） |

### 模型发现

```bash
grok models
```

未登录时输出：

```
Default model: grok-4.5

Available models:
  * grok-4.5 (default)
```

登录后每行 `* <model-id> (default)` 或 `* <model-id>`。解析规则：以 `* ` 前缀开头的行，去掉尾部 `(default)`。

### 鉴权 / 安装

- 鉴权：`XAI_API_KEY` 环境变量 或 OAuth（`grok login`）
- 安装：`curl -fsSL https://x.ai/cli/install.sh | bash`

## 实现方案（仅 ACP 模式）

> 决策：**不实现 CLI 模式**。Grok Build 不注册 `ai.RegisterBackend` CLI factory，不提供 CLIBackend/StreamParser。聊天完全走 ACP stdio（`grok agent stdio`），模型发现单独用 `grok models` 命令。与 Pi 的"仅 CLI"形成对称——Grok 是"仅 ACP"。

### 新文件

| 文件 | 内容 |
| --- | --- |
| `internal/ai/backends/grok/register.go` | `backends.Register(&BackendPlugin{...})`：Spec + ACP 插件（GrokACPRemaps）。**无** `ai.RegisterBackend`（无 CLI factory） |
| `internal/ai/backends/grok/acp.go` | `GrokACPRemaps`：通用 ACP 输入字段重映射 |
| `internal/ai/backends/grok/discovery.go` | `DiscoverGrokModels()`：解析 `grok models` 输出 + 默认回退 |
| `internal/ai/backends/grok/acp_test.go` | 插件注册、Spec 字段、ACP 插件、remaps 测试 |
| `internal/ai/backends/grok/discovery_test.go` | `grok models` 输出解析测试（带/不带 `(default)`、未登录格式、空输出） |

### BackendSpec 规格

```go
model.BackendSpec{
    ID: "grok", Backend: "grok", DefaultCmd: "grok",
    Name: "Grok", Specialty: "xAI 编码代理",
    ThinkingEffortLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
    AcpCommand:           "grok agent stdio",
    ACPLoadSession:       true,
    InstallCmd:           "curl -fsSL https://x.ai/cli/install.sh | bash",
    SortOrder:            13,
}
```

### 运行路径

- **Agent 创建**：`SyncDiscoverAgentsDB` 检测到 `grok` 二进制 → 创建 agent，因 `AcpCommand != ""` 默认 `transport=acp-stdio`
- **聊天**：`NewBackendForAgentWithTransport` 命中 `acp-stdio` + `SupportsACP()` → 直接创建 `ACPBackend`，不经过 CLI factory（无需 `ai.RegisterBackend`）
- **工具名归一化**：Grok 使用标准 ACP 工具（title/kind），共享 `acp_tool_names.go` 前缀匹配已覆盖 Read/Write/Edit/Bash 等，走 `parseGenericACPToolCall`，无需 per-agent 解析函数或 ToolCallIDPrefixes

### 模型发现

`grok models` 输出解析（登录后每行 `* <model-id>` 或 `* <model-id> (default)`）。未登录/命令失败 → 回退默认列表：

```go
var grokDefaultModels = []model.AgentModel{
    {ID: "grok-4.5", Name: "Grok 4.5"},
    {ID: "grok-build", Name: "Grok Build"},
}
```

### 修改文件

| 文件 | 修改 |
| --- | --- |
| `cmd/server/main.go` | 添加 `_ "clawbench/internal/ai/backends/grok"` |
| `web/src/utils/agentIcons.ts` | 添加 grok 图标（`grok.svg`，monochrome + needsBg） |
| `web/src/utils/__tests__/agentIcons.test.ts` | 后端列表加入 `grok` |

## 测试

- **acp_test.go**：插件注册（`backends.Lookup("grok")`）、Spec 字段断言、ACP 插件非空、GrokACPRemaps 键断言
- **discovery_test.go**：带 `(default)` / 不带 / 未登录格式 / 空输出解析；默认回退模型

## 已知限制

- 仅 ACP 模式，无 CLI 回退——若用户将 transport 强制改为 `cli`，会报 "unsupported backend type: grok"（预期行为，Pi 的 CLI-only 是对称情形）
- `grok models` 未登录时只显示默认模型，登录后模型列表由 CLI 返回
