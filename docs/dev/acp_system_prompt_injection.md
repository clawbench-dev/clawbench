# 各 ACP 智能体的「系统提示词注入」支持面调研

本文档回答一个问题：**ClawBench 接入的 12 个 ACP 后端中，哪些支持在会话创建时把系统提示词真正注入模型上下文（而不是拼进 user prompt）？**

> **背景**：ClawBench 目前的 ACP 路径把系统提示词以 `[System Instructions: %s]\n\n%s` 前缀拼进 user prompt（`internal/ai/acp_conn_state.go:518`），只在 `ShouldInjectSystemPrompt()`（首轮 / 间隔 / 压缩后）时生效。这不是真正的系统提示词——它占 user 轮次、可被用户消息覆盖、且在会话中途无法更新。
>
> **方法**：对每个后端，穷举其 `session/new`（及 `load`/`resume`）入口的 `_meta` 读取点，或查 CLI 的进程级 flag。产物已本机安装，全部可复核（复核命令见附录 A）。
>
> **调研日期**：2026-09-18。

## 0. 结论速览

| 后端 | 形态 | `session/new _meta` 注入 | 进程级 flag | 可用性 |
|------|------|--------------------------|-------------|--------|
| **claude**（claude-agent-acp） | 官方适配器桥接 SDK | ✅ **`_meta.systemPrompt`** | — | **最佳**：支持 `{type:'preset',preset:'claude_code',append}` |
| **grok** | Rust 单二进制，自带 ACP | ✅ **`_meta.systemPromptOverride`** + `_meta.rules` | `--system-prompt-override` / `--rules` | **最佳**：有替换与追加两条 |
| **codebuddy** | Node CLI 自带 ACP | ❌（仅 `continue`/`takeWriter`/`followOnlyAccept`/`agent`） | ✅ `--append-system-prompt` / `--system-prompt` / `--agents` | 可用，但只能进程级 |
| **qodercli** | Node CLI 自带 ACP | ❌（`newSession({cwd,mcpServers,additionalDirectories})`） | ✅ `--append-system-prompt` / `--system-prompt` | 可用，但只能进程级 |
| **opencode** | Bun 二进制自带 ACP | ❌（schema 只 `cwd`+`mcpServers`） | ❌ 无 flag（靠 AGENTS.md） | 只能文本注入 |
| **mimo** | opencode fork | ❌（同 opencode） | ❌ | 只能文本注入 |
| **codex**（codex-acp） | 官方适配器 | ❌（`_meta` 只读 additionalDirectories） | ❌ | 只能文本注入 |
| **copilot** | Node CLI 自带 ACP | ❌（只读 `cwd`/`mcpServers`） | ❌（仅 `--no-custom-instructions` 关闭） | 只能文本注入 |
| **kimi** | Python CLI 自带 ACP | ❌（`**kwargs` 吸收后从不读） | ❌ | 只能文本注入 |
| **agy**（antigravity） | 适配器 | ❌（源码显式声明 zero prompt injection） | ❌ | 只能文本注入 |
| **pi**（pi-acp） | 适配器 | ❌ | ❌ | 只能文本注入 |
| **zcode** | npx 桥接 → Electron 引擎 | ❌（ACP 层无 `systemPrompt`） | ❌ | 只能文本注入 |

**一句话**：**只有 claude 与 grok 两个后端提供了 ACP 协议层的系统提示词注入**；codebuddy 与 qodercli 可用进程级 CLI flag 达到同样效果；其余 8 个只能继续用文本前缀。

---

## 1. 两个「协议级」支持的实现细节

### 1.1 claude（`@agentclientprotocol/claude-agent-acp`）

**支持 `session/new` 的 `_meta.systemPrompt`**，且是三种入口共用的 `createSession` 读取，因此 `new` / `load` / `resume` **都能生效**。

```js
// dist/acp-agent.js（createSession，~偏移 354577）
let systemPrompt = { type: "preset", preset: "claude_code" };
if (params._meta?.systemPrompt) {
    const customPrompt = params._meta.systemPrompt;
    if (typeof customPrompt === "string") {
        systemPrompt = customPrompt;              // 完全替换
    } else if (typeof customPrompt === "object" && customPrompt !== null && !Array.isArray(customPrompt)) {
        // Forward all preset options (append, excludeDynamicSections, ...)
        systemPrompt = { ...customPrompt, type: "preset", preset: "claude_code" };
    }
}
```

**推荐用法**（追加而非替换，保留 Claude Code 内置工具说明）：

