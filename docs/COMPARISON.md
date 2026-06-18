[中文](COMPARISON.md) | [English](COMPARISON.en.md)

# 竞品对比

移动端 AI 编程工具全景对比，涵盖开源与闭源产品。

## 竞品概览

| 产品 | 类型 | 定位 | 开源 | Stars |
|------|------|------|------|-------|
| **ClawBench** | 自部署 Web 工作台 | 移动端 AI 工作台（文件+代码+AI+Git+调度） | ✅ MIT | — |
| **Operit** | 原生 Android AI Agent | 移动端全功能 AI Agent（内置 Ubuntu + 40+工具 + 本地模型） | ✅ LGPLv3 | 4.4k |
| **Yep Anywhere** | 自部署 Web UI | 手机监控/交互 Claude Code 和 Codex Agent | ✅ MIT | 380 |
| **Happy** | 远程遥控器 | 手机远程操控电脑上运行的 Claude Code/Codex 会话 | ✅ MIT | 19.9k |
| **AnyCoding** | 远程遥控器 | 手机原生 Android 终端遥控 AI CLI（同 PTY 进程） | ✅ Apache 2.0（客户端） | 9 |
| **AI Agent Remote** | 远程遥控器 | 浏览器远程控制 AI Agent CLI（Claude/Qwen/Gemini） | ✅ MIT | — |
| **FeiLaude** | IM 机器人遥控器 | 飞书 Bot 桥接本地 Claude Code（消息驱动执行+流式状态推送） | ✅ MIT | 2 |
| **Claude Dispatch** | 官方远程控制 | Anthropic 官方手机遥控 Claude Code（Cowork 家族） | ❌ 闭源 | — |
| **Claude Remote** | 远程遥控器 | 非官方 Claude Code 远程控制，支持 API 用户 | ✅ | 30 |
| **Paseo** | 代理编排平台 | 多设备 AI 代理编排平台（Daemon+Relay，跨手机/桌面/CLI 控制多个 AI Agent） | ✅ AGPL-3.0 | 5.5k |
| **Cursor Background Agent** | 云端异步 Agent | 网页/手机提交任务，云端异步执行，查看结果/创建 PR | ❌ 闭源 | — |
| **GitHub Copilot** | 官方集成 | GitHub 移动端 + 网页端 AI 编程助手 | ❌ 闭源 | — |

## 功能矩阵

