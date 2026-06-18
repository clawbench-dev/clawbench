[中文](COMPARISON.md) | [English](COMPARISON.en.md)

# Competitive Comparison

A comprehensive comparison of mobile AI programming tools, covering both open-source and closed-source products.

## Product Overview

| Product | Type | Positioning | Open Source | Stars |
|---------|------|-------------|-------------|-------|
| **ClawBench** | Self-hosted Web workstation | Mobile AI workstation (files + code + AI + Git + scheduling) | ✅ MIT | — |
| **Operit** | Native Android AI Agent | Full-featured mobile AI Agent (built-in Ubuntu + 40+ tools + local models) | ✅ LGPLv3 | 4.4k |
| **Yep Anywhere** | Self-hosted Web UI | Mobile monitoring/interaction with Claude Code and Codex Agents | ✅ MIT | 380 |
| **Happy** | Remote controller | Remotely control Claude Code/Codex sessions running on your PC from your phone | ✅ MIT | 19.9k |
| **AnyCoding** | Remote controller | Native Android terminal mirroring for AI CLIs (same PTY process) | ✅ Apache 2.0 (client) | 9 |
| **AI Agent Remote** | Remote controller | Browser-based remote control of AI Agent CLIs (Claude/Qwen/Gemini) | ✅ MIT | — |
| **FeiLaude** | IM bot remote controller | Feishu Bot bridging local Claude Code (message-driven execution + streaming status) | ✅ MIT | 2 |
| **Claude Dispatch** | Official remote control | Anthropic's official mobile remote control for Claude Code (Cowork family) | ❌ Closed source | — |
| **Claude Remote** | Remote controller | Unofficial Claude Code remote control, supports API users | ✅ | 30 |
| **Paseo** | Agent orchestration platform | Multi-device AI agent orchestration (Daemon+Relay, control multiple AI Agents across phone/desktop/CLI) | ✅ AGPL-3.0 | 5.5k |
| **Cursor Background Agent** | Cloud async Agent | Submit tasks via web/mobile, async execution in the cloud, view results / create PRs | ❌ Closed source | — |
| **GitHub Copilot** | Official integration | GitHub mobile + web AI programming assistant | ❌ Closed source | — |

## Feature Matrix

| Feature | ClawBench | Operit | Yep Anywhere | Happy | AnyCoding | AI Agent Remote | FeiLaude | Claude Dispatch | Claude Remote | Cursor Agent | GitHub Copilot | Paseo |
|---------|-----------|--------|-------------|-------|-----------|-----------------|----------|-----------------|---------------|--------------|----------------|-------|
| **AI backend count** | 12 | 10+ | 2 | 2 | 2+ | 5 | 1 | 1 | 1 | Built-in | Built-in | 3 |
| **File browsing/editing** | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Code preview/syntax highlighting** | ✅ | ✅ | ✅ (server-rendered) | ❌ | ✅ (terminal) | ✅ (terminal) | ❌ | ❌ | ✅ | ✅ | ✅ | ❌ |
| **Git integration** | ✅ (branch graph/Diff/history) | ✅ (basic) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (PR creation/Diff) | ✅ (PR Review) | ❌ (Diff viewing only) |
| **Markdown rendering** | ✅ (KaTeX/Mermaid) | ✅ (KaTeX/Mermaid) | ✅ (server-rendered) | ❌ | ❌ | ❌ | ✅ (Feishu cards) | ❌ | ✅ | ❌ | ❌ | ❌ |
| **Scheduled tasks (Cron)** | ✅ | ✅ (workflow scheduled triggers) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **TTS speech synthesis** | ✅ (5 engines) | ✅ (local + cloud) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Media preview** | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **SSH tunnel port forwarding** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **End-to-end encryption** | ❌ | ❌ | ✅ (SRP-6a + TweetNaCl) | ✅ | ✅ (Direct mode) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (ECDH + AES-256-GCM) |
| **Real-time voice** | ❌ | ✅ (STT + TTS + voice wake-up) | ✅ (voice input) | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (Push-to-Talk STT) |
| **Push notifications** | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ (Feishu message push) | ✅ | ❌ | ❌ | ✅ | ✅ |
| **Multi-client sync** | ❌ | ❌ | ✅ | ✅ | ✅ (multi-tab + multi-PC) | ✅ (multi-browser) | ✅ (multi-user) | ✅ | ✅ | ❌ | ❌ | ✅ |
| **Permission approval** | ❌ | ✅ (tool-level permission control) | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ | ✅ |
| **PWA install** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| **Self-hosted** | ✅ | ❌ (local app only) | ✅ | ✅ (optional) | ✅ (Direct mode) | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ✅ |
| **Offline/LAN** | ✅ | ✅ (local models + offline TTS) | ✅ | ✅ (LAN) | ✅ (LAN/Direct) | ✅ (LAN) | ❌ | ❌ | ✅ | ❌ | ❌ | ✅ (LAN direct) |
| **Local model inference** | ❌ | ✅ (MNN + llama.cpp) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Built-in terminal/Linux env** | ❌ | ✅ (Ubuntu 24 chroot) | ❌ | ❌ | ✅ (native VT100 terminal) | ✅ (browser terminal) | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (built-in terminal) |
| **UI automation (control phone)** | ❌ | ✅ (Accessibility/ADB/Root) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **MCP plugin ecosystem** | ❌ | ✅ (MCP marketplace + Skill marketplace) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (Daemon as MCP Server) |
| **Persona/character cards** | ❌ | ✅ (import/export/group chat) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Built-in browser** | ❌ | ✅ (tabs + web automation) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Session fork/clone** | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Diff viewing** | ✅ (Git Diff) | ❌ | ✅ (Agent Diff) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ✅ (Agent Diff) |
| **File upload** | ✅ | ✅ | ✅ (camera roll direct) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Remote device streaming** | ❌ | ❌ | ✅ (Android WebRTC) | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **IM message interaction** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (Feishu) | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Multi-workspace/multi-session** | ✅ (multi-session) | ✅ (multi-session) | ✅ | ✅ | ✅ | ✅ | ✅ (multi-workspace + multi-session) | ❌ | ❌ | ❌ | ❌ | ✅ (multi-session + multi-host) |
| **Task queue** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (auto-queue + merge) | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Agent-to-agent orchestration** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (Skills: handoff/loop/advisor/committee) |
| **Git Worktree isolation** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ (auto per-Agent worktree) |

