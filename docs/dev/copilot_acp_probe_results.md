# GitHub Copilot CLI · ACP 探测结果

- 探测日期：2026-11-24
- CLI：`@github/copilot` **1.0.115-8**（`npm install -g @github/copilot`，装在 `/tmp/cli-probe`）
- ACP 入口：**`copilot --acp`**（`copilot --help` 的 `--acp`：Start as Agent Client Protocol server）
- 探测脚本：`/tmp/acp_probe.py`（握手）、`/tmp/acp_probe_perms.py`（自动批准权限）、`/tmp/acp_mcp_probe.py`（注入 MCP）
- 环境：本机 `GH_TOKEN` / `GITHUB_TOKEN` 已设置（classic PAT），**没有**跑交互式 `copilot login`；所有会写用户目录的探测都用隔离的 `COPILOT_HOME=/tmp/...`

## 1. 握手（`initialize`）

```json
{"protocolVersion":1,
 "agentCapabilities":{
   "auth":{"logout":{}},
   "loadSession":true,
   "mcpCapabilities":{"http":true,"sse":true},
   "promptCapabilities":{"image":true,"audio":false,"embeddedContext":true},
   "sessionCapabilities":{"close":{},"list":{}}},
 "agentInfo":{"name":"Copilot","title":"Copilot","version":"1.0.115-8"},
 "authMethods":[{"id":"copilot-login","name":"Log in with Copilot CLI",
   "description":"Run `copilot login` in the terminal",
   "_meta":{"terminal-auth":{"command":".../copilot","args":["login"],"label":"Copilot Login"}}}]}
```

要点：

- `loadSession:true` + `sessionCapabilities {close, list}` → `session/load`、`session/list`、`session/close` 都在，**不需要磁盘兜底扫描**。
- `mcpCapabilities.http/sse` 都广告了，且实测真的接（见第 5 节）。
- `promptCapabilities.image:true`、`embeddedContext:true`、`audio:false`。
- 只有一个 authMethod（`copilot-login`），带 `_meta.terminal-auth` 的 `args:["login"]`，与 `copilot login` 一致。

## 2. 认证

| 场景 | `session/new` 结果 |
|---|---|
| 什么都不设 | `-32000 Authentication required` |
| 设 `GH_TOKEN`（classic PAT） | 同上。ACP 日志：`no authentication available for this ACP connection`；另有 `Classic PATs are not supported` |
| 设 `GITHUB_TOKEN` / `COPILOT_GITHUB_TOKEN` | 同上 |
| **BYOK 环境变量**（`COPILOT_PROVIDER_BASE_URL` + `_API_KEY` + `_MODEL`） | **成功**，返回 `sessionId` + `models` + `configOptions` + `modes` |

- 结论：**环境变量里的 GitHub token 对 ACP 无效**，ACP 只认 `copilot login` 落下的登录态。非交互模式（`-p`）则直接报 `Classic Personal Access Tokens (ghp_) are not supported by Copilot`。
- **BYOK 是免登录的正路**。官方文档原文（`copilot help providers-env`）："GitHub authentication isn't required"。
- `settings.json` 里写 `{"model": "..."}` **不生效**（BYOK 下仍然报 `BYOK providers require an explicit model`），只有 `COPILOT_MODEL` / `COPILOT_PROVIDER_MODEL_ID` / `--model` 三个入口管用 —— 非交互模式三种都验过能跑。
- `providers.json` 单独存在（没有 `COPILOT_PROVIDER_*`）时 ACP 报 `session.model.list failed ... Not authenticated`，且环境变量一旦设置就会被忽略（官方："ignores the saved providers.json"）→ **不采用 providers.json**。

### BYOK 配方（可直接复制，不需要 GitHub 登录）

```bash
COPILOT_PROVIDER_BASE_URL=https://your-endpoint/v1 \
COPILOT_PROVIDER_API_KEY=sk-... \
COPILOT_PROVIDER_TYPE=openai \      # openai(默认) | azure | anthropic | google
COPILOT_PROVIDER_MODEL_ID=my-model \  # BYOK 必填，否则 CLI 拒绝启动
copilot --acp
```

