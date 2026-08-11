package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePresenceMap(t *testing.T) {
	raw := map[string]any{
		"port": 20000,
		"proxy": map[string]any{
			"enabled": true,
		},
		"ssh": map[string]any{
			"enabled": false,
		},
		"tts": map[string]any{
			"engine": "edge",
		},
	}

	presence := ParsePresenceMap(raw)

	expectedKeys := []string{
		"port",
		"proxy",
		"proxy.enabled",
		"ssh",
		"ssh.enabled",
		"tts",
		"tts.engine",
	}
	for _, key := range expectedKeys {
		if !presence[key] {
			t.Errorf("expected key %q to be present", key)
		}
	}

	// Keys that should NOT be present
	unexpectedKeys := []string{"password", "proxy.port", "ssh.port"}
	for _, key := range unexpectedKeys {
		if presence[key] {
			t.Errorf("expected key %q to NOT be present", key)
		}
	}
}

func TestParsePresenceMapEmpty(t *testing.T) {
	presence := ParsePresenceMap(nil)
	if len(presence) != 0 {
		t.Errorf("expected empty presence map, got %d keys", len(presence))
	}

	presence = ParsePresenceMap(map[string]any{})
	if len(presence) != 0 {
		t.Errorf("expected empty presence map, got %d keys", len(presence))
	}
}

func TestParsePresenceMapDeeplyNested(t *testing.T) {
	raw := map[string]any{
		"tts": map[string]any{
			"api": map[string]any{
				"base_url": "https://api.openai.com/v1/chat/completions",
			},
		},
	}
	presence := ParsePresenceMap(raw)

	expectedKeys := []string{"tts", "tts.api", "tts.api.base_url"}
	for _, key := range expectedKeys {
		if !presence[key] {
			t.Errorf("expected key %q to be present", key)
		}
	}
}

// setupTestBinDir creates a temp dir, sets BinDir and DataDir, and returns a cleanup function.
func setupTestBinDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	origBinDir := BinDir
	origDataDir := DataDir
	BinDir = tmpDir
	DataDir = filepath.Join(tmpDir, ".clawbench")
	t.Cleanup(func() { BinDir = origBinDir; DataDir = origDataDir })
	return tmpDir
}

func TestApplyDefaultsEmptyConfig(t *testing.T) {
	tmpDir := setupTestBinDir(t)

	cfg := Config{}
	autoPassword := ApplyDefaults(&cfg, nil)

	// Should have auto-generated a password
	if autoPassword == "" {
		t.Error("expected auto-generated password for empty config")
	}
	if cfg.Password != autoPassword {
		t.Errorf("cfg.Password = %q, want %q", cfg.Password, autoPassword)
	}

	// Password should be persisted to file
	pwFile := filepath.Join(tmpDir, ".clawbench", "auto-password")
	data, err := os.ReadFile(pwFile)
	if err != nil {
		t.Errorf("auto-password file should exist: %v", err)
	} else if string(data) != autoPassword {
		t.Errorf("auto-password file = %q, want %q", string(data), autoPassword)
	}

	// Check all defaults
	if cfg.Port != 20000 {
		t.Errorf("Port = %d, want 20000", cfg.Port)
	}
	if cfg.LogDir == "" {
		t.Error("LogDir should not be empty")
	}
	if cfg.LogMaxDays != 7 {
		t.Errorf("LogMaxDays = %d, want 7", cfg.LogMaxDays)
	}
	if cfg.Upload.MaxSizeMB != 100 {
		t.Errorf("Upload.MaxSizeMB = %d, want 100", cfg.Upload.MaxSizeMB)
	}
	if cfg.Upload.MaxFiles != 20 {
		t.Errorf("Upload.MaxFiles = %d, want 20", cfg.Upload.MaxFiles)
	}
	if cfg.Chat.InitialMessages != 20 {
		t.Errorf("Chat.InitialMessages = %d, want 20", cfg.Chat.InitialMessages)
	}
	if cfg.Chat.PageSize != 20 {
		t.Errorf("Chat.PageSize = %d, want 20", cfg.Chat.PageSize)
	}
	if cfg.Session.MaxCount != 10 {
		t.Errorf("Session.MaxCount = %d, want 10", cfg.Session.MaxCount)
	}
	if !cfg.PortForward.Enabled {
		t.Error("PortForward.Enabled should default to true when not in config")
	}
	if cfg.TTS.Engine != "edge" {
		t.Errorf("TTS.Engine = %q, want %q", cfg.TTS.Engine, "edge")
	}
	if cfg.Summarize.TTSBackend != "simple" {
		t.Errorf("Summarize.TTSBackend = %q, want %q", cfg.Summarize.TTSBackend, "simple")
	}
	if cfg.TTS.Speed != 1.0 {
		t.Errorf("TTS.Speed = %v, want 1.0", cfg.TTS.Speed)
	}
	if cfg.RAG.SearchPoolSize != 20 {
		t.Errorf("RAG.SearchPoolSize = %d, want 20", cfg.RAG.SearchPoolSize)
	}
}