| 功能 | ClawBench | Operit | Yep Anywhere | Happy | AnyCoding | AI Agent Remote | FeiLaude | Claude Dispatch | Claude Remote | Cursor Agent | GitHub Copilot | Paseo |
|------|-----------|--------|-------------|-------|-----------|-----------------|----------|-----------------|---------------|--------------|----------------|-------|
| **AI 后端数量** | 12 | 10+ | 2 | 2 | 2+ | 5 | 1 | 1 | 1 | 内置 | 内置 | 3 |
| **文件浏览/编辑** | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **代码预览/语法高亮** | ✅ | ✅ | ✅（服务端渲染） | ❌ | ✅（终端） | ✅（终端） | ❌ | ❌ | ✅ | ✅ | ✅ | ❌ |
| **Git 集成** | ✅（分支图/Diff/历史） | ✅（基础） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（PR 创建/Diff） | ✅（PR Review） | ❌（仅 Diff 查看） |
| **Markdown 渲染** | ✅（KaTeX/Mermaid） | ✅（KaTeX/Mermaid） | ✅（服务端渲染） | ❌ | ❌ | ❌ | ✅（飞书卡片） | ❌ | ✅ | ❌ | ❌ | ❌ |
| **定时任务（Cron）** | ✅ | ✅（工作流定时触发） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **TTS 语音合成** | ✅（5 种引擎） | ✅（本地+云端） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **媒体预览** | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **SSH 隧道端口转发** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **端到端加密** | ❌ | ❌ | ✅（SRP-6a+TweetNaCl） | ✅ | ✅（Direct 模式） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（ECDH+AES-256-GCM） |
| **实时语音** | ❌ | ✅（STT+TTS+语音唤醒） | ✅（语音输入） | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（Push-to-Talk STT） |
| **推送通知** | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅（飞书消息推送） | ✅ | ❌ | ❌ | ✅ | ✅ |
| **多客户端同步** | ❌ | ❌ | ✅ | ✅ | ✅（多标签+多PC） | ✅（多浏览器） | ✅（多用户） | ✅ | ✅ | ❌ | ❌ | ✅ |
| **权限审批** | ❌ | ✅（工具级权限控制） | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ | ✅ |
| **PWA 安装** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| **自部署** | ✅ | ❌（仅本地 App） | ✅ | ✅（可选） | ✅（Direct 模式） | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ✅ |
| **离线/局域网** | ✅ | ✅（本地模型+离线TTS） | ✅ | ✅（局域网） | ✅（局域网/Direct） | ✅（局域网） | ❌ | ❌ | ✅ | ❌ | ❌ | ✅（局域网直连） |
| **本地模型推理** | ❌ | ✅（MNN+llama.cpp） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **内置终端/Linux 环境** | ❌ | ✅（Ubuntu 24 chroot） | ❌ | ❌ | ✅（原生 VT100 终端） | ✅（浏览器终端） | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（内置终端） |
| **UI 自动化（操控手机）** | ❌ | ✅（无障碍/ADB/Root） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **MCP 插件生态** | ❌ | ✅（MCP 市场+技能市场） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（Daemon 作为 MCP Server） |
| **角色卡/人设系统** | ❌ | ✅（导入/导出/群聊） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **内置浏览器** | ❌ | ✅（标签页+Web自动化） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **会话分叉/克隆** | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Diff 查看** | ✅（Git Diff） | ❌ | ✅（Agent Diff） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ✅（Agent Diff） |
| **文件上传** | ✅ | ✅ | ✅（相机胶卷直传） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **远程设备串流** | ❌ | ❌ | ✅（Android WebRTC） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **IM 消息交互** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（飞书） | ❌ | ❌ | ❌ | ❌ | ❌ |
| **多工作区/多会话** | ✅（多会话） | ✅（多会话） | ✅ | ✅ | ✅ | ✅ | ✅（多工作区+多会话） | ❌ | ❌ | ❌ | ❌ | ✅（多会话+多主机） |
| **任务队列** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（自动排队+合并） | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Agent 间协作** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（Skills: handoff/loop/advisor/committee） |
| **Git Worktree 隔离** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅（自动每 Agent 隔离） |

## 架构差异

