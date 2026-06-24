//nolint:govet // shadowed err is standard Go pattern in sequential blocks
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"clawbench/internal/ai"
	_ "clawbench/internal/ai/backends"
	_ "clawbench/internal/ai/backends/claude"
	_ "clawbench/internal/ai/backends/cline"
	_ "clawbench/internal/ai/backends/codebuddy"
	_ "clawbench/internal/ai/backends/codex"
	_ "clawbench/internal/ai/backends/copilot"
	_ "clawbench/internal/ai/backends/deepseek"
	_ "clawbench/internal/ai/backends/kimi"
	_ "clawbench/internal/ai/backends/mimo"
	_ "clawbench/internal/ai/backends/opencode"
	_ "clawbench/internal/ai/backends/pi"
	_ "clawbench/internal/ai/backends/qoder"
	_ "clawbench/internal/ai/backends/vecli"
	"clawbench/internal/cli"
	"clawbench/internal/handler"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/push"
	"clawbench/internal/rag"
	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/ssh"
	"clawbench/internal/startup"
	"clawbench/internal/summarize"
	"clawbench/internal/terminal"
	"clawbench/internal/version"
	"clawbench/internal/ws"
)

// multiHandler sends log records to multiple handlers
type multiHandler struct {
	handlers []slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var lastError error
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, r); err != nil {
			lastError = err
		}
	}
	return lastError
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithAttrs(attrs)
	}
	return &multiHandler{handlers: newHandlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithGroup(name)
	}
	return &multiHandler{handlers: newHandlers}
}

// buildLogHandlers constructs the list of slog handlers for the multi-handler.
// If fileHandler is nil (e.g., file logging failed to initialize), only the
// text handler is used; otherwise both are included.
func buildLogHandlers(textHandler, fileHandler slog.Handler) []slog.Handler {
	handlers := []slog.Handler{textHandler}
	if fileHandler != nil {
		handlers = append(handlers, fileHandler)
	}
	return handlers
}

// generateBcryptHash creates a bcrypt hash of the given password.
// If bcrypt generation fails (e.g., password too long), it logs a warning
// and returns nil, causing the auth system to fall back to SHA256.
func generateBcryptHash(password string) []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		slog.Warn("failed to generate bcrypt hash, password verification will use SHA256 fallback", slog.String("err", err.Error()))
		return nil
	}
	return hash
}

// makeRestartFunc returns the function called when a server restart is requested.
// Under a supervisor (systemd/Docker), it just triggers graceful shutdown and
// lets the supervisor restart the process. Otherwise, it launches a sentinel
// process that waits for this process to exit, then starts a new one.
func makeRestartFunc(shutdown func()) func() {
	return func() {
		if handler.IsRunningUnderSupervisor() {
			slog.Info("running under supervisor, triggering graceful shutdown for restart")
		} else {
			cmd, err := handler.LaunchSentinelProcess()
			if err != nil {
				slog.Error("failed to launch sentinel process for restart", "err", err)
				return
			}
			slog.Info("sentinel process launched for restart", "sentinel_pid", cmd.Process.Pid)
		}
		shutdown()
	}
}

