# ClawBench 系统设计规格

ClawBench 是移动端交互适配优先、桌面端完整支持的多端 AI 工作台，将多种 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、Kimi、Copilot、MiMo-Code、Pi、Antigravity、Grok Build、ZCode）包装为 Web 可访问的平台。Go 后端通过 shell 调用 CLI 工具并经 WebSocket 流式输出 JSON，同时支持 ACP（Agent Client Protocol）stdio 传输，提供结构化的模式切换、斜杠命令和权限管理。Vue 3 前端实时渲染流式事件。支持 SSH 隧道端口映射、FRP 公网隧道、任务系统（含 GitHub/GitLab 事件触发）、会话标签、零配置启动引导、聊天自动摘要、钉钉/飞书企业推送、系统资源监控、thinking 惰性加载和消息聚类分析。

## 模块地图

### core/ — 核心业务

| 模块 | 说明 |
|------|------|
| [聊天流程](core/chat-flow.md) | 用户发消息到 AI 回复的完整链路：handler → 唯一 turn 实现 → AI 后端 → WebSocket StreamHub → 前端；含 ACP 权限审批、交互式提问卡（`<clawbench-ask-question>` → AskUserQuestion 工具调用，单选可取消，答案状态跨重渲染持久化）、/cb-* 内置命令注入、请求构造唯一实现（直发与排队路径一致）、文件附件行范围、自动摘要（AI 失败降级结论文本）、分叉上下文按优先级压缩（保留全部用户消息）、thinking 惰性加载、子智能体内容分组（按 `_meta` 父工具调用 id 折叠进父 Agent 卡片）、工具调用耗时、消息元信息区在气泡外（用户消息同样支持复制/详情）、会话重置（卡死会话一键重启进程保留上下文）、消息回溯 Rewind（原址截断会话历史并重启 AI 会话，同时清空计划面板）、完成通知（后台事件时纯通知卡片，类别 chip + 事件类型 chip + 主体名称 + 最多 4 行摘要 + 跨项目整卡换色与区隔带 + 跳转顺带标记已读 + 关闭，5 秒自动关闭，覆盖会话/任务/仓库且与系统通知对齐，详见[应用内完成通知](features/completion-popup.md)）、未读自动清除、错误码透传与展示、取消耗时与 finalize 阶段计时、滚动保持机制、按项目恢复上次会话、输入草稿与会话快照恢复、DB 持久化消息队列（drain loop 原子出队 + 出队熔断 + 兜底回收器）、ACP `_meta` Token/成本明细（最新完整快照合并，供[用量统计](features/usage-stats.md)聚合）展示 |
| [AI 后端抽象](core/ai-backend.md) | 双传输后端（CLI shell-out + ACP stdio）、流式事件累加（AccumulateBlock + 回放检测 + 连续 thinking 合并 + AskQuestion 转换）、ACP 状态提取（mode/thinking/model）、ACP 崩溃诊断、acpStdoutFilter 协议修复（含 SessionModelState 提取）、ACP context_state 持久化、ACP 会话恢复重试与 NewSessionFallback、thinking 惰性加载、CodeWhale 字段重映射、Grok Build 双传输（ACP + streaming-json CLI）、ZCode ACP 桥接（zcode-acp-server）、共享规则模板、连接管理（AgentID/BackendID 无锁防死锁、用户取消保护存活连接、ensureAliveWithSession 使用 ResumeSession）、LoadSession 异步回放、ListSessions 磁盘扫描回退、EnsureAlive、CodeBuddy MCP 配置注入、CodeBuddy Plugin Skills 竞态修复、ACP `_meta` 扩展元信息解析（per-agent 归一化 → chat_metadata）、子智能体父工具调用归属（`_meta` parentToolCallId / parentToolUseId → ParentToolCallID） |
| [流式传输体系](core/streaming.md) | 单一 WebSocket StreamHub（含断线 ≤10s 缓冲重放、≤50 条上限、>120s 清理订阅）+ 旁注小 SSE/WS 通道；含前端重连状态同步、subscribeOnly 模式、replay_done 事件、投递可观测（`/api/ws/delivery-stats`）、关键事件等待/高频增量丢弃的双层分级、前端有界缓冲回放 |
| [会话生命周期](core/session-lifecycle.md) | 聊天会话的创建、执行、排队、取消、归档（软删除）、物理删除（Destroy）、续接对话（标题时间戳前缀 + 锁定）、分叉（含 beforeMessageId、可选 Agent）、会话标题派生（transcript 双候选提取）、设置即时持久化、会话标签、过期归档自动清理、Codex 项目级历史会话发现（磁盘扫描 + ACP 合并）、单一 owner runner（运行态与可取消性同源）、唯一 turn 实现与队列兜底回收、优雅退出（WaitStreamsDrained + GracefulStopAll 等待流落库再回收进程） |
| [摘要管线](core/summarization.md) | 双管线（TTS vs 阅读摘要）、summarizeMessage 统一调度、SummaryCards 结构化卡片、摘要视图 warning/error 横幅通道、多 pass 压缩、Block 提取算法、降级链（AI 失败使用结论文本）、热重载、推荐回复（stable/rolling 分离 + prompt caching） |

