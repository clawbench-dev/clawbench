# AGENTS.md

## 项目概述

ClawBench 是面向手机 / 平板 / 桌面的多端 AI 工作台，移动端交互适配优先、桌面端完整支持，将 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、Qoder CN、VeCLI、CodeWhale、MiMo-Code、Pi、Copilot、Kimi、Antigravity、Grok Build、ZCode、DeepSeek Harness、Hermes Agent、Cursor、Command Code、OpenClaw）封装为 Web 平台。Go 后端调用 CLI 工具，通过 WebSocket 流式传输 JSON 事件；Vue 3 前端实时渲染。支持 ACP (Agent Client Protocol) stdio 传输（含桥接适配器）、**多智能体群聊**（主持人 / 自由两种模式 + 正交的自动巡航开关）、**会议与 AI 听记**（屏幕 / 摄像头 / 麦克风共享 + 实时转写与纪要，记录永久；默认点对点，可选 LiveKit 中继容纳数百人）、SSH 隧道端口转发、任务系统（含 GitHub/GitLab 事件触发）。

规格文档：`docs/spec/`（模块索引见 `docs/spec/README.md`）。面向使用者的图文操作手册：`docs/user-guide/README.md`（截图维护流程见 `docs/Skills/clawbench-user-guide/SKILL.md`）。

## 构建与运行

```bash
# 构建
./build.sh                                            # Go 二进制 + Vue 前端 + Excalidraw
./build.sh --windows|--linux|--linux-arm64|--darwin   # 交叉编译
go build -o clawbench ./cmd/server                    # 仅 Go 二进制

# 开发
./dev-server.sh [--fg|--stop|--restart]               # Vite HMR 代理到后端
./clawbench [--port 8080] [--data-dir /data/.clawbench]   # 直接运行，默认端口 20000

# 测试与检查
go test ./...                                         # 单包：go test ./internal/ai/...
npm test                                              # Vitest 前端测试
npm --prefix harmony test                             # 鸿蒙宿主契约测试（node:test，独立于根 vitest）
./scripts/pre-push-checks.sh [--skip-coverage|--skip-android]   # 推送前全量检查

# E2E（Playwright，见下方「E2E 测试」规则）
go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock   # e2e 依赖这两个二进制
npx playwright test --config e2e/playwright.config.ts --project=chromium-coverage e2e/specs/<你的 spec>.spec.ts

# 编译并后台重启（可在 Web 终端内执行）
./build.sh --restart [--restart-skip-build] [--restart-port=8080]
```

### 运维：一个数据目录只能跑一个服务

服务启动时在数据目录下取一把独占锁（`.instance.lock`，进程退出即由系统释放，不存在残留锁）。同一个数据目录已有实例在跑时，新进程最多等 10 秒（覆盖重启时旧进程还没退完的窗口）后**拒绝启动**并报错。

原因不是抢端口，而是两个实例共用一个数据库会互相破坏：启动恢复把「库里还有 `streaming=1`」当成「上个进程崩溃了」，于是把对方**正在跑**的那一轮改写成「已取消 + 服务重启中断」并自己重跑一遍，用户看到的就是「消息内容显示已结束，但终止按钮和列表动画还在」。要在本地起第二个实例做实验，必须显式给 `--data-dir /tmp/<某个临时目录>`。

### 运维：僵尸进程清理

`./scripts/kill-zombies.sh` 清理僵尸（defunct）进程及其孤儿进程树。默认 dry-run 列出僵尸与将要杀的进程树；`--kill` 实际清理（`--force` 跳过确认）；`--port 8080` 额外保护 8080 端口的服务器。

**安全规则（脚本默认强制执行）：**

- **绝不触碰 20000 端口主服务器** 及其完整后代树（包括 `clawbench --acp` 会话派生的 vitest/build/worker 进程）——通过 `/proc` 树形遍历识别，非 `pgrep -f` 模糊匹配
- 僵尸父进程是 init（PID 1）时自动跳过（init 会自动 reap）
- 杀进程树按子孙先 TERM → 再 KILL 顺序，避免留下新僵尸
- `--kill-protected` 可显式覆盖保护（危险，谨慎使用）

## 客户端日志回传

前端 JS、Android 原生与 Electron 主进程日志统一回传服务器，汇入**单文件** `{data-dir}/logs/client.log`，行内用 `[js]` / `[android]` / `[electron]` 标记区分来源。

- **开关**：设置 → 调试 →「调试日志捕获」（`logCapture`，默认关）。开启后 App 模式（Android）的 JS 日志仅 HTTP 上报一份（跳过 console 与 native 桥，避免 `WebView:LOG` 重复与 `[object Object]` 失真），网页模式则 console + HTTP 双份；关闭时日志只在本地（logcat / console）可见。
- **JS（`web/src/utils/appLog.ts`）**：批量 POST `/api/client-log`（2s / 200 条缓冲 / 200 条每请求），`source="js"` → `[js]` 行。
- **Android（`android/app/.../AppLog.java`）**：捕获开启时每 3s POST `/api/client-log`，`source="android"` → `[android]` 行；请求带 WebView 会话 Cookie（同进程读取）。
- **Electron 主进程（`desktop/src/main/clientLog.ts`）**：同样的 2s / 200 条缓冲，`source="electron"` → `[electron]` 行；认证用 Electron cookie jar 里的会话 Cookie（`session.defaultSession.cookies`，按 `_clawbench_session` 后缀匹配，因服务端名字带端口前缀）。同时写本地 `{userData}/desktop.log`。未捕获异常/未处理拒绝经 `recordError` 一并上报（此前只进 stdout，关掉终端即丢失）。`native:log` 桥（渲染进程 → 主进程）落同一个文件——桌面没有 logcat 兜底，这条链是它唯一的本地副本。
  - **桌面不跳过 console**：`appLog.emit()` 的 `singleHttp` 分支只对 Android 生效。桌面若照搬，`desktop.log` 会因监听 `console-message` 却拿不到 console 输出而**恒为空**（实测：捕获开启时写 5 条 appLog 仍 0 字节）。因此桌面保留 console，改为跳过 native 桥转发——否则同一行会在 `desktop.log` 与 `client.log` 各出现两次。
- **服务端（`internal/handler/android_log.go`）**：`ServeClientLog` 统一写 `{LogDir}/logs/client.log`，行格式 `2006-01-02T15:04:05.000 [js] I/ChatStream: msg`，50MiB 轮转到 `client.log.1`。
  - **需鉴权**：这是向服务端文件追加写入的原语，匿名调用者可伪造日志行或反复触发轮转以销毁上一代日志。三个客户端上报时均已登录（JS 中继受 `logCapture` 门控且仅在已认证的应用内启用；Android `startCapture` 由登录后的 WebView 桥触发；Electron 主进程需读到会话 Cookie，未登录时静默跳过）。
  - **字段清洗**：`Msg`/`Tag`/`Level`/`Source` 全部转义换行、CR 与 NUL 并截断——只转义 `Msg` 会让 `Tag` 可伪造整行。`Source` 有独立的长度上限（32）而非复用 level 的 8：`"electron"` 恰好 8 字节，复用会让下一个更长的来源名被静默截断成另一个来源。
  - **上限**：单请求 200 条、总计 256 KiB。**不限流**——磁盘已由字节上限与 50 MiB 文件上限封死，限流不改变该边界；且 429 会被两端客户端静默丢弃（`appLog.ts` 不重试），徒增丢日志风险。
- **查看**：`tail -f {data-dir}/logs/client.log`、`grep '\[js\]' {data-dir}/logs/client.log`、`grep '\[electron\]' {data-dir}/logs/client.log`。

## 架构

### 后端（Go）

入口：`cmd/server/main.go`。各包深度设计见 `docs/spec/`。