func main() { //nolint:gocognit,gocyclo // complex startup orchestration
	startTime := time.Now()

	// Root --help handler
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Println("ClawBench - Mobile-first AI workstation")
		fmt.Println()
		fmt.Println("Usage: clawbench <command> [options]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  task    Manage scheduled tasks (cron-based AI execution)")
		fmt.Println("  rag     Search and retrieve conversation history")
		fmt.Println()
		fmt.Println("Run \"clawbench <command> --help\" for more information.")
		fmt.Println()
		fmt.Println("Server options:")
		fmt.Println("  --port PORT    Server port (overrides config file, default: 20000)")
		os.Exit(0)
	}

	// Task subcommand dispatch (e.g., "clawbench task create --name ...")
	if len(os.Args) > 1 && os.Args[1] == "task" {
		os.Exit(cli.RunTaskCommand(os.Args[2:]))
	}

	// RAG subcommand dispatch (e.g., "clawbench rag search -q ...")
	if len(os.Args) > 1 && os.Args[1] == "rag" {
		os.Exit(cli.RunRAGCommand(os.Args[2:]))
	}

	// Parse CLI flags
	cliPort := 0
	for i, arg := range os.Args[1:] {
		if arg == "--port" && i+1 < len(os.Args[1:]) {
			if p, err := strconv.Atoi(os.Args[i+2]); err == nil && p > 0 && p <= 65535 {
				cliPort = p
			}
		}
	}

	// Determine binary directory for data storage (green portable layout)
	absBinPath, _ := filepath.Abs(os.Args[0])
	model.BinDir = filepath.Dir(absBinPath)

	// Load configuration — config/config.yaml is optional
	var cfg model.Config
	var presence map[string]bool

	// Search for config in priority order:
	// 1. <BinDir>/config/config.yaml (green portable: next to binary)
	// 2. config/config.yaml (CWD-relative, standard layout)
	configPath := cli.FindConfigPath(model.BinDir)

	data, err := os.ReadFile(configPath)
	if err == nil {
		// Parse into raw map first for presence detection (bool defaults)
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse %s: %v\n", configPath, err)
			os.Exit(1)
		}
		presence = model.ParsePresenceMap(raw)

		// Parse into typed config struct
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse %s: %v\n", configPath, err)
			os.Exit(1)
		}
	} else if !os.IsNotExist(err) {
		// File exists but can't be read (permissions, etc.)
		fmt.Fprintf(os.Stderr, "Failed to read %s: %v\n", configPath, err)
		os.Exit(1)
	}
	// If file doesn't exist: cfg stays zero-value, presence is nil → all defaults apply

	// Apply all defaults (returns auto-generated password if created)
	autoPassword := model.ApplyDefaults(&cfg, presence)
	model.ConfigInstance = cfg

	// Set global variables from config
	model.RootPaths = platform.ListRootPaths()
	model.UploadMaxSizeMB = cfg.Upload.MaxSizeMB
	model.UploadMaxFiles = cfg.Upload.MaxFiles
	model.ChatInitialMessages = cfg.Chat.InitialMessages
	model.ChatPageSize = cfg.Chat.PageSize
	model.ChatSessionPageSize = cfg.Chat.SessionPageSize
	model.ChatSystemPromptInterval = cfg.Chat.SystemPromptInterval
	model.SessionMaxCount = cfg.Session.MaxCount
	model.RecentProjectsMaxCount = cfg.RecentProjects.MaxCount
	model.TTSMaxCacheFiles = cfg.TTS.MaxCacheFiles
	model.RequireAuthForLocalhost = cfg.RequireAuthForLocalhost

	// Apply TTS text processing config (defaults applied in ApplyDefaults)
	summarize.InlineCodeMaxLen = cfg.TTS.InlineCodeMaxLen
	summarize.MaxSummarizeRunes = cfg.TTS.MaxSummarizeRunes

	// NOTE: TTS summarizer initialization is deferred until after DB init,
	// because the API key may need to be resolved from agent_api_keys table.

	// Initialize TTS synthesis provider from config
	var ttsProvider speech.SpeechProvider
	engine := cfg.TTS.Engine

	switch engine {
	case "edge":
		p := speech.NewEdgeTTSProvider()
		if cfg.TTS.Voice != "" {
			p.Voice = cfg.TTS.Voice
		}
		if cfg.TTS.Speed > 0 {
			// Convert speed multiplier (e.g. 1.5) to edge-tts rate percentage (e.g. "+50%")
			ratePercent := int((cfg.TTS.Speed - 1.0) * 100)
			if ratePercent > 0 {
				p.Rate = fmt.Sprintf("+%d%%", ratePercent)
			} else if ratePercent < 0 {
				p.Rate = fmt.Sprintf("%d%%", ratePercent)
			}
		}
		ttsProvider = p
		slog.Info(
			"tts provider configured",
			slog.String("engine", "edge"),
			slog.String("voice", p.Voice),
			slog.String("rate", p.Rate),
		)
	case "piper":
		p := speech.NewPiperProvider()
		// Resolve model path: explicit config > voice-based path
		p.ModelPath = speech.ResolveModelPath(cfg.TTS.Voice, cfg.TTS.Piper.ModelPath)
		if cfg.TTS.Piper.NoiseScale > 0 {
			p.NoiseScale = cfg.TTS.Piper.NoiseScale
		}
		// LengthScale: explicit piper.length_scale takes priority;
		// otherwise convert speed multiplier (length_scale = 1/speed)
		if cfg.TTS.Piper.LengthScale > 0 {
			p.LengthScale = cfg.TTS.Piper.LengthScale
		} else if cfg.TTS.Speed > 0 {
			p.LengthScale = 1.0 / cfg.TTS.Speed
		}
		if cfg.TTS.Piper.SentenceSilence > 0 {
			p.SentenceSilence = cfg.TTS.Piper.SentenceSilence
		}
		ttsProvider = p
		slog.Info(
			"tts provider configured",
			slog.String("engine", "piper"),
			slog.String("model_path", p.ModelPath),
			slog.Float64("noise_scale", p.NoiseScale),
			slog.Float64("length_scale", p.LengthScale),
			slog.Float64("sentence_silence", p.SentenceSilence),
		)
	case "kokoro":
		k := speech.NewKokoroProvider()
		if cfg.TTS.Voice != "" {
			k.Voice = cfg.TTS.Voice
		}
		if cfg.TTS.Speed > 0 {
			k.Speed = cfg.TTS.Speed
		}
		if cfg.TTS.Kokoro.Lang != "" {
			k.Lang = cfg.TTS.Kokoro.Lang
		}
		k.ModelPath, k.VoicesPath = speech.ResolveKokoroPaths(cfg.TTS.Kokoro.ModelPath, cfg.TTS.Kokoro.VoicesPath)
		ttsProvider = k
		slog.Info(
			"tts provider configured",
			slog.String("engine", "kokoro"),
			slog.String("model_path", k.ModelPath),
			slog.String("voices_path", k.VoicesPath),
			slog.String("voice", k.Voice),
			slog.String("lang", k.Lang),
			slog.Float64("speed", k.Speed),
		)
	case "moss-nano":
		m := speech.NewMossNanoProvider()
		if cfg.TTS.MossNano.Backend != "" {
			m.Backend = cfg.TTS.MossNano.Backend
		}
		m.ModelDir = speech.ResolveMossNanoModelDir(cfg.TTS.MossNano.ModelDir)
		if cfg.TTS.MossNano.PromptSpeech != "" {
			m.PromptSpeech = cfg.TTS.MossNano.PromptSpeech
		}
		if cfg.TTS.MossNano.Voice != "" {
			m.Voice = cfg.TTS.MossNano.Voice
		}
		ttsProvider = m
		slog.Info(
			"tts provider configured",
			slog.String("engine", "moss-nano"),
			slog.String("backend", m.Backend),
			slog.String("model_dir", m.ModelDir),
			slog.String("prompt_speech", m.PromptSpeech),
			slog.String("voice", m.Voice),
		)
	default:
		// Default to Edge TTS when engine is empty or unrecognized
		p := speech.NewEdgeTTSProvider()
		if cfg.TTS.Voice != "" {
			p.Voice = cfg.TTS.Voice
		}
		if cfg.TTS.Speed > 0 {
			ratePercent := int((cfg.TTS.Speed - 1.0) * 100)
			if ratePercent > 0 {
				p.Rate = fmt.Sprintf("+%d%%", ratePercent)
			} else if ratePercent < 0 {
				p.Rate = fmt.Sprintf("%d%%", ratePercent)
			}
		}
		ttsProvider = p
		slog.Info(
			"tts provider configured",
			slog.String("engine", "edge"),
			slog.String("voice", p.Voice),
			slog.String("rate", p.Rate),
		)
	}
	handler.SetSpeechProvider(ttsProvider)

	fileHandler, err := service.NewFileHandler(cfg.LogDir, "clawbench", cfg.LogMaxDays)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize file logger, logging to stderr only: %v\n", err)
	} else {
		defer func() { _ = fileHandler.Close() }()
	}

	// Log level from config (default: "info")
	logLevel := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	// Create a multi-writer for both stderr and file
	textHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	multiHandler := &multiHandler{
		handlers: buildLogHandlers(textHandler, fileHandler),
	}
	slog.SetDefault(slog.New(multiHandler))
	slog.Info("server starting")

	// Load .env file into process environment (before loading agents,
	// so agent env ${VAR} references can be resolved at request time)
	dotenvPath := filepath.Join(model.BinDir, ".env")
	if _, err := os.Stat(dotenvPath); os.IsNotExist(err) {
		dotenvPath = ".env"
	}
	if _, err := os.Stat(dotenvPath); err == nil {
		if err := model.LoadDotEnv(dotenvPath); err != nil {
			slog.Warn("failed to load .env file", slog.String("path", dotenvPath), slog.String("err", err.Error()))
		} else {
			slog.Info("loaded .env file", slog.String("path", dotenvPath))
		}
	}

	// Ensure $SHELL reflects the user's login shell (from /etc/passwd).
	// On Debian/Ubuntu, $SHELL may be /bin/sh (dash) when started from
	// non-login contexts (systemd, cron, nohup), but AI CLI tools read
	// $SHELL to decide which shell their "Bash tool" uses.
	platform.SetLoginShell()

	// Auto-generated password info is now shown in the startup banner (below).
	// ISS-003d: don't log plaintext password to slog.
	if autoPassword != "" {
		slog.Info(
			"auto-generated password (no password configured)",
			slog.String("file", filepath.Join(model.BinDir, ".clawbench", "auto-password")),
		)
	}

	// Initialize password verification state
	if sha256Hash := model.ParseSHA256Hash(cfg.Password); sha256Hash != "" {
		// Password is stored as SHA-256 hash — use directly for login/SSH verification
		model.SessionToken = sha256Hash
		model.PasswordIsSHA256 = true
		// No bcrypt: the SHA-256 hash IS the verifier for login/SSH
		model.PasswordHash = nil
	} else {
		// Plaintext password (or auto-generated) — existing behavior
		hash := sha256.Sum256([]byte(cfg.Password + "clawbench-salt"))
		model.SessionToken = hex.EncodeToString(hash[:])
		model.PasswordIsSHA256 = false
		// Generate bcrypt hash for secure password verification (ISS-003a)
		if cfg.Password != "" {
			bcryptHash := generateBcryptHash(cfg.Password)
			model.PasswordHash = bcryptHash
		}
	}

	// Initialize cookie token (cryptographically random, decoupled from password).
	// Load from disk if available; otherwise generate a new one.
	// This ensures the cookie value cannot be derived from the password hash.
	// (ISS-117, ISS-131, ISS-183)
	if model.SessionToken != "" {
		if persisted := model.LoadCookieToken(); persisted != "" {
			model.CookieToken = persisted
		} else {
			model.CookieToken = model.GenerateRandomToken(32)
			model.PersistCookieToken(model.CookieToken)
		}
	}

	// Initialize SQLite database (runFromServer=true: clean up orphaned streaming messages)
	if err := service.InitDB(true); err != nil {
		slog.Error("failed to initialize database", slog.String("err", err.Error()))
		service.CloseDB()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: CloseDB called explicitly above; defer is for normal path
	}
	defer service.CloseDB()

	// Load persisted agent capabilities from DB so mode/thinking/command chips
	// appear immediately on startup without requiring prefetch.
	ai.SetRegistryDB(service.DB)
	ai.GetAgentCapabilityRegistry().LoadFromDB(service.DB)

	// Kill orphan AI subprocesses from a previous server crash.
	// On Linux, scans /proc for CLAWBENCH_CHILD=1 env marker.
	ai.CleanupOrphans()

	// Resolve summarize API key from agent_api_keys table if not in config.
	// New setups write the key directly to config.yaml. This fallback resolves
	// the key from DB for legacy configs that have key="" and agent_id set.
	if cfg.Summarize.Backend == "api" && cfg.Summarize.API.Key == "" && cfg.Summarize.API.AgentID != "" {
		if _, _, ak, err := service.LoadAgentAnyAPIKey(service.DB, cfg.Summarize.API.AgentID); err == nil && ak != "" {
			cfg.Summarize.API.Key = ak
			slog.Info("resolved summarize API key from agent_api_keys", slog.String("agent_id", cfg.Summarize.API.AgentID))
		} else if err != nil {
			slog.Warn("failed to resolve summarize API key from agent_api_keys", slog.String("agent_id", cfg.Summarize.API.AgentID), slog.String("err", err.Error()))
		}
	}

	// Inject API key loader for Pi CLI runtime (avoids import cycle between ai and service packages)
	ai.SetAgentAPIKeyLoader(func(agentID string) (provider, customURL, apiKey string, found bool) {
		p, cu, ak, err := service.LoadAgentAnyAPIKey(service.DB, agentID)
		if err != nil || ak == "" {
			return "", "", "", false
		}
		return p, cu, ak, true
	})

	// Inject external session ID getter for ResumeSession recovery
	ai.SetExternalSessionIDGetter(service.GetExternalSessionID)

	// Inject auto-approve getter for ACP permission auto-approval
	ai.SetAutoApproveGetter(service.GetSessionAutoApprove)

	// Inject session running checker for ACP idle sweep (avoids import cycle)
	ai.GetACPConnManager().SetSessionRunningChecker(service.IsSessionRunning)

	// Inject permission state change callback (emits WS event on approval state change)
	ai.SetPermissionStateChangeCallback(func(clawbenchSID string, pending bool, toolName string) {
		status := "permission_resolved"
		if pending {
			status = "permission_pending"
		}
		service.EmitSessionEvent(clawbenchSID, status, false, toolName)
	})

	// Initialize TTS summarizer from config (deferred from earlier — needs DB for API key resolution).
	// Language is now per-request (sent from frontend), not configured at startup.
	summarizeBackend := cfg.Summarize.Backend

	var ttsSummarizer summarize.Summarizer
	switch summarizeBackend {
	case "", "simple":
		ttsSummarizer = summarize.NewSimple()
		slog.Info("tts summarizer configured", slog.String("backend", "simple"))
	case "api":
		if cfg.Summarize.API.BaseURL == "" {
			slog.Error("summarize.backend is \"api\" but summarize.api.base_url is not configured")
			os.Exit(1)
		}
		if cfg.Summarize.API.Format == "anthropic" {
			s := summarize.NewAnthropic(cfg.Summarize.API.BaseURL, cfg.Summarize.API.Key, cfg.Summarize.Model)
			ttsSummarizer = s
			slog.Info("tts summarizer configured", slog.String("backend", "api"), slog.String("format", "anthropic"), slog.String("model", s.Model))
		} else {
			s := summarize.NewOpenAI(cfg.Summarize.API.BaseURL, cfg.Summarize.API.Key, cfg.Summarize.Model)
			ttsSummarizer = s
			slog.Info("tts summarizer configured", slog.String("backend", "api"), slog.String("format", "openai"), slog.String("model", s.Model))
		}
	default:
		s, err := summarize.NewAIBackendSummarizer(summarizeBackend)
		if err != nil {
			slog.Error("failed to create AI backend summarizer, falling back to simple",
				slog.String("backend", summarizeBackend), slog.String("error", err.Error()))
			ttsSummarizer = summarize.NewSimple()
		} else {
			s.Model = cfg.Summarize.Model // empty = use backend default
			ttsSummarizer = s
			slog.Info("tts summarizer configured", slog.String("backend", summarizeBackend), slog.String("model", s.Model))
		}
	}
	handler.SetSummarizer(ttsSummarizer)

	// Initialize RAG history memory system
	if err := rag.Init(cfg.RAG); err != nil {
		slog.Warn("failed to initialize RAG system, search will be limited", slog.String("err", err.Error()))
	}
	defer rag.Shutdown()

	// Determine port before loading skills/agents (skills and agents need {{PORT}})
	port := cfg.Port
	// Allow PORT environment variable to override config
	if portStr := os.Getenv("PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}
	// CLI --port flag takes highest priority
	if cliPort > 0 {
		port = cliPort
	}

	// Set global port for cookie name scoping (multi-instance on same hostname)
	model.ServerPort = port

	// Load agent configurations (set ClawbenchBin first for placeholder replacement)
	model.ClawbenchBin = absBinPath

	// 1. Detect installed CLIs and write new agents to DB
	present := model.SyncDiscoverAgentsDB(service.DB)

	// 1a. Load manually-defined agents from config/agents/*.yaml (e.g., acp-mock for E2E)
	model.LoadYamlAgents(service.DB, filepath.Dir(configPath))

	// 2. Synchronous model discovery (run when agents may have empty model lists)
	discoveredModels := model.SyncDiscoverModels()

	// 2a. Migrate custom_system_prompt BEFORE LoadAgentsIntoMemory so the
	// composition logic (commonPrompt + customSystemPrompt) works correctly
	// on first startup with legacy system_prompt data.
	service.MigrateCustomSystemPrompt(service.DB)

	// 3. Merge runtime data: fill models/levels from discovery results/registry, delete missing CLIs, reload memory
	model.MergeDiscoveredDataDB(service.DB, discoveredModels, present)

	slog.Info("agents loaded", slog.Int("count", len(model.AgentList)))

	// 4. Async: refresh model cache in background (non-blocking)
	model.AsyncRefreshModelCache(service.DB)

	// Set default agent ID from config, or fall back to first agent
	if cfg.DefaultAgent != "" {
		if _, ok := model.Agents[cfg.DefaultAgent]; ok {
			model.DefaultAgentID = cfg.DefaultAgent
		} else {
			// List available agent IDs to help the user fix the config
			availableIDs := make([]string, 0, len(model.AgentList))
			for _, a := range model.AgentList {
				availableIDs = append(availableIDs, a.ID)
			}
			slog.Warn("configured default_agent not found, using first agent",
				slog.String("configured", cfg.DefaultAgent),
				slog.Any("available", availableIDs))
		}
	}
	if model.DefaultAgentID == "" && len(model.AgentList) > 0 {
		model.DefaultAgentID = model.AgentList[0].ID
	}
	if model.DefaultAgentID != "" {
		slog.Info("default agent", slog.String("id", model.DefaultAgentID))
	} else {
		slog.Warn("no agents available, session creation will fail")
	}

	// Initialize and start scheduler (MUST be after agents are loaded so model.Agents is populated)
	scheduler := service.NewScheduler()

	// Initialize task summarizer if summarization backend is configured (MUST be before scheduler.Start())
	if cfg.Summarize.Backend == "simple" {
		// Simple mode: extract final answer for chat, SimpleSummarizer for tasks
		pipeline := summarize.NewPipelineWithOpts(
			func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
				return summarize.NewSimple().Summarize(ctx, text, "")
			},
			"",
			summarize.SummarizeOption{PreserveMarkdown: true},
		)
		taskSummarizer := summarize.NewTaskSummarizerFromPipeline(pipeline)
		scheduler.SetTaskSummarizer(taskSummarizer)
		service.SetTaskSummarizerInstance(taskSummarizer)
		service.SetChatSummaryMode("simple")
		service.SetChatSummaryEnabled(cfg.Summarize.IsChatSummaryEnabled())
		slog.Info("task summarizer configured", slog.String("backend", "simple"))
	} else if cfg.Summarize.Backend != "" {
		taskSummarizer, err := initTaskSummarizer(cfg)
		if err != nil {
			slog.Warn(
				"failed to create task summarizer, task summaries will be disabled",
				slog.String("backend", cfg.Summarize.Backend),
				slog.String("err", err.Error()),
			)
		} else {
			scheduler.SetTaskSummarizer(taskSummarizer)
			// Also set the global instance for AsyncSummarize (chat messages + task executions)
			service.SetTaskSummarizerInstance(taskSummarizer)
			service.SetChatSummaryMode("ai")
			service.SetChatSummaryEnabled(cfg.Summarize.IsChatSummaryEnabled())
			slog.Info(
				"task summarizer configured",
				slog.String("backend", cfg.Summarize.Backend),
			)
		}
	}
	// else: cfg.Summarize.Backend == "" — fully disabled, no taskSummarizerInstance

	// Load all tasks from all projects
	if err := scheduler.LoadTasksFromDB(""); err != nil {
		slog.Warn("failed to load scheduled tasks", slog.String("err", err.Error()))
	}
	scheduler.Start()
	defer scheduler.Stop()
	service.GlobalScheduler = scheduler

	// Stop ACP connection pool on shutdown (kills long-lived agent processes)
	defer ai.GetACPConnManager().StopAll()

	// Start periodic cleanup of stale WS subscriptions (every 60s)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if mgr := ws.GetManager(); mgr != nil {
				mgr.CleanupStale()
			}
		}
	}()

	host := cfg.Host
	addr := fmt.Sprintf("%s:%d", host, port)
	slog.Info(
		"server ready",
		slog.String("addr", addr),
		slog.String("roots", strings.Join(model.RootPaths, ", ")),
		slog.Bool("auth_enabled", model.SessionToken != ""),
	)
	if cfg.DevPort > 0 {
		slog.Info("dev HTTP listener enabled", slog.Int("port", cfg.DevPort))
	}

	// Initialize RAG indexer (needs final port number)
	if rag.GlobalStore != nil {
		rag.StartIndexer(cfg.RAG)
	}

	// Start cleanup worker for soft-deleted data
	rag.StartCleanupWorker(cfg.RAG)

	// Initialize proxy service (port forwarding) and SSH tunnel server.
	// ProxyRegistry is only created when SSH tunnel is enabled — it has no
	// standalone purpose without the SSH tunnel to transport traffic.
	var sshServerRef *ssh.Server
	if cfg.PortForward.Enabled {
		proxyService := service.NewProxyRegistry(port)
		// Always apply config — empty AllowedPorts means "allow all ports"
		proxyService.SetAllowedPorts(cfg.PortForward.AllowedPorts)
		service.ProxyService = proxyService
		defer proxyService.Stop()

		sshServerRef = ssh.NewServer(cfg.PortForward, port, cfg.Password, proxyService)
		handler.SetSSHServer(sshServerRef)
		go func() {
			if err := sshServerRef.ListenAndServe(); err != nil {
				slog.Error("SSH server failed", slog.String("err", err.Error()))
			}
		}()
		defer func() { sshServerRef.Close() }()
	} else {
		slog.Info("SSH tunnel and port forwarding disabled by config")
	}

	// Initialize file watcher for auto-refresh (non-critical — continue on failure)
	if err := service.InitFileWatcher(); err != nil {
		slog.Warn(
			"file watcher not available, auto-refresh disabled",
			slog.String("err", err.Error()),
		)
	} else {
		defer service.StopFileWatcher()
	}

	// Initialize terminal manager (interactive web terminal)
	if cfg.Terminal.Enabled {
		terminalMgr := terminal.NewManager(cfg.Terminal, port)
		handler.SetTerminalManager(terminalMgr)
		defer func() { terminalMgr.Close() }()
		slog.Info(
			"terminal manager initialized",
			slog.String("idle_timeout", func() string {
				if cfg.Terminal.IdleTimeout == "" || cfg.Terminal.IdleTimeout == "0" {
					return "never"
				}
				return cfg.Terminal.IdleTimeout
			}()),
			slog.Int("buffer_lines", cfg.Terminal.BufferLines),
		)
	}

	// Initialize WS event manager
	jpushClient := push.NewJPushClient(cfg.Push.JPush)
	ws.InitManager(jpushClient)
	handler.SetPushClient(jpushClient)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Wire up the restart function for POST /api/config/restart
	// The sentinel process approach: launch a watcher that starts a new process
	// after this one exits, then trigger graceful shutdown.
	handler.SetRestartFunc(makeRestartFunc(selfSignalInterrupt))

	srv := &http.Server{Handler: mux}

	// Optional localhost-only HTTP dev listener (for Vite dev proxy)
	var devSrv *http.Server
	if cfg.DevPort > 0 {
		devSrv = &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", cfg.DevPort),
			Handler: mux,
		}
	}

	// Resolve TLS config and scheme before banner.
	// This also logs the HTTP/TLS mode so those slog lines appear
	// *before* the banner and don't visually disrupt it.
	scheme := "http"
	tlsCertFile := ""
	tlsKeyFile := ""
	if cfg.TLS.Enabled {
		tlsCertFile = cfg.TLS.CertFile
		if tlsCertFile == "" {
			tlsCertFile = os.Getenv("CERT_FILE")
		}
		tlsKeyFile = cfg.TLS.KeyFile
		if tlsKeyFile == "" {
			tlsKeyFile = os.Getenv("KEY_FILE")
		}
		if tlsCertFile == "" || tlsKeyFile == "" {
			slog.Warn("TLS enabled but cert_file and key_file are not configured, falling back to HTTP")
		} else {
			scheme = "https"
			slog.Info("starting with TLS", slog.String("cert", tlsCertFile))
		}
	}
	if scheme == "http" {
		slog.Info("starting with HTTP")
	}

	// Pre-bind the main listener to detect port conflicts BEFORE printing the banner.
	// Without this, PrintBanner shows a password for an instance that immediately fails
	// to bind, confusing users who then see "wrong password" when they connect to
	// whichever process actually holds the port.
	mainLn, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		slog.Error("failed to listen", slog.String("addr", addr), slog.String("err", err.Error()))
		os.Exit(1)
	}

	// Start dev HTTP listener before banner (so its slog doesn't disrupt the banner)
	var devLn net.Listener
	if devSrv != nil {
		devLn, err = (&net.ListenConfig{}).Listen(context.Background(), "tcp", devSrv.Addr)
		if err != nil {
			_ = mainLn.Close()
			slog.Error("failed to listen on dev port", slog.String("addr", devSrv.Addr), slog.String("err", err.Error()))
			os.Exit(1)
		}
		if scheme == "https" {
			go func() {
				if err := devSrv.Serve(devLn); err != nil && err != http.ErrServerClosed {
					slog.Error("dev listener failed", slog.String("err", err.Error()))
				}
			}()
		}
	}

	// --- Print startup banner (MUST be the last output before HTTP server starts) ---
	// All subsystem initialization is complete; this is the final summary
	// shown to the operator before the server begins accepting connections.
	// Placed here to avoid being visually disrupted by subsequent slog lines.

	// Build agent info list
	agentInfos := make([]startup.AgentInfo, 0, len(model.AgentList))
	for _, a := range model.AgentList {
		agentInfos = append(agentInfos, startup.AgentInfo{
			Name:   a.Name,
			Models: len(a.Models),
		})
	}

	// Count scheduled tasks
	taskCount := scheduler.TaskCount()

	// Determine SSH port
	sshEnabled := cfg.PortForward.Enabled && sshServerRef != nil
	sshPort := 0
	if sshEnabled {
		sshPort = sshServerRef.Port()
	}

	startup.PrintBanner(startup.BannerConfig{
		Version:         version.Get(),
		Scheme:          scheme,
		Port:            port,
		LocalIP:         platform.GetOutboundIP(),
		AutoPassword:    autoPassword,
		DataDir:         filepath.Join(model.BinDir, ".clawbench"),
		Agents:          agentInfos,
		SSHEnabled:      sshEnabled,
		SSHPort:         sshPort,
		TTSEngine:       engine,
		RAGAvailable:    rag.GlobalStore != nil,
		TerminalOn:      cfg.Terminal.Enabled,
		TaskCount:       taskCount,
		StartupDuration: time.Since(startTime),
	})

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		slog.Info("received shutdown signal, draining connections...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", slog.String("err", err.Error()))
		}
		if devSrv != nil {
			if err := devSrv.Shutdown(shutdownCtx); err != nil {
				slog.Error("dev listener shutdown error", slog.String("err", err.Error()))
			}
		}
	}()

	// Start HTTP server using the pre-bound listener (blocking)
	if scheme == "https" {
		if err := srv.ServeTLS(mainLn, tlsCertFile, tlsKeyFile); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", slog.String("err", err.Error()))
			os.Exit(1)
		}
	} else {
		if err := srv.Serve(mainLn); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", slog.String("err", err.Error()))
			os.Exit(1)
		}
	}
	slog.Info("server stopped")
}