### features/ — 功能特性

| 模块 | 说明 |
|------|------|
| [首次访问欢迎面板](features/setup-wizard.md) | WelcomeOverlay 后端检测面板（非 5 步向导）；Agent 创建走自动发现 + AgentInstallDialog；14 个后端规格 |
| [任务](features/scheduled-tasks.md) | cron 调度 → AI 执行 → 摘要推送，支持暂停/恢复/手动触发/续接对话，运行中流式状态展示，执行级逐条已读（不再切 tab 自动清零）；含事件触发任务（GitHub/GitLab 事件唤起，只读事件上下文注入，列表行与聊天预览卡均展示订阅事件而非空白 cron 字段，见 [Forge 集成](features/forge-integration.md)） |
| [Forge 集成](features/forge-integration.md) | GitHub/GitLab Issue + PR/MR 只读浏览（仓库绑定 + 列表/详情/评论）、面板内「动态」页签（未读/已读/全部条目聚合）、后台轮询感知变化（水位线 + 快照 diff）、CI 完成事件（per-run 去重表 + 按 run 去重 debounce）、流水线 ↔ PR 双向跳转、按条目未读与通知、事件触发 AI 任务、URL 附件「引用到对话」、按 host 凭据隔离与自部署实例 http/https |
| [会话标签](features/session-tags.md) | 按项目隔离的标签定义（`UNIQUE(name, project_path)`）+ 会话关联、长按菜单打标签、会话行标签行、顶部过滤栏（仅列在用标签，可换行 + 高度封顶）、可读性校准的哈希配色、胶囊即选中控件、失败可见、PATCH 全量替换语义 |
| [语音合成](features/tts.md) | 多引擎 TTS（云/本地），文本清理，缓存策略 |
| [语音输入](features/stt.md) | 双模式语音识别（流式 WS + 非流式 POST）、vLLM Whisper 引擎、增量识别 + 最终全量、安全上下文检测、快捷键触发 |
| [推荐回复](features/chat-recommendation.md) | AI 回复完成后自动生成下一步建议、stable/rolling 分离支持 prompt caching、快捷指令感知、离线恢复、会话隔离 |
| [Web 终端](features/terminal.md) | PTY 多标签会话（独立进程组防 /dev/tty 阻塞）、三模式手势系统（浏览/手势/选择）、拖拽选择+浮动复制栏、虚拟修饰键、键位/符号配置、终端主题切换、终端输入抽屉、终端帮助抽屉、TUI 应用支持 |
| [Git 管理](features/git-management.md) | 历史浏览（含工作区变更页按需重拉）、文件 Diff 抽屉（prev/next 顺序导航）、Worktree 隔离、分支/标签 CRUD、内联操作按钮、代码量统计（存量 cloc 快照，两层排除=内置规则 ∩ 项目 `.gitignore` + 增量 git stats 时间窗，双子页）、停靠页与抽屉共享同一套历史视图逻辑 |
| [文件管理](features/file-management.md) | 目录浏览（browse）+ 文件查看（view）独立 Tab、按 `.gitignore` 灰显 git 不跟踪的条目（仅淡化、仍可操作）、停靠预览窗格（工具栏开关 → 列表下方可拖拽高度的预览区，目录列出内容、文件复用代码切片/媒体渲染）、预览按行窗口取数（`lineStart`/`lineEnd` + `totalLines`，大文件不整体传输）、CodeMirror 代码编辑（浏览/编辑双模式）、VS Code 风格 sticky scroll、Markdown 标题锚定滚动同步、Markdown HTML 导出（共享渲染管线重建自包含单文件）、代码链接预览（点击验证过的代码文件路径/path:line 链接弹出代码切片浮层卡片，详见文件管理规格）、文件分享链接（capability token 公开只读，边界=创建时快照的根）、Excalidraw 画布编辑（iframe 内嵌独立构建 + 保存写回原文件）、内联音频/视频播放器、二进制文件处理（64KB/512KB 截断 + forceText）、目录导航栈、双候选路径解析、文件刷新与差异高亮（useFileRefresh 统一三种触发 + Markdown 块级差异 + 代码行级差异 + 两阶段闪烁）、刷新跳过加载遮罩、编辑、上传（含文件夹上传/目录树下载/粘贴上传）、目录跳转、拖放移动、面包屑拖拽到聊天、排序、网格视图、键盘快捷键、代码符号提取、归档打包 |
| [文件发现](features/file-discovery.md) | 搜索融合进文件管理器主界面（内嵌视图，结果复用目录条目交互与 git 忽略灰显）、结果展示所在目录、全局搜索蕴含递归、PC Shift 范围选、最近文件、统一覆盖层打开行为 |
| [附件与系统分享](features/attachments-and-share.md) | 多文件附件（含行范围）、上传历史（支持删除）、Share In（支持删除）、文件夹上传（保持目录结构）、目录树下载（File System Access API）、粘贴上传、面包屑拖拽附件、缩略图与项目隔离 |
| [会话导航与分叉](features/session-navigation.md) | 用户消息索引（含搜索框即时过滤 + 命中高亮）、跨分页定位、Ctrl+Up/Down 跳转消息、从指定消息创建对话分支（含 beforeMessageId、可选 Agent）、分叉上下文字符预算压缩（优先保留全部用户消息 + 助手条目填充剩余额度） |
| [快捷操作](features/quick-actions.md) | 聊天 Quick Send、终端 Quick Commands、CRUD 与排序 |
| [RAG 检索](features/rag.md) | 文档分块（含 chunk_overlap 配置）、向量化（可独立开关）、SQLite vec0 向量索引、混合检索（含 search_mode 配置）、三级索引重建（向量重建 + 全量重建 + 独立 FTS 重建）、可配置批次大小（`rag.batch_size`）、索引磁盘占用展示、会话聚合搜索、消息聚类分析、索引进度跟踪 |
| [推送通知](features/push-notifications.md) | WebSocket 实时推送、通知音效开关（防止蓝牙耳机中断）、权限待审推送、离线事件持久化与游标拉取、钉钉/飞书企业机器人推送（Stream API + 交互式卡片/Markdown 单聊 + 会话交互命令） |
| [应用内完成通知](features/completion-popup.md) | 后台事件时滑入纯通知卡片：头部区（图标 + 主类别徽章 + 事件类型纯文字标题 + 关闭）+ 正文区（标题段 + 最多 4 行摘要）+ 跨项目整卡换色与区隔带 + 跳转（顺带标记已读），5 秒自动关闭；事件覆盖与系统通知完全对齐（会话/任务/仓库），队列上限 3 + 同仓库合并 |
| [智能体用量统计](features/usage-stats.md) | 按项目聚合 `chat_metadata` 用量行（独立台账，不随会话删除丢失）的数据统计：用量总览环形图 + 缓存命中下钻、按指标拆分的图表（bar/pie/trend 可切换）、24h/7d/30d/自定义时间窗与 model/backend/agent 筛选、费用两位小数统一、移动端纵向堆叠；数据统计页签另含代码存量/代码增量双子页（见 [Git 管理](features/git-management.md)） |
| [系统资源监控](features/system-resources.md) | gopsutil 采集 CPU/内存/磁盘/网络/负载、500ms 采样缓存、WS 按订阅需求推送（`metrics_preference` 声明速率）、可见性感知、WS 断线时显示连接状态 |

