[English](README.en.md) | [中文](README.md)

# ClawBench — AI Workbench, United Across Devices

<p align="center">
  <img src="docs/screenshots/product_hero.en.png" alt="ClawBench" width="960">
</p>

<p align="center">
  <img src="docs/screenshots/pc-desktop.png" alt="ClawBench PC Desktop" width="960">
</p>

**From Palm to Desktop** — An AI workbench for every screen.

Brings the full power of AI coding agents to every screen — phone, tablet, and desktop. File browsing, code editing, AI conversation, Git operations, tasks, one app does it all, whether you're on the go or at your desk.

Core Advantage: Native passthrough of AI capabilities (tool calls, extended thinking, Skills, MCP) with zero adaptation cost, fully preserving the power of coding agents. ClawBench is a complete workbench on every platform — files, code, Git, AI, tasks, TTS — with mobile interactions carefully crafted for one-handed use and full desktop support for serious work.

- **Supported Platforms**: Browser (PC / Tablet / Phone), Android App, PWA
- **AI Backends**: CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, DeepSeek Harness, MiMo-Code, Pi, Copilot, Kimi, Antigravity, Grok Build, ZCode

📖 **User Guides** (Chinese): [Desktop](docs/user-guid/user-guid.md) · [Mobile](docs/user-guid/user-guid-mobile.md)

<p align="center">
  <img src="assets/architecture.en.svg" alt="ClawBench Deployment Architecture" width="640">
</p>

---

## Quick Start

### Prerequisites