| 维度 | ClawBench | Operit | Yep Anywhere | Happy | AnyCoding | AI Agent Remote | FeiLaude | Claude Dispatch | Claude Remote | Cursor Agent | Paseo |
|------|-----------|--------|-------------|-------|-----------|-----------------|----------|-----------------|---------------|--------------|-------|
| **架构** | C/S（Go Web + SSE） | 本地 Android App | C/S（TypeScript Web + SSE） | P2P + 中继（E2E 加密同步） | C/S（WebSocket 中继） | C/S（WebSocket 桥接） | IM Bot（飞书 WebSocket） | 中心化（Anthropic 服务器中转） | WebSocket 桥接（node-pty） | 云端异步 Agent | Daemon + E2E 加密 WebSocket Relay（Cloudflare DO） |
| **AI 在哪运行** | 服务器本地 CLI | 手机本地（MNN/llama.cpp）+ 云端 API | 电脑本地 CLI（Agent SDK） | 电脑本地 CLI | 电脑本地 CLI（同 PTY） | 电脑本地 CLI | 电脑本地 CLI | 电脑本地 CLI | 电脑本地 CLI | Cursor 云端 | 电脑本地 CLI（Daemon 子进程） |
| **手机角色** | 完整工作台 | 完整 Agent（本地执行） | Agent 监控/交互器 | 遥控器 | 遥控器（原生终端） | 遥控器（浏览器终端） | 消息发送器 | 遥控器 | 遥控器 | 任务提交器 | Agent 编排器（多 Agent 协调） |
| **需电脑在线** | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **后端语言** | Go | Kotlin | TypeScript | TypeScript | JavaScript/Java/Kotlin | JavaScript | Python | — | JavaScript | — | TypeScript |
| **移动端** | 浏览器 + Android WebView | Jetpack Compose 原生 | 浏览器（Mobile-first Web） | Expo（React Native）原生 | Android 原生（Termux VT100） | 浏览器（xterm.js） | 飞书 App | 原生 App | Tauri 2.0（Android） | PWA | Expo（React Native）iOS + Android + Web + Electron 桌面 |
| **数据存储** | SQLite 本地持久化 | 本地 SQLite | CLI 会话历史（无额外 DB） | 无持久化（加密同步） | 256KB 环形缓冲 | 无 | YAML 文件 | 云端 | 无 | 云端 | 无额外持久化 |
| **部署方式** | 单二进制解压即用 | APK 安装（仅 Android） | pnpm install + Docker | npm install + App 配对 | npm install + App 配对 | npm install + 浏览器 | pip install + 飞书 App 配置 | App 登录即用 | npm install + 连接 | 浏览器登录 | npm install + Daemon 启动 |

## 各竞品详析

### Operit（GitHub: AAswordman/Operit，4.4k Stars）

开源 Android 原生 AI Agent，定位"最强移动端 AI Agent"，深度集成 Android 系统权限。

- ✅ 内置 Ubuntu 24 Linux 环境（chroot），手机直接跑 Python/Node.js
- ✅ 10+ AI 后端，含 MNN + llama.cpp 本地离线推理
- ✅ 40+ 内置工具 + MCP/技能市场插件生态
- ✅ 三通道 UI 自动化（无障碍/ADB/Root），AI 可自主操控手机
- ✅ 完整语音管线：语音唤醒 + STT + TTS + 连续对话
- ✅ 角色卡/人设系统，支持角色间群聊 + @ 提及
- ✅ 内置浏览器 + Web 自动化脚本
- ❌ 仅限 Android（无 iOS/网页版）
- ❌ 无服务端架构，不可自部署/远程访问
- ❌ 无 SSH 隧道/端口转发
- ❌ 无推送通知
- ❌ 无多客户端同步（单设备运行）
- ❌ 开源协议为 LGPLv3（较 MIT 限制更多）

### Yep Anywhere（GitHub: kzahel/yepanywhere，380 Stars）

自部署移动端 Web UI，手机监控/交互 Claude Code 和 Codex Agent，无需数据库、无云账号。

- ✅ 端到端加密远程访问（SRP-6a + TweetNaCl），中继运营方无法查看数据
- ✅ 权限审批 + 推送通知，锁屏直接响应 Agent 请求
- ✅ 会话分叉/克隆——从任意消息点分支探索不同路径
- ✅ 分层收件箱（需关注/活跃/最近/未读），高效管理多 Agent
- ✅ 全局活动流——一览所有 Agent 运行状态
- ✅ 使用 Anthropic 官方 claude-agent-sdk，合规性好
- ✅ CLI/VS Code 互操作——终端启动的会话可直接在 Web 继续使用
- ✅ 服务端渲染 Markdown/代码高亮，移动端性能好
- ✅ 远程 Android 设备串流（WebRTC），手机操控模拟器/真机
- ✅ 语音输入（浏览器 Speech API）
- ✅ 文件上传（相机胶卷直传）
- ❌ 仅支持 Claude Code + Codex（2 种后端）
- ❌ 无文件浏览/编辑、Git 集成、定时任务
- ❌ 电脑必须在线
- ❌ 无 TTS 语音合成
- ❌ 依赖 Node.js + pnpm，非单二进制部署

开源最热门的移动端 AI 编程遥控器。

