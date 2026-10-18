# Command Code ACP 接入方案（含 BYOK 免额度配方）

> 调研日期：2026-10-17 ｜ CLI 版本：`command-code@1.84.0` ｜ 探针：`internal/ai/commandcode_acp_probe_integration_test.go`
>
> 探针实测 **8/8 全 PASS（70.6s）**，全部经 BYOK（MiniMax）真实跑通。
>
> 运行：
> ```bash
> # BYOK 免额度（需 MINIMAX_API_KEY；探针会临时改写并逐字节恢复 ~/.commandcode/*.json）
> MINIMAX_API_KEY=... go test -tags integration -v \
>     -run 'TestCommandCodeACPProbe' -timeout 900s ./internal/ai/
>
> # 已 `cmd login` 时也可直接跑（走真实账号）
> go test -tags integration -v -run 'TestCommandCodeACPProbe' -timeout 900s ./internal/ai/
> ```

## 结论摘要

**可以接入，且可以完全不用 Command Code 的额度（BYOK）。** 但有四条与其它 ACP 后端不同的硬约束，其中三条会改变接入结构，不是"接上就能跑"：

1. **BYOK 必须靠 `config.json` 预置模型**，ACP 的模型选择器对 BYOK 无效（列表锁死在 CC 自带 catalog）。
2. **一个 `cmd acp` 进程只服务一个项目目录**（cwd lease），但同目录可多会话。
3. **没有轮中注入**（`session/steer` / `_session/steering` 都不存在），且同会话并发 prompt 被拒 —— `/ps` 不可用。
4. **未登录是协议级错误 `-32000`**，必须映射成用户可理解的提示。

## 一、能力面（实测）

| 项 | 值 | 备注 |
|---|---|---|
| 命令 | `cmd acp`（Linux/macOS 也可用 `cmdc acp`） | bin: `cmd` / `cmdc` / `commandcode` / `command-code`，四者指向同一入口 |
| 安装 | `npm i -g command-code` | |
| `loadSession` | **true** | → acp-load / acp-sync 可用 |
| `sessionCapabilities` | `{list, resume, close}` | |
| `promptCapabilities` | `{audio:false, image:true, embeddedContext:true}` | 音频不支持 |
| `mcpCapabilities` | `{http:true, sse:false}` | 编辑器侧 sse 被跳过 |
| `authMethods` | `command-code-cli` | "Sign in once with `cmd login`" |
| modes | `default` / `auto-accept` / `plan` / `dont-ask` / `bypass` | 会校验，未知回 `-32602` |
| configOptions | `model`（92 个 CC 网关模型）、`effort`（`category:"thought_level"`） | |
| 扩展方法 | `session/set_effort`、`session/state`、`session/exit` | 非标准 |

**没有**：`session/steer`、`_session/steering`、`session/fork`。

## 二、BYOK 免额度配方（核心）

### 2.1 原理

ACP 的鉴权门只做**非空判定**，不校验 key 有效性：

```js
// cli.mjs: runAcp()
const isAuthenticated = async () =>
  null !== await getCommandAuthKey({ path: getAuthFile(), env: process.env });

// getCommandAuthKey: 环境变量 COMMAND_CODE_API_KEY 优先，其次 auth.json 的 apiKey
```

因此：**放一个占位 key 过闸，真实推理走自备 provider**。实测确认不会消耗 Command Code 额度（BYOK 请求直达上游 `api.minimaxi.com`）。

### 2.2 两份配置文件（缺一不可）

```jsonc
// ~/.commandcode/providers.json
// 顶层键必须是 "provider"（不是平铺，也不是 "providers"）—— 实测：写错则静默不加载
{
  "provider": {
    "minimax": {
      "name": "MiniMax (BYOK)",
      "api": "anthropic-messages",          // 或 openai-completions / openai-responses
      "baseURL": "https://api.minimaxi.com/anthropic",
      "apiKey": "$MINIMAX_API_KEY",         // 支持 $ENV / {env:VAR} / !command 三种引用
      "models": {
        "MiniMax-M2.7": { "name": "MiniMax M2.7" }
      }
    }
  }
}
```

```jsonc
// ~/.commandcode/config.json
// 缺 model/modelProvider 就会回落 CC 网关（实测 401）
{
  "installed": true,
  "model": "minimax/minimax-m2.7",   // "<provider>/<model 小写>"
  "modelProvider": "minimax"
}
```

