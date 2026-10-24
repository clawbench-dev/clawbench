# OpenClaw ACP 接入方案

> 调研日期：2026-10-24 ｜ CLI 版本：`openclaw@2026.10.6`（Node ≥ 24.22.1）｜ 探针：`internal/ai/openclaw_acp_probe_integration_test.go`
>
> 探针实测 **7/7 全 PASS（226s）**，全部经本机真实 Gateway + MiniMax 跑通。
>
> 运行：
> ```bash
> openclaw gateway start          # 必须先起 Gateway（见下）
> go test -tags integration -v -run 'TestOpenClawACPProbe' \
>     -timeout 1500s ./internal/ai/
> ```

## 结论摘要

**可以接入，但有四条与其它 ACP 后端不同的硬约束，每一条都会改变接入结构，不是"接上就能跑"。**

最本质的一条：**`openclaw acp` 不是 agent，是 bridge。** 它把 ACP 请求转发给另一个长期运行的 Gateway 进程（WebSocket，默认 `ws://127.0.0.1:18789`）。这条事实派生出下面四条约束中的三条。

| # | 约束 | 实测证据 | 接入处置 |
|---|---|---|---|
| 1 | Gateway 必须先运行 | Gateway 停掉时 `openclaw acp` 打印 `ACP bridge failed: connect ECONNREFUSED` 到 stderr 后**直接退出**，客户端只看到 initialize 超时（没有协议级错误可翻译） | `LoginCmd = "openclaw gateway start"`，把超时渲染成可执行指令 |
| 2 | `session/resume` 只对**本进程**有效 | 硬杀进程后用同一 id 调 `session/resume` → `-32603 "Session <uuid> not found"`；`session/load` 同一 id → 成功且历史完整（模型能答出被杀前埋的暗号） | 新增 `BackendSpec.ACPResumeIsProcessLocal`，恢复直接走 load |
| 3 | per-session `mcpServers` 被**拒绝** | `session/new` 带 `{name, command, args: [], env: []}` → `-32603 "ACP bridge mode does not support per-session MCP servers"` | 加入 `model.mcpUnsupportedBackends` |
| 4 | `modes` 与 `thought_level` **是同一组值** | `modes.availableModes` = `[off,minimal,low,medium,high,adaptive]`，`configOptions[thought_level].options` 完全相同；且 `set_config_option(configId="mode")` → `-32603 "ACP bridge mode does not support session config option \"mode\""` | 新增 `ACPPlugin.ModesMirrorThoughtLevel`，抑制 mode 面 |

此外还有一条**渲染层**约束（不阻塞接入，但会画错卡片）：

| # | 约束 | 实测证据 | 接入处置 |
|---|---|---|---|
| 5 | 插件/MCP 工具先发一个**包装帧** | 先 `tool_call: id: web_fetch, args: {...}`（kind=other, id=`call_xxx`），再 `web_fetch: url: ...`（kind=fetch, id=`tool_call:call_xxx:web_fetch:1`）。两个 id 不同，无法按 id 去重 | 新增 `ACPPlugin.ToolCallWrapperTitlePrefixes`，按标题丢弃包装帧 |

## 一、为什么"Gateway 必须先运行"是最重要的事实

`openclaw acp` 的进程模型是：

```
ClawBench ──stdio JSON-RPC──> openclaw acp (bridge) ──WebSocket──> Gateway ──> 模型
```

Bridge 自己不做任何 agent 工作。Gateway 不可达时它的行为是：

```bash
$ openclaw acp --url ws://127.0.0.1:19999
[stderr] ACP bridge failed: connect ECONNREFUSED 127.0.0.1:19999
# 进程退出，stdout 无任何输出
```

客户端侧只能看到 initialize 超时。**这意味着 `-32601`/`-32000` 这类协议错误码永远不会出现**，`LoginCmd` 是把这条死路变成用户可执行指令的唯一手段。

探针因此把 Gateway 可达性做成**前置 skip**（`openclaw health --json`），而不是让它以"initialize 超时"的形式伪装成产品缺陷。

## 二、`session/resume` 是进程内语义（新增能力位）

ClawBench 的自动崩溃恢复（`recoverExistingSession`）先试 `session/resume`，只在收到 **-32601 Method not found** 时回退到 `session/load`。这个设计对 CodeBuddy/pi 是对的（前者实现了 resume 但不广告，后者都不实现）。

但 openclaw 的 resume 是**实现了、只是作用域限于本进程**：跨进程调用返回 `-32603 "Session <uuid> not found"`。这不是 -32601，所以：