- ✅ 端到端加密，隐私安全
- ✅ 实时语音、设备即时切换
- ✅ 免费开源，可自建服务器
- ✅ 支持 Claude Code + Codex
- ❌ 只能遥控，无文件浏览/编辑/Git
- ❌ 仅 2 种 AI 后端
- ❌ 电脑必须在线

### AnyCoding（GitHub: gurudin/anycoding，9 Stars）

开源远程遥控器，原生 Android 终端镜像，手机共享电脑的同一 PTY 进程。

- ✅ 原生 Android VT100 终端（Termux 渲染），体验接近桌面
- ✅ 同一 PTY 进程——手机和桌面共享完全相同的终端会话
- ✅ 多标签+多 PC 支持，一个 App 连多台电脑
- ✅ 断连自动重连 + 256KB 环形缓冲回放
- ✅ 多种网络模式：云中继（零配置）/ Direct 自托管 / 纯局域网
- ✅ 会话所有权保护——桌面启动的会话手机无法误关
- ❌ 仅遥控终端，无文件/Git/定时任务等开发环境
- ❌ 电脑必须在线
- ❌ 客户端开源，但云中继服务闭源
- ❌ 目前仅 Android（iOS 计划中）
- ❌ 项目早期阶段（9 Stars）

### AI Agent Remote（GitHub: duran4000/ai-agent-remote）

开源浏览器端远程控制，通过 WebSocket 桥接 AI Agent CLI。

- ✅ 零安装——手机浏览器直接使用
- ✅ 支持 5 种 AI Agent（Claude/Qwen/Gemini/OpenCode/iFlow）
- ✅ 多项目目录管理
- ✅ 双层认证（Token + 密码）
- ✅ 纯 Node.js 轻量栈，部署简单
- ✅ 支持 Tailscale/ZeroTier 跨网访问
- ❌ 仅遥控终端，无文件/Git/定时任务
- ❌ 电脑必须在线
- ❌ 无端到端加密
- ❌ 无推送通知
- ❌ 项目极早期（0 Stars）

### FeiLaude（GitHub: johnqxu/feilaude，2 Stars）

开源飞书 Bot 桥接本地 Claude Code，通过 IM 消息驱动 AI 执行。

- ✅ 零新 App——直接用飞书 App 交互，无需安装额外客户端
- ✅ 流式状态推送——实时显示 Claude 工具调用（读文件/编辑/执行命令）
- ✅ 智能任务队列——执行期间新消息自动排队，完成后合并提交
- ✅ 多工作区+多会话管理——每个工作区绑定不同项目目录，支持 session 切换恢复
- ✅ 飞书交互卡片——长文本自动分段，代码块完整性保护
- ✅ 任务取消——跨平台进程树清理
- ✅ 部署简单——pip install + 飞书应用配置
- ❌ 仅支持 Claude Code（1 种后端）
- ❌ 无文件浏览/编辑、Git 集成、定时任务
- ❌ 电脑必须在线
- ❌ 无端到端加密（消息经飞书服务器）
- ❌ 依赖飞书生态，非飞书用户无法使用
- ❌ 无 TTS、无语音、无媒体预览
- ❌ 项目极早期（2 Stars）

### Claude Dispatch（Anthropic 官方）

Claude Cowork 家族成员，手机远程控制 Claude Code。

- ✅ 开箱即用，界面精美
- ✅ 与 Claude 生态深度集成
- ❌ 仅限 Pro/Max 订阅用户
- ❌ 只支持 Claude，必须联网
- ❌ 数据经 Anthropic 服务器中转
- ❌ 电脑必须唤醒

### Claude Remote（GitHub: RioArisk/claudecode_api_RemoteControl）

非官方 Claude Code 远程控制方案，支持 API 用户。

- ✅ 支持 API 用户（Dispatch 不支持）
- ✅ 权限审批、模型切换
- ✅ 局域网/Tailscale/Cloudflare 多种连接方式
- ❌ 仅支持 Claude Code
- ❌ 功能简单，无文件/Git/定时任务
- ❌ 仅 Android

