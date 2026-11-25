# MiniMax Code ACP 探测结论（2026-11-24）

> 后端 id `minimax`，npm 包 `@minimax-ai/code`，bin 名 `mcode`，版本 **0.7.9**。
> 探测脚本 `/tmp/acp_probe.py`（握手 / 单轮）＋三个自写小脚本（多工具轮、MCP 探针、MCP 对照组），
> 全部在隔离数据目录下跑：`MINIMAX_DATA_DIR=/tmp/probe-minimax/data`。

## 1. 握手（`mcode acp`）

```json
{"protocolVersion":1,
 "agentCapabilities":{
   "loadSession":true,
   "mcpCapabilities":{"http":true,"sse":true},
   "promptCapabilities":{"image":true,"audio":false,"embeddedContext":false},
   "sessionCapabilities":{"list":{},"fork":{},"resume":{},"close":{}}},
 "agentInfo":{"name":"minimax-code","title":"MiniMax Code","version":"0.7.9"},
 "_meta":{"minimax-code/extensions":{
   "version":1,
   "methods":["session/activate","mcode/session/activate","mcode/session/steer",
     "mcode/session/queue/{list,enqueue,update,delete,steer}",
     "mcode/session/goal/{get,create,patch,clear}",
     "mcode/session/delegation/{get,stop,message,transcript}"],
   "notifications":["mcode/session/current_session_update","mcode/session/queue_update",
     "mcode/session/goal_update","mcode/session/delegation_update"]}}}
```

- **没有 `authMethods`**：凭据不经过 ACP 协商。
- `session/new` 未配置凭据时返回 `-32000 "Authentication required: Run `mcode login` and try again."`

## 2. 鉴权

- 只设 `MINIMAX_API_KEY` **不够**：`mcode exec` 直接报
  `Sign in to MiniMax to use Agent features. Run 'mcode login', then retry.`
  包里 `MINIMAX_API_KEY` 只出现在一份 **其他厂商** 的 env 映射表里（provider key → env 名），不是 mcode 自己的凭据入口。
- **BYOK 走 `mcode provider add`**（写自己的 `config.yaml`）：

  ```bash
  MINIMAX_DATA_DIR=<dir> mcode provider add \
    --name ClawBench --base-url https://api.minimaxi.com/anthropic \
    --api-format anthropic-messages --model MiniMax-M3 \
    --api-key-env MINIMAX_API_KEY --support-image
  ```

  产物（`<dir>/config.yaml`）：`custom_provider.clawbench = {api: anthropic-messages, options:{apiKey:<env 引用>, baseURL, authMode: api-key}, models:{...}}`，
  `provider list` 显示 `custom_provider:clawbench  enabled  sk-c****WSJo`（**--api-key-env 只存 env 引用**）。
  再把 `defaultModel` 改成 `custom_provider:clawbench/MiniMax-M3`。
- 实测：这样配好之后 `mcode exec` 与 ACP `session/new + session/prompt` 都能正常跑完一轮，
  密钥来自环境变量 `MINIMAX_API_KEY`（值从未打印）。
- 结论：**环境变量只能提供凭据；端点/模型必须在 mcode 的 config.yaml 里声明**。
  环境里没有可让 mcode 改推理端点的变量（`MCODE_API_BASE_URL` 是它自己的鉴权服务地址，不是推理端点）。
- **因此 ClawBench 的提供商面板不支持 MiniMax Code**（`ProviderEnv: &model.ProviderEnvSpec{}`）。
  只注环境变量不生效，而如果声明成「OpenAI 协议 + MINIMAX_API_KEY」，UI 会允许绑定一个端点、
  注入 key，mcode 却仍连它自己 config 里的端点——用户以为切了其实没切，属于静默失败。
  BYOK 目前要用户自己在 mcode 里配（上面的 `mcode provider add` + `defaultModel`）。