- **A PC (Linux / macOS / Windows) or an Android phone with [Termux](docs/TERMUX.md)**: To run the ClawBench server, with at least one AI coding agent CLI installed (CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Copilot, or Kimi)
- **Any device**: Install the [ClawBench Android App](https://github.com/xulongzhe/clawbench/releases), or open the server address in any browser — desktop, tablet, or phone

### npm Install

Install via npm in one command:

```bash
npm install -g @xulongzhe/clawbench
# Start
clawbench
```

Supports Linux (x64/arm64), macOS (Intel/Apple Silicon), and Windows (x64). npm automatically selects the correct platform-specific binary package.

### Download & Start

Download the latest ZIP package from [GitHub Releases](https://github.com/xulongzhe/clawbench/releases), extract and you're ready:

```bash
# Asset names carry the version, so resolve the latest tag before building the URL
TAG=$(curl -sI https://github.com/xulongzhe/clawbench/releases/latest \
  | grep -i '^location:' | sed 's|.*/tag/||' | tr -d '\r\n')
wget "https://github.com/xulongzhe/clawbench/releases/download/${TAG}/clawbench-linux-amd64-${TAG}.zip"
unzip "clawbench-linux-amd64-${TAG}.zip"
cd clawbench
./clawbench
```

### Docker Deployment

```bash
docker pull ghcr.io/clawbench-dev/clawbench:latest
docker run -d --restart unless-stopped -p 20000:20000 -v clawbench-data:/data ghcr.io/clawbench-dev/clawbench:latest
```

Customize the host port with `-p` (e.g., `-p 20300:20000`). The `clawbench-data` volume persists all data.

> `--restart unless-stopped` (or `always`) is required for in-app upgrades: the container replaces its own binary and exits with code 0, and Docker's restart policy brings the new version back up. Do **not** use `--restart on-failure` — a graceful shutdown exits 0, so it never triggers and the container stays stopped. The recommended upgrade path is still `docker pull` + recreate the container, since rebuilding from an unchanged image reverts the in-place replacement.

To view the auto-generated password:

```bash
docker exec $(docker ps -qf ancestor=ghcr.io/clawbench-dev/clawbench) cat /data/.clawbench/auto-password
```

> A random 32-character hex password (128-bit entropy) is auto-generated on first startup and printed to the console in a bordered box. Save it securely.

Once deployed, access `http://server-ip:20000` from your phone app or any browser:

- **Android App**: Native integration, auto-connect, full feature support
- **Mobile / Desktop Browser**: **Chrome** recommended on mobile — supports installing as a PWA app (Add to Home Screen) for a near-native experience

### 📱 Run Completely on an Android Phone (Termux)

> See the full guide in **[Termux (Android)](docs/TERMUX.md)**.

ClawBench runs completely on your Android phone inside [Termux](https://f-droid.org/repo/com.termux.app.apk). The pure-Go `linux-arm64` backend, the built-in web frontend, and your AI coding agents all run locally on the phone — no separate PC or server required:

```bash
pkg install -y nodejs-lts git
npm install -g @xulongzhe/clawbench
clawbench
```

> 📡 **Public Access**: To access ClawBench from the public internet (commuting, traveling, etc.), see the **[Public Access Guide](docs/PUBLIC_ACCESS.md)**  — supports IPv6 direct connection, FRP tunnel, and EasyTier decentralized networking (no VPS required).

---

## Core Features

> The table below is a feature overview. **Per-feature instructions, screenshots, and edge cases live in the [Desktop](docs/user-guid/user-guid.md) / [Mobile](docs/user-guid/user-guid-mobile.md) user guides (Chinese).**

| Module | Highlights |
|---|---|
| 📁 **File Management** | Recursive browsing (120+ extensions), search & sort, list/grid views, multi-select batch ops, upload & directory-tree download, drag-and-drop move, paste upload, `.gitignore`-aware dimming, **file share links** (revocable capability tokens), **interactive OpenAPI/Swagger preview** |
| 🎨 **Code Preview & Editing** | CodeMirror browse/edit dual mode, syntax highlighting, autocompletion (11 languages), **Sticky Scroll**, VS Code-style search bar, diff flash highlighting, **Excalidraw canvas**, path jumps with line ranges |
| 📝 **Markdown** | Render/source toggle, TOC drawer, LaTeX, Mermaid, image lightbox, **code-link preview overlay**, **self-contained HTML export** (KaTeX fonts inlined) |
| 📄 **Documents & Media** | Native Word / Excel / PowerPoint rendering, paged PDF with zoom, inline image/audio/video players, lightbox |
| 🤖 **AI Agents** | **15 backends** (CodeBuddy, Claude Code, OpenCode, Codex, Qoder, VeCLI, CodeWhale, DeepSeek Harness, MiMo, Pi, Copilot, Kimi, Antigravity, Grok Build, ZCode), streaming responses, visible reasoning, **sub-agent content grouping**, thinking-depth levels, persisted model choice, persisted ACP context state, local skill scanning |
| 💬 **AI Conversation** | Tool-call visualization, **interactive question cards**, suggested replies, slash commands (ACP + built-in merged), quote-to-ask, message queue, disconnect protection, rewind & fork, auto-summary, RAG result cards, draft restore, completion popup |
| 📂 **Session Management** | Create / switch / archive sessions, **session tags** (project-scoped, hash-colored), unread counted **per item**, per-project session restore, swipe-to-switch toggle |
| ⏰ **Task Scheduling** | Cron schedules (presets + custom) and **event triggers** (`issue.opened` / `pr.merged` / `pipeline_done`), 3-level breadcrumb navigation, continue-chat from run details, completion push |
| 🔗 **Git & Forge** | Commit history, **branch graph**, diff view, working-tree changes, three-tab management (worktree/branches/tags), swipe-to-delete; **GitHub / GitLab integration** (Activity/Issues/Merges/Pipelines tabs, "mine" filters, event-driven tasks, quote to chat, per-host credential isolation) |
| 💻 **Web Terminal** | PTY + xterm.js, multi-session multi-tab, **three-mode gesture system**, virtual key toolbar, key/symbol config, quick commands, 157 terminal themes, mobile input drawer |
| 📊 **Statistics** | Usage overview (donut with cache-hit drilldown), dimension filters and chart switching, **code inventory** (gocloc per-language snapshot), **code churn** (per-author time trend) |
| 🔊 **Speech** | **TTS**: 5 engines (Edge / MiniMax / Piper / Kokoro / MOSS-Nano), auto-summarized read-aloud; **STT**: streaming and non-streaming modes, vLLM Whisper |
| 🔀 **SSH Tunnel** | Transparent for all protocols (HTTP/HTTPS/WS/SSE/gRPC), arbitrary target hosts, automatic port allocation, health check & reconnect, one-tap localhost URL |
| 🎨 **Themes** | **36 named themes**, live system-follow, **custom wallpaper** (upload or local path + opacity/blur), custom fonts, quick theme picker |
| 📱 **Multi-Device** | Android App (native bridge, **floating status window**, **Live Updates / Dynamic Island**, self-update, version-mismatch detection, full i18n), installable PWA, full local run on a phone via Termux |
| 🔔 **Notifications** | Sound + haptics, browser push, task-completion push, **DingTalk / Feishu bot push** (browse sessions and send messages from IM) |
| 🔒 **Security** | Salted SHA-256 password, path-traversal protection, Git argument-injection guards, XSS sanitization (DOMPurify), **short-lived HMAC tokens** (local requests no longer trusted by address alone), TLS auto-discovery, per-instance cookie isolation |

---

## FAQ

See **[FAQ](docs/FAQ.en.md)** .

---

## License

Copyright (c) 2026 xulongzhe

Licensed under the MIT License