### Paseo（GitHub: getpaseo/paseo，5.5k Stars）

开源 AI 代理编排平台，Daemon + E2E 加密 WebSocket Relay 架构，从手机/桌面/Web/CLI 统一编排多个 AI Agent。名字源自西班牙语"散步"——随时随地查看代理状态。

- ✅ 原生移动应用：Expo (React Native) 同时支持 iOS + Android + Web + Electron 桌面，四端统一体验
- ✅ E2E 加密 Relay：ECDH 密钥交换 + AES-256-GCM，经 Cloudflare Durable Objects 中转但中继无法读/改流量，NAT 穿透零配置
- ✅ Agent 间协作 Skills 系统：handoff（交接）、loop（循环到验收）、advisor（顾问）、committee（对抗委员会），多 Agent 编排范式
- ✅ Git Worktree 一等公民：每个 Agent 自动创建独立 worktree，并行执行互不冲突
- ✅ 多主机支持：一个客户端连接多台机器的 Daemon，统一查看所有 Agent
- ✅ 语音控制：本地优先 push-to-talk STT，解放双手
- ✅ 权限审批 + 推送通知，从任何设备审批 Agent 请求
- ✅ 内置终端 + MCP Server 模式，可被其他工具集成调用
- ✅ 社区活跃：5.5k stars，86 个 release，3,000+ commits
- ❌ 仅 3 种 AI 后端（Claude Code / Codex / OpenCode），不支持 CodeBuddy、Gemini、Qoder、VeCLI
- ❌ 无文件浏览/编辑器——纯 Agent 遥控，无开发环境
- ❌ 无 Git 可视化 UI（仅 Diff 查看，无分支图/历史）
- ❌ 无定时任务（Cron 调度）
- ❌ 无 TTS 语音合成
- ❌ 无数据持久化——无 SQLite/DB，会话数据不保留
- ❌ 需电脑在线（Daemon 运行在电脑上）
- ❌ 非 Go 单二进制部署，需 Node.js + npm

### Cursor Background Agent（闭源商业）

Cursor 的云端异步 Agent，通过浏览器/手机提交任务。

- ✅ 无需本地电脑在线，云端执行
- ✅ 自动创建 PR，多模型对比
- ✅ PWA 支持，任何浏览器可用
- ❌ 闭源，依赖 Cursor 云端
- ❌ 异步模式——无法实时观看执行过程
- ❌ 需 GitHub 仓库，不支持本地项目
- ❌ 需 Cursor 付费订阅

### GitHub Copilot（闭源商业）

GitHub 官方 AI 编程助手，支持移动端。

- ✅ GitHub 生态深度集成
- ✅ 移动端 PR Review
- ❌ 无完整开发环境
- ❌ 闭源，依赖 GitHub
- ❌ 需付费订阅

## ClawBench 核心优势

1. **唯一的全功能移动端工作台**：其他产品全是"遥控器"——远程操控电脑上的会话。ClawBench 本身就是完整开发环境：文件、代码、Git、AI、定时任务、TTS、媒体预览，手机上直接干活。

2. **12 种 AI 后端**：Happy/Yep Anywhere 只有 2 种，Claude Dispatch/Remote 只有 1 种。ClawBench 支持 CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Cline、Copilot、Kimi，覆盖最广。（注：Operit 支持 10+ 后端但以 API 调用为主，ClawBench 是唯一支持 CLI Agent 的多后端工作台）

3. **不依赖电脑在线**：Happy/Dispatch/Remote/AnyCoding/AI Agent Remote/Yep Anywhere 都需要电脑在线运行 CLI。ClawBench 部署在服务器上，手机随时连上就用，服务器挂机即可。

4. **定时任务调度**：所有竞品都没有。AI 提案 → 确认 → Cron 自动执行，适合自动化运维、每日 review 等场景。（Operit 有工作流定时触发，但非 Cron 式 AI 调度）