- 不会触发 load 回退；
- 会走"会话不可恢复"分支（`killAndMarkDeadLocked` + 报错）——这是**正确的**设计（不能静默开新会话导致失忆），但对 openclaw 意味着**每次 Gateway 重启都会永久废掉所有会话**。

实测 `session/load` 跨进程是好的，且**真的恢复历史**：

```
进程 A: prompt "记住暗号 PROBE-SECRET-42" → 硬杀
进程 B: session/resume(<uuid>) → -32603 not found
进程 B: session/load(<uuid>)   → OK，replay user_message_chunk + tool_call + agent_message_chunk
进程 B: prompt "暗号是什么"     → "PROBE-SECRET-42"
```

所以新增 `BackendSpec.ACPResumeIsProcessLocal`，置 true 时恢复直接走 load。

> **为什么不用"广告的 SessionCapabilities.Resume"来判断**：那是已有注释里明确否定的做法（CodeBuddy 实现但不广告，pi 广告但不实现）。新能力位表达的是**另一个维度**——"实现了，但作用域不够"。

## 三、per-session MCP 被拒绝（且是载荷相关的）

这条比"静默忽略"更糟：它会**让每一轮都失败**。

但拒绝是**载荷相关**的，这点容易踩坑：

| `mcpServers` 载荷 | 结果 |
|---|---|
| `{name, command}` | **接受**（并静默忽略） |
| `{name, command, args: ["hi"]}` | **接受**（并静默忽略） |
| `{name, command, args: [], env: []}` | **拒绝** |

ClawBench 的 `toACPMcpServers` 对每个 stdio server 都**总是**输出 `args: []` 和 `env: []`（`nonNilStrings` / `toEnvVariables` 的存在就是为了避免 JSON null 被严格消费者拒绝）。所以**实际发出的载荷正是被拒绝的那一种**。

> 探针第一版只发 `{name, command, args: [...]}`，测出来"接受"，差点得出"openclaw 不需要 MCP 排除"的相反结论。修法是让探针发 ClawBench 的真实载荷形状，并加一条对照断言钉住"最小载荷仍被接受"——两边都钉住，未来任一侧变化都会失败。

**不要"修复"成裁剪空切片**：那只会把硬失败换成静默无效注入，正是 `mcpUnsupportedBackends` 这个表要防的事。

## 四、`modes` 与 `thought_level` 是同一组值

`session/new` 同时返回：

```json
"modes": {"currentModeId": "adaptive",
          "availableModes": [{"id":"off"},…,{"id":"adaptive"}]},
"configOptions": [
  {"id":"thought_level", "category":"thought_level", "currentValue":"adaptive",
   "options":[{"value":"off"},…,{"value":"adaptive"}]},
  {"id":"fast_mode"}, {"id":"verbose_level"}, {"id":"trace_level"},
  {"id":"reasoning_level"}, {"id":"response_usage"}, {"id":"elevated_level"}]
```

两者值集**完全相同**（探针用 `ElementsMatch` 钉住）。而写 mode 是被拒的：

```
session/set_config_option {configId:"mode", value:"high"}
→ -32603 "ACP bridge mode does not support session config option \"mode\""
```

若两个都照收，UI 会出现两个一模一样的选择器，其中一个点了没反应。

`ACPPlugin.ModesMirrorThoughtLevel = true` → `applyExtractedState` 在写注册表**之前**丢弃 modes。放在写注册表之前是必要的：否则陈旧的能力快照会持久化，重启后死选择器又回来了。

### thought_level 的可用值依赖模型

广告的是 bridge 的**静态枚举**，实际可写性看 Gateway 当前模型：

```
thought_level=off/minimal/low/medium/high → OK
thought_level=adaptive                    → -32603 "thinkingLevel \"adaptive\" is not
                                            supported for minimax/MiniMax-M2.5-highspeed
                                            (use off|minimal|low|medium|high|ultra)"
```

所以 `ThinkingEffortLevels` 留 nil（静态列表会给用户一个当前模型拒绝的档位），由前端运行时从 `configOptions` 取。探针的断言也因此是"**至少一个**广告值可写"，而不是"每个都可写"。

注意错误信息里提到的 `ultra` **没有被广告**——即广告集与可写集是双向不一致的。

## 五、包装帧（渲染层）

插件/MCP 工具的调用会**先发一个包装帧**：

```
[1] tool_call  id=call_65d02a2f...           kind=other  title="tool_call: id: web_fetch, args: {\"url\":...}"
[2] tool_call  id=tool_call:call_65d02a2f...:web_fetch:1  kind=fetch  title="web_fetch: url: https://example.com"
```

