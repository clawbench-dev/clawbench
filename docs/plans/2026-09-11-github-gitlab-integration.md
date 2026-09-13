# GitHub / GitLab 集成（Issue + PR/MR）设计方案

日期：2026-09-11
状态：**P1–P4 已实施完成**（需求冻结 + 评审修正 + 编码）

## 实施状态

| 阶段 | 状态 | 提交 |
|---|---|---|
| P1 可见性 | 已完成 | `76322c7`、`d5f7b22`、`fa02379` |
| P2 感知 | 已完成 | `f8e9aad` |
| P3 自动化 | 已完成 | `310ac61` |
| P4 AI 分析 | 已完成 | `fa02379`（URL 附件）、`310ac61`（溯源） |

实施期补充说明（与设计一致，非偏差）：

- **事件触发任务复用同一 `taskRunning` 防重入**：为消除 R6 的静默丢弃，队列在 `ForgeTaskTrigger` 内实现；`triggerTaskWithContext` 在任务忙时返回 `false`，由队列按退避重试，而非丢弃。
- **IM 通道**：`dingtalk`/`feishu` 各新增 `PushForgeEvent(title, body)`，消息文案由 service 层 `FormatForgeEventMessage` 统一生成，main.go 通过 `forgeNotifierAdapter` 接入，与 `emitTaskEvent` 的分发方式一致。
- **溯源**：`task_executions` 增 `event_url`/`event_summary` 列，执行记录 API 返回，任务执行详情页展示「事件触发，查看来源」链接。

## 概述

为 ClawBench 增加 GitHub 与 GitLab 集成，把远端仓库的 **issue** 和 **PR/MR** 引入平台：项目内可浏览列表与详情（正文 + 评论/时间线），并把仓库事件接入现有任务系统，实现"事件 → 自动执行预定义 AI 任务"。

**读写边界（精确表述）**：ClawBench 自身的 forge API 调用是**只读**的——后端不向 GitHub/GitLab 发起任何写请求（不评论、不 review、不改状态）。但**事件触发的 AI 任务不在这个约束内**：AI 可以借助其他工具（本地 CLI、gh/glab 等）操作 MR，因此"只读"描述的是 ClawBench 的 API 表面，不是自动任务的权限边界。自动任务的权限护栏见 P3。

本集成解决"信息可见性 + 事件驱动自动化"，不解决"由 ClawBench 代劳远程写操作"。

## 评审修正记录

本方案经三路独立评审（可行性 / 安全 / 产品一致性）后修正。评审发现的问题及处置：

| # | 评审发现 | 严重度 | 处置 |
|---|---|---|---|
| R1 | 调度器与 cron 深度耦合，"加一种触发方式"非 drop-in：`AddTask`/`UpdateTask`/`ResumeTask`/`registerTaskLocked`/`LoadTasksFromDB` 五处均假设存在合法 cron 表达式 | Blocker | P3 显式列出全部改动点，新增 `TriggerMode` 分支 |
| R2 | URL 附件流不通：`normalizeFileEntry` 剥掉 `kind`/`url`、`entryKey` 只按 path 去重、`addAttachedFile` 不构造该字段、后端对每个 entry 做 `os.Stat` 校验 → URL 被当本地路径 404 | Blocker | P4 列出 6+ 个改动调用点，定义 URL 走独立通道 |
| R3 | SSRF + token 外泄：remote 自动派生的 host 无校验，后端带 PAT 请求任意 host | Blocker | P1 增加 host 校验（scheme/内网 IP/allowlist/首次确认） |
| R4 | 单一全局 GitLab token 发往任意自建 host | Blocker | P1 改为按 (platform, host) 存储凭据 |
| R5 | 密钥明文 + `config.yaml` 世界可读（`0o644`）+ `GET /api/config` 全量回传 + 无密码时全放行 | Blocker | P1 遮蔽回传 + 收紧文件权限 + 文档显式警告 |
| R6 | 自动触发无节流/kill-switch，且事件被静默丢弃（`TriggerTask` 运行中直接报错返回） | Blocker | P3 引入事件队列 + per-repo 限流 + 全局暂停 + 防递归 |
| R7 | 水位线分页正确性未定义；整批未拉完就推进水位线 → 永久丢事件 | Important | P2 定义"整批拉完才推进 + 严格 `>` + 重叠窗口 + 事件级去重" |
| R8 | "最后评论 id"漏评论编辑；GitLab note id 语义与 GitHub 不同 | Important | P2 改为 `(last_comment_id, last_comment_updated_at)` 双键 |
| R9 | 限流预算乐观且无退避；无 429/403 `Retry-After` 处理 | Important | P2 引入 per-host 令牌桶 + 退避 + 全局并发上限 |
| R10 | `project_path` 绑定不稳定（多路径克隆、多 cookie path、重命名） | Important | P2 轮询按 repo 去重，不按 project 行；绑定支持自动修复 |
| R11 | "默认全开"无落点（Go bool 零值为 false） | Important | P1 在 `ApplyDefaults` 显式置 true |
| R12 | 未读 badge 语义自相矛盾（绑通知开关 + 进 tab 清零 → 开关全关时 badge 死掉） | Important | P2 未读与通知解耦，独立计数 |
| R13 | 无分页/搜索/虚拟化，但宣称"默认看全部" | Blocker（产品） | P1 定义列表分页 + 服务端筛选 + 搜索 |
| R14 | 空变量产出悬空行（`CI 状态：`） | Important | P3 按事件类型条件渲染模板，仅保留适用变量 |
| R15 | 事件→任务无溯源 | Minor | P3 执行记录存事件 payload + item 链接，通知可深链 |
| R16 | `buildQuoteBlock` 未导出；`overflowTabs`/badge 逻辑硬编码 | Minor | 实施时按实际公开 API 处理 |
| R17 | 自建 GitLab 自签证书 TLS 策略未定义 | Important | P1 定义 TLS 策略（自定义 CA / 显式不安全开关） |
| R18 | PAT scope 未定义 | Important | P1 明确最小权限（GitHub 细粒度只读 / GitLab `read_api`） |