## Architecture Differences

| Dimension | ClawBench | Operit | Yep Anywhere | Happy | AnyCoding | AI Agent Remote | FeiLaude | Claude Dispatch | Claude Remote | Cursor Agent | Paseo |
|-----------|-----------|--------|-------------|-------|-----------|-----------------|----------|-----------------|---------------|--------------|-------|
| **Architecture** | C/S (Go Web + SSE) | Local Android App | C/S (TypeScript Web + SSE) | P2P + relay (E2E encrypted sync) | C/S (WebSocket relay) | C/S (WebSocket bridge) | IM Bot (Feishu WebSocket) | Centralized (Anthropic server relay) | WebSocket bridge (node-pty) | Cloud async Agent | Daemon + E2E encrypted WebSocket Relay (Cloudflare DO) |
| **Where AI runs** | Server-local CLI | Phone-local (MNN/llama.cpp) + cloud API | PC-local CLI (Agent SDK) | PC-local CLI | PC-local CLI (same PTY) | PC-local CLI | PC-local CLI | PC-local CLI | PC-local CLI | Cursor cloud | PC-local CLI (Daemon subprocess) |
| **Phone's role** | Full workstation | Full Agent (local execution) | Agent monitor/interactor | Remote controller | Remote controller (native terminal) | Remote controller (browser terminal) | Message sender | Remote controller | Remote controller | Task submitter | Agent orchestrator (multi-Agent coordination) |
| **PC must be online** | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ |
| **Backend language** | Go | Kotlin | TypeScript | TypeScript | JavaScript/Java/Kotlin | JavaScript | Python | — | JavaScript | — | TypeScript |
| **Mobile client** | Browser + Android WebView | Jetpack Compose native | Browser (Mobile-first Web) | Expo (React Native) native | Android native (Termux VT100) | Browser (xterm.js) | Feishu App | Native App | Tauri 2.0 (Android) | PWA | Expo (React Native) iOS + Android + Web + Electron desktop |
| **Data storage** | SQLite local persistence | Local SQLite | CLI session history (no extra DB) | No persistence (encrypted sync) | 256KB ring buffer | None | YAML files | Cloud | None | Cloud | No extra persistence |
| **Deployment** | Single binary, extract and run | APK install (Android only) | pnpm install + Docker | npm install + App pairing | npm install + App pairing | npm install + browser | pip install + Feishu App config | App login and go | npm install + connect | Browser login | npm install + Daemon start |

## Detailed Analysis of Each Competitor

### Operit (GitHub: AAswordman/Operit, 4.4k Stars)

An open-source native Android AI Agent, positioned as "the most powerful mobile AI Agent," with deep Android system permission integration.

