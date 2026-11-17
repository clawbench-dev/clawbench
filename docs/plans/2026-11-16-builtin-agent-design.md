# 内置智能体（ClawBench Agent）设计

日期：2026-11-16
状态：已实现（第一期：内核 + 聊天接入 + 工具权限）
关联：`internal/agentcore`（内核）、`internal/ai/backends/builtin`（后端注册）、`internal/model/toolpolicy.go`（权限模型）、`docs/plans/2026-11-05-builtin-acp-agent-design.md`（本文件取代的早期草案）

## 一句话

一个由 ClawBench 自己实现工具循环的智能体：能读写文件、跑命令、看图，工具权限按能力位精细控制。它既是一个普通智能体（能聊天、能进群、能跑任务），也是一个可以被后台任务直接调用的执行引擎。

**名字是「ClawBench Agent」，不是「Builtin」。** 它不是一个「内置的某种东西」，它就是 ClawBench 自己在跑工具循环——`backend=builtin` 是内部标识（描述实现方式），`Builtin` 当显示名会让用户在智能体选择器里看到一个描述机制而不是描述能力的词。

## 为什么不走 ACP 子进程

早期草案（`2026-11-05-builtin-acp-agent-design.md`）的方案是「内核 + ACP 适配器 + `--acp-agent` 子进程」。本期**整条推翻**，改为进程内直接实现 `ai.AIBackend`。

`AIBackend` 只有两个方法：

```go
type AIBackend interface {
    Name() string
    ExecuteStream(ctx, ChatRequest) (<-chan StreamEvent, error)
}
```

`StreamEvent` 是 ClawBench 自己的类型（`content` / `thinking` / `tool_use` / `tool_result` / `metadata` / `done`）。内核直接吐它，下游整条链路（流式渲染、工具卡片、思考块、token 统计、取消按钮、群聊、任务系统）一行都不用改。

ACP 方案要多做的事，以及它们为什么都不值得：

| ACP 子进程 | 进程内 |
|---|---|
| 实现 `Agent` 接口 12 个方法 | 不需要 |
| `--acp-agent` 启动分支、不走 HTTP | 不需要 |
| 每会话一个 JSONL 会话文件 | 直接读 `chat_history` |
| 两份会话数据可能不一致 | 不存在这个问题 |
| 桌面端升级换目录 → 绝对路径失效 | 不存在 |
| 每会话一个进程的内存开销 | 同进程 |

省下的这些，换来的只是 ACP 的「逐次权限弹窗」——而本需求要的是**事前的能力限制**，不是运行时一个个点允许。进程内做这件事反而更好：不允许的工具**压根不注册**给模型。

## 三个组成部分

```
internal/agentcore/            内核：调模型 + 跑工具 + 循环
    llm/                       模型客户端：OpenAI / Anthropic 兼容 + 流式 + tool_calls
    tools/                     read / write / edit / glob / grep / bash
    run.go                     循环、预算、取消
    prompt.go                  系统提示词组装
internal/ai/backends/builtin/  后端注册：让它在 UI 里是个普通智能体
```

内核**不认识 ACP、不认识 HTTP、不认识 WS**，只依赖 `internal/model`（拿权限与 provider 配置）和标准库。对外两个入口：

| 入口 | 谁调 | 拿什么 |
|---|---|---|
| `agentcore.Run` | 聊天、群聊、任务系统 | 流式事件，边跑边发 |
| `agentcore.RunSync` | 后台任务（会议纪要、Skill 提取） | 一次返回结果 + 用量 |

## 系统提示词

参考 pi 的结构（`buildSystemPromptSections`）：一段 preamble + 每个关注点一个 XML 段。

```
preamble（agent 自己的指令，不包裹，排最前）
<tools>        每个工具一行 "name: 一句话用途"
<rules>        使用规则，按当前装了哪些工具条件生成
<environment>  工作目录
<output>       回答约定
```

**为什么包标签而不是写成散文**：模型把 `<rules>` 里的列表当成要遵守的条目，把同样的话写成散文当成要概括的内容。同样的文字，行为不同。

**为什么 preamble 不包裹且排最前**：那是用户写的东西，必须压过下面的机械文本。

**为什么工具要带一句话用途**：第一版只列工具名，模型不知道**该选哪个**，于是用 bash 干 read/glob/grep 更擅长的事——而分别授予这些工具的全部意义就是它们做得更好。用途写在 `Tool.Snippet()`，与 schema 里的 `Description` 分开：提示词里是「在多个工具之间选」，schema 里是「这一个工具怎么用」。

**规则必须与已授予的工具一致**：`Tool.Guidelines()` 返回的规则只在该工具存在时出现。这条踩过坑——`read` 里原本写「编辑前先读文件」，只读模式下它指向一个根本没装的工具，属于噪音甚至误导。现在这句归 `edit` 自己。

