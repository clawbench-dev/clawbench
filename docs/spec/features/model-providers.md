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

每个后端在自己的 `BackendPlugin.ProviderEnv` 里声明能力（`internal/ai/backends/<id>/`），
`model.RegisterProviderEnvSpec` 汇总成注册表（`internal/model/provider_env.go` 只留注册与查表），
覆盖全部已注册后端 + `custom-acp`（后者没有插件，条目留在 `model` 里）。**双向覆盖测试**
（`internal/ai/backends/provider_env_coverage_test.go`）保证新增后端必须归类。

| 后端 | 方式 | 变量 |
|---|---|---|
| claude | **配置目录**（+ anthropic 环境变量兜底） | `settings.json` 的 `env` 段 + `CLAUDE_CONFIG_DIR` |
| codebuddy | **配置目录**（+ 私有协议） | `models.json`（逐模型 `url`）+ `CODEBUDDY_CONFIG_DIR`；环境变量 `CODEBUDDY_*` 仍作为兜底；私有协议走 `CODEBUDDY_API_KEY` + cloudhosted |
| opencode / mimo / pi / zcode | anthropic、openai | `ANTHROPIC_*` / `OPENAI_*` |
| kimi | **配置目录**（+ OpenAI 环境变量） | `config.toml` + `KIMI_SHARE_DIR`；`KIMI_BASE_URL` / `KIMI_API_KEY` 仅 `type="kimi"` 时生效 |
| pi | **配置目录** | `models.json` + `PI_CODING_AGENT_DIR` |
| deepseek (codewhale) / dsh / hermes | openai | `OPENAI_BASE_URL` / `OPENAI_API_KEY` |
| grok | openai | `GROK_XAI_API_BASE_URL` / `XAI_API_KEY` |
| antigravity | gemini | `GOOGLE_GEMINI_BASE_URL` / `GEMINI_API_KEY`；也支持私有协议 |
| cursor | key-only | `CURSOR_API_KEY`（不支持自定义端点）；也支持私有协议 |
| commandcode | key-only | `COMMAND_CODE_API_KEY`（完整 BYOK 仍需其 providers.json）；也支持私有协议 |
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

## 配置目录模式（codebuddy / kimi / pi / claude）

这四个后端有**配置目录开关**（`CODEBUDDY_CONFIG_DIR` / `KIMI_SHARE_DIR` / `PI_CODING_AGENT_DIR` / `CLAUDE_CONFIG_DIR`），
所以不需要像 codex 那样改用户的文件：每个智能体在 `{data-dir}/providers/agents/<agent-id>/` 下拿到一个**私有目录**，
里面只写 ClawBench 自己的那一个文件，其余条目**符号链接**回用户自己的配置目录。

- 为什么用符号链接而不是复制：这些目录里有登录态（kimi 的 `credentials` / `device_id`）和用户技能，
  codebuddy 那份实测 18 GB（blobs / file-history）。复制会既占空间又把登录冻结在复制那一刻。
- 为什么用目录而不是改用户文件：用户那份是**全局唯一**的，两个智能体无法指向不同提供商（codex 的已知限制就是这么来的）。
  私有目录让「一智能体一提供商」真正成立。
- 目录布局由 `model.ProviderUsesConfigDir` / `ProviderConfigDirEnv` / `ProviderConfigDirFile` 决定，
  实现在 `internal/service/provider_config_dir.go`。
- `model` 不能 import `service`（会成环），所以写入函数通过 `model.ConfigDirEnvForAgent` 这个函数变量注入。

| 后端 | 文件 | 协议 | 密钥 |
|---|---|---|---|
| codebuddy | `models.json` | 仅 OpenAI | `${CLAWBENCH_PROVIDER_API_KEY}` 引用，不落盘 |
| kimi | `config.toml` | OpenAI + Anthropic | **明文**（该文件没有 `${ENV}` 语法） |
| pi | `models.json` | OpenAI + Anthropic | `$CLAWBENCH_PROVIDER_API_KEY` 引用，不落盘 |
| claude | `settings.json` 的 `env` 段 | 仅 Anthropic | **明文**（`${VAR}` 会被原样发出去，实测 401） |