- ✅ Built-in Ubuntu 24 Linux environment (chroot), runs Python/Node.js directly on phone
- ✅ 10+ AI backends, including MNN + llama.cpp for on-device offline inference
- ✅ 40+ built-in tools + MCP/Skill marketplace plugin ecosystem
- ✅ Three-channel UI automation (Accessibility/ADB/Root), AI can autonomously control the phone
- ✅ Complete voice pipeline: voice wake-up + STT + TTS + continuous conversation
- ✅ Persona/character card system with inter-character group chat + @ mentions
- ✅ Built-in browser + web automation scripting
- ❌ Android only (no iOS/web version)
- ❌ No server architecture, cannot self-host or access remotely
- ❌ No SSH tunnel/port forwarding
- ❌ No push notifications
- ❌ No multi-client sync (single device only)
- ❌ LGPLv3 license (more restrictive than MIT)

### Yep Anywhere (GitHub: kzahel/yepanywhere, 380 Stars)

A self-hosted mobile-first Web UI for monitoring and interacting with Claude Code and Codex Agents. No database, no cloud accounts, no sign-ups required.

- ✅ End-to-end encrypted remote access (SRP-6a + TweetNaCl), relay operator cannot see data
- ✅ Permission approval + push notifications, respond to Agent requests from lock screen
- ✅ Session fork/clone — branch from any message point to explore alternative paths
- ✅ Tiered inbox (Needs Attention / Active / Recent / Unread), efficient multi-Agent management
- ✅ Global activity stream — see all Agent statuses at a glance
- ✅ Uses Anthropic's official claude-agent-sdk, compliant by design
- ✅ CLI/VS Code interop — sessions started in terminal work seamlessly in Web UI
- ✅ Server-side Markdown/code rendering, fast mobile performance
- ✅ Remote Android device streaming (WebRTC), control emulators/devices from phone
- ✅ Voice input (browser Speech API)
- ✅ File upload (camera roll direct)
- ❌ Only supports Claude Code + Codex (2 backends)
- ❌ No file browsing/editing, Git integration, or scheduled tasks
- ❌ PC must be online
- ❌ No TTS speech synthesis
- ❌ Requires Node.js + pnpm, not single-binary deployment

### Happy (GitHub: slopus/happy, 19.9k Stars)

The most popular open-source mobile AI programming remote controller.

- ✅ End-to-end encryption, privacy security
- ✅ Real-time voice, instant device switching
- ✅ Free and open source, self-hosted server option
- ✅ Supports Claude Code + Codex
- ❌ Remote control only, no file browsing/editing/Git
- ❌ Only 2 AI backends
- ❌ PC must be online

### AnyCoding (GitHub: gurudin/anycoding, 9 Stars)

An open-source remote controller with native Android terminal mirroring, sharing the same PTY process as the desktop.

- ✅ Native Android VT100 terminal (Termux rendering), near-desktop experience
- ✅ Same PTY process — phone and desktop share the exact same terminal session
- ✅ Multi-tab + multi-PC support, one app connects to multiple computers
- ✅ Auto-reconnect on disconnect + 256KB ring buffer replay
- ✅ Multiple network modes: cloud relay (zero config) / Direct self-hosted / LAN-only
- ✅ Session ownership protection — desktop-launched sessions can't be accidentally closed from phone
- ❌ Remote terminal only, no files/Git/scheduled tasks development environment
- ❌ PC must be online
- ❌ Client open source, but cloud relay service is closed source
- ❌ Android only for now (iOS planned)
- ❌ Very early stage (9 Stars)

### AI Agent Remote (GitHub: duran4000/ai-agent-remote)

An open-source browser-based remote control tool, bridging AI Agent CLIs via WebSocket.

- ✅ Zero install — works directly in mobile browser
- ✅ Supports 5 AI Agents (Claude/Qwen/Gemini/OpenCode/iFlow)
- ✅ Multi-project directory management
- ✅ Dual-layer authentication (Token + password)
- ✅ Lightweight pure Node.js stack, simple deployment
- ✅ Tailscale/ZeroTier cross-network access
- ❌ Remote terminal only, no files/Git/scheduled tasks
- ❌ PC must be online
- ❌ No end-to-end encryption
- ❌ No push notifications
- ❌ Very early stage (0 Stars)

### FeiLaude (GitHub: johnqxu/feilaude, 2 Stars)

An open-source Feishu Bot bridging local Claude Code, driven by IM messages.