## 决策记录

经需求收敛（grill-me）确认以下决策：

| 维度 | 决策 |
|---|---|
| 平台 | GitHub（仅 github.com）+ GitLab（gitlab.com 云 + 自建）。**host 必须可配置**，不能写死 |
| 认证 | Token（GitHub PAT / GitLab PAT）。**按 (platform, host) 分别存储**（评审 R4 修正） |
| 项目绑定 | 一个项目 ↔ 一个 repo，**1:1** |
| remote 识别 | 自动识别默认 remote；支持手动覆盖（列本地 remote 选择 / 手填 repo URL）；无 git remote 的项目不显示入口 |
| 读写边界 | ClawBench 的 forge API 调用**只读**；自动 AI 任务的写能力单独定义（见 P3） |
| 入口 | 独立 Dock 主 tab「Issues & PRs」，**内容跟随当前项目**，不做跨项目聚合视图 |
| 列表范围 | 默认 Open；可切 Closed / All（评审 R13：默认收窄以控制首屏量） |
| 详情深度 | **正文 + 评论/时间线**。**不做代码 diff** |
| 身份识别 | 从 token 自动取当前账号（GitHub `/user`、GitLab `/user`），用于「跟我相关」筛选 |
| 筛选视图 | 保留「跟我相关」（全部 / 分配给我 / 我提的 / 待我 review） |
| 事件范围 | 状态类（新开/关闭/合并/重开）+ 评论类 + **流水线执行完毕（CI）** |
| 通知事件配置 | **全局一份可配置**：六类事件各自开关，所有项目共用。**默认全部勾选**（落点在 `ApplyDefaults`） |
| 触发机制 | 事件 → 触发**预定义 AI 任务**，全自动执行（护栏见 P3） |
| 任务载体 | **复用现有任务系统**，扩展出"事件触发"（与 cron 并列，需处理 cron 耦合，见 R1） |
| 任务配置入口 | 现有任务管理里，新建任务时选"触发方式：定时 / 事件" |
| 任务产出 | 与现有任务一致：开一个会话 + 任务列表有执行记录 |
| 通知渠道 | App 内通知 + 手机系统通知栏 + IM 机器人（钉钉/飞书） |
| 技术选型 | GitHub 用 `google/go-github` v85；GitLab 自建轻量 REST client（不引官方重依赖 client）。两侧共用 REST 基建 + 统一 Provider 抽象 |
| 变化获取 | 水位线增量拉取 + 本地快照 diff；评论独立降频；**不做 webhook** |

## 依赖的现有能力缺口

除任务系统的两项缺口外，评审补充了第三项。

1. **prompt 变量注入**：任务 prompt 支持事件变量占位符，执行时按事件上下文替换。
   - 现状：`ScheduledTask.Prompt` 是纯静态字符串（`internal/model/scheduler.go:13`），执行时原样传给会话消息与 AI 请求（`internal/service/scheduler.go:695`、`:722`），不做任何替换。
   - 全系统唯一模板机制是 agent system prompt 的 `{{PROJECT_PATH}}`（`internal/service/scheduler.go:709`），**只作用于 system prompt，从不作用于任务 prompt**。