各后端的实测约束（都是真机验证过才写进表里的）：

- **codebuddy**：每个模型自带 `url`，且这个 `url` 的优先级**高于** `CODEBUDDY_BASE_URL`——这是它能做多提供商的唯一通道。
  它的请求路径恒为 `/chat/completions`，`models.json` 里的 `provider` 字段填 `"anthropic"` 也照样走这条路径，
  所以能力表里它**只声明 OpenAI**（之前被错标成 Anthropic，界面上能选到一个永远不通的组合）。
- **kimi**：`KIMI_*` 变量只在配置里 `type = "kimi"` 时生效，`type = "anthropic"` 时**完全不读环境变量**。
  因此 `ANTHROPIC_*` 那行注入已从能力表删除——它以前让绑定「成功」而端点被静默忽略。
  隔离目录必须包含 `credentials` / `device_id`（已由符号链接覆盖），否则 `session/new` 直接 `-32000`。
- **pi**：模型寻址是 `<provider>/<model>`，所以选择器下发的是 `clawbench/<model>`（`ProviderModelIDFor`）。
- **claude**：它是唯一一个「配置文件不是环境变量的替代品、而是必需品」的后端——只设 `ANTHROPIC_BASE_URL`
  能建起会话，但回合永远不出结果（适配器从合并后的 settings 读端点，不读原始环境变量）。所以不接这条，
  claude 的 Anthropic 协议绑定实际上是不可用的。
  它的 `settings.json` 只部分属于 ClawBench：`env` 段归我们，`permissions` / `enabledPlugins` / `modelOverrides`
  等**从用户自己的 `~/.claude/settings.json` 原样带过来**——私有目录是新建的，只写 `env` 会让这个智能体
  突然失去全部审批规则，用户会理解成「ClawBench 把我的权限关了」。`env` 段里用户自己的其它变量（代理等）也保留。
  它**不要求模型清单**（模型按会话选），所以 `ConfigDirDeclaresModels` 是逐后端的开关而非「有配置文件就有」。

绑定行为：

- **必须声明模型**（codebuddy / kimi / pi，**不含 claude**）：这三个后端的配置文件要列出模型，CLI 才能解析模型 id；
  空清单的 provider 块在它们眼里等于不存在。
  所以绑定一个没有模型清单的提供商会在**绑定时**被拒绝（400 `ProviderInvalid`，`detail.field = "models"`），
  而不是等到对话时才失败。判据与写入侧完全一致（清单为空时退回到默认模型，所以只填默认模型也算数）。
- **不能清空已有清单**：已经绑定了这类智能体的提供商，清空模型清单会被拒绝（409 `ProviderModelsRequired`），
  否则那个智能体下一轮就跑不起来。只看「配置目录后端」的智能体，环境变量后端不受影响。
- 只有**密钥必须明文落盘**的后端（kimi、claude）才要求 `confirm_config_write`，前端弹的是一份专门的说明
  （文件在 ClawBench 数据目录内、不会改用户的配置、但密钥会写进这个文件）。
- **没有跨智能体冲突检查**：每个智能体一个目录，两个智能体指向不同提供商是正常用法。
- 解绑 / 删除智能体 / 启动对账都会清掉对应目录；agent 已不存在的孤儿目录由 `pruneOrphanConfigDirs` 清掉，
  避免删了智能体还留一份密钥在磁盘上。

## 提供商自带模型清单

`providers.models`（有序 JSON 数组）+ `providers.default_model`。