**注意 `providers.json` 里 apiKey 不接受裸密钥**（会被忽略并告警）——必须用 `$ENV` / `{env:VAR}` / `!command` 引用形式。

### 2.3 四个坑（每个都实测踩过）

| # | 坑 | 证据 | 影响 |
|---|---|---|---|
| 1 | **ACP 模型列表锁死在 CC 自带 catalog** | `session/new` 的 `configOptions[model].options` 恒为 92 个 CC 网关模型；BYOK 模型**永不出现**（`availableModels(){return catalogModels()}`） | `session/set_model` 传 BYOK id 必回 `-32602 Unknown model` |
| 2 | **provider 绑定只在会话创建时、且只认 config.json** | `startingModel()`（非 resume）→ `getConfiguredModel()`；`providerForModel(modelId)` 要求模型在 CC catalog 且 lane 支持 ⇒ BYOK 模型返回 `undefined` ⇒ 回落 CC 网关 | 不写 config.json 就 `POST /alpha/generate → 401` |
| 3 | **ACP 下无法会话内切模型** | 见坑 1+2：任何 `set_model` 只能选 CC catalog 里的模型 | 切了会把会话拉回 CC 网关 |
| 4 | **`CMD_LOCAL_ONLY=1` 不能与 ACP 同用** | 它会拦一切 CC API 调用（连 taste-learning 都失败，错误文本被当 `agent_message_chunk` 流出） | CLI `-p` 模式可以（`-p -m minimax/minimax-m2.7` 实测返回 `BYOK-TEST-OK`），ACP 不行 |

**反向对照（已做）**：有 `model`+`modelProvider` → 会话起在 `minimax/minimax-m2.7`、回合 `end_turn` 成功；删掉这两个键 → 会话起在 `deepseek/deepseek-v4-flash`、回合 401。因果确认。

### 2.4 对 ClawBench 的含义

**BYOK 的模型选择必须是"进程级预置"，不能走 ACP 的模型选择器。** 可选实现：

- **方案 A（推荐）**：每个 BYOK provider 各起一个 `cmd acp` 进程，spawn 前写对应的 `config.json`。ClawBench 连接池本来就是按会话 id 建连接，天然支持"一 provider 一进程"。
- **方案 B**：一个 agent 只允许一个 BYOK provider（`modelProvider` 固定），UI 里把模型选择器禁用或只显示该 provider 的模型。
- **方案 C**：不进 BYOK，只支持 CC 账号（`cmd login`）。此时模型选择器正常工作。

⚠️ 方案 A 的风险：`~/.commandcode/config.json` 是**全局单例**。多个 cmd 进程并发 spawn 时若各自写不同 provider 会互相覆盖。可行做法是**按 provider 分组串行化 spawn**，或给每个 provider 一个独立 `HOME`（`HOME=/tmp/cc-<provider>`，但这样也隔离了 auth.json，需要一并复制）。

## 三、接入结构

### 3.1 新增子包 `internal/ai/backends/commandcode/`

参照 `hermes`（纯 ACP，无 CLI）：

```go
backends.Register(&backends.BackendPlugin{
    ID: "commandcode",
    Spec: model.BackendSpec{
        ID: "commandcode", Backend: "commandcode",
        // ⚠️ 不能写 "cmd"：Windows 上 cmd 恒存在（系统 shell），
        // probeBackendPresence 会因此给所有 Windows 用户造出幽灵 agent。
        // cmdc 是同一入口的另一别名，三平台都有（已实测 Linux）。
        DefaultCmd: "cmdc",
        AltCmd:     "commandcode",
        Name:       "Command Code",
        Specialty:  "持续学习编码风格的智能体",
        AcpCommand: "cmdc acp",      // 三平台通用
        ACPLoadSession: true,        // initialize 广告 loadSession:true
        InstallCmd: "npm i -g command-code",
        SortOrder:  18,
        // Command Code 自己会读用户级 ~/.commandcode/skills 与 ~/.agents/skills
        // （cli.mjs: resolveSkillLoadPlan 的 user 层，两者都在），以及项目级
        // <project>/.commandcode/skills 与 <project>/.agents/skills。
        // 声明 + AutoLoadsNativeSkills=true ⇒ ClawBench 不重复注入（否则同一技能出现两份）。
        // 注意：项目级 <project>/.agents/skills 由 skill.Registry 单独处理（总是注入），
        // 与这里的用户级声明不冲突。
        NativeSkillsDirs: []string{".commandcode/skills", ".agents/skills"},
        AutoLoadsNativeSkills: true,
        // ThinkingEffortLevels 留空：effort 是 ACP configOption（category:"thought_level"），
        // 前端运行时从 ACP 取（同 cursor/hermes 姿态）
    },
    ACP: &backends.ACPPlugin{
        ToolTitlePrefixes: CommandCodeACPTitlePrefixes,
        InputRemaps:       CommandCodeACPInputRemaps,
        // ModelSwitchMethod 留空（走默认 config-option 路径）是正确的：
        // cli.mjs 的 handleSetConfigOption 对 configId="model" 直接调 switchModel，
        // 与 session/set_model 是同一个函数 ⇒ 两条路径等价，无需特殊方法。
    },
})
```