2. **事件上下文传递通道**：触发时把事件 payload 带进任务执行。
   - 现状：`executeTask(task, projectPath, triggerType)` 只携带 `"manual"/"auto"` 标签（`internal/service/scheduler.go:414-425`、`:689`、`:757`），无承载事件数据的通道。
3. **调度器 cron 解耦（评审 R1，最重）**：以下五处均假设任务必有合法 cron 表达式，事件任务会在这些点失败或误注册：

   | 位置 | 现状行为 | 事件任务的问题 |
   |---|---|---|
   | `AddTask`（`scheduler.go:305`） | `cron.ParseStandard` 失败即 return error | 空 cron → 任务无法创建 |
   | `UpdateTask`（`:433`） | 同上 + 重算 `NextRunAt` | 无法更新 |
   | `ResumeTask`（`:401`） | 同上 | 无法恢复 |
   | `registerTaskLocked`（`:487`） | 解析 cron 后注册 `cron.FuncJob` | 事件任务无 cron 可注册 |
   | `LoadTasksFromDB`（`:239-274`） | 把所有 `active` 任务无条件注册进 cron | 事件任务被误注册 |

   另有完成块（`:909-956`）按 `schedule.Next` + `repeatMode` 处理，对事件任务不适用。

## 阶段划分总览

| 阶段 | 目标 | 交付 | 关闭的评审项 |
|---|---|---|---|
| **P1 可见性** | 能看 issue/PR | forge 抽象 + remote 解析 + 绑定 + 列表/详情 + 设置/token | R3 R4 R5 R11 R13 R17 R18 |
| **P2 感知** | 知道有变化 | 轮询 worker + 快照 diff + 通知 + 未读 badge | R7 R8 R9 R10 R12 |
| **P3 自动化** | 变化触发 AI | 调度器扩展（cron 解耦 + 变量注入 + 上下文通道）+ 事件任务 | R1 R6 R14 R15 |
| **P4 AI 分析** | 把 issue/PR 喂给 AI | URL 附件类型 + 「让 AI 分析」 | R2 |

P1 可立即开工，不依赖 P2-P4 的边界定义。各阶段独立可交付、可验证。

---

## P1：可见性（只读浏览）

### 目标

在项目内浏览对应 repo 的 issue / PR 列表与详情。无轮询、无通知、无任务触发。

### 交互

**入口**：新增独立 Dock 主 tab「Issues & PRs」，与 chat / browse / view / history 平级。

- 移动端加入固定区，溢出由现有响应式逻辑自动处理。
- 桌面端追加到宽屏垂直活动栏。
- 内容跟随当前项目；不引入跨项目聚合视图。

**空状态**（复用 AskUserQuestion 卡片形态：卡片内一句提问 + 可点选项按钮）：

- 未选项目 → 卡片问"要查看哪个项目？"，列出项目作为选项。
- 已选项目但未绑定 repo → 卡片问"要绑定哪个仓库？"，带「绑定」按钮。

**列表**：

- 一个列表 + 类型 chip（Issues / PRs）。
- 状态 chip 组：**Open（默认）/ Closed / All**。
- 「跟我相关」chip 组：全部 / 分配给我 / 我提的 / 待我 review（身份从 token 自动获取）。
- 布局分两行：第一行类型 chip + 状态 chip；第二行「跟我相关」chip 组（移动端第二行可横向滚动，避免垂直占用过重）。
- 排序：按更新时间倒序。
- 列表行：标题 + 编号 + 状态图标（open/closed/merged）+ 作者 + 更新时间。
- **分页与搜索（评审 R13 新增）**：滚动到底部加载下一页（复用聊天 `loadMore` 模式）；顶部提供搜索框，搜索在**服务端**按 repo 范围执行（GitHub/GitLab 均支持 issue 搜索），不做客户端全量过滤。
- 加载态：居中 `LoadingIndicator`。刷新：仅顶部 `RefreshButton`。
- 拉取失败：错误卡片 + 重试按钮，区分原因（token 失效 → 引导去设置；限流 → 提示稍后再试；网络不通 → 提示检查连通）。

**详情**：

- 原地替换列表（`currentView` 模式），面包屑返回。
- 正文 + 评论/时间线（**不含 diff**）。
- 正文与评论复用 `useMarkdownRenderPipeline`。
- 评论先展示最新 N 条，向上加载更多。
- header 右侧「在浏览器打开」图标按钮。

**仓库绑定**：

