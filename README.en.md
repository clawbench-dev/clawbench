[中文](README.md) | [English](README.en.md)

# ClawBench — AI Workstation Built for Mobile

<p align="center">
  <img src="docs/screenshots/product_hero.png" alt="ClawBench" width="960">
</p>

> 🎬 **Demo Video**: [OpenClaw and Hermes are toys, so I built one that actually works](https://b23.tv/ewACF0h) — Bilibili

**From Terminal to Palm** — An AI workstation built for mobile.

Brings the full power of AI coding agents to browsers and mobile apps, creating a true mobile development environment. File browsing, code editing, AI conversation, Git operations, scheduled tasks — one app does it all.

Core Advantage: Native passthrough of AI capabilities (tool calls, extended thinking, Skills, MCP) with zero adaptation cost, fully preserving the power of coding agents. Unlike other mobile AI tools that are merely "remote controllers," ClawBench is a full-featured mobile workstation — files, code, Git, AI, scheduled tasks, TTS, get real work done on your phone without needing a PC online.

- **Supported Platforms**: Browser (PC / Tablet / Phone), Android App, PWA
- **AI Backends**: CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Copilot, Kimi, Antigravity, Grok Build

<p align="center">
  <img src="assets/architecture.en.svg" alt="ClawBench Deployment Architecture" width="640">
</p>

---

## Screenshots

### Login & Navigation

| Login | Home | Select Project | Settings Panel |
|-------|------|----------------|----------------|
| ![Login](docs/screenshots/login.png) | ![Home](docs/screenshots/home.png) | ![Select Project](docs/screenshots/project-select.png) | ![Settings Panel](docs/screenshots/settings-panel.png) |

### File Browsing & Code Editing

| File Browser | Search & Filter | Code Editor | Quote & Ask |
|-------------|----------------|-------------|-------------|
| ![File Browser](docs/screenshots/file-browser.png) | ![Search & Filter](docs/screenshots/file-search.png) | ![Code Editor](docs/screenshots/code-editor.png) | ![Quote & Ask](docs/screenshots/quote-question.png) |

### Markdown & Document Preview

| Markdown Render | LaTeX Formulas | Mermaid Diagrams | Table of Contents |
|-----------------|----------------|------------------|-------------------|
| ![Markdown Render](docs/screenshots/markdown-preview.png) | ![LaTeX Formulas](docs/screenshots/latex-formula.png) | ![Mermaid Diagrams](docs/screenshots/mermaid-diagram.png) | ![Table of Contents](docs/screenshots/toc-drawer.png) |

### AI Agents

| Agent Selection | AI Conversation | ACP Permission | RAG Search | Session Manager |
|-----------------|-----------------|----------------|------------|-----------------|
| ![Agent Selection](docs/screenshots/agent-selector.png) | ![AI Conversation](docs/screenshots/chat-panel.png) | ![ACP Permission](docs/screenshots/acp-permission.png) | ![RAG Search](docs/screenshots/rag-search.png) | ![Session Manager](docs/screenshots/session-manager.png) |

| Scheduled Tasks | Create Task | Task Card |
|-----------------|-------------|-----------|
| ![Scheduled Tasks](docs/screenshots/scheduled-tasks.png) | ![Create Task](docs/screenshots/task-create.png) | ![Task Card](docs/screenshots/schedule-proposal.png) |

### Git Integration

| Commit History & Branch Graph | Branch Management | Commit Detail | Comparison Report |
|-------------------------------|-------------------|---------------|-------------------|
| ![Commit History & Branch Graph](docs/screenshots/git-history.png) | ![Branch Management](docs/screenshots/git-branches.png) | ![Commit Detail](docs/screenshots/git-commit-detail.png) | ![Comparison Report](docs/screenshots/git-comparison-report.png) |

### Media Preview

| Image Viewer | Video Player | Audio Player | PDF Preview |
|-------------|-------------|-------------|------------|
| ![Image Viewer](docs/screenshots/image-viewer.png) | ![Video Player](docs/screenshots/video-player.png) | ![Audio Player](docs/screenshots/audio-player.png) | ![PDF Preview](docs/screenshots/pdf-preview.png) |

### SSH Tunnel & Web Terminal

| Port Forwarding | Interactive Terminal | Key/Symbol Configuration |
|----------------|---------------------|-------------------------|
| ![Port Forwarding](docs/screenshots/port-forwarding.png) | ![Interactive Terminal](docs/screenshots/terminal.png) | ![Key/Symbol Configuration](docs/screenshots/terminal-key-config.png) |

### System Resource Monitor

| System Monitor |
|----------------|
| ![System Monitor](docs/screenshots/system-monitor.png) |

- Real-time monitoring of server CPU, memory, disk, and network usage
- Header panel display with WebSocket push updates
- Auto-switches to connection status indicator (disconnected/reconnecting) when WS is down, replacing the resource panel

---

## Quick Start

### Prerequisites

- **A PC (Linux / macOS / Windows)**: To run the ClawBench server, with at least one AI coding agent CLI installed (CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Copilot, or Kimi)
- **A phone**: Install the [ClawBench Android App](https://github.com/xulongzhe/clawbench/releases), or use a mobile browser (Chrome recommended) to access the server address

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
wget https://github.com/xulongzhe/clawbench/releases/latest/download/clawbench-linux-amd64.zip
unzip clawbench-linux-amd64.zip
cd clawbench
./clawbench
```

### Docker Deployment

```bash
docker pull ghcr.io/clawbench-dev/clawbench:latest
docker run -d -p 20000:20000 -v clawbench-data:/data ghcr.io/clawbench-dev/clawbench:latest
```

Customize the host port with `-p` (e.g., `-p 20300:20000`). The `clawbench-data` volume persists all data. To view the auto-generated password:

```bash
docker exec $(docker ps -qf ancestor=ghcr.io/clawbench-dev/clawbench) cat /data/.clawbench/auto-password
```

> A random 8-character hex password is auto-generated on first startup and printed to the console in a bordered box. Save it securely.

Once deployed, access `http://server-ip:20000` from your phone app or mobile browser:

- **Phone App**: Native integration, auto-connect, full feature support
- **Mobile Browser**: **Chrome** recommended — supports installing as a PWA app (Add to Home Screen) for a near-native experience

> 📡 **Public Access**: To access ClawBench from the public internet (commuting, traveling, etc.), see the **[Public Access Guide](docs/PUBLIC_ACCESS.md)**  — supports IPv6 direct connection, FRP tunnel, and EasyTier decentralized networking (no VPS required).

---

## Features

### 📁 File Browser
- Recursive directory browsing with 120+ file extension support (including Office documents .docx/.xlsx/.xls/.pptx)
- Search filtering, sorting (name/time/extension/size)
- **Office document preview**: Word, Excel, and PowerPoint documents rendered natively in the browser — no download needed
- **File Preview Overlay**: Office files open in a preview overlay on top of the browse tab, supporting navigation stack (multi-file switching + back)
- **List/Grid View Toggle**: Grid view shows image thumbnails for visual file browsing
- **Image Thumbnails**: Backend generates square thumbnails with dominant-color padding for quick preview
- Context menu: rename, delete, copy, cut, paste, new file/folder, download, open as project
- **Multi-Select Operations**: Toggle multi-select mode from toolbar, batch copy/cut/delete; mobile long-press triggers context menu
- File upload (all file types supported, configurable size and count)
- Toggle hidden file visibility
- **Document search exclusion**: Office documents are excluded from file content search to improve performance (same as PDF)
- **Drill-down Browsing + Edge Swipe Back**: Tap folders to drill down, swipe from right edge to go back — intuitive mobile navigation
- **Ctrl+F/Cmd+F Context-Aware Search**: Automatically opens the appropriate search drawer based on current tab — Chat tab: session search (RAG); Browse tab with file overlay: in-file content search; Browse tab without overlay: filename search; if already open, focuses the search input
- **File Preview Overlay**: Click a file to open a preview overlay on top of the browse tab, no tab switching needed; supports navigation stack (multi-file switching + back), close to return to file list
- **Binary File Preview**: Binary files show a placeholder UI with "Open as text" option; large files auto-truncate (64KB binary / 512KB text), truncation notice banner when truncated

### 🎨 Code Preview & Editing
- CodeMirror-based code browsing and editing dual mode, read-only by default, one-click switch to edit mode
- Syntax highlighting, sticky line numbers, word wrap toggle, 30+ language extensions (high-frequency static imports, low-frequency lazy loading)
- **Sticky Scroll**: VS Code-style sticky scroll based on backend tree-sitter symbol data, showing enclosing scope context (functions, classes, structs, etc.) as you scroll
- Double-click to copy code line content (flash animation feedback)
- **File Change Flash Highlight**: When files are modified externally, deleted characters flash red and new characters flash blue for quick change identification
- **Quote & Ask**: Select a code snippet, one-click ask AI, auto-attaches file path and line number
- **File Path Navigation**: Clickable file paths in code previews with import path resolution (e.g., @/composables/useFoo resolves to the actual file path); line range navigation support (e.g., `file.go:42-50`) with flash highlight
- **Edit Mode**: undo/redo, save & exit, unsaved changes confirmation dialog, visual distinction for edit mode (accent-tinted background + top border)
- **Markdown Heading-Anchored Scroll Sync**: Scroll position synchronized between rendered view and source edit based on heading anchors
- Swipe gestures: swipe left/right to switch files

### 📝 Markdown
- Toggle between rendered view / source view
- **Quote & Ask**: Select text, one-click ask AI
- Smart table of contents drawer (TOC) with tree-sitter code symbol extraction (100+ languages, 17 symbol kind icons), LaTeX math, Mermaid diagrams
- **Image Lightbox**: Images support zoom, swipe browsing
- **File Path Navigation**: Clickable file paths in Markdown, with line range navigation

### 🤖 AI Agents
- **Streaming Response**: Real-time WebSocket push, thinking process and tool calls fully visible
- **Multi-Agent Support**: General assistant, coding expert, handyman, etc.; custom agents can be loaded via `config/agents/*.yaml` (supplementary method for non-standard agents)
- **AI Backend Switching**: CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Copilot, Kimi, Antigravity, Grok Build — session-level isolation
- **Thinking Effort Levels**: Per-agent thinking depth selection (Low / Medium / High, etc.), supported by 10 backends (Claude/CodeBuddy/OpenCode/Codex/MiMo/Pi/Copilot/Kimi/Antigravity/Grok), selection auto-persisted
- **Model Selection Modal**: Unified model switching and thinking effort selection in a dual-tab interface, with search filtering, one-click model list refresh (for agents supporting auto-discovery), and long-press to set default model
- **Model Selection Persistence**: Model choice and thinking effort per agent auto-saved to localStorage, restored on reload/session switch
- **Scheduled Tasks**: AI creates Cron schedules via CLI subcommands, executes automatically; independent tab with 4-level breadcrumb navigation; task cards embedded in chat messages; frequency presets (hourly/daily/weekly/monthly) + custom cron expressions; per-execution read tracking + TTS playback; execution auto-summary + completion notification (sound/haptic/toast)
- **Continue Conversation**: One-click continue conversation from task execution detail, auto-copies history messages and summaries to a new session, inherits backend/agent/model/thinking effort; sessions originated from scheduled tasks show a purple "Task" badge in session list
- **Multi-Session Management**: Create, switch, archive independent sessions, swipe to switch; archived sessions recoverable via search, physical delete (irreversible) and archive retention auto-cleanup available; Ctrl/Cmd+Delete to quick-archive current session
- **Swipe Session Toggle**: Toggle left/right swipe session switching in Settings → Chat; defaults to off to prevent accidental switches when scrolling wide content
- **Image Upload**: Upload images for AI conversation (multimodal)
- **Disconnect Protection**: Messages persist immediately, no data loss on disconnect, 15s heartbeat keep-alive + 30s timeout auto-reconnect (live content updates during polling fallback); on reconnect, auto-checks session state to prevent UI stuck when AI completed during disconnect
- **Auto Resume**: Automatically sends "continue" after Claude/CodeBuddy/Qoder/CodeWhale/MiMo/Pi/Copilot/Kimi exits Plan Mode
- **Message Queue**: Messages queue when AI is busy, sent sequentially
- **Message Clusters**: Auto-analyze chat history patterns, group semantically similar user messages into clusters, one-click add to Quick Send; Union-Find + Sørensen-Dice similarity, on-demand computation with progress tracking
- **Auto Summary**: Automatically generates a summary of the last assistant message on session complete; toggle between summary/original via bottom banner; TTS playback also uses the summary
- **@ Commands**: Type `@chatsearch` to search conversation history, `@task` to manage scheduled tasks — autocomplete popup menu, purple command badge in user messages
- **RAG Results Card**: RAG search results in AI responses rendered as purple-themed cards; click to open detail drawer, one-click resume conversation
- **Inline Thinking Streaming**: Thinking process streams inline during active session; auto-collapses to clickable chip on completion; thinking content lazy-loaded — after stream ends, only thumbnail is kept, full text loaded on demand when expanded
- **Session Progress Indicator**: Session drawer shows capsule progress bar with color-coded fill (blue/orange/red) based on usage
- **ACP Context State Persistence**: Mode, thinking effort, and context usage auto-persisted to database; state survives server restarts

### 🤖 AI Conversation
- **Tool Call Visualization**: Name, parameters, execution results displayed in real time with success/error status
- **Extended Thinking**: Complex tasks auto-trigger extended thinking, reasoning visible in real time
- **File Path Navigation**: Clickable file paths in AI responses, with line range navigation
- **Localhost URL Navigation**: localhost URLs in AI responses (e.g., http://localhost:3000) are auto-detected with an open button; in App mode, port forwarding is auto-registered and the URL opens via WebView with zero manual config
- **Quick Send**: Preset common commands (continue, build, commit, etc.) with drag reorder, one-click send, input placeholder hint showing current quick send; long-press fills input box (with progressive fill bar) for editing before sending; message clusters analysis discovers recurring patterns and adds them
- **Quote & Ask**: Select code or text, ask AI directly, auto-attaches context
- **Current Directory Attachment**: Chat input supports attaching current directory context, AI auto-gets directory structure
- **Drag & Paste Upload**: Drag files onto chat area or paste clipboard content (screenshots/files), auto-upload and attach as tags without opening the attach drawer
- **Compact Context**: When ACP session context usage ≥ 75%, a "Compact context" button appears in the session-info bar, one-click sends `/compact` command to free context space
- **Unread Badge**: Chat panel icon shows unread message count
- **Attach Drawer Footer**: Selected files shown as persistent scrollable tags at the bottom of the attach drawer, with direct removal support
- **Auto-Approve Indicator**: Mode chip turns green when auto-approve is enabled, providing visual feedback for ACP permission mode

### 🖼️ Media Preview
- In-app preview of images, audio, video
- Lightbox zoom, fullscreen view, support for pinch-zoom and drag

### 📄 Office Document Preview
- **Word (.docx)**: Native document rendering with table and image support
- **Excel (.xlsx/.xls)**: Spreadsheet preview with multi-sheet switching, toolbar auto-hidden
- **PowerPoint (.pptx)**: Slide-by-slide preview with pinch-to-zoom (mobile) and Ctrl+scroll zoom (desktop)
- **Loading & Error Handling**: Skeleton animation on load; retry and download buttons on failure
- **AI Integration**: Select text from Office documents and one-click ask AI, file path context auto-attached

### 🔊 TTS Speech Synthesis
- Auto-summarize and read AI replies aloud, listen while reading
- **5 TTS Engines**: Edge TTS (free, native Go implementation, no external dependency), MiniMax (best quality), Piper / Kokoro / MOSS-Nano (local offline)
- **Summarization Backends**: simple (text-only cleanup) and api (OpenAI/Anthropic compatible) modes
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
- **Multi-Tab Management**: Close all tabs, empty state with create button, dock icon shows active session count
- **Three-Mode Gesture System**: Browse (default, touch scroll), Gesture (Termius-style swipe→arrow keys, hold-to-repeat, double-tap→Tab, pinch-to-zoom), Selection (drag-to-select text + floating copy bar)
- **Virtual Key Toolbar**: Color-coded key groups (modifiers, shortcuts, navigation, arrows, actions), three-state modifier toggle
- **Key/Symbol Configuration**: Full-screen configuration drawer with keys and symbols dual tabs; supports tap-to-add, drag-to-reorder, gesture mode auto-hides certain keys; configuration persisted to database
- **Symbol Bar**: Expandable symbol input row with 19 high-frequency terminal symbols, smart sorting using exponential decay (balances frequency and recency)
- **Selected Text Auto-Copy**: Selected terminal text automatically copied to clipboard with toast feedback
- **Quick Commands**: CRUD management of common commands with drag reorder, hidden flag, auto-execute (auto-run on every connect/reconnect)
- **Android Volume Keys**: Volume up/down remapped to arrow keys when terminal is open in the app
- **Android Soft Keyboard Stability**: Read-only mode prevents soft keyboard popup; tapping terminal avoids keyboard collapse-then-reopen flicker
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
- **DingTalk/Feishu Bot Push**: Instant push via DingTalk or Feishu bot on AI session completion, permission approval, and scheduled task status changes; view session list and send messages to sessions from IM
- See [DingTalk Push Setup](docs/DINGTALK_PUSH.en.md) | [Feishu Push Setup](docs/FEISHU_PUSH.en.md)


### 🎨 Themes
- Light / Dark mode, follows system preference

### 📱 PWA Support
- Installable to home screen, runs in standalone window

### 🔒 Security
- Optional password protection (SHA-256 salted hash storage, password change available in settings panel)
- Multi-instance cookie isolation (cookies auto-prefixed by port, no collisions on same domain)
- Path traversal protection, all operations restricted to project directory
- Git parameter injection protection (SHA/branch name/tag name validation, `--` separator)
- Configurable file upload size and count (default 100MB / 20 files), all file types supported
- XSS protection (DOMPurify sanitization)
- TLS support (manual certificate configuration required)

---

## FAQ

See **[FAQ](docs/FAQ.en.md)** .

---

## License

Copyright (c) 2026 xulongzhe

Licensed under the MIT License
