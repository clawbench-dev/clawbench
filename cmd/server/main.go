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
	_ "clawbench/internal/ai/backends/antigravity"
	_ "clawbench/internal/ai/backends/claude"
	_ "clawbench/internal/ai/backends/codebuddy"
	_ "clawbench/internal/ai/backends/codex"
	_ "clawbench/internal/ai/backends/copilot"
	_ "clawbench/internal/ai/backends/deepseek"
	_ "clawbench/internal/ai/backends/grok"
	_ "clawbench/internal/ai/backends/kimi"
	_ "clawbench/internal/ai/backends/mimo"
	_ "clawbench/internal/ai/backends/opencode"
	_ "clawbench/internal/ai/backends/pi"
	_ "clawbench/internal/ai/backends/qoder"
	_ "clawbench/internal/ai/backends/vecli"
	"clawbench/internal/cli"
	"clawbench/internal/frontend"
	"clawbench/internal/frp"
	"clawbench/internal/handler"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/push/common"
	"clawbench/internal/push/dingtalk"
	"clawbench/internal/push/feishu"
	"clawbench/internal/rag"
	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/ssh"
	"clawbench/internal/startup"
	"clawbench/internal/stt"
	"clawbench/internal/summarize"
	"clawbench/internal/terminal"
	"clawbench/internal/version"
	"clawbench/internal/ws"
)

const (
	summarizeBackendAPI    = "api"
	summarizeBackendSimple = "simple"
)

// dingtalkDBAdapter bridges the dingtalk package's DB interface to service package
// functions, avoiding import cycles between service → dingtalk → service.
type dingtalkDBAdapter struct{}

func (dingtalkDBAdapter) MergeConfigSubscribers(users []string) {
	service.MergeDingTalkConfigSubscribers(users)
}

func (dingtalkDBAdapter) GetSubscribers() ([]common.SubscriberInfo, error) {
	subs, err := service.GetDingTalkSubscribers()
	if err != nil {
		return nil, err
	}
	result := make([]common.SubscriberInfo, len(subs))
	for i, s := range subs {
		result[i] = common.SubscriberInfo{
			UserID:         s.UserID,
			ConversationID: s.ConversationID,
			UserName:       s.UserName,
			Source:         s.Source,
		}
	}
	return result, nil
}

func (dingtalkDBAdapter) UpsertSubscriber(userID, conversationID, userName, source string) error {
	return service.UpsertDingTalkSubscriber(userID, conversationID, userName, source)
}

func (dingtalkDBAdapter) DeleteSubscriber(userID string) error {
	return service.DeleteDingTalkSubscriber(userID)
}

// dingtalkSessionMessenger bridges the dingtalk package's SessionMessenger interface
// to service package functions, avoiding import cycles (service → dingtalk → service).
type dingtalkSessionMessenger struct{}

func (dingtalkSessionMessenger) FindSessionsByPrefix(prefix string, runningOnly bool) ([]common.SessionInfo, error) {
	var sessions []service.DingTalkSessionInfo
	var err error
	if runningOnly {
		sessions, err = service.FindRunningSessionsByPrefix(prefix)
	} else {
		sessions, err = service.FindSessionsByPrefix(prefix)
	}
	if err != nil {
		return nil, err
	}
	result := make([]common.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = common.SessionInfo{
			ID:          s.ID,
			Title:       s.Title,
			ProjectPath: s.ProjectPath,
			Backend:     s.Backend,
			AgentID:     s.AgentID,
			Model:       s.Model,
		}
	}
	return result, nil
}

func (dingtalkSessionMessenger) ListRecentSessions(limit int) ([]common.SessionInfo, error) {
	sessions, err := service.ListRecentSessions(limit)
	if err != nil {
		return nil, err
	}
	result := make([]common.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = common.SessionInfo{
			ID:          s.ID,
			Title:       s.Title,
			ProjectPath: s.ProjectPath,
			Backend:     s.Backend,
			AgentID:     s.AgentID,
			Model:       s.Model,
		}
	}
	return result, nil
}

func (dingtalkSessionMessenger) IsSessionRunning(sessionID string) bool {
	return service.IsSessionRunning(sessionID)
}