| 包 | 职责 |
|---|------|
| `internal/handler/` | 全部 `/api/` HTTP 端点（经 `middleware.Auth` 鉴权）+ WebSocket 聊天流式推送 |
| `internal/api/` | `go:embed` OpenAPI 规格，按 operationId 渲染内置斜杠命令注入给 AI 的接口提示片段 |
| `internal/wallpaper/` | 壁纸校验 / 缩放 / 编码 + 磁盘布局与生效解析；handler 与 service worker 共用。缩放上限取舍见源码注释 |
| `internal/gitignore/` | 判定「git 是否会跟踪该路径」：go-git 模式引擎 + 来自 index 的三条规则（已跟踪文件/含已跟踪文件的目录永不忽略、祖先被排除则整体忽略、自身最后一条匹配）。文件管理器灰显与 cloc 排除共用；按真实 `git check-ignore` 差分验证 |
| `internal/store/` | SQLite 数据层叶子包（只依赖 `model` + `dbutil`，供 `service` 与 `rag` 共用而不成环）：连接句柄与**进程级唯一写锁**（同时串行化 service 写池与 RAG 自有 `*sql.DB` 对同一文件的写）、读写原语、`projects.go` 项目注册表（path↔id 解析 + 缓存 + `ForgetProjectPath`/`RenameProject` + 跨包改名钩子）、`session_queries.go`/`cluster.go` 的跨业务会话与聚类查询 |
| `internal/service/` | 业务逻辑：聊天持久化、摘要与推荐的调度、调度器（cron + 事件触发任务；cron 可选的前置脚本执行器 `task_script.go`，静默成功即跳过 AI 且不发通知）、SQLite、Schema 迁移、**Agent 存储与守卫**（`agent_store.go`：`disabled` 列 + `project_id` 列（`NULL`=全局，值=仅该项目可见，只拦新建入口不追溯已有会话）+ `GetAgentUsage`/`GetAllAgentUsage` 计数——会话数 = 1:1 聊天 + 活跃 `group_member` 关系、排除任务执行产生的 `scheduled` 会话；删除即**接管**（`agent_takeover.go`：会话/任务/群行全部转交默认智能体，只有「有会话正在运行」才 409；同后端沿用原生会话、跨后端强制重开并注入历史），`PatchAgentFields` 支持 `disabled`/`project_id`，`ReorderAgents` 单事务写 `sort_order`；`model.SortAgentsByOrder` 是 DB 加载与 reorder 的唯一排序规则；`CreateCustomACPAgent` 建自定义 ACP 智能体（backend 恒 `custom-acp`、传输固定 `acp-stdio`、命令 ≤4096 字节且按空格拆分无引号）；`agent_sessions.go` 的 `ListAgentSessions`（`GET /api/agents/{id}/sessions`）列出该智能体的会话——1:1 聊天 + 它当主持人/当成员的群聊，**跨项目且含归档**，键集分页；范围刻意与上面那个计数同源，否则列表会和设置页上方的数字打架，群成员行还会带 `role`/`memberName` 让前端标出它是怎么参与这个群的）、用量聚合（`usage_stats.go`，含 `wall_ms` 墙钟时长——取消回合也记，`MigrateLedgerWallClock` 一次性回填历史）、会话截断；**项目以整数 id 为身份**（`internal/store` 的项目注册表 + `projects_migrate.go` 的路径→id 转换：所有项目作用域表存 `project_id` 而非路径，改名/移动目录只需一条 UPDATE；`project_id=0` 是全局标签与无法归属分享的保留哨兵，故不声明外键）；**项目注册表管理**（`project_registry.go` 的 `ListAllProjects`/`GetProjectDetail`——列表只做单次 `os.Stat` 判目录是否存在、详情附 `DetectRepoLayout` 仓库类型；`project_delete.go` 的 `DeleteProjectData` 在同一事务清理约 15 张项目作用域表 + 子表，写锁内判运行中会话守卫，提交后再调 `rag.DeleteProjectData` 避免持锁死锁；`project_rename.go` 的 `RenameProjectData` 是「修改路径」——只改 `projects` 一条 UPDATE、不移动磁盘目录，拒绝全局哨兵/未知 id/目标非目录/目标被别的项目占用/有运行中会话（runner 持有旧路径，事后写入会把旧路径重新注册成新项目），改名的是当前项目时由 handler 重写 `clawbench_project` cookie）；会话运行态收敛在 `session_runner.go`（单一 owner runner，运行态与可取消性同源），AI 回合编排唯一实现 `run_turn.go`，请求构造唯一实现 `chat_request.go`；**排队消息存独立表 `queued_messages`（出队才落 `chat_history`，`queue_store.go` 的 claim 在同一事务内 DELETE 队列行 + INSERT 历史行，使历史行 id 顺序恒等于对话顺序）**，队列兜底回收 `queue_reaper.go`；**AI 群聊编排器 `group_orchestrator.go`**（主持人/自由两模式共用循环内核 `drainSpeakers`/`runSpeakerTurn`；**正交的自动巡航开关** `cruise`——把循环的终止从「交回用户」改为「成员确认任务已满足」（`<clawbench-goal-satisfied/>` 标签，并入 `internal/grouprouting` 双镜像），host 一人拍板 / free 群发半数 `votes*2>=n`，六种主持人收工形态（含无标签收尾散文与大白话提问）全拦改触发确认回合、开关每回合实时读；`group_store.go` 成员与群设置（成员**禁用/启用**是 `chat_sessions.disabled`，只影响调度、不动行与上下文，启用后补齐禁用期间内容；**移出群聊是 `chat_sessions.removed`**——行保留是因为 `chat_history.agent_id` 指向它，花名册/调度一律过滤 `removed=0`，只有发言人归属解析包含它；主持人、最后一位可发言成员不可禁用/删除，本轮讨论进行中不可删，均 409）、`group_inject.go` 增量注入、`group_prompt.go` 提示词、`group_member_request.go` 成员 resume、`group_member_compact.go` 单成员压缩（直接驱动该成员的 ACP 连接、状态落成员行、不在群时间线产出输出；期间消息排队、开始/完成/失败写系统行）；成员即隐藏 `chat_sessions` 行 `session_type='group_member'`，消息只存群会话，`run_turn` 新增 `TimelineSessionID` 解耦时间线语义）；`/btw` 旁路问答（`btw.go`，独立表 `btw_questions`）；**用户身份**（`user_identity_prompt.go`：把 `user.nickname` 与 `user.profile` 拼成 `## About the User` 段注入系统提示，**请求时读**故改完下一轮生效；两个系统提示生产者（`chat_request.go` 与 `scheduler.go`）都必须调用，默认称谓 `User` 视为占位不注入）；**模型提供商**（`providers.go` + `provider_config.go`：独立 `providers` 表存端点与凭据，密钥只写不回显；按「后端 × 协议」注入环境变量，Codex 走托管配置文件块 + 备份/解绑还原；摘要模型改为引用 providers 行而不是自己存一份端点）；**通知中心**（`notifications.go` 的 `StoreNotification`/`ListNotifications`/`ClearNotifications`/`CleanupNotifications`——单表 `notifications` 为唯一数据源，**只追加日志、无已读/无角标**，五条写入路径（会话终态、任务事件、`ForgeNotificationRecorder` sink、用量提醒、会议事件 `EmitMeetingEvent`）**不复用 `pending_events` 门控**，`created_at` 亚秒精度供 `(created_at,id)` 复合游标分页，30 天/500 条双上限；单聊自动巡航 `cruise.go` 的 `NewCruiseRunner` 与 `auto_continue.go` 对称、共用 drain 循环；**用量提醒** `usage_meter.go` + `usage_meter_worker.go`——一条全局规则（**独立 `enabled` 开关**；起始时间/周期/额度全部可选，服务端按已配置内容做最大程度展示，缺额度只统计不设预算、缺周期则窗口永不滚动——**不再补 30 天**，那会让累计值每月静默归零；周期可为固定天数或自然月，后者按起始时刻的日号每月滚动、分母取当月实际天数），按线性匀速算基准线，越线与用尽各提醒一次且**只提醒不拦截**（缺额度或缺周期即完全不发——「超出计划」是关于基准的陈述），`usage_meter_update`（该提醒了）与 `usage_meter_tick`（数字动了、只刷胶囊）刻意是两个事件）；最近项目分组（`recent_project_groups.go` + `repo_layout.go`，纯文件系统识别主仓库/工作树/子目录）；含 SessionCleanupWorker / BingWallpaperWorker / ForgePoller（forge 变化轮询）、桌面端升级检查（`desktop_upgrade.go`）等后台 worker |
| `internal/ai/` + `backends/` | AI 后端抽象：`AIBackend` → `CLIBackend`（CLI+行解析）或 `ACPBackend`（JSON-RPC over stdio）；20 个后端子包（Qoder 国际版 / 国内版是两个独立发行包，共用一份 argv 构造器）；CLI/ACP 均支持无进度看门狗（ACP 侧按**模型进展**判定，与连接活性双信号分离）；`compact_detect.go` 识别各后端上下文压缩信号（压缩后下一轮重注入系统提示；精确信号 = 工具调用/`_meta`/协议事件/adapter 自己的完成通知文本，兜底 = usage 占用塌陷 ≥60%；模型在回复里自称「已压缩」的散文不算数）；**不做帧级内容去重**（wire 上「重试」与「模型又输出一遍相同文本」字节相同，三代文本判据全部误删真实内容，已整体移除；重复只在 CodeBuddy team 成员帧 + ≥5 秒写入停顿时出现，详见 `docs/dev/acp_dup_chunk_port.md`）；**运行中插话**（`midturn.go` 的 `MidTurnInjector` 按后端分列：CodeBuddy `session/steer` 可切分回复 / Claude·Codex `_session/steering` 只能合并——能力位 `MidTurnSplitsReply` 决定前端是否标「回复已并入上一轮」；注入走裸 `CallRaw` 通道与 SDK 共用 lockedWriter，且必须先过 `PromptInFlight()` 防 Codex 幽灵轮）；**点选切模型**（`ACPPlugin.ModelSwitchMethod` + `acp_model_switch.go`：Hermes 收 `set_config_option(model)` 只存不读，真正认 `session/set_model`）；**后端专属 ACP 约束用插件能力位表达**（`ACPResumeIsProcessLocal` 恢复走 `session/load`、`ModesMirrorThoughtLevel` 丢弃重复的 modes、`ToolCallWrapperTitlePrefixes` 吞掉插件/MCP 的包装帧——OpenClaw 的 `acp` 是到 Gateway 的桥而非 agent，四条约束各对应一位）；ACP 客户端能力按 agent 定制（CodeBuddy 隐藏 Terminal / `fs.readTextFile`——声明能力会替换其原生工具实现）；**MCP 注入**（`acp_mcp_inject.go` + `acp_conn_lifecycle.go`：**四条建会话路径**（new/load/resume/SyncLoadSession）都必须带 `mcpServers`，漏一条表现为「进程重启后工具消失」；每轮前比对策略哈希，变了就带新列表重连而不打断运行中的回合；哈希**不含能力位**，否则每个新进程都要多断连一次；`--mcp-config` 仅在全局列表为空时作为 CodeBuddy 老用户兼容路径保留）；`keepUnlistedModel` 不覆盖 agent 未广告的模型，避免 BYOK 被静默重路由；ACP 连接缓存状态用 leaf lock `stateMu`（绝不在 RPC 期间持有），避免通知回调与在飞 RPC 互相等待致通知队列溢出杀连接 |
| `internal/askquestion/` | `<clawbench-ask-question>` 载荷解析的**唯一** Go 实现（叶子包，不 import 任何 internal 包）；与前端 `web/src/utils/askQuestion.ts` 互为镜像，由 `testdata/parity_corpus.json` 双向固化。契约：检测即解析；不可解析时剥离标签、把标签内文字作为 Fallback 交给 Markdown 渲染（`Match.Fallback`），**绝不丢内容**。旧 XML 子元素格式与标签内 JSON 已不再解析 |
| `internal/grouprouting/` | 群聊路由标签（`<clawbench-mention targets="A,B">…</clawbench-mention>` + `<clawbench-group-end/>`）解析的**唯一** Go 实现（叶子包，不 import 任何 internal 包）；与前端 `web/src/utils/groupRouting.ts` 互为镜像，由 `testdata/parity_corpus.json` 双向固化。契约与 askquestion 一致（检测即解析、不可解析不剥离、绝不丢内容）。密送（`private` 提及）的注入侧 fail-closed（`StripProtocolTags` 剥一切畸形/嵌套形态）与显示侧 fail-open 分列两套 |
| `internal/model/` | 数据模型、后端注册表、模型发现（`ModelSource` 注册表 + 单一合并点 `ResolveModels`）、27 个 LLM Provider；Agent 含 `avatar`（用户自选的 DiceBear SVG）、`disabled`（禁用开关）与 `project_id`（「仅当前项目」作用域）字段，都贯穿 DB 列与两条持久化路径（`LoadAgentsFromDB`/`loadAgentsFromDBRows` 读、`SaveAgent`/`saveAgentToDB` 写）；`SortAgentsByOrder`（`sort_order` 升序 + `id` 决胜）是 DB 加载与 reorder 共用的唯一排序规则；`ThinkingEffortByModel`（`agents.thinking_effort_by_model`）按模型 id 记忆用户显式选择的思考档位；真正解析一轮用哪个档位的是 `service.EffectiveThinkingEffortForModel`（`chat_request.go` / `scheduler.go` / 会话切模型共用），优先级 = 该模型的选择 > 全局默认（**但全局默认须在该模型的支持列表里**，否则留空交给 agent 自决）；agent 自报的档位只用于显示；**`CustomACPBackend = "custom-acp"` 刻意不在 `BackendRegistry` 里**（用户自定义 ACP 智能体的 backend）——`RefreshAgents` 的 `syncAgentInfrastructure` 按 backend 重写 `acp_command`，进了注册表用户的命令会在下次 rescan/启动时被静默覆盖；`BackendHasSpec` 是运行期字段（加载时由 `FindSpecByBackend != nil` 算出），前端据此决定 ACP 命令行是输入框还是只读，与 PATCH 守卫同一判据；**MCP**：全局定义在 `config.yaml` 的 `mcp.servers`（`mcp.go`），智能体只存策略 `agents.mcp_servers`（`all`/`none`/`selected`），`ResolveMCPServers(agentID, caps)` 做「策略 → 后端黑名单（dsh/deepseek/pi）→ 能力位（http/sse）」三层过滤；**必须传 agentID 而非 `*Agent`**——`ACPConn` 持有的是建连时的一次性快照，读它会让策略变更对已有连接永久不可见 |
| `internal/speech/` + `internal/stt/` + `internal/audio/` | 语音：TTS（Edge / OpenAI 兼容）与 STT（vLLM Whisper / 豆包 Seed-ASR，流式 + 非流式）；`internal/audio` 是纯 Go 的 MP3 解码 + 下混。**嵌入式设备的两条一体化 WS**（都边合成边推无容器头的 16-bit 单声道 PCM，采样率随源音频）：`WS /api/tts/speech/ws` 只给 `messageId`（摘要走服务端自己的管线，先单独发一帧文字再推音频，`type=say` 还能一步完成发消息→生成→朗读）与 `WS /api/voice/turn/ws`（识别 → RunTurn → 摘要 → 推送全在同一条连接上，**一条连接可跑多轮**）。另保留同步的 `POST /api/tts/generate/pcm`。拆成两条的原因不是功能划分而是**每轮只做一次 TLS 握手**：设备在第一条连接刚关闭时常常申请不到第二份加密上下文（`MBEDTLS_SSL_SETUP_FAILED`） |
| `internal/rag/` | RAG：SQLite + sqlite-vec 向量存储 + FTS5 全文检索，OpenAI 兼容嵌入 API；消息聚类（ClusterWorker） |
| `internal/terminal/` | Web 终端：PTY 会话、环形缓冲回放、多标签 |
| `internal/ws/` | WebSocket 事件通道：StreamHub 会话级扇出，Manager 广播 + 重连缓冲回放；`delivery_stats.go` 记录按原因的投递丢弃计数（`GET /api/ws/delivery-stats`），关键事件在通道满时等待空位（ACP 来源除外）。遥测类事件（`system_resources`）走**非缓冲投递**路径（不写回放缓冲、队列满时丢弃而非断连），需求由客户端 `metrics_preference` 声明 |
| `internal/ssh/` + `internal/proxy/` | SSH 隧道服务器；HTTP 反向代理 + 端口转发。反向映射仅绑 127.0.0.1 且禁绑保留端口 |
| `internal/tunnel/` | h2 流隧道的传输无关内核（`/api/tunnel/stream` 数据面 + `/api/tunnel/control` 反向控制面）：`bind.go`（端口分配 + 白名单/保留端口）、`guard.go`（`PortGuard` 镜像 `internal/ssh` 的端口策略，保证 h2 与 SSH 同一套规则）、`claim.go`（`-R` 的单次 token 认领）、`ndjson.go`（控制流分帧）、`relay.go`（双向转发 + 半关闭）、`target.go`（目标地址解析） |
| `internal/forge/` | GitHub/GitLab 集成：平台无关的只读 `Provider` 抽象（统一 Issue/PR/Comment/Pipeline 模型）+ `github/`（go-github）/ `gitlab/`（轻量 REST client）adapter；remote URL 解析（host 与 scheme 分离解析）、per-host 令牌桶限流、事件推导引擎；**服务端总开关 `forge.enabled`（默认开）**——关闭即停轮询、不再自动绑定、并清空全部项目的仓库绑定且打 opt-out 标记（重新启用不自动恢复），关的是出站流量与凭据故必须服务端一致而非每设备偏好。**无 host 安全闸门**（内网/自建实例一律放行，风险提示在前端绑定弹窗） |
| `internal/push/` | IM 机器人推送：`common/`（共享接口 + 会话命令）、`dingtalk/`（Stream API）、`feishu/`（Lark SDK WebSocket + 互动卡片）、`wechat/`（个人微信，经 openilink SDK 长轮询；**扫码登录、没有静态密钥**——出站消息必须带只有用户先给机器人发消息才下发的 `context_token`，因此「登录成功」不等于「推送可达」，面板要分开表达） |
| `internal/mail/` | 内置邮件客户端的协议层（不认识 DB 与 HTTP）：连接、`LIST`/`FETCH`/`STORE`/`MOVE`/`APPEND`、MIME 解析、发信、错误归类（auth/connect/tls/timeout/server）。`Client` 是接口、`Dialer` 可替换，故 service 层用内存假客户端测、协议层用内存 IMAP 服务器测。持久化与编排在 `internal/service/mail_*.go`（账号、3 分钟增量同步 worker、按需拉正文、发信、`mail_task_trigger.go` 邮件触发任务去重），端点在 `internal/handler/mail.go`；**服务端总开关 `mail.enabled`**（关闭即停同步与触发任务、前端隐藏页签）。信头抓完整 `HEADER` 而非 `ENVELOPE`——QQ 的 ENVELOPE 不合 RFC 9051 文法，会让整条连接作废、一封坏信拖垮整个账号 |
| `internal/symbol/` | 基于 tree-sitter 的代码符号提取（纯 Go，无 CGO） |
| `internal/skill/` | 后端无关的跨智能体 Skill 发现框架（Skill = 含 `SKILL.md` 的目录）。只依赖 `internal/model`（不得 import `internal/ai`/`backends`，否则成环）：`scanner.go` 递归扫描（有界深度、跳过 `.git`/`node_modules` 等）、`registry.go` 按 `SourceKind`（本智能体原生 > 项目 `.agents/skills` > 用户目录 > git > 其他原生）去重、`git.go`+`worker.go` clone/pull git 源（单例 worker，启动 + 定时 + 手动 `POST /api/skills/refresh` 三触发）。**项目级来源**：按会话项目根扫 `<projectPath>/.agents/skills`（`InjectedForProject` / `AllForProject`，`GET /api/skills` 按项目 Cookie 只列当前项目；qoder/codewhale 在 cwd=项目根时也读它，故总是注入）。**本地目录新增走 PATCH 同步重扫、Git 仓库新增由前端在 addRepo 成功后自动调一次 refresh**（PATCH 只落盘不联网，避免在 config 写锁内做网络 IO）；`POST /api/skills/rescan` 是**纯本地重扫**（不联网、不写同步状态）。系统提示词注入在 `service.AppendSkillsSection`，**两个 `ai.ChatRequest.SystemPrompt` 生产者都必须调用**（`chat_request.go` 与 `scheduler.go`）。`AutoLoadsNativeSkills` 决定后端是否自加载（codebuddy=false 必须注入；其余多为 true），取值由 `internal/ai/backends/native_skills_test.go` 双向表钉住 |
| `internal/liveshare/` | 屏幕/声音共享的房间模型与发言授权状态机。**只依赖标准库**（不 import 任何 internal 包、不 import websocket），以便整体抽离为独立中继应用：连接用 `Sink` 接口表示而非 `*websocket.Conn`。`room.go` 房间注册表（按 token 与 projectKey 双索引、幂等创建、**按模式设上限**（P2P 8 人 / 中继无上限）、空闲回收）+ `talk.go` 发言状态机（只有 `none ↔ talking` 两态：发布端一个房间级开关（默认关）+ 每个观众自己的麦克风开关，**刻意没有逐人审批**——房间开关本身就是安全边界；非法转移一律返回错误而非静默忽略）+ `signal.go` 协议结构体（与前端镜像，含**成员反向共享屏幕**的房间开关与 `screen-state`/`screen-denied`：一次只允许一人，转发成本随共享者数量平方增长）+ `livekit.go` 中继（HS256 自签 token + 管理接口，**仍只用标准库**，因此这个包能整体抽走）。**媒体不经过服务端**：handler 只转发 SDP/ICE；中继模式下媒体交给独立部署的 LiveKit，服务端只签发 token、管房间、同步禁言。HTTP/WS 绑定在 `internal/handler/live_share.go`（+ `live_sfu.go` 配置端点、`live_relay.go` 权限同步、`live_proxy.go` 仅回环的 `/livekit/*` 反代）；**AI 听记与会议记录**在 `internal/service/live_notes.go`（`live_notes` 表一行 = 一场会，发布端连上信令即创建、断开即收尾，与「录没录到东西」无关；按信道落库 + 按 `startedAt` 重排 + 纪要 map-reduce；会议结束后把逐信道录音片段**重封装**成整场 + 每信道各一个文件，转写存 WebVTT（cue id 承载信道、`<v 称谓>` 承载显示名）；未命名会议先以开始时间占位，纪要生成后由摘要模型改名，**只替换占位名**），`live_notes_artifacts.go` / `live_notes_regen.go`（三个产物各有内容轴与任务轴，可单独重新生成、向上/向下级联、DB 事务抢占单任务、10 分钟无心跳判中断；转写「重新生成」= 重跑 ASR，`live_notes_audio_split.go` 按 Cluster 切片），`live_notes_shots.go`（关键画面：配额、落库），上传端点在 `internal/handler/live_notes.go`，采集只能在发布端——纯 P2P 下服务端收不到音频，而发布端是唯一保留信道分离的地方 |
| `internal/mcp/` | MCP 探针客户端（**不是**通用 MCP 实现）：只做「握手 + 列工具」这一步，用来回答「这条 MCP 服务真的能用吗」。刻意不做 socket 连通性检查——命令能启动但说错协议、URL 返回 200 却是 HTML、服务接了连接永不回复，这些只有真握手能暴露。**stdio 探针必须能自己收场**：launcher（uvx/npx/pipx）把服务作为子进程启动，子进程继承探针的管道，只杀 launcher 会让 `cmd.Wait` 永久阻塞（实测握手成功却永不返回、界面 15s 超时）；故独立进程组 + 整组 SIGKILL（`proc_unix.go`/`proc_windows.go`）+ `cmd.WaitDelay` 兜底 |
| `internal/summarize/` | 摘要与推荐的底层引擎（多后端 provider、多 pass 压缩、`StripMarkdown`、`RecommendNextStep`） |
| `internal/system/` | 系统资源监控：CPU / 内存 / 磁盘 / 网络实时采集与推送 |
| `internal/cli/` | AI Agent 自助命令：仅剩 upgrade-replace（自升级内部机制）；task/rag 业务子命令已移除，改由 `/cb-*` 内置斜杠命令直调 HTTP API |
| `internal/middleware/` | 鉴权、请求日志、panic 恢复、请求 ID |
| `internal/platform/` | 跨平台路径解析、Shell 检测、二进制替换原语（`ReplaceBinary`，升级路径共用） |