func TestApplyDefaultsPartialConfig(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{
		Port:     3000,
		Password: "my-secret",
		Upload: struct {
			MaxSizeMB int `yaml:"max_size_mb"`
			MaxFiles  int `yaml:"max_files"`
		}{
			MaxSizeMB: 50,
		},
	}

	autoPassword := ApplyDefaults(&cfg, map[string]bool{"port": true, "password": true, "upload": true, "upload.max_size_mb": true})

	// Explicitly set values should be preserved
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000 (explicitly set)", cfg.Port)
	}
	if cfg.Password != "my-secret" {
		t.Errorf("Password = %q, want %q (explicitly set)", cfg.Password, "my-secret")
	}
	if autoPassword != "" {
		t.Errorf("autoPassword = %q, want empty (password was set)", autoPassword)
	}
	if cfg.Upload.MaxSizeMB != 50 {
		t.Errorf("Upload.MaxSizeMB = %d, want 50 (explicitly set)", cfg.Upload.MaxSizeMB)
	}

	// Unset values should get defaults
	if cfg.Upload.MaxFiles != 20 {
		t.Errorf("Upload.MaxFiles = %d, want 20 (default)", cfg.Upload.MaxFiles)
	}
}

func TestApplyDefaultsBoolPresencePortForwardEnabledFalse(t *testing.T) {
	cfg := Config{}
	presence := map[string]bool{
		"port_forward":         true,
		"port_forward.enabled": true,
	}
	cfg.PortForward.Enabled = false

	ApplyDefaults(&cfg, presence)

	if cfg.PortForward.Enabled {
		t.Error("PortForward.Enabled should stay false when explicitly set to false")
	}
}

func TestApplyDefaultsBoolPresencePortForwardNoSection(t *testing.T) {
	cfg := Config{}
	// No port_forward section at all in config
	ApplyDefaults(&cfg, nil)

	if !cfg.PortForward.Enabled {
		t.Error("PortForward.Enabled should default to true when port_forward section is absent from config")
	}
}

func TestApplyDefaultsPasswordAutoGenerated(t *testing.T) {
	tmpDir := setupTestBinDir(t)

	cfg1 := Config{}
	pwd1 := ApplyDefaults(&cfg1, nil)

	cfg2 := Config{}
	pwd2 := ApplyDefaults(&cfg2, nil)

	if pwd1 == "" || pwd2 == "" {
		t.Error("expected non-empty auto-generated passwords")
	}
	// Second call should reuse the saved password from file
	if pwd1 != pwd2 {
		t.Errorf("second call should reuse saved password: pwd1=%q, pwd2=%q", pwd1, pwd2)
	}

	// 32 hex chars format (16 bytes = 128-bit entropy, ISS-269)
	if len(pwd1) != 32 {
		t.Errorf("auto-generated password length = %d, want 32 (hex format, 128-bit entropy)", len(pwd1))
	}

	// Verify the auto-password file exists
	pwFile := filepath.Join(tmpDir, ".clawbench", "auto-password")
	if _, err := os.Stat(pwFile); err != nil {
		t.Errorf("auto-password file should exist: %v", err)
	}
}

func TestApplyDefaultsPasswordReuse(t *testing.T) {
	setupTestBinDir(t)

	// First call: generates and saves password
	cfg1 := Config{}
	pwd1 := ApplyDefaults(&cfg1, nil)

	// Second call with empty config should reuse saved password
	cfg2 := Config{}
	pwd2 := ApplyDefaults(&cfg2, nil)

	if pwd1 != pwd2 {
		t.Errorf("password should be reused across calls: pwd1=%q, pwd2=%q", pwd1, pwd2)
	}
	if cfg1.Password != cfg2.Password {
		t.Errorf("cfg.Password should match: cfg1=%q, cfg2=%q", cfg1.Password, cfg2.Password)
	}
}