### infra/ — 基础设施

| 模块 | 说明 |
|------|------|
| [认证与中间件](infra/auth-and-middleware.md) | SHA-256 密码认证、本机 AI 短时令牌（30 分钟 HMAC，地址+签名双条件）、隧道场景信任边界限制、按路由认证、API 密钥加密（`agent_api_keys` 已移除）、请求链（含 NoCache）、panic 恢复 |
| [国际化](infra/i18n.md) | go-i18n bundle、嵌入式 YAML 翻译、X-Locale/Cookie/Accept-Language 优先级链、推送通知独立 Localizer |
| [SSH 隧道](infra/ssh-tunnel.md) | direct-tcpip 端口映射、密码认证、自动 host key、暴力破解防护、端口白名单默认 1024-65535（ISS-186 修复）、端点按受众拆分（公开 `/api/ssh/info` 仅端口发现，`/api/ssh/info/full` 需鉴权） |
| [FRP 隧道](infra/frp-tunnel.md) | 进程内 FRP 客户端、状态机生命周期、代理配置热重载 vs 通用配置重启、自动端口分配、WS 事件广播、双认证级别 API |
| [Proxy 注册表](infra/proxy.md) | 反向代理、Host 头重写、特权端口映射、前端端口展示、CORS 代理（Swagger UI "Try it out"） |
| [配置与自动发现](infra/config-and-discovery.md) | 零配置启动、DB-backed Agent 存储、双传输选择、供应商注册表、Model 自动发现（含 Kimi 与 Codex 自定义模型发现函数）、ACP 运行时模型验证、多实例 Cookie 隔离、TLS 证书自动发现、Schema 迁移、默认项目持久化、配置连通性测试、覆盖率门禁 |
| [事件体系](infra/event-system.md) | ws.Manager 系统广播、StreamHub 会话扇出、断线缓冲重放、投递丢弃计数（`/api/ws/delivery-stats`）与关键事件可靠投递、摘要与权限事件推送 |
| [应用自升级](infra/self-upgrade.md) | 版本检查、安装目录可写预检、镜像 tarball URL 归一化、备份替换、进度推送、服务重启与断线轮询、容器内强制就地替换 |
| [版本号策略](infra/versioning.md) | versionCode（`major*1e8+minor*1e5+patch*1e3+distance`，决定 Android 能否覆盖安装）与 versionName（仅展示）两套口径；CI 走 `--tag-only` 只拉 tag ref 不拉历史，本地走 `git describe` 含 distance；位宽防 `v0.100.0`/`v1.0.0` 撞码；release 带 `--assert` 防退化 |
| [本地文件服务](infra/local-file-serving.md) | `/api/local-file/` 路径编码、媒体预览、下载与访问边界、目录树列表、批量文件存在检查、批量图片 Base64 |
| [Docker 部署](infra/docker-deployment.md) | 单阶段运行时镜像、数据卷持久化、GHCR 双架构发布、容器内升级提示镜像优先 |
| [系统资源监控](infra/system-resources.md) | CPU/内存/磁盘/磁盘 I/O/网络/系统负载实时采集、gopsutil 采样、500ms 缓存、MetricsPusher 按订阅需求推送（非缓冲投递）、前台/后台双速、AppHeader 压力指示图标、WS 断线状态展示、Gauge 弹出面板 |
| [CLI 子命令](infra/cli-reference.md) | 仅剩 upgrade-replace（应用自升级内部机制）；业务子命令 task/rag 已移除，改由内置斜杠命令直调 HTTP API |
| [Bugfix 工作流](infra/bugfix-workflow.md) | 自动化 bugfix 生命周期：扫描分类→worktree 隔离修复→测试验证→PR+CI→合并关闭 |

