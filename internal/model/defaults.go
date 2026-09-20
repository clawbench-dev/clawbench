package model

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

// FirstRun records whether this process started against a brand-new install.
// It is captured during ApplyDefaults (before the database is created) and
// exposed to the client so the frontend can apply out-of-box appearance
// defaults such as the default theme. It is process-scoped, not persisted:
// a restart of an existing install re-evaluates to false.
var FirstRun bool

// HealedBingFetch records that ApplyDefaults repaired an unrepresentable
// appearance state — the wallpaper mode says Bing while the fetch switch is off
// — so startup knows the repaired value needs writing to disk. ApplyDefaults
// fixes the value in memory, so this flag is the only surviving evidence that a
// write is needed.
var HealedBingFetch bool

// IsFreshInstall reports whether this is a brand-new installation, which is
// what gates the out-of-box appearance defaults (Bing daily wallpaper).
//
// A missing config.yaml alone is not sufficient evidence: config.yaml is
// optional and is never written at startup, so a long-running install that
// simply never changed a setting looks identical to a fresh one. The database
// file, by contrast, is created on every startup (service.InitDB) and is
// therefore a reliable "this install has run before" marker.
func IsFreshInstall(presence map[string]bool) bool {
	if presence != nil {
		return false
	}
	if DataDir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(DataDir, "ClawBench.db")); err == nil {
		return false
	}
	return true
}

// ParsePresenceMap walks a raw YAML map and returns a flat set of dot-separated
// keys that were explicitly present. For example, given:
//
//	port_forward:
//	  enabled: true
//
// It returns: {"port_forward": true, "port_forward.enabled": true}
func ParsePresenceMap(raw map[string]any) map[string]bool {
	presence := make(map[string]bool)
	walkPresenceMap(raw, "", presence)
	return presence
}

func walkPresenceMap(m map[string]any, prefix string, presence map[string]bool) {
	for key, val := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}
		presence[fullKey] = true
		if nested, ok := val.(map[string]any); ok {
			walkPresenceMap(nested, fullKey, presence)
		}
	}
}

