package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

// DefaultForkContextBudget is the default character budget for the conversation
// history re-injected into a forked or rewound session (chat.fork_context_budget).
// It bounds the injected text, not the current prompt, and is deliberately a
// single global value rather than per-model metadata — the model registry
// carries no context-window sizes.
const DefaultForkContextBudget = 100000

// PushModeDisabled is the push_mode value that turns every outbound
// notification channel off. The notification-center row is unaffected: it is
// an in-app log, not a push.
const PushModeDisabled = "disabled"

// DefaultLanguage is the UI language used before the frontend reports one.
// It matches the frontend's fallbackLocale so a fresh server localizes
// background strings the same way the UI would.
const DefaultLanguage = "zh"

// DefaultAutoContinueMaxRetries is how many times an abnormally terminated
// turn is auto-resumed by default (chat.auto_continue_max_retries).
const DefaultAutoContinueMaxRetries = 3

// DefaultRestartResumeWindowMinutes bounds how old an interrupted turn may be
// and still be auto-resumed after a restart (chat.restart_resume_window_minutes).
// One hour: long enough to cover any real restart, short enough that a session
// the user walked away from days ago is not resurrected.
const DefaultRestartResumeWindowMinutes = 60

// AutoContinueUnlimited is the sentinel meaning "no user-configured retry
// limit". The service layer still applies an internal ceiling so a
// deterministic failure cannot spin forever.
const AutoContinueUnlimited = -1

// User-identity limits. They are exported because the settings PATCH handler
// validates against the same numbers the frontend and the prompt builder use.
const (
	// DefaultUserNickname is the fallback display name applied where the
	// nickname is USED (group routing, the settings/meeting UI) when the user
	// never configured one. It is deliberately NOT written into the config: an
	// empty stored value means "not configured", which is distinguishable from
	// a user who explicitly chose this word. It is also the name the
	// group-chat routing parser reserves for the human user, so an agent named
	// "User" is rejected.
	DefaultUserNickname = "User"
	// MaxUserNicknameRunes bounds the nickname. It is written verbatim into the
	// group-routing tag's targets attribute and shown in routing chips, so it
	// must stay short; 32 leaves generous room for CJK names. It also matches
	// the live-share viewer-name cap, so a nickname that is valid here is valid
	// when joining a meeting.
	MaxUserNicknameRunes = 32
	// MaxUserProfileRunes bounds the free-form self-description. It is injected
	// into EVERY agent system prompt, so an unbounded value would be a
	// per-turn cost the user cannot see. 4000 runes is a few paragraphs.
	MaxUserProfileRunes = 4000
)

// UserConfig is the human user's identity (config.yaml `user` section).
type UserConfig struct {
	// Nickname is how agents address the user and how other meeting viewers see
	// them. It must not collide with any agent name and must not contain the
	// group-routing protocol's reserved characters (", <, >, comma).
	Nickname string `yaml:"nickname"`
	// Profile is a free-form self-description ("I am a backend engineer, prefer
	// concise answers…"). Injected into every agent's system prompt so the
	// agent understands the user's role, expertise and preferences.
	Profile string `yaml:"profile"`
}

