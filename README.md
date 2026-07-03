[中文](README.md) | [English](README.en.md)

# ClawBench —— 为移动端打造的AI工作台

> 🎬 **演示视频**：[OpenClaw 和 Hermes 就是玩具，于是我写了一个能干活的](https://b23.tv/ewACF0h) — Bilibili

<p>
  <img src="assets/logo.png" alt="ClawBench" width="96" height="96" align="left" style="margin-right:16px;">
</p>

**从终端到掌心** — 为移动端打造的 AI 工作台。

将强大的 AI 编程智能体能力完整移植到浏览器与移动端 App，打造真正的移动端工作环境。文件浏览、代码编辑、AI 对话、Git 操作、定时调度、命令行终端 —— 一个应用，全部搞定。


- **支持平台**：浏览器（PC / 平板 / 手机）、Android App、PWA
- **AI 后端**：CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Cline、Copilot、Kimi

---

## 截图预览

### 登录与导航

| 登录 | 首页 | 选择项目 | 设置面板 |
|------|------|----------|----------|
| ![登录](docs/screenshots/login.png) | ![首页](docs/screenshots/home.png) | ![选择项目](docs/screenshots/project-select.png) | ![设置面板](docs/screenshots/settings-panel.png) |

### 文件浏览与代码编辑

| 文件浏览 | 搜索过滤 | 代码编辑器 | 引用提问 |
|----------|----------|------------|----------|
| ![文件浏览](docs/screenshots/file-browser.png) | ![搜索过滤](docs/screenshots/file-search.png) | ![代码编辑器](docs/screenshots/code-editor.png) | ![引用提问](docs/screenshots/quote-question.png) |

### Markdown 与文档预览

| Markdown 渲染 | LaTeX 公式 | Mermaid 图表 | 目录导航 |
|---------------|------------|-------------|----------|
| ![Markdown 渲染](docs/screenshots/markdown-preview.png) | ![LaTeX 公式](docs/screenshots/latex-formula.png) | ![Mermaid 图表](docs/screenshots/mermaid-diagram.png) | ![目录导航](docs/screenshots/toc-drawer.png) |


### AI 智能体

| 智能体选择 | AI 对话 | ACP 权限审批 | RAG 检索 | 会话管理 |
|------------|---------|-------------|----------|----------|
| ![智能体选择](docs/screenshots/agent-selector.png) | ![AI 对话](docs/screenshots/chat-panel.png) | ![ACP 权限审批](docs/screenshots/acp-permission.png) | ![RAG 检索](docs/screenshots/rag-search.png) | ![会话管理](docs/screenshots/session-manager.png) |

| 定时任务 | 创建任务 | 任务卡片 |
|----------|----------|----------|
| ![定时任务](docs/screenshots/scheduled-tasks.png) | ![创建任务](docs/screenshots/task-create.png) | ![任务卡片](docs/screenshots/schedule-proposal.png) |

### Git 集成

| 提交历史与分支图 | 分支管理 | 提交详情 | 比较报告 |
|------------------|----------|----------|----------|
| ![提交历史与分支图](docs/screenshots/git-history.png) | ![分支管理](docs/screenshots/git-branches.png) | ![提交详情](docs/screenshots/git-commit-detail.png) | ![比较报告](docs/screenshots/git-comparison-report.png) |

### 媒体预览

| 图片查看 | 视频播放 | 音频播放 | PDF 预览 |
|----------|----------|----------|----------|
| ![图片查看](docs/screenshots/image-viewer.png) | ![视频播放](docs/screenshots/video-player.png) | ![音频播放](docs/screenshots/audio-player.png) | ![PDF 预览](docs/screenshots/pdf-preview.png) |

### SSH 隧道与 Web 终端

| 端口转发 | 交互式终端 | 按键/符号配置 |
|---------|-----------|--------------|
| ![端口转发](docs/screenshots/port-forwarding.png) | ![交互式终端](docs/screenshots/terminal.png) | ![按键配置](docs/screenshots/terminal-key-config.png) |

---

## 快速开始

### 前置准备

- **一台 PC（Linux / macOS / Windows）**：用于运行 ClawBench 服务端，需已安装至少一种 AI 编程智能体 CLI（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Cline、Copilot、Kimi 均可）
- **一台手机**：安装 [ClawBench Android App](https://github.com/xulongzhe/clawbench/releases)，或使用手机浏览器（推荐 Chrome）访问服务端地址

### npm 安装（国内镜像加速）

通过 npm 一键安装，国内用户走淘宝源秒下：

```bash
# 配置淘宝镜像（仅需一次）
npm config set registry https://registry.npmmirror.com/
# 全局安装
npm install -g @xulongzhe/clawbench
# 启动
clawbench
```

支持 Linux (x64/arm64)、macOS (Intel/Apple Silicon)、Windows (x64)。npm 根据当前平台自动选择对应的二进制包。

### 安装包部署

从 [GitHub Releases](https://github.com/xulongzhe/clawbench/releases) 下载最新版 ZIP 包，解压即可运行，无需安装：

```bash
wget https://github.com/xulongzhe/clawbench/releases/latest/download/clawbench-linux-amd64.zip
unzip clawbench-linux-amd64.zip
cd clawbench
./clawbench
```



### Docker 部署

```bash
docker pull ghcr.io/clawbench-dev/clawbench:latest
docker run -d -p 20000:20000 -v clawbench-data:/data ghcr.io/clawbench-dev/clawbench:latest
```

修改 `-p` 可自定义端口（如 `-p 20300:20000`），`clawbench-data` 卷持久化数据。查看自动生成的密码：

```bash
docker exec $(docker ps -qf ancestor=ghcr.io/clawbench-dev/clawbench) cat /data/.clawbench/auto-password
```

> 首次启动会自动生成8位随机密码，以字符框突出打印到控制台，请妥善保存。

部署完成后，使用手机 App 或手机浏览器访问 `http://服务器IP:20000` 即可开始使用：

- **手机 App**：原生集成，自动连接，支持完整功能
- **手机浏览器**：推荐使用 **Chrome 浏览器**访问，支持将网页安装为 PWA 应用（添加到主屏幕），获得接近原生 App 的体验


> 编译构建、高级配置、部署说明、架构设计等详细文档请参阅 **[编译与开发指南](docs/DEVELOPMENT.md)**。

---

## 功能详解

### 📁 文件浏览
- 递归目录浏览，支持 120+ 种文件扩展名
- 搜索过滤、排序（名称/时间/扩展名/大小）
- **列表/网格视图切换**：网格视图以图片缩略图展示文件，直观浏览图片资源
- **图片缩略图**：后端生成方形缩略图（主色调填充），快速预览图片内容
- 右键菜单：重命名、删除、复制、剪切、粘贴、新建文件/文件夹、下载、作为项目打开
- **多选操作**：工具栏切换多选模式，批量复制/剪切/删除，移动端长按触发右键菜单
- 文件上传（支持所有文件类型，大小和数量可配置）
- 隐藏文件显示/隐藏切换
- **下钻浏览 + 边缘滑动回退**：点击文件夹下钻进入，右边缘左滑返回上一级，移动端直觉操作
- **文件预览覆盖层**：点击文件直接在浏览页上方弹出预览覆盖层，无需切换标签页；支持导航栈（多文件切换 + 返回），关闭即回到文件列表
- **二进制文件预览**：二进制文件显示占位界面，支持"以文本方式打开"；大文件自动截断（64KB 二进制 / 512KB 文本），截断时显示提示横幅

### 🎨 代码预览
- 语法高亮，粘性行号，自动换行切换
- **Sticky Scroll**：VS Code 风格的粘性滚动，滚动时自动显示当前所在的作用域上下文（函数/类/结构体等）
- 双击复制代码行内容（闪烁动画反馈）
- **文件改动闪烁高亮**：文件被外部修改时，删除字符红色脉冲闪烁，新增字符蓝色脉冲闪烁，快速定位改动
- **引用提问**：选中代码片段后，一键向 AI 提问，自动附上文件路径和行号
- **文件路径跳转**：代码预览中的文件路径可点击跳转，支持 import 路径解析（如 @/composables/useFoo 解析为实际文件路径）；支持行范围导航（如 `file.go:42-50`），高亮闪烁指定行范围
- 滑动手势：左右滑动切换文件

### 📝 Markdown
- 渲染视图 / 源码视图一键切换
- **引用提问**：选中文本，一键向 AI 提问
- 智能目录抽屉（TOC），支持 Tree-sitter 代码符号提取（100+ 语言，17 种符号类型图标），LaTeX 数学公式，Mermaid 图表
- **图片灯箱**：图片支持放大、左右切换浏览
- **文件路径跳转**：Markdown 中的文件路径可点击跳转，支持行范围导航

### 🤖 AI 智能体
- **流式响应**：SSE 实时推送，思维过程、工具调用全程可见
- **多 Agent 支持**：全能助手、编码专家、勤杂工等，YAML 配置即插即用
- **AI 后端切换**：CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Cline、Copilot、Kimi，会话级隔离
- **深度思考档位**：支持按智能体选择思考深度（Auto / Low / Medium / High），Claude/CodeBuddy/OpenCode/Codex/MiMo/Pi/Cline/Copilot/Kimi 九后端支持，选择自动持久化
- **模型选择模态框**：统一模型切换与思考深度选择，双 Tab 界面，搜索过滤，一键刷新模型列表（支持自动发现的智能体），长按设为默认模型
- **模型选择持久化**：每个智能体的模型选择和思考档位自动保存到 localStorage，刷新/切换会话自动恢复
- **定时任务**：AI 通过 CLI 子命令创建 Cron 调度，定时自动执行；独立标签页管理，4 级面包屑导航；频率预设（每小时/每天/每周/每月）+ 自定义 Cron 表达式；任务卡片内嵌聊天消息；执行级别已读追踪 + TTS 朗读；执行完成后自动摘要 + 完成通知（音效/震动/Toast）
- **继续对话**：定时任务执行详情页可一键继续对话，自动复制历史消息和摘要到新会话，继承后端/智能体/模型/思考档位；会话列表中定时任务来源的会话显示紫色「定时」角标
- **多会话管理**：创建、切换、删除独立会话，滑动切换
- **滑动会话切换开关**：可在设置中开关聊天区域左右滑动切换会话，默认关闭避免滚动宽内容时误触
- **图片上传**：支持上传图片与 AI 对话（多模态）
- **断连保护**：消息立即落库，网络断开不丢失，15 秒心跳保活 + 30 秒超时自动重连（降级轮询时实时更新内容）
- **自动恢复**：Claude / CodeBuddy / Qoder / CodeWhale / MiMo / Pi / Cline / Copilot / Kimi 退出 Plan Mode 后自动发送"继续"
- **消息队列**：AI 忙碌时消息排队，依次发送
- **自动摘要**：会话完成后自动生成最后一条助手消息的摘要，底部横幅一键切换摘要/原文；TTS 朗读也使用摘要
- **@ 命令**：输入 `@chatsearch` 搜索历史对话、`@task` 管理定时任务，自动补全弹出菜单，用户消息显示紫色命令徽章
- **RAG 结果卡片**：AI 回复中的 RAG 搜索结果渲染为紫色卡片，点击弹出详情抽屉，可一键恢复对话
- **思维流内联显示**：思考过程流式内联展示，完成后自动折叠为可点击芯片
- **会话进度指示**：会话抽屉显示胶囊进度条，颜色随用量变化（蓝/橙/红）

### 🤖 AI 对话
- **工具调用可视化**：名称、参数、执行结果实时展示，成功/失败状态一目了然
- **深度思考**：复杂任务自动触发 extended thinking，推理过程实时可见
- **文件路径跳转**：AI 回复中的文件路径可点击跳转，支持行范围导航
- **Localhost URL 跳转**：AI 回复中的 localhost URL（如 http://localhost:3000）自动识别并添加打开按钮，App 模式下自动注册端口转发并通过 WebView 打开，无需手动配置
- **快捷发送**：预设常用指令（继续、编译、提交等），支持拖拽排序，一键发送，输入框占位提示当前快捷指令；长按可填充到输入框（带渐进填充条），便于编辑后再发送
- **引用提问**：选中代码或文本，直接向 AI 提问，自动附带上下文
- **当前目录附加**：聊天输入支持附加当前目录上下文，AI 自动获取目录结构
- **未读徽章**：聊天面板图标显示未读消息数

- **自动审批指示**：模式芯片在启用 auto-approve 时变为绿色，直观显示权限模式

### 🖼️ 媒体预览
- 图片、音频、视频应用内直接预览
- 灯箱放大、全屏查看，支持缩放和拖拽

### 🔊 TTS 语音朗读
- AI 回复自动总结后朗读，边听边看
- **5 种 TTS 引擎**：Edge TTS（免费，原生 Go 实现，无外部依赖）、MiniMax（音质最佳）、Piper / Kokoro / MOSS-Nano（本地离线）
- **11 种总结后端**：simple 纯清洗、mmx-cli、api（OpenAI/Anthropic 兼容）、Claude、CodeBuddy、OpenCode、Codex、Qoder、VeCLI、DeepSeek、Pi
- 详见 [TTS 语音合成部署指南](docs/TTS.md)

### 📂 Git 集成
- 项目级 / 文件级提交历史浏览
- **Git 分支图**：纵向分支拓扑图，直观展示分支关系
- **Git Diff 视图**：查看文件相对 HEAD 的变更，字符级高亮
- 提交详情查看（作者、时间、提交信息）
- 工作树变更视图（已暂存 / 未暂存文件）
- **三标签页管理**：工作树 / 分支 / 标签三标签页统一管理，默认标签持久化到 localStorage
- **滑动删除**：分支、工作树、标签支持左滑删除，安全保护（当前分支/默认分支/当前工作树不可删除）
- **标签管理**：浏览项目标签，点击标签可 Checkout，脏工作树自动弹窗处理

### 🔀 SSH 隧道端口转发
- **远程开发**：在 Android App 上直接访问服务器本地端口
- **全协议透明**：HTTP、HTTPS、WebSocket、SSE、gRPC，无需 URL 重写
- **指定目标地址**：支持转发到任意可达主机（局域网/远程主机，不仅限 127.0.0.1）
- **自动端口分配**：同一目标端口转发到不同主机时自动分配本地端口
- **端口编辑**：支持修改已注册的端口转发配置
- **Localhost URL 自动打开**：聊天中出现的 localhost URL（如 AI 启动的 Web 服务），App 模式下一键自动注册端口转发并通过 WebView 打开
- **隧道健康检测与重连**：打开 localhost URL 前自动检测隧道健康状态，异常时自动重连；断开的隧道支持一键重连

### 💻 Web 终端
- **交互式终端**：基于 PTY + WebSocket + xterm.js，浏览器内直接操作服务器终端
- **多会话并发**：每个客户端拥有独立 PTY 会话，互不干扰
- **多标签管理**：支持关闭所有标签页，空状态显示创建按钮，Dock 图标显示活跃会话数
- **虚拟按键栏**：按类型分组的颜色编码按键（修饰键、快捷键、导航键、方向键、操作键），修饰键支持三态切换
- **按键/符号配置**：全屏配置抽屉，按键和符号双标签页，支持点选添加、拖拽排序、手势模式自动隐藏部分按键，配置持久化到数据库
- **符号栏**：可展开的符号输入行，19 个终端高频符号，过渡动画
- **触摸手势**：Termius 风格手势（滑动→方向键、长按重复、双击→Tab、捏合缩放），手势关闭时支持触摸滚动，切换时 Toast 提示
- **选中文本自动复制**：选中终端文本自动复制到剪贴板，Toast 反馈
- **快捷命令**：CRUD 管理常用命令，支持拖拽排序、隐藏、自动执行（每次连接自动运行）
- **Android 音量键**：App 内终端打开时，音量键映射为方向键上下
- 详见 [Web 终端使用指南](docs/TERMINAL.md)

### 🌐 国际化
- 中文 / 英文双语界面，自动检测系统语言

### 📱 Android App
- 原生桥接集成：自动登录、文件下载（支持 POST 下载压缩包）、端口转发管理
- 静态 HTML 登录页：首次启动或连接失败时显示，与 Web 端视觉风格统一
- SSH 密码管理、服务器对话框
- WebView 连接保护：连接期间隐藏 WebView，防止浏览器错误页闪现
- 终端音量键映射：打开终端时音量键作为方向键

### 🔔 通知
- 通知音效 + 触觉反馈（AI 完成时提醒）
- 浏览器推送通知
- **任务完成推送**：定时任务执行完成后推送包含响应预览摘要，点击跳转至执行详情


### 🎨 主题
- 亮色 / 暗色模式，跟随系统偏好

### 📱 PWA 支持
- 可安装到主屏幕，独立窗口运行

### 🔒 安全
- 可选密码保护（SHA-256 加盐哈希存储，设置面板支持修改密码）
- API Key 加密存储（AES-256-GCM + HKDF-SHA256 密钥派生，修改密码自动轮换加密密钥）
- 多实例部署安全隔离（Cookie 按端口自动前缀，同域名不同端口互不干扰）
- 路径穿越防护，所有操作限制在项目目录内
- Git 参数注入防护（SHA/分支名/标签名校验，`--` 分隔符）
- 文件上传大小和数量可配置（默认 100MB / 20 个），支持所有文件类型
- XSS 防护（DOMPurify 净化）
- TLS 支持（需手动配置证书）

---

## 常见问题

详见 **[FAQ](docs/FAQ.md)**。

---

## 许可证

Copyright (c) 2026 xulongzhe

Licensed under the MIT License