- 提供商编辑页可「选择模型」：从实测 `/models` 拉取，失败用权威目录兜底，也可手填；保存后即为该端点的模型集合
- **清单不遮挡实测列表**：`GET /api/providers/{id}/models` 恒返回端点实测（或目录兜底）的 `models`，另用 `curated` 单独返回已保存清单。否则选择过一次并保存后，再打开选择器就只剩已选项，无法继续添加（实测缺陷）
- 绑定该提供商的智能体直接继承这份清单（CLI 与 ACP 未上报时都适用），模型下拉不会再出现端点跑不了的 id
- 默认模型：`preferred_model` 在清单内 → 用它；否则用 `default_model`；再否则用清单第一个
- **绑定不清除后端原生模型**：`preferred_model` 若属于该智能体自己发现的模型（如 opencode 的 `opencode/<model>`，出现在 `agents.models` 里），按原值跑、不加提供商前缀。加了前缀等于让注入的端点收到一个它从没提供过的模型名，上游直接 400；而 1:1 聊天用的是会话已存的模型，所以同一智能体只在「走默认兜底」的路径上翻车——群聊成员行、调度任务就是这两条（issue #651 群聊复现）。只有既不在提供商清单、也不在智能体自报列表里的 id 才是「自定义…」，才需要加前缀并声明进配置内容
- **ACP 上报后仍以 ACP 为准**；清单为空时退回原来的发现逻辑
- 摘要模型从「提供商 → 模型」里选：清单非空的提供商只列清单内模型（校验同样只放行清单内）；清单为空的提供商按 `GET /api/providers/{id}/models` 解析（实测 → 目录兜底）后列出，解析不到时退化为手填模型 id —— 只按清单过滤会把「新建但没编目」的提供商整个藏掉，表现为「明明配了提供商却提示没有可用的提供商」

## 权威模型目录（快照）

来源：`https://models.dev/api.json`（OpenCode 同源），约 6 MB。线上原始 234 个供应商 / 8900+ 模型，按本应用支持的三种协议过滤后约 211 个供应商 / 6900 个模型。

- 快照落在 `{data-dir}/catalog/models.json`，含 `fetched_at` / `source` / 每个供应商的模型（id、名称、上下文与输出上限）
- 启动时同步读盘（冷启动也有列表），过期（7 天）或缺文件时**后台**刷新，不阻塞启动
- `GET /api/catalog` 返回状态（供应商数、时间、是否过期）；`POST /api/catalog` 立即刷新
- 映射靠 `providers.preset_id`：从预设创建的 provider 记住 models.dev 的供应商 id；手填端点没有映射，不会误用别人的目录

**模型单价**：同一份快照还带各模型的输入 / 输出报价（每百万 token）。模型选择器展示的单价是**下发时派生**的，**从不落库**——快照每周刷新，一旦存进 `agents.models`，价格就永久冻结在保存那天。取值规则：

- 优先取**模型自己厂商**的标价（按 id 家族判定），取不到才用多来源共识价
- 共识价、以及按显示名匹配到的价格一律标 `~`，不冒充官方报价
- 只有一个渠道报价、或 0/0（订阅套餐）**一律不标**；`auto` / `default` 这类通用 id 不参与匹配
- 宁可空着也不给一个会被读成「免费」或「官方价」的数字

**模型倍率**：CodeBuddy 这类按积分计费的智能体会在自己的模型列表里带一个相对倍率（`x0.06 credits`）。选择器**优先显示倍率，没有倍率时才退回美元单价**——两者口径不同，一行也只放得下一个，而倍率才是能和客户端对照的数字。与单价相反，倍率是**智能体自己的元数据**：随模型列表落库，并在每次发现 / ACP 握手时刷新，不是目录派生。来源有两处：CodeBuddy 的 product 配置与运行时缓存（`credits` 字段），以及 ACP 握手的 `availableModels[]._meta.credits`。**后者是唯一的来源**——同一个列表还会以 `model` 配置项下发，但配置项里没有 `_meta`，所以配置项路径必须再用扩展列表按 id 补齐，否则倍率会在提取时丢掉。其他智能体目前不上报倍率，仍显示美元单价。