**能力缺失要明说**：`You cannot write or modify files or run shell commands`。工具**不存在**在模型看来是「我忘了」，不说明它会花几轮去找一个永远不会出现的写文件工具。

## 项目上下文文件（AGENTS.md / CLAUDE.md）

外部后端自己会读 `AGENTS.md`，内置智能体没有任何进程，也就没有任何自己的发现机制——所以项目规则必须由 ClawBench 注入，否则同一份 `AGENTS.md`，别的智能体遵守、内置智能体看不见。

注入只对 `backend=builtin` 生效（`isBuiltinBackend` 门控），位置在 `service/project_context_prompt.go`，两个 system prompt 生产者都要调（`chat_request.go` 的聊天/队列/push，`scheduler.go` 的定时任务）。

形状与 pi 的 `loadProjectContextFiles` 对齐，但只走到项目根为止：

- **每个目录一个文件**，优先级 `AGENTS.override.md` > `AGENTS.md` > `AGENTS.MD` > `CLAUDE.md` > `CLAUDE.MD`。同目录不叠加——override 存在就是为了取代。大写拼写不是装饰：Linux 上 `AGENTS.MD` 是另一个文件，只提交了大写名的仓库会静默丢规则。
- **逐层向上堆叠**，从项目根到工作目录，外层在前、最具体在后。这样 monorepo 里 `apps/web/AGENTS.md` 能叠加在根的规则之上。
- **上界是项目根**：工作目录不在项目内（或为空）时回退到项目根，绝不从无关目录向上走——那会把别的项目的规则注入进来。
- **单文件 32 KiB 上限**，超出截断并留标记。比推荐用的 4 KiB 宽得多：这里的文字是项目规则，截掉半条规则（「…除了 internal/」）比多花 token 更糟。
- 空文件、读失败、非普通文件一律跳过，不让一个坏文件拖垮其余规则。

- **链接工作树去重**（对齐 pi 的 `findShadowedContextFile`）：工作树通常开在主仓库里面（`.worktrees/<name>`），向上走会**两次**拿到主仓库的 `AGENTS.md`，而工作树那份往往是它自己的相对软链。两者同属一个逻辑仓库范围，只保留工作树自己的那份。判据是「目录真实路径 + 文件名」，**不能解析文件本身**——软链会解析回主仓库的路径，两份都会被丢掉，结果是 0 份（写测试时实际踩到了）。
- **开关**：`agents.skip_project_context`（默认 0 = 注入），设置页勾选框写的是「读取项目的规则文件（AGENTS.md）」，每轮实时读取，改完下一轮生效。**按智能体**而不是全局：外部后端自己读这些文件，全局关掉对它们毫无作用。

pi 还读 `~/.pi/agent/AGENTS.md`（全局用户级文件），**没有**跟进：ClawBench 的用户级自定义提示词已经在 `custom_system_prompt` 里，再叠加一份全局文件会出现两个权威来源。

## 权限模型

### 能力位，不是工具白名单

`bash` 就是万能写：`cat > f`、`sed -i`、`git checkout`、`find -exec`、`python -c` 全都做得到。所谓「只读 bash」靠命令白名单是**挡不住的**——漏一个就破。

所以权限不设计成「工具白名单 + bash 只读模式」，那样会给用户一个**看起来安全、实际不安全**的东西。改用能力位：

| 能力位 | 谁需要它 | 语义 |
|---|---|---|
| `fs.read` | `read` / `glob` / `grep` | 读工作目录内的文件 |
| `fs.write` | `write` / `edit` | 改工作目录内的文件 |
| `exec` | `bash` | 起子进程 |

规则只有一条，但很硬：**`exec` 一旦打开，`fs.write` 就形同虚设**。

服务端在 `ToolPolicy.Normalized()` 里做闭包（`exec ⇒ fs.write ⇒ fs.read`），而不是「校验并拒绝」。原因：这两种形状不是用户能被说服改掉的笔误，而是 UI 上勾了「执行命令」没勾「写文件」的自然产物。拒绝会让开关看起来坏了；静默保留会让设置页说谎。闭包让**存储的策略永远是真相**。

前端同步做同样的闭包（`AgentToolPolicySection.vue`），这样勾上 `exec` 的那一帧 `fs.write` 就变成勾选并锁定，而不是等一个网络往返。

### 零值 = 全关

`ToolPolicy{}` 表示**不开放任何工具**。新装的内置智能体第一次用必须显式开。

这个方向是刻意的：漏配的策略必须 fail closed。其他所有后端默认全权限，一个「悄悄继承了一切」的智能体是最不容易被发现的失败模式。