### 前端（Vue 3 + TypeScript）

源码根：`web/src/`。无 Vue Router，基于抽屉的单页布局。单一 `reactive()` store (`stores/app.ts`)。**同一时刻只允许一个标签页跑应用**（`useSingleTab.ts` 选举，第二标签页只显示阻塞屏、不挂载应用）：服务端按 `localStorage` 里的 `client_id` 键控 WS 订阅槽，同源所有标签页共用同一个 id，两个标签页会互相顶掉 socket 并各自重连，实测 17 分钟 1402 次 subscribe、约 2900 请求/分钟。新增全局唯一资源的消费者前先确认是否也受此约束。选举优先用 **Web Locks**（`navigator.locks` 单一独占锁；锁在上下文消亡时由浏览器自动释放，故 owner 崩溃/关闭/导航都不会留下"幽灵占用"永久阻塞等待方），不支持时回退 **BroadcastChannel**（jsdom、老浏览器/WebView）。owner 在**每次** `pagehide` 都释放槽位（含 bfcache 进入——冻结页无法运行代码，持锁会阻塞等待方最长 ~10 分钟），`pageshow` 时重新认领。

Composable 与组件均按域分组（Chat、Session、Terminal、File、Git、Meeting/Live、Mail、Navigation/Gesture、Settings、Agent、Task、Infrastructure、System）。新建 composable 须放 `web/src/composables/` 并以 `useXxx` 命名，测试用 `*.test.ts` 同目录或 `__tests__/`。

