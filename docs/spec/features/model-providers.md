# 模型提供商（Model Providers）

## 目标

让用户在一个地方配置「端点 + 凭据」，智能体详情页一键引用，避免为每个智能体重复配置各自的模型提供商。

与 MCP 服务同一取舍：**定义集中一处，智能体只存引用**（`agents.provider_id`）。轮换密钥只改一行，所有引用它的智能体下一轮生效。

**模型列表不切换**：提供商只替换端点与密钥，可选模型仍由该智能体后端的自身发现决定。

## 数据模型

表 `providers`（DDL 常量 `service.ProvidersDDL`）：

| 列 | 说明 |
|---|---|
| `id` | 自增主键 |
| `name` | 显示名，`COLLATE NOCASE` 唯一 |
| `protocol` | `anthropic` / `openai` / `gemini` |
| `base_url` | 端点；空表示只注入密钥 |
| `api_key` | 凭据，**永不回显**（接口只返回 `hasKey`） |
| `auth_mode` | `auto` / `x-api-key` / `bearer` |

`agents.provider_id INTEGER DEFAULT NULL`：NULL = 使用 CLI 自身配置，不注入。

内存快照 `model.ReplaceProviders` / `GetProvider` / `AgentProvider`：spawn 路径每轮读快照，不查库。写路径（CRUD）在事务后重载快照。

## 后端能力表

`internal/model/provider_env.go` 的 `providerEnvTable` 是唯一事实来源，覆盖全部已注册后端 + `custom-acp`。**双向覆盖测试**（`internal/ai/backends/provider_env_coverage_test.go`）保证新增后端必须归类。

| 后端 | 方式 | 变量 |
|---|---|---|
| claude | anthropic | `ANTHROPIC_BASE_URL` + `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` |
| codebuddy | anthropic | `CODEBUDDY_BASE_URL` + `CODEBUDDY_API_KEY` / `CODEBUDDY_AUTH_TOKEN` |
| opencode / mimo / pi / zcode | anthropic、openai | `ANTHROPIC_*` / `OPENAI_*` |
| kimi | openai、anthropic | `KIMI_BASE_URL` / `KIMI_API_KEY`；`ANTHROPIC_*` |
| deepseek (codewhale) / dsh / hermes | openai | `OPENAI_BASE_URL` / `OPENAI_API_KEY` |
| grok | openai | `GROK_XAI_API_BASE_URL` / `XAI_API_KEY` |
| antigravity | gemini | `GOOGLE_GEMINI_BASE_URL` / `GEMINI_API_KEY` |
| cursor | key-only | `CURSOR_API_KEY`（不支持自定义端点） |
| commandcode | key-only | `COMMAND_CODE_API_KEY`（完整 BYOK 仍需其 providers.json） |
| codex | **配置文件** | `~/.codex/config.toml` 托管块 + `CLAWBENCH_PROVIDER_API_KEY` |
| copilot / qoder / qodercn / vecli / openclaw / custom-acp | 不支持 | 绑定时 400 `ProviderUnsupported` |

不支持的后端**不会静默无效**：PATCH 校验直接拒绝，前端对应选项灰显并说明原因。

## 注入实现

三处子进程启动点，均在 `os.Environ()` 之后追加（重复键后者生效）：

- `internal/ai/cli_backend.go`（通用 CLI）
- `internal/ai/codex_stream.go`（codex 专用流）
- `internal/ai/acp_conn_lifecycle.go`（ACP spawn）

后端 id 取**智能体**的 `Backend`，而不是 `CLIBackend.BackendName`：自定义 `command` 可能指向包装器，但变量名由声明的后端决定。

密钥选择：

- `auto`：优先 `APIKey`，没有则 `AuthToken`
- `x-api-key` / `bearer`：显式要求，后端不支持该形式时报错（不回落，避免把密钥发进错误的头）

## ACP 连接复用门

环境变量在 exec 时固定，因此编辑提供商或重新绑定必须重建进程。`ACPConn` 记录 `providerVersion`（`model.ProviderEnvVersion` = sha256(name/protocol/baseURL/apiKey/authMode)），`ensureAliveWithSession` 每轮比对：

- 一致 → 复用
- 不一致 → 本轮开始前重连（与 MCP 的 `mcpVersion` 同款，不打断运行中的回合）

三条建立会话的路径（recover / new / SyncLoadSession）都要记录新版本，否则下一轮会无谓重连。

## 配置文件模式（Codex）

Codex 的 provider 是全局 `model_provider` 键，无法用环境变量表达。

- 写入 `~/.codex/config.toml`：托管块（`# >>> clawbench provider >>>` … `# <<<`）+ 顶层 `model_provider = "clawbench"`。
- `env_key = "CLAWBENCH_PROVIDER_API_KEY"`，**密钥不落盘**。
- 修改前备份 `<file>.clawbench-bak-<ts>`（保留 3 份），状态写入 `{data-dir}/providers/config-state.json`（原始顶层值）。
- 首次绑定需要 `confirm_config_write: true`（否则 409 `ProviderConfigConsentRequired`），前端弹确认框。
- 同一后端全局只能绑定一个提供商：冲突返回 409 `ProviderConfigConflict`。
- 解绑/删除/启动对账（`ReconcileProviderConfigs`）会移除托管块并恢复原始值。

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/providers` | 列表 + `hasKey` + `agentCount` |
| POST | `/api/providers` | 新建 |
| GET | `/api/providers/presets` | 内置预设（models.dev 快照，204 家） |
| POST | `/api/providers/test` | `GET {base}/models` 探活（诊断，不改状态） |
| GET/PUT/DELETE | `/api/providers/{id}` | 详情 / 更新（`apiKey` 空=不变，`clearApiKey`）/ 删除（被引用 409） |
| PATCH | `/api/agents` | 新增 `provider_id`（null 清除） |

## 前端

- 设置 → AI 与对话 → **模型提供商**：列表 + 内联表单（预设下拉、协议、端点、密钥、认证方式），每行可测试/编辑/删除。
- 智能体详情 → 偏好设置 → **模型提供商**：仅列出该后端支持的协议；配置文件后端选中后弹确认框。
- 预设来自 `internal/model/provider_presets.json`（由 `scripts/gen-provider-presets.mjs` 从 models.dev 生成），只预填名称/协议/端点。

## 已知限制

- Codex 的配置文件是全局的：同一后端只能有一个生效 provider。
- 模型列表不随 provider 切换；模型 id 不属于该提供商时，错误由上游返回（401/404）。
- `mimo` / `dsh` / `grok` 的变量名基于二进制/源码证据，尚未逐台真机验证；验证失败应降级为不支持，而不是保留「看起来支持」。