两个 id 不同（[2] 的 id 里嵌了 [1] 的 id，但作为整体不等），所以**任何按 id 去重的逻辑都合并不了**；只能按标题识别。照收会渲染两张卡片，第一张名字直接是字符串 `tool_call`。

`ToolCallWrapperTitlePrefixes = ["tool_call: id: "]` → 事件循环在 `update.ToolCall` 分支最前面丢弃该帧（仍 `SetToolInFlight(true)`，避免看门狗在包装帧与真帧之间误触发）。

> 核心工具（read/write/edit/exec/ls）**不发**包装帧，直接是真帧。

## 六、工具名与输入字段

### 标题形态（L4 实测）

| 标题 | kind | 解析结果 |
|---|---|---|
| `read: path: /a/b.go` | read | Read（共享表） |
| `write: path: /x, content: …` | edit | Edit（共享表） |
| `edit: path: /x, edits: [...]` | edit | Edit（共享表） |
| `exec: command: echo hi` | execute | Bash（共享表） |
| `web_fetch: url: …` | fetch | WebFetch（共享表） |
| `web_search: query: …` | search | Grep（共享表，见下） |
| `ls: path: /x` | other | **需映射** → LS |
| `tool_search: query: …` | search | **需映射** → Grep |
| `memory_search: query: …` | search | **需映射** → Grep |
| `apply_patch: input: *** Begin Patch` | other | **需映射** → Edit |
| `progress_card: markdown: …` | other | **需映射** → DeepThink |
| `sessions_list` | other | **需映射** → DeepThink |
| `tool_describe: id: …` | other | **需映射** → DeepThink |

`web_search` 落到 Grep 是**共享表的既有行为**（grep 与 web_search 都用 label "Search" + kind=search，无法区分），不是 openclaw 特有缺陷。

### 输入字段：扁平 remap 表不够用

| 工具 | rawInput | 卡片读的字段 |
|---|---|---|
| read | `{"path": …}` | `file_path` |
| write | `{"path": …, "content": …}` | `file_path` |
| edit | `{"path": …, "edits":[{"oldText","newText"}]}` | `file_path` + `old_string`/`new_string` |
| **ls** | `{"path": …}` | **`path`** |
| exec | `{"command": …, "workdir": …}` | `command` |

两个扁平表无法表达的点：

1. **`path` 对不同工具映射不同**：read/write/edit 要变 `file_path`，但 **ls 必须保持 `path`**（LS 卡片读 `path`）。全局映射会把 LS 卡片弄空。
2. **edit 的参数在嵌套 `edits[]` 里**，且没有扁平的 `old_string`/`new_string`。diff 渲染器只读扁平字段，必须把 `edits[0]` 提升上来。

所以 openclaw 走**工具感知**的解析器（`parseOpenClawACPToolCall` → `normalizeOpenClawACPInput`），复用 pi 的 `normalizePiEditInput`（嵌套 edits 处理完全相同），而不是加进共享的扁平 remap 表。

## 七、其它实测事实

- **能力面**：`loadSession: true`，`sessionCapabilities {list, resume, close}`，`promptCapabilities {image: true, audio: false, embeddedContext: true}`，`mcpCapabilities {http: false, sse: false}`，`authMethods: []`（不需要 ACP 层鉴权）。
- **无模型选择面**：没有 `model` config option，`session/set_model` → **-32601 Method not found**。模型在 Gateway 侧配置。故 `ModelSwitchMethod` 留空。
- **`session/list` 返回 Gateway 全部会话**（含 cron/钉钉/飞书会话），id 形如 `agent:main:acp-bridge:<uuid>`；裸 uuid 在**新进程**里 `session/resume`/`session/load` 都可能解析不到，用 `--session <key>` 或 `_meta.sessionKey` 建立的会话可以用 key 直接 resume。
- **不触发权限请求**：exec/write 都不发 `session/request_permission`（Gateway 侧的 exec policy 决定），故探针的自动应答分支未被触发。
- **`agent_thought_chunk` 未观测到**：即使 `reasoning_level=stream` + `thought_level=high`，也只发 `agent_message_chunk`。thinking 可能只存在于 Gateway 侧日志。
- **技能目录**：`<workspace>/skills`、`<workspace>/.agents/skills`、`~/.agents/skills`（仅默认 state dir）、`~/.openclaw/skills`、per-agent `workshop-skills`。用 marker `SKILL.md` 验证：`~/.openclaw/skills` 与 `~/.agents/skills` 中的标记都被模型按名字列出；**workspace 级**的两个标记没出现（ACP 会话的 workspace 是 Gateway 配置的 workspace，不是 `session/new` 的 `cwd`）。故 `NativeSkillsDirs` 只声明两个用户级目录。
- **`usage_update`**：`{used, size, _meta:{source:"gateway-session-store", approximate:true}}`，`used` 常为 0 —— 近似值，不能当真实 token 计数。