**邮件面板**（`components/mail/`）按宽度自适应：窄时列表 → 阅读 / 写信逐层下钻，宽时列表与右侧内容用共享 `SplitView` 并排（比例存布局记忆 `mailSplitRatio`）。写邮件是面板内的一个视图而不是弹窗，返回手势按来源分派（回复回那封信、新建回列表）。`mail.enabled` 关闭时页签从 Dock 注册表里消失，程序化跳转也要一并拒绝。

**界面缩放只有一个机制：根字号**（`utils/uiScale.ts` 写 `--ui-scale`，`base.css` 乘进 `html { font-size }`，所有几何尺寸用 `--dp` = `1rem / 15` 表达）。CSS `zoom` 与 Electron 原生 `setZoomFactor` 都已废弃——它们不缩放布局视口，会让 `100vw`/`dvh`/媒体查询/ResizeObserver 全部失真。三条必守：几何尺寸写 `calc(N * var(--dp))` 而非裸 px（发丝线保持字面 `1px`）；**同时出现在 CSS 与 JS 里的数字必须在 JS 侧乘 `uiScale`**（`getBoundingClientRect()` 返回已缩放值，直接比较会算错「放得下几个」）；画进 canvas 的表面（xterm、图表）读不到 CSS 变量，得自己乘，且**存储值保持设计值**。缩放变化不触发 `resize`，靠 `UI_SCALE_EVENT` 广播，宽屏断点用有效宽度 `innerWidth / uiScale`。

`web/src/utils/askQuestion.ts` 与 Go 的 `internal/askquestion` 互为镜像（共享语料 `internal/askquestion/testdata/parity_corpus.json` 双向固化）——改一侧必须同步另一侧，否则同一段文本会在前后端得到不同解析。`web/src/utils/groupRouting.ts` 与 Go 的 `internal/grouprouting` 同样互为镜像（群聊 @ 提及 / 结束标签 / 密送，共享 parity 语料）。