**Windows 注意（这是本后端最容易埋雷的一处）**：`getBinaryCommand()` 返回 `win32 === platform ? "cmdc" : "cmd"`。
若 `DefaultCmd: "cmd"`，则 Windows 上 `CheckCLIExists("cmd")` **恒为真**（系统 shell），
`probeBackendPresence` 会给**每个** Windows 用户造出一个幽灵 Command Code agent，
其 `cmd acp` 会去跑 `cmd.exe acp`（报错或被当命令执行）。

两种可行做法：

- **A（简单）**：`DefaultCmd: "cmdc"` + `AltCmd: "commandcode"`，`AcpCommand: "cmdc acp"`。
  Linux/macOS 上 `cmdc` 也是有效 bin（四个 bin 指向同一入口），所以不必按平台分支。
- **B（更精确）**：按 `runtime.GOOS` 决定 `DefaultCmd`/`AcpCommand`。但 `BackendSpec` 是静态字面量，
  需要在 `init()` 里用 `if runtime.GOOS == "windows"` 构造，增加复杂度。

**建议 A**：`cmdc` 在三个平台都可用，`cmd` 只在 Windows 有歧义。

**已实测验证**（Linux）：`npm i command-code` 后 `node_modules/.bin/` 同时存在
`cmd` / `cmdc` / `commandcode` / `command-code` 四个入口；`cmdc --version` → `1.84.0`，
`cmdc acp --help` → 正常输出 ACP 帮助。故 `DefaultCmd: "cmdc"` 在三平台都成立。

### 3.2 工具名映射（已用 `extractToolName` 实证校验）

CC 的 toolCallID 形如 `call_function_2ft7niegnoyv_1`（**不含工具名**），工具名在 title 开头
`"<label>: <value>"` ⇒ 必须走 `ToolTitlePrefixes`（同 Hermes）。

**但 CC 的 title 是"人话标签"而非原生工具名**（`toolTitleFor` 用 `rn` 表转换）：

| 原生工具 | 标签（title 前缀） | ACP kind |
|---|---|---|
| `read_file` / `read_multiple_files` | `Read` | read |
| `read_directory` | `List` | read |
| `edit_file` | `Edit` | edit |
| `write_file` | `Write` | edit |
| `grep` | `Search` | search |
| `glob` | `Glob` | search |
| `web_search` | `Search` | search |
| `web_fetch` | `Fetch` | fetch |
| `shell_command` | `Shell` | execute |
| `run_command` | `Command` | execute |
| `monitor_command` | `Monitor` | execute |
| `kill_shell` | `Kill` | execute |
| `todo_write` | `Updating todos`（**无冒号**） | think |
| `task` | `Agent` | other |
| `get_diagnostics` | `Diagnostics`（无 key） | read |
| `enter_plan_mode` / `exit_plan_mode` / `plan_review` / `ask_user_question` / `monitor_events` / `shell_tasks` / `bash_output` | **原生名原样**（`rn` 表无条目 → 回退 `e`） | switch_mode / think / execute |

#### 实测发现的真缺陷

用真实的 `extractToolName` 跑上表，**未加 `ToolTitlePrefixes` 时**有 8 个工具会泄漏原生名或误归类：

