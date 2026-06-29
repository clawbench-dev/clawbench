package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// IsSHA256Password returns true if the password field contains a SHA-256
// hashed value (prefixed with "sha256:"). These passwords are stored as
// SHA-256(password + "clawbench-salt") and cannot be reversed to plaintext.
func IsSHA256Password(password string) bool {
	return strings.HasPrefix(password, "sha256:")
}

// ParseSHA256Hash extracts the hex hash from a "sha256:<hex>" formatted password.
// Returns empty string if the format is invalid or not a SHA-256 password.
func ParseSHA256Hash(password string) string {
	if !IsSHA256Password(password) {
		return ""
	}
	hash := strings.TrimPrefix(password, "sha256:")
	if len(hash) != 64 { // SHA-256 hex is 64 chars
		return ""
	}
	return hash
}

// Config holds the application configuration.
type Config struct {
	Port                int    `yaml:"port"`
	Host                string `yaml:"host"`      // Bind address (empty = 0.0.0.0, "localhost" = 127.0.0.1 only)
	LogLevel            string `yaml:"log_level"` // Log level: "debug", "info", "warn", "error" (default: "info")
	Password            string `yaml:"password"`
	DefaultAgent        string `yaml:"default_agent"`
	LogDir              string `yaml:"log_dir"`
	LocalhostAuthExempt bool   `yaml:"localhost_auth_exempt"` // true = localhost bypasses auth (default)
	LogMaxDays          int    `yaml:"log_max_days"`
	TLS                 struct {
		Enabled  bool   `yaml:"enabled"`
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	} `yaml:"tls"`
	DevPort int `yaml:"dev_port"` // Localhost-only HTTP port for dev proxy (0 = auto=Port+2 when TLS enabled, -1 = disabled)
	Upload  struct {
		MaxSizeMB int `yaml:"max_size_mb"` // Maximum file upload size in MB (default: 100)
		MaxFiles  int `yaml:"max_files"`   // Maximum number of files per upload (default: 20)
	} `yaml:"upload"`
	Chat struct {
		InitialMessages      int `yaml:"initial_messages"`       // Number of messages to load initially (default: 20)
		PageSize             int `yaml:"page_size"`              // Number of messages per lazy-load batch (default: 20)
		SessionPageSize      int `yaml:"session_page_size"`      // Number of sessions per page in session list (default: 10)
		SystemPromptInterval int `yaml:"system_prompt_interval"` // Re-inject system prompt every N assistant turns (0=never, default: 10)
	} `yaml:"chat"`
	Session struct {
		MaxCount int `yaml:"max_count"` // Maximum number of chat sessions per project (default: 10)
	} `yaml:"session"`
	RecentProjects struct {
		MaxCount int `yaml:"max_count"` // Maximum number of recent projects to keep (default: 10)
	} `yaml:"recent_projects"`
	TTS struct {
		Engine            string         `yaml:"engine"`              // TTS engine: "edge" (default), "piper", "kokoro", "moss-nano"
		TTSModel          string         `yaml:"tts_model"`           // TTS model for speech synthesis (default: "Speech-2.8-Turbo")
		Voice             string         `yaml:"voice"`               // Voice ID for TTS (default: "female-chengshu")
		Speed             float64        `yaml:"speed"`               // Speech speed multiplier (default: 1.0)
		Format            string         `yaml:"format"`              // Audio output format (default: "mp3")
		InlineCodeMaxLen  int            `yaml:"inline_code_max_len"` // Max inline code content length (runes) to preserve for TTS; longer code is removed (default: 100)
		MaxSummarizeRunes int            `yaml:"max_summarize_runes"` // Max runes for summarization input; longer text is truncated (default: 10000, simple mode: 1000)
		MaxCacheFiles     int            `yaml:"max_cache_files"`     // Max cached TTS audio files to keep; oldest are auto-deleted (0=unlimited, default: 100)
		Piper             PiperConfig    `yaml:"piper"`               // Piper-specific configuration (only used when engine: "piper")
		Kokoro            KokoroConfig   `yaml:"kokoro"`              // Kokoro-specific configuration (only used when engine: "kokoro")
		MossNano          MossNanoConfig `yaml:"moss_nano"`           // MOSS-TTS-Nano-specific configuration (only used when engine: "moss-nano")
	} `yaml:"tts"`
	Summarize   SummarizeConfig   `yaml:"summarize"`    // Shared summarization configuration (TTS + Tasks)
	Proxy       ProxyConfig       `yaml:"proxy"`        // Legacy: kept for backward-compatible YAML reading
	PortForward PortForwardConfig `yaml:"port_forward"` // SSH tunnel server + port forwarding configuration
	RAG         RAGConfig         `yaml:"rag"`          // RAG history memory configuration
	Terminal    TerminalConfig    `yaml:"terminal"`     // Interactive web terminal configuration
	Push        PushConfig        `yaml:"push"`         // Push notification configuration
}