// Config holds the application configuration.
type Config struct {
	Port         int    `yaml:"port"`
	Host         string `yaml:"host"`      // Bind address (empty = 0.0.0.0, "localhost" = 127.0.0.1 only)
	LogLevel     string `yaml:"log_level"` // Log level: "debug", "info", "warn", "error" (default: "info")
	Password     string `yaml:"password"`
	DefaultAgent string `yaml:"default_agent"`
	// Language is the UI language ("zh" / "en"). It exists so background
	// goroutines with no HTTP request in scope (scheduled tasks, push
	// notifications, auto-continue) can localize the strings they persist.
	// The frontend keeps it in sync on every locale change — see
	// web/src/composables/useSettingsConfig.ts.
	Language   string `yaml:"language"`
	LogDir     string // always <DataDir>/logs; not configurable via yaml
	LogMaxDays int    `yaml:"log_max_days"`
	TLS        struct {
		CertDir string `yaml:"cert_dir"` // Directory containing HTTPS cert/key files; presence of valid files enables HTTPS
		// Deprecated legacy fields — read for migration only, not used at runtime.
		Enabled  bool   `yaml:"enabled"`
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	} `yaml:"tls"`
	Fonts struct {
		Dir string `yaml:"dir"` // Directory containing user-supplied font files (default: <DataDir>/fonts)
	} `yaml:"fonts"`
	Appearance AppearanceConfig `yaml:"appearance"`
	DevPort    int              `yaml:"dev_port"` // Localhost-only HTTP port for dev proxy (0 = auto=Port+2 when TLS enabled, -1 = disabled)
	Upload     struct {
		MaxSizeMB int `yaml:"max_size_mb"` // Maximum file upload size in MB (default: 100)
		MaxFiles  int `yaml:"max_files"`   // Maximum number of files per upload (default: 20)
		// RetentionEnabled enables periodic deletion of upload files that no
		// message references any more (default: false). Attachments are
		// temporary by nature but nothing used to remove them, so the directory
		// grew without bound.
		RetentionEnabled bool `yaml:"retention_enabled"`
		// RetentionDays is how long an UNREFERENCED upload is kept before
		// deletion (default: 30). Files still named by a message are never
		// deleted regardless of age. Values below 1 are rejected by the API and
		// clamped to the default on load; "keep forever" is expressed by
		// RetentionEnabled=false, not by 0.
		RetentionDays int `yaml:"retention_days"`
	} `yaml:"upload"`
	// User is the human user's identity. It is a first-class section rather
	// than a field under chat because it is not chat behavior: the nickname is
	// how agents address the user (single chat, group chat and meetings), and
	// the profile is injected into every agent's system prompt so an agent can
	// understand who it is working with.
	User UserConfig `yaml:"user"`
	Chat struct {
		InitialMessages          int  `yaml:"initial_messages"`           // Number of messages to load initially (default: 20)
		PageSize                 int  `yaml:"page_size"`                  // Number of messages per lazy-load batch (default: 20)
		SessionPageSize          int  `yaml:"session_page_size"`          // Number of sessions per page in session list (default: 10)
		SystemPromptInterval     int  `yaml:"system_prompt_interval"`     // Re-inject system prompt every N assistant turns (0=never, default: 0)
		RecommendEnabled         bool `yaml:"recommend_enabled"`          // 推荐回复: generate a next-step recommendation after each assistant reply (default: false)
		RecommendContextMessages int  `yaml:"recommend_context_messages"` // 推荐回复参考的最近消息条数（用户+助手） (default: 10)
		ForkContextBudget        int  `yaml:"fork_context_budget"`        // Max characters of history re-injected on fork/rewind; older messages are omitted beyond this (default: 100000)
		// AutoContinueEnabled resumes a session whose turn ended abnormally
		// (agent crash, empty reply, backend error) by auto-sending a localized
		// "continue" message. Manual cancels are never resumed. (default: false)
		AutoContinueEnabled bool `yaml:"auto_continue_enabled"`
		// AutoContinueMaxRetries bounds those auto-continue attempts per user
		// turn. -1 means unlimited (still capped internally), 0 disables the
		// retries without turning the feature off. (default: 3)
		AutoContinueMaxRetries int `yaml:"auto_continue_max_retries"`
		// RestartResumeWindowMinutes bounds how old an interrupted turn may be
		// and still be auto-resumed after a restart (default: 60).
		//
		// A restart warning stays in the database forever, so without a bound
		// every abandoned session would look like "interrupted a moment ago" and
		// be resumed on the next boot. The window expresses the actual intent —
		// "pick up what the restart just interrupted" — rather than a cleanup
		// policy. 0 disables the age check (resume regardless of age).
		RestartResumeWindowMinutes int `yaml:"restart_resume_window_minutes"`
		// AutoRenameEnabled replaces a session's local title with an AI summary
		// of its user messages, at the same moment the local title is written
		// (first message, or first message after a fork/continue). Falls back to
		// the local title when the shared ai_summary model is unconfigured or
		// the call fails — which is why it can default ON: without the model it
		// is simply inert. (default: true)
		AutoRenameEnabled bool `yaml:"auto_rename_enabled"`
		// GroupMaxSpeeches caps how many times the AGENTS may speak to each
		// other in a group before the discussion is forced to stop and the
		// floor returns to the human. It counts MEMBER speeches only — the
		// host's routing/summary turns are not counted (the host is the
		// moderator, not a participant). The cap is mode-agnostic: host and
		// free mode share the same meaning. Reaching it stops the AI
		// discussion no matter whose turn it is. (default: 100)
		GroupMaxSpeeches int `yaml:"group_max_speeches"`
		// RecentProjectCardsMaxCount caps how many "other projects" cards the
		// session list shows below the current project's rows. The cards cover
		// two kinds of project: those with an active session (running / pending
		// approval / unread) and those merely active in the last 24 hours.
		// (default: 10, minimum 1)
		RecentProjectCardsMaxCount int `yaml:"recent_project_cards_max_count"`
	} `yaml:"chat"`
	Session struct {
		MaxCount                int  `yaml:"max_count"`                 // Maximum number of chat sessions per project (default: 15)
		ArchiveRetentionEnabled bool `yaml:"archive_retention_enabled"` // Enable auto-purge of archived sessions after retention period (default: false)
		ArchiveRetentionDays    int  `yaml:"archive_retention_days"`    // Days to keep archived sessions before auto-purge (0=keep forever, default: 0)
	} `yaml:"session"`
	RecentProjects struct {
		MaxCount int `yaml:"max_count"` // Maximum number of recent projects to keep (default: 10)
	} `yaml:"recent_projects"`
	TTS struct {
		Engine string `yaml:"engine"` // TTS engine: "edge" (default) or "openai"
		// Voice is the Edge engine's voice. It used to be shared by both
		// engines, and the settings page actively reset it on every engine
		// switch — so picking a voice for Edge and then trying the OpenAI
		// engine silently overwrote it with that engine's first voice. The
		// OpenAI engine has its own tts.openai.voice.
		Voice             string          `yaml:"voice"`               // Edge voice (default: "zh-CN-XiaoxiaoNeural")
		TTSModel          string          `yaml:"tts_model"`           // forward-compatible: MiniMax TTS model name (no-op until that provider is wired)
		Speed             float64         `yaml:"speed"`               // Speech speed multiplier (default: 1.0) — shared: "how fast I want to hear it" is one preference
		Format            string          `yaml:"format"`              // forward-compatible: audio format override (no-op, inferred from engine)
		InlineCodeMaxLen  int             `yaml:"inline_code_max_len"` // Max inline code content length (runes) to preserve for TTS; longer code is removed (default: 100)
		MaxSummarizeRunes int             `yaml:"max_summarize_runes"` // Max runes for summarization input; longer text is truncated (default: 10000, simple mode: 1000)
		MaxCacheFiles     int             `yaml:"max_cache_files"`     // Max cached TTS audio files to keep; oldest are auto-deleted (0=unlimited, default: 100)
		OpenAI            OpenAITTSConfig `yaml:"openai"`              // OpenAI-compatible /v1/audio/speech configuration (only used when engine: "openai")
	} `yaml:"tts"`
	STT         STTConfig         `yaml:"stt"`          // Speech-to-text (voice input) configuration
	AISummary   AISummaryConfig   `yaml:"ai_summary"`   // Shared AI model configuration (TTS summary + next-step recommendation)
	Summarize   SummarizeConfig   `yaml:"summarize"`    // Voice reading summary: only the summary-type selection (uses AISummary for "api")
	Meeting     MeetingConfig     `yaml:"meeting"`      // AI 听记 / meeting minutes defaults and output limits
	PortForward PortForwardConfig `yaml:"port_forward"` // SSH tunnel server + port forwarding configuration
	RAG         RAGConfig         `yaml:"rag"`          // RAG history memory configuration
	Terminal    TerminalConfig    `yaml:"terminal"`     // Interactive web terminal configuration
	DingTalk    DingTalkConfig    `yaml:"dingtalk"`     // DingTalk enterprise bot push notifications
	Feishu      FeishuConfig      `yaml:"feishu"`       // Feishu (飞书) enterprise bot push notifications
	WeChat      WeChatConfig      `yaml:"wechat"`       // Personal WeChat (iLink Bot) push credentials, obtained by QR scan
	PushMode    string            `yaml:"push_mode"`    // Push notification mode: "native" (default), "dingtalk", "feishu", "wechat", "disabled"
	FileSearch  FileSearchConfig  `yaml:"file_search"`  // File search configuration
	Forge       ForgeConfig       `yaml:"forge"`        // GitHub / GitLab integration (read-only issue & PR browsing)
	Mail        MailConfig        `yaml:"mail"`         // Built-in mail client master switch
	Skills      SkillsConfig      `yaml:"skills"`       // Cross-agent skill discovery (native dirs + custom dirs + git repos)
	UsageMeter  UsageMeterConfig  `yaml:"usage_meter"`  // Periodic usage budget + over-plan notification
	// MCP is the global MCP server list shared by every ACP agent. Agents only
	// store a policy (all / none / selected), never the definitions — see
	// internal/model/mcp.go. CLI backends ignore it: they have no mcpServers
	// parameter and read their own config files.
	MCP MCPConfig `yaml:"mcp"`
	// ShellEnv controls what environment a spawned agent process starts from.
	ShellEnv ShellEnvConfig `yaml:"shell_env"`
}