**群聊前端**：`useGroupChat.ts`（群回合状态、路由卡片渲染）+ `useGroupMembers.ts`（成员花名册与 `isGroupSession` 判定，用后端 `sessionType === 'group'` 而非"名单非空"）+ `GroupSettingsSheet.vue`（群聊设置抽屉：成员管理 + 群设置）、`GroupAvatarStack.vue` / `GroupMemberStack.vue`（头像堆叠，主持人排最前）、`SessionGroupHeader.vue`。建群入口是会话列表头的 `+`，复用 `AgentSelectorDrawer` 的多选模式（同一智能体可用步进器 `− N +` 加多份，每个实例是独立成员，显示名取 `chat_sessions.agent_name`、头像取 `agent_avatar`）。**「单聊 / 群聊」分段开关的门槛是「至少一个可选智能体」而非「≥2 个不同智能体」**——重复实例本就是独立成员，只装一个智能体也能靠步进器建两实例群；只有列表为空才隐藏。

**会议前端**：左栏 Dock 页签 `live`（标题「视频会议」，面板内标题「会议」）由 `components/live/MeetingPanel.vue` 做五视图状态机（记录列表 / 创建 / 加入 / 会议中 / 记录详情），其余组件按视图拆在 `components/live/`（`MeetingList` / `MeetingCreate` / `MeetingJoin` / `MeetingStage` / `MeetingDetail` / `MeetingResources` / `MeetingParticipants`，以及共用的 `ChatMessageRow` / `TranscriptList` / `DeviceChoiceGrid` / `CaptureSourceGrid` / `MemberScreenSurface`）。**会议中四个页签**（会议管理 / 参会者 / 转写 / 聊天）共用一条常驻底部动作条（结束会议 + 标注画面）；画面、摄像头、麦克风都是**设备卡片**而不是勾选框，会中换设备不重协商。状态与数据在 `composables/`：`useLiveShare.ts`（发布端房间、采集、每观众 PeerConnection，画面可随时开关且只 `replaceTrack`）、`liveNotes.ts` + `useLiveNotes.ts`（分信道录音与上传，纯逻辑部分可单测）、`useMeetingHistory.ts`（记录列表 / 单条 / 产物 / 聊天 / 删除 / 引用载荷）、`useMeetingResources.ts`（资源页签的文件清单）、`useMeetingViewState.ts`（返回时恢复详情页签）、`useMeetingNickname.ts`（称谓复用账号称谓并可改）、`useLiveRelay.ts`（中继模式的发布 / 订阅 / data channel）、`useLiveSFUConfig.ts`（中继配置与「保存并测试连接」）、`useMeetingShots.ts`（关键画面抓帧、dHash 去重、配额）、`useWakeLock.ts`（会议期间保持屏幕常亮）。**会议没有类型**：创建页是一组独立勾选（屏幕 / 摄像头 / 麦克风 / 听记 / 关键画面 / 中继），全不勾也允许开会；`picture-state` 的 `video` / `camera` / `hostScreen` 三个正交标志决定观众占位与桌面类能力（标注、切源、远程控制）是否可用。会议记录可拖进聊天作引用，引用条与 issue/PR 详情共用同一套 composer 模式（先预览 chip、填批注后提交才落卡片）。

**会话间阅读进度**：`utils/chatScrollMemory.ts`（内存 Map，按会话记 `scrollTop` + 消息锚点 + 比例）。契约有三条互相牵制：停在底部离开的不记（回来该看最新）、翻着离开的记且三元组一起恢复（历史未加载完也能落对）、**只有静止会话才恢复**（运行中的回来仍落底部，否则把新输出藏在半中间）。切会话会重建列表 DOM（`listKey` 含会话 id），浏览器原生 `scrollTop` 随之丢失，这就是它存在的理由。

**连续对话（一直听）**：`composables/useFreeDialog.ts` 跑 `listening / thinking / speaking` 三态循环——一直开麦 → 句尾说到触发词或停顿够久就自动发送 → 回答朗读完再等 0.5 秒开麦（等喇叭尾音散掉）→ 如此往复。发送与朗读期间麦克风是关的；打断靠**临时开麦**判定，识别结果与正在朗读的文本大量重合时按回声丢弃（否则它会自己打断自己）。`utils/audioTuning.ts` 是**叶子模块**，麦克风约束 / 灵敏度门限 / 触发词与打断词解析 / 回声比对都在这里，`useVoiceInput`、`useFreeDialog`、会议听记共用同一份解析（会议**不套频段过滤**，否则观众听到的是电话音质）；`utils/pcmCapture.ts` 把 16 kHz PCM 采集图（频段过滤 → AudioWorklet → 静音门限）抽成一处。动作栏按钮四态各一个图标（待机是对话气泡而非 `MicOff`——带斜杠的麦克风图形本身就是「麦克风已关闭」，与按下它要做的事相反）。**进入模式时自动打开的朗读，退出时按进入前的状态还原**：用户中途自己关掉的不会被重新打开。

**一排图标按钮只有一个实现**：`components/common/ActionBar.vue` 统一管「空间够就带标签、放不下就收进『更多』菜单（`mode="menu"`）或左右滚动（`mode="scroll"`）」，宽度是量出来的不估算。接入方式、`mode` 语义、临界宽度不闪的两条前提、以及已接入清单见 [`docs/spec/client/design-guide.md`](docs/spec/client/design-guide.md)。新增工具栏不要再自己写一套测量逻辑。

宽屏 Dock 页签定义在 `web/src/composables/dockTabs.ts`（单一注册表，渲染集合与切换白名单都从它派生），图标单独放 `dockTabMeta.ts`。`dockTabs.ts` 必须保持零 import（`useWideScreenLayout` 依赖它，而多个测试文件对 `lucide-vue-next` 做了窄 mock）。**forge 页签的标题按项目绑定的平台动态取**（`useForgeBinding` 的 `forgeTabTitleKey` → 「GitHub 集成 / GitLab 集成」，未绑定默认 GitHub），且必须与 `forgeDockIconKind()` 共用同一判定——图标与文案各判一次就会出现「GitLab 图标配 GitHub 文案」。三个标题入口（窄屏溢出菜单项、窄屏内联按钮 tooltip、宽屏竖向 Dock tooltip）都要走它，溢出按钮的 tooltip 尤其不能直接读注册表的中性标题。**所有面板/布局状态**（左栏折叠、聊天区折叠、分割比例、会话侧栏、TOC 停靠栏、文件管理器预览比例、左栏页签）统一登记在 `web/src/utils/panelState.ts`（叶子模块）：id / 键 / scope / 默认值 / 编解码一处声明，读写走 `readPanelState`/`writePanelState`，键名统一 `clawbench-panel.<名>`（按项目项追加 `:<项目根>`），并有一次性老键迁移。**除左栏页签（`leftTab`）按项目外，其余全为全局**；左栏页签由 `useProjectPanel.ts` 记忆；项目切换期间用计数器抑制误写（可并发调用，布尔会被先结束者清掉）。`switchLeftTab` **不隐式展开左栏**（它是程序性恢复路径），用户点页签的入口显式展开。

`web/vendor-build/excalidraw/` 是独立的 Excalidraw 编辑器构建（React），由 `build.sh` 单独构建到 `.clawbench-web/vendor/excalidraw/`，`.excalidraw` 文件通过 iframe 懒加载，Vue 主包不含 React 依赖。它**有意不进根 Vite 构建**（否则主 bundle 会膨胀约 8MB），因此根 `npm run build` 不产出它——**所有 CI / release job 都必须显式构建该 vendor bundle**，否则发布二进制内嵌的前端里没有 `vendor/excalidraw/`，`/vendor/excalidraw/index.html` 走 `ServeIndex` 的 `http.NotFound` 返回 Go 的 "404 page not found"（本地用 `build.sh` 构建正常，缺陷只在 release / Docker 产物上暴露）。

`web/src/share/` 是分享链接的独立只读 SPA（文件分享=类型分派渲染 + TOC + 下载；会话分享=快照对话 + 目录导航 + 导出 JSON），由 vite 多入口构建为 `share.html`，服务端在 `/share/{token}` 无鉴权公开（token 即凭证）。

`web/landing/` 是**项目主页**（单页营销站，纯静态 HTML/CSS/JS + 中英双语切换），同样**不进根 Vite 构建**，也不由 `build.sh` 产出——它由 `.github/workflows/deploy-landing.yml` 在推送 `main` 时用 wrangler 直传 Cloudflare Pages（`_headers` 声明安全头与缓存策略：`/img/*` 一年 immutable，因此**图片只换新文件名、不原地改**；`index.html` 不缓存）。**不用 Cloudflare 的 Git 集成**：其构建器克隆后会无条件跑 `git submodule update --init --recursive`，而本仓库根没有 `.gitmodules`，任何 gitlink 都会让它在构建开始前就失败。同一目录另由 `.github/workflows/deploy-landing-pages.yml` 发布到 GitHub Pages 项目页（`https://clawbench-dev.github.io/clawbench/`）：上传预构建产物，因此**不带 `_headers`**（Pages 无此语法，会被当成普通文件下发）并补 `.nojekyll`；仓库需一次性把 Settings → Pages 的 Source 设为 GitHub Actions，否则 deploy job 报 "Get Pages site failed"。页面里的下载按钮写死 `href` 为 Releases 页，脚本再按 `api.github.com/.../releases/latest` 的 `tag_name` 改写成直链——资产名带版本号，不存在 `/releases/latest/download/<name>` 这种稳定地址，取不到 tag 时保留回落链接。`web/landing/landing.test.ts` 覆盖翻译残留、图片 404、灯箱与画廊循环、下载资产名与 `release.yml` 的一致性——改页面后跑 `npx vitest run web/landing/landing.test.ts`。

