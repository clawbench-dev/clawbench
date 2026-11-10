# 各后端「模型提供商」配置方式调研（多提供商可行性）

> 调研日期：2026-11-10 ｜ 状态：**仅调研，未改代码**
>
> 触发：现在的提供商注入只有环境变量一种方式。环境变量是**进程级**的，因此「一个智能体一个提供商」已经成立，
> 但「同一个智能体里混用几家的模型」做不到。目标是确认：改成写各 CLI 自己的配置文件后，哪些后端能支持多提供商。
>
> 证据分四档，文中逐条标注：
>
> - **实测**：本机装了该 CLI，读它的配置目录 / 跑 `--help` / 读它自己的文档；
> - **源码**：读了该 CLI 发布包或上游仓库的实现；
> - **仓库**：沿用 `internal/model/provider_env.go`、`internal/service/provider_config.go` 与 `docs/dev/` 既有探针结论；
> - **未验证**：没有直接证据，只能按「暂不支持」处理，不作为结论依据。

## 一、现状（作为对照）

`internal/model/provider_env.go` 的 `providerEnvTable` 是唯一事实来源，按「后端 × 协议」给出环境变量名。
三处 spawn 点（CLI / codex 流 / ACP）在 `os.Environ()` 之后追加，重复键后者生效 —— 也就是说：

- 一个进程只能被指向**一个**端点；
- 因为一个智能体一个进程，所以**每个智能体各绑各的提供商是已经支持的**；
- 缺少的是「一个智能体 → 多个提供商」，以及某些后端其实能用配置文件、而我们只发了环境变量。

配置文件的两种注入形态要分清，后面会反复用到：

| 形态 | 含义 | 例子 |
|---|---|---|
| **配置内容** | 不落盘，进程启动时用变量塞一整份配置 | `OPENCODE_CONFIG_CONTENT`、`MIMOCODE_CONFIG_CONTENT` |
| **托管块** | 写进用户的配置文件，但只写自己那一块，并留备份 | `~/.codex/config.toml` 的 `# >>> clawbench provider >>>` |

## 二、逐后端结论

### 2.1 能放多个提供商

#### opencode —— 已经是多提供商结构（实测 2.2.7）

- `~/.config/opencode/opencode.json`：`provider` 是一张表，每项 `{npm, name, options.baseURL, options.apiKey, models}`；
  模型寻址是 `<provider>/<model>`。
- 本机实例就挂着 `codebuddy` 一家自定义 provider（3 个模型），`opencode models` 直接输出 `codebuddy/auto`、
  `codebuddy/deepseek-v4.1-flash` 等 → **多提供商共存已实证**，不是理论。
- 进程内注入：`OPENCODE_CONFIG_CONTENT`（我们已经在用）。密钥用 `{env:VAR}` 插值，不落盘。

#### mimo —— opencode 换皮，但我们现在只发环境变量（源码 0.1.16）

- 包是启动器，真正的 CLI 是 opencode 派生代码：配置项、`provider` schema、`<provider>/<model>` 寻址都一致。
- `packages/cli/src/config/paths.ts`：配置目录 = 全局配置目录 + 项目 `.mimocode/` + `MIMOCODE_CONFIG_DIR`。
- `packages/cli/src/config/config.ts:699`：`source === "MIMOCODE_CONFIG_CONTENT"` → **有和 opencode 同款的配置内容通道**。
- 结论：**能多提供商**，而且改动量最小（照抄 opencode 那一条注入即可）。这是本次最划算的一处。

#### kimi —— providers / models 两张表（实测 1.46.0）

- `~/.kimi/config.toml`：`[provider]` 只是当前默认那一家的简写；完整结构是 `providers: dict[str, LLMProvider]` +
  `models: dict[str, LLMModel]`，模型用 `provider` 字段指回某一家（`kimi_cli/config.py`）。
- `LLMProvider` 有 `type` / `base_url` / `api_key` / `env` / `custom_headers`。
- 进程内注入：`--config <TOML/JSON 字符串>` 或 `--config-file <路径>`；二者互斥，且 `--config` 是**整份替换**
  （`cli/__init__.py:504` 起），不是合并 —— 要用它就必须自己先读用户配置、合并后再传。
- 密钥：配置里存明文（`SecretStr` 只做显示遮蔽），没找到 `${ENV}` 引用。

#### codex —— 文件早已支持多家，我们只写了一家（本机配置 + 仓库）

- `~/.codex/config.toml`：`[model_providers.<id>]` 可以有多块（本机就有 `minimax` 和 `deepseek` 两家），
  每块 `base_url` / `env_key` / `wire_api`；顶层 `model_provider` 选默认。