// initTaskSummarizer creates a TaskSummarizer based on the summarize.backend config.
// Supports: AI CLI backends (claude/codebuddy/kimi/etc.), "api" (OpenAI/Anthropic HTTP), "simple".
func initTaskSummarizer(cfg model.Config) (*summarize.TaskSummarizer, error) {
	backend := cfg.Summarize.Backend
	modelName := cfg.Summarize.Model

	switch backend {
	case "simple":
		// Simple summarizer: truncate-only, no AI call. Wrap in pipeline with PreserveMarkdown.
		pipeline := summarize.NewPipelineWithOpts(
			func(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
				return summarize.NewSimple().Summarize(ctx, text, "")
			},
			"", // use default prompt
			summarize.SummarizeOption{PreserveMarkdown: true},
		)
		return summarize.NewTaskSummarizerFromPipeline(pipeline), nil

	case "api":
		if cfg.Summarize.API.BaseURL == "" {
			return nil, fmt.Errorf("summarize.backend is \"api\" but summarize.api.base_url is not configured")
		}
		// For API backends, create OpenAI/Anthropic summarizer and wrap its pass function
		// in a pipeline with PreserveMarkdown=true and task-specific prompt.
		if cfg.Summarize.API.Format == "anthropic" {
			s := summarize.NewAnthropic(cfg.Summarize.API.BaseURL, cfg.Summarize.API.Key, modelName)
			pipeline := summarize.NewPipelineWithOpts(
				s.DoSummarizePass,
				summarize.TaskSummarizePrompt(),
				summarize.SummarizeOption{PreserveMarkdown: true},
			)
			return summarize.NewTaskSummarizerFromPipeline(pipeline), nil
		}
		s := summarize.NewOpenAI(cfg.Summarize.API.BaseURL, cfg.Summarize.API.Key, modelName)
		pipeline := summarize.NewPipelineWithOpts(
			s.DoSummarizePass,
			summarize.TaskSummarizePrompt(),
			summarize.SummarizeOption{PreserveMarkdown: true},
		)
		return summarize.NewTaskSummarizerFromPipeline(pipeline), nil

	default:
		// AI CLI backends (claude/codebuddy/kimi/opencode/codex/qoder/vecli/deepseek)
		return summarize.NewTaskSummarizer(backend, modelName)
	}
}