- 后续可做：`MINIMAX_DATA_DIR` 实测能整体重定位配置树（新目录下自己生成了 config.yaml / auth/ / v2/），
  理论上可以升级为 `ConfigDirEnv + ConfigDirFile` + 写 `custom_provider.*` 与 `defaultModel` 的
  `ConfigFileApply`；本轮未做（没有恢复钩子，也没端到端跑过一整轮）。

## 3. 模型发现

- 没有 `mcode models`。`mcode --help` 只有 `provider` 子命令；`provider list --json` 列的是 **provider**，
  内置两个（`minimax_oauth` / `minimax_api`）的 `models` 是空数组，只有自定义 provider 才带模型。
- 真正的模型清单在 **ACP `session/new` 返回的 `configOptions`** 里（供界面直接消费）：

  ```json
  {"type":"select","id":"model","name":"Model","category":"model",
   "currentValue":"m:custom_provider%3Aclawbench:MiniMax-M3:v:thinking",
   "options":[{"value":"m:minimax:MiniMax-M3.1-Flash:v:","name":"M3.1-Flash"},
              {"value":"m:minimax:MiniMax-M3.1-Flash:v:thinking","name":"M3.1-Flash · thinking"},
              {"value":"m:minimax:MiniMax-M3:v:","name":"M3"},
              {"value":"m:minimax:MiniMax-M3:v:thinking","name":"M3 · thinking"}, ...]}
  ```

  同一个 `session/new` 还带 `configOptions[0] = permissionMode`（`default`/`auto`/`bypassPermissions`，
  `_meta.minimax-code/scope = process`）和 `modes`（`default` / `plan`）。
- 因此 ClawBench 注册 **静态目录**（`MiniMaxCatalog` = M3.1-Flash、M3）只用于首次握手前的选择器，
  运行时以 ACP 上报为准：`ResolveModels` 拿到 `configOptions` 后以它决定成员/顺序/显示名，
  静态项并不保留。所以这个目录**不要删也不要扩**，它只描述「出厂默认」。
- ⚠️ 注意：ACP 上报的模型 id 是 **编码后的 `m:<provider>:<model>:v:<variant>`** 形式，
  `session/set_model` 需要什么形式未验证（ClawBench 走 config option 路径时不涉及）。

## 4. 工具调用帧（真实抓到，MiniMax-M3 / BYOK）

一次要求「读文件 → 写文件 → 跑 shell」的轮次：

```
tool_call          {toolCallId:"call_01a20e…", title:"read",  name:"read",  kind:"read"}
tool_call_update   {… "status":"in_progress", rawInput:{"path":"/tmp/…/config.yaml"}, locations:[{path:…}]}
tool_call_update   {… "status":"completed",  rawInput:{…}, rawOutput:{content:[{type:"text",…}],
                     details:{textFile:{path,content,numLines,startLine,totalLines,…}}}}

tool_call          {… title:"write", name:"write", kind:"edit"}
tool_call_update   {… rawInput:{"path":…,"content":"hello\n"}, content:[{type:"content",content:{type:"text",…}}]}

tool_call          {… title:"bash",  name:"bash",  kind:"execute"}
tool_call_update   {… rawInput:{"command":"echo done","description":"Echo done"},
                     rawOutput:{content:[{type:"text","text":"done\n"}]}}
```

- 工具名在 `name` 和 `title` 里都是**小写单词**，公共小写别名表已覆盖（read/write/bash…）；
  `rawInput` 字段是 `path` / `content` / `command`，不需要 `InputRemaps`。
- 工具开始前连续多条**空 `tool_call_update`**（只有 toolCallId，无任何字段）——前端把它们当无变化即可。
- `agent_message_chunk` 按 **消息级 `messageId`** 分段（一条回复可能有两个 messageId，中间夹工具调用）。

## 5. MCP（`mcpServers` 注入）

用仓库自带的 `internal/ai/testdata/mcp_probe_server.mjs` 做探针组与对照组：