// EnqueueMessage appends a message to the session's in-memory queue.
// Always succeeds — the underlying service.EnqueueMessage is an in-memory append.
func (dingtalkSessionMessenger) EnqueueMessage(sessionID, message string) error {
	service.EnqueueMessage(sessionID, model.QueuedMessage{
		Text:      message,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	return nil
}

func (dingtalkSessionMessenger) ClearQueue(sessionID string) {
	service.ClearQueue(sessionID)
}

func (dingtalkSessionMessenger) SendMessageToSession(sessionID, message string) error {
	return service.SendMessageToSessionFromDingTalk(sessionID, message)
}

// feishuDBAdapter bridges the feishu package's DB interface to service package
// functions, avoiding import cycles between service → feishu → service.
type feishuDBAdapter struct{}

func (feishuDBAdapter) MergeConfigSubscribers(users []string) {
	service.MergeFeishuConfigSubscribers(users)
}

func (feishuDBAdapter) GetSubscribers() ([]common.SubscriberInfo, error) {
	subs, err := service.GetFeishuSubscribers()
	if err != nil {
		return nil, err
	}
	result := make([]common.SubscriberInfo, len(subs))
	for i, s := range subs {
		result[i] = common.SubscriberInfo{
			UserID:         s.UserID,
			ConversationID: s.ChatID,
			UserName:       s.UserName,
			Source:         s.Source,
		}
	}
	return result, nil
}

func (feishuDBAdapter) UpsertSubscriber(userID, conversationID, userName, source string) error {
	return service.UpsertFeishuSubscriber(userID, conversationID, userName, source)
}

func (feishuDBAdapter) DeleteSubscriber(userID string) error {
	return service.DeleteFeishuSubscriber(userID)
}

// feishuSessionMessenger bridges the feishu package's SessionMessenger interface
// to service package functions, avoiding import cycles.
type feishuSessionMessenger struct{}

func (feishuSessionMessenger) FindSessionsByPrefix(prefix string, runningOnly bool) ([]common.SessionInfo, error) {
	var sessions []service.DingTalkSessionInfo
	var err error
	if runningOnly {
		sessions, err = service.FindRunningSessionsByPrefix(prefix)
	} else {
		sessions, err = service.FindSessionsByPrefix(prefix)
	}
	if err != nil {
		return nil, err
	}
	result := make([]common.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = common.SessionInfo{
			ID:          s.ID,
			Title:       s.Title,
			ProjectPath: s.ProjectPath,
			Backend:     s.Backend,
			AgentID:     s.AgentID,
			Model:       s.Model,
		}
	}
	return result, nil
}

func (feishuSessionMessenger) ListRecentSessions(limit int) ([]common.SessionInfo, error) {
	sessions, err := service.ListRecentSessions(limit)
	if err != nil {
		return nil, err
	}
	result := make([]common.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = common.SessionInfo{
			ID:          s.ID,
			Title:       s.Title,
			ProjectPath: s.ProjectPath,
			Backend:     s.Backend,
			AgentID:     s.AgentID,
			Model:       s.Model,
		}
	}
	return result, nil
}

func (feishuSessionMessenger) IsSessionRunning(sessionID string) bool {
	return service.IsSessionRunning(sessionID)
}

func (feishuSessionMessenger) EnqueueMessage(sessionID, message string) error {
	service.EnqueueMessage(sessionID, model.QueuedMessage{
		Text:      message,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	return nil
}

func (feishuSessionMessenger) ClearQueue(sessionID string) {
	service.ClearQueue(sessionID)
}

func (feishuSessionMessenger) SendMessageToSession(sessionID, message string) error {
	return service.SendMessageToSessionFromFeishu(sessionID, message)
}

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

	// Root --version flag
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version.Get())
		os.Exit(0)
	}

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
		fmt.Println("  --port PORT       Server port (overrides config file, default: 20000)")
		fmt.Println("  --data-dir DIR    Runtime data directory (default: ~/.clawbench)")
		fmt.Println("  --version         Print version and exit")
		os.Exit(0)
	}

	// Parse --data-dir early (before subcommand dispatch) so CLI subcommands
	// can find cookie-token in the correct data directory.
	for i, arg := range os.Args[1:] {
		if arg == "--data-dir" && i+1 < len(os.Args[1:]) {
			absDataDir, err := filepath.Abs(os.Args[i+2])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid --data-dir path: %v\n", err)
				os.Exit(1)
			}
			model.DataDir = absDataDir
		}
	}

	// Task subcommand dispatch (e.g., "clawbench task create --name ...")
	if len(os.Args) > 1 && os.Args[1] == "task" {
		os.Exit(cli.RunTaskCommand(os.Args[2:]))
	}

	// RAG subcommand dispatch (e.g., "clawbench rag search -q ...")
	if len(os.Args) > 1 && os.Args[1] == "rag" {
		os.Exit(cli.RunRAGCommand(os.Args[2:]))
	}

	// Upgrade-replace subcommand dispatch (launched by upgrade service)
	if len(os.Args) > 1 && os.Args[1] == "upgrade-replace" {
		os.Exit(cli.RunUpgradeReplaceCommand(os.Args[2:]))
	}

	// Parse CLI flags
	cliPort := 0
	cliDataDir := ""
	for i, arg := range os.Args[1:] {
		if arg == "--port" && i+1 < len(os.Args[1:]) {
			if p, err := strconv.Atoi(os.Args[i+2]); err == nil && p > 0 && p <= 65535 {
				cliPort = p
			}
		}
		if arg == "--data-dir" && i+1 < len(os.Args[1:]) {
			cliDataDir = os.Args[i+2]
		}
	}

	// Determine binary directory for config search path
	absBinPath, _ := filepath.Abs(os.Args[0])
	model.BinDir = filepath.Dir(absBinPath)

	// Set data directory: --data-dir flag > default ~/.clawbench
	if cliDataDir != "" {
		absDataDir, err := filepath.Abs(cliDataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --data-dir path: %v\n", err)
			os.Exit(1)
		}
		model.DataDir = absDataDir
	} else {
		homeDir := platform.UserHomeDir()
		if homeDir == "" {
			fmt.Fprintf(os.Stderr, "Error: cannot determine home directory (set $HOME or $USERPROFILE)\n")
			os.Exit(1)
		}
		model.DataDir = filepath.Join(homeDir, ".clawbench")
	}

	// Warn about legacy BinDir layout if detected
	if cliDataDir == "" {
		startup.CheckLegacyLayout(model.BinDir, model.DataDir)
	}

	// Load configuration — config/config.yaml is optional
	var cfg model.Config
	var presence map[string]bool
	// Legacy values extracted from the raw map for migration (summarize.tts_model
	// / tts_api were removed from the typed struct).
	var legacySummaryBaseURL, legacySummaryKey, legacySummaryModel, legacySummaryFormat string

	// Search for config in priority order:
	// 1. <DataDir>/config/config.yaml (data directory)
	// 2. config/config.yaml (CWD-relative, standard layout)
	configPath := cli.FindConfigPath(model.DataDir)

	data, err := os.ReadFile(configPath)
	if err == nil {
		// Parse into raw map first for presence detection (bool defaults)
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse %s: %v\n", configPath, err)
			os.Exit(1)
		}
		presence = model.ParsePresenceMap(raw)

		// Capture legacy summarize model/API config for migration.
		if apiMap, ok := raw["summarize"].(map[string]any); ok {
			if legacyBase, ok := apiMap["tts_api"].(map[string]any); ok {
				if b, ok := legacyBase["base_url"].(string); ok && b != "" {
					legacySummaryBaseURL = b
				}
				if k, ok := legacyBase["key"].(string); ok && k != "" {
					legacySummaryKey = k
				}
				if f, ok := legacyBase["format"].(string); ok && f != "" {
					legacySummaryFormat = f
				}
			}
			if m, ok := apiMap["tts_model"].(string); ok && m != "" {
				legacySummaryModel = m
			}
		}

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

	// Migrate legacy TTS summary model config (summarize.tts_model / tts_api)
	// into the new shared ai_summary section so existing configs keep working.
	if cfg.AISummary.API.BaseURL == "" && legacySummaryBaseURL != "" {
		cfg.AISummary.API.BaseURL = legacySummaryBaseURL
		cfg.AISummary.API.Key = legacySummaryKey
	}
	if cfg.AISummary.Model == "" {
		cfg.AISummary.Model = legacySummaryModel
	}
	// Migrate the API format too, so anthropic endpoints are not defaulted to
	// "openai" (which would cause the recommendation/summary call to fail).
	if cfg.AISummary.Format == "" {
		cfg.AISummary.Format = legacySummaryFormat
	}

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
	model.LocalhostAuthExempt = cfg.LocalhostAuthExempt

	// Apply TTS text processing config (defaults applied in ApplyDefaults)
	summarize.InlineCodeMaxLen = cfg.TTS.InlineCodeMaxLen
	summarize.MaxSummarizeRunes = cfg.TTS.MaxSummarizeRunes

	// NOTE: TTS summarizer initialization is deferred until after DB init.

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
		if cfg.TTS.Voice != "" {
			m.Voice = cfg.TTS.Voice
		}
		ttsProvider = m
		slog.Info(
			"tts provider configured",
			slog.String("engine", "moss-nano"),
			slog.String("backend", m.Backend),
			slog.String("model_dir", m.ModelDir),
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

	// Initialize STT (speech-to-text) provider from config
	sttProvider := newSTTProvider(cfg)
	handler.SetSTTProvider(sttProvider)
	slog.Info(
		"stt provider configured",
		slog.String("base_url", cfg.STT.BaseURL),
		slog.String("model", cfg.STT.Model),
		slog.Bool("streaming", cfg.STT.Streaming),
	)

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
			slog.String("file", filepath.Join(model.DataDir, "auto-password")),
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
	ai.SetRegistryDB(service.WriteDB())
	ai.GetAgentCapabilityRegistry().LoadFromDB(service.ReadDB())

	// Kill orphan AI subprocesses from a previous server crash.
	// On Linux, scans /proc for CLAWBENCH_CHILD=1 env marker.
	ai.CleanupOrphans()

	// Inject external session ID getter for ResumeSession recovery
	ai.SetExternalSessionIDGetter(service.GetExternalSessionID)

	// Inject auto-approve getter for ACP permission auto-approval
	ai.SetAutoApproveGetter(service.GetSessionAutoApprove)

	// Inject session running checker for ACP idle sweep (avoids import cycle)
	ai.GetACPConnManager().SetSessionRunningChecker(service.IsSessionRunning)

	// Inject permission state change callback (emits WS event on approval state change)
	ai.SetPermissionStateChangeCallback(func(clawbenchSID string, pending bool, toolName string, toolInput string) {
		status := "permission_resolved"
		if pending {
			status = "permission_pending"
		}
		service.EmitSessionEvent(clawbenchSID, status, false, toolName, toolInput)
	})

	// Initialize TTS summarizer from config (deferred from earlier — needs DB for API key resolution).
	// Language is now per-request (sent from frontend), not configured at startup.
	ttsBackend := cfg.Summarize.TTSBackend

	var ttsSummarizer summarize.Summarizer
	switch ttsBackend {
	case "", summarizeBackendSimple:
		ttsSummarizer = summarize.NewSimple()
		slog.Info("tts summarizer configured", slog.String("backend", summarizeBackendSimple))
	case summarizeBackendAPI:
		ttsSummarizer = buildAISummarizer(cfg)
		if ttsSummarizer == nil {
			slog.Error("summarize.tts_backend is \"api\" but ai_summary.api.base_url is not configured")
			os.Exit(1)
		}
	}
	handler.SetSummarizer(ttsSummarizer)

	// Initialize RAG history memory system
	if err := rag.Init(cfg.RAG); err != nil {
		slog.Warn("failed to initialize RAG system, search will be limited", slog.String("err", err.Error()))
	}
	defer rag.Shutdown()
	defer service.StopSessionCleanupWorker()

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

	// If port was overridden and DevPort was auto-calculated from the original port,
	// recalculate DevPort to match the new port.
	if port != cfg.Port && cfg.DevPort == cfg.Port+2 {
		cfg.DevPort = port + 2
	}
	cfg.Port = port

	// Set global port for cookie name scoping (multi-instance on same hostname)
	model.ServerPort = port

	// Load agent configurations (set ClawbenchBin first for placeholder replacement)
	model.ClawbenchBin = absBinPath

	// 1. Detect installed CLIs and write new agents to DB
	model.SyncDiscoverAgentsDB(service.WriteDB())

	// 1a. Load manually-defined agents from config/agents/*.yaml (e.g., acp-mock for E2E)
	model.LoadYamlAgents(service.WriteDB(), filepath.Dir(configPath))

	// 2. Synchronous model discovery (run when agents may have empty model lists)
	discoveredModels := model.SyncDiscoverModels()

	// 2a. Migrate custom_system_prompt BEFORE LoadAgentsIntoMemory so the
	// composition logic (commonPrompt + customSystemPrompt) works correctly
	// on first startup with legacy system_prompt data.
	service.MigrateCustomSystemPrompt()

	// 3. Merge runtime data: fill models/levels from discovery results/registry, reload memory
	model.MergeDiscoveredDataDB(service.WriteDB(), discoveredModels)

	slog.Info("agents loaded", slog.Int("count", len(model.AgentList)))

	// 4. Async: refresh model cache in background (non-blocking)
	model.AsyncRefreshModelCache(service.WriteDB())

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
			service.CleanupPendingEvents()
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

	// Start cleanup worker for archived data
	rag.StartCleanupWorker(cfg.RAG)

	// Set RAG chunk purge callback for session cleanup worker
	service.SetPurgeRAGChunksFn(func(sessionIDs []string) (int64, error) {
		if rag.GlobalStore == nil {
			return 0, nil
		}
		return rag.GlobalStore.DeleteChunksBySessionIDs(sessionIDs)
	})

	// Start session archive cleanup worker
	service.StartSessionCleanupWorker(cfg)

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

	// Initialize FRP tunnel (Fast Reverse Proxy for remote access from Android).
	// FRP is disabled by default; requires user-provided frps server.
	// The frp client runs in-process as a Go library — no external binary needed.
	var frpManagerRef *frp.Manager
	var frpStatus frp.Status
	if cfg.FRP.Enabled && cfg.FRP.ServerAddr != "" {
		sshPort := 0
		if sshServerRef != nil {
			sshPort = sshServerRef.Port()
		}
		mgr := frp.NewManager(cfg.FRP, port, sshPort)
		if err := mgr.Start(); err != nil {
			slog.Warn("FRP failed to start", slog.String("err", err.Error()))
			cfg.FRP.Enabled = false
		} else {
			frpManagerRef = mgr
			defer mgr.Stop()

			select {
			case frpStatus = <-mgr.OnReady():
				slog.Info("FRP tunnel enabled",
					slog.String("server", cfg.FRP.ServerAddr),
					slog.Int("remotePort", frpStatus.RemotePort),
				)
			case <-time.After(30 * time.Second):
				slog.Warn("FRP port allocation timeout")
			}
		}
	} else if cfg.FRP.Enabled {
		slog.Warn("FRP enabled but server_addr not configured, disabling")
		cfg.FRP.Enabled = false
	}
	handler.SetFRPManager(frpManagerRef, cfg.FRP.Enabled)

	// Initialize DingTalk push notifications (enterprise internal bot via Stream API).
	// DingTalk is disabled by default; requires app_key + app_secret configuration.
	// DB adapter and session messenger are always registered (needed for hot-reload enable/disable).
	dingtalk.RegisterDBAdapter(&dingtalkDBAdapter{})
	dingtalk.RegisterSessionMessenger(&dingtalkSessionMessenger{})
	if cfg.PushMode == "dingtalk" && cfg.DingTalk.AppKey != "" && cfg.DingTalk.AppSecret != "" {
		dingtalkMgr := dingtalk.NewManager(&cfg.DingTalk)
		dingtalk.SetManager(dingtalkMgr)
		if err := dingtalkMgr.Start(); err != nil {
			slog.Warn("DingTalk push failed to start", slog.String("err", err.Error()))
		} else {
			slog.Info("DingTalk push enabled")
		}
	}
	defer func() {
		if mgr := dingtalk.GetManager(); mgr != nil {
			mgr.Stop()
		}
	}()

	// Initialize Feishu push notifications (enterprise internal bot via WebSocket).
	// Feishu is disabled by default; requires app_id + app_secret configuration.
	feishu.RegisterDBAdapter(&feishuDBAdapter{})
	feishu.RegisterSessionMessenger(&feishuSessionMessenger{})
	if cfg.PushMode == "feishu" && cfg.Feishu.AppID != "" && cfg.Feishu.AppSecret != "" {
		feishuMgr := feishu.NewManager(&cfg.Feishu)
		feishu.SetManager(feishuMgr)
		if err := feishuMgr.Start(); err != nil {
			slog.Warn("Feishu push failed to start", slog.String("err", err.Error()))
		} else {
			slog.Info("Feishu push enabled")
		}
	}
	defer func() {
		if mgr := feishu.GetManager(); mgr != nil {
			mgr.Stop()
		}
	}()

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
	ws.InitManager()
	dingtalk.RegisterClientChecker(ws.GetManager())
	feishu.RegisterClientChecker(ws.GetManager())

	// Initialize cluster worker (on-demand, no cron) — must be after ws.InitManager()
	mgr := ws.GetManager()
	if mgr != nil {
		rag.StartClusterWorker(mgr.StreamHub())
	}

	// Register WS chat stream callbacks (breaks import cycle between ws and service)
	ws.OnSubscribe = func(mgr *ws.Manager, clientID, sessionID string) {
		hub := mgr.StreamHub()
		hub.EmitACPStateEvents(clientID, sessionID)
		if service.IsSessionRunning(sessionID) {
			if msgID := service.GetStreamingMessageID(sessionID); msgID > 0 {
				hub.EmitStreamStartEvent(clientID, sessionID, msgID)
			}
		}
	}
	ws.OnCancelSession = service.CancelSession
	ws.OnPermissionRespond = service.RespondPermission

	// Inject DB context state usage fallback into StreamHub (breaks import cycle).
	// Both UsageStatePersist and ContextStateUsage are type aliases of ai.UsageState,
	// so direct assignment is safe — no manual field mapping needed.
	ws.GetManager().StreamHub().SetGetContextStateUsageFunc(func(sessionID string) *ws.ContextStateUsage {
		ctxState := service.GetContextState(sessionID)
		if ctxState == nil || ctxState.Usage == nil {
			return nil
		}
		return ctxState.Usage
	})

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Wire up the restart function for POST /api/config/restart
	// The sentinel process approach: launch a watcher that starts a new process
	// after this one exits, then trigger graceful shutdown.
	handler.SetRestartFunc(makeRestartFunc(selfSignalInterrupt))

	// Wire up the upgrade service functions
	// upgradeShutdownFunc: just graceful shutdown, no sentinel.
	// The upgrade-replace subprocess handles restarting after replacing the binary.
	service.SetUpgradeShutdownFunc(selfSignalInterrupt)
	service.SetUpgradeIsSupervised(handler.IsRunningUnderSupervisor)

	// Clean up stale temp directories from previous upgrade attempts
	service.CleanStaleUpgradeTempDirs()

	// Wire up the hot-reload reconfigure function for config PATCH.
	// Called by applyHotReloadGlobals() after each successful patch.
	handler.SetReconfigureFunc(func() { hotReloadReconfigure(port) })

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
		LocalIPs:        platform.GetLocalIPs(),
		AutoPassword:    autoPassword,
		DataDir:         model.DataDir,
		Agents:          agentInfos,
		SSHEnabled:      sshEnabled,
		SSHPort:         sshPort,
		TTSEngine:       engine,
		RAGAvailable:    rag.GlobalStore != nil,
		TerminalOn:      cfg.Terminal.Enabled,
		TaskCount:       taskCount,
		StartupDuration: time.Since(startTime),
		FRPEnabled:      cfg.FRP.Enabled,
		FRPRemoteURL:    frpStatus.RemoteURL,
		FRPServerAddr:   cfg.FRP.ServerAddr,
		FRPRemotePort:   frpStatus.RemotePort,
		FrontendMode:    frontend.ModeLabel(),
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

// hotReloadReconfigure is called by applyHotReloadGlobals() after a successful
// config PATCH to reconfigure subsystems that support hot-reload.
// It recreates TTS provider, TTS summarizer, and reconfigures terminal.
func hotReloadReconfigure(port int) {
	cfg := model.ConfigInstance

	// --- TTS: recreate speech provider if engine or sub-config changed ---
	ttsProvider := newTTSProvider(cfg)
	handler.SetSpeechProvider(ttsProvider)
	slog.Info("hot-reload: TTS provider reconfigured", slog.String("engine", cfg.TTS.Engine))

	// --- STT: recreate provider on config change ---
	sttProvider := newSTTProvider(cfg)
	handler.SetSTTProvider(sttProvider)
	slog.Info("hot-reload: STT provider reconfigured", slog.String("base_url", cfg.STT.BaseURL))

	// --- Summarize: reconstruct TTS summarizer ---
	ttsSummarizer := newTTSSummarizer(cfg)
	handler.SetSummarizer(ttsSummarizer)

	// --- Terminal: reconfigure or toggle enabled ---
	hotReloadTerminal(cfg, port)

	// --- SSH/Port-Forward: reconfigure or toggle enabled ---
	hotReloadSSH(cfg, port)

	// --- RAG: reconfigure embedder, indexer, cleanup worker ---
	rag.Reconfigure(cfg.RAG)

	// --- Session cleanup: reconfigure archive retention worker ---
	service.ReconfigureSessionCleanup(cfg)

	// --- FRP: reconfigure or toggle enabled ---
	hotReloadFRP(cfg, port)

	// --- DingTalk: reconfigure or toggle enabled ---
	hotReloadDingTalk(cfg)
	hotReloadFeishu(cfg)
}

// hotReloadDingTalk reconfigures or toggles the DingTalk push subsystem on hot-reload.
func hotReloadDingTalk(cfg model.Config) {
	mgr := dingtalk.GetManager()

	if cfg.PushMode == "dingtalk" && cfg.DingTalk.AppKey != "" && cfg.DingTalk.AppSecret != "" {
		if mgr != nil {
			// Manager is running — try in-place reconfigure
			result := mgr.Reconfigure(&cfg.DingTalk)
			if result.NeedsRestart {
				// Credentials or enabled changed — stop old + start new
				mgr.Stop()
				dingtalk.SetManager(nil)
				newMgr := dingtalk.NewManager(&cfg.DingTalk)
				if err := newMgr.Start(); err != nil {
					slog.Warn("hot-reload: DingTalk restart failed", slog.String("err", err.Error()))
					handler.AddHotReloadWarning(fmt.Sprintf("DingTalk: %s", err.Error()))
				} else {
					dingtalk.SetManager(newMgr)
					slog.Info("hot-reload: DingTalk restarted (credentials changed)")
				}
			} else {
				slog.Info("hot-reload: DingTalk reconfigured (in-place update)")
			}
		} else {
			// DingTalk was disabled, now enabled — create new Manager
			newMgr := dingtalk.NewManager(&cfg.DingTalk)
			if err := newMgr.Start(); err != nil {
				slog.Warn("hot-reload: DingTalk failed to start", slog.String("err", err.Error()))
				handler.AddHotReloadWarning(fmt.Sprintf("DingTalk: %s", err.Error()))
			} else {
				dingtalk.SetManager(newMgr)
				slog.Info("hot-reload: DingTalk enabled")
			}
		}
	} else {
		// DingTalk should be disabled
		if mgr != nil {
			mgr.Stop()
			dingtalk.SetManager(nil)
			slog.Info("hot-reload: DingTalk disabled")
		}
	}
}

// hotReloadFeishu reconfigures or toggles the Feishu push subsystem on hot-reload.
func hotReloadFeishu(cfg model.Config) {
	mgr := feishu.GetManager()

	if cfg.PushMode == "feishu" && cfg.Feishu.AppID != "" && cfg.Feishu.AppSecret != "" {
		if mgr != nil {
			// Manager is running — try in-place reconfigure
			result := mgr.Reconfigure(&cfg.Feishu)
			if result.NeedsRestart {
				// Credentials or enabled changed — stop old + start new
				mgr.Stop()
				feishu.SetManager(nil)
				newMgr := feishu.NewManager(&cfg.Feishu)
				if err := newMgr.Start(); err != nil {
					slog.Warn("hot-reload: Feishu restart failed", slog.String("err", err.Error()))
					handler.AddHotReloadWarning(fmt.Sprintf("Feishu: %s", err.Error()))
				} else {
					feishu.SetManager(newMgr)
					slog.Info("hot-reload: Feishu restarted (credentials changed)")
				}
			} else {
				slog.Info("hot-reload: Feishu reconfigured (in-place update)")
			}
		} else {
			// Feishu was disabled, now enabled — create new Manager
			newMgr := feishu.NewManager(&cfg.Feishu)
			if err := newMgr.Start(); err != nil {
				slog.Warn("hot-reload: Feishu failed to start", slog.String("err", err.Error()))
				handler.AddHotReloadWarning(fmt.Sprintf("Feishu: %s", err.Error()))
			} else {
				feishu.SetManager(newMgr)
				slog.Info("hot-reload: Feishu enabled")
			}
		}
	} else {
		// Feishu should be disabled
		if mgr != nil {
			mgr.Stop()
			feishu.SetManager(nil)
			slog.Info("hot-reload: Feishu disabled")
		}
	}
}

// newTTSProvider creates a SpeechProvider from TTS config.
func newTTSProvider(cfg model.Config) speech.SpeechProvider {
	switch cfg.TTS.Engine {
	case "edge":
		return newEdgeTTSProvider(cfg)
	case "piper":
		return newPiperTTSProvider(cfg)
	case "kokoro":
		return newKokoroTTSProvider(cfg)
	case "moss-nano":
		return newMossNanoTTSProvider(cfg)
	default:
		return newEdgeTTSProvider(cfg)
	}
}

func newEdgeTTSProvider(cfg model.Config) *speech.EdgeTTSProvider {
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
	return p
}

func newPiperTTSProvider(cfg model.Config) *speech.PiperProvider {
	p := speech.NewPiperProvider()
	p.ModelPath = speech.ResolveModelPath(cfg.TTS.Voice, cfg.TTS.Piper.ModelPath)
	if cfg.TTS.Piper.NoiseScale > 0 {
		p.NoiseScale = cfg.TTS.Piper.NoiseScale
	}
	if cfg.TTS.Piper.LengthScale > 0 {
		p.LengthScale = cfg.TTS.Piper.LengthScale
	} else if cfg.TTS.Speed > 0 {
		p.LengthScale = 1.0 / cfg.TTS.Speed
	}
	if cfg.TTS.Piper.SentenceSilence > 0 {
		p.SentenceSilence = cfg.TTS.Piper.SentenceSilence
	}
	return p
}

func newKokoroTTSProvider(cfg model.Config) *speech.KokoroProvider {
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
	return k
}

func newMossNanoTTSProvider(cfg model.Config) *speech.MossNanoProvider {
	m := speech.NewMossNanoProvider()
	if cfg.TTS.MossNano.Backend != "" {
		m.Backend = cfg.TTS.MossNano.Backend
	}
	m.ModelDir = speech.ResolveMossNanoModelDir(cfg.TTS.MossNano.ModelDir)
	if cfg.TTS.Voice != "" {
		m.Voice = cfg.TTS.Voice
	}
	return m
}

// buildAISummarizer creates an LLM summarizer from the shared ai_summary config.
// Returns nil if ai_summary.api.base_url is empty.
func buildAISummarizer(cfg model.Config) summarize.Summarizer {
	if cfg.AISummary.API.BaseURL == "" {
		return nil
	}
	s := summarize.NewAISummarizer(cfg.AISummary)
	slog.Info("ai summarizer configured", slog.String("format", cfg.AISummary.Format), slog.String("base_url", cfg.AISummary.API.BaseURL))
	return s
}

// newTTSSummarizer creates a TTS summarizer from config for hot-reload.
func newTTSSummarizer(cfg model.Config) summarize.Summarizer {
	switch cfg.Summarize.TTSBackend {
	case "", summarizeBackendSimple:
		return summarize.NewSimple()
	case summarizeBackendAPI:
		s := buildAISummarizer(cfg)
		if s == nil {
			slog.Warn("hot-reload: summarize.tts_backend is \"api\" but ai_summary.api.base_url is empty, falling back to simple")
			return summarize.NewSimple()
		}
		return s
	default:
		slog.Warn("hot-reload: unsupported tts_backend, falling back to simple", slog.String("backend", cfg.Summarize.TTSBackend))
		return summarize.NewSimple()
	}
}

// hotReloadSSH reconfigures or toggles the SSH tunnel / port-forward server on hot-reload.
func hotReloadSSH(cfg model.Config, port int) {
	sshRef := handler.GetSSHServer()

	if cfg.PortForward.Enabled {
		if sshRef != nil {
			// SSH is running — check if port changed
			newPort := cfg.PortForward.Port
			if newPort == 0 {
				newPort = port + 1
			}
			if sshRef.Port() != newPort {
				// Port changed — close old server, start new one
				sshRef.Close()
				newSrv := ssh.NewServer(cfg.PortForward, port, cfg.Password, service.ProxyService)
				handler.SetSSHServer(newSrv)
				go func() {
					if err := newSrv.ListenAndServe(); err != nil {
						slog.Error("SSH server failed", slog.String("err", err.Error()))
					}
				}()
				slog.Info("hot-reload: SSH tunnel restarted on new port", slog.Int("port", newPort))
			} else {
				// Port unchanged — just update allowed ports if ProxyService exists
				if service.ProxyService != nil {
					service.ProxyService.SetAllowedPorts(cfg.PortForward.AllowedPorts)
				}
				slog.Info("hot-reload: SSH tunnel reconfigured (allowed_ports)")
			}
		} else {
			// SSH was disabled, now enabled — create ProxyRegistry + SSH server
			proxySvc := service.NewProxyRegistry(port)
			proxySvc.SetAllowedPorts(cfg.PortForward.AllowedPorts)
			service.ProxyService = proxySvc

			newSrv := ssh.NewServer(cfg.PortForward, port, cfg.Password, proxySvc)
			handler.SetSSHServer(newSrv)
			go func() {
				if err := newSrv.ListenAndServe(); err != nil {
					slog.Error("SSH server failed", slog.String("err", err.Error()))
					// Clean up: SSH failed, stop the proxy registry we just created
					proxySvc.Stop()
					service.ProxyService = nil
					handler.SetSSHServer(nil)
				}
			}()
			slog.Info("hot-reload: SSH tunnel enabled")
		}
	} else {
		// SSH should be disabled
		if sshRef != nil {
			sshRef.Close()
			handler.SetSSHServer(nil)
			if service.ProxyService != nil {
				service.ProxyService.Stop()
				service.ProxyService = nil
			}
			slog.Info("hot-reload: SSH tunnel disabled")
		}
	}
}

// hotReloadTerminal reconfigures or toggles the terminal subsystem on hot-reload.
func hotReloadTerminal(cfg model.Config, port int) {
	if cfg.Terminal.Enabled {
		mgr := handler.GetTerminalManager()
		if mgr != nil {
			mgr.Reconfigure(cfg.Terminal)
			slog.Info("hot-reload: terminal reconfigured")
		} else {
			// Terminal was disabled, now enabled — create new Manager
			terminalMgr := terminal.NewManager(cfg.Terminal, port)
			handler.SetTerminalManager(terminalMgr)
			slog.Info("hot-reload: terminal enabled")
		}
	} else {
		mgr := handler.GetTerminalManager()
		if mgr != nil {
			mgr.CloseAllSessions()
			handler.SetTerminalManager(nil)
			slog.Info("hot-reload: terminal disabled")
		}
	}
}

// hotReloadFRP reconfigures or toggles the FRP tunnel on hot-reload.
func hotReloadFRP(cfg model.Config, port int) {
	mgr := handler.GetFRPManager()

	// Determine SSH port for FRP proxy
	sshPort := 0
	if sshRef := handler.GetSSHServer(); sshRef != nil {
		sshPort = sshRef.Port()
	}

	if cfg.FRP.Enabled && cfg.FRP.ServerAddr != "" {
		if mgr != nil {
			// FRP is running — try in-place reconfigure
			needsRestart, err := mgr.Reconfigure(cfg.FRP, port, sshPort)
			if err != nil {
				slog.Warn("hot-reload: FRP reconfigure failed", slog.String("err", err.Error()))
				return
			}
			if needsRestart {
				// Common config changed (server_addr/port/token) — restart service
				slog.Info("hot-reload: FRP common config changed, restarting")
				mgr.Stop()
				newMgr := frp.NewManager(cfg.FRP, port, sshPort)
				if err := newMgr.Start(); err != nil {
					slog.Warn("hot-reload: FRP restart failed", slog.String("err", err.Error()))
					handler.SetFRPManager(nil, false)
				} else {
					handler.SetFRPManager(newMgr, true)
					slog.Info("hot-reload: FRP restarted")
				}
			} else {
				slog.Info("hot-reload: FRP reconfigured (proxy only)")
			}
		} else {
			// FRP was disabled, now enabled — create new Manager
			newMgr := frp.NewManager(cfg.FRP, port, sshPort)
			if err := newMgr.Start(); err != nil {
				slog.Warn("hot-reload: FRP failed to start", slog.String("err", err.Error()))
			} else {
				handler.SetFRPManager(newMgr, true)
				slog.Info("hot-reload: FRP enabled")
			}
		}
	} else {
		// FRP should be disabled
		if mgr != nil {
			mgr.Stop()
			handler.SetFRPManager(nil, false)
			slog.Info("hot-reload: FRP disabled")
		}
	}
}

// newSTTProvider builds the STT provider from config.
func newSTTProvider(cfg model.Config) stt.STTProvider {
	return stt.NewVLLMProvider(cfg.STT.BaseURL, cfg.STT.Model, cfg.STT.APIKey, cfg.STT.Language)
}