**用户手册站点**（`/docs/`）由 `web/docs/build.mjs` 从 `docs/user-guide/*.md` 生成，`npm run docs` 输出到 `web/landing/docs/`（已 gitignore），两个部署工作流则 `--out dist/docs`。三条不变量：①**样式不复制**——页面只引用 `../css/landing.css` + 自己的 `docs.css`，落地页换主题手册自动跟随；②**锚点与 GitHub 一致**（`slugify` 去标点/转小写/空格换 `-`），手册里 `settings.md#用量`、`#54-用户称谓` 这类深链靠它才不落空，改算法等于废掉已发布链接；③**侧栏目录取自 `docs/user-guide/README.md` 的结构**，不另立清单。渲染用 `marked` + `highlight.js`（与前端应用内同一套），mermaid 只在真有流程图时随包带 3MB 脚本。新增/重命名手册文件不需要改任何清单，重跑构建即可；`web/docs/build.test.ts` 会校验「每个 `.md` 都成页、站内链接与锚点全部可达、引用的截图都被拷出、页面都引了 landing.css」——改渲染器后跑 `npx vitest run web/docs/build.test.ts`。站点还带**页内目录**（h2/h3）、左侧章节筛选框（`/` 聚焦）与「上一篇 / 下一篇」翻页，三者都从同一份解析结果派生，不另立清单。

### 桌面端（Electron）

源码根：`desktop/src/main/`。桌面端是纯"壳"，复用服务器 + Web 前端全部业务逻辑，仅提供 Web 环境之外的桌面能力（尤其是**窗口最小化/隐藏时仍能弹系统通知**——浏览器标签被冻结时页面内 `Notification` 不会触发）。主进程模块通过 IPC（`native:*`）暴露为 `window.ClawBenchNative`，与 Android WebView 共用同一套前端接口（`web/src/utils/clawbenchNative.ts`）：

| 模块 | 职责 |
|------|------|
| `window.ts` | 主窗口创建（Windows/Linux 无边框，macOS 原生帧）、最小化/最大化/关闭三个自绘控制的 IPC、原生上下文菜单（cut/copy/paste 走 OS role，copy-link/copy-image 按语言翻译）、外部链接拦截交给默认浏览器 |
| `windowChrome.ts` | 无边框窗口的平台判定（纯模块、无 electron import、可单测）：Windows/Linux 自绘控制簇，macOS 保留原生交通灯，未知平台回退原生帧 |
| `splash.ts` | 登录/启动加载屏（对齐 Android splash）：页面加载成功后才淡出；复用页面时必须清掉上一次的淡出类，且加载成功要取消连接超时 |
| `bridge.ts` | IPC 桥：服务器列表/凭据、SSH 端口映射、文件下载、分享、系统通知、主题、语言、日志捕获、屏幕常亮 |
| `capture.ts` | 屏幕/窗口捕获：枚举捕获源 + `setDisplayMediaRequestHandler`（Electron 里 `getDisplayMedia` **不会自己弹选择框**）。**声明的源是一次性的**（`set-pending-capture-source` 消费后立即清空，否则任何页面后续都能静默抓屏）；`get-audio-capture-support` 只在 Windows 报 `systemAudio: true`；共享期间 `set-publishing-active(true)` 关掉窗口后台节流——最小化后 Chromium 降频定时器会让编码器停摆、所有观众画面冻结 |
| `floating.ts` / `floatingPolicy.ts` | 桌面悬浮状态胶囊（Android `FloatingStatusView` 的桌面对应物，两者共用设置里的同一个开关）。计数由主进程自己轮询 `/api/ai/sessions/overview` 算出（与手机端同一口径、网页无需配合），托盘图标与胶囊共享同一份数字。**拖动由主进程驱动而非 `-webkit-app-region: drag`**——后者把拖拽区交给 OS 当标题栏，右键会弹系统菜单、DOM 永远收不到 click（"点一下打开应用"就没了）。所有不依赖 Electron 的判定（三计数、贴边、配色、点击 vs 拖拽）都在 `floatingPolicy.ts` 里单测 |
| `tunnel.ts` | ssh2 客户端，读取 `/api/ssh/info` 建立 SSH 端口映射 |
| `download.ts` | 文件下载（保存对话框 + 下载后定位）、URL/Blob 下载。主进程无 `document.cookie`，故 `fetchToFile` 经 `clientLog.getCookieHeaderForUrl(url)` 从 Electron cookie jar 取**全部**适用 Cookie（只取 session 名会缺 `clawbench_project` → 403），并设 `rejectUnauthorized:false`（自签名证书，与 tunnel/clientLog/h2Transport 一致） |
| `notification.ts` | 原生系统通知，点击导航到会话/任务/仓库（冷启动挂起派发，经 `navReady` 的 `rendererReady()` 握手后才放行）。窗口**可见且未最小化**时**抑制通知**（刻意不看焦点：窗口开着就不打扰）——用户开着应用，通知只会重复应用内完成卡片；判定必须在主进程做，渲染层的 `document.hasFocus()` 在最小化/隐藏窗口里仍可能为真。**已读消除**：保留 Notification 句柄并按 subject 键控，`dismissTerminalNotification` + `native:dismiss-notification` IPC 供前端在收到 `read` 事件时撤掉仍挂在通知栏的旧通知（否则用户手动打开 App 读消息后，旧通知再点会重复派发深链） |
| `clientLog.ts` | 主进程日志回传：缓冲 POST `/api/client-log`（`source="electron"`）+ 写 `{userData}/desktop.log`；镜像渲染进程 console，`recordError` 上报未捕获异常 |
| `identity.ts` | `APP_USER_MODEL_ID`（Windows toast 身份），**必须与 `electron-builder.yml` 的 `appId` 一致**——该 yml 不随包分发，运行时读不到，漂移会让 Windows 通知静默消失；`identity.test.ts` 守住。Linux 任务栏图标另依赖 `desktop/package.json` 的 `desktopName`（决定窗口 `app_id`），**不能改 `productName`**——那会挪动 userData 目录、丢用户配置 |
| `urlPolicy.ts` / `contextMenu.ts` | 外部链接判定（以服务器 Origin 为边界）与原生右键菜单（标准项走 OS role 本地化） |
| `navReady.ts` / `session.ts` | 渲染进程就绪握手、会话缓存强刷 |
| `shortcuts.ts` | 应用级快捷键决策表（`before-input-event` 只认领 Ctrl+Shift+R / F12，F5 等一律放行给页面），不 import electron 便于单测 |
| `updater.ts` | 升级检查：请求**服务端** `/api/desktop/latest`（不查 npm）；语义化版本比较，降级不误报 |
| `versionGate.ts` / `shared/protocol.ts` | **客户端兼容性门禁比协议号、不比版本字符串**：`internal/version/client_protocol.txt` 是唯一来源（只在壳与服务端的破坏性契约变更时 +1），Go embed 后由 `/api/health` 与 `/api/desktop/latest` 下发 `protocol`；桌面端镜像到 `shared/protocol.ts`（对拍测试防漂移）。`shouldGateClient` 相等即放行，任一侧缺字段退回版本比较（老客户端单向升级路径）。`release.yml` 的 `client-guard` job 据此决定「编还是搬」：`android/`、`desktop/` 都没动就复用上一个 Release 的产物，协议号 +1 或上个 Release 不认识协议号则两端强制重编，契约文件改了但协议号没变直接失败 |
| `install.ts` | 自升级安装：多候选 URL 依次降级下载 → SRI 校验 → 解压 zip（剥顶层包装目录、拒绝路径穿越、**恢复可执行位**）→ 侧装到 `~/.clawbench-desktop/app-<version>/` → 翻转 `current` 指针。另支持**增量载荷包**：安装到从当前版本克隆出的目录、复用 Electron 运行时，装前按主版本 + 壳指纹门控，不符即静默回退全量下载 |
| `secrets.ts` / `store.ts` | safeStorage 加密存密码（**按服务端分别存于各自 entry**，旧全局槽迁移时只归属当时的活动服务端）、electron-store 持久化服务器列表/主题/语言 |
| `powersave.ts` | 屏幕常亮（powerSaveBlocker） |