四个变量缺一不可：`BASE_URL` / `API_KEY` 定端点，`TYPE` 定线协议（`gemini` 等非法值启动即报 `Must be one of: openai, azure, anthropic, google`），`MODEL_ID` 定模型（不设则报 `BYOK providers require an explicit model`）。

## 3. 模型发现

- **没有 `copilot models`** 子命令（`copilot models` 报 `Invalid command format`）。主命令里有 `--model <model>`（"use 'auto' to let Copilot pick"）。
- 唯一的机器可读来源是 `session/new` 的响应：

```json
{"models":{"availableModels":[
   {"modelId":"auto","name":"Auto","description":"Let Copilot pick the best model"},
   {"modelId":"gpt-4.1","name":"gpt-4.1","description":"gpt-4.1",
    "_meta":{"copilotUsage":"0x","copilotEnablement":"enabled"}}],
 "currentModelId":"gpt-4.1"},
 "configOptions":[
   {"id":"mode","category":"mode","options":[Agent, Plan, Autopilot]},
   {"id":"model","category":"model","options":[...]},
   {"id":"allow_all","category":"permissions","options":[On, Off]}],
 "modes":{"availableModes":[Agent, Plan, Autopilot]}}
```

- BYOK 下列表 = `auto` + 注入的模型；订阅账号下应是 GitHub 的完整目录（**未验证**，无账号）。
- `configOptions` 只有 **mode / model / allow_all**，**没有 `thought_level`** → `ThinkingEffortLevels` 留空。CLI 有 `--reasoning-effort`（`none|minimal|low|medium|high|xhigh|max`），但 ACP 面不暴露。
- modes 的取值是标准 ACP 的 URL 形式：`https://agentclientprotocol.com/protocol/session-modes#agent|plan|autopilot`。
- 因此本后端的 `ModelSource` 返回空列表 + 原因（"the model list is reported over ACP"），**不臆造目录**：对 BYOK 来说 id 完全取决于端点，臆造出来的 id 会被 CLI 拒绝，然后**静默回落到 GitHub 默认模型**。

### 3.1 会话内能不能换模型（BYOK）——**不能换到没声明过的模型**

实测（`session/set_config_option`，`configId="model"`）：

| 目标 | 结果 |
|---|---|
| 本会话已声明的模型 | ✔ 成功 |
| 未声明过的 id（如 `third-model`） | ✘ `-32602 Invalid modelId 'third-model': not one of this session's models.` |
| `auto`（BYOK 账号） | ✘ `-32603 ... Auto is unavailable for this account; choose a concrete model` |

会话的候选集 = `session/new` 时声明的 `auto` + `COPILOT_PROVIDER_MODEL_ID`。**换模型必须发生在建会话之前**，而 `COPILOT_PROVIDER_MODEL_ID` 是进程级环境变量。

**接线方式**：`ProviderEnvSpec.ModelEnv = "COPILOT_PROVIDER_MODEL_ID"`（公共层字段，注释里点名了 copilot）。它按「agent 固定的模型 → 否则 provider 的 DefaultModel」拼出这个变量，所以每个 agent 能各用各的模型。`settings.json` 的 `model` 实测**不生效**（BYOK 下仍报 "require an explicit model"），不能当替代。

## 3.2 `COPILOT_MODEL` 是不是必填

是 —— 但"必填"指的是模型 id 必需，而**不是**必须叫 `COPILOT_MODEL`。官方文档写的是三选一：`COPILOT_MODEL`（同时当 model id 与 wire model）、`COPILOT_PROVIDER_MODEL_ID`（已知模型 id）、`--model`。三个都实测能让 BYOK 会话跑起来。推荐用 `COPILOT_PROVIDER_MODEL_ID`：它只声明 id，不强行改写发给端点的 wire model 名。

## 4. 工具帧（真实抓到，copilot 1.0.115-8）

用本地假 OpenAI 端点（`/tmp/fake_openai.py`）驱动模型逐个调用工具，抓到原始帧：

