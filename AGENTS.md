# AGENTS.md

## Project Overview

ClawBench is a mobile-first AI workstation wrapping AI CLI tools (CodeBuddy, Claude Code, OpenCode, Gemini CLI, Codex, Qoder CLI, VeCLI, DeepSeek TUI, MiMo-Code, Pi) into a web-accessible platform. Go backend shells out to CLI tools and streams JSON output via SSE; Vue 3 frontend renders the streamed events in real time. Supports ACP (Agent Client Protocol) stdio transport for agents with native or bridge-adapter support, providing structured mode switching, slash commands, and permission management. Also supports SSH tunnel-based port forwarding for remote/mobile access and a scheduled task (cron) system for recurring AI execution.

## Build & Run Commands

```bash
./build.sh                # Full build (Go binary + Vue frontend)
./build.sh --windows      # Cross-compile: Windows amd64
./build.sh --linux        # Cross-compile: Linux amd64
./build.sh --darwin       # Cross-compile: macOS arm64

./dev-server.sh           # Dev mode (Vite HMR proxy to production backend's dev HTTP port)
./dev-server.sh --fg      #   foreground
./dev-server.sh --stop    #   stop
./dev-server.sh --restart #   restart

./clawbench               # Run directly (foreground, default port 20000)
./clawbench --port 8080   #   specify port
./clawbench --data-dir /data/.clawbench  #   custom data directory

go build -o clawbench ./cmd/server   # Go binary only
go test ./...                        # All Go tests
go test ./internal/ai/...            # Package-specific
npm test                             # Vitest (all frontend tests)

# Coverage gate (CI 合入门槛)
./scripts/check-go-coverage.sh              # Go: run tests + check per-package coverage
./scripts/check-go-coverage.sh --skip-test   # Go: reuse existing coverage.out
./scripts/check-go-coverage.sh --update      # Go: auto-update baseline after coverage improvement
./scripts/check-frontend-coverage.sh              # Frontend: run tests + check per-dir coverage
./scripts/check-frontend-coverage.sh --skip-test   # Frontend: reuse existing coverage data
./scripts/check-frontend-coverage.sh --update      # Frontend: auto-update baseline after improvement
./scripts/check-android-coverage.sh              # Android: run tests + check per-class coverage
./scripts/check-android-coverage.sh --skip-test   # Android: reuse existing JaCoCo report

# Android APK (requires JDK 17)
cd android && JAVA_HOME=/usr/lib/jvm/jdk-17.0.12 ./gradlew assembleDebug    # Debug APK
cd android && JAVA_HOME=/usr/lib/jvm/jdk-17.0.12 ./gradlew assembleRelease  # Release APK
```

## Architecture

### Backend (Go)

**Entry point:** `cmd/server/main.go` — config → port → LoadAgents → SyncDiscoverAgents → SyncDiscoverModels → MergeDiscoveredData → AsyncRefreshModelCache → scheduler init.

**Packages:**
- `internal/handler/` — HTTP/SSE endpoints. All `/api/` routes use `middleware.Auth` (localhost bypass for CLI).
- `internal/service/` — Business logic: chat persistence, auto-summary, scheduler, SQLite, versioned schema migration, agent store (DB-backed), API key encryption (AES-256-GCM).
- `internal/ai/` — AI backend abstraction: `AIBackend` interface → `CLIBackend` (CLI args + LineParser) → `AutoResumeBackend` (ExitPlanMode → cancel → resume) → `ACPBackend` (JSON-RPC over stdio, connection pool). Factory: `factory.go`.
- `internal/model/` — Data models, `BackendRegistry` (backend specs + model discovery), `ProviderRegistry` (28 LLM providers). Known models from runtime `provider_models.json`.
- `internal/cli/` — AI agent self-service: `task`, `rag`, `migrate`.
- `internal/middleware/` — Auth, request logging, panic recovery, request ID.
- `internal/platform/` — Cross-platform path resolution, shell detection, Windows CLI utilities.
- `internal/speech/` — TTS: MiniMax, Edge TTS (native Go), Piper/Kokoro/MOSS-Nano.
- `internal/summarize/` — Text summarization for auto-summary, TTS, task summaries.
- `internal/ssh/` — SSH tunnel server. Publishes `tunnel_status` via EventBus.
- `internal/proxy/` — HTTP reverse proxy + port forwarding. Rewrites Host header for virtual-host backends.
- `internal/symbol/` — Code symbol extraction via tree-sitter (`gotreesitter`, pure Go, no CGO). 17 symbol kinds, 100+ languages.
- `internal/rag/` — RAG: DuckDB vector store, Ollama BGE-M3 embeddings.
- `internal/terminal/` — Web terminal: PTY sessions, ring buffer replay, multi-tab support, key/symbol configuration.
- `internal/push/` — Push notifications via JPush (session completed/cancelled + ACP permission_pending).
- `internal/ws/` — WebSocket event channel. JPush fallback on disconnect, buffered replay on reconnect.