- 主路径：列出本地 git remote 供选择。
- 兜底：手动填写 repo URL。
- 入口：未绑定时空状态卡片的「绑定」按钮；已绑定时 panel header「更多」菜单。

### 技术方案

**forge 抽象层** `internal/forge/`：

- `Provider` 接口 + 统一模型（`Issue` / `ChangeRequest` / `Comment`）。
- 实现：`forge/github/`（`google/go-github` v85）、`forge/gitlab/`（自建轻量 REST client）。
- 共用 REST 基建：分页、超时、错误分类。
- 参考 `internal/push/` 的 manager 生命周期与 DB adapter 解耦模式（注意：push 是 IM Stream SDK，不是通用 REST，类比有限）。

**remote URL 解析（评审 R17 补充边界）**：需支持并区分：

- HTTPS 形式 `https://host/group/repo.git`
- SSH 形式 `git@host:group/repo.git`（无 scheme，需独立解析）
- 显式端口 `host:8443`（需剥离）
- `.git` 后缀（需剥离）
- 拒绝网页 URL（`/tree/main`、GitLab `/-/` 路径）
- 平台差异：GitHub 只允许 `owner/repo` 两段；GitLab 允许**多级 group**（host 之后全部段，最后一段为 repo，其余为 namespace）。解析后按平台校验段数，GitHub 收到 3 段应拒绝。

**安全加固（评审 R3 / R4 / R5，P1 必须做）**：

- **SSRF 防护（R3）**：绑定流程中，若解析出的 host 不是 `github.com` / `gitlab.com`，**必须显式展示 host 并要求用户确认**后才允许首次带凭据请求。同时：仅允许 `https`；拒绝私网/回环/链路本地/云元数据地址（`127.0.0.0/8`、`10/8`、`172.16/12`、`192.168/16`、`169.254.0.0/16`、`::1`、`fc00::/7`）；解析后的 host 按绑定固化，不随 remote 静默变更。
- **凭据按 host 隔离（R4）**：token 存储键为 `(platform, host)`，不再是单一全局 token。自建 GitLab 实例各自独立凭据，一个 host 被控不影响其他 host。
- **密钥处理（R5）**：`GET /api/config` **不回传密钥明文**，改为回传 `has_github_token: bool` 之类的存在性标记，密钥字段写后不可读（与密码字段一致的 write-only 语义）；`config.yaml` 写入权限从 `0o644` 收紧为 `0o600`（现为 `settings.go:1646`）。
- **TLS 策略（R17）**：自建 GitLab 支持自定义 CA 证书配置；提供"允许不安全 TLS"显式开关（默认关），开启时在设置页与日志中显著告警。
- **PAT scope（R18）**：文档与设置页明确要求最小权限——GitHub 用**细粒度 token**（Issues: read、Pull requests: read、Actions: read、Metadata: read），避免 classic `repo`（对全账号读写）；GitLab 用 `read_api`。绑定时可校验 scope，不足则提示。

**配置默认值（评审 R11）**：新增的通知开关等布尔配置必须在 `ApplyDefaults`（`internal/model/config.go:256+`）显式设为期望默认值——Go bool 零值为 `false`，不显式设置会默认全关。

**repo 绑定存储**：

- 表 `project_forges`：`project_path`、platform、host、owner/repo（namespace 可能多段）、绑定来源（自动/手动）、创建/更新时间。
- `project_path` 写入前**归一化**（去尾斜杠、解析 symlink、统一大小写策略），避免同一项目产生多行。

**端点**：`/api/forge/*`（列表、详情、评论、绑定 CRUD、测试连接），全部走 `middleware.Auth`。

### 测试面

- remote URL 解析：HTTPS/SSH/端口/`.git`/多级 group/网页 URL 拒绝/GitHub 段数校验。
- SSRF 防护：私网与元数据 IP 拒绝、非 https 拒绝、非官方 host 需确认。
- 凭据按 host 隔离读写。
- 密钥遮蔽：`GET /api/config` 不含明文、`config.yaml` 权限为 0600。
- `ApplyDefaults` 布尔默认值。
- Provider 字段映射与分页；列表分页与搜索；绑定 CRUD。

### 影响文件

| 层 | 文件 |
|---|---|
| 后端新 | `internal/forge/`（接口 + REST 基建 + github/gitlab adapter）、remote URL 解析、`internal/handler/forge.go` |
| 后端改 | `internal/service/database.go`（`project_forges`）、`internal/model/config.go`（凭据 + `ApplyDefaults`）、`internal/handler/settings.go`（密钥遮蔽 + 0600 + 白名单）、`internal/handler/handler.go`（路由）、go.mod |
| 前端新 | issue/PR 列表/详情/绑定表单/空状态组件 + composable |
| 前端改 | `App.vue`（新主 tab）、`settingsFieldMap.ts` + `SettingsIndex.vue`（「GitHub/GitLab 集成」分类）、i18n |

