# ClawBench 系统设计规格

ClawBench 是一个移动优先的 AI 工作站，将多种 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、Cline、Kimi、Copilot、MiMo-Code、Pi）包装为 Web 可访问的平台。Go 后端通过 shell 调用 CLI 工具并经 WebSocket 流式输出 JSON，同时支持 ACP（Agent Client Protocol）stdio 传输，提供结构化的模式切换、斜杠命令和权限管理。Vue 3 前端实时渲染流式事件。支持 SSH 隧道端口转发、定时任务系统、零配置启动引导、聊天自动摘要和系统资源监控。

## 模块地图

### core/ — 核心业务

| 模块 | 说明 |
|------|------|
| [聊天流程](core/chat-flow.md) | 用户发消息到 AI 回复的完整链路：handler → SessionExecutor → AI 后端 → WebSocket StreamHub → 前端；含 ACP 权限审批、@chatsearch/@task 命令注入、文件附件行范围、自动摘要 |
| [AI 后端抽象](core/ai-backend.md) | 双传输后端（CLI shell-out + ACP stdio）、acpStdoutFilter 协议修复、CodeWhale ACP 字段重映射、共享规则模板、连接管理、LoadSession 异步回放 |
| [流式传输体系](core/streaming.md) | 单一 WebSocket StreamHub（含断线 ≤10s 缓冲重放、≤50 条上限、>120s 清理订阅）+ 旁注小 SSE/WS 通道（file_watch/dir_search/tts_audio）；含 replay_done 事件 |
| [会话生命周期](core/session-lifecycle.md) | 聊天会话的创建、执行、排队、取消、续接对话、分叉、设置即时持久化全流程 |
| [摘要管线](core/summarization.md) | 双管线（TTS vs 阅读摘要）、多 pass 压缩、Block 提取算法、降级链、热重载 |

### features/ — 功能特性

| 模块 | 说明 |
|------|------|
| [首次访问欢迎面板](features/setup-wizard.md) | WelcomeOverlay 后端检测面板（非 5 步向导）；Agent 创建走自动发现 + AgentInstallDialog；27 个 LLM 供应商规格 |
| [定时任务](features/scheduled-tasks.md) | cron 调度 → AI 执行 → 摘要推送，支持暂停/恢复/手动触发/续接对话 |
| [语音合成](features/tts.md) | 多引擎 TTS（云/本地），文本清理，缓存策略 |
| [Web 终端](features/terminal.md) | PTY 多标签会话、WebSocket 双向通信、手势与虚拟键盘、键位/符号配置、TUI 应用支持 |
| [Git 管理](features/git-management.md) | 历史浏览、Worktree 隔离、分支/标签 CRUD、滑动手势删除 |
| [文件管理](features/file-management.md) | 浏览+覆盖层预览合一、二进制文件处理（64KB/512KB 截断 + forceText）、目录导航栈、双候选路径解析、编辑、上传、代码符号提取、归档打包 |
| [文件发现](features/file-discovery.md) | 全项目文件搜索（默认非递归）、最近文件、统一覆盖层打开行为 |
| [附件与系统分享](features/attachments-and-share.md) | 多文件附件（含行范围）、上传历史、Share In、缩略图与项目隔离 |
| [会话导航与分叉](features/session-navigation.md) | 用户消息索引、跨分页定位、从指定消息创建对话分支 |
| [快捷操作](features/quick-actions.md) | 聊天 Quick Send、终端 Quick Commands、CRUD 与排序 |
| [RAG 检索](features/rag.md) | 文档分块、向量化（可独立开关）、SQLite vec0 向量索引、混合检索、会话聚合搜索、索引进度跟踪 |
| [推送通知](features/push-notifications.md) | WebSocket 实时推送、权限待审推送、事件缓冲回放、钉钉企业机器人 Stream API + Markdown 单聊 |

### infra/ — 基础设施

