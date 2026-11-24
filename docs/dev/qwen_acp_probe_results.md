# Qwen Code ACP 接入探测（2026-11-24）

实测对象：`@qwen-code/qwen-code` **0.28.4**（`/tmp/cli-probe/node_modules/.bin/qwen`），
启动命令 `qwen --acp`（`qwen --help` 里写作「Starts the agent in ACP mode」）。
探针：`/tmp/acp_probe.py`。本机**没有 Qwen 账号**，登录路径一律未验证。

## 1. 握手（initialize）

```json
{"protocolVersion":1,
 "agentInfo":{"name":"qwen-code","title":"Qwen Code","version":"0.28.4"},
 "authMethods":[{"id":"openai","name":"Use OpenAI API key",
   "description":"Requires setting the `OPENAI_API_KEY` environment variable",
   "_meta":{"type":"terminal","args":["--auth-type=openai"]}}],
 "agentCapabilities":{
   "loadSession":true,
   "promptCapabilities":{"image":true,"audio":true,"embeddedContext":true},
   "sessionCapabilities":{"list":{},"resume":{}},
   "mcpCapabilities":{"sse":true,"http":true},
   "_meta":{"imageCapability":{"autoHandlesWrongModel":true,"maxBytes":10380902,"maxImagesPerTurn":4}}}}
```

- `loadSession:true` 且 `sessionCapabilities.list` / `resume` 都非空 ⇒ 崩溃恢复可以走 resume，
  显式 load 也可以（`ACPLoadSession: true`）。
- `authMethods` **只有一个** openai 条目。注意它和真实凭据来源不一致：实际可用的 authType 还有
  `qwen-oauth`、`qwen-subscription`、`anthropic`、`gemini`、`vertex-ai`（见包内
  `AUTH_ENV_MAPPINGS`），只是 ACP 只广告 openai 一个。
- MCP：广告 sse + http；**未实测** `session/new` 是否真的挂上（无账号，不冒险改配置）。
  因此 `MCPUnsupported` / `MCPRejectsSSE` / `MCPAttachesAsync` 一个都没写。

## 2. session/new

- 无凭据：`-32000 "Authentication required: Use /auth to sign in or configure a model first."`
  （`data.authMethods` 同上）。
- 强制 `QWEN_OAUTH=1`（想走官方 OAuth）：`-32000 "Authentication failed: Qwen OAuth credentials
  expired..."` —— 本机没有 OAuth 凭据，这条路放弃。

### 用假 OpenAI 兼容端点跑通（成功）

起本地 `127.0.0.1:18081` 的 SSE 服务（回固定 chunk），只设：

```
OPENAI_API_KEY=dummy  OPENAI_BASE_URL=http://127.0.0.1:18081/v1  OPENAI_MODEL=test
```

`session/new` 成功，返回：

- `models.currentModelId: "$runtime|openai|test(openai)"`，`availableModels` 只有这一个
  （`_meta.isRuntime: true`）——**绑了自有 provider 后模型列表就是 provider 的那个模型**。
- `modes`：plan / default / auto-edit / auto / yolo，当前 `auto`。
- `configOptions`：`mode`、`model`（select，选项同上这一个模型）、`workflow_mode`
  （`_meta.qwen.workflowMode.available:false`）。**没有 `thought_level`**，所以
  `ThinkingEffortLevels` 留 nil（详见 §6）。
- `_meta.generatedUi: {enabled:false, reason:"unsupported-client"}`，无需处理。

**OPENAI_MODEL 是必须的**：只给 KEY + BASE_URL 时 authType 选了 openai 但没有模型，
`session/new` 依旧 -32000（"configure a model first"）。这也是后端声明
`ProviderEnvSpec.ModelEnv = "OPENAI_MODEL"` 的唯一理由（值由 provider 的默认 / 固定模型填入，见 §7）。

## 3. 模型发现

- `qwen --help` 的子命令里**没有** models 之类：只有 `agents / attach / auth / batch / board /
  channel / extensions / hooks / mcp / memory / review / sandbox / serve / daemon / sessions /
  update`。`-m/--model` 只是个字符串参数。
- `qwen auth` 已标 **removed**；非交互模式没选 authType 时报
  `No auth type is selected. ... (e.g. via settings or --auth-type)`。