```json
{"sessionUpdate":"tool_call","toolCallId":"call_shell","title":"Running: echo hi",
 "kind":"execute","status":"pending","rawInput":{"command":"echo hi"}}
{"sessionUpdate":"tool_call_update","toolCallId":"call_shell","status":"completed",
 "content":[{"type":"content","content":{"type":"text","text":"hi\n<shellId: 0 completed with exit code 0>"}}],
 "rawOutput":{"content":"hi\n...","detailedContent":"hi\n...",
   "contents":[{"type":"shell_exit","shellId":"0","exitCode":0,"cwd":"/tmp/probe-copilot","outputPreview":"hi\n"}],
   "structuredContent":{"shellId":"0"}}}

{"sessionUpdate":"tool_call","toolCallId":"call_grep","title":"Searching for 'needle'","kind":"read",
 "rawInput":{"pattern":"needle","path":"."},"locations":[{"path":"."}]}
{"sessionUpdate":"tool_call","toolCallId":"call_glob","title":"Finding files matching **/*.go","kind":"read",
 "rawInput":{"pattern":"**/*.go"}}
{"sessionUpdate":"tool_call","toolCallId":"call_read_file","title":"Reading shell output","kind":"read",
 "rawInput":{"path":"probe.txt"},"locations":[{"path":"probe.txt"}]}
{"sessionUpdate":"tool_call","toolCallId":"call_write_file","title":"write_agent","kind":"edit",
 "rawInput":{"path":"out.txt","content":"x"},"locations":[{"path":"out.txt"}]}
```

- `toolCallId` 的 `call_` 后面是**模型自己给的参数名**（我们假端点编的），不是工具名 → **不能靠 toolCallId 前缀识工具**。
- 标题里没有人话的「文件名: 行号」，只有一句描述；`kind` 只有 read/execute/edit 三档，单看 kind 会把 grep/glob 混成 Read。
- 模型实际看到的工具名（`tools[].function.name`，共 19 个）：`bash, read_bash, stop_bash, list_bash, view, create, edit, web_fetch, semantic_code_search, run_dynamic_workflow, dynamic_workflows_manage, sql, session_store_sql, read_agent, list_agents, write_agent, grep, glob, task`。
- 实测映射（本后端 `ToolTitlePrefixes` 全部来自上面的帧）：`Running:`→Bash、`Searching for`→Grep、`Finding files matching`→Glob、`Reading shell output`→Read、`write_agent`→Agent，其余裸工具名按名映射。
- `rawOutput.contents[]` 里的 `shell_exit`（exitCode / cwd）是 copilot 私有的结果形状，但**公共层不需要它**：`content[]` 已经带同样的文本，未登记 Behavior 钩子。
- **权限**：本轮的 5 次工具调用**没有触发 `session/request_permission`**（BYOK + 未登录会话下默认就放行工具）。带路径/URL 的选择是否弹审批**未验证**。

## 5. MCP

- `session/new` 带 `mcpServers:[{name:"probe",command:"python3",args:["/tmp/fake_mcp.py"]}]`。
- 结果：会话创建成功；模型收到的工具表里多出 **`probe-clawbench_probe_ping`**（`<server>-<tool>` 形式）；假模型第一轮就调用它，MCP 服务的 `tools/call` 收到请求，`_meta.com.github.copilot/toolCallId` = `call_probe-clawbench_probe_ping`。
- 结论：**MCP 真实生效、同步可用的**，不设 `MCPUnsupported` / `MCPRejectsSSE` / `MCPAttachesAsync`。
- 注意：MCP 工具在标题里**也走同一个前缀映射**（标题 = 工具名原样），这里没专门映射，落回原样名。

## 6. 技能 —— **自己加载，true**

`copilot skill list` 写明扫描来源：

```
Project   .github/skills/, .agents/skills/, .claude/skills/
Personal  ~/.copilot/skills/, ~/.agents/skills/
Plugin    installed plugins that bundle skills
Custom    `copilot skill add <directory>`
```