## 八、接入改动清单

| 文件 | 改动 |
|---|---|
| `internal/ai/backends/openclaw/{register.go,acp.go}` | 新增后端包 |
| `internal/ai/backends/openclaw/register_test.go` | 注册/标题/输入/包装帧/抑制的单元测试 |
| `internal/ai/openclaw_tool.go` | 工具感知解析器（path 分工具映射 + edits 提升） |
| `internal/ai/openclaw_acp_probe_integration_test.go` | 7 个探针（L1 裸通道 / L2 生产链路 / L3×4 约束 / L4 工具形态） |
| `internal/ai/openclaw_event_integration_test.go` | 包装帧丢弃 + mode 抑制的事件层测试 |
| `internal/ai/acp_resume_process_local_test.go` | load-first 恢复路径测试 |
| `internal/model/discovery.go` | `BackendSpec.ACPResumeIsProcessLocal` |
| `internal/model/mcp.go` | `openclaw` 加入 `mcpUnsupportedBackends` |
| `internal/ai/backends/plugin.go` | `ACPPlugin.ToolCallWrapperTitlePrefixes` / `ModesMirrorThoughtLevel` |
| `internal/ai/backends/registry.go` + `acp_wire.go` | 两个新查询函数 + 函数变量接线 |
| `internal/ai/acp_events.go` | tool_call 分支丢弃包装帧 |
| `internal/ai/acp_conn_state.go` | `applyExtractedState` 丢弃镜像 modes |
| `internal/ai/acp_conn_lifecycle.go` + `acp_pool.go` | load-first 恢复分支 + `backendSpec()` 辅助 |
| `cmd/server/main.go`、`internal/model/discovery_test.go`、`internal/ai/backends/native_skills_test.go`、`internal/ai/mcp_probe_table_test.go`、`internal/model/mcp_test.go`、`internal/service/i18n_hardcoded_guard_test.go` | 注册与守卫同步 |
| `web/src/utils/{backendNames.ts,agentIcons.ts}`、`web/src/assets/mono-icon-colors.css` + 两个测试 | 显示名 / 图标（用 lobe-icons 的 `openclaw-color.svg` 红色小龙虾，非单色版）/ 单色主题色 |

## 九、升级路径上的坑

本机原本有两个 Node 前缀都装了 openclaw：

- `~/.nvm/versions/node/v24.14.0` → openclaw **2026.4.15**（旧）
- `~/.nvm/versions/node/v24.22.1` → openclaw **2026.10.6**（新）

PATH 里 24.14 排在前面，于是 `openclaw acp` 走的是**旧版**。旧版读不懂新版迁移后的配置（`meta.migrations`、`agents.defaults.modelPolicy` 等），行为是**打印 "Config invalid" 后退出**——在 ClawBench 里表现为 `initialize: peer disconnected before response`，看起来像 openclaw 接入有问题，实际是 PATH 指向了旧二进制。

**处置**：卸载 24.14 下的旧包，并把 `24.14/bin/openclaw` 做成指向 24.22 版本的符号链接（24.14 的 bin 目录仍在 PATH 最前，且新版 `openclaw.mjs` 会自动用 24.22 的 node 重启自己，所以这样是安全的）。

升级本身还走了桥接版：2026.4.15 早于 2026-07，配置含 `plugins.installs` 等退役字段，**必须**先装 `2026.9.5` 跑 `doctor --fix`，再升 `latest` 跑一次 `doctor --fix`。直接升 latest 会被拒绝（"configuration contains retired fields that current Doctor cannot migrate"）。

Gateway 服务也要重装以指向新 node 前缀（`openclaw gateway install --force`）；它会因 `~/.config/systemd/user` 是 group-writable 而先拒绝，需 `chmod go-w`。

> 升级过程中用户级 `systemd --user` 管理器被全局 OOM 杀掉（14Gi 机器上同时跑着两个 vitest 全量），gateway 服务随之消失。恢复方式是 `sudo systemctl start user@1000.service`，之后 `systemctl --user start openclaw-gateway`。这不是 openclaw 的问题，但排查时容易误判成"接入把服务搞挂了"。
