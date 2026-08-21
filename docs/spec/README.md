# ClawBench 系统设计规格

ClawBench 是移动优先的 AI 工作站，将多种 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、Kimi、Copilot、MiMo-Code、Pi、Antigravity、Grok Build）包装为 Web 可访问的平台。Go 后端通过 shell 调用 CLI 工具并经 WebSocket 流式输出 JSON，同时支持 ACP（Agent Client Protocol）stdio 传输，提供结构化的模式切换、斜杠命令和权限管理。Vue 3 前端实时渲染流式事件。支持 SSH 隧道端口映射、FRP 公网隧道、定时任务系统、零配置启动引导、聊天自动摘要、钉钉/飞书企业推送、系统资源监控、thinking 惰性加载和消息聚类分析。

## 模块地图

### core/ — 核心业务

| 模块 | 说明 |
|------|------|
| [聊天流程](core/chat-flow.md) | 用户发消息到 AI 回复的完整链路：handler → SessionExecutor → AI 后端 → WebSocket StreamHub → 前端；含 ACP 权限审批、@chatsearch/@task 命令注入、文件附件行范围、自动摘要（AI 失败降级结论文本）、分叉上下文仅截断工具输出、thinking 惰性加载、工具调用耗时 |
| [AI 后端抽象](core/ai-backend.md) | 双传输后端（CLI shell-out + ACP stdio）、流式事件累加（AccumulateBlock + 回放检测 + 连续 thinking 合并 + AskQuestion 转换）、ACP 状态提取（mode/thinking/model）、ACP 崩溃诊断、acpStdoutFilter 协议修复（含 SessionModelState 提取）、ACP context_state 持久化、ACP 会话恢复重试与 NewSessionFallback、raw_output 累积缓冲、thinking 惰性加载、CodeWhale 字段重映射、Grok Build 双传输（ACP + streaming-json CLI）、共享规则模板、连接管理（AgentID/BackendID 无锁防死锁、用户取消保护存活连接、ensureAliveWithSession 使用 ResumeSession）、LoadSession 异步回放、ListSessions 磁盘扫描回退、EnsureAlive、CodeBuddy MCP 配置注入、CodeBuddy Plugin Skills 竞态修复 |
| [流式传输体系](core/streaming.md) | 单一 WebSocket StreamHub（含断线 ≤10s 缓冲重放、≤50 条上限、>120s 清理订阅）+ 旁注小 SSE/WS 通道；含前端重连状态同步、subscribeOnly 模式、replay_done 事件 |
| [会话生命周期](core/session-lifecycle.md) | 聊天会话的创建、执行、排队、取消、归档（软删除）、物理删除（Destroy）、续接对话、分叉（含 beforeMessageId、可选 Agent）、设置即时持久化、过期归档自动清理 |
| [摘要管线](core/summarization.md) | 双管线（TTS vs 阅读摘要）、summarizeTarget 统一调度、SummaryCards 结构化卡片、多 pass 压缩、Block 提取算法、降级链（AI 失败使用结论文本）、热重载、推荐回复（stable/rolling 分离 + prompt caching） |

### features/ — 功能特性