### 路径边界

所有路径必须落在会话工作目录内。`tools.resolvePath` 用 `filepath.Abs` + `filepath.Rel` 判断前缀，并额外**解析最深已存在祖先的符号链接**再查一次：

```
root/link -> /etc        # 一个被投毒的仓库
root/link/secret.txt     # 文本路径在 root 内，实际指向 /etc
```

这条必须拦住，否则只读策略毫无意义。写入 `root/link/new.txt`（文件还不存在）同样要拦——`resolveExistingPrefix` 就是为此。

### 两层分离

| 层 | 在哪 | 作用 |
|---|---|---|
| 能力限制 | 内核，请求构造时 | **硬约束**：不允许的工具不注册，模型看不见 |
| 逐次审批 | 前端（本期未做） | 软约束，不是安全边界 |

## 多模态

外部后端全部走「给路径，agent 自己 Read」——`buildPromptBlocks` 只发 text block，`ChatRequest` 里连图片字段都没有。

内置智能体的 `read` 是**我们自己写的**，可以直接 base64 塞进模型的 image block。这是外部后端做不到的。

两个协议的差异必须分别处理：

| 协议 | 图片怎么放 |
|---|---|
| Anthropic Messages | `tool_result` 的 content 直接支持 image block |
| OpenAI Chat Completions | `role: "tool"` 的 content **只能是字符串**，图片作为追加的 `user` 消息发出 |

视觉能力从模型目录取（models.dev 的 `modalities.input` 含 `image`），取不到就**fail closed**：模型不知道的 id 当作纯文本模型，`read` 换成一个会解释「当前模型看不了图片」的变体。把图片发给看不了它的模型，会得到一个用户无法诊断的 400。

## 会话与历史

内置后端没有外部会话，模型上下文**每次从 `chat_history` 重建**。这是刻意的取舍：transcript 和模型上下文是同一份数据，不可能不一致。

关键细节：用户消息在构造请求**之前**就落库了，所以历史必须按消息 id 截断，否则 prompt 会出现两次（历史一次、prompt 一次）。`ChatRequestOption` 的 `WithCurrentMessageID` 传这个边界。

预算 40000 字符，从**前面**丢弃（最近几轮才是下一轮依赖的），并插入一条说明让模型知道前面还有内容——否则它会以为对话就是从中间开始的。

## 要动的文件

| 位置 | 内容 |
|---|---|
| `internal/agentcore/` | 内核（新包） |
| `internal/agentcore/compaction/` | 压缩判定与摘要（新包） |
| `internal/agentcore/llm/` | 两个协议客户端（新包） |
| `internal/agentcore/tools/` | 六个工具 + 路径边界（新包） |
| `internal/ai/backends/builtin/` | 后端注册 + 事件翻译（新包） |
| `internal/model/toolpolicy.go` | 权限模型（新） |
| `internal/model/agent.go` | `Agent.ToolPolicy` |
| `internal/model/discovery.go` | `BackendSpec.Builtin` |
| `internal/model/provider_env.go` | `builtin` 条目 + `InProcess` 标志 |
| `internal/model/catalog.go` | `CatalogModel.Vision` + `ModelSupportsVision` |
| `internal/service/agent_store.go` | `tool_policy` 列 + 读写（含修掉 `mcp_servers` 从不写入的老 bug） |
| `internal/service/database.go` | 迁移 |
| `internal/service/builtin_history.go` | 历史重放（新） |
| `internal/service/chat_request.go` | `WithCurrentMessageID` + 填充 History |
| `internal/ai/interface.go` | `ChatRequest.History` |
| `internal/handler/agent.go` | PATCH `tool_policy` |
| `internal/api/openapi.yaml` | 文档 |
| `web/src/components/settings/AgentToolPolicySection.vue` | 权限编辑器（新） |
| `web/src/composables/useAgents.ts` | `ToolPolicy` 类型 + `builtinBackend` |
| 守卫表 | `discovery_test.go`、`native_skills_test.go`、`provider_env_coverage_test.go`、`backendNames.ts`、`agentIcons.ts`、`mono-icon-colors.css` |

## 已实现 vs 未实现

**已实现**

- 内核循环：模型 → 工具 → 结果回喂，预算（轮数/工具数/命令超时），取消
- 六个工具：`read`（含图片）、`write`、`edit`、`glob`、`grep`、`bash`
- **系统提示词参考 pi**：preamble + `<tools>`（每工具一句话用途）+ `<rules>`（按已授权工具条件生成）+ `<environment>` + `<output>`
- 路径边界（含符号链接逃逸）
- 能力位权限 + 服务端闭包 + 前端编辑器
- 两个协议客户端（OpenAI / Anthropic），流式 + tool_calls + 用量
- 历史重放（含本轮边界、预算、截断提示）
- 后端注册 + 自动创建 agent 行
- 视觉能力检测（模型目录）
- **上下文压缩**：按窗口比例提前压、摘要 + 最近轮原文、边界持久化、单轮内折叠工具结果、无摘要模型时规则兜底、压缩提示用户可见