5. **数据完整持久化**：Happy/Remote 不存数据，AnyCoding 仅 256KB 环形缓冲，Yep Anywhere 依赖 CLI 会话历史无额外 DB，Dispatch 存云端。ClawBench 用 SQLite 本地持久化所有会话、历史、任务，断连不丢，数据主权在用户手中。

6. **绿色单文件部署**：一个二进制 + 静态资源，零依赖。Happy 需要 Node.js + npm + App 配对，Yep Anywhere 需要 Node.js + pnpm + Docker，AnyCoding 需要 Node.js + App 配对，Claude Remote 需要 Node.js + Tailscale，Dispatch 需要订阅。

7. **SSH 隧道端口转发**：内嵌 SSH 服务器，Android App 可直接访问服务器上任意端口。其他产品均无此能力。

8. **TTS 语音合成**：5 种引擎 + 10 种总结后端，AI 回复自动朗读。竞品均无。

9. **跨平台访问**：浏览器 + Android WebView 双端，任何设备都能访问。Operit 仅限 Android 原生 App，AnyCoding 仅限 Android App，AI Agent Remote 仅限浏览器终端。

10. **CLI Agent 深度集成**：ClawBench 通过 CLI 进程直接驱动 AI Agent（CodeBuddy、Claude Code 等），保留完整 Agent 能力（工具调用、思维链、AutoResume）。Operit 以 API 调用为主；Yep Anywhere 使用 Agent SDK 实现了结构化渲染（Diff/审批），但仅限 Claude Code + Codex；AnyCoding/AI Agent Remote 是纯终端镜像，丢失 Agent 结构化事件。

## ClawBench 相对劣势

1. **无端到端加密**：Happy、Yep Anywhere 和 Paseo 的核心卖点，对隐私极度敏感的用户有吸引力。Paseo 的 ECDH+AES-256-GCM 方案中继无法读/改流量，安全性最高
2. **无实时语音**：Happy 支持，Operit 更有完整语音管线（唤醒+STT+TTS+连续对话），Yep Anywhere 支持语音输入，Paseo 支持 push-to-talk STT
3. **无多客户端同步**：同一会话不能多设备同时操控
4. **无权限审批机制**：AI 工具调用没有人工审批流程（Happy/Dispatch/Remote 都有，Operit 也有工具级权限控制，Yep Anywhere 有推送审批，Paseo 支持从任何设备审批）
5. **iOS 无原生 App**：Happy 和 Paseo 都有 iOS 原生 App，Paseo 更是 iOS+Android+Web+Electron 四端
6. **无本地模型推理**：Operit 支持 MNN + llama.cpp 完全离线推理，ClawBench 依赖云端/服务器端 AI
7. **无 UI 自动化能力**：Operit 可通过无障碍/ADB/Root 让 AI 自主操控手机界面，ClawBench 无此能力
8. **无内置终端/Linux 环境**：Operit 内置 Ubuntu 24 chroot 环境，Paseo 内置终端，ClawBench 无终端能力
9. **无会话分叉/克隆**：Yep Anywhere 支持从任意消息点分叉对话，ClawBench 不支持
10. **无远程设备串流**：Yep Anywhere 支持通过 WebRTC 串流 Android 设备/模拟器，ClawBench 无此能力
11. **无 Agent 间协作**：Paseo 的 Skills 系统（handoff/loop/advisor/committee）实现了多 Agent 编排，ClawBench 仅 AutoResume 自动继续
12. **无 Worktree 自动隔离**：Paseo 为每个 Agent 自动创建独立 worktree，ClawBench 多会话共享同一工作目录
13. **无多主机支持**：Paseo 可一个客户端连接多台机器的 Daemon，ClawBench 仅单主机
14. **无 MCP Server 模式**：Paseo Daemon 可作为 MCP Server 被其他工具集成，ClawBench 无此能力