// ApplyDefaults fills zero-value fields in cfg with sensible defaults.
// presence indicates which keys were explicitly set in the config file,
// used to distinguish "user wrote enabled: false" from "user omitted the section".
// Returns the auto-generated password if one was created, empty string otherwise.
func ApplyDefaults(cfg *Config, presence map[string]bool) string { //nolint:gocognit,gocyclo // exhaustive default application for all config fields
	var autoPassword string

	// --- Server ---
	if cfg.Port <= 0 {
		cfg.Port = 20000
	}

	// --- Language ---
	// UI language for background-localized strings (scheduled tasks, pushes,
	// auto-continue). "zh" matches the frontend's fallbackLocale, so a server
	// that has never been told the user's language behaves like the UI does.
	// An explicit value from config.yaml wins; the frontend overwrites it on
	// the first locale change.
	if cfg.Language == "" {
		cfg.Language = DefaultLanguage
	}

	// --- TLS ---
	// Migrate legacy enabled/cert_file/key_file fields to the new cert_dir scheme.
	// If cert_dir is unset but legacy cert_file points to an existing directory
	// containing the cert, derive cert_dir from it (the cert directory already
	// holds the matching key file per the standard layouts).
	if cfg.TLS.CertDir == "" && cfg.TLS.CertFile != "" {
		if dir := filepath.Dir(cfg.TLS.CertFile); dir != "." {
			cfg.TLS.CertDir = dir
		}
	}
	// Default cert directory when neither new nor legacy TLS is configured.
	if cfg.TLS.CertDir == "" {
		cfg.TLS.CertDir = DefaultTLSCertDir()
	}

	// --- Fonts ---
	// Custom font directory defaults to <DataDir>/fonts when unset.
	if cfg.Fonts.Dir == "" {
		cfg.Fonts.Dir = DefaultFontsDir()
	}

	// --- Appearance (custom wallpaper) ---
	// PanelOpacity: default 0.85 (85% opacity for main work panels when a
	// wallpaper is set). An explicit user value (including 0 = fully opaque is
	// NOT a valid target here; range 0.5–1.0 is enforced by PATCH validation)
	// must survive zero-value handling, so only fill when truly unset and the
	// key was not explicitly present in the config file. Treat any missing or
	// zero value as "use default". PanelOpacity intentionally has no presence
	// edge: 0 is outside the valid PATCH range, so a hand-edited 0 can only
	// mean "unset".
	if cfg.Appearance.PanelOpacity <= 0 {
		cfg.Appearance.PanelOpacity = 0.85
	}
	// Wallpaper source selection. Two independent sources exist (a local
	// gallery and the Bing daily image) but only one is shown at a time.
	//
	// The wallpaper layer itself is OFF out of the box: WallpaperEnabled stays
	// at its zero value for fresh installs too, so no image is downloaded or
	// displayed until the user turns the switch on. What a fresh install does
	// pre-select is the *source* (Bing daily) with its fetch switch on, so that
	// flipping the switch shows the Bing image immediately rather than an empty
	// panel with no source chosen.
	//
	// Existing installs are deliberately left alone: a user who never enabled a
	// wallpaper must not have one appear after an upgrade.
	//
	// The decision is captured in FirstRun because the database file this check
	// relies on is created later in startup, so it cannot be re-evaluated once
	// the server is serving requests.
	FirstRun = IsFreshInstall(presence)
	if FirstRun {
		if cfg.Appearance.WallpaperMode == "" {
			cfg.Appearance.WallpaperMode = "bing"
		}
		cfg.Appearance.Bing.Enabled = true
		if cfg.Appearance.Bing.Mkt == "" {
			cfg.Appearance.Bing.Mkt = "zh-CN"
		}
	}

	// Bing is the selected source, so its fetch switch must be on. This is
	// normally kept in step by the mode endpoint, but configs written before
	// that coupling existed can say mode=bing with the switch off — a state the
	// settings UI cannot reach or repair, where the worker silently refuses to
	// fetch and the sync button appears to do nothing.
	if cfg.Appearance.WallpaperMode == "bing" {
		// Record that the value needed repairing so startup persists it; the
		// assignment below destroys the evidence.
		HealedBingFetch = !cfg.Appearance.Bing.Enabled
		cfg.Appearance.Bing.Enabled = true
	}

	// --- DevPort ---
	// -1 = explicitly disabled; 0 = auto (Port+2 when TLS active, disabled otherwise)
	if cfg.DevPort == 0 {
		if cfg.ResolveTLSActive() {
			cfg.DevPort = cfg.Port + 2
		}
	}

	// --- LogLevel ---
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	// --- Password ---
	autoPasswordFile := filepath.Join(DataDir, "auto-password")
	if cfg.Password == "" {
		// Try to reuse previously auto-generated password
		saved, err := os.ReadFile(autoPasswordFile)
		if err == nil && len(saved) > 0 {
			cfg.Password = string(saved)
		} else {
			// Generate new random password (32 hex chars = 16 bytes = 128 bits entropy)
			// ISS-269: increased from 4 bytes (32-bit) to 16 bytes (128-bit)
			// to make offline brute-force infeasible
			b := make([]byte, 16)
			if _, err := rand.Read(b); err != nil {
				// Random generation failure is fatal — password would be predictable
				fmt.Fprintf(os.Stderr, "FATAL: crypto/rand.Read failed: %v\n", err)
				os.Exit(1)
			}
			cfg.Password = fmt.Sprintf("%x", b)
			// Persist for reuse across restarts
			_ = os.MkdirAll(filepath.Dir(autoPasswordFile), 0o755)
			_ = os.WriteFile(autoPasswordFile, []byte(cfg.Password), 0o600)
		}
		autoPassword = cfg.Password
	} else {
		// SHA-256 hashed or user-set plaintext password — remove stale auto-password file
		_ = os.Remove(autoPasswordFile)
	}

	// --- LogDir ---
	// LogDir is always <DataDir>/logs — not configurable via config.yaml.
	// This avoids relative-path pitfalls (CWD-dependent resolution).
	cfg.LogDir = filepath.Join(DataDir, "logs")

	if cfg.LogMaxDays <= 0 {
		cfg.LogMaxDays = 7
	}

	// --- Upload ---
	if cfg.Upload.MaxSizeMB <= 0 {
		cfg.Upload.MaxSizeMB = 100
	}
	if cfg.Upload.MaxFiles <= 0 {
		cfg.Upload.MaxFiles = 20
	}

	// --- Chat ---
	if cfg.Chat.InitialMessages <= 0 {
		cfg.Chat.InitialMessages = 20
	}
	if cfg.Chat.PageSize <= 0 {
		cfg.Chat.PageSize = 20
	}
	if cfg.Chat.SessionPageSize <= 0 {
		cfg.Chat.SessionPageSize = 10
	}
	// SystemPromptInterval: 0 = never re-inject is the intentional DEFAULT.
	// The old `<= 0 → 10` rewrite made 0 unexpressable — a user who
	// explicitly disabled periodic re-injection was silently switched back
	// to every-10-turns. Negative values (hand-edited yaml only; PATCH
	// rejects them earlier) clamp to 0.
	// 0 = 从不重注即为默认值。旧的 `<= 0 → 10` 改写使 0 无法表达——显式
	// 关闭周期性重注的用户会被静默改回每 10 轮。负值(仅手改 yaml 可产生;
	// PATCH 已在更早处拦截)收敛为 0。
	if cfg.Chat.SystemPromptInterval < 0 {
		cfg.Chat.SystemPromptInterval = 0
	}
	// RecommendEnabled: bool zero-value (false) is the intentional default.
	// Use presence map to distinguish "user wrote false" from "user omitted the field".
	if p, ok := presence["chat.recommend_enabled"]; !ok || !p {
		cfg.Chat.RecommendEnabled = false
	}
	if cfg.Chat.RecommendContextMessages <= 0 {
		cfg.Chat.RecommendContextMessages = 10
	}
	// ForkContextBudget bounds the history text re-injected when forking or
	// rewinding a session. 0/negative (omitted field, or hand-edited yaml)
	// falls back to the default rather than meaning "inject nothing".
	if cfg.Chat.ForkContextBudget <= 0 {
		cfg.Chat.ForkContextBudget = DefaultForkContextBudget
	}
	// AutoContinueEnabled: bool zero-value (false) is the intentional default —
	// auto-resuming a session spends tokens without the user asking, so it is
	// opt-in. Use the presence map to distinguish "user wrote false" from
	// "user omitted the field".
	if p, ok := presence["chat.auto_continue_enabled"]; !ok || !p {
		cfg.Chat.AutoContinueEnabled = false
	}
	// AutoContinueMaxRetries: -1 (unlimited) and 0 (retries disabled) are both
	// meaningful, so this CANNOT use a `<= 0 → default` rewrite — that would
	// silently turn "unlimited" into 3. Only an omitted field takes the default.
	if p, ok := presence["chat.auto_continue_max_retries"]; !ok || !p {
		cfg.Chat.AutoContinueMaxRetries = DefaultAutoContinueMaxRetries
	}
	// Negative values other than the -1 sentinel are unrepresentable (PATCH
	// rejects them too); clamp to 0 so a hand-edited yaml cannot produce a
	// value that would be read as "unlimited" by accident.
	if cfg.Chat.AutoContinueMaxRetries < AutoContinueUnlimited {
		cfg.Chat.AutoContinueMaxRetries = 0
	}

	// --- Session ---
	if cfg.Session.MaxCount <= 0 {
		cfg.Session.MaxCount = 15
	}
	// ArchiveRetentionEnabled: bool zero-value (false) is intentional default.
	// Use presence map to distinguish "user wrote false" from "user omitted the field".
	if p, ok := presence["session.archive_retention_enabled"]; ok && p {
		// User explicitly set archive_retention_enabled, keep their value
	} else {
		cfg.Session.ArchiveRetentionEnabled = false
	}
	// ArchiveRetentionDays: 0 = keep forever is the intentional DEFAULT —
	// archived sessions should not vanish by surprise. The old `<= 0 → 30`
	// rewrite made 0 unexpressable, so a user who wanted retention disabled
	// was silently enrolled in a 30-day purge. Negative values clamp to 0.
	// 0 = 永久保留即为默认值——归档会话不应莫名消失。旧的 `<= 0 → 30` 改写
	// 使 0 无法表达,想关闭留存的用户被静默纳入 30 天清理。负值收敛为 0。
	if cfg.Session.ArchiveRetentionDays < 0 {
		cfg.Session.ArchiveRetentionDays = 0
	}

	// --- Recent Projects ---
	if cfg.RecentProjects.MaxCount <= 0 {
		cfg.RecentProjects.MaxCount = 10
	}

	// --- Port Forward (SSH Tunnel) ---
	// Same bool zero-value trap as Proxy.
	if !presence["port_forward.enabled"] {
		cfg.PortForward.Enabled = true
	}
	// Persist host key to avoid SSH fingerprint mismatch after server restart
	if cfg.PortForward.HostKey == "" {
		cfg.PortForward.HostKey = filepath.Join(DataDir, "ssh_host_key")
	}

	// --- FRP ---
	// FRP is disabled by default; users must explicitly enable it.
	// Bool zero-value trap: "enabled" defaults to false (intentional — FRP
	// requires user-provided server), so no presence-map check needed.
	if cfg.FRP.ServerPort == 0 {
		cfg.FRP.ServerPort = 7000
	}

	// --- TTS ---
	if cfg.TTS.Engine == "" {
		cfg.TTS.Engine = "edge"
	}
	// Migrate legacy agent-based summarize backends to "api"
	agentBackends := map[string]bool{
		"claude": true, "codebuddy": true, "opencode": true, "codex": true,
		"qoder": true, "vecli": true, "deepseek": true, "pi": true, "mimo": true,
	}
	if agentBackends[cfg.Summarize.TTSBackend] {
		slog.Warn("summarize.tts_backend is a legacy agent backend, migrating to \"api\"", slog.String("old", cfg.Summarize.TTSBackend))
		cfg.Summarize.TTSBackend = "api"
	}
	if cfg.Summarize.TTSBackend == "" {
		cfg.Summarize.TTSBackend = "simple"
	}

	// --- AISummary (shared AI model config) ---
	// Legacy TTS summary config (summarize.tts_model / summarize.tts_api) is
	// migrated in main.go from the raw YAML map (fields removed from the typed
	// struct, so they no longer unmarshal). Here we only ensure a format default.
	if cfg.AISummary.API.BaseURL != "" && cfg.AISummary.Format == "" {
		cfg.AISummary.Format = "openai"
	}
	if cfg.TTS.Speed <= 0 {
		cfg.TTS.Speed = 1.0
	}
	if cfg.TTS.InlineCodeMaxLen <= 0 {
		cfg.TTS.InlineCodeMaxLen = 100
	}
	if cfg.TTS.MaxSummarizeRunes <= 0 {
		cfg.TTS.MaxSummarizeRunes = 10000
	}
	// MaxCacheFiles: -1 or 0 both mean unlimited; positive = cap
	// We treat 0 as the default (100) for UX convenience,
	// and -1 as explicitly unlimited.
	if cfg.TTS.MaxCacheFiles == 0 {
		cfg.TTS.MaxCacheFiles = 100
	}

	// --- STT ---
	if cfg.STT.BaseURL == "" {
		cfg.STT.BaseURL = "http://localhost:8000/v1"
	}
	if cfg.STT.Model == "" {
		cfg.STT.Model = "openai/whisper-large-v3"
	}
	if cfg.STT.Language == "" {
		cfg.STT.Language = "zh"
	}
	if cfg.STT.ChunkMs <= 0 {
		cfg.STT.ChunkMs = 1000
	}
	if cfg.STT.ShortcutKey == "" {
		cfg.STT.ShortcutKey = "F9"
	}

	// --- RAG ---
	// Bool zero-value trap: default to true when absent from config.
	if !presence["rag.vector_enabled"] {
		cfg.RAG.VectorEnabled = true
	}
	// FTS is always enabled. The Enabled field controls vector embedding only.
	// Backward compatibility: migrate deprecated Ollama fields to new generic fields.
	if cfg.RAG.BaseURL == "" && cfg.RAG.OllamaBaseURL != "" {
		cfg.RAG.BaseURL = cfg.RAG.OllamaBaseURL
	}
	if cfg.RAG.Model == "" && cfg.RAG.OllamaModel != "" {
		cfg.RAG.Model = cfg.RAG.OllamaModel
	}
	if cfg.RAG.BaseURL == "" {
		cfg.RAG.BaseURL = "http://localhost:11434"
	}
	if cfg.RAG.Model == "" {
		cfg.RAG.Model = "bge-m3"
	}
	if cfg.RAG.ChunkSize <= 0 {
		cfg.RAG.ChunkSize = 512
	}
	if cfg.RAG.ChunkOverlap <= 0 {
		cfg.RAG.ChunkOverlap = 64
	}
	if cfg.RAG.PollInterval == "" {
		cfg.RAG.PollInterval = "5s"
	}
	if cfg.RAG.BatchSize <= 0 {
		cfg.RAG.BatchSize = 50
	}
	if cfg.RAG.SearchLimit <= 0 {
		cfg.RAG.SearchLimit = 100
	}
	if cfg.RAG.SearchPoolSize <= 0 {
		cfg.RAG.SearchPoolSize = 20
	}
	if cfg.RAG.RetentionDays <= 0 {
		cfg.RAG.RetentionDays = 90
	}

	// --- Terminal ---
	// Bool zero-value trap: same as proxy/port_forward — default to true when absent.
	if !presence["terminal.enabled"] {
		cfg.Terminal.Enabled = true
	}
	if cfg.Terminal.IdleTimeout == "" {
		cfg.Terminal.IdleTimeout = "0" // 0 = never timeout; PTY lives until process exits or user closes
	}
	if cfg.Terminal.BufferLines <= 0 {
		cfg.Terminal.BufferLines = 2000
	}
	if cfg.Terminal.MaxLineBytes <= 0 {
		cfg.Terminal.MaxLineBytes = 65536 // 64KB per line
	}
	if cfg.Terminal.MaxBufferMB <= 0 {
		cfg.Terminal.MaxBufferMB = 4
	}
	if cfg.Terminal.MaxSessions <= 0 {
		cfg.Terminal.MaxSessions = 10
	}

	// --- DingTalk ---
	// Bool zero-value: enabled defaults to false (intentional — requires config), no presence check needed.

	// --- File Search ---
	if cfg.FileSearch.DisplayLimit <= 0 {
		cfg.FileSearch.DisplayLimit = 100
	}

	// --- PushMode ---
	if cfg.PushMode == "" {
		if cfg.DingTalk.Enabled {
			cfg.PushMode = "dingtalk"
		} else if cfg.Feishu.Enabled {
			cfg.PushMode = "feishu"
		} else {
			cfg.PushMode = "native"
		}
	}
	// Keep DingTalk.Enabled in sync with PushMode
	cfg.DingTalk.Enabled = cfg.PushMode == "dingtalk"
	// Keep Feishu.Enabled in sync with PushMode
	cfg.Feishu.Enabled = cfg.PushMode == "feishu"

	// --- Forge (GitHub / GitLab integration) ---
	// Notification toggles default to ENABLED. Go's bool zero value is false, so
	// without this block every flag would silently default to off — the opposite
	// of the documented behavior. Presence is used to distinguish "user wrote
	// false" from "user omitted the field": an absent key means default (true),
	// a present key means respect the parsed value.
	applyForgeNotifyDefaults(cfg, presence)
	normalizeForgeCredentialKeys(cfg)

	return autoPassword
}

