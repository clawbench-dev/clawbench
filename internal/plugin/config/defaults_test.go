package config

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// 这些用例原先住在 internal/model：功能的默认值搬进本包后，它们跟着搬过来。
// 调用的仍是同一个 ApplyDefaults，只是它现在只处理功能字段。

func TestConfigDefaults_TerminalPresenceFalse(t *testing.T) {
	// When terminal.enabled is NOT in presence, should default to true
	cfg := model.Config{}
	ApplyDefaults(&cfg, nil)

	if !cfg.Terminal.Enabled {
		t.Error("Terminal.Enabled should default to true when absent from config")
	}
}

func TestConfigDefaults_TerminalPresenceExplicit(t *testing.T) {
	// When terminal.enabled IS in presence and set to false, should stay false
	cfg := model.Config{}
	presence := map[string]bool{"terminal.enabled": true}
	cfg.Terminal.Enabled = false
	ApplyDefaults(&cfg, presence)

	if cfg.Terminal.Enabled {
		t.Error("Terminal.Enabled should stay false when explicitly set")
	}
}

// The two new master switches are existing features, so an install that never
// wrote the keys must keep them on. A bool zero value would silently stop every
// schedule and revoke every share link on upgrade.
func TestConfigDefaults_TasksAndShareDefaultEnabled(t *testing.T) {
	cfg := model.Config{}
	ApplyDefaults(&cfg, nil)

	if !cfg.Tasks.Enabled {
		t.Error("Tasks.Enabled should default to true when absent from config")
	}
	if !cfg.Share.Enabled {
		t.Error("Share.Enabled should default to true when absent from config")
	}
}

// An explicit false must survive: it is the user switching the feature off.
func TestConfigDefaults_TasksAndShareExplicitFalse(t *testing.T) {
	cfg := model.Config{}
	presence := map[string]bool{"tasks.enabled": true, "share.enabled": true}
	ApplyDefaults(&cfg, presence)

	if cfg.Tasks.Enabled {
		t.Error("Tasks.Enabled should stay false when explicitly set")
	}
	if cfg.Share.Enabled {
		t.Error("Share.Enabled should stay false when explicitly set")
	}
}

// The sub-switches default to their documented values: notifications and the
// share permissions are ON, retention is OFF (deleting history is opt-in), and
// the tuning numbers have usable defaults.
func TestConfigDefaults_TasksAndShareSubSwitches(t *testing.T) {
	cfg := model.Config{}
	ApplyDefaults(&cfg, nil)

	if cfg.Tasks.RetentionEnabled {
		t.Error("Tasks.RetentionEnabled should default to false")
	}
	if cfg.Tasks.RetentionDays != 30 {
		t.Errorf("Tasks.RetentionDays = %d, want 30", cfg.Tasks.RetentionDays)
	}
	if !cfg.Tasks.NotifyOnSuccess || !cfg.Tasks.NotifyOnFailure {
		t.Error("task notification switches should default to true")
	}
	if cfg.Tasks.Timezone != "" {
		t.Errorf("Tasks.Timezone = %q, want empty (server local)", cfg.Tasks.Timezone)
	}
	if !cfg.Share.AllowFiles || !cfg.Share.AllowSessions || !cfg.Share.AllowMeetings {
		t.Error("share allow_* switches should default to true")
	}
	if !cfg.Share.AllowDownload {
		t.Error("Share.AllowDownload should default to true")
	}
	if !cfg.Share.ShowProjectName {
		t.Error("Share.ShowProjectName should default to true")
	}
	if cfg.Share.LinkTTLDays != 0 {
		t.Errorf("Share.LinkTTLDays = %d, want 0 (never expire)", cfg.Share.LinkTTLDays)
	}
}

// Explicit false must survive for every default-ON sub-switch.
func TestConfigDefaults_TasksAndShareSubSwitchesExplicitFalse(t *testing.T) {
	cfg := model.Config{}
	presence := map[string]bool{
		"tasks.notify_on_success": true,
		"tasks.notify_on_failure": true,
		"share.allow_files":       true,
		"share.allow_sessions":    true,
		"share.allow_meetings":    true,
		"share.allow_download":    true,
		"share.show_project_name": true,
	}
	ApplyDefaults(&cfg, presence)

	if cfg.Tasks.NotifyOnSuccess || cfg.Tasks.NotifyOnFailure {
		t.Error("explicit false task notification switches must survive")
	}
	if cfg.Share.AllowFiles || cfg.Share.AllowSessions || cfg.Share.AllowMeetings {
		t.Error("explicit false share allow_* switches must survive")
	}
	if cfg.Share.AllowDownload || cfg.Share.ShowProjectName {
		t.Error("explicit false download / project-name switches must survive")
	}
}

