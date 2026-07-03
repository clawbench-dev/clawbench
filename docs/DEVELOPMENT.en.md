[中文](DEVELOPMENT.md) | [English](DEVELOPMENT.en.md)

# Build & Development Guide

## Quick Start

### Option 1: Use Release Package (Recommended)

Download the latest ZIP package from [GitHub Releases](https://github.com/xulongzhe/clawbench/releases), extract and deploy. All configuration items have default values, no config file needed to start.

```bash
# 1. Download and extract
wget https://github.com/xulongzhe/clawbench/releases/latest/download/clawbench-linux-amd64.zip
unzip clawbench-linux-amd64.zip

# 2. Start the server (no config file needed)
cd clawbench
./clawbench
```

> On first startup, a random 8-character hex password is auto-generated and saved to `.clawbench/auto-password`; it's printed to the console in a bordered box for visibility. To customize configuration, copy `config/config.example.yaml` to `config/config.yaml` and modify as needed.

Release package contents (Linux):

| File | Description |
|------|-------------|
| `clawbench-linux-amd64` | Backend binary |
| `public/` | Frontend static assets (pre-built) |
| `config/config.example.yaml` | Config template (optional) |
| `dev-server.sh` | Dev debug startup script |

Release package contents (Windows):

| File | Description |
|------|-------------|
| `clawbench-windows-amd64.exe` | Backend binary |
| `public/` | Frontend static assets (pre-built) |
| `config/config.example.yaml` | Config template (optional) |

### Option 2: Build from Source

**Linux/macOS:**

```bash
# 1. Clone the project
git clone https://github.com/xulongzhe/clawbench.git
cd clawbench

# 2. One-click build and start (no config file needed, all items have defaults)
./build.sh && ./clawbench
```

**Dev debug mode:**

```bash
# Start in background (Go dev backend + Vite HMR)
./dev-server.sh

# Start in foreground, view live logs
./dev-server.sh --fg

# Stop background processes
./dev-server.sh --stop

# Restart
./dev-server.sh --restart
```

**Windows:**

```powershell
# 1. Clone the project
git clone https://github.com/xulongzhe/clawbench.git
cd clawbench

# 2. One-click build and start (no config file needed)
./build.sh; ./clawbench
```

**Cross-compilation** (build Windows binary on Linux):

```bash
./build.sh --windows
```

### System Requirements

| Platform | Supported |
|----------|-----------|
| Linux x86_64 / ARM64 | ✅ |
| Windows x86_64 | ✅ |

| Dependency | Description |
|------------|-------------|
| **CodeBuddy CLI** or **Claude Code CLI** | AI backend (install and authenticate in advance; OpenCode / Gemini / Codex / Qoder / VeCLI / MiMo optional) |

### Configuration File

`config/config.yaml` is entirely optional — all configuration items have default values. To customize, copy `config/config.example.yaml` to `config/config.yaml` and modify.

**Defaults:**

| Config Item | Default | Description |
|-------------|---------|-------------|
| `port` | 20000 | Server port |
| `password` | Auto-generated 8-char hex | Generated on first run, saved to `.clawbench/auto-password`, reused on restart |
| `log_dir` | `<BinDir>/.clawbench/logs` | Under the binary's directory |
| `log_max_days` | 7 | Log retention days |
| `upload.max_size_mb` | 100 | Upload size limit in MB |
| `upload.max_files` | 20 | Max files per upload |
| `chat.initial_messages` | 20 | Initial message count to load |
| `chat.page_size` | 20 | Messages per page for lazy loading |
| `chat.collapsed_height` | 150 | Collapsed height for history messages in px |
| `session.max_count` | 10 | Max sessions per project |
| `recent_projects.max_count` | 10 | Max recent projects shown in header dropdown |
| `terminal.enabled` | true | Web terminal enabled by default |
| `terminal.idle_timeout` | 10m | Terminal idle timeout |
| `terminal.max_sessions` | 10 | Max terminal sessions per project |
| `port_forward.enabled` | true | Port forwarding enabled by default |
| `port_forward.port` | 0 (auto) | SSH port (0 = main port + 1) |
| `port_forward.host_key` | (auto) | Host key file path |
| `port_forward.allowed_ports` | (all) | Allowed port range for forwarding |
| `tts.engine` | edge | Edge TTS, free and unlimited |
| `tts.speed` | 1.0 | Normal speech rate |
| `tts.max_cache_files` | 100 | Max cached TTS audio files; oldest auto-deleted when exceeded (-1=unlimited) |
| `tts.inline_code_max_len` | 100 | Max characters (runes) to keep for inline code; exceeded content is removed |
| `tts.max_summarize_runes` | 10000 | Max input characters for summarization; tail is truncated if exceeded |
| `summarize.backend` | simple | Unified summarization backend (shared by TTS + scheduled tasks), zero latency |
| `summarize.model` | (empty) | Model for summarization, empty = backend default |
| `summarize.api` | (empty) | API sub-config (used when backend is "api"), includes base_url/key/format |
| `summarize.chat_summary` | true | Auto-generate summary of last assistant message on session complete (`*bool`, nil=true) |

**Auto-password mechanism**: When `password` is not configured, the system auto-generates a random 8-character hex password, saved to `.clawbench/auto-password` (permissions 0600). On restart, the saved password is reused and not regenerated. Once `password` is configured, the file is auto-deleted. On startup, the password is printed to the console in a bordered box for easy visibility.

**Example configuration:**

```yaml
# All values below are defaults; only configure when you need to override
# port: 20000
# password: "your_password"     # Auto-generates 8-char hex if not configured
# default_agent: "assistant"   # Default agent; uses first agent if empty
```

### Startup Commands

#### Production

| Command | Description |
|---------|-------------|
| `./clawbench` | Run directly (foreground, default port 20000) |
| `./clawbench --port 8080` | Specify port |
| `./clawbench --data-dir /data/.clawbench` | Custom data directory |

#### Dev Debug Mode

| Command | Description |
|---------|-------------|
| `./dev-server.sh` | Start in background (dev backend + Vite, ports 20002/20001) |
| `./dev-server.sh --fg` | Start in foreground |
| `./dev-server.sh --stop` | Stop processes |
| `./dev-server.sh --restart` | Restart |

> **Note**: Dev debug mode uses separate ports and database from production, so both can run simultaneously without interference.

---

## Advanced Configuration

For full configuration reference, see `config/config.example.yaml`. All items are optional; below are examples that override defaults:

```yaml
# port: 20000                        # Production server port (default 20000)
# password: "your_password"          # Access password (auto-generates UUID and saves if not configured)

# Default agent (optional)
default_agent: "assistant"      # Default agent ID; uses first agent if empty
                                 # Available agents: assistant (all-round assistant), coder (coding expert),
                                 # gemini (Gemini ACP), handyman (handyman), codebuddy2 (Gemini), gpt54 (GPT)

# Upload limits (default max_size_mb: 100, max_files: 20)
upload:
  max_size_mb: 10
  max_files: 20

# Log configuration (default .clawbench/logs, 7-day retention)
log_dir: ".clawbench/logs"
log_max_days: 7

# TLS (HTTPS) configuration (optional)
tls:
  enabled: false                # Enable HTTPS
  cert_file: "/path/to/fullchain.pem"   # Certificate file
  key_file: "/path/to/privkey.pem"      # Private key file

# Port forwarding + SSH tunnel configuration (enabled by default, merged into port_forward)
port_forward:
  enabled: true                    # Enable SSH tunnel server (default: true)
  port: 0                          # SSH port (0 = auto = main port + 1)
  host_key: ""                     # Host key file path (empty = auto-generate)
  allowed_ports: ""                # Allowed port range for forwarding (empty = allow all)

# Chat UI configuration (default initial_messages: 20, page_size: 20, collapsed_height: 150)
chat:
  initial_messages: 20
  page_size: 20
  collapsed_height: 150
```

### AI Backend Configuration

ClawBench interacts with AI programming tools by calling local CLIs, no extra API key configuration needed.

**CodeBuddy backend**: Install CodeBuddy CLI and complete login authentication, ensure the `codebuddy` command is available in PATH. Model auto-discovery is implemented by parsing `product.cloudhosted.json` in the installation directory, supporting 21+ models (GLM, DeepSeek, Kimi, MiniMax, Hunyuan, etc.).

**Claude Code backend**: Install Claude Code CLI and complete authentication, ensure the `claude` command is available in PATH.

**OpenCode backend**: Install OpenCode CLI and complete authentication, ensure the `opencode` command is available in PATH.

**Gemini backend**: ACP mode only (via `gemini acp` stdio subprocess). No separate CLI installation needed.

**Codex backend**: Install OpenAI Codex CLI and complete authentication, ensure the `codex` command is available in PATH. Model auto-discovery is implemented via binary strings/state DB scanning.

**Qoder backend**: Install Qoder CLI (Alibaba coding agent) and complete authentication, ensure the `qoder` command is available in PATH. Qoder supports automatic model routing without specifying a specific model. Model auto-discovery is implemented by parsing `dynamic-texts.json`.

**VeCLI backend**: Install VeCLI (Volcengine Doubao) and complete authentication, ensure the `vecli` command is available in PATH. VeCLI outputs plain text (not JSON Lines), does not support session resume, and metadata is extracted from a `--session-summary` file after the process exits. Model auto-discovery is implemented by parsing `MODEL_REGISTRY`.

**CodeWhale backend**: Install CodeWhale (requires v0.8.33+) and complete authentication, ensure the `codewhale` command is available in PATH. Uses `codewhale exec --auto --output-format stream-json` mode with native `--system-prompt`, `--model`, and `--resume` flags.

**MiMo-Code backend**: Install MiMo-Code CLI and complete authentication, ensure the `mimo` command is available in PATH. MiMo-Code is a fork of OpenCode, reusing OpenCode's JSON stream format and stream parser, supporting CLI + ACP dual mode. Uses `mimo run --format json` mode with `--session`, `--model`, and `--variant` (thinking effort) flags. ACP mode enabled via `mimo acp` command.

**Pi backend**: Install Pi CLI and complete authentication, ensure the `pi` command is available in PATH. Pi is a minimalist coding agent that outputs NDJSON event stream via `--mode json`, supports session resume (`--session`/`--continue`) and model selection (`--model`).

All ten backends can be switched in real time on the ClawBench Web UI, with isolated session data.

### TTS Speech Synthesis Configuration

ClawBench supports TTS speech synthesis, automatically summarizing and reading aloud AI replies. Supports 5 TTS engines and 12 summarization backends.

| TTS Engine | Description | Network Requirement |
|------------|-------------|---------------------|
| `edge` | Microsoft Edge TTS, free and unlimited (default), native Go implementation (no Python/CLI dependency) | Requires network |
| `minimax` | Cloud synthesis, best audio quality | Requires mmx CLI + API quota |
| `piper` | Local offline, extremely fast | No network needed |
| `kokoro` | Local offline, high-quality Chinese | No network needed |
| `moss-nano` | Local offline, multilingual, 48kHz voice cloning | Model download on first use |

For detailed instructions on installation, deployment, configuration examples, and available voices for each engine, please refer to **[TTS Speech Synthesis Deployment Guide](TTS.md)**.

---

## Deployment

### HTTPS Configuration (Public Deployment)

Enabling HTTPS is recommended for production environments:

1. **Obtain certificate**: Use Let's Encrypt or another CA to issue a certificate
2. **Configure TLS**: Enable in `config/config.yaml`
   ```yaml
   tls:
     enabled: true
     cert_file: "/etc/letsencrypt/live/your-domain.com/fullchain.pem"
     key_file: "/etc/letsencrypt/live/your-domain.com/privkey.pem"
   ```
3. **Restart server**: Restart the `./clawbench` process

### Multi-Instance Deployment

ClawBench supports running multiple instances on the same server (different ports), with fully isolated data:

```bash
# Instance 1: default port 20000
./clawbench

# Instance 2: custom port
./clawbench --port 20300
```

Browsers don't distinguish cookies by port, so cookies are automatically prefixed by port (e.g. `cb20300_`) to prevent collisions between instances on the same domain. The default port 20000 uses unprefixed cookie names for backward compatibility.

### Docker Deployment

The project provides Docker deployment support for containerized environments:

```bash
# One-step build and start (using scripts/docker-build.sh)
./scripts/docker-build.sh

# Stop container
./scripts/docker-build.sh --stop

# Clean up container and image
./scripts/docker-build.sh --clean
```

Or build manually:

```bash
docker compose up -d
```

Docker configuration details:
- **Dockerfile**: Based on Ubuntu 24.04, dynamic linking (glibc 2.39+), no Python runtime dependency needed (Edge TTS is native Go)
- **docker-compose.yml**: Default port 20300, data persisted to Docker volume (`/data/.clawbench/`)
- **Build script**: `scripts/docker-build.sh` builds image and starts container

### Linux Dynamic Linking

The Linux binary in GitHub Releases uses dynamic linking (CGO_ENABLED=1) and requires glibc 2.39+ (Ubuntu 24.04+). Static linking (`-extldflags '-static'`) was removed because DuckDB's CGO code is incompatible with static linking — the binary builds successfully but crashes with SIGSEGV at runtime when DuckDB runs.

### Data Storage

| Data | Path | Description |
|------|------|-------------|
| Database | `Binary directory/.clawbench/ClawBench.db` | SQLite, sessions/history/projects/scheduled tasks |
| Logs | `Binary directory/.clawbench/logs/` | Daily rotation, auto-cleanup |
| Auto-password | `Binary directory/.clawbench/auto-password` | Auto-generated when password is not configured, reused on restart |
| Uploaded files | `Project directory/.clawbench/uploads/` | User-uploaded files, belonging to specific projects |

> All runtime data is stored under `.clawbench/` next to the binary, enabling green portable deployment — delete the program directory to completely uninstall. When the project directory is the same as the binary directory, uploaded files are also in the same `.clawbench/` directory.

### Dev Debug Mode

Use `./dev-server.sh` to start an independent development environment:

- Backend: `http://localhost:20002`
- Frontend (Vite HMR): `http://localhost:20001`
- Database: Uses `ClawBench-dev.db`, isolated from production data
- RAG vector store: Uses `rag-dev.duckdb`, isolated from production vector data

```bash
./dev-server.sh              # Start in background
./dev-server.sh --fg         # Start in foreground
./dev-server.sh --stop       # Stop
./dev-server.sh --restart    # Restart
```

Development environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `VITE_BACKEND_PROTO` | `https` | Backend protocol for Vite dev proxy (set to `http` in dev to skip HTTPS) |

---

## Architecture Design

### Agent Architecture

ClawBench is more than just a "chat shell" — it is a complete agent runtime platform:

- **Agent Database Storage**: Each agent is stored in the database with dedicated system prompt, model, backend, and thinking effort levels — created via welcome screen install or auto-discovery
- **Auto-Discovery**: On first startup, if no agents exist in the database, the system auto-scans for installed AI CLIs (claude, codebuddy, opencode, codex, qodercli, vecli, deepseek, mimo, pi) and creates agent records in the database for each detected backend. One-time only
- **Shared Rules**: Rules template embedded in Go binary (`commonRulesTemplate` in `agent.go`) defines common behaviors and mandatory rules for all agents (media handling), avoiding duplicate configuration. `@chatsearch`/`@task` commands are injected on demand via `processAtCommand()`, replacing old `SCHEDULED_BEGIN/END` markers and static RAG section
- **Template Placeholder**: `{{AVAILABLE_AGENTS}}` is auto-replaced with the available agent list, facilitating inter-agent dispatching
- **Multi-Agent Dispatching**: Different tasks match different agents; the all-round assistant handles conversations while specialized agents execute scheduled tasks
- **Transparent Tool Calls**: AI tool calls (file read/write, Bash commands, code editing) are visualized in real time
- **Cron Scheduled Execution**: AI creates scheduled tasks via `clawbench task` CLI subcommands; after confirmation, Cron scheduler executes them automatically. Task cards are embedded in chat messages. `list` and `get` subcommands allow inspecting existing tasks; `--prompt` supports `@path` syntax to read prompt text from a file
- **Cron Governance**: During scheduled execution, the `@task` instructions are never injected (only triggered by explicit user input), preventing AI from recursively creating tasks; CLI layer provides dual-layer protection via `CLAWBENCH_SCHEDULED=1` env var
- **Multi-Backend Switching**: The same platform simultaneously supports CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, and Pi backends (CLI mode), plus Gemini/Kimi (ACP mode), with isolated session data

### Project Structure

```
clawbench/
├── cmd/server/main.go           # Application entry point
├── internal/
│   ├── handler/                 # HTTP handlers
│   │   ├── handler.go           # Route registration
│   │   ├── auth.go              # Authentication
│   │   ├── chat.go              # AI chat (SSE streaming + @ command injection + XML ask-question parsing)
│   │   ├── at_command.go        # @ command detection & template injection (@chatsearch/@task)
│   │   ├── session_resume.go    # Restore soft-deleted session (POST /api/ai/session/resume)
│   │   ├── chat_quick_send.go   # Quick send CRUD
│   │   ├── agent.go             # Agent management
│   │   ├── scheduler.go         # Scheduled tasks (CRUD + execution list + continue conversation GET/POST /api/tasks/{id}/executions/{execId}/continue)
│   │   ├── rag_api.go           # RAG search API
│   │   ├── file.go              # File reading
│   │   ├── file_ops.go          # File operations
│   │   ├── file_thumb.go        # Image thumbnail generation (square canvas + dominant-color padding)
│   │   ├── file_archive.go      # File archive download (zip, symlink traversal protection)
│   │   ├── file_watch.go        # File change SSE notifications
│   │   ├── settings.go          # Settings (password change + SHA-256 verification + encryption key rotation)
│   │   ├── setup.go             # Setup wizard (/api/setup/status, providers, models, verify, complete)
│   │   ├── upload.go            # File upload
│   │   ├── git.go               # Git operations
│   │   ├── project.go           # Project management
│   │   ├── ssh_info.go          # SSH tunnel info API
│   │   ├── terminal.go          # Terminal + quick commands CRUD + multi-session
│   │   └── static.go            # Static files
│   ├── middleware/              # Middleware (auth/log/recovery/request ID)
│   ├── platform/                # Platform adaptation (cross-platform paths + Windows CLI utilities)
│   │   ├── path.go              # ListRootPaths, IsPathUnderAnyRoot, ManglePath, ExpandTilde
│   │   ├── path_unix.go         # Unix: root = "/"
│   │   ├── path_windows.go      # Windows: enumerate available drive roots
│   │   ├── shell.go             # Shell detection
│   │   ├── npm.go               # Windows .cmd wrapper resolution (ResolveCLIPath, extract JS entry path)
│   │   ├── strings.go           # Cross-platform binary string extraction (ExtractStrings, replaces POSIX strings)
│   │   └── path_test.go / shell_test.go / npm_test.go / strings_test.go
│   ├── service/                 # Business logic
│   │   ├── database.go          # SQLite initialization
│   │   ├── chat.go              # Chat history management
│   │   ├── continue_conversation.go # Continue conversation (from task execution → new session)
│   │   ├── agent_store.go       # Agent store (DB-backed CRUD + agent_api_keys table)
│   │   ├── agent_migration.go   # Agent migration (YAML → DB one-time migration, idempotent)
│   │   ├── crypto.go            # API key encryption (AES-256-GCM + HKDF-SHA256, key rotation on password change + previousEncryptionKey crash recovery)
│   │   ├── summary.go           # Chat auto-summary (AsyncSummarize + summaries table)
│   │   ├── scheduler.go         # Scheduled task scheduling
│   │   ├── uuid.go              # UUID utility
│   │   └── logger.go            # File logger (daily rotation)
│   ├── model/                   # Data models
│   │   ├── config.go / defaults.go / chat.go / file.go / agent.go / scheduler.go / path.go / ssh.go / discovery.go / provider_registry.go
│   │   └── errors.go
│   ├── ssh/                     # SSH tunnel server
│   │   ├── server.go            # SSH server (direct-tcpip port forwarding)
│   │   └── server_test.go       # Tests
│   ├── proxy/                   # HTTP reverse proxy + port forwarding logic
│   │   ├── reverse_proxy.go     # HTTP reverse proxy (solves SSH tunnel Host header mismatch)
│   │   └── reverse_proxy_test.go # Tests
│   ├── cli/                     # CLI subcommands (AI agent self-service)
│   │   ├── task.go              # Scheduled task subcommands (create/update/delete/pause/resume/trigger/list/get/list-agents; --prompt supports @path syntax)
│   │   ├── migrate.go           # One-time DB migration (task_executions content → chat_history)
│   │   ├── rag.go               # RAG search subcommands (search/message/session)
│   │   ├── help.go              # --help self-documentation infrastructure
│   │   └── helpers.go           # Shared code (loadConfig/apiURL/httpDo/TLS/cookie)
│   ├── ai/                      # AI backend abstraction
│       ├── interface.go         # AIBackend interface
│       ├── factory.go           # Backend factory
│       ├── cli_backend.go       # Shared CLI backend abstraction
│       ├── common_stream.go     # Shared stream args/tool normalization/system prompt
│       ├── stream_parser.go     # Shared stream parsing utilities
│       ├── claude.go / claude_stream.go
│       ├── codebuddy.go / codebuddy_stream.go
│       ├── opencode.go / opencode_stream.go
│       ├── stream_json_parser.go # Gemini/Kimi shared stream parser
│       ├── codex.go / codex_stream.go
│       ├── qoder.go / qoder_stream.go
│       ├── vecli.go / vecli_stream.go
│       ├── deepseek.go / deepseek_stream.go
│       ├── mimo.go              # MiMo-Code backend (reuses OpenCode stream parser)
│       └── pi.go / pi_stream.go
│   └── speech/                  # TTS speech synthesis
│       ├── common_tts.go        # CLISpeechProvider shared base
│       ├── edge_tts.go          # Edge TTS (native Go gorilla/websocket implementation, DRM token, no external dependency)
│       ├── minimax.go / piper.go / kokoro.go / moss_tts_nano.go  # Other TTS engine implementations
│   └── summarize/               # Text summarization (TTS + task execution summaries)
│       ├── summarizer.go        # Summarizer interface + factory
│       ├── simple.go            # Plain text cleanup
│       ├── ai_backend_summarizer.go # AIBackendSummarizer (CLI backend summarization)
│       ├── mmx.go               # MMXSummarizer (mmx-cli text chat)
│       ├── openai.go            # OpenAI Chat Completions API summarization
│       ├── anthropic.go         # Anthropic Messages API summarization
│       ├── strip_markdown.go    # Markdown stripping
│       ├── task.go              # Task execution summary generation
├── web/src/components/common/  # Common components
│   ├── SummaryToggle.vue        # Summary toggle (button/tab modes)
├── config/                      # Configuration files (optional, config.yaml not required)
│   └── config.example.yaml      # Config template
├── web/                         # Vue 3 frontend source
│   └── src/
│       ├── components/          # Vue components
│       ├── composables/         # Composable functions (useQuickSend, useQuickCommands, useChatStream, etc.)
│       ├── stores/              # State management
│       └── utils/               # Utility functions
├── build.sh                     # Build script
├── dev-server.sh                # Dev debug startup script
├── Dockerfile                   # Docker image definition (Ubuntu 24.04 base)
├── docker-compose.yml           # Docker Compose configuration
├── scripts/
│   ├── docker-build.sh          # Docker one-step build script
│   └── fetch-provider-models.sh # Provider model data generation script (models.dev API → provider_models.json, curl+jq)
└── vite.config.ts               # Vite configuration
```

---

## Tech Stack

| Layer | Technology |
|-------|------------|
| Backend | Go 1.25+ (net/http + SQLite) |
| Frontend | Vue 3 + Vite + TypeScript |
| Syntax Highlighting | highlight.js |
| Markdown | marked.js |
| Chart Rendering | Mermaid.js |
| Math Formulas | KaTeX |
| HTML Sanitization | DOMPurify |
| AI Backend | CodeBuddy CLI / Claude Code CLI / OpenCode CLI / Codex CLI / Qoder CLI / MiMo-Code CLI / VeCLI (streaming output → SSE push) + Gemini/Kimi (ACP stdio) |
| TTS Summarization | OpenAI/Anthropic compatible API (local or cloud, e.g. Ollama with `format: "openai"`) |
| SSH Tunnel | golang.org/x/crypto/ssh (embedded SSH server, direct-tcpip port forwarding) |
| Scheduled Scheduling | robfig/cron |
