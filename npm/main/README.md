# ClawBench — 多端一体的 AI 工作台

**从掌心到桌面**：把 AI 编程智能体的完整能力带到手机、平板与桌面。

ClawBench 是一个单文件部署的 AI 工作台服务端：Go 后端调用本地已安装的 AI 编程智能体 CLI（CodeBuddy、Claude Code、Codex、Kimi、Cursor 等），通过 WebSocket 流式推送事件，Vue 3 前端实时渲染。文件浏览、代码编辑、AI 对话、多智能体群聊、Git、任务调度、Web 终端、SSH 隧道 —— 一套界面，所有设备共用。

<p align="center">
  <img src="https://raw.githubusercontent.com/clawbench-dev/clawbench/main/docs/screenshots/product_hero.png" alt="ClawBench" width="880">
</p>

## 特性

- **AI 智能体原生直通**：工具调用、深度思考、Skills、MCP 不做二次封装，保留 CLI 的完整能力；支持 18 个后端 + 任意自定义 ACP 智能体
- **多智能体群聊**：主持人 / 自由两种模式，可跨后端组队，支持 @ 接力、密送、按成员压缩上下文
- **移动端优先的交互**：滑动切换、长按菜单、手势、软键盘适配、PWA 可安装，另有 Android App 与桌面客户端（Electron）
- **一个进程全包**：文件管理、代码编辑与预览、Git 与 Forge 集成、定时/事件任务、Web 终端、TTS/STT、分享链接、通知中心
- **单文件部署**：无外部依赖（SQLite 内置），首次启动自动生成访问密码

## 安装

本包会自动安装对应平台的二进制包（Linux x64/arm64、macOS Intel/Apple Silicon、Windows x64、Android arm64/Termux）。

```bash
# 国内用户可先切淘宝镜像（仅需一次）
npm config set registry https://registry.npmmirror.com/

npm install -g @xulongzhe/clawbench
```

## 使用

```bash
clawbench                    # 启动，默认监听 20000 端口
clawbench --port 8080        # 指定端口
clawbench --data-dir /data   # 指定数据目录（默认 ~/.clawbench）
```

启动后在浏览器或 ClawBench App 中访问 `http://<服务器地址>:20000`。**首次启动会把自动生成的密码打印到控制台，请妥善保存。**

使用前需在服务端所在机器上安装至少一个 AI 编程智能体 CLI。

## 其他安装方式

| 方式 | 说明 |
|---|---|
| [GitHub Releases](https://github.com/clawbench-dev/clawbench/releases) | 下载 ZIP 解压即用，无需安装 |
| Docker | `docker run -d --restart unless-stopped -p 20000:20000 -v clawbench-data:/data ghcr.io/clawbench-dev/clawbench:latest` |
| Termux（Android） | 在手机上完整运行服务端，见下方 Termux 指南 |

## 链接

- **项目主页 / 源码**：<https://github.com/clawbench-dev/clawbench>
- **下载与更新日志**：<https://github.com/clawbench-dev/clawbench/releases>
- **使用手册**：<https://github.com/clawbench-dev/clawbench/blob/main/docs/user-guide/README.md>
- **常见问题**：<https://github.com/clawbench-dev/clawbench/blob/main/docs/user-guide/FAQ.md>
- **Termux 部署指南**：<https://github.com/clawbench-dev/clawbench/blob/main/docs/user-guide/TERMUX.md>
- **问题反馈**：<https://github.com/clawbench-dev/clawbench/issues/new/choose>

## 许可证

[MIT](https://github.com/clawbench-dev/clawbench/blob/main/LICENSE) © 2026 xulongzhe
