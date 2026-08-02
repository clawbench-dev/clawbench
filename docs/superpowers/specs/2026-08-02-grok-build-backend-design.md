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

## 实现方案

### 新文件

| 文件 | 内容 |
| --- | --- |
| `internal/ai/backends/grok/cli.go` | `ai.RegisterBackend("grok", newGrokBackend)` + `backends.Register(...)`；BackendSpec 定义；`newGrokBackend()` 返回 CLIBackend；`buildGrokStreamArgs` |
| `internal/ai/backends/grok/stream.go` | `GrokStreamParser`：解析 `{type,data}` NDJSON → StreamEvent |
| `internal/ai/backends/grok/discovery.go` | `DiscoverGrokModels()`：解析 `grok models` 输出 + 默认回退 |
| `internal/ai/backends/grok/cli_test.go` | 插件注册、CLIBackend 结构、args 构造测试 |
| `internal/ai/backends/grok/stream_test.go` | 各事件类型解析测试 |
| `internal/ai/backends/grok/discovery_test.go` | `grok models` 输出解析测试 |

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

### CLI 参数构造

```go
args := []string{
    "-p", prompt,          // InjectSystemPrompt(req)
    "--output-format", "streaming-json",
    "--always-approve",    // headless 非 TTY 必须
}
if req.Resume && req.SessionID != "" { args = append(args, "--resume", req.SessionID) }
if req.WorkDir != "" { args = append(args, "--cwd", req.WorkDir) }
if req.Model != "" { args = append(args, "-m", req.Model) }
if req.ThinkingEffort != "" { args = append(args, "--reasoning-effort", req.ThinkingEffort) }
```

### ACP 映射

`AcpCommand: "grok agent stdio"`。Grok 使用标准 ACP 工具命名（`tool_call`/`tool_call_update` 带 title/kind），共享 `acp_tool_names.go` 的 title 前缀匹配已覆盖 Read/Write/Edit/Bash 等，无需额外 ToolCallIDPrefixes。ACP Plugin 注册 `InputRemaps` 用通用映射。

### 修改文件

| 文件 | 修改 |
| --- | --- |
| `cmd/server/main.go` | 添加 `_ "clawbench/internal/ai/backends/grok"` |
| `web/src/utils/agentIcons.ts` | 添加 grok 图标（`grok.svg`，monochrome + needsBg） |
| `web/src/utils/__tests__/agentIcons.test.ts` | 后端列表加入 `grok` |

## 测试

- **stream_test.go**：`text`/`thought`/`end`/`error`/未知类型/无效 JSON 各事件类型解析断言
- **cli_test.go**：插件已注册、`*ai.CLIBackend` 类型、Cmd=`grok`、FilterLine 逻辑、args 构造（resume/workdir/model/effort 条件分支）
- **discovery_test.go**：带 `(default)` / 不带 / 未登录格式 / 空输出解析；默认回退模型

## 已知限制

- CLI 模式无工具调用可视化（grok headless 不输出 tool 事件）；ACP 模式功能完整
- `grok models` 未登录时只显示默认模型，登录后模型列表由 CLI 返回
