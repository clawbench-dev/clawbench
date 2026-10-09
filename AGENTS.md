# AGENTS.md

## 项目概述

ClawBench 是面向手机 / 平板 / 桌面的多端 AI 工作台，移动端交互适配优先、桌面端完整支持，将 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Copilot、Kimi、Antigravity、Grok Build、ZCode）封装为 Web 平台。Go 后端调用 CLI 工具，通过 WebSocket 流式传输 JSON 事件；Vue 3 前端实时渲染。支持 ACP (Agent Client Protocol) stdio 传输（含桥接适配器）、**多智能体群聊**（主持人 / 自由两种模式）、SSH 隧道端口转发、任务系统（含 GitHub/GitLab 事件触发）。

规格文档：`docs/spec/`（模块索引见 `docs/spec/README.md`）。面向使用者的图文操作手册：`docs/user-guid/user-guid.md`（截图维护流程见 `docs/Skills/clawbench-user-guide/SKILL.md`）。

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
./scripts/pre-push-checks.sh [--skip-coverage|--skip-android]   # 推送前全量检查

# E2E（Playwright，见下方「E2E 测试」规则）
go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock   # e2e 依赖这两个二进制
npx playwright test --config e2e/playwright.config.ts --project=chromium-coverage e2e/specs/<你的 spec>.spec.ts

