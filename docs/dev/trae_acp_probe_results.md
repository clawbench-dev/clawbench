# TRAE（TraeCode CLI / `traecli`）ACP 探测记录

日期：2026-11-24。结论先说：**没有拿到二进制，一帧都没有抓到。** 下面「实现」一节的每个字段都在代码注释里标了来源，凡是没有实测或第三方文档支撑的，一律不填。

## 1. 能不能拿到 CLI

| 途径 | 结果 |
|---|---|
| 本机已安装 | 无 `traecli` / `trae` / `trae-cli`（`which` 全部落空，`/tmp/cli-probe` 里也只有 qwen / cline / copilot） |
| npm `traecli` | E404 |
| npm `trae-cli` | E404 |
| npm `trae` | 存在，但是一个无关的浏览器 HTTP 客户端库（`huemul/trae`），不是 TraeCode CLI |
| 官方文档 `https://docs.trae.cn/cli_what-is-trae-cli` | 404；`docs.trae.cn` 根路径返回 400。`docs.trae.ai` 能打开，但这个是 IDE 文档站，站内没有任何 `traecli` / `acp` 字样 |
| `www.trae.cn` / `www.trae.ai` / `/download` | 200，但页面里没有 CLI 下载入口 |
| GitHub | 没有官方仓库；`firerlAGI/TraeCli` 是个人 Python 项目（5 star，无描述），与本 CLI 无关 |

**拿不到的根因**：TraeCode CLI 只对 **TRAE 企业旗舰版（Enterprise Flagship）客户**开放（第三方适配器 README 与官方文档口径一致）。没有订阅就拿不到安装包，也不应该去注册或购买（任务边界）。

## 2. 探测帧

无。`initialize` / `session/new` / `session/prompt` **全部未抓取**，所以以下事实**全部未知**：

- `agentInfo`（name / version）
- `agentCapabilities`：`loadSession`、`sessionCapabilities`、`mcpCapabilities`、`promptCapabilities`
- `authMethods`（ACP `authenticate` 的 methodId）
- `session/new` 是否 `-32000 Authentication required`
- 模型列表怎么来
- 工具调用的名字与参数形状
- `session/load` 是否可用

## 3. 实现依据：只有第三方适配器 README

唯一信息源是 `@tunnelbox/trae@0.1.37`（npm，2026-10-24 发布）的 README。**它的可信度是「中等」**：作者自己在 README 里写「Standard ACP surface assumed; **verify with `npm run probe`** on a real TraeCode CLI」，也就是说它同样没有能力矩阵，只是按标准 ACP 假设。

从该 README 采信、并写进代码的事实：

| 事实 | 用在哪 |
|---|---|
| 二进制名 `traecli` | `DefaultCmd` |
| `traecli acp serve`，JSON-RPC over stdio | `AcpCommand` |
| 先 `traecli login`；ACP `authenticate` 可跳过（默认不调） | `LoginCmd` |
| 流式 `session/update`（`agent_message_chunk` / `agent_thought_chunk`） | 不加 `Behavior` 覆盖（走公共解析） |
| `session/request_permission` 审批、`session/cancel` 中止 | 同上，公共路径已覆盖 |
| 会话续接可用 `session/resume`，而 `session/load` 是「when advertised」 | 因此 `ACPLoadSession` 保持默认 false |

README 还提到 `--permission-mode`（`default` / `auto` / `bypass_permissions`）与 `-y`，但这些是用户侧开关，ClawBench 不需要强制，没有接。

## 4. 实现清单与每一处的置信度

`internal/ai/backends/trae/register.go`：

| 字段 | 值 | 置信度 |
|---|---|---|
| `ID` / `Backend` | `trae` | 项目命名约定 |
| `DefaultCmd` | `traecli` | 第三方 |
| `Name` / `Specialty` | `TRAE` / 字节 TRAE 企业版编码智能体 | 项目约定 |
| `AcpCommand` | `traecli acp serve` | 第三方，**未实测** |
| `LoginCmd` | `traecli login` | 第三方，**未实测** |
| `SortOrder` | 26 | lead 分配 |
| `ACPLoadSession` | 不设（false） | 未知，故意不猜 |
| `InstallCmd` | 不设 | 没有公开安装方式，写了就是坏按钮 |
| `ThinkingEffortLevels` | 不设 | 未知 |
| `NativeSkillsDirs` | 不设 | 未知 |
| `MCPUnsupported` | 不设 | 未知（没跑过带 mcpServers 的 session/new） |
| `ACP`（工具映射） | nil | 没有抓过工具帧，不能凭名字猜 |
| `Behavior` | nil | 同上 |
| `ProviderEnv` | `&model.ProviderEnvSpec{}`（空但存在） | 企业订阅，无 BYOK 依据；空 spec = 「不能绑 provider」 |
| `ModelSource` | `PluginSource("trae", discoverTraeModels)`，探针恒定返回空 + 原因 | 没有可信模型源；宁可不列也不猜 id |
| `OrphanCmdline` | `{traecli, "acp serve"}` | 按启动形状推导，**未实测** |

`discovery.go`：`discoverTraeModels` 永远返回 `nil, "trae: model list unknown …"`。这样 UI 显示「未知」，而不是给出一个第一次对话就会失败的模型 id。若将来实测到 `traecli` 有 `models` 子命令或 `session/new` 里带 `model` 配置项，替换成真实探针即可。

注意：`internal/ai/backends/all/all.go` 的注册行由 lead 统一加（我的边界不允许改）。

## 5. 拿到二进制后应该补测什么

按 `/tmp/acp_probe.py` 跑：

```bash
mkdir -p /tmp/probe-trae && cd /tmp/probe-trae
python3 /tmp/acp_probe.py --cmd "traecli acp serve" --cwd /tmp/probe-trae --timeout 30
python3 /tmp/acp_probe.py --cmd "traecli acp serve" --prompt "say hi" --timeout 90   # 需先 traecli login
```

优先确认（按影响排序）：

1. `initialize` 的 `agentCapabilities.loadSession` → 决定 `ACPLoadSession` 是否打开。
2. `authMethods` → 若存在，考虑在 `LoginCmd` 之外补 `authenticate` 通路。
3. 模型：`traecli --help` / `traecli models` 有没有；`session/new` 返回的 `configOptions` 里有没有 `model` → 决定 `ModelSource` 换静态 catalog 还是插件探针。
4. 第一个工具调用的 `toolCallId` / `title` / `rawInput` 形状 → 决定要不要加 `ToolCallIDPrefixes` / `InputRemaps`。
5. `session/new` 传 `mcpServers` 后 MCP 工具是否真的出现 → 决定 `MCPUnsupported`。
6. 能否 BYOK（`-c` 覆盖或环境变量里有 `BASE_URL` / `API_KEY`）→ 决定 `ProviderEnv` 是否保持空。

## 6. 测试

```bash
go test ./internal/ai/backends/trae/ -count=1 -cover
# ok  clawbench/internal/ai/backends/trae  0.01s  coverage: 100.0% of statements
```

测试只钉注册字段（命令、排序、LoginCmd、空 ProviderEnv、OrphanCmdline、模型源报「未知」），**没有伪造任何协议行为断言**——因为不存在真实的帧可以断言。
