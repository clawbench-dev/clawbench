# AGENTS.md

## Project Overview

ClawBench is a mobile-first AI workstation wrapping AI CLI tools (CodeBuddy, Claude Code, OpenCode, Codex, Qoder CLI, VeCLI, CodeWhale, MiMo-Code, Pi, Cline, Copilot, Kimi) into a web-accessible platform. Go backend shells out to CLI tools and streams JSON output via WebSocket; Vue 3 frontend renders the streamed events in real time. Supports ACP (Agent Client Protocol) stdio transport for agents with native or bridge-adapter support, providing structured mode switching, slash commands, and permission management. Also supports SSH tunnel-based port forwarding for remote/mobile access and a scheduled task (cron) system for recurring AI execution.

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
- `internal/handler/` — HTTP endpoints. All `/api/` routes use `middleware.Auth` (localhost bypass for CLI). Chat streaming via WebSocket (`/api/ai/events/ws`).
- `internal/service/` — Business logic: chat persistence, auto-summary, scheduler, SQLite, versioned schema migration, agent store (DB-backed), API key encryption (AES-256-GCM), default project persistence (`is_default` column in `recent_projects`). `SessionExecutor` unifies AI session execution for both chat and scheduled tasks.
- `internal/ai/` + `internal/ai/backends/` — AI backend abstraction and plugin system. `AIBackend` interface → `CLIBackend` (CLI args + LineParser) → `AutoResumeBackend` (ExitPlanMode → cancel → resume) → `ACPBackend` (JSON-RPC over stdio, connection pool). 12 backend sub-packages (claude, cline, codebuddy, copilot, codex, qoder, vecli, deepseek, kimi, mimo, opencode, pi), each registering via `ai.RegisterBackend()` in `init()`. `all.go` aggregates imports for `main.go`. ACP mapping wired by `backends/acp_wire.go`. `acpStdoutFilter` fixes JSON-RPC protocol violations (string-number ID mismatch, non-JSON stdout lines). CodeWhale (registered as `"deepseek"`, CLI: `codewhale`) has ACP support with remaps and stdout filter. Pi has ACP bridge support via `@touchtechclub/pi-acp`. `BackendSpec.AltCmd` for fallback CLI detection.
- `internal/model/` — Data models, `BackendRegistry` (backend specs + model discovery), `ProviderRegistry` (28 LLM providers).
- `internal/cli/` — AI agent self-service: `task`, `rag`, `migrate`.
- `internal/middleware/` — Auth, request logging, panic recovery, request ID.
- `internal/platform/` — Cross-platform path resolution, shell detection, Windows CLI utilities.
- `internal/speech/` — TTS: Edge TTS (native Go), Piper, Kokoro (Python venv bridge), MOSS-TTS-Nano (CLI wrapper). (MiniMax TTS 为预留配置项，尚未实装)
- `internal/summarize/` — Text summarization for auto-summary, TTS, task summaries.
- `internal/ssh/` — SSH tunnel server (password auth, auto host key, IP-level brute-force backoff). 状态查询通过 `GET /api/ssh/info`（无需鉴权，供 Android native 调用）。不发布 WS 事件。
- `internal/proxy/` — HTTP reverse proxy + port forwarding. Rewrites Host header for virtual-host backends.
- `internal/symbol/` — Code symbol extraction via tree-sitter (`gotreesitter`, pure Go, no CGO). 17 symbol kinds, 100+ languages.
- `internal/rag/` — RAG: SQLite + sqlite-vec (vec0 虚拟表) 向量存储 + FTS5 全文检索；OpenAI 兼容嵌入 API（默认 Ollama BGE-M3，可指向任意 OpenAI 兼容端点）。DuckDB 是历史对比项，不再使用。
- `internal/terminal/` — Web terminal: PTY sessions, ring buffer replay, multi-tab support, key/symbol configuration.
- `internal/ws/` — WebSocket event channel. `StreamHub` for session-scoped chat streaming fan-out. `Manager` for broadcast + buffered replay on reconnect. Client subscribe/unsubscribe/cancel/permission_respond messages.

### Frontend (Vue 3 + TypeScript)

**Source root:** `web/src/` — No Vue Router, drawer-based single-page layout. Single `reactive()` store in `stores/app.ts`.