# 编译并后台重启（可在 Web 终端内执行）
./build.sh --restart [--restart-skip-build] [--restart-port=8080]
```

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
| `internal/service/` | 业务逻辑：聊天持久化、摘要与推荐的调度、调度器（cron + 事件触发任务；cron 可选的前置脚本执行器 `task_script.go`，静默成功即跳过 AI 且不发通知）、SQLite、Schema 迁移、Agent 存储、用量聚合、会话截断；**项目以整数 id 为身份**（`internal/store` 的项目注册表 + `projects_migrate.go` 的路径→id 转换：所有项目作用域表存 `project_id` 而非路径，改名/移动目录只需一条 UPDATE；`project_id=0` 是全局标签与无法归属分享的保留哨兵，故不声明外键）；**项目注册表管理**（`project_registry.go` 的 `ListAllProjects`/`GetProjectDetail`——列表只做单次 `os.Stat` 判目录是否存在、详情附 `DetectRepoLayout` 仓库类型；`project_delete.go` 的 `DeleteProjectData` 在同一事务清理约 15 张项目作用域表 + 子表，写锁内判运行中会话守卫，提交后再调 `rag.DeleteProjectData` 避免持锁死锁）；会话运行态收敛在 `session_runner.go`（单一 owner runner，运行态与可取消性同源），AI 回合编排唯一实现 `run_turn.go`，请求构造唯一实现 `chat_request.go`；**排队消息存独立表 `queued_messages`（出队才落 `chat_history`，`queue_store.go` 的 claim 在同一事务内 DELETE 队列行 + INSERT 历史行，使历史行 id 顺序恒等于对话顺序）**，队列兜底回收 `queue_reaper.go`；**AI 群聊编排器 `group_orchestrator.go`**（主持人/自由两模式共用循环内核 `drainSpeakers`/`runSpeakerTurn`；`group_store.go` 成员与群设置、`group_inject.go` 增量注入、`group_prompt.go` 提示词、`group_member_request.go` 成员 resume；成员即隐藏 `chat_sessions` 行 `session_type='group_member'`，消息只存群会话，`run_turn` 新增 `TimelineSessionID` 解耦时间线语义）；`/btw` 旁路问答（`btw.go`，独立表 `btw_questions`）；最近项目分组（`recent_project_groups.go` + `repo_layout.go`，纯文件系统识别主仓库/工作树/子目录）；含 SessionCleanupWorker / BingWallpaperWorker / ForgePoller（forge 变化轮询）、桌面端升级检查（`desktop_upgrade.go`）等后台 worker |
| `internal/ai/` + `backends/` | AI 后端抽象：`AIBackend` → `CLIBackend`（CLI+行解析）或 `ACPBackend`（JSON-RPC over stdio）；15 个后端子包；CLI/ACP 均支持无进度看门狗（ACP 侧按**模型进展**判定，与连接活性双信号分离）；`compact_detect.go` 识别各后端上下文压缩信号（压缩后下一轮重注入系统提示）；ACP 客户端能力按 agent 定制（CodeBuddy 隐藏 Terminal / `fs.readTextFile`——声明能力会替换其原生工具实现）；ACP 连接缓存状态用 leaf lock `stateMu`（绝不在 RPC 期间持有），避免通知回调与在飞 RPC 互相等待致通知队列溢出杀连接 |
| `internal/askquestion/` | `<clawbench-ask-question>` 载荷解析的**唯一** Go 实现（叶子包，不 import 任何 internal 包）；与前端 `web/src/utils/askQuestion.ts` 互为镜像，由 `testdata/parity_corpus.json` 双向固化。契约：检测即解析；不可解析时剥离标签、把标签内文字作为 Fallback 交给 Markdown 渲染（`Match.Fallback`），**绝不丢内容**。旧 XML 子元素格式与标签内 JSON 已不再解析 |
| `internal/grouprouting/` | 群聊路由标签（`<clawbench-mention targets="A,B">…</clawbench-mention>` + `<clawbench-group-end/>`）解析的**唯一** Go 实现（叶子包，不 import 任何 internal 包）；与前端 `web/src/utils/groupRouting.ts` 互为镜像，由 `testdata/parity_corpus.json` 双向固化。契约与 askquestion 一致（检测即解析、不可解析不剥离、绝不丢内容）。密送（`private` 提及）的注入侧 fail-closed（`StripProtocolTags` 剥一切畸形/嵌套形态）与显示侧 fail-open 分列两套 |
| `internal/model/` | 数据模型、后端注册表、模型发现（`ModelSource` 注册表 + 单一合并点 `ResolveModels`）、27 个 LLM Provider；Agent 含 `avatar` 字段（用户自选的 DiceBear SVG，贯穿 DB 列与两条持久化路径） |
| `internal/speech/` + `internal/stt/` | 语音：TTS（Edge / Piper / Kokoro / MOSS-TTS-Nano）与 STT（vLLM Whisper，流式 + 非流式） |
| `internal/rag/` | RAG：SQLite + sqlite-vec 向量存储 + FTS5 全文检索，OpenAI 兼容嵌入 API；消息聚类（ClusterWorker） |
| `internal/terminal/` | Web 终端：PTY 会话、环形缓冲回放、多标签 |
| `internal/ws/` | WebSocket 事件通道：StreamHub 会话级扇出，Manager 广播 + 重连缓冲回放；`delivery_stats.go` 记录按原因的投递丢弃计数（`GET /api/ws/delivery-stats`），关键事件在通道满时等待空位（ACP 来源除外）。遥测类事件（`system_resources`）走**非缓冲投递**路径（不写回放缓冲、队列满时丢弃而非断连），需求由客户端 `metrics_preference` 声明 |
| `internal/ssh/` + `internal/proxy/` | SSH 隧道服务器；HTTP 反向代理 + 端口转发。反向映射仅绑 127.0.0.1 且禁绑保留端口 |
| `internal/tunnel/` | h2 流隧道的传输无关内核（`/api/tunnel/stream` 数据面 + `/api/tunnel/control` 反向控制面）：`bind.go`（端口分配 + 白名单/保留端口）、`guard.go`（`PortGuard` 镜像 `internal/ssh` 的端口策略，保证 h2 与 SSH 同一套规则）、`claim.go`（`-R` 的单次 token 认领）、`ndjson.go`（控制流分帧）、`relay.go`（双向转发 + 半关闭）、`target.go`（目标地址解析） |
| `internal/forge/` | GitHub/GitLab 集成：平台无关的只读 `Provider` 抽象（统一 Issue/PR/Comment/Pipeline 模型）+ `github/`（go-github）/ `gitlab/`（轻量 REST client）adapter；remote URL 解析（host 与 scheme 分离解析）、per-host 令牌桶限流、事件推导引擎。**无 host 安全闸门**（内网/自建实例一律放行，风险提示在前端绑定弹窗） |
| `internal/push/` | IM 机器人推送：`common/`（共享接口 + 会话命令）、`dingtalk/`（Stream API）、`feishu/`（Lark SDK WebSocket + 互动卡片） |
| `internal/symbol/` | 基于 tree-sitter 的代码符号提取（纯 Go，无 CGO） |
| `internal/skill/` | 后端无关的跨智能体 Skill 发现框架（Skill = 含 `SKILL.md` 的目录）。只依赖 `internal/model`（不得 import `internal/ai`/`backends`，否则成环）：`scanner.go` 递归扫描（有界深度、跳过 `.git`/`node_modules` 等）、`registry.go` 按 `SourceKind`（本智能体原生 > 用户目录 > git > 其他原生）去重、`git.go`+`worker.go` clone/pull git 源（单例 worker，启动 + 定时 + 手动 `POST /api/skills/refresh` 三触发）。**本地目录新增走 PATCH 同步重扫、Git 仓库新增由前端在 addRepo 成功后自动调一次 refresh**（PATCH 只落盘不联网，避免在 config 写锁内做网络 IO）；`POST /api/skills/rescan` 是**纯本地重扫**（不联网、不写同步状态）。系统提示词注入在 `service.AppendSkillsSection`，**两个 `ai.ChatRequest.SystemPrompt` 生产者都必须调用**（`chat_request.go` 与 `scheduler.go`）。`AutoLoadsNativeSkills` 决定后端是否自加载（codebuddy=false 必须注入；其余多为 true），取值由 `internal/ai/backends/native_skills_test.go` 双向表钉住 |
| `internal/summarize/` | 摘要与推荐的底层引擎（多后端 provider、多 pass 压缩、`StripMarkdown`、`RecommendNextStep`） |
| `internal/system/` | 系统资源监控：CPU / 内存 / 磁盘 / 网络实时采集与推送 |
| `internal/cli/` | AI Agent 自助命令：仅剩 upgrade-replace（自升级内部机制）；task/rag 业务子命令已移除，改由 `/cb-*` 内置斜杠命令直调 HTTP API |
| `internal/middleware/` | 鉴权、请求日志、panic 恢复、请求 ID |
| `internal/platform/` | 跨平台路径解析、Shell 检测、二进制替换原语（`ReplaceBinary`，升级路径共用） |

### 前端（Vue 3 + TypeScript）

源码根：`web/src/`。无 Vue Router，基于抽屉的单页布局。单一 `reactive()` store (`stores/app.ts`)。**同一时刻只允许一个标签页跑应用**（`useSingleTab.ts` 选举，第二标签页只显示阻塞屏、不挂载应用）：服务端按 `localStorage` 里的 `client_id` 键控 WS 订阅槽，同源所有标签页共用同一个 id，两个标签页会互相顶掉 socket 并各自重连，实测 17 分钟 1402 次 subscribe、约 2900 请求/分钟。新增全局唯一资源的消费者前先确认是否也受此约束。选举优先用 **Web Locks**（`navigator.locks` 单一独占锁；锁在上下文消亡时由浏览器自动释放，故 owner 崩溃/关闭/导航都不会留下"幽灵占用"永久阻塞等待方），不支持时回退 **BroadcastChannel**（jsdom、老浏览器/WebView）。owner 在**每次** `pagehide` 都释放槽位（含 bfcache 进入——冻结页无法运行代码，持锁会阻塞等待方最长 ~10 分钟），`pageshow` 时重新认领。

Composable 与组件均按域分组（Chat、Session、Terminal、File、Git、Navigation/Gesture、Settings、Agent、Task、Infrastructure、System）。新建 composable 须放 `web/src/composables/` 并以 `useXxx` 命名，测试用 `*.test.ts` 同目录或 `__tests__/`。

`web/src/utils/askQuestion.ts` 与 Go 的 `internal/askquestion` 互为镜像（共享语料 `internal/askquestion/testdata/parity_corpus.json` 双向固化）——改一侧必须同步另一侧，否则同一段文本会在前后端得到不同解析。`web/src/utils/groupRouting.ts` 与 Go 的 `internal/grouprouting` 同样互为镜像（群聊 @ 提及 / 结束标签 / 密送，共享 parity 语料）。

**群聊前端**：`useGroupChat.ts`（群回合状态、路由卡片渲染）+ `useGroupMembers.ts`（成员花名册与 `isGroupSession` 判定，用后端 `sessionType === 'group'` 而非"名单非空"）+ `GroupSettingsSheet.vue`（群聊设置抽屉：成员管理 + 群设置）、`GroupAvatarStack.vue` / `GroupMemberStack.vue`（头像堆叠，主持人排最前）、`SessionGroupHeader.vue`。建群入口在会话列表头，复用 `AgentSelectorDrawer` 的多选模式。

宽屏 Dock 页签定义在 `web/src/composables/dockTabs.ts`（单一注册表，渲染集合与切换白名单都从它派生），图标单独放 `dockTabMeta.ts`。`dockTabs.ts` 必须保持零 import（`useWideScreenLayout` 依赖它，而多个测试文件对 `lucide-vue-next` 做了窄 mock）。左侧面板归属（宽屏 Dock 显示哪个页签）是**项目属性**而非代码路径属性，由 `useProjectPanel.ts` 按 `clawbench-project-panel:<项目根>` 记忆；项目切换期间用计数器抑制误写（可并发调用，布尔会被先结束者清掉）。

`web/vendor-build/excalidraw/` 是独立的 Excalidraw 编辑器构建（React），由 `build.sh` 单独构建到 `.clawbench-web/vendor/excalidraw/`，`.excalidraw` 文件通过 iframe 懒加载，Vue 主包不含 React 依赖。它**有意不进根 Vite 构建**（否则主 bundle 会膨胀约 8MB），因此根 `npm run build` 不产出它——**所有 CI / release job 都必须显式构建该 vendor bundle**，否则发布二进制内嵌的前端里没有 `vendor/excalidraw/`，`/vendor/excalidraw/index.html` 走 `ServeIndex` 的 `http.NotFound` 返回 Go 的 "404 page not found"（本地用 `build.sh` 构建正常，缺陷只在 release / Docker 产物上暴露）。

`web/src/share/` 是分享链接的独立只读 SPA（文件分享=类型分派渲染 + TOC + 下载；会话分享=快照对话 + 目录导航 + 导出 JSON），由 vite 多入口构建为 `share.html`，服务端在 `/share/{token}` 无鉴权公开（token 即凭证）。

### 桌面端（Electron）

源码根：`desktop/src/main/`。桌面端是纯"壳"，复用服务器 + Web 前端全部业务逻辑，仅提供 Web 环境之外的桌面能力（尤其是**窗口最小化/隐藏时仍能弹系统通知**——浏览器标签被冻结时页面内 `Notification` 不会触发）。主进程模块通过 IPC（`native:*`）暴露为 `window.ClawBenchNative`，与 Android WebView 共用同一套前端接口（`web/src/utils/clawbenchNative.ts`）：

| 模块 | 职责 |
|------|------|
| `window.ts` | 主窗口创建（Windows/Linux 无边框，macOS 原生帧）、最小化/最大化/关闭三个自绘控制的 IPC、原生上下文菜单（cut/copy/paste 走 OS role，copy-link/copy-image 按语言翻译）、外部链接拦截交给默认浏览器 |
| `windowChrome.ts` | 无边框窗口的平台判定（纯模块、无 electron import、可单测）：Windows/Linux 自绘控制簇，macOS 保留原生交通灯，未知平台回退原生帧 |
| `splash.ts` | 登录/启动加载屏（对齐 Android splash）：页面加载成功后才淡出；复用页面时必须清掉上一次的淡出类，且加载成功要取消连接超时 |
| `bridge.ts` | IPC 桥：服务器列表/凭据、SSH 端口映射、文件下载、分享、系统通知、主题、语言、日志捕获、屏幕常亮 |
| `tunnel.ts` | ssh2 客户端，读取 `/api/ssh/info` 建立 SSH 端口映射 |
| `download.ts` | 文件下载（保存对话框 + 下载后定位）、URL/Blob 下载 |
| `notification.ts` | 原生系统通知，点击导航到会话/任务/仓库（冷启动挂起派发，经 `navReady` 的 `rendererReady()` 握手后才放行）。窗口**可见且未最小化**时**抑制通知**（刻意不看焦点：窗口开着就不打扰）——用户开着应用，通知只会重复应用内完成卡片；判定必须在主进程做，渲染层的 `document.hasFocus()` 在最小化/隐藏窗口里仍可能为真 |
| `clientLog.ts` | 主进程日志回传：缓冲 POST `/api/client-log`（`source="electron"`）+ 写 `{userData}/desktop.log`；镜像渲染进程 console，`recordError` 上报未捕获异常 |
| `identity.ts` | `APP_USER_MODEL_ID`（Windows toast 身份），**必须与 `electron-builder.yml` 的 `appId` 一致**——该 yml 不随包分发，运行时读不到，漂移会让 Windows 通知静默消失；`identity.test.ts` 守住。Linux 任务栏图标另依赖 `desktop/package.json` 的 `desktopName`（决定窗口 `app_id`），**不能改 `productName`**——那会挪动 userData 目录、丢用户配置 |
| `urlPolicy.ts` / `contextMenu.ts` | 外部链接判定（以服务器 Origin 为边界）与原生右键菜单（标准项走 OS role 本地化） |
| `navReady.ts` / `session.ts` | 渲染进程就绪握手、会话缓存强刷 |
| `shortcuts.ts` | 应用级快捷键决策表（`before-input-event` 只认领 Ctrl+Shift+R / F12，F5 等一律放行给页面），不 import electron 便于单测 |
| `updater.ts` | 升级检查：请求**服务端** `/api/desktop/latest`（不查 npm）；语义化版本比较，降级不误报 |
| `install.ts` | 自升级安装：多候选 URL 依次降级下载 → SRI 校验 → 解压 zip（剥顶层包装目录、拒绝路径穿越、**恢复可执行位**）→ 侧装到 `~/.clawbench-desktop/app-<version>/` → 翻转 `current` 指针。另支持**增量载荷包**：安装到从当前版本克隆出的目录、复用 Electron 运行时，装前按主版本 + 壳指纹门控，不符即静默回退全量下载 |
| `secrets.ts` / `store.ts` | safeStorage 加密存密码（**按服务端分别存于各自 entry**，旧全局槽迁移时只归属当时的活动服务端）、electron-store 持久化服务器列表/主题/语言 |
| `powersave.ts` | 屏幕常亮（powerSaveBlocker） |

**自升级采用"侧装 + 指针"而非原地替换**：运行中的进程无法覆盖自身（Windows 上尤其如此）。`install.ts` 把新版本解压到独立目录并改写 `~/.clawbench-desktop/current`；**应用自己**在 `whenReady` 最前面读该指针决定运行哪个版本（`selectStartupVersion` + `handOffToPointedVersion`）——指针缺失、目标目录不存在或指向自身都清指针并继续用当前版本启动，因此失败可回滚、旧版本保留，被删坏或写坏的升级永远不会让应用打不开。必须由应用自己读：原先读指针的 npm 启动器已随 npm 渠道一并移除，双击 Release 解压出的旧 exe 时若无人读指针，会静默退回旧版。

**全量包以 GitHub Release 为准，载荷包只走 npm**：桌面端与服务端同版本发布，`/api/desktop/latest` 直接返回服务端自身版本 + 资产地址，**不查询任何外部服务**（不查 npm、不查 GitHub API）。每个平台返回**候选 URL 列表**（国内镜像优先、直连 github.com 兜底），客户端与前端都取首个可用项。

增量升级用的**载荷包**（应用自身，约 3MB）**只在 npm 发布**（`publish-npm-desktop`），不再作为 Release 资产——在 Release 资产里放一个 3MB 的 zip 会让人误以为它可直接安装（它缺 Electron 运行时，只能增量套用）。`payloads` 因此每平台只有 npm tarball 一个候选；macOS 无载荷（替换会破坏签名），该键缺席即走全量下载。

`tag` 为空表示当前是 dev/未打标签构建（无对应 Release），此时 `downloads` 为空、客户端隐藏下载入口——不要把它当错误处理。

**桌面壳不是"手机 App 模式"**：两者都经原生桥被识别为原生环境，但省电策略相反——Android 退到后台会被系统挂起，故隐藏时主动断开 WebSocket；桌面窗口只是最小化、进程仍在跑，断开 WS 等于自断通知来源（通知全部由 WS 事件产生）。前端用 `isDesktopApp`（preload 注入 → `useAppMode` → 消费点三层打通）区分，门控写成 `isAppMode && !isDesktopApp`。任何一层漏掉都会让最小化后的推送静默失效。端口映射同理要按"期望状态"而非快照管理：重连后必须重建全部 listener，`ensureTunnel` 需单飞守卫（并发调用会互相拆台）。

**平台判定拆为三条正交轴，不再有 `isPC`**（`web/src/composables/usePlatformDetect.ts` + `useWideScreenLayout.ts`）：旧的 `isPC` 把三个无关问题 OR 在一起（宿主 OR 输入 OR 视口），每个边界都判错。现在消费方各取所需——**HOST**（`isElectron` / `isAndroidApp` / `isWebApp` / `isNativeApp`，来自原生桥）、**INPUT**（`isTouchPrimary`，`(pointer: coarse)` 加 UA 兜底）、**VIEWPORT**（`isWideScreen`，独立模块）。Electron 是有物理键盘鼠标的桌面窗口，宿主轴直接判为 `isElectron`，不再靠"参与 `isPC`"来避免被误判成移动端。`web/src/__tests__/platformAxes.test.ts` 钉住 `isPC` 不得回归、共享谓词不得重声明、iPadOS 陷阱（Macintosh UA + `maxTouchPoints>0`）不得重开。

构建与发布：`desktop/` 用 electron-builder 的 `dir` target 产出免安装目录，由 `release.yml` 的 `build-desktop-*` 四个 job（linux / linux-arm64 / windows / macos）打包为 zip 挂 GitHub Release（**只挂全量包**）。资产名必须与 `internal/service/desktop_upgrade.go` 的 `desktopAssetName` 完全一致，否则下载 404。`publish-npm-desktop` 发三个 `@xulongzhe/clawbench-desktop-<plat>-payload` 包（linux-x64 / linux-arm64 / win32-x64；darwin 从不发 npm）；`publish-npm` 只发服务端 CLI `@xulongzhe/clawbench`。CI 构建前需 `ELECTRON_MIRROR` 走镜像，否则 `@electron/get` 从 GitHub 下载常中断。`win.signAndEditExecutable` **不要**在 electron-builder.yml 里禁用——CI 的 windows job 原生可用 rcedit，禁用会跳过向 exe 写入图标与版本元数据；本地无 wine 交叉编译时才用命令行临时覆盖。

## 开发规则

- **改 UI 前先读视觉设计指导手册**：动样式、加组件、加主题、加动效之前必读 [`docs/spec/client/design-guide.md`](docs/spec/client/design-guide.md)——样式三层归属、设计 token（字号/间距/圆角/层级/时长）、36 主题机制与新增步骤、布局骨架，以及七条红线（`v-html` 匹配不到 scoped 规则、共享类基规则也必须全局、`app-region` 豁免只能是控件、对比度不能靠固定跳一档背景、`content-visibility` 滚动跳变、Android WebView 像素怪癖、`scrollbar-color` 弃用 `::-webkit-scrollbar-*`）。改动检查清单与守卫测试索引也在文末。
- **日志必须用封装**：前端一律 `appLog.d/i/w/e()`（`@/utils/appLog`），禁止原始 `console.*`；Android 一律 `AppLog.d/i/w/e()`，禁止 `android.util.Log`。两者的自身实现与测试除外。Tag 约定：短 PascalCase 模块名。
- **功能和 Bug 修复必须包含单元测试**：Go 用 `*_test.go`，前端用 `.test.ts`，放在对应代码旁。测试须验证具体行为，非泛化快乐路径。
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