- 本机配置里还出现了 `[models.MiniMax-M3] model_provider = "minimax"`，说明**可以按模型指定提供商**
  （版本 0.167 的写法；生效与否未单独实测，标为待验证）。
- 我们现在的实现（`internal/service/provider_config.go`）：只写 `[model_providers.clawbench]` 一块 + 顶层键，
  已有备份 / 解绑还原 / 冲突检测。扩成多家的成本不高。
- 密钥：`env_key` 指向环境变量，不落盘。

#### codebuddy —— 每个模型自带端点和密钥（实测 2.180.0 + 官方文档）

- `~/.codebuddy/models.json`（用户级）与 `<项目>/.codebuddy/models.json`（项目级，优先级更高）。
- `models[]` 每项：`id / name / vendor / url / apiKey / maxInputTokens / maxOutputTokens / supportsToolCall …`。
  **`url` 和 `apiKey` 是逐模型的**，且两者都支持 `${ENV}` 引用（官方 `docs/cli/models.md`「Security Configuration」）。
- 结论：**能多提供商，粒度比"按提供商"还细（按模型）**。项目级文件还能不碰用户家目录，代价是往用户仓库里写文件。

#### grok —— 按模型挂端点（实测 1.0.46 + 自带 README）

- `~/.grok/config.toml` 的 `[model.<name>]`：`model` / `base_url` / `api_key` / `env_key` / `api_backend` 等，
  每个模型可以指向不同端点；未写的字段继承内置模型。
- `GROK_HOME` 可覆盖配置目录（`~/.grok`）；`GROK_XAI_API_BASE_URL` 是我们现在用的全局端点开关。
- 密钥：`env_key` 可引用环境变量，不必写明文。

#### hermes —— custom_providers 本来就是列表（实测 0.21.7）

- `~/.hermes/config.yaml`：`model.default` / `model.provider` / `model.base_url`，另有 **`custom_providers:` 列表**
  和 `fallback_providers:` 列表；`--provider` / `-m` 可临时指定。
- 结论：**能多提供商**（一家主用 + 多家兜底/自定义）。未见配置目录覆盖变量；`--ignore-user-config` 只能忽略、不能替换。

#### pi —— models.json 的 providers 表（实测 1.5.0 + 自带 docs）

- `<agent-dir>/models.json`：`providers` 表，每项 `name` / `baseUrl` / `apiKey` / `api` / `models`；
  `apiKey` 支持字面值、`$ENV`、`!command` 三种写法。
- `auth.json` 按提供商存凭据；`models-store.json` 是目录缓存；`PI_CODING_AGENT_DIR` 覆盖配置目录。
- 模型寻址 `provider/id`，`--provider` + `--model` 可直接选。
- 结论：**能多提供商**。

#### zcode —— 结构和 opencode 一样（本机 `~/.zcode/v2/config.json`）

- `provider.<id>.{name, kind, options.apiKey, options.baseURL, models}`，ACP 桥 `zcode-acp-server` 拉起真引擎后读它。
- 结论：结构上**能多提供商**；但「写进去后端到端生效」没有实测，标为待验证。
- 密钥：明文。

#### openclaw —— 能配，但配置属于网关（实测 2026.10.6）

- `~/.openclaw/openclaw.json`：`models.providers.<id>.{baseUrl, apiKey, api, models}`，密钥可写 `${ENV}`。
- 但按 `AGENTS.md` 的定性：这部分属于 **Gateway 进程**，不是我们起的 ACP 桥；且 `--profile` 会把状态目录
  切到 `~/.openclaw-<name>`。
- 结论：网关侧能多提供商；我们若要走这条，改的是「一台共享网关」，不是每个智能体的私有配置。

### 2.2 只能一家（写配置文件也造不出第二家）

#### claude

- `settings.json` 支持 `env`（把 `ANTHROPIC_BASE_URL` 等写进配置）；**`--settings <JSON 或路径>` 可在单次启动时叠加**
  （官方设置文档：优先级高于用户/项目/本地，低于企业托管）。所以「不碰家目录地给某个智能体指定一家」是可行的。
- 但一个进程只认一个端点 → **不能同时挂多家**。
- 真要跨家切换，只有 ACP 的 `providers/set`（`docs/dev/acp_provider_routing.md`：进程内生效、不写盘、UNSTABLE）；
  该文档的结论是「保持统一，暂不实现」。

#### deepseek（CodeWhale）/ dsh

- 现在吃 `OPENAI_BASE_URL` / `OPENAI_API_KEY`（dsh 走 `DEEPSEEK_API_KEY` 或它自己的凭据库，不是我们注入）。
- 没找到可用的多提供商配置文件证据。dsh 是插件架构（Cordis），配置面很大但**未验证**，按「暂不支持」处理。

