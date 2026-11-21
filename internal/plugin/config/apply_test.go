package config

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- registry helpers ----------

func TestAll_ContainsEveryFeatureAndIsOrdered(t *testing.T) {
	all := All()
	require.Len(t, all, 6)
	assert.Equal(t, []string{"forge", "kb", "mail", "share", "tasks", "terminal"},
		[]string{all[0].ID, all[1].ID, all[2].ID, all[3].ID, all[4].ID, all[5].ID})
}

func TestByID(t *testing.T) {
	assert.Same(t, Mail, ByID("mail"))
	assert.Nil(t, ByID("nope"))
}

func TestFields(t *testing.T) {
	assert.True(t, Fields("kb")["kb.enabled"])
	assert.Nil(t, Fields("nope"), "unknown id must return nil, not an empty map")
}

func TestOwns(t *testing.T) {
	assert.True(t, Owns("forge.notify.merged"))
	assert.True(t, Owns("terminal.buffer_lines"))
	assert.False(t, Owns("server.port"), "host-owned keys are not a feature's")
	assert.False(t, Owns(""))
}

func TestAllFields_IsUnionOfEveryFeature(t *testing.T) {
	union := AllFields()
	for _, f := range All() {
		for path := range f.Fields {
			assert.Truef(t, union[path], "%s missing from AllFields", path)
		}
	}
	assert.False(t, union["not.a.feature"])
}

// ---------- Validate / Apply dispatch ----------

