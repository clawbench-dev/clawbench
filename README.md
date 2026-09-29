[中文](README.md) | [English](README.en.md)

# ClawBench —— 多端一体的 AI 工作台

<p align="center">
  <img src="docs/screenshots/product_hero.png" alt="ClawBench" width="960">
</p>

<p align="center">
  <img src="docs/screenshots/pc-desktop.png" alt="ClawBench PC 桌面端" width="960">
</p>

> 🎬 **演示视频**：[OpenClaw 和 Hermes 就是玩具，于是我写了一个能干活的](https://b23.tv/ewACF0h) — Bilibili

**从掌心到桌面，多端一体的 AI 工作台。**

将强大的 AI 编程智能体能力带到每一块屏幕——手机、平板与桌面。文件浏览、代码编辑、AI 对话、Git 操作、定时调度、命令行终端 —— 一个应用，全部搞定，无论你在通勤路上还是坐在桌前。

**单文件部署，无任何依赖**

<p align="center">
  <img src="assets/architecture.zh.svg" alt="ClawBench 部署架构" width="640">
</p>

- **支持平台**：浏览器（PC / 平板 / 手机）、Android App、PWA；AI 智能体可在 PC 上运行，也可通过 [Termux](docs/TERMUX.md) 在安卓手机上完全运行
- **AI 后端**：CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、DeepSeek Harness、MiMo-Code、Pi、Copilot、Kimi、Antigravity、Grok Build、ZCode

📖 **使用手册**：[桌面端使用说明](docs/user-guid/user-guid.md) · [移动端使用说明](docs/user-guid/user-guid-mobile.md)

---

## 快速开始

### 前置准备

- **一台 PC（Linux / macOS / Windows）或装有 Termux 的安卓手机**：用于运行 ClawBench 服务端，并安装至少一个 AI 编程智能体 CLI（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Copilot、Kimi）
- **任意设备**：安装 [ClawBench Android App](https://github.com/xulongzhe/clawbench/releases)，或用任意浏览器（桌面 / 平板 / 手机）访问服务端地址

### npm 安装

通过 npm 一键安装，国内用户走淘宝源秒下：

```bash
# 配置淘宝镜像（仅需一次）
npm config set registry https://registry.npmmirror.com/
# 全局安装
npm install -g @xulongzhe/clawbench
# 启动
clawbench
```

### 安装包部署

从 [GitHub Releases](https://github.com/xulongzhe/clawbench/releases) 下载最新版 ZIP 包，解压即可运行，无需安装：

```bash
# 资产文件名带版本号，所以先解析最新 tag 再拼下载地址
TAG=$(curl -sI https://github.com/xulongzhe/clawbench/releases/latest \
  | grep -i '^location:' | sed 's|.*/tag/||' | tr -d '\r\n')
wget "https://github.com/xulongzhe/clawbench/releases/download/${TAG}/clawbench-linux-amd64-${TAG}.zip"
unzip "clawbench-linux-amd64-${TAG}.zip"
cd clawbench
./clawbench
```



### Docker 部署

```bash
docker pull ghcr.io/clawbench-dev/clawbench:latest
docker run -d --restart unless-stopped -p 20000:20000 -v clawbench-data:/data ghcr.io/clawbench-dev/clawbench:latest
```

修改 `-p` 可自定义端口（如 `-p 20300:20000`），`clawbench-data` 卷持久化数据。

> `--restart unless-stopped`（或 `always`）是应用内升级的必要条件：容器会替换自身二进制并以退出码 0 退出，依赖 Docker 重启策略把新版本拉起来。请勿使用 `--restart on-failure`——优雅退出的退出码为 0，不会触发重启，容器会停在停止状态。推荐的升级方式仍是 `docker pull` 后重建容器——用未更新的镜像重建会回退就地替换的结果。

> 首次启动会自动生成32位随机密码，以字符框突出打印到控制台，请妥善保存。

部署完成后，使用手机 App 或任意浏览器访问 `http://服务器IP:20000` 即可开始使用。

### 📱 在安卓手机上完全运行（Termux）

> 完整指南见 **[Termux（安卓）](docs/TERMUX.md)** 。

ClawBench 可在安卓手机的 [Termux](https://f-droid.org/repo/com.termux.app.apk) 终端模拟器中**完全运行**：纯 Go 的 `linux-arm64` 后端、内置 Web 前端、AI 编程智能体全部在手机本地执行，无需额外 PC 或服务器：

```bash
pkg install -y nodejs-lts git
npm install -g @xulongzhe/clawbench
clawbench
```

> 📡 **公网访问**：如需从外网访问 ClawBench（通勤途中、出差等场景），请参阅 **[公网访问指南](docs/PUBLIC_ACCESS.md)** ，支持 IPv6 直连、FRP 内网穿透和 EasyTier 去中心化组网（无需 VPS）三种方式。

---

## 核心功能

> 下表是功能概览。**逐项操作说明、截图与边界情况见 [桌面端](docs/user-guid/user-guid.md) / [移动端](docs/user-guid/user-guid-mobile.md) 用户手册。**

| 模块 | 能力概要 |
|---|---|
| 📁 **文件管理** | 递归浏览（120+ 扩展名）、搜索排序、列表/网格视图、多选批量操作、上传与目录树下载、拖放移动、粘贴上传、按 `.gitignore` 灰显、**文件分享链接**（capability token，可撤销）、**OpenAPI/Swagger 交互式预览** |
| 🎨 **代码预览与编辑** | CodeMirror 浏览/编辑双模式、语法高亮、自动补全（11 种语言）、**Sticky Scroll**、VS Code 风格搜索条、文件改动闪烁高亮、**Excalidraw 画布**、路径跳转与行范围导航 |
| 📝 **Markdown** | 渲染/源码切换、TOC 抽屉、LaTeX 公式、Mermaid 图表、图片灯箱、**代码链接预览浮层**、**自包含 HTML 导出**（KaTeX 字体内联） |
| 📄 **文档与媒体** | Word / Excel / PowerPoint 原生渲染、PDF 分页缩放、图片/音频/视频内联播放、灯箱放大 |
| 🤖 **AI 智能体** | **15 个后端**（CodeBuddy、Claude Code、OpenCode、Codex、Qoder、VeCLI、CodeWhale、DeepSeek Harness、MiMo、Pi、Copilot、Kimi、Antigravity、Grok Build、ZCode）、流式响应、思维过程可见、**子智能体内容分组**、深度思考档位、模型选择持久化、ACP 上下文状态持久化、本地技能扫描 |
| 💬 **AI 对话** | 工具调用可视化、**交互式提问卡**、推荐回复、斜杠命令（合并 ACP 与内置）、引用提问、消息队列、断连保护、消息回溯（Rewind）与分叉、自动摘要、RAG 结果卡片、输入草稿恢复、完成弹窗 |
| 📂 **会话管理** | 多会话创建/切换/归档、**会话标签**（按项目隔离、哈希配色）、未读按**条目**计数、按项目恢复上次会话、滑动切换开关 |
| ⏰ **任务调度** | 定时（Cron 预设 + 自定义）与**事件触发**（`issue.opened` / `pr.merged` / `pipeline_done`）、3 级面包屑导航、执行详情续接对话、完成推送 |
| 🔗 **Git 与 Forge** | 提交历史、**分支图**、Diff 视图、工作树变更、三标签页管理（工作树/分支/标签）、滑动删除；**GitHub / GitLab 集成**（动态/议题/合并/流水线四页签、跟我相关筛选、事件驱动任务、引用到对话、按 host 隔离凭据） |
| 💻 **Web 终端** | PTY + xterm.js、多会话多标签、**三模式手势系统**、虚拟按键栏、按键/符号配置、快捷命令、157 个终端主题、移动端输入抽屉 |
| 📊 **数据统计** | 用量总览（环形图下钻缓存命中）、按维度筛选与图表切换、**代码存量**（gocloc 按语言快照）、**代码增量**（按作者的时间趋势） |
| 🔊 **语音** | **TTS**：5 种引擎（Edge / MiniMax / Piper / Kokoro / MOSS-Nano），自动总结后朗读；**STT**：流式与非流式双模式，vLLM Whisper |
| 🔀 **SSH 隧道** | 全协议透明（HTTP/HTTPS/WS/SSE/gRPC）、指定目标地址、自动端口分配、健康检测与重连、Localhost URL 一键打开 |
| 🎨 **主题外观** | **36 个命名主题**、跟随系统实时切换、**自定义壁纸**（上传或本地路径 + 不透明度/模糊）、自定义字体、快捷主题选择器 |
| 📱 **多端** | Android App（原生桥接、**桌面悬浮状态窗**、**灵动岛 Live Updates**、应用自升级、版本不匹配检测、全量国际化）、**桌面客户端**（Electron，Windows / macOS / Linux）、PWA 可安装、Termux 手机本地运行 |
| 🖥️ **桌面客户端** | Electron 壳承载同一套 Web UI；原生系统通知（点击跳转会话/任务）、原生右键菜单、外链走默认浏览器、**SSH 端口映射**、多服务器管理（safeStorage 加密凭据）、**应用自升级**（侧装多版本 + 指针切换，可回滚；国内自动走 GitHub 镜像）、Ctrl+F5 硬刷新 |
| 🔔 **通知** | 音效 + 触觉反馈、浏览器推送、任务完成推送、**钉钉 / 飞书机器人推送**（IM 内查看会话与发消息） |
| 🔒 **安全** | SHA-256 加盐密码、路径穿越防护、Git 参数注入防护、XSS 净化（DOMPurify）、**HMAC 短时令牌**（本机请求不再凭地址免密）、TLS 自动发现、多实例 Cookie 隔离 |

---

## 常见问题

详见 **[FAQ](docs/FAQ.md)** 。

---

## 许可证

Copyright (c) 2026 xulongzhe

Licensed under the MIT License
