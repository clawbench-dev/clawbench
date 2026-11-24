# Cline CLI 3.0.75 · ACP 探测结果

探测时间 2026-11-24，CLI 为 `@cline/cli-linux-x64` 3.0.75（`/tmp/cli-probe/node_modules/.bin/cline`），
探测脚本 `/tmp/acp_probe.py` + 手写 JSON-RPC 会话。所有帧均为原始 wire 输出。

## 1. initialize

```json
{"protocolVersion":1,
 "agentCapabilities":{"loadSession":true,
   "promptCapabilities":{"image":true,"audio":false,"embeddedContext":false}},
 "agentInfo":{"name":"cline","version":"3.0.75"},
 "authMethods":[{"id":"cline","name":"Sign in with Cline"},
                {"id":"cline-pass","name":"Sign in with ClinePass"},
                {"id":"openai-codex","name":"Sign in with ChatGPT Subscription"}]}
```

- **没有** `sessionCapabilities` 字段。实测 `session/list` → `-32601 "Method not found": session/list`
  （方法表里有它，但 ACP 面不实现）。
- `mcpCapabilities` 未广告。

## 2. 登录门禁

- 未登录：`session/new` → `-32000 "Authentication required: Call authenticate before starting a session"`。
- 门禁判定在 CLI 里写作 `if(!this.authResult && !process.env.CLINE_API_KEY)`，`authResult` 由
  `tryRestoreAuth()` 从 `settings/providers.json` 里 **cline / cline-pass / openai-codex** 三个
  账号 provider 的凭据恢复。所以：
  - `CLINE_API_KEY` 只让门禁放行，**不改变 provider/模型**（session 仍解析为账号 provider `cline`）。
  - 三项 `authMethods` 实测都是设备码 OAuth（`authenticate` 发起后不返回结果，stderr 打印
    `https://authkit.cline.bot/device?user_code=…` 或 OpenAI 授权链接），无法在非交互环境完成。
  - `authenticate` 只接受这三个 id；传 `openai-compatible` → `-32602 Unsupported auth method`。

## 3. 登录后的会话形状（用本机已有账号态 + `CLINE_API_KEY` 让门禁放行后测得）

`session/new` 返回：

```json
{"sessionId":"1795532036842_3QDdZ_cli",
 "modes":{"availableModes":[{"id":"plan",…},{"id":"act",…}],"currentModeId":"act"},
 "models":{"availableModels":[{"modelId":"…","name":"…"}, …],"currentModelId":"cline-free/nemotron-3.5-super-vl"},
 "configOptions":[
   {"id":"provider","category":"_provider","currentValue":"cline","options":[cline, cline-pass, openai-codex]},
   {"id":"model","category":"model","currentValue":"…","options":[314 项]},
   {"id":"mode","category":"mode","currentValue":"act","options":[plan, act]},
   {"id":"auto_approve","type":"boolean","currentValue":false}]}
```

- **模型列表 314 项来自账号 provider 的目录**（含 `anthropic/claude-…`、`openai/gpt-…` 等），
  `configOptions.provider` 只有三个**账号** provider，不是模型提供商。
- 会话内切模型：`session/set_config_option {configId:"model", value:"deepseek/deepseek-chat"}` **生效**
  （返回值里 currentValue 已变）；`session/set_model` 也返回 `{}`。`session/set_mode` plan/act 生效。
- 首轮 prompt 返回 `-32000 Unauthorized: Please make sure you're using the latest version of Cline
  and re-authenticate` —— 本机没有真实账号，模型侧无法跑通，**工具调用的名字/参数形状未验证**。

## 4. BYOK / providers.json —— 实测**不驱动会话**

`~/.cline/settings/providers.json`（CLI 自己的配置面）形如：

```json
{"version":1,"lastUsedProvider":"openai-compatible","modes":{},
 "providers":{"openai-compatible":{"settings":{"provider":"openai-compatible",
   "apiKey":"…","model":"MiniMax-M2.7","baseUrl":"https://api.minimaxi.com/v1"},
   "updatedAt":"…","tokenSource":"manual"}}}
```

本机 `cline`（非 ACP）用它跑过完整会话（`cline history` 有 `provider:"openai-compatible"` 的记录），
但 **ACP 下无效**，证据：

| 环境 | session/new 的 currentModel | provider 选项 |
|---|---|---|
| 只有 `openai-compatible` 条目的 `providers.json` | `cline-free/nemotron-3.5-super-vl`（314 项账号目录） | `cline` |
| 同上 + `CLINE_DIR` / `--data-dir` 指向该目录 | 同上 | 同上 |
| 同上 + `CLINE_PROVIDER_SETTINGS_PATH` 直指该文件 | 同上 | 同上 |
| 同上 + `-P openai-compatible` | 同上（`-P` 无效） | 同上 |
| 外加 `CLINE_API_KEY` | 同上 | 同上 |

即：ACP 面绕过 `providers.json` 的 provider 选择，只认账号。**这是「不支持 BYOK」的结论**，
不是配置没写对。因此 `ProviderEnv` 用空 spec。

## 5. 模型发现（静态目录）

`cline --help` 没有列模型的子命令。ACP 的模型目录 = CLI 内嵌的 provider 目录：

- 文件：`<node root>/node_modules/@cline/llms/dist/models.js`
- 导出：`MODEL_COLLECTIONS_BY_PROVIDER_ID = { "<provider>": {provider:{…,defaultModelId}, models:{<id>:{id,name,contextWindow,…}, …}}, … }`
- 实测 3.0.75：328 个 provider，7570 条模型记录，3239 个去重 id；**每条都带 `contextWindow:`**，
  而工具/插件等无关 `{id,name}` 记录不带 —— 解析器据此区分。
- 导出是 Proxy，不能求值（会在我们进程里跑 CLI 的 JS）；按文本扫描。

## 6. 会话落盘 / `cline history --json`

- 目录：`<data root>/sessions/<sessionId>/<sessionId>.json` + `.messages.json`。
- `cline history --json`（非交互可用，空目录输出 `[]`）给 `sessionId / cwd / title / status /
  updatedAt / messagesPath`。**ACP 会话也在其中**（实测 session/new 的 id 出现在 history 里，
  `title` 为空；CLI 跑过的会话有 title）。
- ACP session id 直接就是目录名/history 里的 id。
- 会话是**全局**的（不按项目分目录），所以磁盘扫描必须按 cwd 过滤。

## 7. 其它

- `--thinking none|low|medium|high|xhigh`（`--help`）。
- `--config <path>` / `--data-dir` / `--hooks-dir`；`CLINE_DIR` 可重定位配置根（bundle 内实测）。
- 工具名、MCP（`session/new` 的 `mcpServers` 是否被采纳）**未验证**——没有可用的账号。

## 8. 实现结论

| 项 | 取值 | 依据 |
|---|---|---|
| `AcpCommand` | `cline --acp` | 握手成功 |
| `ACPLoadSession` | true | initialize 广告 + 无 -32601 |
| `LoginCmd` | `cline` | 裸 CLI 打开登录/引导 UI |
| `ProviderEnv` | `{}`（不支持 BYOK） | 第 4 节 |
| `ModelSource` | `PluginSource`（读 `models.js`） | 第 5 节 |
| `ListSessions` | `cline history --json` + cwd 过滤 | 第 6 节 |
| `OrphanCmdline` | `{cline, --acp}` | — |
| `ThinkingEffortLevels` | none/low/medium/high/xhigh | `--help` |
| `ACP` / `Behavior` / `MidTurn` | 不写 | 无证据 |

未验证：历史回放（loadSession 是否重放）、工具帧形状、MCP 注入。