---

## P2：感知（变化检测 + 通知）

### 目标

后台检测 repo 变化，按配置推送通知，tab 显示未读。

### 交互

- tab 带**未读数 badge**。
- 设置分类「GitHub/GitLab 集成」内六类事件 switch：新开 / 关闭 / 合并 / 重开 / 评论 / CI 完成，默认全开。
- 通知渠道：App 内 + 系统通知栏 + IM 机器人。

**未读语义（评审 R12 修正）**：未读计数**与通知开关解耦**——它表示"列表里有新变化"，即使某类事件的通知被关闭，未读仍计数（否则全关时 badge 死掉而 AI 任务照跑，用户零信号）。进入 tab 清除未读。

### 技术方案

**变化获取机制**：

主通道是**水位线增量拉取 + 本地快照 diff**，不用平台原生事件/通知 API 当主通道——GitHub `issues?since` 只表明"变了"而不表明"变成了什么事件"；GitLab events API 的 `after`/`before` 只支持到天（`YYYY-MM-DD`）。

- **统一接口**：`Provider.ListChanges(ctx, repo, since) ([]Change, error)`。
- **增量拉取**：GitHub `GET /repos/{owner}/{repo}/issues?since=&state=all&sort=updated&direction=asc`（PR 走同一端点）；GitLab `GET /projects/:id/issues?updated_after=&order_by=updated_at&sort=asc` 与 `/merge_requests`。
- **事件靠本地 diff 推导**：`forge_items` 快照表（item id、type、state、merged 标记、last comment id、last comment updated_at、updated_at）。

**水位线正确性（评审 R7，关键）**：

- 增量拉取**必须分页**，且**整批（所有页）拉完才推进水位线**；任一分页失败则不推进，下轮重试。
- 水位线取"已完整拉取结果中的最大 `updated_at`"。
- 边界用**严格 `>`** 加一个小的重叠窗口（如回退 1s），并对已派发事件做**事件级去重**（键 = item id + 事件类型 + 修订标识），不依赖时间戳唯一性。
- 并发修改：以"整批拉完"为准，避免 item 在分页过程中被更新导致漏拉。

**事件类型推导（含完整状态转移表，评审 R14 相关）**：

| 本地 → 远端 | 事件 |
|---|---|
| 无 → 有 | `created` |
| open → closed（未合并） | `closed` |
| 出现 merged 标记 | `merged` |
| closed → open | `reopened` |
| 新评论（id 或 updated_at 变化） | `commented` |
| pipeline 状态转为完成 | `pipeline_done` |

同一轮内发生多段转移（如 closed → reopened → merged）时，**按终态优先级**派发：`merged` > `reopened` > `closed`，即一个区间内只派发最有信息量的一个事件，并在事件 payload 里保留原始状态序列。

**评论获取（评审 R8 修正）**：

- GitHub：仓库级 `GET /repos/{owner}/{repo}/issues/comments?since=&sort=updated` 一次拉全。
- GitLab：**无仓库级评论端点**，对"本轮有更新的"资源逐条拉 notes。
- 去重键从单一 `last_comment_id` 改为 **`(last_comment_id, last_comment_updated_at)`** 双键——评论被**编辑**时 id 不变、仅 `updated_at` 变，单 id 会漏。GitLab note id 为项目内自增，与 GitHub 全局自增语义不同，两平台分别处理。
- **评论独立降频**：状态类 ~60s，评论类 ~5min（两 ticker 共用同一 worker 生命周期）。

**CI 完成事件**：单独拉取——GitHub `GET /repos/{owner}/{repo}/actions/runs`，GitLab `GET /projects/:id/pipelines`。

**限流与退避（评审 R9，关键）**：

- **per-host 令牌桶** + **全局并发上限**，避免多 repo 同时打满限额。
- 必须处理 `429` / `403`（rate limited）的 `Retry-After` 头，指数退避 + 抖动。
- 预算核算（诚实版）：状态 60s（60/hr）+ 评论 5min（12/hr）+ CI（~60/hr）≈ **130 调用/hr/repo**，未含分页。GitHub 认证限额 5000/hr → 仅够 **~38 个 repo**，且用户浏览列表/详情的交互请求共用同一配额。因此需：可配置轮询间隔、按 repo 数量自适应降频、限额告警。
- GitLab 侧限额按分钟计且自建实例各不相同，需可配置 + 运行时观测 `RateLimit-*` 头。
- GitHub 用 ETag / `If-None-Match`（304 不计入限额）；仅对确实返回 ETag 的端点启用。