```
enter_plan_mode   kind=switch_mode title="enter_plan_mode"   -> "enter_plan_mode"   ❌ 泄漏
exit_plan_mode    kind=switch_mode title="exit_plan_mode"    -> "exit_plan_mode"    ❌ 泄漏
plan_review       kind=think       title="plan_review"       -> "plan_review"       ❌ 泄漏（→DeepThink）
ask_user_question kind=think       title="ask_user_question" -> "ask_user_question" ❌ 泄漏
monitor_events    kind=execute     title="monitor_events"    -> "monitor_events"    ❌ 泄漏
shell_tasks       kind=execute     title="shell_tasks"       -> "shell_tasks"       ❌ 泄漏
bash_output       kind=execute     title="bash_output"       -> "bash_output"       ❌ 泄漏（→Bash）
todo_write        kind=think       title="Updating todos"    -> "DeepThink"         ❌ 误归类
```

**根因**：这些 title 是**单个 token**，`lookupByTitle` 的"单 token 且无 `.`/`/`"分支会**原样返回 title**，
根本走不到后面的 `acpKindToCanonical`。所以**不能指望 kind 兜底**，必须在 `ToolTitlePrefixes` 里显式列出。

（`kill_shell` 的 title 是 `"Kill: t1"`，含冒号故能走到 kind→`Bash`；但它语义上是"杀进程"，
若想显示专门图标也可加 `"Kill": "KillShell"` —— 非必需。）

#### 修正后的映射表（21/21 逐条核验通过）

```go
// CommandCodeACPTitlePrefixes 只覆盖「kind 兜不住」的工具。
// 已验证 kind 已正确的刻意省略：Shell→Bash、Read→Read、List→LS、Edit→Edit、
// Write→Write、Glob→Glob、Fetch→WebFetch、Agent→Agent、Diagnostics→Diagnostics、
// Monitor→Monitor、Command→Bash、Kill→Bash。
var CommandCodeACPTitlePrefixes = map[string]string{
    // title 是单 token 原生名 ⇒ lookupByTitle 会原样透传，必须显式映射
    "enter_plan_mode":   "EnterPlanMode",
    "exit_plan_mode":    "ExitPlanMode",
    "ask_user_question": "AskUserQuestion",
    "monitor_events":    "Bash",
    "shell_tasks":       "Bash",
    "bash_output":       "Bash",        // 不是 BashOutput：前端无此条目（见下）
    // title 是自然语言（无冒号），kind=think 会被误判成 DeepThink
    "Updating todos":    "TodoWrite",
    "plan_review":       "DeepThink",   // 不是 ExitPlanMode：rawInput 无 mode（见下）
}
```

**两个"反直觉"映射**（照字面映射反而更糟，已实测前端渲染器）：

| 工具 | 字面映射 | 问题 | 实际映射 |
|---|---|---|---|
| `bash_output` | `BashOutput` | `web/src/utils/icons.ts` 无 `BashOutput` ⇒ 扳手图标 + 无渲染器，**比映射前（kind=execute→Bash，有终端卡片）更差** | `Bash` |
| `plan_review` | `ExitPlanMode` | `renderModeSwitch` 读 `input.mode`/`input.mode_id` 画徽标，而 `plan_review` 的 rawInput 无 mode ⇒ **空徽标** | `DeepThink` |

**核验方法**（接入时应作为回归测试保留）：用 `ai.ExtractToolNameForTest(title, kind, "commandcode", "call_function_x_1")`
逐条断言上表每一行 + 每个"刻意省略"的行。22 个 case 全部 OK。

**已知遗留**：`get_diagnostics`（title `"Diagnostics"`，无冒号）解析成自身，前端同样无 `Diagnostics` 条目
⇒ 通用扳手卡片。诚实但不好看；归入 `Read` 会误导（输出是诊断列表不是文件预览），故保留原样。

#### ⚠️ 无法用 title 区分的一处：`Search`

`grep` 与 `web_search` **共用 `"Search: ..."` 标签**（`rn` 表里 `grep.label="Search"`、
`web_search.label="Search"`）。两者 kind 都是 `search` ⇒ 都归 `Grep`。
`web_search` 想显示 `WebSearch` 图标，**仅凭 title+kind 无法区分**，需要：

- **方案 1（推荐）**：接受 `web_search` 显示为 `Grep`（视觉上都是"搜索"，可接受），
  不要为它加 title 前缀（加了会把 `grep` 也变成 `WebSearch`）。