#### antigravity

- 只确认了 `GOOGLE_GEMINI_BASE_URL` / `GEMINI_API_KEY`（`agy models` 那条链路）。没有配置文件证据。

### 2.3 不能配

| 后端 | 依据 |
|---|---|
| qoder / qodercn | `~/.qoder/settings.json` 只有 `permissions` / `security` / `securityScan`；登录态在 `.auth`，无提供商配置 |
| copilot | 只有 GitHub 登录，没有自定义端点的证据 |
| cursor | 只有 `CURSOR_API_KEY`，不支持自定义端点（仓库既有结论） |
| vecli | `@volcengine/vecli` 上游仓库返回 404，没有任何可用证据 |

### 2.4 配置能写多个，但同一时刻只生效一个

#### commandcode

- `~/.commandcode/providers.json` + `~/.commandcode/config.json`（`docs/dev/commandcode_acp_probe_results.md`）。
- 密钥**只接受引用形式**（`$ENV` / `{env:VAR}` / `!command`），裸密钥会被忽略并告警。
- `config.json` 是**全局单例**：多个进程各自写不同 provider 会互相覆盖。可行做法是按 provider 串行 spawn，
  或给每个 provider 一个独立 `HOME`（但登录态也会被一起隔离）。
- 结论：文件里能写多家，但一个进程只跑 config.json 指的那一家 → 实质仍是「一进程一家」。

## 三、能力汇总

| 后端 | 现在怎么接 | 配置文件 | 多提供商 | 不碰家目录的注入方式 | 密钥可否只用环境变量 |
|---|---|---|---|---|---|
| opencode | 环境变量 + 配置内容 | `opencode.json` | **能** | `OPENCODE_CONFIG_CONTENT` | 能（`{env:VAR}`） |
| mimo | 仅环境变量 | `mimocode.json(c)` | **能** | `MIMOCODE_CONFIG_CONTENT` | 能（同 opencode） |
| kimi | 环境变量 | `~/.kimi/config.toml` | **能** | `--config` / `--config-file`（整份替换） | 未见，需明文 |
| codex | 托管块 | `~/.codex/config.toml` | **能** | 托管块（现状） | 能（`env_key`） |
| codebuddy | 环境变量 | `models.json` | **能（逐模型）** | 项目级 `.codebuddy/models.json` | 能（`${ENV}`） |
| grok | 环境变量 | `~/.grok/config.toml` | **能（逐模型）** | 会改家目录；或 `GROK_HOME` | 能（`env_key`） |
| hermes | 环境变量 | `~/.hermes/config.yaml` | **能** | 未见目录覆盖 | 需明文 |
| pi | 环境变量 | `models.json` | **能** | `PI_CODING_AGENT_DIR` | 能（`$ENV`） |
| zcode | 环境变量 | `~/.zcode/v2/config.json` | 结构能（未实测） | 未见 | 需明文 |
| openclaw | 不支持（网关持有） | `openclaw.json` | 能（网关侧） | `--profile` 隔离状态目录 | 能（`${ENV}`） |
| commandcode | 仅密钥 | `providers.json` + `config.json` | 写得了多个，只生效一个 | 独立 `HOME`（连登录一起隔离） | 只能用引用形式 |
| claude | 环境变量 | `settings.json` | **不能** | `--settings <JSON>` | 能 |
| deepseek / dsh | 环境变量 | 未确认 | 未确认 | — | — |
| antigravity | 环境变量 | 未见 | 未确认 | — | — |
| qoder / qodercn / copilot / cursor / vecli | 不支持（或仅密钥） | — | 不能 | — | — |

## 四、真要实施，三个绕不开的难点

### 4.1 密钥要不要落盘

可以只用环境变量引用的：opencode、mimo、codebuddy、codex、grok、pi、openclaw。
必须写明文的：kimi、hermes、zcode。commandcode 只能写引用、且被并发写覆盖。

写明文意味着「谁拿到配置文件谁就有钥匙」，和现在「密钥只在数据库 + spawn 时进环境」的安全边界不同。
若采用，至少要做到：与 `providers.api_key` 回显策略一致（默认不回显）、解绑时清除、启动时对账。

### 4.2 多个进程抢同一个配置文件

codex 现在是**单一全局文件**（`AGENTS.md` 已列为已知限制：同一后端只能有一个生效 provider）。
commandcode 的调研直接踩过：两个进程各写各的 `config.json` 会互相覆盖。

可选出路只有三条，各有代价：

1. 全局一份 + 串行 spawn（简单，但并发回合会排队）；
2. 每个智能体一个独立配置目录（`CODEX_HOME` / `PI_CODING_AGENT_DIR` / `GROK_HOME` / `--profile`），
   但要额外处理登录态与用户已有配置的合并；