| 模块 | 说明 |
|------|------|
| [认证与中间件](infra/auth-and-middleware.md) | SHA-256 密码认证、可配置 localhost 旁路、按路由认证、API 密钥加密、请求链（含 NoCache）、panic 恢复 |
| [SSH 隧道](infra/ssh-tunnel.md) | direct-tcpip 端口转发、密码认证、自动 host key、暴力破解防护、端口白名单默认 1024-65535（ISS-186 修复）、状态查询走 `/api/ssh/info` |
| [FRP 隧道](infra/frp-tunnel.md) | 进程内 FRP 客户端、状态机生命周期、代理配置热重载 vs 通用配置重启、自动端口分配、WS 事件广播、双认证级别 API |
| [Proxy 注册表](infra/proxy.md) | 反向代理、Host 头重写、特权端口映射、前端端口展示 |
| [配置与自动发现](infra/config-and-discovery.md) | 零配置启动、DB-backed Agent 存储、双传输选择、供应商注册表、Model 自动发现、多实例 Cookie 隔离、Schema 迁移、默认项目持久化、覆盖率门禁 |
| [事件体系](infra/event-system.md) | ws.Manager 系统广播、StreamHub 会话扇出、断线缓冲重放、摘要与权限事件推送 |
| [应用自升级](infra/self-upgrade.md) | 版本检查、备份替换、进度推送、服务重启与断线轮询 |
| [本地文件服务](infra/local-file-serving.md) | `/api/local-file/` 路径编码、媒体预览、下载与访问边界 |
| [Docker 部署](infra/docker-deployment.md) | 单阶段运行时镜像、数据卷持久化、GHCR 双架构发布、容器内禁用自升级 |
| [CLI 子命令](infra/cli-reference.md) | HTTP API 路由架构、task/rag/upgrade-replace 子命令、@path 文件语法、防递归守卫、Cookie Token 认证 |
| [Bugfix 工作流](infra/bugfix-workflow.md) | 自动化 bugfix 生命周期：扫描分类→worktree 隔离修复→测试验证→PR+CI→合并关闭 |

### client/ — 客户端

| 模块 | 说明 |
|------|------|
| [前端架构](client/frontend-architecture.md) | 单页布局、reactive store、composable 模式、统一 WebSocket 单通道、ACP 会话管理、标注管道、appLog 强制日志规范、FileHeader 三层弹性布局、系统资源面板 |
| [Android 集成](client/android-integration.md) | JS Bridge（25+ 方法）、9 个 Java 类模块（BackgroundService / PendingEventsWorker / BootCompletedReceiver 等）、APK 嵌入（`build.sh --android` → `go:embed` → `/api/apk`）、AppLog 兼容日志端点、推送感知生命周期 |
| [多服务器管理](client/multi-server.md) | 服务器列表、凭据保存、登录页选择、应用内快速切换 |
| [客户端安装与 App 模式](client/install-and-app-mode.md) | PWA 安装、iOS 手动安装、APK 下载与原生模式识别 |

## 核心技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.25+、SQLite（WAL + vec0 向量索引）、robfig/cron、gotreesitter（符号提取）、gopsutil（系统资源） |
| 前端 | Vue 3 + TypeScript、Vite、xterm.js、marked + hljs |
| AI 集成 | Shell-out 到 CLI 工具、ACP JSON-RPC over stdio、stream-json 解析 |
| 实时通信 | WebSocket `/api/ai/events/ws`（统一推送：聊天 + 系统事件 + 摘要 + 权限待审 + replay_done，`StreamHub` 会话级扇出）、旁注小通道（`/api/file/watch`、`/api/dir/search` SSE；`/api/tts/audio/ws` WS）、SSH（端口转发） |
| 安全 | SHA-256 密码存储、AES-256-GCM API 密钥加密、HKDF-SHA256 密钥派生 |
| 移动端 | Android WebView、原生后台服务 |