### api/ — API 规格

| 模块 | 说明 |
|------|------|
| [OpenAPI 规格](../../internal/api/openapi.yaml) | 完整 OpenAPI 3.0 单文件（150 路径 / 189 操作）：所有 HTTP 端点、鉴权标注、统一错误体、请求/响应 schema；WebSocket 与 SSE 端点以说明形式收录。源文件已迁至 `internal/api/openapi.yaml` 以支持 `go:embed`（详见 [API 文档说明](api/README.md)） |

### client/ — 客户端

| 模块 | 说明 |
|------|------|
| [前端架构](client/frontend-architecture.md) | 单页布局、reactive store、composable 模式、统一 WebSocket 单通道、聊天渲染管线（useChatRender + useMarkdownRenderer + 数学块提取保护）、交互式提问卡（AskUserQuestion 选项卡片 + 单选可取消 + 答案状态跨重渲染持久化）、ACP 会话管理（含 context_state 持久化恢复）、标注管道（文件路径 + localhost URL + commit hash + Worktree）、thinking 惰性加载（useThinkingContent）、CodeMirror 代码编辑器（浏览/编辑双模式 + sticky scroll）、Excalidraw 画布编辑（iframe 内嵌独立构建 + postMessage）、会话重置、终端三模式手势 + 选择模式、终端帮助抽屉、统一搜索控件（SearchBar + CodeMirror 搜索面板 + Markdown 内嵌搜索条）、Read 工具行范围展示、流式渲染帧调度（StreamFrameScheduler）、前台恢复自包含重连、appLog 强制日志规范、历史加载 DB 权威重建（陈旧快照下按"会话在跑 + 快照无 streaming 行"保留活跃占位符）、代码链接预览（useCodeLinkPreview 单例状态机 + CodeLinkPreview 浮层/底部抽屉，详见前端架构规格）、FileHeader 三层弹性布局、键盘交互（DialogOverlay/BottomSheet Esc/Enter）、系统资源面板、统一返回状态机、文件/Agent/Provider 图标、会话搜索抽屉、WS 断线连接状态、消息聚类抽屉、LocalLinkGuard 全局链接拦截、文本选择感知、消息排队与 needs_start 重提交、文件刷新与差异高亮、Diff 前后导航、快捷键提示系统（shortcutTips）、会话身份管理（useSessionIdentity）、文件上传管理（useFileUpload）、异步组件重试（useAsyncComponent）、36 命名主题系统（data-theme 机制 + 快捷选择器 + 外观深链 + logo 深链「关于」 + 实时跟随系统）、自定义壁纸背景（半透明层透出 + 文件页"设置为主题背景"入口）、自定义字体双通道（代码/界面 + 备选）、TOC 停靠栏左右侧切换、宽屏聊天区切换、Dock 数据统计页签（StatsTabHost 三子页：用量/代码存量/代码增量）、统一刷新按钮（RefreshButton）、会话列表扁平化、Git 历史视图共享逻辑（useGitHistoryView）、Android 输入恢复（useSelectAllDeleteRecovery）、计数角标统一（.count-badge） |
| [统一返回与跨界面导航](client/unified-back-navigation.md) | 两级分层栈（界面内文件历史 useFileNavStack + 跨界面 jump origin useNavigationContext）、useNavigationStateMachine 确定性优先级裁决、useNavigationCoordinator 全局文件打开唯一入口、目录游历事务 useDirectoryReturn、滚动/阅读位置精准还原 useFileScrollRestore、双击退出协议、多端返回入口（桌面顶栏导航簇 / 移动底部悬浮胶囊 / 右缘手势 / Android 物理键） |
| [Android 集成](client/android-integration.md) | JS Bridge（25+ 方法）、12 个 Java 类模块（BackgroundService / PendingEventsWorker / FloatingStatusView 悬浮状态窗 + 会话面板 / LiveUpdateManager 实时更新等）、Android 全量国际化、APK 嵌入（`build.sh --android` → `go:embed` → `/api/apk`）、AppLog 兼容日志端点、推送感知生命周期、版本不匹配 Overlay、硬件返回键同步委托（evaluateJavascript 读 `__clawbenchBackHandled` + 双击退出） |
| [多服务器管理](client/multi-server.md) | 服务器列表、凭据保存、登录页选择、应用内快速切换 |
| [客户端安装与 App 模式](client/install-and-app-mode.md) | PWA 安装、iOS 手动安装、APK 下载与原生模式识别 |