func TestApplyDefaultsPasswordExplicitRemovesFile(t *testing.T) {
	tmpDir := setupTestBinDir(t)

	// First: auto-generate password (creates file)
	cfg1 := Config{}
	ApplyDefaults(&cfg1, nil)

	pwFile := filepath.Join(tmpDir, ".clawbench", "auto-password")
	if _, err := os.Stat(pwFile); err != nil {
		t.Fatalf("auto-password file should exist after first call: %v", err)
	}

	// Second: user explicitly sets password (should remove file)
	cfg2 := Config{Password: "my-explicit-password"}
	autoPwd := ApplyDefaults(&cfg2, map[string]bool{"password": true})

	if autoPwd != "" {
		t.Errorf("autoPassword should be empty when user sets password explicitly, got %q", autoPwd)
	}
	if _, err := os.Stat(pwFile); !os.IsNotExist(err) {
		t.Error("auto-password file should be removed when user sets password explicitly")
	}
}

func TestApplyDefaults_DevPortWithTLS(t *testing.T) {
	setupTestBinDir(t)

	// When DevPort is 0 and TLS is enabled, DevPort should be Port+2
	cfg := Config{TLS: struct {
		Enabled  bool   `yaml:"enabled"`
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	}{Enabled: true}}
	ApplyDefaults(&cfg, nil)

	if cfg.DevPort != cfg.Port+2 {
		t.Errorf("DevPort = %d, want %d (Port+2 when TLS enabled)", cfg.DevPort, cfg.Port+2)
	}
}

func TestApplyDefaults_DevPortNegative1Disables(t *testing.T) {
	setupTestBinDir(t)

	// DevPort = -1 should be preserved (explicitly disabled)
	cfg := Config{DevPort: -1}
	ApplyDefaults(&cfg, nil)

	if cfg.DevPort != -1 {
		t.Errorf("DevPort = %d, want -1 (explicitly disabled)", cfg.DevPort)
	}
}

func TestApplyDefaults_DevPortZeroNoTLS(t *testing.T) {
	setupTestBinDir(t)

	// DevPort = 0 without TLS should stay 0 (disabled)
	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.DevPort != 0 {
		t.Errorf("DevPort = %d, want 0 (disabled without TLS)", cfg.DevPort)
	}
}

func TestApplyDefaults_TerminalPresenceFalse(t *testing.T) {
	setupTestBinDir(t)

	// When terminal.enabled is NOT in presence, should default to true
	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if !cfg.Terminal.Enabled {
		t.Error("Terminal.Enabled should default to true when absent from config")
	}
}

func TestApplyDefaults_TerminalPresenceExplicit(t *testing.T) {
	setupTestBinDir(t)

	// When terminal.enabled IS in presence and set to false, should stay false
	cfg := Config{}
	presence := map[string]bool{"terminal.enabled": true}
	cfg.Terminal.Enabled = false
	ApplyDefaults(&cfg, presence)

	if cfg.Terminal.Enabled {
		t.Error("Terminal.Enabled should stay false when explicitly set")
	}
}

func TestApplyDefaults_TerminalDefaults(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.Terminal.IdleTimeout != "0" {
		t.Errorf("Terminal.IdleTimeout = %q, want %q", cfg.Terminal.IdleTimeout, "0")
	}
	if cfg.Terminal.BufferLines != 2000 {
		t.Errorf("Terminal.BufferLines = %d, want 2000", cfg.Terminal.BufferLines)
	}
	if cfg.Terminal.MaxLineBytes != 65536 {
		t.Errorf("Terminal.MaxLineBytes = %d, want 65536", cfg.Terminal.MaxLineBytes)
	}
	if cfg.Terminal.MaxBufferMB != 4 {
		t.Errorf("Terminal.MaxBufferMB = %d, want 4", cfg.Terminal.MaxBufferMB)
	}
	if cfg.Terminal.MaxSessions != 10 {
		t.Errorf("Terminal.MaxSessions = %d, want 10", cfg.Terminal.MaxSessions)
	}
}