| 模块 | 说明 |
|------|------|
| [首次访问欢迎面板](features/setup-wizard.md) | WelcomeOverlay 后端检测面板（非 5 步向导）；Agent 创建走自动发现 + AgentInstallDialog；13 个后端规格 |
| [定时任务](features/scheduled-tasks.md) | cron 调度 → AI 执行 → 摘要推送，支持暂停/恢复/手动触发/续接对话，运行中流式状态展示 |
| [语音合成](features/tts.md) | 多引擎 TTS（云/本地），文本清理，缓存策略 |
| [语音输入](features/stt.md) | 双模式语音识别（流式 WS + 非流式 POST）、vLLM Whisper 引擎、增量识别 + 最终全量、安全上下文检测、快捷键触发 |
| [推荐回复](features/chat-recommendation.md) | AI 回复完成后自动生成下一步建议、stable/rolling 分离支持 prompt caching、快捷指令感知、离线恢复、会话隔离 |
| [Web 终端](features/terminal.md) | PTY 多标签会话（独立进程组防 /dev/tty 阻塞）、三模式手势系统（浏览/手势/选择）、拖拽选择+浮动复制栏、虚拟修饰键、键位/符号配置、终端主题切换、终端输入抽屉、终端帮助抽屉、TUI 应用支持 |
| [Git 管理](features/git-management.md) | 历史浏览、文件 Diff 抽屉（prev/next 顺序导航）、Worktree 隔离、分支/标签 CRUD、内联操作按钮 |
| [文件管理](features/file-management.md) | 目录浏览（browse）+ 文件查看（view）独立 Tab、CodeMirror 代码编辑（浏览/编辑双模式）、VS Code 风格 sticky scroll、Markdown 标题锚定滚动同步、内联音频/视频播放器、二进制文件处理（64KB/512KB 截断 + forceText）、目录导航栈、双候选路径解析、文件刷新与差异高亮（useFileRefresh 统一三种触发 + Markdown 块级差异 + 代码行级差异 + 两阶段闪烁）、刷新跳过加载遮罩、编辑、上传（含文件夹上传/目录树下载/粘贴上传）、目录跳转、拖放移动、面包屑拖拽到聊天、排序、网格视图、键盘快捷键、代码符号提取、归档打包 |
| [文件发现](features/file-discovery.md) | 全项目文件搜索（默认非递归）、最近文件、统一覆盖层打开行为 |
| [附件与系统分享](features/attachments-and-share.md) | 多文件附件（含行范围）、上传历史（支持删除）、Share In（支持删除）、文件夹上传（保持目录结构）、目录树下载（File System Access API）、粘贴上传、面包屑拖拽附件、缩略图与项目隔离 |
| [会话导航与分叉](features/session-navigation.md) | 用户消息索引、跨分页定位、Ctrl+Up/Down 跳转消息、从指定消息创建对话分支（含 beforeMessageId、可选 Agent） |
| [快捷操作](features/quick-actions.md) | 聊天 Quick Send、终端 Quick Commands、CRUD 与排序 |
| [RAG 检索](features/rag.md) | 文档分块（含 chunk_overlap 配置）、向量化（可独立开关）、SQLite vec0 向量索引、混合检索（含 search_mode 配置）、两级索引重建（向量重建 + 全量重建）、会话聚合搜索、消息聚类分析、索引进度跟踪 |
| [推送通知](features/push-notifications.md) | WebSocket 实时推送、通知音效开关（防止蓝牙耳机中断）、权限待审推送、离线事件持久化与游标拉取、钉钉/飞书企业机器人推送（Stream API + 交互式卡片/Markdown 单聊 + 会话交互命令） |
| [系统资源监控](features/system-resources.md) | gopsutil 采集 CPU/内存/磁盘/网络/负载、500ms 采样缓存、可见性感知轮询、WS 断线时显示连接状态 |

### infra/ — 基础设施

| 模块 | 说明 |
|------|------|
| [认证与中间件](infra/auth-and-middleware.md) | SHA-256 密码认证、可配置 localhost 旁路、按路由认证、API 密钥加密（`agent_api_keys` 已移除）、请求链（含 NoCache）、panic 恢复 |
| [国际化](infra/i18n.md) | go-i18n bundle、嵌入式 YAML 翻译、X-Locale/Cookie/Accept-Language 优先级链、推送通知独立 Localizer |
| [SSH 隧道](infra/ssh-tunnel.md) | direct-tcpip 端口映射、密码认证、自动 host key、暴力破解防护、端口白名单默认 1024-65535（ISS-186 修复）、状态查询走 `/api/ssh/info` |
| [FRP 隧道](infra/frp-tunnel.md) | 进程内 FRP 客户端、状态机生命周期、代理配置热重载 vs 通用配置重启、自动端口分配、WS 事件广播、双认证级别 API |
| [Proxy 注册表](infra/proxy.md) | 反向代理、Host 头重写、特权端口映射、前端端口展示、CORS 代理（Swagger UI "Try it out"） |
| [配置与自动发现](infra/config-and-discovery.md) | 零配置启动、DB-backed Agent 存储、双传输选择、供应商注册表、Model 自动发现（含 Kimi 模型发现函数）、ACP 运行时模型验证、多实例 Cookie 隔离、TLS 证书自动发现、Schema 迁移、默认项目持久化、配置连通性测试、覆盖率门禁 |
| [事件体系](infra/event-system.md) | ws.Manager 系统广播、StreamHub 会话扇出、断线缓冲重放、摘要与权限事件推送 |
| [应用自升级](infra/self-upgrade.md) | 版本检查、备份替换、进度推送、服务重启与断线轮询 |
| [本地文件服务](infra/local-file-serving.md) | `/api/local-file/` 路径编码、媒体预览、下载与访问边界、目录树列表、批量文件存在检查、批量图片 Base64 |
| [Docker 部署](infra/docker-deployment.md) | 单阶段运行时镜像、数据卷持久化、GHCR 双架构发布、容器内禁用自升级 |
| [系统资源监控](infra/system-resources.md) | CPU/内存/磁盘/磁盘 I/O/网络/系统负载实时采集、gopsutil 采样、500ms 缓存、前台/后台双速轮询、AppHeader 压力指示图标、WS 断线状态展示、Gauge 弹出面板 |
| [CLI 子命令](infra/cli-reference.md) | HTTP API 路由架构、task/rag/upgrade-replace 子命令、@path 文件语法、防递归守卫、Cookie Token 认证 |
| [Bugfix 工作流](infra/bugfix-workflow.md) | 自动化 bugfix 生命周期：扫描分类→worktree 隔离修复→测试验证→PR+CI→合并关闭 |

