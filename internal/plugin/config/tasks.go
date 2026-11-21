package config

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/model"
)

// Tasks 的配置声明。
var Tasks = &Feature{
	ID: "tasks",
	Fields: map[string]bool{
		"tasks.enabled":           true, // 双向：关掉取消运行中的执行，打开重排 cron
		"tasks.retention_enabled": true,
		"tasks.retention_days":    true,
		"tasks.notify_on_success": true,
		"tasks.notify_on_failure": true,
		"tasks.timezone":          true, // 决定「每天 9 点」到底是什么时候
	},
	Validate: validateTasks,
	Defaults: defaultTasks,
	Apply:    applyTasks,
}

func validateTasks(patch map[string]any) error {
	tasksCfg, ok := patch["tasks"].(map[string]any)
	if !ok {
		return nil
	}
	// 0 would mean "delete everything today"; a retention window needs at least
	// one day. The cap mirrors ApplyDefaults.
	if v, ok := tasksCfg["retention_days"].(float64); ok && (v < 1 || v > model.MaxTaskRetentionDays) {
		return fmt.Errorf("tasks.retention_days must be between 1 and %d", model.MaxTaskRetentionDays)
	}
	if v, ok := tasksCfg["timezone"].(string); ok {
		if tz := strings.TrimSpace(v); tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				return fmt.Errorf("tasks.timezone must be a valid IANA timezone (e.g. Asia/Shanghai): %w", err)
			}
		}
	}
	return nil
}

// The destructive side effects (cancelling runs) run AFTER the config write
// succeeds — see applyPluginDisableEffects / TasksSwitchEffect.
func applyTasks(cfg *model.Config, patch map[string]any) {
	tasksMap, ok := patch["tasks"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := tasksMap["enabled"].(bool); ok {
		cfg.Tasks.Enabled = v
		// Synced here rather than in applyHotReloadGlobals so an unrelated PATCH
		// (which may run against a zero-valued ConfigInstance in tests) cannot
		// silently flip the switch.
		model.TasksEnabled = v
	}
	if v, ok := tasksMap["retention_enabled"].(bool); ok {
		cfg.Tasks.RetentionEnabled = v
	}
	if v, ok := tasksMap["retention_days"].(float64); ok {
		cfg.Tasks.RetentionDays = int(v)
	}
	if v, ok := tasksMap["notify_on_success"].(bool); ok {
		cfg.Tasks.NotifyOnSuccess = v
		model.FeaturePolicy.TasksNotifyOnSuccess = v
	}
	if v, ok := tasksMap["notify_on_failure"].(bool); ok {
		cfg.Tasks.NotifyOnFailure = v
		model.FeaturePolicy.TasksNotifyOnFailure = v
	}
	if v, ok := tasksMap["timezone"].(string); ok {
		cfg.Tasks.Timezone = strings.TrimSpace(v)
	}
}

// Tasks (scheduled tasks). The master switch defaults to ON: a bool zero value
// would silently stop every existing schedule for an install that never wrote
// the key.
func defaultTasks(cfg *model.Config, present func(string) bool) {
	if !present("tasks.enabled") {
		cfg.Tasks.Enabled = true
	}
	// Retention defaults to OFF (deleting history is irreversible), but the day
	// count defaults to 30 anyway so flipping the switch has an immediate effect
	// instead of meaning "keep forever".
	if cfg.Tasks.RetentionDays <= 0 {
		cfg.Tasks.RetentionDays = 30
	}
	if cfg.Tasks.RetentionDays > model.MaxTaskRetentionDays {
		cfg.Tasks.RetentionDays = model.MaxTaskRetentionDays
	}
	// Notification toggles default to ON: a bool zero value would silently mute
	// every task notification on an install that never wrote the keys.
	if !present("tasks.notify_on_success") {
		cfg.Tasks.NotifyOnSuccess = true
	}
	if !present("tasks.notify_on_failure") {
		cfg.Tasks.NotifyOnFailure = true
	}
	// An unknown zone is a bad preference, not a broken install: fall back to the
	// server's local zone (the pre-existing behavior) and keep starting.
	cfg.Tasks.Timezone = strings.TrimSpace(cfg.Tasks.Timezone)
	if cfg.Tasks.Timezone != "" {
		if _, err := time.LoadLocation(cfg.Tasks.Timezone); err != nil {
			slog.Warn("tasks.timezone is not a known IANA zone, using server local time",
				slog.String("timezone", cfg.Tasks.Timezone), slog.String("err", err.Error()))
			cfg.Tasks.Timezone = ""
		}
	}
}
