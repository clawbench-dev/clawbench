# 统一 MCP 配置（全局定义 + 按智能体启用）设计

日期：2026-10-19
状态：设计待评审
关联 issue：[#558](https://github.com/clawbench-dev/clawbench/issues/558)
前置调研：[`docs/dev/acp_mcp_probe_results.md`](../dev/acp_mcp_probe_results.md)（17 个后端实测）

## 一、要解决的问题

ClawBench 目前完全不管理 MCP：

- 传给 ACP 的 `mcpServers` 是**四处硬编码的空切片**，这条链路从未被使用过。
- 用户只能去改每个 CLI 自己的配置文件（Claude 的 `.mcp.json`、Codex 的 `config.toml`…），
  格式各不相同，且每个智能体各配一遍。
- 唯一例外是 CodeBuddy：`acp_conn_lifecycle.go:697-712` 读 `~/.codebuddy/.mcp.json`
  再用 `--mcp-config` 注入（#270 的 workaround）。

目标：**在 ClawBench 里配置一次，所有 ACP 智能体默认可用，也能按智能体单独开关。**

## 二、调研结论对设计的约束

探针已实测（详见前置文档），这些结论直接决定方案形状：

| 结论 | 对设计的影响 |
|---|---|
| 6 个后端 `session/new` + resume + load 全部保留 MCP | P0 可以安全实现，不存在「重启后工具消失」 |
| 6 个后端 http 可用，4 个 sse 可用，能力位广告与实际一致 | **必须按能力位过滤**；不过滤会让 codex 直接报错 |
| CodeBuddy 原生支持协议字段 | `--mcp-config` 可迁移，但**不能直接删**（现有用户配置在该文件里，见 §5.4） |
| hermes 异步挂载（首轮拿不到工具） | 需要在 UI 说明，不能承诺「配完立刻可用」 |
| dsh / deepseek / pi 接受字段但完全忽略 | **执行层黑名单**拦截，UI 标注只是提示（默认 `all` 会绕过纯 UI 层） |
| CLI 传输没有 `mcpServers` 参数 | **MCP 仅 ACP 模式生效**，产品上必须写清楚 |

## 三、决策汇总

| 维度 | 决策 | 理由 |
|---|---|---|
| 配置存放 | **全局唯一**（`config.yaml` 的 `mcp.servers`） | MCP 定义含 API key；按智能体各存一份会导致密钥散落、改一个 key 要改 N 处 |
| 按智能体配置 | 只存**启用策略**（`all` / `none` / `selected`） | 定义与策略分离，业界一致（Claude 的 `enabledMcpjsonServers`、CodeBuddy 的 `AgentScopedMcp`） |
| 传输类型 | stdio / http / sse 三种，**按能力位过滤** | 探针验证：能力位广告与实际行为一致 |
| 生效范围 | **仅 ACP 传输** | CLI 后端无此参数，只能改各自配置文件 |
| 配置变更生效 | **期望列表**哈希比对 → 下一轮对话前静默重连 | 不打断正在跑的对话；版本不含能力位（见 §6.4） |
| 密钥回显 | **明文返回**（`env` / `headers` 原值） | 与 STT / RAG / TTS 的 `api_key` 一致，避免设置页出现两种行为 |
| hermes 首轮不可用 | **仅 UI 提示**，不自动重试 | 重试增加延迟与不确定性；用户重发一次即可，行为可预期 |
| 不支持的智能体 | **执行层黑名单 + UI 标灰** | 置灰只是提示；默认 `all` 模式下必须由执行层拦截（见 §5.3） |
| CodeBuddy workaround | **保留兼容期**：`mcp.servers` 为空时仍走 `--mcp-config` | 直接删会让现有 `~/.codebuddy/.mcp.json` 用户静默失去工具（见 §5.4） |
| 入口 | 设置新增「MCP 服务」分类 + 智能体详情页加一节 | 与「技能」的结构对齐 |

> **本版修订说明**：初稿的 §6 建立在错误的连接生命周期模型上（`ACPConn.agent` 是一次性快照，
> 而 `UpdateAgent` 是 copy-on-write，见 §6.1），按初稿实现会出现「按智能体改策略永不生效」。
> §5.1 的能力位采集也漏了 `ForceUpdate` 的全量替换语义。这两处已重写。

## 四、数据模型

### 4.1 全局配置（`config.yaml`）

```yaml
mcp:
  servers:
    - name: websearch            # 唯一标识，也用作 ACP 的 server name
      transport: stdio           # stdio | http | sse
      command: node              # stdio 必填
      args: [/opt/websearch/index.js]
      env:                       # stdio 可选
        EXA_API_KEY: xxx
      # url / headers 仅 http|sse 使用
    - name: remote-tools
      transport: http
      url: https://example.com/mcp
      headers:
        Authorization: Bearer xxx
```

Go 结构：

```go
// internal/model/mcp.go（新文件）
type MCPConfig struct {
    Servers []MCPServer `yaml:"servers"`
}

type MCPServer struct {
    Name      string            `yaml:"name"`
    Transport string            `yaml:"transport"` // stdio | http | sse
    Command   string            `yaml:"command,omitempty"`
    Args      []string          `yaml:"args,omitempty"`
    Env       map[string]string `yaml:"env,omitempty"`
    URL       string            `yaml:"url,omitempty"`
    Headers   map[string]string `yaml:"headers,omitempty"`
}
```

`Config` 加一行：`MCP MCPConfig \`yaml:"mcp"\``（`internal/model/config.go:156` 附近，与 `Skills` / `UsageMeter` 并列）。

### 4.2 按智能体的启用策略

`agents` 表新增列：

```sql
ALTER TABLE agents ADD COLUMN mcp_servers TEXT NOT NULL DEFAULT '{"mode":"all","names":[]}'
```

`model.Agent` 新增字段：

```go
// MCPServers 是该智能体的 MCP 启用策略。
//   {"mode":"all","names":[]}            全部启用（默认）
//   {"mode":"none","names":[]}           全部禁用
//   {"mode":"selected","names":["a","b"]} 只启用列出的
//
// 只存策略不存定义：定义在 config.yaml，改一处全局生效。
MCPServers MCPServerPolicy `json:"mcpServers"`
```

**默认 `all` 的理由**：全局列表为空时，`all` 等价于什么都没配，因此默认值安全；
用户新增服务后无需逐个智能体勾选就能用，符合「全局可用」的产品预期。

**零值语义（必须写死）**：`MCPServerPolicy{}` 的 `mode` 是空串。**空 mode 等价于 `all`**，
与 DB 默认一致。`json.Unmarshal` 失败时保留默认值（`all`），不能留空——
否则 `ResolveMCPServers` 会把空 mode 解释成 `none`，智能体静默失去 MCP。

**迁移写法**（必须套 `pragma_table_info` 守卫，本仓惯例）：

```go
var hasAgentMCPServers int
_ = store.ReadDB().QueryRow(
    "SELECT COUNT(*) FROM pragma_table_info('agents') WHERE name='mcp_servers'").Scan(&hasAgentMCPServers)
if hasAgentMCPServers == 0 {
    if _, err := store.WriteExec(
        `ALTER TABLE agents ADD COLUMN mcp_servers TEXT NOT NULL DEFAULT '{"mode":"all","names":[]}'`); err != nil {
        return fmt.Errorf("failed to add mcp_servers column: %w", err)
    }
}
```

同时 `agent_store.go:80` 的 `CREATE TABLE agents` 也要加这一列，否则全新安装与
升级安装 schema 不一致。

### 4.3 策略解析（必须现读，不能用 `c.agent`）

```go
// ResolveMCPServers 返回该智能体应注入的 server 列表。
//
// agentID 而非 *Agent：调用方必须传 c.agent.ID，函数内部走 model.GetAgent 现读。
// 绝不能把 c.agent 直接传进来 —— 见 §6.1。
func (c *Config) ResolveMCPServers(agentID string, caps MCPCapabilities) []MCPServer
```

**为什么必须现读**：`ACPConn.agent` 是创建时的一次性快照
（`acp_pool.go:903-905` 注释「set once in newACPConn, never modified」，
`newACPConn` 在 `acp_pool.go:1423`），而 `model.UpdateAgent` 是 copy-on-write
（`agent.go:262-283`：发布副本，旧指针永远看到旧值）。若用 `c.agent` 读策略，
用户在设置页把 `all` 改成 `none` 后，已存在的连接永远看不到变更。

### 4.4 `MCPCapabilities` 的定义位置

`internal/model` 不能 import `internal/ai`（`discovery.go:53-56` 用函数变量注入
就是为了绕开这个环）。因此 `MCPCapabilities` 必须定义在 `internal/model/mcp.go`：

```go
type MCPCapabilities struct {
    HTTP bool
    SSE  bool
}
```

`internal/ai` 侧把 registry 里的能力位转换成这个结构再传进来。

### 4.5 版本号的定义（不含能力位）

```go
// MCPPolicyVersion 返回"期望列表"的哈希：全局配置 + 该智能体的策略，
// **不含能力位**。空列表返回 ""。
func (c *Config) MCPPolicyVersion(agentID string) string
```

**为什么不含能力位**：能力位在 `Initialize` 时才拿到，而 `Initialize` 发生在
`spawnLocked` 内部（`acp_conn_lifecycle.go:820`），早于全部四个注入点。
若版本在 spawn 之前算（`ensureAliveWithSession` 入口），此刻能力位为空，
版本只反映 stdio，而实际 `NewSession` 发的是完整列表 → 下一轮必然再重连一次。

代价：给 codex 加一个 sse server 会触发一次无谓重连。这个代价远小于
「每个新进程至少重连一次」，也小于「能力位必须持久化才能跨重启稳定」。

**哈希必须稳定**：`env` / `headers` 是 map，`range` 顺序随机。序列化前必须按 key
排序，否则版本号每轮都变，退化成每轮重连。

## 五、注入点（必须一次改全）

| 位置 | 当前 | 改为 |
|---|---|---|
| `acp_conn_lifecycle.go:308` | `McpServers: []acp.McpServer{}` | `McpServers: c.mcpServersForSession()` |
| `acp_conn_lifecycle.go:441` | 同上（`session/load`） | 同上 |
| `acp_conn_lifecycle.go:501` | 同上（`session/resume`） | 同上 |
| `acp_pool.go:1614` | 同上（`SyncLoadSession`） | 同上 |
| `acp_conn_lifecycle.go:697-712` | CodeBuddy `--mcp-config` | **保留兼容期**（见 §5.4） |

四处共用一个方法，避免以后新增路径时漏改：

```go
// mcpServersForSession 把配置转换成 ACP 结构，并按该 agent 广告的能力位过滤。
// 内部经 model.GetAgent(c.agent.ID) 现读策略，不读 c.agent（见 §6.1）。
func (c *ACPConn) mcpServersForSession() []acp.McpServer
```

### 5.1 能力位采集（当前完全缺失，共 7 处触点）

`acp_conn_lifecycle.go:846-857` 只提取 `listSessions` / `deleteSession`，
`initResp.AgentCapabilities.McpCapabilities` 被直接丢弃。**只加字段不够**，
能力位有 7 个触点，漏任何一处都会让 http/sse 静默消失：

| # | 位置 | 改什么 |
|---|---|---|
| 1 | `agent_capability.go:28` `AgentCapability` | 加 `McpHTTP` / `McpSSE` 两个 `*bool` |
| 2 | `agent_capability.go:115-156` `merge`（经 `Update` 调用，`agent_capability.go:101`） | 逐字段 `if src.McpHTTP != nil` 覆盖 |
| 3 | `agent_capability.go:55-66` `HasData` | 计入非空判定 |
| 4 | `agent_capability.go:205-234` `ForceUpdate` | **全量替换**（`r.caps[agentID] = agentCap`），必须带上这两个字段 |
| 5 | `agent_capability.go:244-255` `ForceUpdateIfNeeded` + 调用点 `acp_conn_state.go:261` | **位置参数**，加两个参数；调用点从 registry 现读再传（与 `listSessions` 同一写法） |
| 6 | `agent_capability.go:804-853` `saveToDB` | 固定列清单 |
| 7 | `agent_capability.go:855-900` `LoadFromDB` | 固定列清单 |

**第 4/5 条是最容易漏的**：`ForceUpdate` 是 `r.caps[agentID] = agentCap` 全量替换，
而 `applyExtractedState` 每轮首次 new/resume 都会调 `ForceUpdateIfNeeded`
（`acp_conn_state.go:261`），它构造的 `AgentCapability` 只填传入的那几个字段。
仓库已经在 `listSessions` 上踩过并留了注释（`acp_conn_state.go:252-260`：
「ListSessions from the registry — it must be carried through because ForceUpdate
fully replaces the AgentCapability struct」）。

照抄那个写法：

```go
// acp_conn_state.go，与 listSessions 并列
mcpHTTP, mcpSSE := reg.GetMCPCapabilities(agentID)
reg.ForceUpdateIfNeeded(agentID, ext.modes, ext.efforts, ext.models, nil,
    ext.configState, loadSession, listSessions, mcpHTTP, mcpSSE)
```

症状：第一轮 http/sse 能用，第二轮能力位变 `nil`，服务器被过滤掉，
用户看到「配了但没生效」——与 §5.2 想避免的完全一样。

### 5.2 过滤掉的要能被用户看到

过滤发生在运行时，但**提示必须前置到配置时**：
界面上给某个智能体勾选一个它不支持的传输类型时，当场置灰并说明。

### 5.3 后端黑名单（执行层，不是 UI 层）

探针证明 dsh / deepseek / pi **接受字段但完全忽略**（两轮都试过，
`acp_mcp_probe_results.md:94-98`）。而默认策略是 `all`——用户什么都没做，
配置就静默无效。**能力位过滤救不了这条**：dsh 广告的是 `http: true`。

所以 `ResolveMCPServers` 里必须有一条**静态黑名单**，与能力位过滤并列：

```go
// mcpUnsupportedBackends 是探针实测「接受 mcpServers 但完全忽略」的后端。
// 命中即返回空列表：配置了也不会生效，发过去只会让用户以为配好了。
var mcpUnsupportedBackends = map[string]bool{
    "dsh": true, "deepseek": true, "pi": true,
}
```

UI 置灰是提示层，不是执行层。验收标准见 §11.3。

### 5.4 CodeBuddy 兼容期（不能直接删）

workaround 存在的理由是 issue #270：CodeBuddy 在 `--acp` 下 `strictDynamic=true`，
`shouldLoadFromFilesystem()` 对**所有** filesystem scope 返回 false
（`acp_conn_lifecycle.go:697-703` 的注释）。也就是说现有用户配在
`~/.codebuddy/.mcp.json` 里的 server，**只能**靠这个 flag 生效。

直接删掉：flag 没了、协议字段里又没有他们的 server（`mcp.servers` 是空的）
→ 工具全部消失，无任何提示。

**规则**：

```
if mcp.servers 为空 {
    保留 --mcp-config 路径（读 ~/.codebuddy/.mcp.json）
} else {
    走协议字段，不再传 --mcp-config
}
```

两者互斥，避免同一 server 注册两次。协议字段这条已由探针验证（codebuddy
实测 OK，见 `acp_mcp_probe_results.md`）。

## 六、配置变更的生效机制

### 6.1 为什么不能读 `c.agent`（初稿的错误）

`ACPConn.agent` 是创建时的一次性快照，`acp_pool.go:903-905` 的注释明确写着
「Immutable fields — set once in newACPConn, never modified」，`newACPConn`
（`acp_pool.go:1423`）之后没有任何地方改写它。而 `model.UpdateAgent` 是
copy-on-write（`agent.go:262-283`：发布副本，旧指针永远看到旧值），
PATCH `/api/agents` 也只经它发布（`agent.go:1066`）。

后果：用户把策略从 `all` 改成 `none`，已存在的连接读到的仍是旧策略。
**而且版本比对也救不了**——`want` 读的是同一个陈旧指针，
`want == c.mcpVersion` 恒成立，重连永不触发。初稿的 §6.2 是自证循环。

**修法**：策略一律经 `model.GetAgent(c.agent.ID)` 现读（仓库契约原话：
「resolve through GetAgent at use time, do not cache」，`agent.go:255-260`）。
`c.agent` 只保留 `ID` / `Backend` 这类真正不变的身份字段。

`GetAgent` 取的是 `agentsMu`（`agent.go:236`），与 `c.mu` 无交叉，因此在
`ensureAliveWithSession` 持 `c.mu` 期间调用是安全的。**但绝不能为了拿 agent
去调 `mgr.GetConnByAgentID`**——那条路径取 `m.mu` 再取 `conn.mu`，
而仓库的锁序是 `m.mu → c.mu`（`acp_pool.go:856-878`），反向即自锁。

### 6.2 方案：期望列表哈希比对

```
每轮对话前（ensureAliveWithSession 入口，c.mu 已持有）：
    want := model.ConfigInstance.MCPPolicyVersion(c.agent.ID)   // 不含能力位
    if want != c.mcpVersion {
        走 spawnLocked 的既有重连路径（它会 kill 进程组并重新 Initialize）
        c.mcpVersion = want
    }
```

好处：
- 正在跑的对话不被打断（只在两轮之间重连）
- 全局改、按智能体改，都自动覆盖（因为策略是现读的）
- 四条注入路径自动统一

### 6.3 边界

- 重连会丢失 ACP session 的进程内状态（模式、思考档位等），但这些在
  `reapplyConfigAfterResume` 里已有重放机制。
- 重连**必须走 `spawnLocked` 的既有 kill 路径**（`setProcessGroup` +
  `reapProcess`），不要新写一套。否则 stdio MCP server 作为子进程会泄漏。
- 版本不含能力位（§4.5）：给 codex 加 sse server 会触发一次重连，
  这是可接受的代价。

### 6.4 配置读取必须走快照

`model.ConfigInstance` 注释写着「read-only afterwards」（`config.go:415-417`），
**这句话不成立**：`applyConfigPatch` 拿 `&model.ConfigInstance` 就地改写
（`settings.go:1683`），只在 handler 的 `configMutex` 下保护，而
`settings.go:39-51` 的注释自己承认「all readers in `internal/service` and
`internal/ai` access the globals WITHOUT this lock」。

在 `internal/ai` 的 prompt 热路径上裸读一个正被就地改写的 `[]MCPServer`
切片头，`go test -race` 会报。

**修法**：`model` 包提供快照访问器：

```go
// MCPServersSnapshot 返回当前配置的深拷贝，可安全跨 goroutine 使用。
func MCPServersSnapshot() []MCPServer
// MCPPolicyVersion 见 §4.5。
func MCPPolicyVersion(agentID string) string
```

内部用 `atomic.Pointer[MCPConfig]`（`applyConfigPatch` 在写完配置后发布新指针），
或把 `configMutex` 下移到 `model` 包。**§5 的四处注入点只允许经这两个函数读，
禁止直接碰 `ConfigInstance.MCP`。**

## 七、API 变更

### 7.1 `GET /api/config`

新增 `mcp` 段：

```json
{
  "mcp": {
    "servers": [
      {
        "name": "websearch",
        "transport": "stdio",
        "command": "node",
        "args": ["/opt/websearch/index.js"],
        "env": { "EXA_API_KEY": "tvly-dev-xxx" },
        "url": "",
        "headers": {}
      }
    ]
  }
}
```

**`env` / `headers` 明文返回**（用户已确认）。理由：

- 与仓库现状一致——STT / RAG / TTS 的 `api_key` 本来就是明文返回
  （`settings.go:899/910/1006`），MCP 若走另一套会让设置页出现两种行为。
- MCP 的 `env` 常含多个变量，只回 key 名会让用户无法确认"我填的到底是不是这个值"，
  改错一个字母排查成本很高。
- 该接口本来就需要登录态（`middleware.Auth`），与 `config.yaml` 的 `0600` 同一信任边界。

`PATCH` 因此也接受完整值（见 7.2），不需要"留空表示不修改"的隐式约定。

> 例外：`config.yaml` 里的 `forge.credentials` 仍然只回 `has_token`——那是既有实现，
> 本次不改动，避免牵连无关页面。

### 7.2 `PATCH /api/config`

新增可 patch 路径：`mcp.servers`（整体替换数组）。

写入规则：
- **整体替换**，与 `skills.repos` 一致。因为前端持有完整列表（含明文 env），
  每次保存都提交全量，不存在"只提交变化字段"的部分更新语义。
- 数组里没有的条目即被删除。

校验（在 `validatePatchFields` 同级）：
- `name` 非空、全局唯一、只允许 `[A-Za-z0-9_-]`
- `transport` ∈ {stdio, http, sse}
- stdio：`command` 必填，且 `exec.LookPath` 必须能找到（**保存即拒绝**，
  与 §5.2「提示前置到配置时」的原则一致）
- http/sse：`url` 必填且能被 `url.ParseRequestURI` 解析

> **`validatePatchFields` 的行为**（`settings.go:1111-1148`）：只有
> `map[string]any` 会递归；数组走 `else` 分支，整条路径作为叶子交给
> `PatchableConfigPaths` 判定。所以 `mcp.servers` 必须像 `skills.repos`
> 那样**在两张白名单里各登记一次**（热更新表 `settings.go:169` 与
> 可 patch 表 `settings.go:787`），否则数组会被 `field '%s' is not allowed'` 拒绝。
> 元素级校验在 `applyConfigPatch` 同级的手写块里做，与 `skills.repos`
> 的写法一致（`settings.go:1354-1368`）。

### 7.3 `GET /api/agents`

每个 agent 增加 `mcpServers` 字段（即策略对象）。

### 7.4 `PATCH /api/agents`

新增可 patch 字段 `mcp_servers`，走 `AgentPatch.MCPServers`。

**生效路径**：因为策略是 `GetAgent` 现读的（§6.1），PATCH 后下一轮对话自动生效，
**不需要** `CloseConnsByAgentID`。这也是不把 `mcp_servers` 加进
`agent.go:860/913/1126` 那几处「改了要断连」判断的原因——断连会打断在跑的对话。

### 7.5 新增 `POST /api/mcp/test`

测试某个 server 能否启动（仅 stdio）：

请求 `{"name":"websearch"}`，响应 `{"ok":true}` 或
`{"ok":false,"error":"..."}`。

**实现方式**：起进程 → 发 `initialize` → 收响应 → **`killProcessGroup` 杀掉整组**。

- 超时用 `acpInitializeTimeout`（60s），**不要用 5s**。MCP server 的主流配法就是
  `npx -y @modelcontextprotocol/server-xxx`，而仓库对 npx 冷启动的既有判断是
  「first launch may spend far longer downloading packages」（`acp_pool.go:220-228`）。
  5 秒会把「首次下载中」判成「启动失败」，而这个按钮存在的唯一价值就是确定性。
- 必须 `killProcessGroup`（`acp_conn_lifecycle.go:747`），否则会留下 npx 派生的
  孤儿进程——探针文档记录过只杀 adapter 导致子进程持有 writer 锁的后果。
- 该端点等价于任意命令执行，与 config 写入同一信任级，OpenAPI 里要写明。

## 八、前端

### 8.1 设置 → AI 与聊天 → MCP 服务

新增分类 `mcp`，与 `skills` 并列（`SettingsIndex.vue:76-77` 附近）。

卡片结构参照技能页：

```
┌─ MCP 服务 ────────────────────────────────┐
│ [说明文字]                                 │
│                                            │
│ websearch    stdio   ✓ 可用    [测试][删]  │
│ remote-tools http    ✓ 可用    [测试][删]  │
│                                            │
│ [+ 添加服务]                               │
└────────────────────────────────────────────┘
```

添加/编辑表单：名字、类型（三选一，切换时显示对应字段）、命令/参数/环境变量 或 URL/请求头。

**环境变量明文显示**（用户已确认）：输入框直接显示存的值，与 STT / RAG / TTS 的
`api_key` 行为一致。多行 `key=value` 编辑，保存时整体提交。

### 8.2 智能体详情页

在「偏好」卡片后新增一节「MCP 服务」：

```
┌─ MCP 服务 ────────────────────────────────┐
│ 启用方式  ○ 全部  ○ 全部禁用  ○ 只选这几个  │
│                                            │
│ ☑ websearch      stdio                     │
│ ☑ remote-tools   http                      │
│ ☐ browser        sse   ⚠ 该智能体不支持 SSE │
└────────────────────────────────────────────┘
```

**不支持的传输类型：整行标灰 + 勾选框禁用 + 旁边一行小字说明**
（用户已确认"标灰"）。

```
┌─ MCP 服务 ────────────────────────────────┐
│ 启用方式  ○ 全部  ○ 全部禁用  ○ 只选这几个  │
│                                            │
│ ☑ websearch      stdio                     │
│ ☑ remote-tools   http                      │
│ ☐ browser        sse   （已置灰）           │
│   ⚠ 该智能体不支持 SSE 型，配置不会生效      │
└────────────────────────────────────────────┘
```

置灰而非"允许勾选但运行时丢弃"：勾了却没用是更糟的误导。用户想让这个智能体
用上，只能换传输类型或换智能体。

### 8.3 提示文案（必须有的三条）

1. **仅 ACP 模式生效**：CLI 模式下配置无效，页面上要说明。
2. **hermes 首轮可能不可用**：异步挂载，第一轮对话看不到工具。
3. **dsh / deepseek / pi 不支持**：这三个后端接受字段但完全忽略。

前两条是全局提示，第三条按 `backend` 匹配后在该智能体页面上显示。

**第 2 条用 UI 提示，不做自动重试**（用户已确认）。理由：重试会给每一轮对话增加
延迟和不确定性，而 hermes 第二轮就能用——用户看到提示后自己重发一次即可，
行为可预期。

## 九、实施顺序

### P0：后端（约 10 个文件）

1. `internal/model/mcp.go`（新）：`MCPConfig` / `MCPServer` / `MCPServerPolicy` /
   `MCPCapabilities` / `ResolveMCPServers` / `MCPServersSnapshot` / `MCPPolicyVersion`
   （含后端黑名单，§5.3）
2. `internal/model/config.go`：`Config` 加 `MCP` 字段；`atomic.Pointer` 快照发布（§6.4）
3. `internal/model/agent.go`：`Agent` 加 `MCPServers`（零值语义见 §4.2）
4. **`internal/model/refresh.go`**：`loadAgentsFromDBRows`（`refresh.go:666`）+ `saveAgentToDB`（`refresh.go:448`）
   —— **这条不能漏**：生产用的 loader 是它，`service.LoadAgentsFromDB` 没有生产调用者
   （`refresh.go:660-665` 的注释已经把这条坑写死，#552 踩过同一个）
5. `internal/service/agent_store.go`：`CREATE TABLE agents` 加列 + 读写 + `AgentPatch`
6. `internal/service/database.go`：`ALTER TABLE agents ADD COLUMN`（套 `pragma_table_info` 守卫）
7. `internal/ai/agent_capability.go`：7 处触点（§5.1）
8. `internal/ai/acp_conn_lifecycle.go`：能力位采集 + 四处注入 + CodeBuddy 兼容分支 + 版本重连
9. `internal/ai/acp_pool.go`：`SyncLoadSession` 注入
10. `internal/ai/acp_conn_state.go`：`applyExtractedState` 的 `ForceUpdateIfNeeded` 调用补参数（§5.1 第 5 条）

**P0 完成即可用**：用户手动改 `config.yaml` 就能给所有智能体配上 MCP。

**前置（已完成）**：§11.0 的失败行为探针已跑完，结论见 §10.4。P0 额外包含
「保存时 LookPath 校验」与「带 MCP 时建会话超时放宽到 120s」两项；
运行时连接后校验降级为待定（原因见 §10.4）。

### P1a：API（已完成）

11. ✅ `internal/handler/settings.go`：`GET` 渲染 + 两张白名单 + 元素校验
    （含 `exec.LookPath` 与 http(s) scheme 检查）
12. ✅ `internal/handler/agent.go`：`mcp_servers` patch
13. ⬜ `internal/handler/mcp_test.go`（新）：`POST /api/mcp/test` —— **未做**，
    见 §10.4 的说明（运行时探测降级为待定）
14. ✅ `internal/api/openapi.yaml`：同步三处（`GET/PATCH config`、`PATCH agents`）

### P1b：前端（已完成）

15. ✅ `settingsFieldMap.ts`：`mcp` 分类
16. ✅ `SettingsIndex.vue`：`groupDefs` 加「MCP 服务」（Plug 图标）
17. ✅ `SettingsCategory.vue`：`PLACEHOLDER_ITEMS` + 挂载 `MCPSetting`
18. ✅ `MCPSetting.vue`（新）：服务列表 + 增删 + 类型切换表单
19. ✅ `AgentMCPSection.vue`（新）：三态策略 + 勾选清单 + 置灰规则
20. ✅ `SettingsAgentDetail.vue`：新增一节
21. ✅ `useSettingsConfig.ts`：`patchAgentField` 放宽到接受对象 + `mcp_servers` 映射
22. ✅ `i18n/locales/{zh,en}.ts`：18 个键

> 实现时发现并修掉的一个真 bug：`PATCH /api/agents` 把策略写进了 DB，但
> `model.UpdateAgent` 的 in-memory 回调里漏了 `MCPServers` 赋值，导致接口返回 200
> 而 `model.GetAgent` 仍读到旧策略（`TestServeAgentsPatch_MCPServersRoundTrip` 抓到）。
> 因为 `ResolveMCPServers` 走 `GetAgent` 现读，这个漏项会让「改成禁用」在下一次
> 重连前完全不生效。

### P2：暂不做

- CLI 传输侧（无参数可用）
- MCP server 进程复用（一会话一套，成本见下）
- ACP transport 型 MCP（`mcp/connect`，无后端实现）
- 与智能体自身配置文件的冲突合并策略（各后端行为不一致，见下）

## 十、已知代价与风险

### 10.1 进程数量（峰值，不是稳态）

每个 ACP 会话独立 spawn 一套 stdio MCP server。10 个会话 × 5 个服务 = 50 个子进程，
但这是**峰值**：`idleConnTimeout = 5min`、`minAliveConns = 3`（`acp_pool.go:204-209`），
且 stdio MCP server 是 agent 进程的子进程，随 `setProcessGroup` + `reapProcess`
一起回收（`acp_conn_lifecycle.go:747`、`acp_pool.go:2371-2394`）。
稳态约为 `minAliveConns × 服务数`。

缓解：
- 默认不预置任何 server，用户自己加
- 文档与 UI 提示优先用 http/sse 型（单例 daemon，不随会话复制）
- 不做代理层（v1 成本收益不划算）

**前提**：§6.3 的重连必须走既有 kill 路径，否则会真的泄漏。

### 10.2 与智能体自身配置文件的冲突

各后端行为不一致（源码确认）：

- **claude**：把我们传的和自己配置文件里的**合并**
- **codex**：同名时**跳过我们传的**（`DISABLE_MCP_CONFIG_FILTERING=true` 可关闭该行为）
- **codebuddy**：ACP 模式下不读文件系统 scope，只认我们传的

v1 不处理：不保证「ClawBench 里的配置一定覆盖智能体自己的」。文档说明即可。

### 10.3 会话指纹副作用

claude 的会话指纹包含 `mcpServers`，配置变化会触发它内部 session 重建。
这与 6.2 的版本重连方向一致，不冲突。

### 10.4 MCP server 启动失败的行为（已实测）

探针：`TestACP_MCPProbeFailure`。三种失败形态各测一遍，外加「一坏一好」混合：

- **F1 missing-command**：`command` 指向不存在的路径
- **F2 exits-immediately**：脚本存在但立刻 `exit 1`
- **F3 never-responds**：脚本活着、读掉 stdin 但永不输出（握手挂住）

#### 实测结果（6 个已验证后端）

| 后端 | F1 缺失命令 | F2 立即退出 | F3 永不响应 | 混合（一坏一好） |
|---|---|---|---|---|
| claude | 静默跳过 0.6s | 静默跳过 0.6s | 静默跳过 0.5s | **TOLERANT** |
| codex | 静默跳过 0.2s | 静默跳过 0.1s | 静默跳过 0.1s | **TOLERANT** |
| codebuddy | 静默跳过 0.2s | 静默跳过 0.2s | 静默跳过 0.2s | **TOLERANT** |
| qoder | 静默跳过 2.9s | 静默跳过 2.6s | 静默跳过 32.5s | **TOLERANT** |
| opencode | 静默跳过 0.9s | 静默跳过 0.8s | 静默跳过 30.8s | **TOLERANT** |
| commandcode | 静默跳过 0.1s | 静默跳过 0.1s | 静默跳过 80.1s | **TOLERANT** |
| hermes | 静默跳过 4.7s | 静默跳过 8.3s | 静默跳过 64.1s | **DEGRADED** |

**结论一：没有后端因为坏 server 让 `session/new` 失败。** 设计原本担心的
「一个写错的 command 锁死所有 ACP 智能体」**不成立** —— 全部是静默跳过。

**结论二：坏 server 不会毒化同一会话里的好 server。** 6/7 是 TOLERANT
（混合配置下好 server 的工具照常可用）。hermes 是 DEGRADED（session 建成功
但好 server 的工具没跑），与它已知的异步挂载不稳定一致，不是新问题。

**结论三（新发现的真问题）：F3 会拖慢建会话，且没有任何提示。**

- 其他五个后端：F3 让 `session/new` 多花 **30~80 秒**（正常是 0.1~3 秒）
- 用户看到的是「点发送后卡住一分钟，然后正常开始」——完全无法归因

这比「报错」更糟：报错至少能定位，静默慢只会被当成「AI 今天有点慢」。

#### 降级策略（定稿）

**采用 B（逐条剔除）**，因为实测证明 agent 本来就是这个行为，不需要我们额外实现。

**P0 只做两条**：

1. **保存时校验**（§7.2）：`exec.LookPath` 检查 `command`，不存在则拒绝保存。
   实测的三种失败形态里，F1（命令不存在）是最常见的错，这一条就能挡住。
2. **超时保护**：`session/new` 的 30 秒超时（`acp_conn_lifecycle.go:298`）对 F3 不够——
   qoder/opencode/commandcode 实测要 30~80 秒。带 MCP 时把建会话超时提到 120 秒，
   并在超过 10 秒时打一条「MCP 服务启动较慢」的日志。

**降级为待定：运行时连接后校验。**

初稿写的是「对比应注入的 server 名与 agent 实际报告的工具/服务名，缺失的记日志 +
发 `system_message`」。**这个方案目前做不了**，原因：

- `session/new` 的响应里**没有**工具列表，工具只能从后续事件推断；
- 各后端暴露 MCP 工具的命名完全不一致：claude 是
  `mcp__<server>__<tool>`、qoder 是 `<tool> (<server> MCP Server)`、
  hermes 是 `tool_call: Xxx`（探针里都实测过）；
- 唯一可靠的做法是「真调一次工具看有没有输出」，但那要消耗 token，
  不能在每个新会话里做。

所以 P0 不做运行时校验。代价是 F3 形态（脚本写错成死循环）仍然只能靠
「建会话慢」这条日志间接发现。等有人真的踩到，再按实际后端补探测逻辑。

**明确不做**：整体失败（策略 A）。实测没有后端这么做，强行实现需要在
`session/new` 前后做两轮探测，成本高且会改变现有行为。

### 10.4.1 群聊与定时任务的路径（实现时验证）

这两个路径不在 §5 的四个注入点里，需要确认它们是否共用同一条连接池路径：

- 群聊成员回合：`internal/service/group_member_request.go`
- 定时任务：`internal/service/scheduler.go`

若它们最终都走 `GetOrCreateConn` → `ensureAliveWithSession`，则 §5 的注入点
自然覆盖，不需要额外改动。若走了别的路径（例如直接 spawn 进程），必须补注入，
否则会出现「单聊有 MCP、群聊没有」。

**已核实（2026-10-19）**：两条路径都汇入同一个入口，不需要额外注入点。

```
群聊成员: defaultRunner (group_orchestrator.go:2118)
            → runTurn → runTurnStart (run_turn.go:271)
              → ai.NewBackendForAgentWithTransport (run_turn.go:296)
                → ACPBackend.ExecuteStream (acp_backend.go:63)
                  → mgr.GetOrCreateConn → ensureAliveWithSession
定时任务: scheduler.go:1168 → runTurnStart（同一条链）
成员压缩: group_member_compact.go:321 → be.ExecuteStream（同一条链）
```

因此 §5 的四个注入点自动覆盖全部路径。**但有一条例外**：
`__list_sessions__` 共享连接走 `GetOrCreateConnNoSession` → `EnsureAlive`，
不经过 `ensureAliveWithSession`，见 §10.6。

**验收判据**（仍需手动冒烟，因为这是路径问题不是代码问题）：
群聊里某个成员调一次探针 MCP 工具能拿到标记。

### 10.5 密钥写入磁盘

`config.yaml` 已按 `0600` 写入（`settings.go` 的 `writeConfigYAML`），
MCP 的 env 与现有 api_key 同级，不需要额外处理。

### 10.6 `__list_sessions__` 共享连接（有意不处理）

`GetOrCreateConnNoSession`（`acp_pool.go:491-516`）用
`connKey := "__list_sessions__:" + agent.ID` 建一条**没有 ACP session** 的连接，
只跑 `EnsureAlive`（→ `spawnLocked`），不跑 `ensureAliveWithSession`。

功能上没问题：MCP 是 per-session 的，没 session 就没有 MCP。但**实现时不能顺手
把版本检查加进 `EnsureAlive`**——那条路径没有 `acpSID`，一旦走
`ensureAliveWithSession` 就会给它建一个会话，破坏「ListSessions 不需要 session」的前提。

同理 `GetConnByAgentID`（`acp_pool.go:618-631`）遍历 map 时可能返回这条共享 conn，
所以「哪条 conn 承载策略」必须明确：**策略属于会话级 conn，不属于共享 conn**。

## 十一、测试计划

### 11.0 前置探针（已完成）

`TestACP_MCPProbeFailure` 已跑完，结果见 §10.4。结论：

- 坏 server **不会**让 `session/new` 失败（7/7 静默跳过）
- 坏 server **不会**毒化同一会话的好 server（6/7 TOLERANT，hermes DEGRADED）
- 但「永不响应」形态会让建会话慢 30~80 秒，且无任何提示 → 需 §10.4 的超时保护

**因此 P0 必须包含**：连接后校验（比对期望 vs 实际 server 名）+ 带 MCP 时的
建会话超时放宽到 120 秒。这两条原设计没有，是这次探针新加的。

### 11.1 单元测试

| 文件 | 覆盖 |
|---|---|
| `model/mcp_test.go` | `ResolveMCPServers`：三种策略模式；能力位过滤；**后端黑名单命中返回空**；版本号稳定性（含 map 排序：同样的 env 不同插入顺序必须得到同一哈希）；**空 mode 等价于 `all`** |
| `handler/settings_test.go` | PATCH 校验：非法 transport、stdio 缺 command、command 不存在、http 缺 url、重名；`mcp.servers` 在 `hotReloadFields` 里 |
| `service/agent_store_test.go` | 列迁移（二次启动不报 duplicate column）、读写往返、默认值 |
| **`model/refresh_test.go`** | **`loadAgentsFromDBRows` 往返**：写入策略 → 经生产 loader 读回 → 值不变。**用 `service.LoadAgentsFromDB` 测会全绿而生产丢字段**（#552 的坑） |
| `ai/agent_capability_test.go` | `ForceUpdate` 全量替换后 `McpHTTP`/`McpSSE` 仍在（模拟第二轮） |
| `ai/acp_conn_lifecycle_test.go` | `mcpServersForSession` 读的是 `GetAgent` 而非 `c.agent`（`UpdateAgent` 后能拿到新策略） |

### 11.2 集成测试（复用现有探针）

`internal/ai/acp_mcp_probe_integration_test.go` 已有完整框架，实现后加两个用例：

1. **通过 ClawBench 的配置路径（而非探针硬编码）注入，验证工具可用** ——
   唯一能证明「配置真的接到了线上路径」的测试。
2. **重启后仍生效**：第一轮拿到工具 → 杀掉进程 → 新进程 resume → 工具仍在。
   这钉住 §5.1 的能力位 carry-through（C3 的回归面）。

### 11.3 验收标准

**能自动化的（CI）**

- [ ] `mcpServersForSession` 的 payload 断言：全局列表含一个 sse server、
  codex 策略为 `all` → 载荷里**不含**该项（能力位过滤）
- [ ] 同上，dsh 策略为 `all` → 载荷为空（后端黑名单）
- [ ] 智能体策略从 `all` 改成 `none` 后，**已存在的连接**下一轮载荷为空
  （钉住 §6.1 的现读，不是「新会话」）
- [ ] `go test -race` 在并发 PATCH config + 读取载荷时不报（§6.4）
- [ ] 迁移：旧 DB 升级后列存在，二次启动不报错

**需要手动冒烟（6 个真实 CLI + 登录态 + 真实模型）**

- [ ] 全局配一个 stdio server，6 个已验证智能体都能调用到工具
- [ ] CodeBuddy：`mcp.servers` 为空 + `~/.codebuddy/.mcp.json` 有内容 → 工具仍可用
  （§5.4 兼容期）
- [ ] CodeBuddy：`mcp.servers` 非空 → 走协议字段，工具可用

**失败路径（依据 §10.4 的实测结论）**

- [ ] 配一个 `command` 不存在的 server，保存时被 `exec.LookPath` 拒绝（F1）
- [ ] 若绕过校验（直接改 config.yaml）→ 会话仍能建立，且界面提示「MCP 服务 X 未连上」
- [ ] 配一个「永不响应」的 server → 建会话最多等 120s，超过 10s 有「MCP 启动较慢」日志
- [ ] 一坏一好两个 server → 好的那个工具仍可用（TOLERANT）
- [ ] 群聊：某成员调用探针 MCP 工具能拿到标记（验证 §10.4.1 的路径共用）

**可观测性判据（替代「不中断」这种不可证伪的说法）**

- [ ] 回合进行中 PATCH config → 该回合流式消息正常收尾、message id 不变
- [ ] 下一回合观察到新的 agent pid（或 `spawnLocked` 日志）且载荷含新 server

**安全**

- [ ] `GET /api/config` 返回 env / header 的明文值，与 STT / RAG 的 `api_key` 行为一致
- [ ] `POST /api/mcp/test` 的 OpenAPI 描述写明「等价于任意命令执行」

## 十二、已确认的决策

| 问题 | 决策 | 影响章节 |
|---|---|---|
| 密钥回显 | **明文返回**，与 STT / RAG / TTS 的 `api_key` 一致 | 7.1 / 7.2 / 8.1 |
| hermes 首轮不可用 | **仅 UI 提示**，不做自动重试 | 8.3 |
| dsh / deepseek / pi 不支持 | **执行层黑名单 + UI 标灰**（只标灰不够，默认 `all` 会绕过） | 5.3 / 8.2 / 8.3 |

## 十三、评审修订记录（2026-10-19）

独立 code-reviewer 子智能体评审后修订。Critical 项全部核实成立：

| # | 问题 | 修法 | 章节 |
|---|---|---|---|
| C1 | `c.agent` 是一次性快照，按智能体改策略永不生效；版本比对读同一陈旧指针 = 自证循环 | 策略一律经 `model.GetAgent` 现读；`c.agent` 只留身份字段 | 4.3 / 6.1 |
| C2 | P0 只改 `service/agent_store.go`，但生产 loader 是 `model/refresh.go` 的 `loadAgentsFromDBRows`（#552 已踩过） | P0 文件清单加 `refresh.go`，测试点名经生产 loader 往返 | 9 / 11.1 |
| C3 | 能力位只加字段不够，`ForceUpdate` 是全量替换，会在第一轮后清空 | 列全 7 处触点，`ForceUpdateIfNeeded` 补参数并 carry-through | 5.1 |
| C4 | 版本在 spawn 前算（能力位为空）与实际发送内容不一致，每进程必多一次重连 | 版本改为「期望列表」哈希，**不含能力位** | 4.5 / 6.3 |
| C5 | `ConfigInstance` 注释说只读但实际就地改写，`internal/ai` 裸读会 race | 加 `atomic.Pointer` 快照访问器，禁止直接读 `ConfigInstance.MCP` | 6.4 |
| C6 | 默认 `all` 完全绕过 UI 标灰，dsh/deepseek/pi 静默无效 | 执行层静态黑名单，与能力位过滤并列 | 5.3 |
| C7 | 直接删 `--mcp-config` 会让现有 CodeBuddy 用户静默失去工具 | 保留兼容期：`mcp.servers` 为空时仍走旧路径 | 5.4 |
| C8 | MCP server 启动失败行为未定义 | **已实测**：不会失败、不会毒化，但会静默慢 30~80s。改为「连接后校验 + 超时放宽」，不做整体失败 | 10.4 / 11.0 |

Important 项也已吸收：`hotReloadFields` 加 `mcp.servers`（I1）、
`SettingsCategory.vue` 接线（I2）、`patchAgentField` 类型与映射（I3）、
test 端点 60s 超时 + `killProcessGroup`（I4）、OpenAPI 四处（I5）、
迁移守卫（I6）、进程数是峰值（I7）、`__list_sessions__` 显式排除（I8）、
零值语义（I9）、锁序（I10）。

行号修正：`PatchableConfigPaths` 实为 `settings.go:692`（初稿写 672）；
PATCH agents handler 实为 `agent.go:732`（初稿写 ~685）。