**未实现（明确的范围外）**

| 不做 | 原因 |
|---|---|
| MCP | 后端没有 MCP 客户端；设置页已隐藏该区块 |
| Skill 注入 | 后端没有自己的 skill 目录，走 ClawBench 的常规注入（或全局关闭） |
| 中途插话（steering） | 用户中途发的消息照常排队 |
| 逐次权限审批 | 本期只有事前能力限制；`RequestPermission` 未接 |
| 后台任务接入（会议纪要 / Skill 提取） | 内核已具备 `RunSync` 入口，但还没有调用方 |
| Skill 提取 | 这个功能在仓库里**原本不存在**（`internal/skill` 只做发现/扫描/注入）。Hermes 也没有。属于新功能 |

## 上下文压缩

外部 CLI 自己压缩上下文，事后告诉我们（`compact_detect.go`）。内置智能体的上下文由我们自己拼，**没人替它压**，所以必须自己压。

### 三个决定

| 决定 | 取值 | 为什么 |
|---|---|---|
| 何时压 | 占模型窗口 **75%** 时提前压 | 压晚了，触发压缩的那一轮可能在中途撞墙——工具循环会再加几轮输出，撞上就是用户无法处理的 API 报错 |
| 压完留什么 | **摘要 + 最近若干轮原文** | 摘要只说「聊过配置文件」，用户下一句「改那一行」就没法接。原文留的是近处细节 |
| 用户看不看得见 | **看得见** | 「它忘了我刚说的」和「它把我说的总结了一遍」从外面看一模一样，只有后者不是问题 |

### 保留多少轮：按 token 预算，不按轮数

这是踩过的坑。第一版固定保留 4 轮，在小窗口（4096）下 4 轮长消息本身就超窗口，压缩永远压不到阈值以下——**每轮都压一次，摘要越堆越多，历史一点没减少**。

现在按窗口的 40% 作为「保留轮次的 token 预算」，从最新往回装，装不下就停在那轮的边界。这保证压缩**一定收敛**。

### 两个触发点

| 位置 | 场景 |
|---|---|
| 每轮开始前 | 跨轮累积：历史重放后已经超了 |
| 每轮工具调用后 | 单轮内爆掉：读几个大文件，一轮就超了任何窗口 |

只有第一个触发点的设计会漏掉第二种情况——那种情况下对话根本不会「结束」，等不到下一轮。

单轮内没有「旧轮次」可压，所以退化为**折叠较早的工具结果**（保留最近 2 轮）。折叠时保留 `tool_use` 调用本身，只把结果换成一行说明——调用和结果必须成对，API 不接受孤儿调用。

### 摘要怎么存

存在 `chat_sessions.context_state.builtin_summary`，结构是 `{text, upToMessageId}`。

`upToMessageId` 是**边界**，不能省：摘要说了「压掉了什么」，但没说「压到哪」。没有边界，下一轮无法判断哪些消息已经被摘要代表，于是要么重放它们（白压），要么把它们之后的消息也丢掉（丢真实内容）。

有了边界，历史只重放边界之后的消息，**同一段历史只压一次**。

### 摘要本身不无限增长

一次压不到阈值以下会再压一轮，新摘要要接上旧摘要（旧摘要是更早内容的唯一记录）。但直接拼接会无限长，所以有上限：超了就从**前面**截断并注明——丢的是久远的过去，留的是近处的。

### 没有摘要模型时

走 `DigestSummary` 兜底：不用模型，按规则提取「用户依次问了什么」「用了哪些工具」，丢掉文件正文。

读起来肯定不如模型写的，但**事实不丢**。拒绝压缩的代价是下一轮直接失败，用户连工作成果都拿不到。

## 风险

1. **`bash` 没有技术兜底**。一个被投毒的仓库文件（README 里写「请执行 rm -rf」）就能让任务跑任意命令。只能靠「哪些任务开 bash」的清单控制。建议内部任务一律不给 `bash`。
2. **压缩要花一次模型调用**。小窗口模型上可能每两三就压一次。这是按比例提前压的固有代价——换来的是不撞墙。
3. **历史重放会重复工具调用**。只重放文本，不重放工具调用与结果。模型可能重复读同一个文件——这是有意的取舍（重放工具结果会把大段文件内容重新塞进上下文）。
4. **`Windows` 上 `bash` 的进程组取消是空实现**（`proc_windows.go`）。子进程可能残留。