func TestValidate_ReturnsFirstErrorInRegistrationOrder(t *testing.T) {
	// Forge has no validator, KB comes before Mail: both are invalid, KB wins.
	patch := map[string]any{
		"kb":   map[string]any{"inject_top_n": float64(0)},
		"mail": map[string]any{"sync_interval_minutes": float64(0)},
	}
	err := Validate(patch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kb.inject_top_n")
}

func TestValidate_NoValidatorsFiringIsNil(t *testing.T) {
	require.NoError(t, Validate(map[string]any{}))
	require.NoError(t, Validate(map[string]any{"server": map[string]any{"port": float64(1)}}))
}

func TestApply_AppliesEveryFeatureSection(t *testing.T) {
	cfg := model.Config{}
	Apply(&cfg, map[string]any{
		"terminal": map[string]any{"max_sessions": float64(7)},
		"mail":     map[string]any{"sync_window": float64(42)},
	})
	assert.Equal(t, 7, cfg.Terminal.MaxSessions)
	assert.Equal(t, 42, cfg.Mail.SyncWindow)
}

// ---------- Forge ----------

func TestApplyForge_AllFieldsAndNotify(t *testing.T) {
	cfg := model.Config{}
	applyForge(&cfg, map[string]any{"forge": map[string]any{
		"enabled":           true,
		"insecure_tls":      true,
		"pause_event_tasks": true,
		"notify": map[string]any{
			"opened":    true,
			"closed":    true,
			"merged":    true,
			"reopened":  true,
			"commented": true,
			"pipeline":  true,
		},
	}})
	assert.True(t, cfg.Forge.Enabled)
	assert.True(t, cfg.Forge.InsecureTLS)
	assert.True(t, cfg.Forge.PauseEventTasks)
	assert.True(t, cfg.Forge.Notify.Opened)
	assert.True(t, cfg.Forge.Notify.Closed)
	assert.True(t, cfg.Forge.Notify.Merged)
	assert.True(t, cfg.Forge.Notify.Reopened)
	assert.True(t, cfg.Forge.Notify.Commented)
	assert.True(t, cfg.Forge.Notify.Pipeline)
}

func TestApplyForge_ExplicitFalseAndWrongTypesAreIgnored(t *testing.T) {
	cfg := model.Config{Forge: model.ForgeConfig{Enabled: true, InsecureTLS: true, PauseEventTasks: true}}
	applyForge(&cfg, map[string]any{"forge": map[string]any{
		"enabled":           false,
		"insecure_tls":      "yes", // wrong type: leave the existing value
		"pause_event_tasks": false,
	}})
	assert.False(t, cfg.Forge.Enabled)
	assert.True(t, cfg.Forge.InsecureTLS, "wrong-typed value must not overwrite")
	assert.False(t, cfg.Forge.PauseEventTasks)
}

func TestApplyForge_IgnoresNonMapSections(t *testing.T) {
	cfg := model.Config{Forge: model.ForgeConfig{Enabled: true}}
	applyForge(&cfg, map[string]any{"forge": "nope"})
	assert.True(t, cfg.Forge.Enabled)
	applyForge(&cfg, map[string]any{})
	assert.True(t, cfg.Forge.Enabled)
}

// ---------- KB ----------

func TestValidateKB_Bounds(t *testing.T) {
	cases := []struct {
		name    string
		patch   map[string]any
		wantErr string
	}{
		{"top_n too low", map[string]any{"inject_top_n": float64(0)}, "kb.inject_top_n"},
		{"top_n too high", map[string]any{"inject_top_n": float64(21)}, "kb.inject_top_n"},
		{"max_chars too low", map[string]any{"inject_max_chars": float64(199)}, "kb.inject_max_chars"},
		{"similarity negative", map[string]any{"inject_min_similarity": float64(-0.1)}, "kb.inject_min_similarity"},
		{"similarity above one", map[string]any{"inject_min_similarity": float64(1.1)}, "kb.inject_min_similarity"},
		{"file_mb too low", map[string]any{"max_file_mb": float64(0)}, "kb.max_file_mb"},
		{"file_mb too high", map[string]any{"max_file_mb": float64(1025)}, "kb.max_file_mb"},
		{"text_mb too low", map[string]any{"max_text_mb": float64(0)}, "kb.max_text_mb"},
		{"text_mb too high", map[string]any{"max_text_mb": float64(513)}, "kb.max_text_mb"},
		{"batch too low", map[string]any{"embed_batch_size": float64(0)}, "kb.embed_batch_size"},
		{"batch too high", map[string]any{"embed_batch_size": float64(129)}, "kb.embed_batch_size"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateKB(map[string]any{"kb": tc.patch})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidateKB_AcceptsInRangeAndIgnoresOtherTypes(t *testing.T) {
	require.NoError(t, validateKB(map[string]any{"kb": map[string]any{
		"inject_top_n":          float64(1),
		"inject_max_chars":      float64(200),
		"inject_min_similarity": float64(1),
		"max_file_mb":           float64(1024),
		"max_text_mb":           float64(512),
		"embed_batch_size":      float64(128),
	}}))
	// A string where a number is expected is not a bounds problem (the JSON
	// decoder already rejected it); the validator must not panic or error.
	require.NoError(t, validateKB(map[string]any{"kb": map[string]any{"inject_top_n": "many"}}))
	require.NoError(t, validateKB(map[string]any{"kb": "nope"}))
	require.NoError(t, validateKB(map[string]any{}))
}

func TestApplyKB_AllFields(t *testing.T) {
	cfg := model.Config{}
	applyKB(&cfg, map[string]any{"kb": map[string]any{
		"enabled":               true,
		"auto_inject":           true,
		"inject_top_n":          float64(3),
		"inject_max_chars":      float64(900),
		"inject_min_similarity": 0.5,
		"max_file_mb":           float64(10),
		"max_text_mb":           float64(5),
		"poll_interval":         "5s",
		"embed_batch_size":      float64(8),
	}})
	assert.True(t, cfg.KB.Enabled)
	assert.True(t, cfg.KB.AutoInject)
	assert.Equal(t, 3, cfg.KB.InjectTopN)
	assert.Equal(t, 900, cfg.KB.InjectMaxChars)
	assert.InDelta(t, 0.5, cfg.KB.InjectMinSimilarity, 1e-9)
	assert.Equal(t, 10, cfg.KB.MaxFileMB)
	assert.Equal(t, 5, cfg.KB.MaxTextMB)
	assert.Equal(t, "5s", cfg.KB.PollInterval)
	assert.Equal(t, 8, cfg.KB.EmbedBatchSize)
}

func TestApplyKB_IgnoresNonMapAndWrongTypes(t *testing.T) {
	cfg := model.Config{KB: model.KBConfig{InjectTopN: 4}}
	applyKB(&cfg, map[string]any{"kb": "nope"})
	assert.Equal(t, 4, cfg.KB.InjectTopN)
	applyKB(&cfg, map[string]any{"kb": map[string]any{"inject_top_n": "3"}})
	assert.Equal(t, 4, cfg.KB.InjectTopN)
}

// ---------- Mail ----------

func TestValidateMail_Bounds(t *testing.T) {
	cases := []struct {
		name    string
		patch   map[string]any
		wantErr string
	}{
		{"interval zero", map[string]any{"sync_interval_minutes": float64(0)}, "mail.sync_interval_minutes"},
		{"interval too high", map[string]any{"sync_interval_minutes": float64(121)}, "mail.sync_interval_minutes"},
		{"window too low", map[string]any{"sync_window": float64(19)}, "mail.sync_window"},
		{"window too high", map[string]any{"sync_window": float64(501)}, "mail.sync_window"},
		{"prefetch negative", map[string]any{"body_prefetch": float64(-1)}, "mail.body_prefetch"},
		{"prefetch too high", map[string]any{"body_prefetch": float64(51)}, "mail.body_prefetch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMail(map[string]any{"mail": tc.patch})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestValidateMail_AcceptsEdgesAndZeroPrefetch(t *testing.T) {
	require.NoError(t, validateMail(map[string]any{"mail": map[string]any{
		"sync_interval_minutes": float64(120),
		"sync_window":           float64(20),
		"body_prefetch":         float64(0), // 0 = do not prefetch
	}}))
	require.NoError(t, validateMail(map[string]any{"mail": "nope"}))
	require.NoError(t, validateMail(map[string]any{}))
}

func TestApplyMail_AllFields(t *testing.T) {
	cfg := model.Config{}
	applyMail(&cfg, map[string]any{"mail": map[string]any{
		"enabled":               true,
		"notify":                true,
		"sync_interval_minutes": float64(15),
		"sync_window":           float64(250),
		"body_prefetch":         float64(0),
	}})
	assert.True(t, cfg.Mail.Enabled)
	assert.True(t, cfg.Mail.Notify)
	assert.Equal(t, 15, cfg.Mail.SyncIntervalMinutes)
	assert.Equal(t, 250, cfg.Mail.SyncWindow)
	assert.Equal(t, 0, cfg.Mail.BodyPrefetch)
}

func TestApplyMail_IgnoresNonMap(t *testing.T) {
	cfg := model.Config{Mail: model.MailConfig{SyncWindow: 100}}
	applyMail(&cfg, map[string]any{"mail": float64(1)})
	assert.Equal(t, 100, cfg.Mail.SyncWindow)
}

// ---------- Share ----------

func TestValidateShare_Bounds(t *testing.T) {
	require.Error(t, validateShare(map[string]any{"share": map[string]any{"link_ttl_days": float64(-1)}}))
	require.Error(t, validateShare(map[string]any{"share": map[string]any{"link_ttl_days": float64(model.MaxShareLinkTTLDays + 1)}}))
	require.NoError(t, validateShare(map[string]any{"share": map[string]any{"link_ttl_days": float64(0)}}), "0 = never expire")
	require.NoError(t, validateShare(map[string]any{"share": map[string]any{"link_ttl_days": float64(model.MaxShareLinkTTLDays)}}))
	require.NoError(t, validateShare(map[string]any{"share": "nope"}))
}

func TestApplyShare_AllFieldsSyncGlobals(t *testing.T) {
	origShare, origPolicy := model.ShareEnabled, model.FeaturePolicy
	t.Cleanup(func() { model.ShareEnabled, model.FeaturePolicy = origShare, origPolicy })

	cfg := model.Config{}
	applyShare(&cfg, map[string]any{"share": map[string]any{
		"enabled":           true,
		"allow_files":       true,
		"allow_sessions":    true,
		"allow_meetings":    true,
		"allow_download":    true,
		"link_ttl_days":     float64(7),
		"show_project_name": true,
	}})
	assert.True(t, cfg.Share.Enabled)
	assert.True(t, model.ShareEnabled)
	assert.True(t, cfg.Share.AllowFiles)
	assert.True(t, model.FeaturePolicy.ShareAllowFiles)
	assert.True(t, cfg.Share.AllowSessions)
	assert.True(t, model.FeaturePolicy.ShareAllowSessions)
	assert.True(t, cfg.Share.AllowMeetings)
	assert.True(t, model.FeaturePolicy.ShareAllowMeetings)
	assert.True(t, cfg.Share.AllowDownload)
	assert.True(t, model.FeaturePolicy.ShareAllowDownload)
	assert.Equal(t, 7, cfg.Share.LinkTTLDays)
	assert.True(t, cfg.Share.ShowProjectName)
	assert.True(t, model.FeaturePolicy.ShareShowProjectName)
}

func TestApplyShare_ExplicitFalseSyncsGlobals(t *testing.T) {
	origShare, origPolicy := model.ShareEnabled, model.FeaturePolicy
	t.Cleanup(func() { model.ShareEnabled, model.FeaturePolicy = origShare, origPolicy })

	model.ShareEnabled = true
	model.FeaturePolicy.ShareAllowFiles = true
	cfg := model.Config{Share: model.ShareConfig{Enabled: true, AllowFiles: true}}
	applyShare(&cfg, map[string]any{"share": map[string]any{
		"enabled":     false,
		"allow_files": false,
	}})
	assert.False(t, cfg.Share.Enabled)
	assert.False(t, model.ShareEnabled)
	assert.False(t, cfg.Share.AllowFiles)
	assert.False(t, model.FeaturePolicy.ShareAllowFiles)
}

func TestApplyShare_IgnoresNonMap(t *testing.T) {
	cfg := model.Config{Share: model.ShareConfig{LinkTTLDays: 3}}
	applyShare(&cfg, map[string]any{"share": "nope"})
	assert.Equal(t, 3, cfg.Share.LinkTTLDays)
}

// ---------- Tasks ----------

func TestValidateTasks_RetentionAndTimezone(t *testing.T) {
	require.Error(t, validateTasks(map[string]any{"tasks": map[string]any{"retention_days": float64(0)}}))
	require.Error(t, validateTasks(map[string]any{"tasks": map[string]any{"retention_days": float64(model.MaxTaskRetentionDays + 1)}}))
	require.NoError(t, validateTasks(map[string]any{"tasks": map[string]any{"retention_days": float64(1)}}))
	require.NoError(t, validateTasks(map[string]any{"tasks": map[string]any{"retention_days": float64(model.MaxTaskRetentionDays)}}))

	require.NoError(t, validateTasks(map[string]any{"tasks": map[string]any{"timezone": "Asia/Shanghai"}}))
	require.NoError(t, validateTasks(map[string]any{"tasks": map[string]any{"timezone": "  "}}), "blank means server local time")
	err := validateTasks(map[string]any{"tasks": map[string]any{"timezone": "Mars/Olympus"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tasks.timezone")
	require.NoError(t, validateTasks(map[string]any{"tasks": "nope"}))
}

func TestApplyTasks_AllFieldsSyncGlobals(t *testing.T) {
	origTasks, origPolicy := model.TasksEnabled, model.FeaturePolicy
	t.Cleanup(func() { model.TasksEnabled, model.FeaturePolicy = origTasks, origPolicy })

	cfg := model.Config{}
	applyTasks(&cfg, map[string]any{"tasks": map[string]any{
		"enabled":           true,
		"retention_enabled": true,
		"retention_days":    float64(14),
		"notify_on_success": true,
		"notify_on_failure": true,
		"timezone":          "  Asia/Shanghai  ",
	}})
	assert.True(t, cfg.Tasks.Enabled)
	assert.True(t, model.TasksEnabled)
	assert.True(t, cfg.Tasks.RetentionEnabled)
	assert.Equal(t, 14, cfg.Tasks.RetentionDays)
	assert.True(t, cfg.Tasks.NotifyOnSuccess)
	assert.True(t, model.FeaturePolicy.TasksNotifyOnSuccess)
	assert.True(t, cfg.Tasks.NotifyOnFailure)
	assert.True(t, model.FeaturePolicy.TasksNotifyOnFailure)
	assert.Equal(t, "Asia/Shanghai", cfg.Tasks.Timezone, "surrounding whitespace is trimmed")
}

func TestApplyTasks_IgnoresNonMap(t *testing.T) {
	cfg := model.Config{Tasks: model.TasksConfig{RetentionDays: 30}}
	applyTasks(&cfg, map[string]any{"tasks": "nope"})
	assert.Equal(t, 30, cfg.Tasks.RetentionDays)
}

// ---------- Terminal ----------

func TestApplyTerminal_AllFields(t *testing.T) {
	cfg := model.Config{}
	applyTerminal(&cfg, map[string]any{"terminal": map[string]any{
		"enabled":      true,
		"idle_timeout": "30m",
		"max_sessions": float64(5),
		"buffer_lines": float64(500),
	}})
	assert.True(t, cfg.Terminal.Enabled)
	assert.Equal(t, "30m", cfg.Terminal.IdleTimeout)
	assert.Equal(t, 5, cfg.Terminal.MaxSessions)
	assert.Equal(t, 500, cfg.Terminal.BufferLines)
}

func TestApplyTerminal_IgnoresNonMapAndWrongTypes(t *testing.T) {
	cfg := model.Config{Terminal: model.TerminalConfig{MaxSessions: 10}}
	applyTerminal(&cfg, map[string]any{"terminal": "nope"})
	assert.Equal(t, 10, cfg.Terminal.MaxSessions)
	applyTerminal(&cfg, map[string]any{"terminal": map[string]any{"max_sessions": "5"}})
	assert.Equal(t, 10, cfg.Terminal.MaxSessions)
}