// A hand-edited unknown zone must not stop the server: it falls back to local
// time. PATCH rejects the same value with a 400, so this is the load path only.
func TestConfigDefaults_TasksTimezoneFallsBackOnInvalid(t *testing.T) {
	cfg := model.Config{Tasks: model.TasksConfig{Timezone: "Not/AZone"}}
	ApplyDefaults(&cfg, nil)
	if cfg.Tasks.Timezone != "" {
		t.Errorf("invalid timezone should fall back to empty, got %q", cfg.Tasks.Timezone)
	}

	cfg = model.Config{Tasks: model.TasksConfig{Timezone: "Asia/Shanghai"}}
	ApplyDefaults(&cfg, nil)
	if cfg.Tasks.Timezone != "Asia/Shanghai" {
		t.Errorf("valid timezone must be kept, got %q", cfg.Tasks.Timezone)
	}
}

func TestConfigDefaults_TerminalDefaults(t *testing.T) {
	cfg := model.Config{}
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

// TestApplyDefaults_MailEnabledPresence covers the mail master switch's default.
// Same bool zero-value trap as forge: an install that never wrote the key must
// keep the mail client it already had.
func TestConfigDefaults_MailEnabledPresence(t *testing.T) {
	cfg := model.Config{}
	ApplyDefaults(&cfg, nil)
	if !cfg.Mail.Enabled {
		t.Error("Mail.Enabled should default to true when absent from config")
	}

	// Explicitly false → respected, not overwritten.
	cfg = model.Config{}
	ApplyDefaults(&cfg, map[string]bool{"mail.enabled": true})
	if cfg.Mail.Enabled {
		t.Error("Mail.Enabled must stay false when explicitly set")
	}

	// The whole section absent (empty presence map) still defaults ON.
	cfg = model.Config{}
	ApplyDefaults(&cfg, map[string]bool{})
	if !cfg.Mail.Enabled {
		t.Error("Mail.Enabled should default to true when the mail section is absent")
	}
}

// TestApplyDefaults_MailNotifyPresence covers the new-mail notification switch.
//
// Same bool zero-value trap as the master switch: an install that never wrote
// the key must keep notifications on. An explicit false is a deliberate mute
// and must survive ApplyDefaults.
func TestConfigDefaults_MailNotifyPresence(t *testing.T) {
	cfg := model.Config{}
	ApplyDefaults(&cfg, nil)
	if !cfg.Mail.Notify {
		t.Error("Mail.Notify should default to true when absent from config")
	}

	cfg = model.Config{}
	ApplyDefaults(&cfg, map[string]bool{"mail.notify": true})
	if cfg.Mail.Notify {
		t.Error("Mail.Notify must stay false when explicitly set")
	}
}

// TestApplyDefaults_MailSyncTuning covers the three mail sync knobs.//
// The subtle one is body_prefetch: 0 is a real choice ("do not prefetch"), so
// the presence map must distinguish "user wrote 0" from "key absent" — an
// absent key silently turning prefetching off would look like a broken
// feature, not a default.
func TestConfigDefaults_MailSyncTuning(t *testing.T) {
	cfg := model.Config{}
	ApplyDefaults(&cfg, nil)
	if cfg.Mail.SyncIntervalMinutes != 3 {
		t.Errorf("SyncIntervalMinutes = %d, want 3", cfg.Mail.SyncIntervalMinutes)
	}
	if cfg.Mail.SyncWindow != 100 {
		t.Errorf("SyncWindow = %d, want 100", cfg.Mail.SyncWindow)
	}
	if cfg.Mail.BodyPrefetch != 10 {
		t.Errorf("BodyPrefetch = %d, want 10", cfg.Mail.BodyPrefetch)
	}

	// An explicitly written 0 for body_prefetch must survive: it is the
	// "do not prefetch" value, not a missing field.
	cfg = model.Config{}
	ApplyDefaults(&cfg, map[string]bool{"mail.body_prefetch": true})
	if cfg.Mail.BodyPrefetch != 0 {
		t.Errorf("explicit BodyPrefetch 0 was overwritten to %d", cfg.Mail.BodyPrefetch)
	}

	// The same presence rule keeps a user's own numbers.
	cfg = model.Config{}
	cfg.Mail.SyncIntervalMinutes = 15
	cfg.Mail.SyncWindow = 300
	ApplyDefaults(&cfg, map[string]bool{
		"mail.sync_interval_minutes": true,
		"mail.sync_window":           true,
	})
	if cfg.Mail.SyncIntervalMinutes != 15 || cfg.Mail.SyncWindow != 300 {
		t.Errorf("configured values were overwritten: %d / %d",
			cfg.Mail.SyncIntervalMinutes, cfg.Mail.SyncWindow)
	}
}

// TestApplyDefaults_ForgeEnabledPresence covers the master switch's default.
// Go's bool zero value is false, so without the presence check every existing
// install would read "forge disabled" on upgrade and lose its integration.
func TestConfigDefaults_ForgeEnabledPresence(t *testing.T) {
	// Absent from the config file → default ON.
	cfg := model.Config{}
	ApplyDefaults(&cfg, nil)
	if !cfg.Forge.Enabled {
		t.Error("Forge.Enabled should default to true when absent from config")
	}

	// Explicitly false → respected, not overwritten.
	cfg = model.Config{}
	cfg.Forge.Enabled = false
	ApplyDefaults(&cfg, map[string]bool{"forge.enabled": true})
	if cfg.Forge.Enabled {
		t.Error("Forge.Enabled must stay false when explicitly set")
	}

	// The whole forge section absent (nil presence map) still defaults ON.
	cfg = model.Config{}
	ApplyDefaults(&cfg, map[string]bool{})
	if !cfg.Forge.Enabled {
		t.Error("Forge.Enabled should default to true when the forge section is absent")
	}
}

func TestConfigDefaults_KnowledgeBase(t *testing.T) {
	cfg := model.Config{}
	ApplyDefaults(&cfg, map[string]bool{})
	if !cfg.KB.Enabled {
		t.Fatalf("KB master switch must default to true: %+v", cfg.KB)
	}
	if cfg.KB.AutoInject {
		t.Fatalf("KB auto-inject must default to false (opt-in): %+v", cfg.KB)
	}
	if cfg.KB.InjectTopN != 4 || cfg.KB.InjectMaxChars != 2000 {
		t.Fatalf("unexpected injection defaults: %+v", cfg.KB)
	}
	if cfg.KB.InjectMinSimilarity != 0.35 || cfg.KB.MaxFileMB != 50 || cfg.KB.MaxTextMB != 20 {
		t.Fatalf("unexpected numeric defaults: %+v", cfg.KB)
	}
	if cfg.KB.PollInterval != "2s" || cfg.KB.EmbedBatchSize != 16 {
		t.Fatalf("unexpected worker defaults: %+v", cfg.KB)
	}

	// An explicit false must survive: the presence map is what distinguishes
	// "user turned it off" from "section omitted".
	cfg = model.Config{}
	ApplyDefaults(&cfg, map[string]bool{"kb.enabled": true, "kb.auto_inject": true})
	if cfg.KB.Enabled || cfg.KB.AutoInject {
		t.Fatalf("explicit false was overwritten: %+v", cfg.KB)
	}
}

// TestApplyDefaults_ForgeNotifyDefaultsEnabled is the regression guard for the
// Go bool zero-value trap: without explicit defaulting, every notification
// toggle would silently default to off, contradicting the documented default.
func TestConfigDefaults_ForgeNotifyDefaultsEnabled(t *testing.T) {
	var cfg model.Config
	ApplyDefaults(&cfg, nil)

	assert.True(t, cfg.Forge.Notify.Opened, "opened must default on")
	assert.True(t, cfg.Forge.Notify.Closed, "closed must default on")
	assert.True(t, cfg.Forge.Notify.Merged, "merged must default on")
	assert.True(t, cfg.Forge.Notify.Reopened, "reopened must default on")
	assert.True(t, cfg.Forge.Notify.Commented, "commented must default on")
	assert.True(t, cfg.Forge.Notify.Pipeline, "pipeline must default on")
}

// TestApplyDefaults_ForgeNotifyRespectsExplicitFalse verifies that a user who
// explicitly disables a toggle keeps it disabled — presence must win over the
// default.
func TestConfigDefaults_ForgeNotifyRespectsExplicitFalse(t *testing.T) {
	var cfg model.Config
	ApplyDefaults(&cfg, map[string]bool{
		"forge.notify.commented": true,
	})

	assert.True(t, cfg.Forge.Notify.Opened, "absent toggles still default on")
	assert.False(t, cfg.Forge.Notify.Commented, "an explicitly-present toggle is respected")
}