// TerminalConfig holds configuration for the interactive web terminal.
type TerminalConfig struct {
	Enabled      bool   `yaml:"enabled"`        // Enable interactive terminal (default: true)
	IdleTimeout  string `yaml:"idle_timeout"`   // Close PTY after no WS connections for this duration (default: "0" = never timeout)
	BufferLines  int    `yaml:"buffer_lines"`   // Replay buffer line count (default: 2000)
	MaxLineBytes int    `yaml:"max_line_bytes"` // Per-line byte cap to prevent memory bloat (default: 65536 = 64KB)
	MaxBufferMB  int    `yaml:"max_buffer_mb"`  // Total buffer memory cap in MB (default: 4)
	MaxSessions  int    `yaml:"max_sessions"`   // Max concurrent terminal sessions (default: 10)
}

// SummarizeConfig holds unified summarization configuration shared by TTS and scheduled tasks.
type SummarizeConfig struct {
	Backend     string    `yaml:"backend"`      // Summarization backend: "" (disabled), "simple" (extract final answer), "api", "claude", "codebuddy", etc.
	Model       string    `yaml:"model"`        // Model for summarization (empty = backend default)
	ChatSummary *bool     `yaml:"chat_summary"` // Enable auto-summarization for chat messages (default: true, nil = true)
	API         APIConfig `yaml:"api"`          // API-based summarization (used when backend is "api")
}

// IsChatSummaryEnabled returns whether chat message auto-summarization is enabled.
// Defaults to true when ChatSummary is nil (not explicitly set).
func (s SummarizeConfig) IsChatSummaryEnabled() bool {
	if s.ChatSummary == nil {
		return true
	}
	return *s.ChatSummary
}

// PushConfig holds configuration for push notifications.
type PushConfig struct {
	JPush JPushConfig `yaml:"jpush"`
}

// JPushConfig holds configuration for the JPush push notification service.
type JPushConfig struct {
	Enabled      bool   `yaml:"enabled"`
	AppKey       string `yaml:"app_key"`
	MasterSecret string `yaml:"master_secret"`
}

// RAGConfig holds configuration for the RAG history memory system.
// RAG is always enabled. When the embedding API is unavailable, falls back to BM25 full-text search.
type RAGConfig struct {
	BaseURL        string `yaml:"base_url"`         // OpenAI-compatible API base URL (default: "http://localhost:11434")
	Model          string `yaml:"model"`            // Embedding model name (default: "bge-m3")
	APIKey         string `yaml:"api_key"`          // API key for the embedding service (optional, for cloud providers)
	OllamaBaseURL  string `yaml:"ollama_base_url"`  // Deprecated: use base_url
	OllamaModel    string `yaml:"ollama_model"`     // Deprecated: use model
	ChunkSize      int    `yaml:"chunk_size"`       // Chunk size in tokens (default: 512)
	ChunkOverlap   int    `yaml:"chunk_overlap"`    // Overlap between chunks in tokens (default: 64)
	PollInterval   string `yaml:"poll_interval"`    // Indexer poll interval (default: "10s")
	BatchSize      int    `yaml:"batch_size"`       // Messages per indexer batch (default: 10)
	SearchLimit    int    `yaml:"search_limit"`     // Default search result limit (default: 5)
	SearchPoolSize int    `yaml:"search_pool_size"` // Candidates per search source before RRF fusion (default: 20)
	RetentionDays  int    `yaml:"retention_days"`   // Soft-deleted data retention days (0=keep forever, default: 90)
}

// PiperConfig holds configuration for the Piper TTS engine.
type PiperConfig struct {
	ModelPath       string  `yaml:"model_path"`       // Path to .onnx model file (empty = .clawbench/piper-models/<voice>.onnx)
	NoiseScale      float64 `yaml:"noise_scale"`      // Noise scale for sampling (default: 0.667)
	LengthScale     float64 `yaml:"length_scale"`     // Length scale for speech rate (default: 1.0)
	SentenceSilence float64 `yaml:"sentence_silence"` // Silence between sentences in seconds (default: 0.2)
}

// KokoroConfig holds configuration for the Kokoro TTS engine.
type KokoroConfig struct {
	ModelPath  string `yaml:"model_path"`  // Path to kokoro .onnx model file (empty = .clawbench/kokoro-models/kokoro-v1.0.onnx)
	VoicesPath string `yaml:"voices_path"` // Path to voices .bin file (empty = .clawbench/kokoro-models/voices-v1.0.bin)
	Lang       string `yaml:"lang"`        // espeak language code for phonemization (default: "cmn" for Mandarin Chinese)
}