| 组 | `session/new.mcpServers` | 结果 |
|---|---|---|
| 对照组 | `[]` | 模型找不到 `clawbench_mcp_probe_ping`，回复「Search returned only browser」，**标记未出现** |
| 探针组 | `[{name:"probe", command:"node", args:[probe_server.mjs], env:[]}]` | 第一轮即调用 `mcp__probe__clawbench_mcp_probe_ping`，原样复述 **`CLAWBENCH_MCP_PROBE_OK`** |

结论：**MCP 可用且首轮即可见**。注入的工具名形如 `mcp__<server>__<tool>`，`kind:"other"`。

## 6. 会话存储（供 ListSessions 判断，未实现）

- `session/list` **可用**，按 cwd 返回 `{sessionId, cwd, title?, updatedAt}`（无标题的会话没有 `title` 字段）。
- 磁盘：`<data-dir>/v2/sessions/YYYY/MM/DD/<ts>-session_<b64>/manifest.json`，
  另有 sqlite `<data-dir>/v2/sqlite/runtime-state.sqlite` 的 `local_runtime_sessions`
  表（`session_id, workspace_dir, title, archived, visibility, updated_at_ms`）。
- 因为 RPC 已覆盖，ClawBench 不需要磁盘扫描（`ListSessions` 留空）。

## 7. 私有扩展（只登记，未接）

- `mcode/session/steer`：看起来是「轮中插话」。公共 `MidTurn` 需要「注入边界」才能切分回复，
  没有实测到回执（和 claude/codex 的 `_session/steering` 同型风险），**未接**。
- `mcode/session/queue/*`、`goal/*`、`delegation/*`：ClawBench 没有对应宿主机制，**不接**。

## 8. 崩溃残留清理

`AcpCommand` 是 `mcode acp`，spawn 后 argv 形如 `mcode\0acp`（主信号仍是 `CLAWBENCH_CHILD=1`）。
已登记命令行兜底 `{Binary:"mcode", Arg:"acp"}`：两个子串都要求，「mcode」单独会误配 `mcode exec`，
裸「acp」会误配别的工具。单测钉住 `mcode\0acp` 命中、`mcode\0exec` 不命中。

## 9. 已知未验证 / 风险

- `--api-key-env` 引用的是**启动 mcode 的那个进程的环境**：实测如此。但 ClawBench 面板目前不给
  MiniMax 绑 provider（见 §2），所以这条只影响「用户自己在 `~/.minimax` 里配好、再用 ClawBench 起 agent」
  的情况，**没有在 ClawBench 里端到端跑过一整轮**。
- mcode 额外启动的进程（`mcode-tools` 等）从当前进程环境继承——按上述链路应能拿到变量，未单独验证。
- 未验证 `session/set_model`、`session/set_config_option(model)` 的取值形式（编码 id）；切换模型依赖 config option 通道。
- 未验证 `session/load` / `session/resume` 的恢复：能力位都广告了，但未在进程重启后实测。
- 未验证图片输入（`promptCapabilities.image = true` 已广告）。
- `~/.minimax/skills` 确实是它的技能目录（bundle 里按 `<home>/.minimax/skills` 解析），
  且用户自己的技能会出现在 `availableCommands` 里，因此声明
  `NativeSkillsDirs: [".minimax/skills"]` + `AutoLoadsNativeSkills: true`。
  `MINIMAX_DATA_DIR` 能整体重定位时这条声明会指向默认位置——与 pi（`PI_CODING_AGENT_DIR`）
  同一取舍：默认安装占绝大多数，先按默认目录声明，避免重复注入。

## 实跑（2026-11-24，真实模型）

先用 `mcode provider add --name … --base-url https://api.minimax.cn/v1 --api-format
openai-completions --model MiniMax-M3.1-Flash --api-key-env MINIMAX_API_KEY --use` 配好提供商，
再跑 `mcode acp` 一整轮：模型调 `bash`（输出含 `CLAWBENCH_MCODE_OK`）、回 `DONE`、
`stopReason:"end_turn"`，`_meta` 里带 provider/model/usage（`providerId=custom_provider:…`）。