3. 只用「配置内容」型通道（opencode / mimo 那种），完全不落盘 —— **最干净，但只有这两个后端有**。

### 4.3 数据结构要动

现在是「一个智能体 → 一个 `provider_id`」。要支持多家，必须选一种：

- **方案 A：一对多**。`agents.provider_ids`（或一张关联表）；模型列表按提供商分组展示。
  选模型时仍需知道这个模型属于哪家 → 模型 id 必须带归属信息。
- **方案 B：每模型带提供商**。模型条目上加 `providerId`（空 = 用智能体默认）。
  更贴近 codebuddy / grok 这类「逐模型」后端，但对「按 provider 切换」的后端要多做一层映射。
- **方案 C：分后端两种语义**。能逐模型的后端用 B，只能按进程的后端用 A（且同一进程仍只有一家生效）。

无论哪种，都要复用 #651 的教训：**「提供商前缀」只在后端内部用于寻址，必须按模型归属判断，不能见谁都贴**
（见 `internal/model/agent.go` 的 `providerModelID`）。否则就是群聊 400 那次的翻版。

## 五、建议的推进顺序（未执行）

**第一档：不改数据结构，先把已有的能力用上**

1. **mimo**：照抄 opencode 的配置内容注入，直接获得多提供商 + 自定义模型。成本最低、收益最直接。
2. **codebuddy**：改用 `models.json`（逐模型端点 + `${ENV}`），替代现在只能一家的 `CODEBUDDY_*`。
3. **codex**：托管块扩成「按需写多家 `[model_providers.*]`」，默认用哪家仍由顶层键或按模型指定。

**第二档：动数据结构**

4. 定 A / B / C 其中一种，改设置页与模型下拉。
5. 再接 kimi / pi / grok / hermes / zcode（每个都要单独处理「合并用户配置」与密钥落盘）。

**明确不做**：claude 的多提供商（进程级限制，只能走已搁置的 ACP `providers/set`）、
qoder / copilot / cursor / vecli（后端本身不给配）。

## 六、验证方法（复现用）

```bash
# opencode：看 provider 表与模型寻址
cat ~/.config/opencode/opencode.json | python3 -c "import json,sys;print(list(json.load(sys.stdin).get('provider',{})))"
opencode models | head

# mimo：配置内容通道存在（看上游源码，opencode 派生）
curl -sL https://raw.githubusercontent.com/XiaomiMiMo/MiMo-Code/main/packages/cli/src/config/config.ts \
  | grep -n "MIMOCODE_CONFIG_CONTENT"
curl -sL https://raw.githubusercontent.com/XiaomiMiMo/MiMo-Code/main/packages/cli/src/config/paths.ts \
  | grep -n "MIMOCODE_CONFIG_DIR"

# kimi：providers / models 结构（uv 安装目录）
grep -n "providers: dict\|models: dict" ~/.local/share/uv/tools/kimi-cli/lib/python*/site-packages/kimi_cli/config.py

# codex：现有托管块与多家写法
grep -n "model_provider\|\[model_providers" ~/.codex/config.toml

# codebuddy：逐模型端点 + ${ENV}
python3 -c "import json,os;d=json.load(open(os.path.expanduser('~/.codebuddy/models.json')));print([(m['id'],m.get('url'),m['apiKey'][:8]) for m in d['models']][:3])"

# grok / hermes / pi / zcode：各自的配置表
grep -n "^\[model\." ~/.grok/config.toml | head
grep -n -A3 "custom_providers" ~/.hermes/config.yaml | head
python3 -c "import json,os;print(list(json.load(open(os.path.expanduser('~/.pi/agent/models.json')))['providers']))"
python3 -c "import json,os;print(list(json.load(open(os.path.expanduser('~/.zcode/v2/config.json')))['provider']))"

# openclaw：网关侧 providers
python3 -c "import json,os;print(list(json.load(open(os.path.expanduser('~/.openclaw/openclaw.json')))['models']['providers']))"
```

## 七、本次调研的局限

- **codex / claude 本机没有可执行文件**：codex 的结论来自本机配置文件与仓库实现；claude 的 `--settings` 来自官方设置文档，
  均未跑端到端回合。
- **zcode**：结构与 opencode 一致是直接读文件得出的，写入后是否被 ACP 桥拾取未实测。
- **dsh / vecli / antigravity**：没有足够证据，文中已标「未确认」，不应据此做实现假设。
- **codex 的按模型 `model_provider`（`[models.X]`）** 只有本机配置样例，未单独验证生效。
- commandcode 的并发结论沿用 `docs/dev/commandcode_acp_probe_results.md`，本次未复跑。