**轮询按 repo 去重（评审 R10）**：

- 轮询单元是 **repo（host + owner/repo）**，不是 `project_forges` 行——同一 repo 被两个项目绑定只轮询一次，通知去重。
- 绑定自动修复：项目 git remote 变更时重新解析并提示用户确认；项目重命名/移动导致 `project_path` 变化时，提供绑定迁移或重新识别。

**首次全量**：新绑定 repo 先拉一次全量建快照，但**不派发历史事件**（水位线初始化为"现在"）。

**快照剪枝（评审 Minor）**：定期全量扫描并清理 `forge_items` 中远端已不存在的行，避免无限增长与"重建同名 item 误 diff"。

**worker 生命周期**：参照 `SessionCleanupWorker`（`internal/service/session_cleanup.go`）的 Start/Stop/run + ticker，在 `cmd/server/main.go` 与 scheduler / rag cleanup 一并启停。**token 失效/被吊销（401）时暂停该 host 的轮询并置为可见错误状态**，不持续重试。

**通知派发**：事件 → 查全局开关（命中则推送）→ 未读计数。通知与未读共用事件源但独立于开关。

### 测试面

- 水位线：分页未拉完不推进；多页变更不丢；边界严格 `>` + 重叠窗口 + 事件级去重。
- 状态转移表（含同区间多段转移的终态优先级）。
- 评论编辑（id 不变、updated_at 变）能识别；GitLab note 逐条拉取。
- 限流：429/403 `Retry-After` 退避；令牌桶与并发上限；ETag 304 不计配额。
- 同一 repo 多项目绑定只轮询一次。
- 首次全量不派发历史事件；快照剪枝。
- token 失效暂停轮询 + 错误可见。
- 未读计数与通知开关解耦。
- 通知开关默认全开；关闭后不推送但未读仍计数。

### 影响文件

| 层 | 文件 |
|---|---|
| 后端新 | 轮询 worker、快照 diff 引擎、限流器（令牌桶 + 退避）、`internal/service/forge_sync.go` |
| 后端改 | `internal/service/database.go`（`forge_items`）、`internal/model/config.go`（通知开关 + 默认值）、`internal/handler/settings.go`、`cmd/server/main.go`（worker 启停）、通知派发复用 `internal/push/` |
| 前端改 | `App.vue`（未读 badge）、列表/详情（分页 + 搜索）、i18n |

---

## P3：自动化（事件触发 AI 任务）

### 目标

事件到达时全自动触发预定义 AI 任务。

### 交互

- 任务管理里新建任务时，`scheduleInfo` 区首个 form-group 新增"**触发方式：定时 / 事件**"preset-buttons；选"事件"后条件展开事件类型多选。
- **事件上下文固定注入**：prompt 区上方出现只读事件上下文块，按模板列出变量，用户可见不可编辑；用户输入在下方，二者拼接为最终 prompt。
- **模板按事件类型条件渲染（评审 R14 修正）**：只保留**适用**变量，不适用的一律省略整行，避免产出 `CI 状态：`（空值）这类悬空行。例如评论事件不出现 `PIPELINE_*` 行。
- 变量全集：`EVENT_TYPE`、`REPO`、`ITEM_NUMBER`、`ITEM_TYPE`、`TITLE`、`URL`、`AUTHOR`、`STATE`、`COMMENT_BODY`、`PIPELINE_STATUS`、`PIPELINE_URL`。
- 事件到达 → 执行 → 产出会话 + 任务执行记录 → 四通道通知。
- **溯源（评审 R15）**：任务执行记录存触发事件 payload 与 item 链接，通知可深链回原始 issue/PR。

### 技术方案

**调度器 cron 解耦（评审 R1，P3 核心改动）**：

- `model.ScheduledTask` 增 `TriggerMode`（`cron` / `event`）与事件配置字段；`scheduled_tasks` 表增对应列（幂等 `ALTER TABLE` + `pragma_table_info` 守卫）。
- 以下五处逐一加 `TriggerMode` 分支：

  | 位置 | cron 任务 | 事件任务 |
  |---|---|---|
  | `AddTask`（`:305`） | 解析 cron、算 `NextRunAt` | 跳过 cron 解析；校验事件配置合法性 |
  | `UpdateTask`（`:433`） | 重算 `NextRunAt`、重注册 | 跳过 cron；仅更新事件配置 |
  | `ResumeTask`（`:401`） | 解析 cron | 跳过 cron；重新加入事件监听 |
  | `registerTaskLocked`（`:487`） | 注册 `cron.FuncJob` | 不注册 cron；注册到事件匹配表 |
  | `LoadTasksFromDB`（`:239-274`） | 注册进 cron | **排除**，改为注册到事件匹配表 |