// ShellEnvConfig controls whether agent processes inherit the variables the
// user exports from their shell startup files (.bashrc / .zshrc / .profile).
//
// Off by default, and deliberately so: capturing that environment runs the
// user's startup files and hands the agent everything they export (proxy
// settings, unrelated credentials, LD_PRELOAD). Users who keep their agent
// credentials there have to opt in.
type ShellEnvConfig struct {
	// Inherit adds the shell's exports to the agent process environment when
	// the variable is missing from ClawBench's own environment. Existing
	// entries always win, and the shell is queried at most once every few
	// minutes (see platform.LoginShellEnv).
	Inherit bool `yaml:"inherit"`
}

// UsageMeterConfig is the single global usage budget rule.
//
// Deliberately ONE rule, not a list: the header shows exactly one capsule, and
// the notification has to name which rule fired. A user who wants both a
// short-horizon and a long-horizon budget has to pick one. Widening this to a
// list later is a backward-compatible YAML change (an old single object reads
// as a one-element list).
//
// The rule is opt-in via Enabled. Every field below it is optional: the meter
// reports whatever it can, and only a rule with nothing at all is silent.
// (Quota used to double as the on/off switch, which made "I want to see my
// spend since this date" impossible without inventing a budget.)
type UsageMeterConfig struct {
	// Enabled turns the meter on. It is the ONLY switch: a rule with an anchor
	// but no quota is meaningful ("how much have I used since then"), so the
	// absence of a quota can no longer stand for "off".
	//
	// Existing installs have no such key. ApplyDefaults treats a missing key
	// with a configured rule as enabled, so upgrading does not silently turn
	// anyone's meter off.
	Enabled bool `yaml:"enabled"`
	// AnchorDate is the cycle start, in the SERVER's local timezone (same
	// convention as cron expressions). Two shapes are accepted:
	//
	//   "YYYY-MM-DD"           → 00:00:00 on that day
	//   "YYYY-MM-DDTHH:MM[:SS]" → that exact wall-clock instant
	//
	// The date-only form is what every pre-existing install holds and keeps its
	// original meaning, so no migration is needed. Use
	// ParseUsageMeterAnchor/FormatUsageMeterAnchor rather than parsing this
	// string directly — they are the single definition of the accepted shapes.
	//
	// Empty means "since the beginning of the ledger": the meter then reports a
	// single running total with no cycle boundary and no rollover.
	AnchorDate string `yaml:"anchor_date"`
	// CycleMode selects how cycles roll: a fixed number of days, or the
	// calendar month. Empty means "fixed" (every install written before this
	// field existed), so no migration is needed.
	//
	// The two modes answer different questions. "Fixed" keeps every cycle the
	// same length, which is what a rolling 30-day budget means. "Monthly" makes
	// the cycle end at the same wall-clock instant each month, so the budget
	// lines up with a calendar month (and with monthly billing), at the price
	// of cycles that are 28-31 days long.
	CycleMode string `yaml:"cycle_mode"`
	// PeriodDays is the cycle length in days, used by CycleMode "fixed" only.
	// Cycle boundaries are computed as fixed 24h spans from the anchor rather
	// than calendar-date differences, so the cycle length stays constant across
	// DST transitions.
	//
	// 0 means "no cycle": the window runs from the anchor to now and never
	// rolls over. That is the honest reading of "the user never configured a
	// period" — inventing 30 days would silently reset their total every month.
	PeriodDays int `yaml:"period_days"`
	// Metric selects what is counted: "tokens" | "credit" | "cost".
	Metric string `yaml:"metric"`
	// Quota is the budget for one full cycle, in the metric's own unit
	// (tokens / credit / USD). 0 means "no budget": the meter still reports the
	// running total, it just has no baseline, no remaining amount and no
	// exhaustion projection.
	Quota float64 `yaml:"quota"`

	// NotifiedCycleStart is the cycle start (RFC3339) the last notification
	// belonged to. A new cycle start means the cycle rolled over, which
	// re-arms notification without any cleanup task.
	NotifiedCycleStart string `yaml:"notified_cycle_start"`
	// NotifiedStage records how far notification has progressed within the
	// current cycle: "" → "over" → "full". Monotonic; never goes backwards.
	//
	// Both fields are internal state, not user settings: they are hidden from
	// the settings page and never accepted from PATCH /api/config (see
	// PatchableConfigPaths). They are written by the service layer under
	// configMutex.
	NotifiedStage string `yaml:"notified_stage"`
}

