# Kiro CLI ACP 探测结果

> 调研日期：2026-11-24 ｜ CLI 版本：**3.5.0**（`kirocli-x86_64-linux.zip`，139MB，解压后 `kirocli/bin/kiro-cli`）
> 探针：`/tmp/acp_probe.py`（initialize / session/new / session/prompt 原始帧）
> 后端包：`internal/ai/backends/kiro/`

## 0. 没做到的部分（先看这个）

**没有可用的 Kiro 账号（Pro/Pro+/Pro Max/Power 订阅或 API key），因此「真跑一轮」全线未验证**：

- 没有看到任何 `tool_call` / `tool_call_update` 帧 → **工具名与参数形状未知**，所以 `ACP` 字段（`ToolCallIDPrefixes` / `InputRemaps`）一个都没填；
- 没有拿到服务端模型目录 → 静态 catalog 里只放了一项目录里能确认的 `auto`；
- `session/prompt` 返回 `-32000 ModelRegistryAccessDeniedError`（见 §3），不是「跑通了」；
- 未验证：`session/load` 回放内容、MCP 注入的工具是否真的出现在轮次里、`_kiro/*` 扩展的行为、中途插话。

文件里其它结论全部来自**实测握手帧**或**官方文档**，逐条标了来源。

## 1. 启动命令

`acp` 是**隐藏子命令**（`kiro-cli --help` 里不列），帮助要单独 `kiro-cli acp --help` 才能看到。

```
kiro-cli acp --agent-engine=v3
```

依据（官方文档 `/docs/cli/v3/acp-migration/` + `acp --help` 实测）：

- `--agent-engine` 取值 `v1|v2|v3`，**覆盖**保存的 `acp.agentEngine` 设置；
- 不带这个 flag 时由 CLI 自己决定：文档写「v3，或遇到它认为是 v2-only 的客户端、以及跑不了 v3 的系统时用 v2」。把引擎选择交给 CLI 不是这个后端该做的决定，所以显式钉 v3。
- **实测差异**：未登录时 v3 / v2 / 不带 flag 三者的 `initialize` 与 `-32000` **逐字节相同**，所以「v2 vs v3」的差别只能在能跑通的账号下才看得见（未验证）。
- v3 拒绝 v2-only 的启动 flag（`--agent` / `--model` / `--effort` / `--trust-all-tools` / `--trust-tools`），所以 `AcpCommand` 里一个都不带。

## 2. 认证：两种形态，能力面完全不同（实测）

Kiro 的 ACP 通道**只认两种凭据**：CLI 登录态（`kiro-cli login`，浏览器/BFF）或 `KIRO_API_KEY` 环境变量。判据就是环境变量非空——**不校验有效性**。

### 2.1 未登录

```json
{"jsonrpc":"2.0","id":2,"error":{"code":-32000,
 "message":"Authentication required. Not logged in. Set the KIRO_API_KEY environment variable or run `kiro-cli login` first. Then reconnect to start the Kiro agent."}}
```

stderr 同样打印这一行。此时 `initialize` 仍然成功，但能力面被压到最小：

| 项 | 未登录 |
|---|---|
| `agentInfo` | `{"name":"Kiro CLI Agent","title":"Kiro CLI Agent","version":"3.5.0"}` |
| `loadSession` | **false** |
| `promptCapabilities` | `{image:false, audio:false, embeddedContext:false}` |
| `mcpCapabilities` | `{http:false, sse:false}` |
| `sessionCapabilities` | `{}` |
| `authMethods` | 一条：`kiro-login`「Run 'kiro-cli login' in a terminal, then reconnect」 |

### 2.2 `KIRO_API_KEY` 非空（用一个**无效**的假 key 实测）

握手直接放行为完整形态：

| 项 | 有 key |
|---|---|
| `loadSession` | **true** |
| `promptCapabilities` | `{image:true, embeddedContext:true}`（无 `audio` 键 ⇒ false） |
| `mcpCapabilities` | `{http:true, sse:true}` |
| `sessionCapabilities` | `{list:{}, close:{}, delete:{}, fork:{_meta:{kiro:{messageId:true}}}}` |
| `authMethods` | `aws-builder-id`、`aws-iam-identity-center` |
| `agentCapabilities._meta.kiro.extensionMethods` | 约 47 个 `_kiro/*` 方法（memory / workflow / permissions / configuration / job / session …） |

关键含义：**客户端若在 initialize 里声明 terminal auth，未登录时被给的是一条 `kiro-login` 方法；不声明时也是同一条**——与文档「声明 terminal auth 会被给到 'Log in to Kiro' 终端方法」不完全一致（文档描述的 id/name 不同）。我们没实现 ACP 的 terminal-auth client 能力，所以 `LoginCmd` 用的是文档与错误文本都指向的 `kiro-cli login`。