**手填模型**：列表永远不是封闭的。摘要模型与「绑定了提供商的智能体」都保留「自定义…」入口，可输入列表之外的 id（网关的私有模型、刚上线还没进目录的模型）。绑定提供商时后端接受任意非空模型 id；未绑定时仍按发现列表校验，避免把拼写错误存成一个跑不起来的模型。

**模型实测**：编辑弹窗里每个模型 chip 都能单独测试。测试按该提供商的协议真实发送一次最小对话请求（openai → `/chat/completions`，anthropic → `/messages`，gemini → `/v1beta/models/{model}:generateContent`），返回 `{ok, model, status, latencyMs, reply?|error?}`。这是唯一能回答「这个模型在这套端点 + 密钥下能不能用」的方式——网关可能列出了却服务不了，也可能服务得了却没列出，所以 `/models` 探活不能替代它。请求会带上弹窗里未保存的 `protocol` / `baseUrl` / `apiKey` / `authMode`，因此可以先测再保存；为空则用已保存的行。认证头与真实调用共用 `applyProviderAuth`，两边不会各写一套。

**优先级**：ACP 上报的模型 > 目录。目录只在两个地方出现：

1. 提供商 `/models` 探测失败或返回空时，`GET /api/providers/{id}/models` 用目录兜底（响应 `source: "catalog"`）
2. ACP 智能体尚未上报模型时，`/api/agents` 额外返回 `catalogModels`，设置页用它预填默认模型；回合仍不指定模型，等 ACP 确认后才生效

## AI 摘要（共享摘要模型）

语音摘要、推荐回复、会话自动命名与 `/btw` 共用 `ai_summary` 配置。它不再自带端点与密钥，只保存：

- `ai_summary.provider_id`：引用 `providers` 行，**端点、密钥、协议全部取自该行**；
- `ai_summary.model`：模型 id（自由文本，或从该提供商的 `/models` 列表中选择）。

约束与行为：