// normalizeForgeCredentialKeys rewrites credential and scheme keys into the
// canonical host form.
//
// Older builds stored whatever the user typed after only lowercasing and
// trimming, so entering a URL — the very input this feature exists to support —
// persisted a row keyed "https://gitlab.internal". Lookups normalize their
// argument (ForgeToken calls NormalizeForgeHost), so such a row can never be
// found, and DELETE normalizes too, so it can never be removed either. The
// result is a credential the settings page reports as "Set" that no request can
// use and no click can clear.
//
// Normalizing here fixes every existing install in one place, on load, rather
// than leaving a permanent class of unreachable rows behind.
func normalizeForgeCredentialKeys(cfg *Config) {
	cfg.Forge.Credentials = normalizeForgeHostKeys(cfg.Forge.Credentials, "credential")
	cfg.Forge.Schemes = normalizeForgeHostKeys(cfg.Forge.Schemes, "scheme")
}

// normalizeForgeHostKeys rebuilds a host-keyed map with canonical keys,
// returning nil when there is nothing to do.
//
// On a collision the non-empty value wins: two spellings of one host must
// collapse to a single entry, and an empty value is the absence of information
// (an unset scheme), so it must not overwrite a real one. A collision between
// two *different* non-empty values keeps the first in sorted order, which makes
// the outcome deterministic rather than dependent on map iteration.
func normalizeForgeHostKeys(in map[string]string, label string) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	changed := false

	// Sort the source keys so a collision resolves the same way on every load.
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := in[k]
		canonical := NormalizeForgeHost(k)
		if canonical == "" {
			// Nothing usable to key on; dropping it is the only option that
			// does not leave an unreachable row behind.
			slog.Warn("dropping forge entry with unusable host", slog.String("kind", label), slog.String("host", k))
			changed = true
			continue
		}
		if canonical != k {
			changed = true
		}
		if existing, ok := out[canonical]; ok {
			changed = true
			if existing != "" {
				// Keep the existing value; the loser is recorded so a genuine
				// data conflict is visible rather than silently swallowed.
				slog.Warn("forge host collision, keeping first value",
					slog.String("kind", label), slog.String("host", canonical))
				continue
			}
		}
		out[canonical] = v
	}

	if !changed {
		return in
	}
	return out
}

// applyForgeNotifyDefaults fills the forge notification toggles, defaulting each
// absent flag to true. A flag explicitly present in the config file (even as
// false) is left untouched.
func applyForgeNotifyDefaults(cfg *Config, presence map[string]bool) {
	defaultTrue := func(key string, target *bool) {
		if _, present := presence[key]; !present {
			*target = true
		}
	}
	defaultTrue("forge.notify.opened", &cfg.Forge.Notify.Opened)
	defaultTrue("forge.notify.closed", &cfg.Forge.Notify.Closed)
	defaultTrue("forge.notify.merged", &cfg.Forge.Notify.Merged)
	defaultTrue("forge.notify.reopened", &cfg.Forge.Notify.Reopened)
	defaultTrue("forge.notify.commented", &cfg.Forge.Notify.Commented)
	defaultTrue("forge.notify.pipeline", &cfg.Forge.Notify.Pipeline)
}
