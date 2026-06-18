# ClawBench 系统设计规格

ClawBench 是一个移动优先的 AI 工作站，将多种 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、Cline、Kimi、Copilot、MiMo-Code、Pi）包装为 Web 可访问的平台。Go 后端通过 shell 调用 CLI 工具并经 SSE 流式输出 JSON，同时支持 ACP（Agent Client Protocol）stdio 传输，提供结构化的模式切换、斜杠命令和权限管理。Vue 3 前端实时渲染流式事件。支持 SSH 隧道端口转发、定时任务系统、零配置启动引导和聊天自动摘要。

## 模块地图

### core/ — 核心业务

| 模块 | 说明 |
|------|------|
| [聊天流程](core/chat-flow.md) | 用户发消息到 AI 回复的完整链路：handler → SessionExecutor → AI 后端 → SSE → 前端；含 ACP 权限审批、自动摘要 |
| [AI 后端抽象](core/ai-backend.md) | 双传输后端（CLI shell-out + ACP stdio）、统一接口、流式解析、AutoResume 机制、连接管理 |
| [流式传输体系](core/streaming.md) | SSE 推送与重连策略、WebSocket 事件通道（含摘要+权限推送）、ACP 状态事件、HTTP 轮询降级 |
| [会话生命周期](core/session-lifecycle.md) | 聊天会话的创建、执行、排队、取消、续接对话、设置即时持久化全流程 |

### features/ — 功能特性

| 模块 | 说明 |
|------|------|
| [设置向导](features/setup-wizard.md) | 首次启动引导：5 步创建 Agent，支持 27+ LLM 供应商和自定义 URL |
| [定时任务](features/scheduled-tasks.md) | cron 调度 → AI 执行 → 摘要推送，支持暂停/恢复/手动触发/续接对话 |
| [语音合成](features/tts.md) | 多引擎 TTS（云/本地），文本清理，缓存策略 |
| [Web 终端](features/terminal.md) | PTY 多标签会话、WebSocket 双向通信、手势与虚拟键盘、键位/符号配置、TUI 应用支持 |
| [Git 管理](features/git-management.md) | 历史浏览、Worktree 隔离、分支/标签 CRUD、滑动手势删除 |
| [文件管理](features/file-management.md) | 浏览+覆盖层预览合一、目录导航栈、双候选路径解析、编辑、上传、代码符号提取、归档打包 |
| [RAG 检索](features/rag.md) | 文档分块、向量化（BGE-M3）、SQLite vec0 向量索引、混合检索 |
| [推送通知](features/push-notifications.md) | JPush 集成、WebSocket 后备、权限待审推送、推送感知的后台策略 |

### infra/ — 基础设施

| 模块 | 说明 |
|------|------|
| [认证与中间件](infra/auth-and-middleware.md) | SHA-256 密码认证、localhost 旁路、API 密钥加密、请求链、panic 恢复 |
| [SSH 隧道](infra/ssh-tunnel.md) | direct-tcpip 端口转发、密码认证、自动 host key、暴力破解防护 |
| [Proxy 注册表](infra/proxy.md) | 反向代理、Host 头重写、特权端口映射、前端端口展示 |
| [配置与自动发现](infra/config-and-discovery.md) | 零配置启动、DB-backed Agent 存储、双传输选择、供应商注册表、Model 自动发现 |
| [事件体系](infra/event-system.md) | EventBus、WebSocket 广播、JPush 后备、断线缓冲重放、摘要+权限事件推送 |

### client/ — 客户端

| 模块 | 说明 |
|------|------|
| [前端架构](client/frontend-architecture.md) | 单页布局、reactive store、composable 模式、SSE/WebSocket 双通道、ACP 会话管理、标注管道 |
| [Android 集成](client/android-integration.md) | JS Bridge、后台服务、SSH 端口转发、推送感知生命周期 |

## 核心技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.23+、SQLite（WAL + vec0 向量索引）、robfig/cron、gotreesitter（符号提取） |
| 前端 | Vue 3 + TypeScript、Vite、xterm.js、marked + hljs |
| AI 集成 | Shell-out 到 CLI 工具、ACP JSON-RPC over stdio、stream-json 解析 |
| 实时通信 | SSE（聊天流）、WebSocket（系统事件+摘要推送+权限待审）、SSH（端口转发） |
| 安全 | SHA-256 密码存储、AES-256-GCM API 密钥加密、HKDF-SHA256 密钥派生 |
| 移动端 | Android WebView、JPush 推送、原生后台服务 |
