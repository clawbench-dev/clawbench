# TRAE（TRAE CLI / `traecli`）ACP 探测记录

日期：2026-11-25。**首轮（2026-11-24）的结论「拿不到二进制」已作废**——当时找错了入口：
npm 上没有、文档站里 `cli_what-is-trae-cli` 是 404。正确入口是
`https://docs.trae.cn/cli_get-started-with-trae-cli`，官方提供公开安装脚本，本轮已装并完成握手。

## 1. 安装（官方，已实测）

```bash
# macOS / Linux
sh -c "$(curl -fsSL https://trae.cn/trae-cli/install_v2.sh)"
# Windows
irm https://trae.cn/trae-cli/install_v2.ps1 | iex
```

- 实测：在隔离 HOME（`/tmp/traehome`）里执行，装到 `~/.local/bin/traecli`，版本 **0.216.0**，
  并在 `~/.bashrc` 里补 PATH。
- 文档同时说明 **1.0 已进入弱维护，请升级到 2.0**；2.0 首次启动会提示迁移旧配置
  （`traecli.yaml` → `traecli.toml`）。

## 2. 握手（0.216.0，未登录，实测）

`initialize` 返回的关键字段：

```json
{"agentInfo":{"name":"traecli-acp","title":"TRAE CLI","version":"0.216.0"},
 "agentCapabilities":{
   "loadSession":true,
   "promptCapabilities":{"image":true,"audio":false,"embeddedContext":true},
   "mcpCapabilities":{"http":true,"sse":false},
   "sessionCapabilities":{"list":{},"additionalDirectories":{},"systemPrompt":{},
                          "fork":{},"resume":{},"close":{}},
   "auth":{"logout":{}}},
 "authMethods":[
   {"id":"trae-tob-browser","name":"Sign in with Enterprise SSO"},
   {"id":"trae-tob-pat","name":"Sign in with Personal Access Token",
    "description":"Requires `TRAECLI_PERSONAL_ACCESS_TOKEN` to be set before starting TRAE CLI."}]}
```

`session/new` 未登录：`-32000 "Authentication required"`。

`_meta` 里另有：

| 键 | 内容 | 含义 |
|---|---|---|
| `steering.supported` | `true` | 插话（mid-turn）可能可用，**未实测** |
| `trae.acpBackend` | `"app-server"` | ACP 面挂在它自己的 app-server 上 |
| `trae.options` | `mcpServers` / `skills` | 它自己管理 MCP 与技能清单 |
| `goal.controlMethod` | `_session/goal`（set/pause/resume/clear） | 目标控制扩展，本仓未接 |

**结论：二进制公开可装，但登录是企业账号（ToB：企业 SSO 或企业控制台签发的 PAT）。**
个人版账号没有这两条登录路径，所以「能装、不能免费用」——这解释了第三方适配器
README 里「仅企业版旗舰版可用」的说法。

## 3. 技能目录

二进制里有 `"${TRAE_HOME:-$HOME/.trae}/skills"` 的字面量，且 `_meta.trae.options.skills`
说明技能是它的会话选项之一。因此：

```go
NativeSkillsDirs:      []string{".trae/skills"}
AutoLoadsNativeSkills: true
```

不声明的话，ClawBench 会再注入一份技能表，用户看到重复技能。

## 4. 模型列表

`traecli models --json` 存在，未登录时返回 `[]`（rc=0）。登录后应返回账号可用模型，但
**JSON 字段形状未知**，所以 `ModelSource` 仍保持「未知」而不是猜字段：

```go
func discoverTraeModels() ([]model.AgentModel, string) {
	return nil, "trae: model list unknown (traecli models needs an enterprise login; shape unverified)"
}
```

## 5. 实现清单（`internal/ai/backends/trae/register.go`）

| 字段 | 值 | 依据 |
|---|---|---|
| `ID` / `Backend` | `trae` | 项目命名约定 |
| `DefaultCmd` | `traecli` | 安装脚本产物 |
| `AcpCommand` | `traecli acp serve` | `traecli acp --help` 只有一个 `serve` 子命令 |
| `ACPLoadSession` | `true` | initialize 广告 `loadSession:true` |
| `InstallCmd` | `sh -c "$(curl -fsSL https://trae.cn/trae-cli/install_v2.sh)"` | 官方文档 + 实装；Windows 用 ps1 变体（字段只能放一条） |
| `LoginCmd` | `traecli login` | `traecli login --help` 存在（另有 `login status`） |
| `NativeSkillsDirs` | `[".trae/skills"]` | 二进制里的路径字面量 |
| `AutoLoadsNativeSkills` | `true` | 同上 |
| `ProviderEnv` | `KeyOnly: "TRAECLI_PERSONAL_ACCESS_TOKEN"` | authMethod 文案点名该变量；没有端点槽，不是 BYOK |
| `ModelSource` | `PluginSource`，恒定返回「未知」 | 见 §4 |
| `MCPUnsupported` | 不设 | 广告 `http:true`，未实测注入 |
| `ACP`（工具映射） | nil | 没抓到工具帧 |
| `Behavior` | nil | 同上 |
| `OrphanCmdline` | `{traecli, "acp serve"}` | 启动形状 |

## 6. 仍未验证（需要企业账号）

1. 一轮真实对话：工具帧的 `toolCallId` / `title` / `rawInput` 形状 → 决定要不要工具映射。
2. `session/new` 的 `configOptions` 里有没有 `model`、模型 id 长什么样。
3. 注入 MCP 服务后工具是否真的出现。
4. `session/load` / `session/resume` 是否真回放。
5. 插话：`_meta.steering.supported=true` 只是广告，注入是否成功、是否回显插入帧都要真跑。
6. 权限：`session/request_permission` 是否触发、审批文案。

## 7. 复现命令

```bash
mkdir -p /tmp/traehome && HOME=/tmp/traehome \
  sh -c "$(curl -fsSL https://trae.cn/trae-cli/install_v2.sh)"
export HOME=/tmp/traehome
/tmp/traehome/.local/bin/traecli --version           # traecli 0.216.0 / installation: public
/tmp/traehome/.local/bin/traecli acp --help          # serve: Start the ACP server over stdio

cd /tmp && mkdir -p probe-trae && cd probe-trae
python3 /tmp/acp_probe.py --cmd "/tmp/traehome/.local/bin/traecli acp serve" \
  --cwd /tmp/probe-trae --timeout 30
# 一轮对话需要企业登录：TRAECLI_PERSONAL_ACCESS_TOKEN=trae-lt-…
```

## 8. 测试

```bash
go test ./internal/ai/backends/trae/ -count=1
```

钉住：命令、排序、`ACPLoadSession`、安装命令、技能声明、`KeyOnly` 注入与
「带端点的 provider 必须被拒」、孤儿识别、模型源报「未知」。