- **方案 2**：在 `tool_call` 的 `rawInput` 里区分（`grep` 有 `pattern` + 无 `query`；
  `web_search` 有 `query`）。这需要新增一个后端钩子，成本高于收益。

同理 `monitor_command`（label `Monitor`）已被共享 `acpToolNamePatterns` 正确解析为 `Monitor`，无需处理。

### 3.3 权限选项

CC 发 `session/request_permission` 时带 3 个 option（无 risk 时 2 个）：

| optionId | 场景 |
|---|---|
| `allow_once` | 总是存在 |
| `allow_always` | 无 risk 时存在 |
| `reject_once` | 总是存在 |
| `reject_always` | 无 risk 时存在 |

ClawBench 的 `RequestPermission` 默认取第一个 option（`allow_once`），兼容。自动批准路径也兼容。

### 3.4 错误码映射（新增）

| code | 含义 | ClawBench 处理 |
|---|---|---|
| `-32000` | `AUTH_REQUIRED`（未登录） | 显示「请先在终端执行 `cmd login`」，**不要**当通用后端错误 |
| `-32602` | `INVALID_PARAMS`（cwd 冲突 / 未知 mode / 未知 model） | 按 message 区分：cwd 冲突是配置问题，未知 model 是 UI 选错 |
| `-32600` | `INVALID_REQUEST`（prompt 并发 / 运行中切模型） | ClawBench 应自行串行化，正常不该出现 |
| `-32603` | `INTERNAL_ERROR`（含 local-only 拒绝、上游 401） | 原样透出 message（它已含上游状态码） |

### 3.5 必须同步的联动点（参照 hermes 接入）

| 文件 | 改动 |
|---|---|
| `cmd/server/main.go` | 空白 import `_ "clawbench/internal/ai/backends/commandcode"` |
| `internal/ai/backends/native_skills_test.go` | 技能表加 `"commandcode": true` |
| `internal/model/discovery_test.go` | `expectedIDs` 加 `"commandcode"` |
| `internal/service/i18n_hardcoded_guard_test.go` | Specialty 含中文需加白名单 |
| `web/src/utils/backendNames.ts` | `commandcode: 'Command Code'` |
| `web/src/utils/agentIcons.ts` | 图标（lobe-icons 有无 commandcode？无则用 fallback） |
| `web/src/assets/mono-icon-colors.css` | 若是 monochrome 图标需加 `mono-commandcode` |
| `docs/spec/infra/config-and-discovery.md` | 纯 ACP 后端清单加 `commandcode` |
| `AGENTS.md` | 后端清单加 Command Code |

## 四、探针覆盖（8 个点，全 PASS）

| 探针 | 验证什么 | 实测结果 |
|---|---|---|
| `L0_BYOK` | 占位 key 过闸 + BYOK 真实推理 | PASS 10.4s，`minimax/minimax-m2.7` + `end_turn` |
| `L1_Initialize` | loadSession / authMethods / 能力面 | PASS 3.2s |
| `L1_AuthGate` | 未登录 → `-32000` | PASS 3.2s |
| `L1_RawTurn` | 带工具调用回合、tool title 形状 | PASS 11.9s，`Shell: echo ...` / `kind=execute` |
| `L2_Backend` | ClawBench 生产链路 42 事件 | PASS 8.0s |
| `L3_CwdLease` | 同 cwd 可复用 / 异 cwd 被拒 / 相对 cwd 被拒 | PASS 3.3s，三条全确认 |
| `L3_NoMidTurnInjection` | steer 与 _steering 都 `-32601` | PASS 3.4s |
| `L3_ConcurrentPromptAndSwitches` | 并发 prompt `-32600`、未知 mode/model `-32602` | PASS 27.2s |

## 五、待办（接入前需补的验证）