// ForgeConfig holds the GitHub/GitLab integration settings.
//
// Credentials are scoped per (platform, host): a single global GitLab token
// would be sent to every self-hosted instance a user binds, so each host gets
// its own token. Notification toggles are global and default to enabled (set in
// ApplyDefaults — Go's bool zero value would otherwise disable everything).
type ForgeConfig struct {
	// Enabled is the master switch for the whole integration. Off means: no
	// polling, no event notifications, no dock entry, and every project's
	// repository binding has been cleared (turning it off deletes them — see
	// service.ClearAllProjectForges). Defaults to TRUE via ApplyDefaults'
	// presence check, so an install that has never written the key keeps the
	// integration it already had.
	//
	// This is a SERVER-side switch, unlike most UI preferences: "stop talking to
	// GitHub" is about the credentials and the outbound traffic, which live on
	// the server, so every client of one server must agree. A per-device flag
	// would leave one client polling after another had "disabled" it.
	Enabled bool `yaml:"enabled"`
	// Credentials maps a host (e.g. "github.com", "git.acme.internal:8443") to
	// its token. Hosts are stored lowercased.
	Credentials map[string]string `yaml:"credentials"`
	// Schemes maps a host to the API scheme ("http" or "https") it is served
	// over, as observed when the user configured that host's credential.
	//
	// It exists because a host alone cannot answer the question for a self-hosted
	// instance: a git remote of "git@gitlab.internal:team/repo.git" carries no
	// scheme, so an http-only instance would otherwise be probed over https and
	// fail with an opaque TLS error. The credential form is where the user names
	// the instance explicitly, so it is where the scheme is learned.
	//
	// A host with no entry is unknown, not https — the resolver decides the
	// fallback, so that a later https hint is not silently masked by a default
	// written here.
	Schemes map[string]string `yaml:"schemes"`
	// InsecureTLS allows skipping TLS verification for self-hosted instances
	// with self-signed certificates. Off by default; enabling it logs a warning.
	InsecureTLS bool `yaml:"insecure_tls"`
	// Notify controls which event kinds push a notification. All default true.
	Notify ForgeNotifyConfig `yaml:"notify"`
	// PauseEventTasks is the global kill-switch for event-triggered AI tasks.
	// When true, forge events still notify and count as unread, but no task is
	// fired. Off by default.
	PauseEventTasks bool `yaml:"pause_event_tasks"`
}

// ForgeNotifyConfig selects which forge events push a notification. Each flag
// is independent; the unread badge is NOT gated by these (it tracks changes,
// not notifications).
type ForgeNotifyConfig struct {
	Opened    bool `yaml:"opened"`
	Closed    bool `yaml:"closed"`
	Merged    bool `yaml:"merged"`
	Reopened  bool `yaml:"reopened"`
	Commented bool `yaml:"commented"`
	Pipeline  bool `yaml:"pipeline"`
}

// MailConfig is the master switch for the built-in mail client.
//
// It is a SERVER-side switch, like Forge.Enabled: the thing being turned off is
// the background IMAP polling and the email-triggered tasks that ride on it, and
// those live on the server. A per-device flag would leave one client's server
// syncing after another device had "disabled" mail.
//
// Off means: no background sync, no email-triggered task firing, no dock entry
// and no unread polling. It does NOT delete the configured accounts — turning
// the switch back on resumes exactly where it left off.
type MailConfig struct {
	// Enabled defaults to TRUE via ApplyDefaults' presence check: mail is an
	// existing feature, so an install that never wrote the key must keep it.
	Enabled bool `yaml:"enabled"`
}

// AppearanceConfig holds the custom-wallpaper *resources* the server owns.
//
// Which wallpaper a device displays is NOT here: the source, the on/off switch
// and the selected gallery image are per-device choices kept in the browser
// (localStorage), alongside panel translucency / blur / edge-fade / wave speed.
// Two devices on one server can therefore show different wallpapers.
//
// What remains server-side is what a browser cannot own: the uploaded gallery
// files on disk and the fetched Bing daily image, both of which are shared.
type AppearanceConfig struct {
	Local LocalWallpaperConfig `yaml:"local"`
	Bing  BingWallpaperConfig  `yaml:"bing"`
}

// LocalWallpaperConfig holds the user-uploaded wallpaper gallery.
type LocalWallpaperConfig struct {
	Items []LocalWallpaperItem `yaml:"items"` // Gallery contents, in upload order
}

// LocalWallpaperItem describes one uploaded gallery image. File is a bare name
// resolved inside <DataDir>/theme/local; Name preserves the original upload
// file name for display only.
type LocalWallpaperItem struct {
	File       string `yaml:"file"`
	Name       string `yaml:"name"`
	UploadedAt int64  `yaml:"uploaded_at"` // Unix seconds
	Size       int64  `yaml:"size"`        // Bytes on disk
}

// BingWallpaperConfig holds the Bing daily wallpaper cache. Every field is
// server-owned (written by the fetch worker), because File is joined onto
// <DataDir>/theme/bing when served. The worker fetches once a day
// unconditionally — there is no enable switch — and a failed fetch keeps the
// previous File so the last good image keeps serving.
type BingWallpaperConfig struct {
	LastSuccessDate string `yaml:"last_success_date"` // yyyymmdd of the last successful fetch; prevents refetching the same day
	File            string `yaml:"file"`              // Bare cached file name, e.g. "bing-20260910.jpg"
	Copyright       string `yaml:"copyright"`         // Photographer credit, shown in the settings panel only
	Title           string `yaml:"title"`
	LastError       string `yaml:"last_error"`      // "" when healthy; a failed fetch keeps File so the last good image still serves
	LastAttemptAt   int64  `yaml:"last_attempt_at"` // Unix seconds of the last fetch attempt
}