### client/ — 客户端

| 模块 | 说明 |
|------|------|
| [前端架构](client/frontend-architecture.md) | 单页布局、reactive store、composable 模式、统一 WebSocket 单通道、聊天渲染管线（useChatRender + useMarkdownRenderer + 数学块提取保护）、ACP 会话管理（含 context_state 持久化恢复）、标注管道（文件路径 + localhost URL + commit hash + Worktree）、thinking 惰性加载（useThinkingContent）、CodeMirror 代码编辑器（浏览/编辑双模式 + sticky scroll）、终端三模式手势 + 选择模式、终端帮助抽屉、搜索工具集、Read 工具行范围展示、流式渲染帧调度（StreamFrameScheduler）、前台恢复自包含重连、appLog 强制日志规范、FileHeader 三层弹性布局、键盘交互（DialogOverlay/BottomSheet Esc/Enter）、系统资源面板、边缘滑动返回、文件/Agent/Provider 图标、会话搜索抽屉、WS 断线连接状态、消息聚类抽屉、LocalLinkGuard 全局链接拦截、文本选择感知、消息排队与 needs_start 重提交、文件刷新与差异高亮、Diff 前后导航、快捷键提示系统（shortcutTips）、会话身份管理（useSessionIdentity）、文件上传管理（useFileUpload）、异步组件重试（useAsyncComponent） |
| [Android 集成](client/android-integration.md) | JS Bridge（25+ 方法）、9 个 Java 类模块（BackgroundService / PendingEventsWorker / BootCompletedReceiver 等）、APK 嵌入（`build.sh --android` → `go:embed` → `/api/apk`）、AppLog 兼容日志端点、推送感知生命周期、版本不匹配 Overlay |
| [多服务器管理](client/multi-server.md) | 服务器列表、凭据保存、登录页选择、应用内快速切换 |
| [客户端安装与 App 模式](client/install-and-app-mode.md) | PWA 安装、iOS 手动安装、APK 下载与原生模式识别 |

## 核心技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.25+、SQLite（WAL + vec0 向量索引）、robfig/cron、gotreesitter（符号提取）、gopsutil（系统资源）、fatedier/frp（进程内 FRP 客户端）、go-i18n/v2（国际化） |
| 前端 | Vue 3 + TypeScript、Vite、CodeMirror（代码浏览+编辑）、xterm.js、marked + hljs（选择性语言注册）、KaTeX（字符串级渲染）、vue-draggable-plus |
| AI 集成 | Shell-out 到 CLI 工具、ACP JSON-RPC over stdio、stream-json 解析 |
| 实时通信 | WebSocket `/api/ai/events/ws`（统一推送：聊天 + 系统事件 + 摘要 + 推荐待审 + 权限待审 + replay_done + cluster_progress，`StreamHub` 会话级扇出）、旁注小通道（`/api/file/watch`、`/api/dir/search` SSE；`/api/tts/audio/ws`、`/api/stt/transcribe/ws` WS）、SSH（端口映射） |
| 安全 | SHA-256 密码存储、AES-256-GCM API 密钥加密（`agent_api_keys` 已移除）、HKDF-SHA256 密钥派生 |
| 移动端 | Android WebView、原生后台服务 |