func TestApplyDefaults_RAGDefaults(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.RAG.BaseURL != "http://localhost:11434" {
		t.Errorf("RAG.BaseURL = %q, want %q", cfg.RAG.BaseURL, "http://localhost:11434")
	}
	if cfg.RAG.Model != "bge-m3" {
		t.Errorf("RAG.Model = %q, want %q", cfg.RAG.Model, "bge-m3")
	}
	if cfg.RAG.ChunkSize != 512 {
		t.Errorf("RAG.ChunkSize = %d, want 512", cfg.RAG.ChunkSize)
	}
	if cfg.RAG.ChunkOverlap != 64 {
		t.Errorf("RAG.ChunkOverlap = %d, want 64", cfg.RAG.ChunkOverlap)
	}
	if cfg.RAG.PollInterval != "5s" {
		t.Errorf("RAG.PollInterval = %q, want %q", cfg.RAG.PollInterval, "5s")
	}
	if cfg.RAG.BatchSize != 50 {
		t.Errorf("RAG.BatchSize = %d, want 50", cfg.RAG.BatchSize)
	}
	if cfg.RAG.SearchLimit != 100 {
		t.Errorf("RAG.SearchLimit = %d, want 100", cfg.RAG.SearchLimit)
	}
	if cfg.RAG.RetentionDays != 90 {
		t.Errorf("RAG.RetentionDays = %d, want 90", cfg.RAG.RetentionDays)
	}
	if !cfg.RAG.VectorEnabled {
		t.Error("RAG.VectorEnabled should default to true when absent from config")
	}
}

func TestApplyDefaults_RAGEnabledExplicitFalse(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{RAG: RAGConfig{VectorEnabled: false}}
	presence := map[string]bool{"rag.vector_enabled": true}
	ApplyDefaults(&cfg, presence)

	if cfg.RAG.VectorEnabled {
		t.Error("RAG.VectorEnabled should stay false when explicitly set to false")
	}
}

func TestApplyDefaults_RAGOllamaMigration(t *testing.T) {
	setupTestBinDir(t)

	// When Ollama fields are set but new fields are empty, should migrate
	cfg := Config{}
	cfg.RAG.OllamaBaseURL = "http://old-ollama:11434"
	cfg.RAG.OllamaModel = "old-model"
	ApplyDefaults(&cfg, nil)

	if cfg.RAG.BaseURL != "http://old-ollama:11434" {
		t.Errorf("RAG.BaseURL = %q, want %q (migrated from OllamaBaseURL)", cfg.RAG.BaseURL, "http://old-ollama:11434")
	}
	if cfg.RAG.Model != "old-model" {
		t.Errorf("RAG.Model = %q, want %q (migrated from OllamaModel)", cfg.RAG.Model, "old-model")
	}
}

func TestApplyDefaults_ChatSessionPageSize(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.Chat.SessionPageSize != 10 {
		t.Errorf("Chat.SessionPageSize = %d, want 10", cfg.Chat.SessionPageSize)
	}
	if cfg.Chat.SystemPromptInterval != 10 {
		t.Errorf("Chat.SystemPromptInterval = %d, want 10", cfg.Chat.SystemPromptInterval)
	}
}

func TestApplyDefaults_TTSCacheFiles(t *testing.T) {
	setupTestBinDir(t)

	// MaxCacheFiles = 0 should default to 100
	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.TTS.MaxCacheFiles != 100 {
		t.Errorf("TTS.MaxCacheFiles = %d, want 100", cfg.TTS.MaxCacheFiles)
	}
}

func TestApplyDefaults_TTSInlineCodeMaxLen(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.TTS.InlineCodeMaxLen != 100 {
		t.Errorf("TTS.InlineCodeMaxLen = %d, want 100", cfg.TTS.InlineCodeMaxLen)
	}
	if cfg.TTS.MaxSummarizeRunes != 10000 {
		t.Errorf("TTS.MaxSummarizeRunes = %d, want 10000", cfg.TTS.MaxSummarizeRunes)
	}
}

func TestApplyDefaults_PortForwardHostKey(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.PortForward.HostKey == "" {
		t.Error("PortForward.HostKey should have a default value")
	}
}

func TestApplyDefaults_RecentProjectsMaxCount(t *testing.T) {
	setupTestBinDir(t)

	// Default: 10
	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.RecentProjects.MaxCount != 10 {
		t.Errorf("RecentProjects.MaxCount = %d, want 10", cfg.RecentProjects.MaxCount)
	}
}

func TestApplyDefaults_RecentProjectsMaxCountExplicit(t *testing.T) {
	setupTestBinDir(t)

	// Explicitly set value should be preserved
	cfg := Config{}
	cfg.RecentProjects.MaxCount = 25
	ApplyDefaults(&cfg, map[string]bool{"recent_projects": true, "recent_projects.max_count": true})

	if cfg.RecentProjects.MaxCount != 25 {
		t.Errorf("RecentProjects.MaxCount = %d, want 25 (explicitly set)", cfg.RecentProjects.MaxCount)
	}
}