```json
{ "_meta": { "systemPrompt": { "append": "你的额外指令", "snapshot": true } } }
```

底层 `@anthropic-ai/claude-agent-sdk` 的 `systemPrompt` 类型（`sdk.d.ts:2213`）支持四种形态：

| 形态 | 语义 |
|------|------|
| `string` / `string[]` | 完全替换系统提示词（**危险**：内置工具使用说明会丢） |
| `{type:'preset',preset:'claude_code'}` | 默认 Claude Code 提示词 |
| `{type:'preset',preset:'claude_code',append:'...'}` | **默认 + 追加**（推荐） |
| `{type:'preset',preset:'claude_code',excludeDynamicSections:true}` | 剥离 cwd/memory/git 等动态段（多用户共享缓存前缀） |

`snapshot` 选项（`sdk.d.ts:3929-3934`）：把系统提示词记录进会话 transcript 并逐轮复用，避免中途变化导致 prompt cache 前缀失效（`append` 变化会作废扩展思考的既有推理）。**官方推荐 `snapshot: true`**；在 Bedrock/Vertex 等尚未启用的账号上该字段被接受但无效，可安全设置。

**注意**：会话指纹 `computeSessionFingerprint`（`dist/acp-agent.js:16205`）只含 `cwd` + `mcpServers`，**不含 systemPrompt**——所以同 sessionId 复用时若只改 systemPrompt 不会触发重建，需换 sessionId 或显式 teardown。

**路径**：`internal/ai/backends/claude/cli.go:19` 用的就是 `npx -y @agentclientprotocol/claude-agent-acp@latest`（本机 v0.76.0；全局装的 `@agentclientprotocol/claude-agent-acp@0.37.0`）。

### 1.2 grok（`grok agent stdio`，Rust 单二进制）

**`session/new` 的 `_meta` 支持两个字段**，且官方文档（内嵌于二进制的 ACP 文档，`/tmp/grok.txt:279493` 附近）明确列出：

```
## Session `_meta` options
Optional fields on `session/new`:
| Field                  | Description                                    |
| `rules`                | Extra rules appended to the system prompt.      |
| `systemPromptOverride` | Replacement system prompt.                      |
| `agentProfile`         | Agent profile name or JSON object.              |
| `yoloMode`             | When true, always-approve for this session.     |
| `autoMode`             | When true, auto permission mode for this session. |
```

实现证据（二进制字符串）：

- `crates/codegen/xai-grok-shell/src/agent/mvp_agent/mod.rs:2998` 附近有 `systemPromptOverride`
- `agent_ops.rs` 有日志串 `cold-load: systemPromptOverride already matches head, no-op` 与 `cold-load: applied systemPromptOverride to loaded head` → 说明 load/resume 时也会应用

进程级等价 flag（`grok --help`）：

```
--rules <RULES>                      Extra rules to append to the system prompt
--system-prompt-override <PROMPT>    Override the agent's system prompt (compat alias: --system-prompt)
```

**推荐用法**：优先 `_meta.rules`（追加语义，安全），需要替换才用 `systemPromptOverride`。

**另有 `x.ai/*` 扩展方法族**（`x.ai/session/*`、`x.ai/git/*`、`x.ai/fs/*` 等），但不含系统提示词写入方法。

---

## 2. 两个「进程级 flag」可用的后端

### 2.1 codebuddy（`codebuddy --acp`）

ACP 协议层**没有**任何系统提示词通道（详见 `codebuddy_acp_extensions.md` 与本文附录 B）。但 CodeBuddy 的 ACP host 就是它自己那个进程，agent 指令从 **argv 解析结果**渲染，因此进程级 flag 生效：

```bash
--system-prompt <prompt>          # 整体覆盖（危险：替掉内置工具使用说明）
--system-prompt-file <path>
--append-system-prompt <prompt>   # 追加（推荐）
--agents <json>                   # 定义自定义 agent 的 prompt 作为 instructions
--agent <name>
--prompt-vars-file <path>
```

代码路径证据（`dist/codebuddy.js`）：

- `renderDefaultAgentInstructions`（~5 040 751）优先级：`session.options.systemPrompt` → `getCliSystemPromptOverride()`（读 `cliProvider.parseOpts().opts.systemPrompt` / `.systemPromptFile`）→ 内置 `agent.instructions`
- `isSessionPromptOverrideEligible`（~5 040 975）：门控只是「非子会话（无 `parentSessionId`）+ 不是内部辅助 agent」，**不检查 ACP** → ACP 会话可命中
- `buildAgentsFromProductConfiguration`（~5 013 953）显式把 `systemPrompt||systemPromptFile||appendSystemPrompt` 当作「需要等产品配置」的信号
- CLI 选项定义（~5 283 643）：`--append-system-prompt` 描述即 "Append additional content to the system prompt"