`KIRO_API_KEY` 的文档口径（`/docs/cli/headless/`）：仅 **Pro / Pro+ / Pro Max / Power** 订阅可生成；企业托管账号需管理员开启。ACP 的报错文本自己点名了这个变量，且实测它确实改写了握手——所以它是 ACP 的合法凭据路径，不只是 headless 的。

## 3. 模型：目录在服务端，本地拿不到（实测）

| 尝试 | 结果 |
|---|---|
| 未登录 `chat --list-models --format json` | `Not logged in. Set the KIRO_API_KEY environment variable or run \`kiro-cli login\` first.`，无列表 |
| 假 key `chat --list-models --format json` | `error: service error`（stdout，exit 0） |
| 假 key 跑 \`session/prompt\` | `-32000` + `data.errorType=ModelRegistryAccessDeniedError` / `detail=AccessDeniedException`，消息「Kiro could not load the available models because this account does not have access to them.」 |
| `settings list --all` | 只有 `chat.defaultModel`（无枚举）、`chat.modelPicker.*`；**没有**任何可读的本地目录 |

结论：**不能做 CLI 探测式发现**（每次都要 15s、且必然失败），改为 `model.StaticSource` + `catalog.go`。`catalog.go` 里只放了 `auto`（Kiro 文档明确推荐的默认，且「Auto 是独立可选项」）。

文档 `/docs/models/available-models/` 只列**显示名**（Claude Sonnet 4.5、GPT-5.6 Sol、Kimi K3 …），**不给 id 字符串**，所以没有凭显示名硬编 id——猜错的表现是「model not found」，比没有更糟。真实 id 应从 ACP 会话的模型面（`configOptions` / `_kiro` 模型相关事件）抓取后补，抓取者请同时改 `TestKiroCatalogOnlyHasAssertedIDs` 的断言。

## 4. session/new 实测形状（有 key）

成功，返回 `sessionId`（`sess_<uuid>`）+ 同一帧里带 `modes` 与 `configOptions`：

- `modes.availableModes`：`vibe`(Default) / `spec` / `quick-spec` / `bug-fix` / `plan` / `autonomous` / `semantic_reviewer`，`currentModeId=vibe`
- `configOptions`（全部 `type: select`）：
  - `mode`（`category: mode`）
  - `memoryReflection`：`on/off`，默认 off
  - `autopilot`：`on`(Autopilot) / `off`(Supervised)，**默认 on** ← 注意：默认自动执行工具，不弹确认
  - `sessionToolsMessaging`：`on/off`，默认 off
  - `contentCollection`：`enabled/disabled`，默认 enabled
- 没有 `model` 或 `thought_level` 类 configOption（模型面在服务端门后）

session/new 接受 `mcpServers`（返回无错），initialize 也广告 `http/sse`，所以 `MCPUnsupported` **不置位**。但注入的工具是否真的出现**未验证**。

## 5. ACP 服务端反向请求（客户端必须应答，否则会等）

有 key 的会话一开始就发这些 **agent→client** 请求/通知，其中带 `id` 的必须回：

| 帧 | 说明 |
|---|---|
| `_kiro/terminal/shell_type`（**请求，id:0**） | 客户端没回时它自己发 `$/cancel_request` 放弃 |
| `_kiro/memory/syncStatus`（通知） | |
| `_kiro/governance/state`（通知） | `policyState=undecided`，随后工具才 `didChange` |
| `_kiro/mcp/status`、`_kiro/powers/items_changed`、`_kiro/tools/didChange`、`_kiro/sessions/changed`、`_kiro/steering/documents_changed`、`_kiro/progressive_context/items_changed` | 全是通知 |
| `session/update` 的 `sessionUpdate` 值 | 除标准 `available_commands_update` 外，还发 `session_info_update`（带 `_meta.kiro.kind`：`context_usage` / `user_message_id_assigned` / `focus_update`） |

**未验证**：ClawBench 的 ACP 客户端对这些 `_kiro/*` 反向请求一律回「method not found」是否有害（Kiro 文档要求客户端「对不支持的 agent→client 请求显式回错误」）。`_kiro/terminal/shell_type` 实测在无客户应答时会被主动取消，说明它是可选的；但 `governance/state` 之后没看到工具面变化，需要真账号复核。

## 6. 技能：原生加载，进提示词（实测）

**结论：`AutoLoadsNativeSkills = true`，`NativeSkillsDirs = [".kiro/skills"]`。**

实证方法：在两个位置各放一个带唯一标记的 `SKILL.md`（`UNICORNSKILL42` / `PENGUINSKILL77`），启动 ACP 会话。

```json
{"method":"_kiro/progressive_context/items_changed","params":{"items":[
 {"name":"zzprobe-marker","type":"skill","scope":"global",
  "uri":"file:///tmp/kirohome/.kiro/skills/zzprobe-marker/SKILL.md",
  "_meta":{"kiro":{"resource":{"resourceType":"skill","source":{"origin":"user"}}}}},
 {"name":"zzproj-marker","type":"skill","scope":"workspace",
  "uri":"file:///tmp/probe-kiro/.kiro/skills/zzproj-marker/SKILL.md",
  "_meta":{"kiro":{"resource":{"resourceType":"skill","source":{"origin":"workspace","root":"/tmp/probe-kiro"}}}}}]}}
```

即**会话启动时**就自动收集，且**全局 + 项目**两处都扫。补充证据：

- 二进制里有 `skill://.kiro/skills/*/SKILL.md`，挂在 `DEFAULT_AGENT_RESOURCES` 上；
- 有专门的提示词构造模块 `crates/agent/src/agent/prompts/skills.rs`（错误串「expected a list of skill resources」出自它）；
- `Skills` 是 `UserContent` 的变体（与 `AgentContent` / `AgentThought` 并列），说明技能文本**进了提示内容**，不是只挂成可调用工具；
- 文档字符串：「Reusable prompt templates invocable as slash commands from .kiro/skills/」——同时暴露成斜杠命令。
- **不支持 `.agents/skills`**：二进制里 `agents/skills` 出现 0 次。

`NativeSkillsDirs` 只登记 home 相对路径 `~/.kiro/skills`；项目级 `.kiro/skills` 是 per-cwd 的，Kiro 自己按 session cwd 解析，写进这张表会被拼成固定的 home 子路径。

一条未验证的细节：没跑到模型那一轮，所以「模型能否复述技能名」这个最终效果没测（被 `-32000` 拦住）；但技能被收集进上下文是实测的。

## 7. 实现落点

| 位置 | 内容 | 依据 |
|---|---|---|
| `Spec.AcpCommand` | `kiro-cli acp --agent-engine=v3` | §1 |
| `Spec.ACPLoadSession` | `true` | §2.2 实测 `loadSession=true`（该字段**覆盖**握手报告，必须一致） |
| `Spec.LoginCmd` | `kiro-cli login` | §2 错误文本 + 文档 |
| `Spec.InstallCmd` | `curl -fsSL https://cli.kiro.dev/install \| bash` | 官方 headless 文档的安装步骤 |
| `Spec.SortOrder` | 25 | lead 分配 |
| `Spec.MCPUnsupported` | 不置位（false） | §4 |
| `Spec.NativeSkillsDirs` | `[".kiro/skills"]` | §6 实测 |
| `Spec.AutoLoadsNativeSkills` | `true` | §6 实测 |
| `ModelSource` | `StaticSource("kiro","kiro-cli",KiroCatalog)` | §3 |
| autopilot（session/new 默认 on） | 需建会话后 `session/set_config_option(autopilot=off)`，**公共层未接线** | §7.1 实测可关 |
| `ProviderEnv` | `KeyOnly: "KIRO_API_KEY"`（**没有** OwnServiceEnv） | §2：唯一凭据，无 endpoint 槽、无 private 协议入口 |
| `ACP.*` | 全空 | 无工具帧可证 |
| `Behavior` | 不填 | |
| `OrphanCmdline` | **未登记** | 见下 |
| `ThinkingEffortLevels` | 未填 | v3 没有 effort configOption（`--effort` 是 v2-only） |