1. **`session/load` 实测**：`loadSession:true` 已确认广告，但探针未测实际 load（需要真实登录态或 BYOK 下的持久会话）。
2. **BYOK 多 provider 并发**：`config.json` 全局单例的冲突方案需实测（独立 `HOME` vs 串行 spawn）。
3. ~~**Windows `cmdc`**~~ → **已解决**：`cmdc` 在 Linux 实测可用（`--version` / `acp --help` 均正常），四平台 bin 同名，`DefaultCmd: "cmdc"` 即可，无需平台分支。
4. **effort 档位**：CC 的 effort 是 configOption，需确认 ClawBench 的 `thought_level` 解析路径能接住（同 cursor）。
5. **技能目录去重**：已确认 CC 读 `.commandcode/skills` + `.agents/skills`（用户级与项目级）。已核对 `internal/skill/registry.go` 的去重语义：声明 `NativeSkillsDirs` + `AutoLoadsNativeSkills=true` ⇒ 该目录的技能**不重复注入**（`InjectedFor` 的 "own native" 分支）。CC 确实自己读这两个目录，故声明是对的——与 Hermes 的差别在于 CC **真的**读 `.agents/skills`（Hermes 只在 `skills.external_dirs` 里配置时才读）。
6. **`Search` 标签二义性**：`grep` 与 `web_search` 共用标签，需决定接受 `Grep` 还是加后端钩子按 `rawInput` 区分（见 §3.2）。

## 六、与其它 ACP 后端的对比

| 维度 | Command Code | Hermes | Cursor | Claude / Codex |
|---|---|---|---|---|
| 传输 | `cmd acp` | `hermes acp` | `cursor-agent acp` | npx 桥接 |
| 鉴权 | `cmd login` / `COMMAND_CODE_API_KEY`；**可 BYOK 绕过** | provider 配置 | `CURSOR_API_KEY` | 各自 CLI 登录 |
| 轮中注入 | ❌ | ✅ redirect/queue | ❌ | ✅ `_session/steering` |
| 单进程单项目 | ✅（cwd lease） | ❌ | ❌ | ❌ |
| 工具名位置 | title 前缀 | title 前缀 | 未捕获 | toolCallID / title |
| loadSession | ✅ | ✅ | ✅ | ✅ |

**Command Code 是唯一有"进程级项目绑定"的后端** —— 这意味着 ClawBench 的"同一 agent 多会话跨项目"会各起进程（本来就是），但**同一会话内切换工作目录会失败**。需在 UI 层确认没有"会话内换项目"的路径。

---

## 附：文档中每条结论的证据来源

| 结论 | 证据类型 |
|---|---|
| 能力面（loadSession / sessionCapabilities / promptCapabilities / authMethods） | **真实 wire**（`cmd acp` initialize 响应，探针 L1_Initialize） |
| `-32000 AUTH_REQUIRED` | **真实 wire**（未登录 session/new 响应，探针 L1_AuthGate） |
| cwd lease 三条行为 | **真实 wire**（探针 L3_CwdLease：同 cwd 接受 / 异 cwd -32602 / 相对 cwd -32602） |
| 无 steer / 并发 prompt 被拒 | **真实 wire**（探针 L3_NoMidTurnInjection、L3_ConcurrentPromptAndSwitches） |
| 工具 title 形态（`"Shell: echo ..."` / `kind=execute`） | **真实 wire**（探针 L1_RawTurn 打印） |
| BYOK 配方（providers.json + config.json + 占位 key） | **真实回合**（探针 L0_BYOK：`minimax/minimax-m2.7` + `end_turn` + 18824 tokens） |
| BYOK 反向对照（缺 model 回落 CC 网关 401） | **真实回合**（手工对照实验，见 §2.3） |
| ACP 模型列表锁死 catalog | **源码**（`availableModels(){return catalogModels()}`）+ **真实 wire**（92 个选项里无 BYOK 模型） |
| provider 绑定只在会话创建时 | **源码**（`startingModel` / `providerForModel`）+ **真实回合**（反向对照） |
| `set_config_option("model")` ≡ `set_model` | **源码**（`handleSetConfigOption` → `switchModel`） |
| 工具名解析缺陷（8 个泄漏/误归类） | **真实函数调用**（`ai.ExtractToolNameForTest` 21 条 case） |
| `cmdc` 三平台可用 | **真实命令**（Linux 上 `cmdc --version` / `cmdc acp --help`） |
| 技能目录与去重语义 | **源码**（`resolveSkillLoadPlan` + `internal/skill/registry.go`） |
| `CMD_LOCAL_ONLY` 与 ACP 冲突 | **真实回合**（报 `Local-only mode: refused ... /alpha/generate`） |
| 权限选项 id 集合 | **源码**（`Sn`/`wn` 常量表） |