**自升级采用"侧装 + 指针"而非原地替换**：运行中的进程无法覆盖自身（Windows 上尤其如此）。`install.ts` 把新版本解压到独立目录并改写 `~/.clawbench-desktop/current`；**应用自己**在 `whenReady` 最前面读该指针决定运行哪个版本（`selectStartupVersion` + `handOffToPointedVersion`）——指针缺失、目标目录不存在或指向自身都清指针并继续用当前版本启动，因此失败可回滚、旧版本保留，被删坏或写坏的升级永远不会让应用打不开。必须由应用自己读：原先读指针的 npm 启动器已随 npm 渠道一并移除，双击 Release 解压出的旧 exe 时若无人读指针，会静默退回旧版。

**全量包以 GitHub Release 为准，载荷包只走 npm**：桌面端与服务端同版本发布，`/api/desktop/latest` 直接返回服务端自身版本 + 资产地址，**不查询任何外部服务**（不查 npm、不查 GitHub API）。每个平台返回**候选 URL 列表**（国内镜像优先、直连 github.com 兜底），客户端与前端都取首个可用项。

增量升级用的**载荷包**（应用自身，约 3MB）**只在 npm 发布**（`publish-npm-desktop`），不再作为 Release 资产——在 Release 资产里放一个 3MB 的 zip 会让人误以为它可直接安装（它缺 Electron 运行时，只能增量套用）。`payloads` 因此每平台只有 npm tarball 一个候选；macOS 无载荷（替换会破坏签名），该键缺席即走全量下载。

`tag` 为空表示当前是 dev/未打标签构建（无对应 Release），此时 `downloads` 为空、客户端隐藏下载入口——不要把它当错误处理。

**发布时客户端「编还是搬」由协议号决定**：APK 与桌面端都是薄壳（Web UI 由服务端下发），一次普通发布两者根本没变，却仍要编五个 job 产出字节相同的包。`release.yml` 的 `client-guard` job（`scripts/lib/client-guard.mjs`）比较上个 release tag 与本次：`android/`、`desktop/` 都没动就**下载上个 Release 的产物改名挂到新 Release**并作为同名 artifact 交给下游；**协议号 +1 或上个 Release 还不认识协议号时两端强制重编**（后者不强制会让老壳陷入「下载完仍不匹配」的死循环）；复用失败自动回退成真编译，不让 release 静默缺件。**契约文件（共享桥接接口、health handler、桌面 preload、Android 桥）改了但协议号没变 → release 直接失败**。桌面端 `/api/desktop/latest` 因此多一个 `clientVersion`：复用发布时它是被复用那个构建的版本，安装目录按它命名（用服务端版本命名会让 `app.getVersion()` 与指针版本不一致，启动交接随即删指针并静默回退升级）。

**桌面壳不是"手机 App 模式"**：两者都经原生桥被识别为原生环境，但省电策略相反——Android 退到后台会被系统挂起，故隐藏时主动断开 WebSocket；桌面窗口只是最小化、进程仍在跑，断开 WS 等于自断通知来源（通知全部由 WS 事件产生）。前端用 `isDesktopApp`（preload 注入 → `useAppMode` → 消费点三层打通）区分，门控写成 `isAppMode && !isDesktopApp`。任何一层漏掉都会让最小化后的推送静默失效。端口映射同理要按"期望状态"而非快照管理：重连后必须重建全部 listener，`ensureTunnel` 需单飞守卫（并发调用会互相拆台）。

**平台判定拆为三条正交轴，不再有 `isPC`**（`web/src/composables/usePlatformDetect.ts` + `useWideScreenLayout.ts`）：旧的 `isPC` 把三个无关问题 OR 在一起（宿主 OR 输入 OR 视口），每个边界都判错。现在消费方各取所需——**HOST**（`isElectron` / `isAndroidApp` / `isHarmonyApp` / `isWebApp` / `isNativeApp`，来自原生桥）、**INPUT**（`isTouchPrimary`，`(pointer: coarse)` 加 UA 兜底）、**VIEWPORT**（`isWideScreen`，独立模块）。Electron 是有物理键盘鼠标的桌面窗口，宿主轴直接判为 `isElectron`，不再靠"参与 `isPC`"来避免被误判成移动端。**鸿蒙是第四个宿主身份**（`getHostPlatform()==='harmonyos'`），`isAndroidApp` 显式排除它——安卓专属的字体/合成补偿、APK 更新、后台断连策略在鸿蒙上都不成立。`web/src/__tests__/platformAxes.test.ts` 钉住 `isPC` 不得回归、共享谓词不得重声明、iPadOS 陷阱（Macintosh UA + `maxTouchPoints>0`）不得重开。

构建与发布：`desktop/` 用 electron-builder 的 `dir` target 产出免安装目录，由 `release.yml` 的 `build-desktop-*` 四个 job（linux / linux-arm64 / windows / macos）打包为 zip 挂 GitHub Release（**只挂全量包**）。资产名必须与 `internal/service/desktop_upgrade.go` 的 `desktopAssetName` 完全一致，否则下载 404。`publish-npm-desktop` 发三个 `@xulongzhe/clawbench-desktop-<plat>-payload` 包（linux-x64 / linux-arm64 / win32-x64；darwin 从不发 npm）；`publish-npm` 只发服务端 CLI `@xulongzhe/clawbench`。CI 构建前需 `ELECTRON_MIRROR` 走镜像，否则 `@electron/get` 从 GitHub 下载常中断。`win.signAndEditExecutable` **不要**在 electron-builder.yml 里禁用——CI 的 windows job 原生可用 rcedit，禁用会跳过向 exe 写入图标与版本元数据；本地无 wine 交叉编译时才用命令行临时覆盖。

### 鸿蒙端（HarmonyOS，实验性）

`harmony/` 是 ArkTS / ArkUI Stage + ArkWeb 的原生壳，**不在 `release.yml` 里**（不进正式发布流水线），CI 由独立的 `harmonyos.yml` 跑两类测试：`npm --prefix harmony test`（node:test 宿主契约，根 vitest 已把 `harmony/tests/**` 排除）与 `harmony/tests/native` 的 CMake/ctest 原生回归。构建走 `./scripts/build-harmonyos.ps1 -DevEcoRoot $env:DEVECO_ROOT`（PowerShell + DevEco，产物在 `release/`）。设计概览见 [`docs/spec/client/harmonyos-client.md`](docs/spec/client/harmonyos-client.md)，移植过程与逐项验收台账在 `docs/harmonyos/`（`FEATURE_PARITY.md` 状态、`NATIVE_CAPABILITIES.md` 平台边界、`MAINTENANCE.md` 构建记录）。

**壳里只有系统能力，界面仍是同一份 Vue 前端**：服务端下发什么界面就显示什么，鸿蒙端不维护第二套 UI。原生侧负责本地登录页（复用安卓 `login.html` 资源，独立 ArkWeb 控制器、CSP 禁网）、按服务器保存密码（只把 Cookie 交给页面）、文件保存/分享/导入、通知、剪贴板与选区菜单、系统栏与键盘避让。

**原生桥不注入全局代理**：`rawfile/harmony-bootstrap.js` 经 `javaScriptOnDocumentStart` 在页面里装 `window.ClawBenchHarmony`（形状与 Android/Electron 的 `ClawBenchNative` 相同，故 `getNative()` 优先读它、业务代码零分支），真正的调用走面向精确 Origin 的随机 `MessagePort` + 连接代次，离页即关闭端口并拒绝在飞请求。新增方法要同时改 `ClawBenchNative` 类型、bootstrap 的方法映射与 ArkTS 分派；新增原生事件必须加进 bootstrap 的 `nativeEvents` 白名单，否则事件被静默丢弃。

**宿主身份独立于 Android**：`getNativeHostPlatform()` 返回 `'harmonyos'`，前端 `isHarmonyApp` 与 `isAndroidApp` 互斥。误并进 Android 会连带启用安卓字体/合成补偿、APK 更新入口与后台断连策略——这些在鸿蒙上都是错的；终端音量键这类能力按能力位（`getCapabilities().volumeKeys`）开，不按宿主身份开。协议门禁与桌面/Android 同源：`app==='clawbench' && protocol===1`。

**原生隧道只能由原生层做**：ArkWeb 建不了 SSH，故用 C++ NAPI（SSH 走 libssh2 + mbedTLS、HTTP2 走 nghttp2）实现正反向映射，异步执行不阻塞 UI，按服务器保存配置并在重连后恢复；主机密钥首次或变化需确认，**不接受跳过 TLS / 主机校验来换取连通**。当前能力表里报 `false` 的项（跨应用悬浮窗、实况窗）是签名级权限受限，不要用应用内弹层或空实现冒充；模拟器子场景通过也不等于该项功能验收通过，口径以 `docs/harmonyos/FEATURE_PARITY.md` 为准。

## 开发规则

