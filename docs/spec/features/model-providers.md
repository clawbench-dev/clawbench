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
| `api_key` | 凭据，默认**不回显**（列表与写回执只返回 `hasKey`）；只有显式 `GET /api/providers/{id}?reveal=1` 才返回明文，供设置页「查看密钥」使用 |
| `auth_mode` | `auto` / `x-api-key` / `bearer` |
| `wire_api` | `responses`（默认）/ `chat`；只影响 codex 托管配置块，决定它走 Responses API 还是 `/chat/completions` |

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

## 提供商自带模型清单

`providers.models`（有序 JSON 数组）+ `providers.default_model`。

- 提供商编辑页可「选择模型」：从实测 `/models` 拉取，失败用权威目录兜底，也可手填；保存后即为该端点的模型集合
- 绑定该提供商的智能体直接继承这份清单（CLI 与 ACP 未上报时都适用），模型下拉不会再出现端点跑不了的 id
- 默认模型：`preferred_model` 在清单内 → 用它；否则用 `default_model`；再否则用清单第一个
- **ACP 上报后仍以 ACP 为准**；清单为空时退回原来的发现逻辑
- 摘要模型从「提供商 → 模型」里选：清单非空的提供商只列清单内模型（校验同样只放行清单内）；清单为空的提供商按 `GET /api/providers/{id}/models` 解析（实测 → 目录兜底）后列出，解析不到时退化为手填模型 id —— 只按清单过滤会把「新建但没编目」的提供商整个藏掉，表现为「明明配了提供商却提示没有可用的提供商」

## 权威模型目录（快照）

来源：`https://models.dev/api.json`（OpenCode 同源），约 6 MB。线上原始 234 个供应商 / 8900+ 模型，按本应用支持的三种协议过滤后约 211 个供应商 / 6900 个模型。

- 快照落在 `{data-dir}/catalog/models.json`，含 `fetched_at` / `source` / 每个供应商的模型（id、名称、上下文与输出上限）
- 启动时同步读盘（冷启动也有列表），过期（7 天）或缺文件时**后台**刷新，不阻塞启动
- `GET /api/catalog` 返回状态（供应商数、时间、是否过期）；`POST /api/catalog` 立即刷新
- 映射靠 `providers.preset_id`：从预设创建的 provider 记住 models.dev 的供应商 id；手填端点没有映射，不会误用别人的目录

**手填模型**：列表永远不是封闭的。摘要模型与「绑定了提供商的智能体」都保留「自定义…」入口，可输入列表之外的 id（网关的私有模型、刚上线还没进目录的模型）。绑定提供商时后端接受任意非空模型 id；未绑定时仍按发现列表校验，避免把拼写错误存成一个跑不起来的模型。

**优先级**：ACP 上报的模型 > 目录。目录只在两个地方出现：

1. 提供商 `/models` 探测失败或返回空时，`GET /api/providers/{id}/models` 用目录兜底（响应 `source: "catalog"`）
2. ACP 智能体尚未上报模型时，`/api/agents` 额外返回 `catalogModels`，设置页用它预填默认模型；回合仍不指定模型，等 ACP 确认后才生效

## AI 摘要（共享摘要模型）

语音摘要、推荐回复、会话自动命名与 `/btw` 共用 `ai_summary` 配置。它不再自带端点与密钥，只保存：

- `ai_summary.provider_id`：引用 `providers` 行，**端点、密钥、协议全部取自该行**；
- `ai_summary.model`：模型 id（自由文本，或从该提供商的 `/models` 列表中选择）。

约束与行为：

- **强制引用**：设置页不再写 `format` / `api.*`；PATCH 白名单只接受 `provider_id` 与 `model`。
- **迁移**：启动时 `MigrateAISummaryProvider` 把旧的 `ai_summary.api.base_url/key` 建成一条 provider（名称 `AI Summary`，协议按旧 `format` 或 URL 推断），重写 `config.yaml` 并清掉旧字段。迁移失败时旧的读取路径仍然生效，下次启动重试。
- **协议限制**：摘要器只会说 OpenAI / Anthropic 两种线格式；绑定 Gemini 提供商在 PATCH 时 400，运行时也视为未配置。
- **引用守卫**：摘要正在使用的 provider 不能删除（`ProviderUsedBySummary`），也不能把协议改成 Gemini。
- **模型列表**：`GET /api/providers/{id}/models` 返回该提供商声明的模型；网关不暴露列表时返回空数组与原因，设置页退化为手填。

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/providers` | 列表 + `hasKey` + `agentCount` |
| POST | `/api/providers` | 新建 |
| GET | `/api/providers/presets` | 内置预设（models.dev 快照，209 家） |
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