**OrphanCmdline 没登记**：它是 `{Binary, Arg}` 形状，用来识别「`<binary> <arg>` 命令行里残留的子进程」。Kiro 的子进程是它自己的 bundled runtime，探测中没有留下可识别的 `<binary> <arg>` 特征（`ps` 里没有 Kiro 后代进程残留）。不做猜测。

## 7.1 autopilot 默认 on：**可以关**，但必须建会话后发（实测）

有凭据的 `session/new` 返回的 `configOptions` 里带：

```json
{"type":"select","id":"autopilot","name":"Autopilot","currentValue":"on",
 "options":[{"value":"on","name":"Autopilot","description":"Agent executes tools without confirmation"},
            {"value":"off","name":"Supervised","description":"Agent asks for approval before file changes"}]}
```

默认 **on**：Kiro 直接改文件、跑命令，不弹权限请求。ClawBench 的权限卡靠 `session/request_permission` 渲染——autopilot 开着时这一帧根本不会到来，用户会以为每一步都问过，而实际没有。

### 实测：`session/set_config_option` 能关掉

```
→ {"jsonrpc":"2.0","id":9,"method":"session/set_config_option",
   "params":{"sessionId":"sess_9f99…","configId":"autopilot","value":"off"}}

← {"jsonrpc":"2.0","id":9,"result":{"configOptions":[…,
    {"type":"select","id":"autopilot","name":"Autopilot","currentValue":"off", …},…]}}
```