ACP 路径共用的实测（每个技能都单独摆出来，看 `session/new` 的 `availableCommands`）：

| 位置 | ACP 会话中出现 |
|---|---|
| `<HOME>/.copilot/skills/home-probe/SKILL.md` | ✔ |
| `<HOME>/.agents/skills/agents-home/SKILL.md` | ✔ |
| `<项目>/.github/skills/probe-skill/SKILL.md` | ✔ |
| `<项目>/.agents/skills/agents-probe/SKILL.md` | ✔ |
| `<项目>/.claude/skills/claude-probe/SKILL.md` | ✔ |
| 默认 `~/.agents/skills/` 下的 `grill-me` 等 | 未出现（该目录只有 `~/.agents/skills`，而无 `~/.copilot/skills` 时） |

结论：**Copilot 技能：true**。ACP 会话启动时自己把用户级与项目级技能读进 `availableCommands`，不需要也不应该由 ClawBench 注入。

- `NativeSkillsDirs = [".copilot/skills", ".agents/skills"]`、`AutoLoadsNativeSkills = true`。两个都声明，是因为 `skill list` 把两者都算"Personal"，且 ACP 实测从两处都读到了 —— 少声明一个，ClawBench 会去重注入它，用户就会看到重复技能。
- 项目级 `.github/skills`、`.agents/skills`、`.claude/skills` 是 cwd 相对的，不属于 home 相对目录，故不在 `NativeSkillsDirs` 里。

## 7. 会话

- 存储位置（`copilot help sessions`）：`~/.copilot/session-state/<session-id>/`（`events.jsonl` / `workspace.yaml` / `plan.md` / `session.db`），`session-store.db` 是索引。
- 每轮都会发 `session_info_update`（`title` = 第一条消息、`updatedAt`）、`usage_update`（`used` / `size`，BYOK 下是估算值）、`available_commands_update`（一大串 `/xxx` 斜杠命令）。
- `session/close` 由 `sessionCapabilities.close` 广告；**未实测**它的行为（关闭后同 id 的 `session/load` 是否仍可恢复未验证）。本后端没有为它加钩子，只登记这一事实。
- 一轮结束返回 `{"stopReason":"end_turn"}`。

## 8. BYOK 变量全表（`copilot help providers-env`，实测的标 ✔）

| 变量 | 作用 | 实测 |
|---|---|---|
| `COPILOT_PROVIDER_BASE_URL` | 端点 | ✔ |
| `COPILOT_PROVIDER_API_KEY` | 密钥 | ✔ |
| `COPILOT_PROVIDER_TYPE` | `openai`(默认) / `azure` / `anthropic` / `google` | ✔ openai / anthropic / google 都建会话成功；`gemini` 等非法值启动即报 `Must be one of: openai, azure, anthropic, google` |
| `COPILOT_MODEL` | 模型 id（同时当 wire model） | ✔ |
| `COPILOT_PROVIDER_MODEL_ID` | 已知模型 id（影响上下文长度等） | ✔ 单独设也能起会话 |
| `COPILOT_PROVIDER_WIRE_API` | `completions`(默认) / `responses` | 设了不报错，取值是否真生效未验证 |
| `COPILOT_PROVIDER_HEADERS` | 自定义请求头（`Name: Value` 换行分隔） | ✔ 假端点收到了 `X-ClawBench-Protocol` |
| `COPILOT_PROVIDER_MAX_PROMPT_TOKENS` / `_MAX_OUTPUT_TOKENS` / `_VISION` / `_TRANSPORT` / `_AZURE_API_VERSION` / `_API_KEY_COMMAND` / `_BEARER_TOKEN` | 其余旋钮 | 未验证 |

`COPILOT_HOME` 可重定位整个 `~/.copilot`（登录态、session store 都在里面）。

## 8.1 ProviderEnv 三种线协议各自带 Extra

`ProtocolEnv.Extra` 按协议各配一份（实测 `BuildProviderEnv` 会把 `Extra` 原样注入）：