### Frontend (Vue 3 + TypeScript)

**Source root:** `web/src/` — No Vue Router, drawer-based single-page layout. Single `reactive()` store in `stores/app.ts`.

**Composables:** `useChatSession`, `useChatStream` (SSE + reconnect + polling), `useChatRender` (block parsing + coalescing), `useAutoSpeech`, `useQuickSend`, `useSessionIdentity`, `useSessionManager`, `useSetup`, `useReconnect`, `useFileRefresh`, `useSystemEvents`, `useGlobalEvents`, `usePortForward`, `useBackHandler`, `useEdgeSwipeBack`, `useTerminalSession`, `useTerminalTabs` (multi-tab management), `useTerminalKeys` (virtual key processing), `useKeyConfig` (key/symbol persistence), `useTerminalGestures`, `useSwipeDelete`, `useSwipeSession`, `useCodeSymbols`, `useStickyScroll`, `useLocalhostAnnotation`, `useWorktreeAnnotation`, `useFileUpload`, `useAgents`, `useFilePathAnnotation`, `useFileNavStack` (file overlay navigation stack), `useTaskTab`.

**Components:** `ChatPanel`, `FileManager`/`FileViewer`/`FileOverlay` (browse tab + file preview overlay, merged from browse/viewer tabs), `TocDrawer`, `TaskTab`, `TaskExecDetail`, `TerminalPanel` (multi-tab, key/symbol config drawer), `KeyConfigDrawer`, `KeyConfigTab`, `TerminalTabMenu`, `GitGraph`, `GitManageContent`, `SessionSettingModal`, `SetupWizard`, `ContentBlocks`, `SummaryToggle`, `SessionDrawer`, `AcpSessionDrawer`, `PlanPanel`, `BottomSheet`, `PopupMenu`, `Lightbox`, `SwipeToDeleteRow`, `PasswordChangeDialog`.

**Vite:** `hljsThemeWrapper` plugin for light/dark coexistence. Root `web/`, output `public/`. Alias `@` → `web/src/`.

## Key Patterns

