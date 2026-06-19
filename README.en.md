[中文](README.md) | [English](README.en.md)

# ClawBench — AI Workstation Built for Mobile

> 🎬 **Demo Video**: [OpenClaw and Hermes are toys, so I built one that actually works](https://b23.tv/ewACF0h) — Bilibili

<p>
  <img src="assets/logo.png" alt="ClawBench" width="96" height="96" align="left" style="margin-right:16px;">
</p>

**From Terminal to Palm** — An AI workstation built for mobile.

Brings the full power of AI coding agents to browsers and mobile apps, creating a true mobile development environment. File browsing, code editing, AI conversation, Git operations, scheduled tasks — one app does it all.

Core Advantage: Native passthrough of AI capabilities (tool calls, extended thinking, Skills, MCP) with zero adaptation cost, fully preserving the power of coding agents. Unlike other mobile AI tools that are merely "remote controllers," ClawBench is a full-featured mobile workstation — files, code, Git, AI, scheduled tasks, TTS, get real work done on your phone without needing a PC online. ([Similar Projects Comparison](docs/COMPARISON.en.md))

- **Supported Platforms**: Browser (PC / Tablet / Phone), Android App, PWA
- **AI Backends**: CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Cline, Copilot, Kimi

---

## Screenshots

### Login & Navigation

| Login | Home | Select Project |
|-------|------|----------------|
| ![Login](docs/screenshots/login.png) | ![Home](docs/screenshots/home.png) | ![Select Project](docs/screenshots/project-select.png) |

### File Browsing & Code Editing

| File Browser | Search & Filter | Code Editor | Quote & Ask |
|-------------|----------------|-------------|-------------|
| ![File Browser](docs/screenshots/file-browser.png) | ![Search & Filter](docs/screenshots/file-search.png) | ![Code Editor](docs/screenshots/code-editor.png) | ![Quote & Ask](docs/screenshots/quote-question.png) |

### Markdown & Document Preview

| Markdown Render | LaTeX Formulas | Mermaid Diagrams | Table of Contents |
|-----------------|----------------|------------------|-------------------|
| ![Markdown Render](docs/screenshots/markdown-preview.png) | ![LaTeX Formulas](docs/screenshots/latex-formula.png) | ![Mermaid Diagrams](docs/screenshots/mermaid-diagram.png) | ![Table of Contents](docs/screenshots/toc-drawer.png) |

### AI Agents

| Agent Selection | AI Conversation | Structured Question | Session Manager |
|-----------------|-----------------|---------------------|-----------------|
| ![Agent Selection](docs/screenshots/agent-selector.png) | ![AI Conversation](docs/screenshots/chat-panel.png) | ![Structured Question](docs/screenshots/ask-question.png) | ![Session Manager](docs/screenshots/session-manager.png) |

| Scheduled Tasks | Create Task | Task Card |
|-----------------|-------------|-----------|
| ![Scheduled Tasks](docs/screenshots/scheduled-tasks.png) | ![Create Task](docs/screenshots/task-create.png) | ![Task Card](docs/screenshots/schedule-proposal.png) |

### Git Integration

| Commit History & Branch Graph | Commit Detail | Comparison Report |
|-------------------------------|---------------|-------------------|
| ![Commit History & Branch Graph](docs/screenshots/git-history.png) | ![Commit Detail](docs/screenshots/git-commit-detail.png) | ![Comparison Report](docs/screenshots/git-comparison-report.png) |

### Media Preview

| Image Viewer | Video Player | Audio Player | PDF Preview |
|-------------|-------------|-------------|------------|
| ![Image Viewer](docs/screenshots/image-viewer.png) | ![Video Player](docs/screenshots/video-player.png) | ![Audio Player](docs/screenshots/audio-player.png) | ![PDF Preview](docs/screenshots/pdf-preview.png) |

### SSH Tunnel & Web Terminal

| Port Forwarding | Interactive Terminal |
|----------------|---------------------|
| ![Port Forwarding](docs/screenshots/port-forwarding.png) | ![Interactive Terminal](docs/screenshots/terminal.png) |

---

## Technical Architecture

ClawBench's core philosophy:

- **Zero-Adaptation Passthrough**: Instead of reimplementing AI capabilities, ClawBench uses AI coding agent CLIs as backend engines, wrapping them as HTTP API + SSE streaming interfaces via a web server. This fully preserves tool calls, extended thinking, Skills, MCP, and all other capabilities with zero adaptation cost. The frontend only handles rendering and interaction — all intelligent logic is natively provided by the CLI.
- **AI Handles Changes, I Handle Review**: The project does not provide direct file editing capabilities — all modifications are done through AI. The focus is on building an excellent Markdown and code preview experience, along with interaction with AI during preview — select code or text to ask AI questions or request modifications for rapid iteration.

```mermaid
graph LR
    Client["📱 Phone / PWA / Pad"] -->|HTTP / SSE| Server["🏗️ ClawBench\nGo Web Server"]
    Server -->|CLI Invocation · Stream Output| CB["🤖 CodeBuddy CLI"]
    Server -->|CLI Invocation · Stream Output| CC["🤖 Claude Code CLI"]
    Server -->|CLI Invocation · Stream Output| OC["🤖 OpenCode CLI"]
    Server -->|CLI Invocation · Stream Output| CX["🤖 Codex CLI"]
    Server -->|CLI Invocation · Stream Output| QR["🤖 Qoder CLI"]
    Server -->|CLI Invocation · Stream Output| VC["🤖 VeCLI"]
    Server -->|CLI Invocation · Stream Output| DS["🐋 CodeWhale"]
    Server -->|CLI Invocation · Stream Output| MM["🚀 MiMo-Code"]
    Server -->|CLI Invocation · Stream Output| PI["🥧 Pi"]
    Server -->|CLI Invocation · Stream Output| CL["🤖 Cline"]
    Server -->|CLI Invocation · Stream Output| CP["🤖 Copilot"]
    Server -->|CLI Invocation · Stream Output| KM["🤖 Kimi"]
    Server -->|Read/Write| DB[("💾 SQLite\nSessions · History · Scheduled Tasks")]
    CB -->|Native Support| Tools["🔧 Tool Calls"]
    CB -->|Native Support| Think["🧠 Extended Thinking"]
    CB -->|Native Support| Skills["🎯 Skill"]
    CB -->|Native Support| MCP["🔌 MCP"]
    CC -->|Native Support| Tools
    CC -->|Native Support| Think
    CC -->|Native Support| Skills
    CC -->|Native Support| MCP
    OC -->|Native Support| Tools
    CX -->|Native Support| Tools
    QR -->|Native Support| Tools
    VC -->|Native Support| Tools
    DS -->|Native Support| Tools
    MM -->|Native Support| Tools
    MM -->|Native Support| Think
    PI -->|Native Support| Tools
    PI -->|Native Support| Think
```

---

## Quick Start

### Prerequisites

- **A PC (Linux / macOS / Windows)**: To run the ClawBench server, with at least one AI coding agent CLI installed (CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Cline, Copilot, or Kimi)
- **A phone**: Install the [ClawBench Android App](https://github.com/xulongzhe/clawbench/releases), or use a mobile browser (Chrome recommended) to access the server address

### Download & Start

Download the latest ZIP package from [GitHub Releases](https://github.com/xulongzhe/clawbench/releases), extract and you're ready:

```bash
wget https://github.com/xulongzhe/clawbench/releases/latest/download/clawbench-linux-amd64.zip
unzip clawbench-linux-amd64.zip
cd clawbench
./clawbench
```

That's it — on every startup, ClawBench automatically scans for installed AI CLI tools, generates minimal agent configs for each detected backend, and auto-discovers available models and thinking effort levels. No manual configuration needed.

> If no AI CLI is installed but the embedded Pi agent is available (from the release package or `--with-pi` build), a setup wizard appears on first launch, guiding you through selecting an LLM provider, entering your API key, verifying model access, and creating an agent — all in one click.

> A random 8-character hex password is auto-generated on first startup and printed to the console in a bordered box. Save it securely.

Once deployed, access `http://server-ip:20000` from your phone app or mobile browser:

- **Phone App**: Native integration, auto-connect, full feature support
- **Mobile Browser**: **Chrome** recommended — supports installing as a PWA app (Add to Home Screen) for a near-native experience

### Customize Agents

Auto-discovered agent configs use minimal defaults (no model lists or thinking effort levels — these are auto-discovered at runtime). To customize model lists, system prompts, etc., create new agents via the setup wizard in the Web UI. Agent configs are stored in the database.

> For build instructions, advanced configuration, deployment, and architecture details, see **[Build & Development Guide](docs/DEVELOPMENT.en.md)**.

---

## Features

### 📁 File Browser
- Recursive directory browsing with 120+ file extension support
- Search filtering, sorting (name/time/extension/size)
- **List/Grid View Toggle**: Grid view shows image thumbnails for visual file browsing
- **Image Thumbnails**: Backend generates square thumbnails with dominant-color padding for quick preview
- Context menu: rename, delete, copy, cut, paste, new file/folder, download, open as project
- **Multi-Select Operations**: Toggle multi-select mode from toolbar, batch copy/cut/delete; mobile long-press triggers context menu
- File upload (all file types supported, configurable size and count)
- Toggle hidden file visibility
- **Drill-down Browsing + Edge Swipe Back**: Tap folders to drill down, swipe from right edge to go back — intuitive mobile navigation
- **File Preview Overlay**: Click a file to open a preview overlay on top of the browse tab, no tab switching needed; supports navigation stack (multi-file switching + back), close to return to file list
- **Binary File Preview**: Binary files show a placeholder UI with "Open as text" option; large files auto-truncate (64KB binary / 512KB text), truncation notice banner when truncated

### 🎨 Code Preview
- Syntax highlighting, sticky line numbers, word wrap toggle
- **Sticky Scroll**: VS Code-style sticky scroll that shows enclosing scope context (functions, classes, structs, etc.) as you scroll
- Double-click to copy code line content (flash animation feedback)
- **File Change Flash Highlight**: When files are modified externally, deleted characters flash red and new characters flash blue for quick change identification
- **Quote & Ask**: Select a code snippet, one-click ask AI, auto-attaches file path and line number
- **File Path Navigation**: Clickable file paths in code previews with import path resolution (e.g., @/composables/useFoo resolves to the actual file path); line range navigation support (e.g., `file.go:42-50`) with flash highlight
- Swipe gestures: swipe left/right to switch files

### 📝 Markdown
- Toggle between rendered view / source view
- **Quote & Ask**: Select text, one-click ask AI
- Smart table of contents drawer (TOC) with tree-sitter code symbol extraction (100+ languages, 17 symbol kind icons), LaTeX math, Mermaid diagrams
- **Image Lightbox**: Images support zoom, swipe browsing
- **File Path Navigation**: Clickable file paths in Markdown, with line range navigation

### 🤖 AI Agents
- **Streaming Response**: Real-time SSE push, thinking process and tool calls fully visible
- **Multi-Agent Support**: General assistant, coding expert, handyman, etc. — YAML config, plug-and-play
- **AI Backend Switching**: CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Cline, Copilot, Kimi — session-level isolation
- **Thinking Effort Levels**: Per-agent thinking depth selection (Auto / Low / Medium / High), supported by 9 backends (Claude/CodeBuddy/OpenCode/Codex/MiMo/Pi/Cline/Copilot/Kimi), selection auto-persisted
- **Model Selection Modal**: Unified model switching and thinking effort selection in a dual-tab interface, with search filtering, one-click model list refresh (for agents supporting auto-discovery), and long-press to set default model
- **Model Selection Persistence**: Model choice and thinking effort per agent auto-saved to localStorage, restored on reload/session switch
- **Scheduled Tasks**: AI creates Cron schedules via CLI subcommands, executes automatically; independent tab with 4-level breadcrumb navigation; task cards embedded in chat messages; frequency presets (hourly/daily/weekly/monthly) + custom cron expressions; per-execution read tracking + TTS playback; execution auto-summary + completion notification (sound/haptic/toast)
- **Continue Conversation**: One-click continue conversation from task execution detail, auto-copies history messages and summaries to a new session, inherits backend/agent/model/thinking effort; sessions originated from scheduled tasks show a purple "Task" badge in session list
- **Setup Wizard**: When no AI CLI is installed, the embedded Pi agent provides a 5-step guided setup (Welcome → Select Provider → Enter API Key → Verify Model → Name Agent), supporting 23 LLM providers (OpenAI, Anthropic, Google, DeepSeek, Alibaba Qwen, etc.); supports custom URL mode for any OpenAI/Anthropic-compatible endpoint with auto-detected API format and direct HTTP verification (no Pi CLI needed). API keys are encrypted with AES-256-GCM and encryption keys auto-rotate on password change
- **Multi-Session Management**: Create, switch, delete independent sessions, swipe to switch
- **Swipe Session Toggle**: Toggle left/right swipe session switching in Settings → Chat; defaults to off to prevent accidental switches when scrolling wide content
- **Image Upload**: Upload images for AI conversation (multimodal)
- **Disconnect Protection**: Messages persist immediately, no data loss on disconnect, 15s heartbeat keep-alive + 30s timeout auto-reconnect (live content updates during polling fallback)
- **Auto Resume**: Automatically sends "continue" after Claude/CodeBuddy/Qoder/CodeWhale/MiMo/Pi/Cline/Copilot/Kimi exits Plan Mode
- **Message Queue**: Messages queue when AI is busy, sent sequentially
- **Auto Summary**: Automatically generates a summary of the last assistant message on session complete; toggle between summary/original via bottom banner; TTS playback also uses the summary
- **@ Commands**: Type `@chatsearch` to search conversation history, `@task` to manage scheduled tasks — autocomplete popup menu, purple command badge in user messages
- **RAG Results Card**: RAG search results in AI responses rendered as purple-themed cards; click to open detail drawer, one-click resume conversation
- **Inline Thinking Streaming**: Thinking process streams inline during active session; auto-collapses to clickable chip on completion
- **Session Progress Indicator**: Session drawer shows capsule progress bar with color-coded fill (blue/orange/red) based on usage

### ⏰ Scheduled Tasks
- **Cron Scheduling**: AI creates Cron schedules via CLI subcommands, executes automatically
- **Independent Tab Management**: 4-level breadcrumb navigation, quickly switch between task lists
- **Frequency Presets**: Hourly / Daily / Weekly / Monthly — one-click selection of common frequencies
- **Custom Cron Expressions**: Full 5-field Cron syntax support, flexibly customize execution timing
- **Task Cards**: Embedded chat message preview, at-a-glance task content and recent execution results
- **Execution Tracking**: Per-execution read status tracking, unread message badge alerts
- **TTS Playback**: Auto-summarize after task completion with voice playback, listen while reviewing
- **Execution Summary**: Auto-generated summary for each completed execution (configurable summarization backend)
- **Completion Notification**: Sound + haptic + toast alert when task execution completes
- **Instant Trigger**: Support immediate manual execution without affecting next scheduled time
- **Lifecycle Management**: Pause / Resume / Delete — flexibly control task state

### 🤖 AI Conversation
- **Tool Call Visualization**: Name, parameters, execution results displayed in real time with success/error status
- **Extended Thinking**: Complex tasks auto-trigger extended thinking, reasoning visible in real time
- **File Path Navigation**: Clickable file paths in AI responses, with line range navigation
- **Localhost URL Navigation**: localhost URLs in AI responses (e.g., http://localhost:3000) are auto-detected with an open button; in App mode, port forwarding is auto-registered and the URL opens via WebView with zero manual config
- **Quick Send**: Preset common commands (continue, build, commit, etc.) with drag reorder, one-click send, input placeholder hint showing current quick send; long-press fills input box (with progressive fill bar) for editing before sending
- **Quote & Ask**: Select code or text, ask AI directly, auto-attaches context
- **Current Directory Attachment**: Chat input supports attaching current directory context, AI auto-gets directory structure
- **Unread Badge**: Chat panel icon shows unread message count

- **Auto-Approve Indicator**: Mode chip turns green when auto-approve is enabled, providing visual feedback for ACP permission mode

### 🖼️ Media Preview
- In-app preview of images, audio, video
- Lightbox zoom, fullscreen view, support for pinch-zoom and drag

### 🔊 TTS Speech Synthesis
- Auto-summarize and read AI replies aloud, listen while reading
- **5 TTS Engines**: Edge TTS (free, native Go implementation, no external dependency), MiniMax (best quality), Piper / Kokoro / MOSS-Nano (local offline)
- **11 Summarization Backends**: simple (text-only cleanup), mmx-cli, api (OpenAI/Anthropic compatible), Claude, CodeBuddy, OpenCode, Codex, Qoder, VeCLI, DeepSeek, Pi
- See [TTS Deployment Guide](docs/TTS.en.md)

### 📂 Git Integration
- Project-level / file-level commit history browsing
- **Git Branch Graph**: Vertical branch topology, intuitive branch relationships
- **Git Diff View**: View changes relative to HEAD, character-level highlighting
- Commit detail view (author, time, commit message)
- Working tree changes view (staged / unstaged files)
- **3-Tab Management**: Worktree / Branches / Tags tabs for unified management, default tab persisted to localStorage
- **Swipe to Delete**: Branches, worktrees, and tags support swipe-to-delete with safety guards (current branch, default branch, and current worktree cannot be deleted)
- **Tag Management**: Browse project tags, click a tag to checkout, auto-prompt for dirty working tree

### 🔀 SSH Tunnel Port Forwarding
- **Remote Development**: Access server local ports directly from Android App
- **Protocol Transparent**: HTTP, HTTPS, WebSocket, SSE, gRPC — no URL rewriting needed
- **Custom Target Host**: Forward to any reachable host (LAN/remote, not limited to 127.0.0.1)
- **Auto Port Assignment**: Automatically allocates local ports when forwarding the same target port to different hosts
- **Port Editing**: Modify existing port forwarding configurations
- **Auto-Open Localhost URLs**: localhost URLs appearing in chat (e.g., web services started by AI) can be opened with one tap — port forwarding is auto-registered and the URL opens via WebView in App mode
- **Tunnel Health Check & Reconnect**: Auto-checks tunnel health before opening localhost URLs; reconnects if unhealthy; one-tap reconnect for disconnected tunnels

### 💻 Web Terminal
- **Interactive Terminal**: PTY + WebSocket + xterm.js, operate server terminal directly in browser
- **Concurrent Sessions**: Each client gets an independent PTY session, no interference
- **Virtual Key Toolbar**: Color-coded key groups (modifiers, shortcuts, navigation, arrows, actions), three-state modifier toggle
- **Symbol Bar**: Expandable symbol input row with 19 high-frequency terminal symbols, smart sorting using exponential decay (balances frequency and recency)
- **Touch Gestures**: Termius-style gestures (swipe→arrow keys, hold-to-repeat, double-tap→Tab, pinch-to-zoom), touch scroll when gestures disabled
- **Quick Commands**: CRUD management of common commands with drag reorder, hidden flag, auto-execute (auto-run on every connect/reconnect)
- **Android Volume Keys**: Volume up/down remapped to arrow keys when terminal is open in the app
- See [Web Terminal User Guide](docs/TERMINAL.en.md)

### 🌐 Internationalization
- Chinese / English bilingual UI, auto-detect system language

### 📱 Android App
- Native bridge integration: auto-login, file download (including POST archive downloads), port forwarding management
- Static HTML login page: shown on first launch or connection failure, matches web UI visual style
- SSH password management, server dialog
- WebView connection protection: WebView hidden during connection attempts to prevent browser error page flash
- Terminal volume key mapping: volume keys act as arrow keys when terminal is open

### 🔔 Notifications
- Notification sound + haptic feedback (alerts when AI completes)
- Browser push notifications
- **Task Completion Push**: Scheduled task completion notifications include response preview summary; tap to navigate to execution details
- **ACP Approval Push**: When ACP agents need user approval (e.g., file write, command execution), JPush sends Android notification showing tool name; tap to navigate directly to session for approval

### 🎨 Themes
- Light / Dark mode, follows system preference

### 📱 PWA Support
- Installable to home screen, runs in standalone window

### 🔒 Security
- Optional password protection (SHA-256 salted hash storage, password change available in settings panel)
- API Key encrypted storage (AES-256-GCM + HKDF-SHA256 key derivation, encryption key auto-rotation on password change)
- Multi-instance cookie isolation (cookies auto-prefixed by port, no collisions on same domain)
- Path traversal protection, all operations restricted to project directory
- Git parameter injection protection (SHA/branch name/tag name validation, `--` separator)
- Configurable file upload size and count (default 100MB / 20 files), all file types supported
- XSS protection (DOMPurify sanitization)
- TLS support (manual certificate configuration required)

---

## FAQ

See **[FAQ](docs/FAQ.en.md)**.

---

## License

Copyright (c) 2026 xulongzhe

Licensed under the MIT License