- **返回成功**（`result` 里 `autopilot.currentValue` 已是 `"off"`，无 error）；
- 同一时刻还推送了一条 `session/update` 的 `config_option_update`（同样的 configOptions，`autopilot=off`）；
- 重复发同样的值也成功（幂等）；
- `currentValue` 由 `on` → **`off`**，确认生效。

### 三个约束（都实测）

| 约束 | 证据 |
|---|---|
| **必须 `session/new` 之后** | 带不存在的 sessionId 提前发：`{"code":-32000,"message":"Session 'sess_bogus' not found"}` |
| **只对当前会话有效** | 关掉后再 `session/new`，新会话 `autopilot.currentValue` 仍是 **`on`** |
| **没有本地持久化通道** | `settings list --all` 无 autopilot 项；`acp/chat --help` 无 flag；二进制 `grep -i autopilot` 只有枚举 `AUTOPILOT`/`SUPERVISED`，无环境变量 |

### 对 ClawBench 的含义（公共层缺口）

正确做法：**每个新会话建好后立刻发一次** `session/set_config_option(configId:"autopilot", value:"off")`。

当前 `internal/ai/` **没有**「建会话后写初始 config option」的注册点（grep `InitialConfigOptions` / `InitialMode` 均无），所以 kiro 包无法自己完成这一步。在公共层补上之前，Kiro 后端在 ClawBench 里就是**工具静默执行**——这是必须跟用户说清的已知行为，不能让人以为有权限卡。

· 补充：关掉之后没能验证「工具会弹 `session/request_permission`」——假 key 下 prompt 仍被 `-32000 ModelRegistryAccessDeniedError` 挡住，没有走到工具执行。`off` 的语义（Supervised：「asks for approval before file changes」）来自选项描述。

## 8. 复现方式

```bash
# 下载并解压（别装到 ~/.local/bin）
mkdir -p /tmp/kiro2 && cd /tmp/kiro2
curl -fL -C - --retry 20 -o kiro.zip \
  https://prod.download.cli.kiro.dev/stable/latest/kirocli-x86_64-linux.zip
unzip -q -o kiro.zip -d extracted          # → extracted/kirocli/bin/kiro-cli
/tmp/kiro2/extracted/kirocli/bin/kiro-cli --version   # kiro-cli 3.5.0

# 用隔离 HOME，避免污染真实 ~/.kiro
export HOME=/tmp/kirohome && mkdir -p $HOME
mkdir -p /tmp/probe-kiro

# A. 未登录：initialize 完整，session/new -32000
python3 /tmp/acp_probe.py --cmd "/tmp/kiro2/extracted/kirocli/bin/kiro-cli acp --agent-engine=v3" \
  --cwd /tmp/probe-kiro --timeout 30

# B. 假 key：完整能力面，session/new 成功
python3 /tmp/acp_probe.py --cmd "/tmp/kiro2/extracted/kirocli/bin/kiro-cli acp --agent-engine=v3" \
  --cwd /tmp/probe-kiro --timeout 30 --env "KIRO_API_KEY=kiro-fake-key-for-probe"

# C. 假 key + prompt：-32000 ModelRegistryAccessDeniedError
python3 /tmp/acp_probe.py --cmd "... acp --agent-engine=v3" --prompt "say hi" --timeout 120 \
  --env "KIRO_API_KEY=kiro-fake-key-for-probe"

# D. 真账号下的补充探测（本环境未做）
#   kiro-cli login（浏览器）或 KIRO_API_KEY=<真实 key>
#   然后：initialize 的完整帧、session/new 的 model configOption、
#        一次 read_file 的 tool_call 形状、session/load 回放、MCP 注入
```

## 9. 待补（有真账号后）

1. 抓一次 `read_file` 的工具调用帧 → 决定要不要 `ToolTitlePrefixes` / `ToolCallIDPrefixes`。
2. 抓模型 id → 替换 `KiroCatalog` 的占位项，改 `TestKiroCatalogOnlyHasAssertedIDs`。
3. 验证 `session/load` 真能回放历史（`loadSession:true` 只是广告）。
4. 验证注入 MCP 后工具是否出现（`MCPUnsupported` 是否该置位）。
5. 验证 `_kiro/*` 反向请求不答的后果（尤其 `governance/state`）。
6. v2 与 v3 的真实差异（未登录帧完全相同，所以只能靠真账号对比）。
7. autopilot 已在 §7.1 实测**可以关**（建会话后发 `session/set_config_option(autopilot=off)`）；待补的是公共层的「初始 config option」机制，以及真账号下验证关掉后确实弹 `session/request_permission`。