func TestApplyDefaults_LogLevel(t *testing.T) {
	setupTestBinDir(t)

	// Default LogLevel
	cfg := Config{}
	ApplyDefaults(&cfg, nil)
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}

	// Explicit LogLevel should be preserved
	cfg2 := Config{LogLevel: "debug"}
	ApplyDefaults(&cfg2, map[string]bool{"log_level": true})
	if cfg2.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q (explicitly set)", cfg2.LogLevel, "debug")
	}
}

func TestApplyDefaults_LocalhostAuthExempt(t *testing.T) {
	setupTestBinDir(t)

	// Default: true when not in presence map
	cfg := Config{}
	ApplyDefaults(&cfg, nil)
	if !cfg.LocalhostAuthExempt {
		t.Error("LocalhostAuthExempt should default to true when absent from config")
	}

	// Explicitly set to false via presence map
	cfg2 := Config{}
	cfg2.LocalhostAuthExempt = false
	ApplyDefaults(&cfg2, map[string]bool{"localhost_auth_exempt": true})
	if cfg2.LocalhostAuthExempt {
		t.Error("LocalhostAuthExempt should stay false when explicitly set")
	}
}

func TestApplyDefaults_FRPDefaults(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.FRP.ServerPort != 7000 {
		t.Errorf("FRP.ServerPort = %d, want 7000", cfg.FRP.ServerPort)
	}
	// FRP.Enabled should default to false (bool zero-value, intentional)
	if cfg.FRP.Enabled {
		t.Error("FRP.Enabled should default to false")
	}
}

func TestApplyDefaults_SummarizeTTSBackendMigration(t *testing.T) {
	setupTestBinDir(t)

	// Test legacy agent backend migration for Summarize.TTSBackend
	for _, backend := range []string{"claude", "codebuddy", "opencode", "codex"} {
		t.Run("tts_backend_"+backend, func(t *testing.T) {
			cfg := Config{}
			cfg.Summarize.TTSBackend = backend
			ApplyDefaults(&cfg, nil)
			if cfg.Summarize.TTSBackend != "api" {
				t.Errorf("Summarize.TTSBackend = %q after migration from %q, want %q", cfg.Summarize.TTSBackend, backend, "api")
			}
		})
	}
}

func TestApplyDefaults_SummarizeTTSBackendNonAgent(t *testing.T) {
	setupTestBinDir(t)

	// Non-agent backends should NOT be migrated
	cfg := Config{}
	cfg.Summarize.TTSBackend = "simple"
	ApplyDefaults(&cfg, nil)
	if cfg.Summarize.TTSBackend != "simple" {
		t.Errorf("Summarize.TTSBackend = %q, want %q (non-agent backend should not be migrated)", cfg.Summarize.TTSBackend, "simple")
	}
}

func TestApplyDefaults_RAGExplicitOverridesOllama(t *testing.T) {
	setupTestBinDir(t)

	// When both new and old fields are set, new fields take precedence
	cfg := Config{}
	cfg.RAG.BaseURL = "http://new-url:11434"
	cfg.RAG.Model = "new-model"
	cfg.RAG.OllamaBaseURL = "http://old-url:11434"
	cfg.RAG.OllamaModel = "old-model"
	ApplyDefaults(&cfg, nil)

	if cfg.RAG.BaseURL != "http://new-url:11434" {
		t.Errorf("RAG.BaseURL = %q, want %q (new field should take precedence)", cfg.RAG.BaseURL, "http://new-url:11434")
	}
	if cfg.RAG.Model != "new-model" {
		t.Errorf("RAG.Model = %q, want %q (new field should take precedence)", cfg.RAG.Model, "new-model")
	}
}

// TestApplyDefaults_AutoPasswordEntropy verifies ISS-269: auto-generated
// password has 128 bits of entropy (16 bytes = 32 hex chars), not 32 bits.
func TestApplyDefaults_AutoPasswordEntropy(t *testing.T) {
	setupTestBinDir(t)

	cfg := Config{}
	pwd := ApplyDefaults(&cfg, nil)

	if len(pwd) != 32 {
		t.Errorf("auto-generated password length = %d, want 32 (16 bytes hex = 128-bit entropy, ISS-269)", len(pwd))
	}

	// Verify it's valid hex
	for _, c := range pwd {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("auto-generated password contains non-hex char %q, ISS-269", c)
			break
		}
	}
}