- ⇒ 用 `model.StaticSource("qwen", "qwen", QwenCatalog)`，catalog 从包内模型表
  （`chunks/chunk-RZHZV5B2.js` 的 reasoning manifest）抄：qwen3.8-max/flash、qwen3.7-max/plus、
  qwen3.6-plus/flash、qwen3.5-plus、qwen3-coder-plus、**coder-model**（`DEFAULT_QWEN_MODEL`，
  OAuth 直连时唯一可用）。（附：`qwen3-coder-next`、`qwen3-max-2026-01-23` 等只在别处的
  生成配置里出现，没放进 picker，避免把内部 id 当可选项。）
- 运行时 ACP 会自报模型列表（见 §2），静态表只在起会话之前兜底。

## 4. 工具帧（真跑一轮，假端点注入 tool_call）

注入：`read_file`，第一次参数故意写成 `path`（错），第二次 `file_path`（对）。
所有 `session/update` 帧：

```jsonc
// 1) tool_call：起手帧，没有名字、没有参数
{"sessionUpdate":"tool_call","toolCallId":"call_1","status":"pending",
 "title":"ReadFile","content":[],"locations":[],"kind":"read","rawInput":{},
 "_meta":{"toolName":"read_file","provenance":"builtin","phase":"preparing"}}

// 2) tool_call_update：名字与参数在这一帧才到
{"sessionUpdate":"tool_call_update","toolCallId":"call_1","status":"in_progress",
 "title":"ReadFile: hello.txt","content":[],
 "locations":[{"path":"/tmp/probe-qwen/hello.txt","line":null}],
 "kind":"read","rawInput":{"file_path":"/tmp/probe-qwen/hello.txt"},
 "_meta":{"toolName":"read_file","provenance":"builtin","startedAt":...}}

// 3) 失败分支（参数名错时）：
{"sessionUpdate":"tool_call_update","toolCallId":"call_1","status":"failed",
 "content":[{"type":"content","content":{"type":"text",
   "text":"params must have required property 'file_path'"}}],
 "_meta":{"toolName":"read_file","provenance":"builtin"}}

// 4) 每帧 _meta 里还有私有生命周期（未接，未发现有用信息）：
//    executionLifecycle{request|ended, executionId, promptId, phase, outcome, durationMs}
//    toolLifecycle{phase, executionStatus, callId, ...}
```

结论：

- **名字在 `_meta.toolName`，不在 toolCallId**。toolCallId 就是上游模型给的 tool_call id
  （这里是 `call_1`，换 provider 形状就变），所以 `ToolCallIDPrefixes` 表在这条路径上用不上；
  保留它只是因为公共解析器在没有 `_meta` 时还会按 id/标题查（名字表写的是 qwen 自己的工具名，
  全是公共表能消化的目标值）。
- **参数已是 canonical**：`file_path`、`locations[].path`。不需要 `InputRemaps`。
- `read_file` 只认 `file_path`（`path` 直接报错）。
- **必须自定义 `ParseToolCallUpdate`**：公共 `NewMetaToolCallParser` 的 update 分支只在
  “当前名还是小写”时才替换，而起手帧没名字 ⇒ 通用路径按 `kind=read` 给出 `"Read"`（大写），
  之后的 `_meta.toolName` 就永远盖不上。现在 update 只要带 `_meta.toolName` 就采用。
- 起手帧 `rawInput:{}`，参数全靠 update 带上——与前端「先画卡、再补参数」的时序一致。

**未验证**：写文件 / shell / glob / grep / web 搜索 / ask_user 的真实名字与参数形状。
`QwenACPTCIDPrefixes` 里除 `read_file` 外都是按 qwen 内置工具命名推断的，写错只会退回通用路径
（不会张冠李戴），但正确性没有实测。

## 5. 会话落盘（ListSessions）

`session/new` 之后即便没发 prompt 也已落盘（实测）：

```
~/.qwen/projects/<slug>/chats/<sessionId>.jsonl        # 一行一条：system/user/assistant/system...
~/.qwen/projects/<slug>/chats/<sessionId>.title.json   # {"version":1,"displayName":"hi","source":"prompt","updatedAt":...}
```

`<slug>` 与 CodeBuddy 同规则：绝对路径去掉前导 `/`、`/` 换 `-`（`/tmp/probe-qwen` →
`-tmp-probe-qwen`）。`sessionId` 就是 ACP 的会话 id。每行带 `sessionId`、`cwd`、`timestamp`、
`version`。`qwen/notify/session/last-user-prompt` 是 qwen 发给客户端的私有通知（未处理）。