// MossNanoConfig holds configuration for the MOSS-TTS-Nano TTS engine.
type MossNanoConfig struct {
	ModelDir     string `yaml:"model_dir"`     // Directory for ONNX model files (empty = .clawbench/moss-nano-models; CLI auto-downloads if missing)
	PromptSpeech string `yaml:"prompt_speech"` // Path to reference audio for voice cloning (empty = use built-in voice preset)
	Voice        string `yaml:"voice"`         // Built-in voice preset for ONNX backend when no prompt-speech (default: "Junhao")
	Backend      string `yaml:"backend"`       // Inference backend: "onnx" (default, CPU) or "pytorch" (requires GPU)
}

// APIConfig holds configuration for the API-based summarization backend.
type APIConfig struct {
	BaseURL string `yaml:"base_url"` // Full endpoint URL (e.g., "https://api.openai.com/v1/chat/completions")
	Key     string `yaml:"key"`      // API key (sent as Bearer token for OpenAI, x-api-key for Anthropic)
	Format  string `yaml:"format"`   // API format: "openai" (default) or "anthropic"
	AgentID string `yaml:"agent_id"` // Reference to agent whose API key to use (key read from agent_api_keys table at runtime)
}

// ConfigInstance holds the resolved configuration after ApplyDefaults.
// Set once during startup, read-only afterwards.
var ConfigInstance Config

// Global application state
var (
	BinDir              string   // Directory of the running binary
	RootPaths           []string // Filesystem root paths (Linux/macOS: ["/"], Windows: drive list)
	SessionToken        string   // Legacy: stores the password-derived token for "has password" check; NOT used for cookie validation when CookieToken is set
	CookieToken         string   // Cryptographically random session token for cookie validation (ISS-117, ISS-131, ISS-183)
	PasswordHash        []byte   // bcrypt hash for password verification (ISS-003a)
	PasswordIsSHA256    bool     // true when config.yaml stores password as sha256:<hex>
	ServerPort          int      // Server listen port — set once at startup before HTTP listeners start, read-only afterwards. Do NOT modify after server starts; cookie names must be stable.
	SessionCookie       = "clawbench_session"
	DefaultAgentID      string // Default agent for new sessions, set from config or first agent
	LocalhostAuthExempt bool   // When true, localhost requests bypass auth (default)

	// Upload limits (set from config, with defaults)
	UploadMaxSizeMB int // Default: 100
	UploadMaxFiles  int // Default: 20

	// Chat UI config (set from config, with defaults)
	ChatInitialMessages      int // Default: 20
	ChatPageSize             int // Default: 20
	ChatSessionPageSize      int // Default: 10
	ChatSystemPromptInterval int // Re-inject system prompt every N assistant turns (0=never, default: 10)

	// Session limits (set from config, with defaults)
	SessionMaxCount int // Default: 10

	// Recent projects limits (set from config, with defaults)
	RecentProjectsMaxCount int // Default: 10

	// TTS cache limits (set from config, with defaults)
	TTSMaxCacheFiles int // Default: 100; 0 = unlimited
)

// ScopedCookieName returns a port-prefixed cookie name when running on a
// non-default port, so multiple ClawBench instances on the same hostname
// don't collide. Default port 20000 uses unprefixed names for backward
// compatibility; other ports get a "cb{port}_" prefix (e.g. "cb20300_").
func ScopedCookieName(name string) string {
	if ServerPort != 0 && ServerPort != 20000 {
		return fmt.Sprintf("cb%d_%s", ServerPort, name)
	}
	return name
}

// GenerateRandomToken creates a cryptographically random hex token of the
// specified byte length. Used for session cookie tokens to decouple them
// from password hashes. (ISS-117, ISS-131, ISS-183)
func GenerateRandomToken(byteLen int) string {
	b := make([]byte, byteLen)
	// crypto/rand.Read always fills b or returns an error; panic is appropriate
	// for a failure this fundamental (system entropy source unavailable).
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// PersistCookieToken writes the cookie token to .clawbench/cookie-token so it
// survives server restarts. The token is not secret (it's validated via
// constant-time compare), but it should not be readable by other users.
func PersistCookieToken(token string) {
	if BinDir == "" {
		return
	}
	dir := BinDir + "/.clawbench"
	_ = os.MkdirAll(dir, 0o755) // best-effort: if this fails, WriteFile will also fail
	path := dir + "/cookie-token"
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		// Non-fatal: cookie will simply not survive restart; user re-logs in.
		_ = err
	}
}

// LoadCookieToken reads the persisted cookie token from .clawbench/cookie-token.
// Returns empty string if the file does not exist or cannot be read.
func LoadCookieToken() string {
	if BinDir == "" {
		return ""
	}
	data, err := os.ReadFile(BinDir + "/.clawbench/cookie-token")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