func TestApplyDefaults_PushMode_DefaultNative(t *testing.T) {
	tmpDir := setupTestBinDir(t)
	_ = tmpDir

	cfg := Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.PushMode != "native" {
		t.Errorf("expected PushMode=native, got %q", cfg.PushMode)
	}
	if cfg.DingTalk.Enabled {
		t.Error("expected DingTalk.Enabled=false when PushMode=native")
	}
}

func TestApplyDefaults_PushMode_MigrationFromDingTalkEnabled(t *testing.T) {
	tmpDir := setupTestBinDir(t)
	_ = tmpDir

	cfg := Config{}
	cfg.DingTalk.Enabled = true
	cfg.DingTalk.AppKey = "test-key"
	cfg.DingTalk.AppSecret = "test-secret"
	ApplyDefaults(&cfg, nil)

	if cfg.PushMode != "dingtalk" {
		t.Errorf("expected PushMode=dingtalk when DingTalk.Enabled=true, got %q", cfg.PushMode)
	}
	if !cfg.DingTalk.Enabled {
		t.Error("expected DingTalk.Enabled=true when PushMode=dingtalk")
	}
}

func TestApplyDefaults_PushMode_ExplicitNativeKeepsDingTalkDisabled(t *testing.T) {
	tmpDir := setupTestBinDir(t)
	_ = tmpDir

	cfg := Config{}
	cfg.PushMode = "native"
	ApplyDefaults(&cfg, nil)

	if cfg.PushMode != "native" {
		t.Errorf("expected PushMode=native, got %q", cfg.PushMode)
	}
	if cfg.DingTalk.Enabled {
		t.Error("expected DingTalk.Enabled=false when PushMode=native")
	}
}

func TestApplyDefaults_PushMode_ExplicitDingTalkSyncsEnabled(t *testing.T) {
	tmpDir := setupTestBinDir(t)
	_ = tmpDir

	cfg := Config{}
	cfg.PushMode = "dingtalk"
	ApplyDefaults(&cfg, nil)

	if cfg.PushMode != "dingtalk" {
		t.Errorf("expected PushMode=dingtalk, got %q", cfg.PushMode)
	}
	if !cfg.DingTalk.Enabled {
		t.Error("expected DingTalk.Enabled=true when PushMode=dingtalk")
	}
}

func TestApplyDefaults_PushMode_DisabledSyncsEnabled(t *testing.T) {
	tmpDir := setupTestBinDir(t)
	_ = tmpDir

	cfg := Config{}
	cfg.PushMode = "disabled"
	ApplyDefaults(&cfg, nil)

	if cfg.PushMode != "disabled" {
		t.Errorf("expected PushMode=disabled, got %q", cfg.PushMode)
	}
	if cfg.DingTalk.Enabled {
		t.Error("expected DingTalk.Enabled=false when PushMode=disabled")
	}
}

func TestApplyDefaults_PushMode_MigrationFromFeishuEnabled(t *testing.T) {
	tmpDir := setupTestBinDir(t)
	_ = tmpDir

	cfg := Config{}
	cfg.Feishu.Enabled = true
	ApplyDefaults(&cfg, nil)

	if cfg.PushMode != "feishu" {
		t.Errorf("expected PushMode=feishu when Feishu.Enabled=true, got %q", cfg.PushMode)
	}
	if !cfg.Feishu.Enabled {
		t.Error("expected Feishu.Enabled=true when PushMode=feishu")
	}
	if cfg.DingTalk.Enabled {
		t.Error("expected DingTalk.Enabled=false when PushMode=feishu")
	}
}

func TestApplyDefaults_PushMode_ExplicitFeishuSyncsEnabled(t *testing.T) {
	tmpDir := setupTestBinDir(t)
	_ = tmpDir

	cfg := Config{}
	cfg.PushMode = "feishu"
	ApplyDefaults(&cfg, nil)

	if cfg.PushMode != "feishu" {
		t.Errorf("expected PushMode=feishu, got %q", cfg.PushMode)
	}
	if !cfg.Feishu.Enabled {
		t.Error("expected Feishu.Enabled=true when PushMode=feishu")
	}
	if cfg.DingTalk.Enabled {
		t.Error("expected DingTalk.Enabled=false when PushMode=feishu")
	}
}