**未验证点**：`--append-system-prompt` 单独使用（不带 `--system-prompt`）时，`getCliSystemPromptOverride()` 在无 `systemPrompt` 时会忽略 `appendSystemPrompt`；但 `collectSystemPromptOptions → buildSystemPromptAgentOverride → agent.appendInstructions` 这条产品配置链也成立。**两条链静态分析都成立，但只有真机才能确认追加是否真的进了 context。**

**落地位置**：`internal/ai/acp_conn_lifecycle.go:508-513` 已有先例（按 backend 追加 `--mcp-config`），同样手法可加 `--append-system-prompt`。连接池按 ClawBench 会话键控（`acp_pool.go:463`），基本一会话一进程；但 `GetOrCreateConnNoSession` 用共享键 `__list_sessions__:<agentID>`（`acp_pool.go:432`），共享进程会把提示词带给所有会话——需注意。

### 2.2 qodercli（`qodercli --acp`）

ACP 层 `newSession({cwd, mcpServers, additionalDirectories})` **不解构 `_meta`**（`bundle/qodercli.js:29754783`），`newSessionConfig` 只处理 mcpServers 与 additionalDirectories。

但 CLI 提供两个 flag（`qodercli --help`）：

```
--system-prompt <text>               System prompt for the session
--append-system-prompt <text>        Append to the default system prompt
```

且 bundle 中 `systemPrompt` 出现 119 次、`appendSystemPrompt` 40 次、`system-prompt` 7 次——说明 flag 贯通到 agent 运行配置（`promptConfig.systemPrompt` 等）。落地方式与 codebuddy 相同（spawn 时按 backend 追加参数）。

---

## 3. 其余 8 个后端的证据

| 后端 | 证据 |
|------|------|
| **opencode**（Bun 二进制） | `session/new` 的 zod schema 为 `{cwd:string, mcpServers:array}`，无 `_meta` 解构；ACP service `newSession` 只 `session.create({directory,agent,model})`。无任何 `--system-prompt` flag。 |
| **mimo**（opencode fork，`@mimo-ai/cli`） | `newSession(A){let D=A.cwd; ...sessionManager.create(A.cwd,A.mcpServers,B)...}` —— 同 opencode，只读 `cwd`/`mcpServers`。 |
| **codex**（`@agentclientprotocol/codex-acp`） | `newSession(request)` 只用 `request.cwd` / `request.additionalDirectories` / `request.mcpServers`；`_meta` 仅经 `readAdditionalDirectories` 读取。`extMethod` 分支只有 auth/steer/goal/asyncTask，无提示词。 |
| **copilot**（`@github/copilot`） | ACP `newSession(e)` 只用 `e.cwd` + `e.mcpServers`（`app.js:10673842`）。13 处 `systemPrompt` 全是内部遥测/上下文统计/MCP sampling，无会话注入。仅 `--no-custom-instructions`（关闭）。 |
| **kimi**（`kimi acp`，Python） | `server.py:151` `async def new_session(self, cwd, mcp_servers=None, **kwargs)` —— `**kwargs` 只吸收 ACP 框架传来的 `_meta`，函数体从不读它；全文件仅 `field_meta`（auth 相关）出现。 |
| **agy**（`agy-acp`，antigravity） | 源码注释三处显式声明 "Zero prompt injection: only client `params.prompt` content is encoded and forwarded to agy. No adapter-authored labels, instructions, or follow-ups." `instructions` 仅出现在该注释中。 |
| **pi**（`pi-acp`） | `_meta` 读取点只有 `_meta?.["terminal..."]`；`instructions` 仅出现在 `/compact` 的 UI 文案。 |
| **zcode**（`npx -y zcode-acp-server` → `/opt/ZCode/resources/glm/zcode.cjs`） | ACP 桥接层无 `session/new` 字符串；引擎 `zcode.cjs` 的 23 处 `systemPrompt` 全是内部子 agent / workflow / token 统计，无外部注入通道。 |

---

## 4. 对 ClawBench 的建议路径

**统一抽象**：把「系统提示词如何注入」下沉为 backend 能力，而不是在 `buildPromptBlocks` 里统一拼文本：

