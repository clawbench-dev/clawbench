package config

import (
	"fmt"

	"clawbench/internal/model"
)

// Mail 的配置声明。
var Mail = &Feature{
	ID: "mail",
	Fields: map[string]bool{
		"mail.enabled":               true,
		"mail.notify":                true,
		"mail.sync_interval_minutes": true,
		"mail.sync_window":           true,
		"mail.body_prefetch":         true,
	},
	Validate: validateMail,
	Defaults: defaultMail,
	Apply:    applyMail,
}

// Mail sync tuning. The PATCH path does not re-run ApplyDefaults, so a value
// accepted here is what GET /api/config echoes back — every bound has to be
// enforced at the edge or the UI and the worker disagree.
func validateMail(patch map[string]any) error {
	mailCfg, ok := patch["mail"].(map[string]any)
	if !ok {
		return nil
	}
	// No "0 = never sync": that is what mail.enabled is for, and a second way to
	// stop syncing would leave users guessing which one is in effect.
	if v, ok := mailCfg["sync_interval_minutes"].(float64); ok && (v < 1 || v > 120) {
		return fmt.Errorf("mail.sync_interval_minutes must be between 1 and 120")
	}
	// Below 20 the list reads as empty after a sync; above 500 every pass pulls
	// a mailbox worth of headers for little benefit.
	if v, ok := mailCfg["sync_window"].(float64); ok && (v < 20 || v > 500) {
		return fmt.Errorf("mail.sync_window must be between 20 and 500")
	}
	// 0 is valid and means "do not prefetch" (metered connections).
	if v, ok := mailCfg["body_prefetch"].(float64); ok && (v < 0 || v > 50) {
		return fmt.Errorf("mail.body_prefetch must be between 0 and 50")
	}
	return nil
}

// The accounts live in the database and are managed through
// /api/mail/accounts, so disabling mail must not (and does not) touch them.
func applyMail(cfg *model.Config, patch map[string]any) {
	mailMap, ok := patch["mail"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := mailMap["enabled"].(bool); ok {
		cfg.Mail.Enabled = v
	}
	if v, ok := mailMap["notify"].(bool); ok {
		cfg.Mail.Notify = v
	}
	if v, ok := mailMap["sync_interval_minutes"].(float64); ok {
		cfg.Mail.SyncIntervalMinutes = int(v)
	}
	if v, ok := mailMap["sync_window"].(float64); ok {
		cfg.Mail.SyncWindow = int(v)
	}
	if v, ok := mailMap["body_prefetch"].(float64); ok {
		cfg.Mail.BodyPrefetch = int(v)
	}
}

// Mail (built-in mail client). Every default here is ON: a bool zero value would
// silently turn mail off, or mute its notifications, for an install that never
// wrote the key.
func defaultMail(cfg *model.Config, present func(string) bool) {
	if !present("mail.enabled") {
		cfg.Mail.Enabled = true
	}
	if !present("mail.notify") {
		cfg.Mail.Notify = true
	}
	// Sync tuning uses presence for a different reason: 0 is a MEANINGFUL value
	// for body_prefetch ("do not prefetch"), so an absent key must not be read
	// as "user chose 0".
	if !present("mail.sync_interval_minutes") {
		cfg.Mail.SyncIntervalMinutes = 3
	}
	if !present("mail.sync_window") {
		cfg.Mail.SyncWindow = 100
	}
	if !present("mail.body_prefetch") {
		cfg.Mail.BodyPrefetch = 10
	}
}