- **改 UI 前先读视觉设计指导手册**：动样式、加组件、加主题、加动效之前必读 [`docs/spec/client/design-guide.md`](docs/spec/client/design-guide.md)——样式三层归属、设计 token（字号/间距/圆角/层级/时长）、36 主题机制与新增步骤、布局骨架，以及七条红线（`v-html` 匹配不到 scoped 规则、共享类基规则也必须全局、`app-region` 豁免只能是控件、对比度不能靠固定跳一档背景、`content-visibility` 滚动跳变、Android WebView 像素怪癖、`scrollbar-color` 弃用 `::-webkit-scrollbar-*`）。改动检查清单与守卫测试索引也在文末。
- **日志必须用封装**：前端一律 `appLog.d/i/w/e()`（`@/utils/appLog`），禁止原始 `console.*`；Android 一律 `AppLog.d/i/w/e()`，禁止 `android.util.Log`。两者的自身实现与测试除外。Tag 约定：短 PascalCase 模块名。
- **功能和 Bug 修复必须包含单元测试**：Go 用 `*_test.go`，前端用 `.test.ts`，放在对应代码旁。测试须验证具体行为，非泛化快乐路径。
- **顺手修复脆皮测试**：在改动过程中遇到（或让自己的改动暴露出来）的脆皮测试要当场修掉，别只绕开——脆皮的判定是**结果取决于与断言无关的因素**：固定 `sleep`/超时、依赖用例执行顺序或共享可变状态、按位置索引或易漂移类名选元素、断言实现细节而非行为、同义反复（无论实现对错都通过）、被 `--tests`/`-run` 过滤后零匹配静默通过。修法是消除不确定性来源（条件等待、每例独立夹具、稳定 hook、断言可区分对错），**不是**调大超时或加重试把它压绿。若修复需要改动被测代码的公共接口或明显超出当前任务范围，就地留 `TODO(flaky): <原因与判据>` 并在交付说明里点出，不要默默跳过。
- **E2E 测试（`e2e/`，Playwright）**：改动涉及跨组件流程 / 真实后端行为（聊天流式、ACP、终端、文件、任务、排队）时，须在 `e2e/specs/` 补对应 spec。**CI 会在每个 PR 跑全量 3 浏览器**（chromium/firefox/webkit），但拆成 **3 个并行 job**（每个 `--project=<浏览器>`），墙钟约 13 分钟。
  - **本地默认不跑全量 e2e**：`npx playwright test`（不带 `--project`/`--grep`）会跑 3 浏览器 × 全部 spec，约 33 分钟且与并发 agent 争抢资源。本地只跑**本次新增的 spec** 与**改动设计所涉及的 spec**：
    ```bash
    npx playwright test --config e2e/playwright.config.ts --project=chromium-coverage e2e/specs/<新增或受影响的>.spec.ts
    ```
  - **前置二进制**：e2e 用仓库根的 `./clawbench` 与 `./acp-mock`（`e2e/helpers/server.ts` 复制到临时目录；可用 `E2E_SERVER_BIN` 覆盖）。改了 Go 代码后先 `go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock`，否则 e2e 跑的是旧二进制。
  - **写 spec 的约定**：优先稳定 hook（`data-tab`/`data-session-id`/`data-action`）而非位置索引（`.nth(2)`）或易漂移的类名；跨 spec 的共享状态（ACP mode、会话、terminal PTY）会泄漏，改动 mode 的 spec 须在 `afterAll` 调 `restoreNonBlockingMode()`；不用固定 `waitForTimeout` 掩盖竞态，改用条件等待（`expect.poll`/`toBeVisible`）。
  - **flaky 判定**：spec 单独跑绿、全量跑红 ⇒ 先查跨 spec 状态泄漏与共享服务器状态（3 浏览器共用同一服务器/DB），勿直接归咎产品。全量留给 CI；本地判「是否我引入的回归」用隔离单跑。
- **改动 HTTP 接口必须同步 OpenAPI 文档**：任何新增 / 删除 / 修改 `/api/` 端点（路径、方法、鉴权、参数、请求 / 响应字段、状态码）都必须同步更新 `internal/api/openapi.yaml`（已从 `docs/spec/api/` 迁入以支持 `go:embed`）。
  - 字段名、参数名、方法**必须从 handler 代码里抄**（`decodeJSON` 结构体的 JSON tag、`r.URL.Query().Get(...)`、`requireMethod(...)` / `switch r.Method`），**禁止凭路由名望文生义**。
  - 路由唯一来源是 `internal/handler/handler.go` 的 `RegisterRoutes`；`internal/handler/openapi_drift_test.go` 双向校验路径与鉴权（**不校验字段名**）。
  - 完整维护清单见 `docs/spec/api/README.md`。
- **纯前端改动完成后必须自觉编译**：若只涉及 `web/src/`、`web/index.html` 等（未动 Go / Android），跑通测试后直接构建，供用户立即在浏览器 / App 中测试，无需用户再要求：

  ```bash
  npm run build        # 仓库根目录（web/package.json 的 build 会 cd .. 转调同一脚本）
  ```

  vite 的 `outDir` 是仓库根 `.clawbench-web/`（**不叫 `public`**，见下），服务端在 CWD 存在该目录时走 disk 模式（`frontend.GetFS()`）直接读它，因此构建后**立即生效，无需重启**。`internal/frontend/dist` 仅在 CWD 无该目录时被读取（单二进制分发场景）。
- **前端产物目录必须叫 `.clawbench-web/`**：这个路径是**相对进程 CWD** 解析的，所以名字必须唯一到不可能与无关目录撞车。`public` 这种通用名踩过真实的坑（issue #461）：macOS 自带 `~/Public`，而 APFS 默认大小写不敏感，于是从家目录启动时 `os.Stat("public")` 命中了系统目录，服务端放弃内嵌前端去读那个空目录，所有页面 404、App 卡在启动页。改名同时让老安装遗留的旧 `public/` 自动失效（不再被任何代码读取），无需用户清理。改名前请勿换回通用名；`TestDiskDirName_DoesNotCollideWithUnrelatedDirs` 会拒绝一批常见目录名。
- **前端编译完后必须嵌入到程序中**：`go:embed` 只在**编译期**读盘（`internal/frontend/embed.go` 的 `//go:embed all:dist`），所以磁盘上换了前端文件，**运行中的进程不会看到**——必须重新编译并重启。完整链路：

  ```bash
  ./build.sh --restart     # 构建前端 → 同步 embed → 重编 Go → 重启（一条命令全覆盖）
  ```

  只做纯前端迭代时，`npm run build` 后 disk 模式会立即生效（无需重启）；但要**分发**二进制、或要让 embed 内容跟上，必须走 `build.sh`。
- **embed 只在两种情况下需要手动处理**：
  - **APK**：`ServeAPK` 恒定读 `EmbeddedFS()`（`internal/handler/apk.go`），**不查 CWD**——`/api/apk` 下发的是**构建 Go 二进制时**嵌入的 APK，与磁盘 `.clawbench-web/` 无关，换了 APK 必须重编二进制才生效。且 APK 必须放进 `.clawbench-web/assets/`（源在 `public` 时代的同一相对位置）：`build.sh` 会 `rm -rf internal/frontend/dist && cp -r .clawbench-web internal/frontend/dist`，只存在于 `dist/` 的 APK 会被冲掉（实测发生过，`/api/apk` 静默退回 404），放在构建输出目录的 `assets/` 才随 `cp -r` 带回。
  - **可分发二进制**：`go build` / 交叉编译前必须同步 embed（`cp -r .clawbench-web internal/frontend/dist`），否则二进制内没有前端。
- **覆盖率门槛**：每 PR / 推送到 main 强制执行——包级覆盖率不低于基线、变更行覆盖率 ≥ 80%。
- **推送前必须运行本地检查**：`./scripts/pre-push-checks.sh`
- **跑全量测试前先评估成本，且不得与并发 agent 争抢**：主工作区可能同时有多个 agent 在改同一棵树，全量测试是**共享的稀缺资源**——实测全量 vitest 约 14 分钟、`go test ./internal/service` 约 128 秒、前端构建 1.5–4 分钟；并发时彼此争抢 CPU/内存，会把对方拖到超时（同一命令并发下撞 600s 超时，单独跑仅 128s）并产生假失败。
  - **先评估要不要跑全量**：能用隔离测试回答的问题（`npx vitest run <file>`、`go test ./internal/<pkg>/ -run <TestName>`）就不要跑全量。判「是否我引入的回归」一律先隔离单跑。
  - **跑之前先检查他人是否已在跑**：`ps -eo pid,ppid,etime,cmd | grep -E "vitest|go test|npm run build" | grep -v grep`。有则 `sleep` 等待其结束后再跑，不要并发起跑。
  - **不重复跑同一个全量**：复用已有结果，例如 `./scripts/check-go-coverage.sh --skip-test` 复用已生成的 `coverage.out`。
  - **绝不同时跑 `npm run build` 与全量 vitest**：会 OOM 被 Killed，并把结果污染成成片假失败。必须串行。
  - **判据**：若一个验证动作**不改变结论**，就不该跑。跑之前问「这个结果会让我改代码吗」，不会就别跑。
  - **baseline 陈旧导致的 Tier 1 失败不要靠补测试去「修」**：先确认归属（比对失败包是否被自己改过）；Tier 1-only 是 non-blocking（脚本以 `exit 2` 区分）。