**Composables** (by domain):
- Chat: `useChatSession`, `useChatStream`, `useChatRender`, `useChatContext`, `useChatKeyboard`, `useAutoSpeech`, `useQuickSend`, `useQuoteQuestion`, `useUserMsgIndex`
- Session: `useSessionIdentity`, `useSessionManager`, `useReconnect`, `useSessionSearch`
- Terminal: `useTerminalSession`, `useTerminalTabs`, `useTerminalKeys`, `useTerminalGestures`, `useTerminalKeyboard`, `useTerminalViewport`, `useKeyConfig`
- File: `useFileNavStack`, `useFileRefresh`, `useFileUpload`, `useFilePathAnnotation`, `useWorktreeAnnotation`, `useFileWatch`
- Navigation/Gesture: `useBackHandler`, `useEdgeSwipeBack`, `useSwipeDelete`, `useSwipeSession`, `useStickyScroll`
- Settings: `useSettingsConfig`, `useSettingsNavigation`
- Agent: `useAgents`, `useAcpSession`
- Task: `useTaskTab`, `useTaskForm`, `useTaskHistory`, `useTaskOverview`, `useTaskExecStream`
- Infrastructure: `useGlobalEvents`, `useConnectivityTest`, `usePortForward`, `useFrp`, `useCodeSymbols`, `useToast`, `useDialog`, `useTabDrawer`, `usePwaInstall`, `useAppMode`, `useLocale`, `useWakeLock`

**Components** (by domain):
- Chat: `ChatInputBar`, `ChatMessageItem`, `ChatMessageList`, `ChatPanelContent`, `ChatMetadataModal`, `ContentBlocks`, `SummaryToggle`, `QuickCommandDrawer`, `QuickCommandEditModal`, `QuickSendDrawer`, `QuickSendEditModal`, `QuoteQuestionBar`, `UserMsgIndexDrawer`, `OutputDrawer`, `ToolDetailDrawer`, `PlanPanel`, `AttachDrawer`
- File: `FileManagerContent`, `FileOverlay`, `FileViewer`, `FileHeader`, `FileIcon`, `DirBreadcrumb`, `FileAttachmentList`, `FileChangesDrawer`, `FileDetailsDrawer`, `CodePreview`, `MarkdownPreview`, `PdfPreview`, `OfficePreview`, `ImagePreview`, `VideoPreview`, `AudioPreview`, `OpenApiPreview`
- Terminal: `TerminalPanelContent`, `KeyConfigDrawer`, `KeyConfigTab`, `TerminalTabMenu`
- Git: `GitGraph`, `GitManageContent`, `GitHistoryDrawer`, `GitHistoryContent`, `GitDiffView`, `GitBreadcrumb`, `GitBranchList`, `GitBranchRow`, `GitCommitList`, `GitCommitMeta`, `GitTagList`, `GitWorktreeList`, `GitWorktreeCard`, `DiffDrawer`
- Session/Agent: `SessionDrawer`, `AcpSessionDrawer`, `AgentInstallDialog`, `CopyAgentDialog`, `SearchDrawer`, `RagDetailDrawer`, `SessionSearchDrawer`, `SessionSearchDetailModal`
- Task: `TaskTab`, `TaskListPage`, `TaskDetailPage`, `TaskFormPage`, `TaskExecDetail`, `TaskBreadcrumb`, `TaskHistoryTab`, `TaskOverviewTab`
- Settings: `SettingsPage`, `SettingsIndex`, `SettingsCategory`, `SettingsGroupPanel`, `SettingsItem`, `SettingsAgentsIndex`, `SettingsAgentDetail`, `SettingsRestartDialog`, `PasswordChangeDialog`
- Common: `BottomSheet`, `PopupMenu`, `Lightbox`, `ModalDialog`, `DialogOverlay`, `ToastNotification`, `SwipeToDeleteRow`, `AppHeader`, `HeaderMarquee`, `IosInstallDrawer`, `TabPanel`, `TableRowModal`, `ProxyPanelContent`, `ProxyPortItem`, `SearchInput`, `VersionMismatchOverlay`, `WelcomeOverlay`, `LoginView`, `ProjectDialog`

**Vite:** `hljsThemeWrapper` plugin for light/dark coexistence. Root `web/`, output `public/`. Alias `@` → `web/src/`.

## Key Patterns