1. **能力枚举**：给 `model.BackendSpec` 增加字段（例如 `SystemPromptMode`：`meta` / `argv` / `text`），或复用现有 per-backend 注册表。
2. **`meta` 类（claude / grok）**：在 `acp_conn_lifecycle.go:223` 的 `acp.NewSessionRequest` 里填 `Meta`（SDK 的 `NewSessionRequest` 已有 `Meta map[string]any` 字段，`types_gen.go:3242`）。claude 用 `{"systemPrompt":{"append":...}}`，grok 用 `{"rules":...}`——**两者 key 不同，需 per-backend 适配**。
3. **`argv` 类（codebuddy / qodercli）**：在 `spawnLocked` 里按 backend 追加 flag（已有 `--mcp-config` 先例）。**注意共享的 `__list_sessions__` 连接**。
4. **`text` 类（其余 8 个）**：保持现状（`[System Instructions:]` 前缀）。

**收益**：
- claude / grok：系统提示词真正进入 system role，不占 user 轮次，不受用户消息覆盖
- 四个后端可从「首轮/间隔注入」升级为「每轮都在 system 层」
- 中途改提示词（如切 agent 配置）可经 `load`/`resume` 重新注入（claude 的 `createSession` 三入口共用；grok 有 `cold-load: applied systemPromptOverride` 日志）

**风险**：
- `--system-prompt` / `systemPromptOverride` 是**替换**语义，会丢掉 CLI 内置的工具使用说明与安全规则 → 一律优先「追加」形态（claude `append` / grok `rules` / codebuddy `--append-system-prompt`）
- claude 的 `snapshot` 默认关闭时，`append` 每轮重新渲染会作废 prompt cache 前缀 → 建议显式 `snapshot: true`
- 会话指纹不含 systemPrompt（claude），同 sessionId 改提示词不重建

---

## 附录 A：复核命令

```bash
# claude — 读适配器源码（未混淆，带 .d.ts）
P=~/.npm/_npx/*/node_modules/@agentclientprotocol/claude-agent-acp
python3 -c "import re;s=open('$P/dist/acp-agent.js',encoding='utf-8',errors='replace').read();i=s.find('let systemPrompt = { type: \"preset\"');print(s[i:i+1200])"
# SDK 的 systemPrompt 类型文档
S=~/.npm/_npx/*/node_modules/@anthropic-ai/claude-agent-sdk
sed -n '2110,2230p' $S/sdk.d.ts

# grok — 二进制内嵌 ACP 文档 + 字符串
strings -n 6 ~/.grok/downloads/grok-linux-x86_64 | grep -n 'systemPromptOverride'
strings -n 6 ~/.grok/downloads/grok-linux-x86_64 | grep -n 'cold-load: applied systemPromptOverride'

# codebuddy — ACP agent 段（4.13M–4.22M）内 systemPrompt 出现次数应为 0
python3 -c "import re;s=open('~/.nvm/.../codebuddy.js'.replace('~','$HOME'),encoding='utf-8',errors='replace').read();print(len(re.findall('systemPrompt',s[4130000:4210000])))"
codebuddy --help | grep -iE 'system-prompt|agents'

# qodercli
Q=~/.nvm/versions/node/*/lib/node_modules/@qoder-ai/qodercli/bundle
python3 -c "import re;s=open('$Q/qodercli.js',encoding='utf-8',errors='replace').read();i=s.find('async newSession({cwd');print(s[i:i+300])"
qodercli --help | grep -iE 'system-prompt'

# opencode / mimo / codex / copilot / kimi / agy / pi / zcode
strings -n 4 <binary> | grep -E 'newSession"\)\(function'
grep -n '_meta' ~/.local/share/uv/tools/kimi-cli/lib/python3.13/site-packages/kimi_cli/acp/server.py
grep -rn 'Zero prompt injection' ~/.npm/_npx/*/node_modules/agy-acp/dist/
```

## 附录 B：与既有文档的关系

- **`codebuddy_acp_extensions.md`**：CodeBuddy 的完整扩展面清单。本文结论「CodeBuddy ACP 层无系统提示词通道」是该文档的直接推论——`extMethod` 37 个 case 与 163 个 `_meta` key 中均无提示词写入方法。
- **`claude_acp_extensions.md`**：Claude 的扩展面（结论：几乎完全遵循 ACP 规范，只通过 `_meta` 暴露少量桥接信息）。本文补充了其中一个关键 `_meta` 字段 `systemPrompt` 的用法。
- **`docs/spec/core/ai-backend.md:127,130`**：ClawBench 当前的系统提示词注入策略（`commonRulesTemplate` + 技能摘要注入 `SystemPrompt`，经 ACP 走文本前缀）。