- 完成块（`:909-956`）：`repeatMode` / `maxRuns` / 移除 cron 条目的逻辑仅对 cron 任务生效；事件任务不因执行次数结束（除非显式配置上限）。

**变量注入**：`executeTask` 在写入会话消息与构造 `ChatRequest` 前渲染 prompt（替换点：`internal/service/scheduler.go:695` 与 `:722` 之前）。模板引擎支持 `{{VAR}}` 占位符，未知变量保持原样并记日志。

**事件上下文通道**：`TriggerTask` / `executeTask` 扩展签名以接收事件上下文 map。

**触发护栏（评审 R6，关键）**：

- **事件队列**：`TriggerTask` 现用 `taskRunning.LoadOrStore`，运行中再来事件直接报错丢弃（`:420-422`）——这是数据丢失。改为**持久化事件队列**：同一任务运行中时，新事件入队，待当前执行结束后按序/合并执行。
- **per-repo 限流 + debounce**：短时间内同一 repo 的同类事件合并（如 5 条连续评论合并为一次触发），避免事件风暴。
- **全局 kill-switch**：设置里提供"暂停所有事件触发"总开关。
- **并发上限**：限制同时运行的事件触发任务数，超出排队。
- **防递归（关键）**：`CLAWBENCH_SCHEDULED=1` 只阻止 AI 通过 `/cb-task` 再建任务，**不阻止 AI 用 gh/glab 写回 forge**。因此：识别 AI 自身身份（token 对应账号）产生的写操作事件并**抑制**，或要求自动任务在只读模式下运行，二者择一（实施期定，但必须有一道）。

**默认关闭策略建议**：新的事件触发任务默认**不自动执行**，需用户在任务上显式开启"全自动"（与决策记录的"全自动"一致，但把风险点显式化、可逐个关闭）。

### 测试面

- 五处 cron 解耦分支（事件任务可创建/更新/恢复/加载，不误注册 cron）。
- 事件任务不因执行次数被误标 completed。
- prompt 变量替换（含未知变量保留）。
- 事件上下文传递到执行记录与 prompt。
- 事件队列：运行中事件入队不丢；debounce 合并；kill-switch 生效；并发上限；防递归抑制自产事件。
- 模板条件渲染：不适用变量整行省略。

### 影响文件

| 层 | 文件 |
|---|---|
| 后端新 | 事件队列、事件匹配表、防递归判定 |
| 后端改 | `internal/model/scheduler.go`、`internal/service/scheduler.go`（五处分支 + 变量注入 + 上下文通道）、`internal/service/database.go`（列 + 队列表）、`internal/model/config.go`（kill-switch）、`internal/handler/scheduler.go`、通知派发 |
| 前端改 | `TaskFormPage.vue`（触发方式 + 只读模板 + 事件类型多选）、任务详情（溯源深链）、设置（kill-switch）、i18n |

---

## P4：AI 分析（URL 附件 + 让 AI 分析）

### 目标

从 issue/PR 详情一键把内容喂给 AI。

### 交互

- 详情页底部固定栏「**让 AI 分析**」主按钮。
- 点击后复用引用对话输入框：issue/PR 地址作为**会话附件**样式添加，chip 文案 `owner/repo#123`；用户划取的文本以 markdown fence 进入消息文本；用户输入在最后。
- 最终消息结构：**URL 附件 + 选中文本 fence + 用户输入**。
- **会话目标（评审 I7 补充）**：若当前项目无活跃会话，则新建一个会话并切到聊天 tab；若有活跃会话，追加到当前会话。

### 技术方案

**URL 附件类型（评审 R2，P4 核心改动）**：

现有附件通道**只支持本地文件路径**，URL 走不通。需打通的调用点（比初版文档列的多）：