## 参考资料

`docs/dev/` 存放各 AI 后端的工具定义、调用样例与协议逆向分析，供接入/排查时查阅。

| 文档 | 说明 |
|------|------|
| [CodeBuddy ACP 非标准扩展接口清单](../dev/codebuddy_acp_extensions.md) | `codebuddy --acp` 在 ACP v1 之外的扩展面：17 个 `extMethod` 请求、19 个自定义 `session/*` 方法（steer / 消息队列 / inject_history / 回滚 / multitask）、10 个 agent→client 扩展通知、7 类非标准 `sessionUpdate`、163 个 `codebuddy.ai/*` meta key、`initialize` 双向能力协商与各接口门控条件；含**真机探针实测结论**（`session/steer` 可用、裸 JSON-RPC 通道可行、`_codebuddy.ai/question` 在 stdio 下不触发） |
| [Claude ACP 扩展面（与 CodeBuddy 对比）](../dev/claude_acp_extensions.md) | `claude-agent-acp` 是桥接 Claude Agent SDK 的适配器，**无 `extMethod` 兜底**：无 steer / 队列 / inject_history 等私有方法，`sessionUpdate` 全为标准类型，扩展仅靠 `_meta`（`claudeCode` / `_claude/origin` / `terminal_*`）；但有内建 prompt 排队（`promptQueueing`，并发调标准 `session/prompt`）——注意 ClawBench 现有 `RegisterSession` 覆盖写不支持并发 |

## 核心技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.25+、SQLite（WAL + vec0 向量索引）、robfig/cron、gotreesitter（符号提取）、gopsutil（系统资源）、fatedier/frp（进程内 FRP 客户端）、go-i18n/v2（国际化） |
| 前端 | Vue 3 + TypeScript、Vite、CodeMirror（代码浏览+编辑）、xterm.js、marked + hljs（选择性语言注册）、KaTeX（字符串级渲染）、vue-draggable-plus |
| AI 集成 | Shell-out 到 CLI 工具、ACP JSON-RPC over stdio、stream-json 解析 |
| 实时通信 | WebSocket `/api/ai/events/ws`（统一推送：聊天 + 系统事件 + 摘要 + 推荐待审 + 权限待审 + replay_done + cluster_progress，`StreamHub` 会话级扇出）、旁注小通道（`/api/file/watch/ws`、`/api/tts/audio/ws`、`/api/stt/transcribe/ws` WS；`/api/dir/search` SSE）、SSH（端口映射） |
| 安全 | SHA-256 密码存储、AES-256-GCM API 密钥加密（`agent_api_keys` 已移除）、HKDF-SHA256 密钥派生 |
| 移动端 | Android WebView、原生后台服务 |