本后端**依赖 `sessionCapabilities.list`**（已广告），所以没有注册磁盘兜底扫描器。

## 6. 技能与记忆（决定 `AutoLoadsNativeSkills`）

**结论：qwen 自己把技能加载进模型上下文（true）。**

- 包内 `SKILL_PROVIDER_CONFIG_DIRS=[".qwen",".agents"]`；`getUserSkillsDirs()` 把 user 级解析成
  `~/.qwen/skills` 与 `~/.agents/skills`，project 级是 `<cwd>/.qwen/skills` 与
  `<cwd>/.agents/skills`（`getSkillsBaseDirs(level)`）。
- **ACP 走同一份代码**：`qwen --acp` 一轮请求里带着
  `The following skills are available for use with the Skill tool ... <available_skills>`，
  共 19 条（`<location>bundled` / `<location>user`）。其中 6 条是本机用户自己的技能
  （find-skills、grill-me、goal-draft、guizang-ppt-skill、mmx-cli …），与
  `~/.agents/skills` 下的目录一一对应；`<available_skills>` / `SKILL.md` 的拼装代码就在
  同一个包（`chunks/chunk-5VB4YX3Z.js` 等），不是另一条 CLI 路径。
- ⇒ `NativeSkillsDirs: [".qwen/skills", ".agents/skills"]`、`AutoLoadsNativeSkills: true`。

**项目级目录刻意不登记**（反例）：同一次会话里，把 SKILL.md 放进 `<cwd>/.qwen/skills/xxx`、
`<cwd>/.agents/skills/xxx`，以及新建 `~/.qwen/skills/xxx`，都不出现在请求里（qwen 自己把目录
结构列进了 prompt，所以不是没看见）。是否加载未知；登记了却没加载会让 `AutoLoadsNativeSkills`
把这些目录排除在 ClawBench 注入之外——正是 `BackendSpec` 注释点名的危险方向。

- 记忆：`~/.qwen/memories/MEMORY.md` 的标记出现在请求里（qwen 自己加载），与 ClawBench 无关。

## 7. Provider 绑定（BYOK）

- 只有 OpenAI 协议：`OPENAI_BASE_URL` / `OPENAI_API_KEY`（+ `OPENAI_MODEL`，包内
  `AUTH_ENV_MAPPINGS.openai = {apiKey:[OPENAI_API_KEY], baseUrl:[OPENAI_BASE_URL],
  model:[OPENAI_MODEL, QWEN_MODEL]}`）。
- 不声明 Anthropic：qwen 有 `AUTH_ENV_MAPPINGS.anthropic`，但那条路（authType=anthropic）没有
  任何 ACP 实测，声明了等于接受一个可能读不到的绑定。
- 启动模型走 `ProviderEnvSpec.ModelEnv = "OPENAI_MODEL"`：值取 agent 固定的模型，否则取
  provider 的默认模型；两样都没有就不注入（qwen 自己的报错会点名缺模型）。
- **已知缺口（实测）**：绑了 provider 后，实际请求里的 `model` 恒为**启动时**那个值。
  把 `OPENAI_MODEL` 设成 A、再设 `MODEL=B`（qwen 接受的通用模型变量），请求里仍是 A。
  环境变量只能在 spawn 前读，所以 **BYOK 下会话里换模型不生效**，换模型要改 provider 的默认模型
  （或给 agent 固定一个模型）。

## 8. 其它事实

- 启动会打一条 Node 版本警告（`Node.js 24.22.1 ...`），与功能无关。
- 会话中 qwen 会主动发一个**私有请求** `craft/drainMidTurnQueue`（服务端→客户端，带
  `params.sessionId`），客户端回什么未测。ClawBench 的 raw RPC 侧遇到未知方法会怎么处理需另看，
  本后端没有据此登记 `MidTurn`（插话未验证）。
- 每轮结束 `session/prompt` 返回 `{"stopReason":"end_turn","_meta":{"qwen.branchPoint":{...}}}`。
- 默认模式 `auto`（LLM 分类自动批准），与 ClawBench 的权限卡片是两套机制；权限请求未在本次
  探测中出现（假端点不触发工具执行链的授权）。