| 位置 | 现状 | 需改动 |
|---|---|---|
| `FileEntry`（`internal/model/chat.go:18-23`） | 仅 `path/isDir/startLine/endLine` | 后端模型增 `kind` + `url` |
| `internal/handler/chat.go:371-379` | 对每个 entry `validateAndResolvePath` + `os.Stat` → 404 | `kind='url'` 时跳过路径校验，不落 `Files` |
| `normalizeFileEntry`（`fileAttachmentUtils.ts:15`） | 剥掉未知字段 | 保留 `kind`/`url` |
| `entryKey`（`:37`）/ `dedupeFiles` | 只按 `path` 去重 | URL 按 `url` 去重 |
| `addAttachedFile`（`useChatContext.ts:55`） | 构造 `{path,isDir,startLine,endLine}` | 支持构造 URL entry |
| `buildSendChannels`（`fileAttachmentUtils.ts:67-87`） | 无行号 entry 一律进 `filePaths` | URL 走独立通道 |
| `AttachmentTags.vue:26-36` | `getFileName(path)` + `isImageFile` + `file-click` | 按 kind 渲染链接样式 |
| `ChatPanelContent.vue:901-933` | `optimisticMsg.filePath = filePaths[0]`、`files: files` | 序列化带 kind/url |

**选中文本**：复用 `buildQuoteBlock` 的 fence 格式（注意：`buildQuoteBlock` 当前**未导出**，公开 API 是 `buildMultiQuoteMessage`，实施时按其实际签名使用或导出）。

**复用入口**：`useChatContext`（`attachedFiles` / `stagedQuotes`）、`useQuoteQuestion().showBar()`。

### 测试面

- URL entry 全链路：构造 → normalize 保留 → 去重 → chip 渲染 → 发送序列化 → 后端跳过路径校验。
- 后端不将 URL 当本地路径（无 404）。
- 无活跃会话时新建会话并切 tab。
- 消息拼接顺序：URL 附件 + 选中文本 fence + 用户输入。

### 影响文件

| 层 | 文件 |
|---|---|
| 后端改 | `internal/model/chat.go`、`internal/handler/chat.go`（URL entry 跳过校验） |
| 前端改 | `utils/fileAttachmentUtils.ts`、`composables/useChatContext.ts`、`components/chat/AttachmentTags.vue`、`components/chat/ChatPanelContent.vue`、`utils/quoteQuestionUtils.ts`、详情页底部栏、i18n |

---

## 已接受的限制

1. **ClawBench 的 forge API 调用只读**：不发起写请求；自动 AI 任务的写能力由 P3 护栏约束。
2. **不做跨项目聚合视图**：tab 内容跟随当前项目。
3. **不做 diff**：详情页不展示代码 diff。
4. **不做 webhook**：仅用轮询获取变化，不接收 forge 入站回调。
5. **事件靠本地 diff 推导**：平台侧删除历史数据时本地快照与远端会短暂不一致；已加剪枝缓解。
6. **评论类事件有延迟**：评论独立降频（~5min），评论通知非实时。
7. **单 repo 绑定**：一个项目只绑一个 repo，多 remote 需选一个。
8. **轮询有规模上限**：按诚实预算（~130 调用/hr/repo），GitHub 5000/hr 约支撑 ~38 个 repo；超出需降频。
9. **token 仍存于配置文件**：P1 收紧为 0600 权限 + 回传遮蔽，但未做加密存储（加密是独立议题）。
10. **事件触发默认全自动**：用户在任务上显式接受；提供全局 kill-switch 与 per-repo debounce 兜底。

## 影响文件清单（汇总）

| 阶段 | 后端 | 前端 |
|---|---|---|
| P1 | 新：`internal/forge/`（接口 + REST 基建 + github/gitlab）、remote 解析、`handler/forge.go`；改：`database.go`、`model/config.go`、`handler/settings.go`、`handler/handler.go`、go.mod | 新：列表/详情/绑定/空状态组件 + composable；改：`App.vue`、`settingsFieldMap.ts`、`SettingsIndex.vue`、i18n |
| P2 | 新：轮询 worker、快照 diff、限流器；改：`database.go`（`forge_items`）、`model/config.go`、`handler/settings.go`、`cmd/server/main.go` | 改：`App.vue`（badge）、列表（分页/搜索）、i18n |
| P3 | 新：事件队列、事件匹配表、防递归；改：`model/scheduler.go`、`service/scheduler.go`（五处分支 + 变量注入 + 上下文通道）、`database.go`、`model/config.go`、`handler/scheduler.go` | 改：`TaskFormPage.vue`、任务详情（溯源）、设置（kill-switch）、i18n |
| P4 | 改：`model/chat.go`、`handler/chat.go` | 改：`fileAttachmentUtils.ts`、`useChatContext.ts`、`AttachmentTags.vue`、`ChatPanelContent.vue`、`quoteQuestionUtils.ts`、详情底部栏、i18n |