- **WebSocket streaming:** Chat streaming and system events unified on single WS connection (`/api/ai/events/ws`). Clients subscribe to sessions via `{ type: "subscribe", session_id }`. `StreamHub` fans out events to all subscribers (multi-client). Cancel and permission response via WS client messages. HTTP cancel endpoint kept as fallback. WS reconnection handled by `useGlobalEvents`; on reconnect, client re-subscribes and StreamHub re-emits cached ACP state.
- **Block coalescing + @ commands:** Text/thinking merge into last same-type block; `tool_use` is boundary. `@chatsearch`/`@task` detected by `extractAtCommand()`, template-injected via `processAtCommand()`, rendered as purple badges. Frontend autocomplete in `ChatInputBar.vue`.
- **AutoResumeBackend:** ExitPlanMode → cancel → resume "继续". Emits `resume_split` for DB finalization.
- **ACP backend:** `ACPBackend` wraps ACP stdio agents with connection pooling (`ACPConnectionPool`, lazy init, 5-min idle sweep). Falls back to CLI via `sync.Once` if ACP not supported. State (mode/config/thinking/commands) cached and re-emitted on reconnect. Bridge adapters provide ACP for agents without native support. `acpStdoutFilter` fixes JSON-RPC protocol violations (string-number ID mismatch, non-JSON stdout lines) using `io.Pipe` to prevent cleanup hangs on process kill.
- **Agent system:** DB-backed (`agents` table). Models auto-discovered at runtime via `BackendRegistry` strategies. Shared rules template (`commonRulesTemplate`) embedded in Go binary. `@chatsearch`/`@task` template-injected on demand. `BackendSpec.InstallCmd` defines the install command for each backend; frontend shows install button for undetected backends.
- **Data directory:** Default `~/.clawbench/` (Windows: `%USERPROFILE%\.clawbench\`). Override with `--data-dir`. Multi-instance on different ports with auto-scoped cookies (`ScopedCookieName()`); use distinct `--data-dir` for data isolation.
- **Zero-config startup:** `config/config.yaml` optional. Auto-password persisted to `~/.clawbench/auto-password`. Filesystem root paths via `platform.ListRootPaths()`.
- **Versioned schema migration:** Auto-incrementing version numbers in `schema_migrations` table. Dirty flag prevents silent corruption. New migrations: append function + next version number.
- **Bugfix workflow (GitHub Issues):** Report bugs as GitHub Issues. Scheduled auto-fix task (Task #27): classify → fix in isolated worktree → write tests → verify → PR + CI → auto-merge → close issue. Labels track state: `bugfix:in-progress`, `bugfix:awaiting-review`, `bugfix:needs-design`, `bugfix:failed`, `bugfix:needs-verification`. Every bug fix MUST include a targeted unit test reproducing the original failure (Go: `*_test.go`, Frontend: `.test.ts` next to patched code). If genuinely untestable, explain why and use integration test or `bugfix:needs-verification` label.
- **Docker deployment:** `Dockerfile` + `docker-compose.yml` + `scripts/docker-build.sh`. Data via Docker volume at `/data/.clawbench/`.
- **Android integration:** HTML login + `AndroidNative` JS bridge. `BackgroundService` for SSH + WS. APK embedded in Go binary via `go:embed` (built by `--android` flag, placed in `internal/frontend/dist/assets/`). `/api/apk` endpoint serves APK from embedded FS only. Single-binary deployment includes APK without external files.
- **File path & worktree annotation:** `useWorktreeAnnotation` annotates worktree paths in chat messages, running before `useFilePathAnnotation` to prevent partial matches. `useFilePathAnnotation` detects file paths in code blocks and renders them as clickable links. Supports import path resolution (e.g., `@/composables/useFoo` → `web/src/composables/useFoo.ts`), external file paths, and line ranges (e.g., `file.go:42-50`). `scrollToLine()` flash-highlights a range of lines (200-line cap for performance).
- **File overlay navigation:** `useFileNavStack` manages a stack-based file overlay on the browse tab. Clicking a file pushes it onto the stack (overlay on top of file list); back button pops; close clears the stack.
- **Default project persistence:** Server-side `is_default` column in `recent_projects` table. `GetDefaultProject()` fallback chain: `is_default=1` row → most recently accessed project → home dir → first root path.
- **Binary file handling:** Backend `sanitizeTextContent` truncates binary files to 64KB and large text to 512KB (UTF-8 boundary safe). `forceText=1` query parameter returns sanitized content for binary files. Frontend shows placeholder with "Open as text" button for binary files.
- **Chat summary modes:** `simple` mode extracts last answer text from blocks (no AI call); `ai` mode uses `AsyncSummarize`; empty string disables summarization. Text summary and voice summary have separate backend/model/API configurations. Mode set via `SetChatSummaryMode()`.
- **Terminal multi-tab:** `useTerminalTabs` manages tab lifecycle (create/close/switch). `useTerminalKeys` processes virtual key input with modifier lock. `useKeyConfig` persists custom key/symbol layouts to DB via `/api/terminal/key-config`.
- **Settings navigation architecture:** Three-level hierarchy: Level 1 = `SettingsIndex` menu, Level 2 = category page, Level 3 = sub-page with a single `GroupPanel`. Rule: a `GroupPanel` that requires batch save (multiple fields submitted together) MUST have its own dedicated sub-page — it cannot share a page with other items. Flat instant-save items stay on the level 2 category page. Navigation action items (type `action`, source `local`, with `navigateTo` field) link to level 3 sub-pages. Sub-pages use colon-separated route IDs (e.g., `tts:tts_engine`, `chat:summarization_text`) registered in `subPagePanelMap` for data-driven rendering — no hard-coded `if` branches. Categories with only one GroupPanel and no flat items (e.g., `terminal`, `rag`, `portForward`, `frp`, `notification`) render the panel directly on level 2 without a separate level 3 sub-page.

## Development Rules

- **Mandatory appLog for all frontend logging:** All frontend code MUST use `appLog.d/i/w/e()` from `@/utils/appLog` instead of raw `console.log/debug/warn/error/info`. `appLog` prints to browser console AND relays to Android `AppLog` via the `AndroidNative.log()` JS bridge, ensuring logs are visible in Android WebView and persisted to `.clawbench/logs/android.log` through the server's `/api/android-log` endpoint. Raw `console.*` calls are only allowed in test files (`*.test.ts`). Tag convention: short PascalCase module name (e.g., `'ClawBench'`, `'ChatStream'`, `'Store'`, `'FileManager'`).
- **Mandatory AppLog for all Android logging:** All Android Java/Kotlin code MUST use `AppLog.d/i/w/e()` instead of raw `android.util.Log`. `AppLog` writes to logcat AND posts to the backend `/api/android-log` endpoint for centralized log persistence. Raw `android.util.Log` calls are only allowed in `AppLog.java` itself (to avoid recursion) and test code. Enforcement: `lint-android.sh` runs in pre-push checks and CI, catching violations at build time.
- **Mandatory unit tests for features and bug fixes:** Every new feature and bug fix MUST include targeted unit tests. Go: `*_test.go` next to the code; Frontend: `.test.ts` next to the composable/component. Tests must verify the specific behavior/fix, not just generic happy paths.
- **Coverage gate:** Two-tier enforcement on every PR/push to main. Tier 1: per-package coverage `>= baseline% - 1.5%`. Tier 2: changed lines coverage `>= 80%`.
- **Local pre-push validation (mandatory before push/PR):** Before pushing or creating a PR, MUST run the local pre-check script that replicates all CI checks:
  ```bash
  ./scripts/pre-push-checks.sh              # 全量检查（lint + test + build + typecheck + 覆盖率）
  ./scripts/pre-push-checks.sh --skip-coverage  # 跳过覆盖率门槛
  ./scripts/pre-push-checks.sh --skip-android   # 跳过 Android 覆盖率
  ./scripts/pre-push-checks.sh --fix            # golangci-lint 自动修复
  ```

## Configuration

`config/config.yaml` is entirely optional. See `config/config.example.yaml` for all available options. Key defaults:

| Section | Key defaults |
|---------|-------------|
| Server | `port` 20000, `password` auto-generated, `dev_port` 0 (auto) |
| TTS | `tts.engine` edge, `tts.max_cache_files` 100 |
| Summarize | `summarize.backend` "simple", `summarize.chat_summary` true |
| Port Forward | `port_forward.enabled` true, `port_forward.port` 0 (auto) |
| RAG | `rag.model` bge-m3, `rag.chunk_size` 512, `rag.retention_days` 90 |
| Terminal | `terminal.enabled` true, `terminal.idle_timeout` 10m, `terminal.max_sessions` 10 |
