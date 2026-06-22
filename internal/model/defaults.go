package model

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"clawbench/internal/platform"
)

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

	// --- DevPort ---
	// -1 = explicitly disabled; 0 = auto (Port+2 when TLS enabled, disabled otherwise)
	if cfg.DevPort == 0 {
		if cfg.TLS.Enabled {
			cfg.DevPort = cfg.Port + 2
		}
	}

	// --- LogLevel ---
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	// --- Password ---
	autoPasswordFile := filepath.Join(BinDir, ".clawbench", "auto-password")
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
	if cfg.LogDir == "" {
		cfg.LogDir = filepath.Join(BinDir, ".clawbench", "logs")
	}
	cfg.LogDir = platform.ExpandTilde(cfg.LogDir)

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
	if cfg.Chat.SystemPromptInterval <= 0 {
		cfg.Chat.SystemPromptInterval = 10
	}

	// --- Session ---
	if cfg.Session.MaxCount <= 0 {
		cfg.Session.MaxCount = 10
	}

	// --- Recent Projects ---
	if cfg.RecentProjects.MaxCount <= 0 {
		cfg.RecentProjects.MaxCount = 10
	}

	// --- Proxy (legacy) ---
	// proxy.enabled and proxy.allowed_ports have been removed.
	// ProxyConfig is kept for backward-compatible YAML reading only.

	// --- Port Forward (SSH Tunnel) ---
	// Same bool zero-value trap as Proxy.
	if !presence["port_forward.enabled"] {
		cfg.PortForward.Enabled = true
	}
	// Persist host key to avoid SSH fingerprint mismatch after server restart
	if cfg.PortForward.HostKey == "" {
		cfg.PortForward.HostKey = filepath.Join(BinDir, ".clawbench", "ssh_host_key")
	}

	// --- TTS ---
	if cfg.TTS.Engine == "" {
		cfg.TTS.Engine = "edge"
	}
	if cfg.Summarize.Backend == "" {
		cfg.Summarize.Backend = "simple"
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

	// --- RAG ---
	// RAG is always enabled. No "enabled" toggle needed.
	// When the embedding API is unavailable, falls back to BM25 full-text search.
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
		cfg.RAG.PollInterval = "10s"
	}
	if cfg.RAG.BatchSize <= 0 {
		cfg.RAG.BatchSize = 10
	}
	if cfg.RAG.SearchLimit <= 0 {
		cfg.RAG.SearchLimit = 5
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

	return autoPassword
}