// STTConfig holds configuration for speech-to-text (voice input).
//
// Credentials and engine-specific knobs live in PER-ENGINE sub-sections, not in
// shared top-level fields. Sharing them means switching the engine overwrites
// the previous engine's key, endpoint and options: point the recogniser at
// Doubao once and the self-hosted Whisper endpoint you had configured is gone
// when you switch back. Only the things that are genuinely one choice for the
// user — which engine, the recording shortcut, microphone processing, the voice
// gate and free-dialog behavior — stay at this level.
type STTConfig struct {
	Provider    string           `yaml:"provider"` // "vllm" (default), "doubao" (Volcano Engine Seed-ASR) or "alibaba" (Model Studio / DashScope)
	VLLM        STTEngineConfig  `yaml:"vllm"`
	Doubao      STTDoubaoConfig  `yaml:"doubao"`
	Alibaba     STTAlibabaConfig `yaml:"alibaba"`
	ShortcutKey string           `yaml:"shortcut_key"` // Recording shortcut (default: "F9")

	// Microphone preprocessing. Applied where the microphone is opened, so the
	// echo/noise/gain trio also reaches a meeting audience; the band filter is
	// applied on the recogniser side only (see web/src/utils/audioTuning.ts).
	MicEchoCancel    bool `yaml:"mic_echo_cancel"`    // Browser echo cancellation (default: true)
	MicNoiseSuppress bool `yaml:"mic_noise_suppress"` // Browser noise suppression (default: true)
	MicAutoGain      bool `yaml:"mic_auto_gain"`      // Browser automatic gain control (default: true)
	MicBandFilter    bool `yaml:"mic_band_filter"`    // Cut sub-100Hz rumble and above-8kHz hiss before recognition (default: true)

	// Voice gate. Audio that carries no speech is not sent to the engine at all,
	// which saves recognition cost and stops the model from inventing words out
	// of room tone.
	VoiceGate            bool   `yaml:"voice_gate"`             // Skip silent audio (default: true)
	VoiceGateSensitivity string `yaml:"voice_gate_sensitivity"` // conservative | standard | aggressive (default: standard)

	// Free-dialog mode: keep the microphone open, send on a trigger word or a
	// long pause, and resume listening once the reply has been read out.
	TriggerWords       string `yaml:"trigger_words"`        // Comma-separated send words (default: 发送,好了,就这样)
	SilenceSendSeconds int    `yaml:"silence_send_seconds"` // Pause that also sends, 0 = never (default: 3)
	IdleExitSeconds    int    `yaml:"idle_exit_seconds"`    // Leave the mode after this long with no recognized words, 0 = never (default: 60)
	BargeIn            string `yaml:"barge_in"`             // off | any | keyword (default: any)
	BargeInWords       string `yaml:"barge_in_words"`       // Comma-separated interrupt words (default: 等等,停一下)
}

// STTEngineConfig is the OpenAI-compatible (vLLM Whisper, or any server that
// speaks /v1/audio/transcriptions) engine's own configuration.
type STTEngineConfig struct {
	BaseURL string `yaml:"base_url"` // OpenAI-compatible base URL (default: "http://localhost:8000/v1")
	APIKey  string `yaml:"api_key"`  // Bearer token (may be empty for a local server)
	Model   string `yaml:"model"`    // Recognition model (default: "openai/whisper-large-v3")
	// Language is passed to the engine per request. Empty means "let the server
	// decide", which is what a multilingual Whisper deployment usually wants.
	Language string `yaml:"language"` // default "zh"
	// Streaming is the client-facing switch: the browser sends PCM frames
	// instead of uploading one file. This engine cannot stream natively, so the
	// server cuts the PCM into chunks on pauses.
	Streaming bool `yaml:"streaming"` // true=streaming incremental, false=non-streaming full (default: false)
	ChunkMs   int  `yaml:"chunk_ms"`  // Streaming slice interval in ms (default: 1000)
	// FinalRecheck re-transcribes the whole recording once the client sends
	// "end", replacing the text it accumulated from the chunks. It is the only
	// way a buffered engine's final answer can span chunk boundaries, but it
	// costs a second pass over the entire clip, so it is off by default and the
	// client keeps what the chunks produced.
	FinalRecheck bool `yaml:"final_recheck"` // Re-transcribe everything at the end (default: false)
}

// STTDoubaoConfig is the Volcano Engine (豆包 Seed-ASR) engine's own config.
//
// Auth is not a bearer token: the key goes in X-Api-Key together with the
// purchased resource id. Language is auto-detected, so there is no language
// field here.
type STTDoubaoConfig struct {
	APIKey     string `yaml:"api_key"`     // Console-issued key
	ResourceID string `yaml:"resource_id"` // Purchased resource (default: "volc.seedasr.sauc.duration")
	// Recognition options.
	EnableITN       bool   `yaml:"enable_itn"`        // Normalize numbers/dates to written form (default: true)
	EnablePunc      bool   `yaml:"enable_punc"`       // Add punctuation (default: true)
	EnableDDC       bool   `yaml:"enable_ddc"`        // Semantic smoothing: drop filler words and repeats (default: false)
	OutputZHVariant string `yaml:"output_zh_variant"` // "" | traditional | tw | hk (default: "")
	EndWindowSize   int    `yaml:"end_window_size"`   // Streaming VAD silence threshold in ms (default: 800)
	// Streaming is the client-facing switch; this engine streams natively.
	Streaming bool `yaml:"streaming"` // true=streaming incremental, false=non-streaming full (default: false)
}

// STTAlibabaConfig is the Alibaba Cloud Model Studio engine's own config.
//
// Two APIs sit behind one key: whole-file transcription always uses
// qwen3-asr-flash, while live streaming uses a Paraformer real-time model that
// the user picks here.
type STTAlibabaConfig struct {
	APIKey  string `yaml:"api_key"`  // DashScope key
	BaseURL string `yaml:"base_url"` // default "https://dashscope.aliyuncs.com" (intl: dashscope-intl.aliyuncs.com)
	Model   string `yaml:"model"`    // real-time model (default: "paraformer-realtime-v2")
	// Language is a hint; empty means "detect it".
	Language string `yaml:"language"` // default "zh"
	// Recognition options.
	EnableITN     bool `yaml:"enable_itn"`      // Normalize numbers/dates to written form (default: true)
	EnablePunc    bool `yaml:"enable_punc"`     // Add punctuation (default: true)
	EnableDDC     bool `yaml:"enable_ddc"`      // Semantic smoothing (default: false)
	EndWindowSize int  `yaml:"end_window_size"` // Streaming VAD silence threshold in ms (default: 800)
	// Streaming is the client-facing switch; this engine streams natively.
	Streaming bool `yaml:"streaming"` // true=streaming incremental, false=non-streaming full (default: false)
}