- ✅ Zero new App — interact directly through Feishu App, no additional client needed
- ✅ Streaming status push — real-time display of Claude tool calls (reading/editing/executing)
- ✅ Smart task queue — new messages during execution auto-queue, merged on completion
- ✅ Multi-workspace + multi-session management — each workspace bound to a different project directory, with session switching/resume
- ✅ Feishu interactive cards — long text auto-segmented with code block integrity protection
- ✅ Task cancellation — cross-platform process tree cleanup
- ✅ Simple deployment — pip install + Feishu App configuration
- ❌ Only supports Claude Code (1 backend)
- ❌ No file browsing/editing, Git integration, or scheduled tasks
- ❌ PC must be online
- ❌ No end-to-end encryption (messages go through Feishu servers)
- ❌ Tied to Feishu ecosystem — unusable for non-Feishu users
- ❌ No TTS, no voice, no media preview
- ❌ Very early stage (2 Stars)

### Claude Dispatch (Anthropic Official)

A Claude Cowork family member for mobile remote control of Claude Code.

- ✅ Out-of-the-box, beautiful interface
- ✅ Deep integration with the Claude ecosystem
- ❌ Pro/Max subscription only
- ❌ Only supports Claude, must be online
- ❌ Data relayed through Anthropic servers
- ❌ PC must be awake

### Claude Remote (GitHub: RioArisk/claudecode_api_RemoteControl)

An unofficial Claude Code remote control solution that supports API users.

- ✅ Supports API users (Dispatch does not)
- ✅ Permission approval, model switching
- ✅ LAN/Tailscale/Cloudflare multiple connection options
- ❌ Only supports Claude Code
- ❌ Simple features, no files/Git/scheduled tasks
- ❌ Android only

### Paseo (GitHub: getpaseo/paseo, 5.5k Stars)

An open-source AI agent orchestration platform with Daemon + E2E encrypted WebSocket Relay architecture. Orchestrate multiple AI Agents from phone, desktop, web, and CLI. The name means "stroll" in Spanish — check on your agents while away from your desk.

- ✅ Native mobile apps: Expo (React Native) supports iOS + Android + Web + Electron desktop, unified experience across all four platforms
- ✅ E2E encrypted Relay: ECDH key exchange + AES-256-GCM, relayed via Cloudflare Durable Objects but the relay cannot read/modify traffic; zero-config NAT traversal
- ✅ Agent-to-agent Skills system: handoff (delegate), loop (iterate to acceptance), advisor (second opinion), committee (adversarial panel) — a multi-Agent orchestration paradigm
- ✅ Git Worktree first-class: each Agent automatically gets its own isolated worktree, parallel execution without conflicts
- ✅ Multi-host support: one client connects to Daemons on multiple machines, unified view of all Agents
- ✅ Voice control: local-first push-to-talk STT, hands-free interaction
- ✅ Permission approval + push notifications, approve Agent requests from any device
- ✅ Built-in terminal + MCP Server mode, can be integrated by other tools
- ✅ Active community: 5.5k stars, 86 releases, 3,000+ commits
- ❌ Only 3 AI backends (Claude Code / Codex / OpenCode), no support for CodeBuddy, Gemini, Qoder, VeCLI
- ❌ No file browser/editor — pure Agent remote control, no development environment
- ❌ No Git visualization UI (Diff viewing only, no branch graph/history)
- ❌ No scheduled tasks (Cron scheduling)
- ❌ No TTS speech synthesis
- ❌ No data persistence — no SQLite/DB, session data not retained
- ❌ PC must be online (Daemon runs on the PC)
- ❌ Not a Go single-binary deployment, requires Node.js + npm

### Cursor Background Agent (Closed-source Commercial)

Cursor's cloud async Agent, submit tasks via browser/mobile.

- ✅ No local PC required, cloud execution
- ✅ Auto-create PRs, multi-model comparison
- ✅ PWA support, works in any browser
- ❌ Closed source, depends on Cursor cloud
- ❌ Async mode — cannot watch execution in real time
- ❌ Requires GitHub repo, no local project support
- ❌ Requires Cursor paid subscription

### GitHub Copilot (Closed-source Commercial)

GitHub's official AI programming assistant, with mobile support.

- ✅ Deep GitHub ecosystem integration
- ✅ Mobile PR Review
- ❌ No complete development environment
- ❌ Closed source, depends on GitHub
- ❌ Requires paid subscription

## ClawBench Core Advantages

1. **The only full-featured mobile workstation**: All other products are "remote controllers" — they remotely control sessions on a PC. ClawBench itself is a complete development environment: files, code, Git, AI, scheduled tasks, TTS, media preview — get work done directly on your phone.