- **SSE reconnection:** Chat: 3 attempts → HTTP polling. System events: 5 attempts → HTTP polling. Multi-client: `sse_busy` → second client falls back to HTTP polling with incremental block updates.
- **Block coalescing:** Text/thinking merge into last same-type block; `tool_use` is boundary. `@chatsearch`/`@task` detected by `extractAtCommand()` and rendered as purple badges.
- **AutoResumeBackend:** ExitPlanMode → cancel → resume "继续". Emits `resume_split` for DB finalization.
- **ACP backend:** `ACPBackend` wraps ACP stdio agents with connection pooling (`ACPConnectionPool`, lazy init, 5-min idle sweep). Falls back to CLI via `sync.Once` if ACP not supported. State (mode/config/thinking/commands) cached and re-emitted on reconnect. Bridge adapters provide ACP for agents without native support.
- **Agent system:** DB-backed (`agents` table). Models auto-discovered at runtime via `BackendRegistry` strategies. Shared rules template (`commonRulesTemplate`) embedded in Go binary. `@chatsearch`/`@task` template-injected on demand.
- **Setup wizard:** 5-step flow when no agents + embedded Pi detected. Custom URL mode for any OpenAI/Anthropic-compatible endpoint. API keys AES-256-GCM encrypted with HKDF-SHA256. `previousEncryptionKey` fallback for crash recovery.
- **External session ID:** All backends write CLI session ID to `external_session_id` at creation. Continued sessions inherit from source.
- **Cancel reason:** `"user"` (explicit) vs `"disconnect"` (SSE gone). `ForceCancelSession` kills zombie CLI processes.
- **Green portable deployment:** All runtime data under `.clawbench/`. Multi-instance on different ports with auto-scoped cookies (`ScopedCookieName()`).
- **Zero-config startup:** `config/config.yaml` optional. Auto-password persisted to `.clawbench/auto-password`. Filesystem root paths via `platform.ListRootPaths()`.
- **Versioned schema migration:** Auto-incrementing version numbers in `schema_migrations` table. Dirty flag prevents silent corruption. New migrations: append function + next version number.
- **Coverage gate (CI 合入门槛):** Two-tier. Tier 1: per-package coverage `>= baseline% - 1.5%`. Tier 2: changed lines coverage `>= 80%`. CI enforces on every PR/push to main.
- **Bugfix workflow (GitHub Issues):** Report bugs as GitHub Issues. Scheduled auto-fix task (Task #27): classify → fix in isolated worktree → write tests → verify → PR + CI → auto-merge → close issue. Labels track state: `bugfix:in-progress`, `bugfix:awaiting-review`, `bugfix:needs-design`, `bugfix:failed`, `bugfix:needs-verification`.
- **Bugfix regression tests (mandatory):** Every bug fix MUST include a targeted unit test reproducing the original failure. Go: `*_test.go` next to patched code. Frontend: `.test.ts` next to patched composable/component. If genuinely untestable, explain why and use integration test or `bugfix:needs-verification` label.
- **Docker deployment:** `Dockerfile` + `docker-compose.yml` + `scripts/docker-build.sh`. Data via Docker volume at `/data/.clawbench/`.
- **@ command injection:** `processAtCommand()` detects `@chatsearch`/`@task` → template injection. Frontend autocomplete in `ChatInputBar.vue`.
- **Inline thinking streaming:** Thinking blocks stream inline during active session, auto-collapse to clickable chip on completion.
- **Android integration:** HTML login + `AndroidNative` JS bridge. `BackgroundService` for SSH + WS. Push-aware: JPush when available, keep WS alive otherwise.
- **SPA hot project switch:** In-place state reset + Vue `:key` rebuild, no `window.location.reload()`.
- **Worktree annotation:** `useWorktreeAnnotation` annotates worktree paths in chat messages. Runs before file path annotation to prevent partial matches.
- **Provider models auto-generation:** `scripts/fetch-provider-models.sh` fetches from models.dev API (curl+jq, no Python), writes `provider_models.json` to `<BinDir>/.clawbench/`. Read at runtime by `LoadProviderModelsFromFile()`. `build.sh` and CI generate automatically.
- **Terminal multi-tab:** `useTerminalTabs` manages tab lifecycle (create/close/switch). `useTerminalKeys` processes virtual key input with modifier lock. `useKeyConfig` persists custom key/symbol layouts to DB via `/api/terminal/key-config`.
- **Chat summary modes:** `simple` mode extracts last answer text from blocks (no AI call); `ai` mode uses `AsyncSummarize`; empty string disables summarization. Mode set via `SetChatSummaryMode()`.
- **Permission pending push:** ACP `permission_pending` events trigger JPush notifications with tool name, allowing mobile users to approve from notification.
- **File overlay navigation:** `useFileNavStack` manages a stack-based file overlay on the browse tab. Clicking a file pushes it onto the stack (overlay on top of file list); back button pops; close clears the stack. Replaces the separate viewer tab with a unified browse+overlay experience.

## Development Rules

- **Mandatory unit tests for features and bug fixes:** Every new feature and bug fix MUST include targeted unit tests. Go: `*_test.go` next to the code; Frontend: `.test.ts` next to the composable/component. Tests must verify the specific behavior/fix, not just generic happy paths.
- **Local CI validation before push/PR:** Before pushing code or creating a PR to remote, MUST run and pass the coverage gate locally:
  ```bash
  ./scripts/check-go-coverage.sh       # Go coverage gate
  ./scripts/check-frontend-coverage.sh # Frontend coverage gate
  ```
  Ensure all tests pass and coverage meets CI requirements (Tier 1: per-package >= baseline-1.5%; Tier 2: changed lines >= 80%).

## Configuration

`config/config.yaml` is entirely optional. See `config/config.example.yaml`.

| Section | Key options |
|---------|------------|
| Server | `port` (20000), `host`, `log_level` ("info"), `password` (auto-8-char-hex, SHA-256 salted hash storage), `default_agent`, `dev_port` (0=auto, Port+2 when TLS) |
| Upload | `upload.max_size_mb`, `upload.max_files` |
| Chat UI | `chat.initial_messages`, `chat.page_size`, `chat.collapsed_height`, `chat.system_prompt_interval` (10) |
| Session | `session.max_count` |
| Recent Projects | `recent_projects.max_count` (10) |
| TLS | `tls.enabled`, `tls.cert_file`, `tls.key_file` |
| TTS | `tts.engine`, `tts.speed`, `tts.voice`, `tts.max_cache_files` (100) |
| Summarize | `summarize.backend` ("simple"), `summarize.model`, `summarize.api`, `summarize.chat_summary` (true) |
| Port Forward | `port_forward.enabled` (true), `port_forward.port` (0=auto), `port_forward.host_key` |
| RAG | `rag.enabled`, `rag.ollama_base_url`, `rag.ollama_model` (bge-m3), `rag.chunk_size` (512), `rag.retention_days` (90) |
| Terminal | `terminal.enabled` (true), `terminal.idle_timeout` (10m), `terminal.max_sessions` (10) |
| Push | `push.jpush.enabled`, `push.jpush.app_key`, `push.jpush.master_secret` |