// FileSearchConfig holds configuration for the file search feature.
type FileSearchConfig struct {
	DisplayLimit int `yaml:"display_limit"` // Max search results to display (default: 100); request limit is display_limit+1 to detect truncation
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

// DingTalkConfig holds configuration for DingTalk enterprise bot push notifications.
type DingTalkConfig struct {
	Enabled   bool     `yaml:"enabled"`    // Enable DingTalk push (default: false)
	AppKey    string   `yaml:"app_key"`    // Enterprise app AppKey (ClientID)
	AppSecret string   `yaml:"app_secret"` // Enterprise app AppSecret (ClientSecret)
	AgentID   int64    `yaml:"agent_id"`   // Enterprise application agent_id (numeric, from DingTalk developer console)
	Users     []string `yaml:"users"`      // Static DingTalk userId list for single-chat push
}

// FeishuConfig holds configuration for Feishu (飞书) enterprise bot push notifications.
type FeishuConfig struct {
	Enabled   bool     `yaml:"enabled"`    // Enable Feishu push (default: false)
	AppID     string   `yaml:"app_id"`     // Enterprise app App ID (from Feishu developer console)
	AppSecret string   `yaml:"app_secret"` // Enterprise app App Secret, masked after save
	Users     []string `yaml:"users"`      // Static Feishu open_id list for single-chat push
}

// WeChatConfig holds the personal-WeChat (iLink Bot) push credentials. The
// values are produced by the QR login flow, not typed by the user.
//
// Deliberately has NO Enabled field: for DingTalk/Feishu, Enabled is derived
// from PushMode by ApplyDefaults, which cannot express "logged out". WeChat's
// gate is PushMode == "wechat" && BotToken != "", so a stale token left behind
// by a failed re-login cannot silently re-enable the channel.
//
// Deliberately has NO Users list: a push needs a context_token, which the
// platform only mints when the user messages the bot, so a static subscriber
// list would be a dead field. Subscribers are discovered from inbound traffic
// (source = "stream") only.
type WeChatConfig struct {
	BotToken string `yaml:"bot_token"` // Bot token from the QR login; write-only, never returned by GET /api/config
	BotID    string `yaml:"bot_id"`    // ilink_bot_id of the logged-in bot
	BaseURL  string `yaml:"base_url"`  // Server-assigned API base URL; empty = SDK default
	UserID   string `yaml:"user_id"`   // ilink_user_id of the account that scanned
}

// AISummaryConfig holds the shared AI model configuration used by both the
// voice/TTS summarizer (when summarize.tts_backend is "api") and the
// next-step recommendation feature (chat.recommend_enabled).
type AISummaryConfig struct {
	// ProviderID binds the summary model to a providers row. This is the only
	// supported way to configure it: the endpoint, key and wire format all come
	// from that row, so rotating a key happens once for every feature that uses
	// the shared model. Nil = not configured.
	ProviderID *int64 `yaml:"provider_id,omitempty"`
	Model      string `yaml:"model"` // Model name (empty = backend default)

	// Endpoint fallback for configs written before the provider binding existed
	// (and for hand-edited files): read only while provider_id is absent. The
	// settings UI never writes them.
	Format string    `yaml:"format,omitempty"` // "openai" / "anthropic" ("" = detect from base_url)
	API    APIConfig `yaml:"api,omitempty"`    // API endpoint + key
}

// SummarizeConfig holds configuration for voice/TTS reading summaries.
// It only selects the summary type; the detailed model/API configuration
// lives in AISummaryConfig (shared with next-step recommendation).
type SummarizeConfig struct {
	TTSBackend string `yaml:"tts_backend"` // Voice/TTS summarization type: "" (disabled), "simple" (extract conclusion), "api" (LLM via AISummaryConfig), "auto" (route by content)
	// AutoJunkRatio is the stripped-rune ratio at/above which the "auto"
	// backend routes a message to the LLM instead of speaking the cleaned text.
	// See internal/summarize.AutoJunkRatio. Default 0.5.
	AutoJunkRatio float64 `yaml:"auto_junk_ratio"`
}

// Meeting minutes output limits. Exported so the settings PATCH validator and
// the frontend's number field agree on the same range.
const (
	DefaultMeetingMinutesMaxTokens = 3000
	MinMeetingMinutesMaxTokens     = 1000
	MaxMeetingMinutesMaxTokens     = 8000
)

// MeetingConfig holds the defaults and output limits for AI meeting notes.
//
// It deliberately has NO model or endpoint fields: meeting minutes reuse the
// shared AISummaryConfig, so rotating a provider key happens once for every
// feature that summarizes text. Adding a meeting-specific model here would
// create a second source of truth for the same credential.
type MeetingConfig struct {
	// AutoMinutes asks the publisher's client to generate minutes when the
	// meeting ends. The trigger is client-side (the server only archives the
	// meeting); this switch is read by the client, not by a server job.
	AutoMinutes bool `yaml:"auto_minutes"`
	// Proofread produces a corrected transcript before the minutes. Turning it
	// off saves one model pass per meeting; the minutes then use the raw
	// transcript. Existing proofread artifacts are still preferred when present.
	Proofread bool `yaml:"proofread"`
	// Language is the minutes output language: "" follows the transcript,
	// "zh" forces Simplified Chinese, "en" forces English. It does not affect
	// the transcript itself (that is each engine's own language setting).
	Language string `yaml:"language"`
	// FileExcerpts sends short excerpts of the files opened during the meeting
	// to the minutes model. This is the only setting that sends project file
	// CONTENT off the machine; the file timeline itself is still included when
	// it is off.
	FileExcerpts bool `yaml:"file_excerpts"`
	// MinutesMaxTokens caps the final minutes output. The map-reduce chunk
	// extraction has its own fixed budget: this only controls how long the
	// final document may be, and it is a cap rather than a target.
	MinutesMaxTokens int `yaml:"minutes_max_tokens"`
}

// RAGConfig holds configuration for the RAG history memory system.
// FTS (full-text search) is always enabled. VectorEnabled controls vector embedding only.
type RAGConfig struct {
	VectorEnabled  bool   `yaml:"vector_enabled"`   // Enable vector embedding (default: true). FTS is always on.
	BaseURL        string `yaml:"base_url"`         // OpenAI-compatible API base URL (default: "http://localhost:11434")
	Model          string `yaml:"model"`            // Embedding model name (default: "bge-m3")
	APIKey         string `yaml:"api_key"`          // API key for the embedding service (optional, for cloud providers)
	OllamaBaseURL  string `yaml:"ollama_base_url"`  // Deprecated: use base_url
	OllamaModel    string `yaml:"ollama_model"`     // Deprecated: use model
	ChunkSize      int    `yaml:"chunk_size"`       // Chunk size in tokens (default: 512)
	ChunkOverlap   int    `yaml:"chunk_overlap"`    // Overlap between chunks in tokens (default: 64)
	PollInterval   string `yaml:"poll_interval"`    // Indexer poll interval (default: "5s")
	BatchSize      int    `yaml:"batch_size"`       // Messages per indexer batch (default: 50)
	SearchLimit    int    `yaml:"search_limit"`     // Default search result limit (default: 100)
	SearchPoolSize int    `yaml:"search_pool_size"` // Candidates per search source before RRF fusion (default: 20)
	RetentionDays  int    `yaml:"retention_days"`   // Archived data retention days (0=keep forever, default: 90)
}

// OpenAITTSConfig holds configuration for the OpenAI-compatible TTS engine.
//
// This engine carries its own endpoint and credential — reusing ai_summary.api
// would force the speech endpoint and the summarization endpoint to be the
// same service, which they need not be.
//
// Voice is a free-text field rather than a fixed list: the built-in voices
// (alloy, ash, ballad, coral, echo, fable, onyx, nova, sage, shimmer, verse,
// marin, cedar) are a subset of what a compatible server may accept, and the
// API also supports custom voice objects referenced by id.
type OpenAITTSConfig struct {
	BaseURL    string `yaml:"base_url"`    // OpenAI-compatible base URL, e.g. "https://api.openai.com/v1" (completed to …/audio/speech)
	APIKey     string `yaml:"api_key"`     // Bearer token (may be empty for a local server)
	Model      string `yaml:"model"`       // TTS model, e.g. "gpt-4o-mini-tts"; user-supplied (no default)
	Voice      string `yaml:"voice"`       // Voice name or custom voice id, e.g. "alloy" / "voice_1234"
	StreamMode string `yaml:"stream_mode"` // "auto" (default, per-model), "sse" (force SSE), "audio" (force raw body)
}

// APIConfig holds configuration for the API-based summarization backend.
type APIConfig struct {
	BaseURL string `yaml:"base_url"` // Endpoint URL: bare host, partial base (…/v1), or full endpoint (…/v1/chat/completions) — completed by summarize.BuildEndpointURL
	Key     string `yaml:"key"`      // API key (sent as Bearer token for OpenAI, x-api-key for Anthropic)
}

// ConfigInstance holds the resolved configuration after ApplyDefaults.
// Set once during startup, read-only afterwards.
var ConfigInstance Config

// Global application state
var (
	BinDir           string   // Directory of the running binary
	DataDir          string   // Runtime data directory (default: ~/.clawbench; override with --data-dir)
	RootPaths        []string // Filesystem root paths (Linux/macOS: ["/"], Windows: drive list)
	SessionToken     string   // Legacy: stores the password-derived token for "has password" check; NOT used for cookie validation when CookieToken is set
	CookieToken      string   // Cryptographically random session token for cookie validation (ISS-117, ISS-131, ISS-183)
	PasswordHash     []byte   // bcrypt hash for password verification (ISS-003a)
	PasswordIsSHA256 bool     // true when config.yaml stores password as sha256:<hex>
	// PasswordIsAutoGenerated is true when Password came from the auto-password
	// file rather than from user configuration. Such a password is a runtime
	// fallback and must never be written to config.yaml: doing so would
	// "promote" it to an explicit password, and the next startup would then
	// delete the auto-password file and stop printing the password in the banner.
	PasswordIsAutoGenerated bool
	ServerPort              int // Server listen port — set once at startup before HTTP listeners start, read-only afterwards. Do NOT modify after server starts; cookie names must be stable.
	SessionCookie           = "clawbench_session"
	DefaultAgentID          string // Default agent for new sessions, set from config or first agent
	// Language is the resolved UI language ("zh" / "en"), read by background
	// goroutines that must localize without an HTTP request in scope.
	Language string

	// Upload limits (set from config, with defaults)
	UploadMaxSizeMB int // Default: 100
	UploadMaxFiles  int // Default: 20

	// Chat UI config (set from config, with defaults)
	ChatInitialMessages      int  // Default: 20
	ChatPageSize             int  // Default: 20
	ChatSessionPageSize      int  // Default: 10
	ChatSystemPromptInterval int  // Re-inject system prompt every N assistant turns (0=never, default: 0)
	ChatRecommendEnabled     bool // 推荐回复: generate next-step recommendation after each assistant reply (default: false)
	// ChatForkContextBudget bounds the history text re-injected on fork/rewind
	// (default: DefaultForkContextBudget). Read by service.BuildForkContext.
	ChatForkContextBudget int
	// ChatAutoContinueEnabled / ChatAutoContinueMaxRetries drive the
	// abnormal-termination auto-resume. Read by internal/service at turn end.
	ChatAutoContinueEnabled    bool
	ChatAutoContinueMaxRetries int
	// ChatRestartResumeWindowMinutes bounds the age of an interrupted turn that
	// the restart-resume worker will pick up (0 = no age bound). Read by
	// internal/service at startup.
	ChatRestartResumeWindowMinutes int
	// ChatAutoRenameEnabled switches on the AI summary rename of a session at
	// the moment its local title is written. Read by internal/service.
	ChatAutoRenameEnabled bool
	// UserNickname is the human user's display name (default "User"). Read by
	// internal/service at request time so a rename takes effect without a
	// restart. Group chat uses it as the routing target; single chat uses it
	// only to address the user.
	UserNickname string
	// UserProfile is the user's free-form self-description, injected into every
	// agent system prompt (see service.BuildUserIdentitySection). Read at
	// request time for the same reason.
	UserProfile string
	// ChatGroupMaxSpeeches caps member-to-member speeches in a group before
	// the discussion is forced to stop (default 100). Read by internal/service
	// at turn time so a change takes effect without a restart.
	ChatGroupMaxSpeeches int
	// ChatRecentProjectCardsMaxCount caps the "other projects" cards in the
	// session list (default 10). Read by the overview handler at request time,
	// so a change takes effect without a restart.
	ChatRecentProjectCardsMaxCount int

	// Session limits (set from config, with defaults)
	SessionMaxCount int // Default: 15

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

// PersistCookieToken writes the cookie token to the data directory so it
// survives server restarts. The token is not secret (it's validated via
// constant-time compare), but it should not be readable by other users.
func PersistCookieToken(token string) {
	if DataDir == "" {
		return
	}
	_ = os.MkdirAll(DataDir, 0o755) // best-effort: if this fails, WriteFile will also fail
	path := filepath.Join(DataDir, "cookie-token")
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		// Non-fatal: cookie will simply not survive restart; user re-logs in.
		_ = err
	}
}

// LoadCookieToken reads the persisted cookie token from the data directory.
// Returns empty string if the file does not exist or cannot be read.
func LoadCookieToken() string {
	if DataDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(DataDir, "cookie-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SplitForgeHostScheme parses a user-supplied forge host into the scheme it
// named and the normalized host key.
//
// The returned scheme is "" when the input did not name one, which callers must
// distinguish from an explicit "https": a bare host must not overwrite a scheme
// the user set earlier by typing a URL. The scheme is returned as written
// (lowercased) rather than validated here, because the resolver is the place
// that knows which schemes are usable and what to do with an unknown one.
//
// The host half is the same key NormalizeForgeHost produces, since both go
// through this function — a scheme and its host can therefore never be read from
// two different spellings of the same input.
func SplitForgeHostScheme(raw string) (scheme, host string) {
	h := strings.ToLower(strings.TrimSpace(raw))
	if h == "" {
		return "", ""
	}
	// The scheme is whatever precedes "://". Split rather than TrimPrefix so an
	// unknown scheme ("ftp://") is surfaced to the resolver instead of being
	// silently treated as absent.
	if i := strings.Index(h, "://"); i >= 0 {
		scheme = h[:i]
		h = h[i+len("://"):]
	}
	// A userinfo prefix ("git@gitlab.com") carries no scope information.
	if i := strings.LastIndex(h, "@"); i >= 0 {
		h = h[i+1:]
	}
	// Anything from the first "/" on is a path, not the host. This also drops a
	// trailing slash ("gitlab.com/" -> "gitlab.com").
	if i := strings.Index(h, "/"); i >= 0 {
		h = h[:i]
	}
	// A bracketed IPv6 literal must be handled before the port split: its own
	// colons are not a port separator, and treating them as one would truncate
	// "[::1]:8080" to "[". Everything up to "]" is the address; a numeric
	// ":port" after it is kept, anything else is a path tail and is dropped.
	if strings.HasPrefix(h, "[") {
		end := strings.Index(h, "]")
		if end < 0 {
			// Unterminated bracket: not a usable host, so fall through to the
			// generic handling rather than inventing one.
			return scheme, ""
		}
		rest := h[end+1:]
		if strings.HasPrefix(rest, ":") && isAllDigits(rest[1:]) {
			return scheme, h[:end+1] + rest
		}
		return scheme, h[:end+1]
	}
	// Keep "host:port" but drop "host:path" (an scp-like remote's tail).
	if i := strings.Index(h, ":"); i >= 0 && !isAllDigits(h[i+1:]) {
		h = h[:i]
	}
	return scheme, strings.TrimSpace(h)
}

// NormalizeForgeHost canonicalizes a forge host into the credential scope key.
//
// The key must be derived from the HOST alone, because that is what the
// binding stores and what ForgeToken looks up. A user typing "https://gitlab.com"
// or "gitlab.com/" means the same instance as "gitlab.com"; storing those as
// distinct keys would leave the real lookup empty, and an unauthenticated
// request to a private repository comes back as 404 Project Not Found — an
// error that names the wrong problem. So the scheme, credentials, any path and
// a trailing slash are all stripped here, and the result is lowercased.
//
// A port is preserved ("git.acme.internal:8443"): it is part of the instance
// identity, not decoration. The scheme is dropped rather than folded in for the
// same reason it is tracked separately: "http://h" and "https://h" are one
// repository reached two ways, not two repositories.
func NormalizeForgeHost(host string) string {
	_, h := SplitForgeHostScheme(host)
	return h
}

// isAllDigits reports whether s is a non-empty run of ASCII digits (a port).
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ForgeToken returns the token configured for a host, or "" when none is set.
// The host is normalized so any spelling of the same instance resolves.
func (c *Config) ForgeToken(host string) string {
	if c.Forge.Credentials == nil {
		return ""
	}
	return c.Forge.Credentials[NormalizeForgeHost(host)]
}

// SetForgeToken stores (or clears, when token is empty) the token for a host.
// The host is normalized to the credential scope key; an empty host is rejected
// as a no-op.
func (c *Config) SetForgeToken(host, token string) {
	host = NormalizeForgeHost(host)
	if host == "" {
		return
	}
	if c.Forge.Credentials == nil {
		c.Forge.Credentials = make(map[string]string)
	}
	if token == "" {
		delete(c.Forge.Credentials, host)
		return
	}
	c.Forge.Credentials[host] = token
}

// ForgeHasToken reports whether a token is configured for a host, without
// revealing the token itself. This is what GET /api/config exposes.
func (c *Config) ForgeHasToken(host string) bool {
	return c.ForgeToken(host) != ""
}

// ForgeScheme returns the recorded API scheme for a host, or "" when the host
// has no hint. The empty result means "unknown", not https — the resolver owns
// the fallback, so a default written here could never be distinguished from a
// user's explicit choice.
func (c *Config) ForgeScheme(host string) string {
	if c.Forge.Schemes == nil {
		return ""
	}
	return c.Forge.Schemes[NormalizeForgeHost(host)]
}

// SetForgeScheme records (or clears, when scheme is empty) the API scheme a host
// is served over.
//
// Clearing is the default outcome for an empty scheme rather than storing "": a
// bare host must not overwrite a hint the user set earlier by typing a URL. The
// value is stored as given — validation belongs to the resolver, which is the
// only place that knows which schemes are usable.
func (c *Config) SetForgeScheme(host, scheme string) {
	host = NormalizeForgeHost(host)
	if host == "" {
		return
	}
	if scheme == "" {
		// Nothing to record, and possibly something to forget: a delete here is
		// what makes "clear the token" able to drop the hint with it.
		delete(c.Forge.Schemes, host)
		return
	}
	if c.Forge.Schemes == nil {
		c.Forge.Schemes = make(map[string]string)
	}
	c.Forge.Schemes[host] = scheme
}