2. **12 AI backends**: Happy/Yep Anywhere has only 2, Claude Dispatch/Remote has only 1. ClawBench supports CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Cline, Copilot, and Kimi — the broadest coverage. (Note: Operit supports 10+ backends but primarily via API calls; ClawBench is the only multi-backend workstation supporting CLI Agents)

3. **No dependency on PC being online**: Happy/Dispatch/Remote/AnyCoding/AI Agent Remote/Yep Anywhere all require a PC online running the CLI. ClawBench is deployed on a server — connect from your phone anytime; just leave the server running.

4. **Scheduled task dispatch**: No competitor has this. AI proposes → confirm → Cron auto-executes. Ideal for automated ops, daily reviews, and similar scenarios. (Operit has workflow scheduled triggers, but not Cron-style AI scheduling)

5. **Complete data persistence**: Happy/Remote don't store data, AnyCoding has only a 256KB ring buffer, Yep Anywhere relies on CLI session history with no extra DB, Dispatch stores in the cloud. ClawBench uses SQLite for local persistence of all sessions, history, and tasks — no data loss on disconnect, data sovereignty stays with the user.

6. **Green single-file deployment**: One binary + static assets, zero dependencies. Happy needs Node.js + npm + App pairing, Yep Anywhere needs Node.js + pnpm + Docker, AnyCoding needs Node.js + App pairing, Claude Remote needs Node.js + Tailscale, Dispatch requires a subscription.

7. **SSH tunnel port forwarding**: Built-in SSH server; the Android App can directly access any port on the server. No other product offers this capability.

8. **TTS speech synthesis**: 5 engines + 10 summarization backends, automatic reading of AI responses. No competitor has this.

9. **Cross-platform access**: Browser + Android WebView dual client, accessible from any device. Operit is limited to Android native App, AnyCoding to Android App, AI Agent Remote to browser terminal only.

10. **Deep CLI Agent integration**: ClawBench drives AI Agents directly via CLI processes (CodeBuddy, Claude Code, etc.), preserving full Agent capabilities (tool calls, thinking chains, AutoResume). Operit primarily uses API calls; Yep Anywhere uses Agent SDK for structured rendering (Diff/approval) but is limited to Claude Code + Codex; AnyCoding/AI Agent Remote are pure terminal mirrors that lose Agent structured events.

## ClawBench Relative Disadvantages

1. **No end-to-end encryption**: A core selling point of Happy, Yep Anywhere, and Paseo. Paseo's ECDH + AES-256-GCM scheme ensures the relay cannot read/modify traffic — the highest security guarantee
2. **No real-time voice**: Happy supports this, and Operit has a complete voice pipeline (wake-up + STT + TTS + continuous conversation), Yep Anywhere supports voice input, Paseo supports push-to-talk STT
3. **No multi-client sync**: The same session cannot be controlled simultaneously from multiple devices
4. **No permission approval mechanism**: No human approval flow for AI tool calls (Happy/Dispatch/Remote all have this, Operit has tool-level permission control, Yep Anywhere has push approval, Paseo supports approval from any device)
5. **No native iOS App**: Happy and Paseo both have native iOS Apps; Paseo covers iOS + Android + Web + Electron across all four platforms
6. **No local model inference**: Operit supports MNN + llama.cpp for fully offline inference; ClawBench depends on cloud/server-side AI
7. **No UI automation capability**: Operit can let AI autonomously control the phone's UI via Accessibility/ADB/Root; ClawBench has no such capability
8. **No built-in terminal/Linux environment**: Operit has a built-in Ubuntu 24 chroot environment, Paseo has a built-in terminal; ClawBench has no terminal capability
9. **No session fork/clone**: Yep Anywhere supports forking conversations from any message point; ClawBench does not
10. **No remote device streaming**: Yep Anywhere supports streaming Android devices/emulators via WebRTC; ClawBench has no such capability
11. **No agent-to-agent orchestration**: Paseo's Skills system (handoff/loop/advisor/committee) enables multi-Agent orchestration; ClawBench only has AutoResume auto-continuation
12. **No automatic Worktree isolation**: Paseo automatically creates an isolated worktree per Agent; ClawBench's multiple sessions share the same working directory
13. **No multi-host support**: Paseo can connect one client to Daemons on multiple machines; ClawBench is single-host only
14. **No MCP Server mode**: Paseo's Daemon can serve as an MCP Server for integration by other tools; ClawBench has no such capability