- **强制引用**：设置页不再写 `format` / `api.*`；PATCH 白名单只接受 `provider_id` 与 `model`。
- **端点回退（无启动迁移）**：`ai_summary.api` 端点块只在未绑 provider 时作为回退读取（provider 优先），设置页从不写入；更早的 `summarize.tts_api` / `tts_model` 键已删除、不再读取，legacy agent 后端值（claude/codebuddy/…）直接丢弃并落回 `simple`。`tts_backend="api"` 而摘要模型未配置时，启动与热重载都降级为 simple 并打 ERROR，进程不退出。
- **协议限制**：摘要器只会说 OpenAI / Anthropic 两种线格式；绑定 Gemini 提供商在 PATCH 时 400，运行时也视为未配置。
- **引用守卫**：摘要正在使用的 provider 不能删除（`ProviderUsedBySummary`），也不能把协议改成 Gemini。
- **模型列表**：`GET /api/providers/{id}/models` 返回该端点实测（或目录兜底）的模型，另带已保存清单 `curated`；网关不暴露列表时返回空数组与原因，设置页退化为手填。

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/providers` | 列表 + `hasKey` + `agentCount` |
| POST | `/api/providers` | 新建 |
| GET | `/api/providers/presets` | 内置预设（models.dev 快照，209 家） |
| POST | `/api/providers/test` | 不带 `model`：`GET {base}/models` 探活；带 `model`：按协议真实发一次最小对话请求（诊断，不改状态） |
| GET/PUT/DELETE | `/api/providers/{id}` | 详情 / 更新（`apiKey` 空=不变，`clearApiKey`）/ 删除（被引用 409） |
| PATCH | `/api/agents` | 新增 `provider_id`（null 清除） |

## 私有协议（自有 Token）

提供商表单的协议除了 anthropic / openai / gemini，还有一个 **私有协议**（`private`）。它不是线上协议，
而是「这份凭据属于该后端自己的官方服务」：没有端点可填、没有认证方式可选、也不需要列模型。

每个支持它的后端在 `ProviderEnv` 里声明 `OwnServiceEnv`（要注入的变量）和可选的 `OwnServiceExtra`
（固定开关）：

| 后端 | 注入 | 说明 |
|---|---|---|
| codebuddy | `CODEBUDDY_API_KEY` + `CODEBUDDY_INTERNET_ENVIRONMENT=cloudhosted` | 还要**跳过配置目录写入**（它的 `models.json` 要求每个模型自带 URL，装不下这种模式） |
| cursor | `CURSOR_API_KEY` | 本来就没有自定义端点 |
| commandcode | `COMMAND_CODE_API_KEY` | 完整 BYOK 仍需它自己的配置文件，私有协议只负责凭据 |
| antigravity | `GEMINI_API_KEY` | 走 Google 自己的服务 |

`SupportedProtocols` 只对声明了 `OwnServiceEnv` 的后端列出 `private`；其他后端的智能体看不到、也绑不上。
绑定时都不会弹「要写配置文件」的确认框。

一个私有协议的提供商可以绑给同一后端的多个智能体，换 key 只改一处——和其他智能体的「自有 key」完全同一套机制。
表单在选中私有协议后会隐藏端点、认证方式、模型和「测试连接」，提交时强制 `base_url=''`、`auth_mode=auto`。

## 前端
- 设置 → AI 与对话 → **模型提供商**：列表整行可点即编辑，右侧只留测试 / 删除两个图标；密钥状态是「圆点 + 文字」，目录状态在列表底部。
- 编辑是**一个弹窗里的三个视图**（表单 / 预设 / 模型），不是三层叠加的弹窗：头部返回箭头与 Android 返回键都先回表单，不叠第二层遮罩。
  - **表单**按「连接 / 凭据 / Codex 调用方式 / 模型」分段，每个控件都有可见标签（早期只有 placeholder，填完就不知道这格是什么）；协议是三段式；密钥状态显式显示，「清除密钥」是可撤销的显式动作而不是随手点一下。
  - **模型**内联管理：搜索、全选 / 清空、星标设默认、逐模型实测（结果就地显示）。表单里只留已选 chips 与默认标记——模型与默认模型分散在两个地方，用户永远不知道哪个才是生效的。
  - 底部「测试连接」用**当前未保存的草稿值**验证端点，所以可以先测再存。
  - 创建成功后不关弹窗，直接转入模型视图并提示「已创建」——省掉「关掉 → 找到行 → 再编辑 → 再开弹窗」这一圈。
- 智能体详情 → 偏好设置 → **模型提供商**：仅列出该后端支持的协议；配置文件后端选中后弹确认框。
  私有协议只对 CodeBuddy 列出，且不会弹配置文件确认（见上）。
- 预设来自 `internal/model/provider_presets.json`（由 `scripts/gen-provider-presets.mjs` 从 models.dev 生成），只预填名称/协议/端点。

## 已知限制

- Codex 的配置文件是全局的：同一后端只能有一个生效 provider。**codebuddy / kimi / pi / claude 不受此限制**——
  它们走配置目录模式（见上），每个智能体一份，可以各自指向不同提供商。
- 模型列表不随 provider 切换；模型 id 不属于该提供商时，错误由上游返回（401/404）。
- `mimo` / `dsh` / `grok` 的变量名基于二进制/源码证据，尚未逐台真机验证；验证失败应降级为不支持，而不是保留「看起来支持」。
- **zcode / grok 的配置目录开关已确认存在，但回合未跑通**，因此未纳入配置目录模式（见
  [`docs/dev/multi_provider_config_research.md`](../../dev/multi_provider_config_research.md) 第八节）。
- **opencode 只能走「配置内容」通道**：`OPENCODE_CONFIG_DIR` / `OPENCODE_CONFIG` / `XDG_CONFIG_HOME` 在 2.2.7 上都没能换掉配置目录。
  另外它默认连常驻后台服务，手工用环境变量验证时必须加 `--standalone`，否则会误以为注入不生效（`opencode acp` 不受影响）。