```go
OpenAI:    &model.ProtocolEnv{BaseURL: "COPILOT_PROVIDER_BASE_URL", APIKey: "COPILOT_PROVIDER_API_KEY",
                              Extra: map[string]string{"COPILOT_PROVIDER_TYPE": "openai"}},
Anthropic: &model.ProtocolEnv{...same vars..., Extra: map[string]string{"COPILOT_PROVIDER_TYPE": "anthropic"}},
Gemini:    &model.ProtocolEnv{...same vars..., Extra: map[string]string{"COPILOT_PROVIDER_TYPE": "google"}},
```

没有 azure 行：ClawBench 没有 azure 协议，Azure OpenAI 按 OpenAI 兼容端点绑定。

## 9. 实现落点

`internal/ai/backends/copilot/`

- `register.go`：`ACPLoadSession:true`、`LoginCmd:"copilot login"`、`InstallCmd:"npm install -g @github/copilot"`、`SortOrder:23`、`NativeSkillsDirs:[".copilot/skills",".agents/skills"]`+`AutoLoadsNativeSkills:true`、`ToolTitlePrefixes`、`OrphanCmdline{copilot,--acp}`。
- `ProviderEnv`：**纯环境变量，没有配置文件**。三个协议行各自 `ProtoEnv("COPILOT_PROVIDER_BASE_URL","COPILOT_PROVIDER_API_KEY","")` + `Extra{ProviderTypeEnv: ProviderType(protocol)}`，再加 `ModelEnv:"COPILOT_PROVIDER_MODEL_ID"`。
- `discovery.go`：空列表 + 原因（模型只在 `session/new` 响应里）。
- 曾有一版 `ConfigDirEnv`(`COPILOT_HOME`) + `settings.json` 渲染器，**已删除**：实测文件里写 `model` 与 `providerType` 都不生效，留着就是死代码加一个假落点。代价是每个 agent 不再有独立的 `~/.copilot` 副本，而是共用用户的（登录态与 session store 因此天然共享）。

## 10. 未验证 / 已知缺口

1. **订阅账号的模型目录**：无账号，列表形式未知（应为 GitHub 目录 + `auto`）。
2. **`session/close` 语义**、以及 `session/list` 的字段形状。
3. **权限弹窗**：路径/URL 类审批是否走 `session/request_permission` 未验证（本轮 5 次工具调用都没弹）。
4. **会话内换模型**：BYOK 下候选集在建会话时就定死了（`auto` + `COPILOT_PROVIDER_MODEL_ID`），换到别的 id 一律 `-32602`，换 `auto` 是 `-32603`。所以"每会话选模型"在 Copilot BYOK 上等于"每次换模型要重建连接"——`ModelEnv` 是进程级默认值，不是会话级切换的替代。
5. **provider 不声明模型时**：`ModelEnv` 什么都不加，CLI 会用自己的报错（"BYOK providers require an explicit model"）失败。这比随手编一个端点没服务过的 id 好。
6. 老数据：`MigrateRemoveRetiredBackends` 已整体删除（vecli 的清理代码不再保留），所以老库里的 copilot agent 不会被任何迁移删除，下次发现刷新会补上 `acp_command`；老行里的裸模型 id（如 `gpt-5`）在 BYOK 下无意义，需要用户重新选模型。

## 实跑（2026-11-24，BYOK + 真实模型）

`COPILOT_PROVIDER_BASE_URL=https://api.minimax.cn/v1`、`_TYPE=openai`、
`_MODEL_ID=MiniMax-M3.1-Flash` 跑通一整轮：模型调 shell（输出含 `CLAWBENCH_COPILOT_OK`）、
回 `DONE`、`stopReason:"end_turn"`，并上报 `usage`（input/output/total）。

`session/new` 的 configOptions 里另有 `allow_all`（`category:"permissions"`，默认 `off`）。
它和 `mode` 里的 Autopilot 是权限弹窗的开关；本轮 `off` 下工具仍直接执行了，说明
「不弹 `session/request_permission`」可能不只由这两个开关决定——权限弹窗仍未验证。
