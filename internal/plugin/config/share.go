package config

import (
	"fmt"

	"clawbench/internal/model"
)

// Share 的配置声明。
var Share = &Feature{
	ID: "share",
	Fields: map[string]bool{
		"share.enabled":           true,
		"share.allow_files":       true,
		"share.allow_sessions":    true,
		"share.allow_meetings":    true,
		"share.allow_download":    true,
		"share.link_ttl_days":     true,
		"share.show_project_name": true, // 隐私：链接页是否显示项目名
	},
	Validate: validateShare,
	Defaults: defaultShare,
	Apply:    applyShare,
}

func validateShare(patch map[string]any) error {
	shareCfg, ok := patch["share"].(map[string]any)
	if !ok {
		return nil
	}
	// 0 is valid and means "never expire"; negative is not.
	if v, ok := shareCfg["link_ttl_days"].(float64); ok && (v < 0 || v > model.MaxShareLinkTTLDays) {
		return fmt.Errorf("share.link_ttl_days must be between 0 and %d", model.MaxShareLinkTTLDays)
	}
	return nil
}

// The destructive side effect (revoking issued links) runs AFTER the config
// write succeeds — see ShareDisableEffect.
func applyShare(cfg *model.Config, patch map[string]any) {
	shareMap, ok := patch["share"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := shareMap["enabled"].(bool); ok {
		cfg.Share.Enabled = v
		model.ShareEnabled = v
	}
	if v, ok := shareMap["allow_files"].(bool); ok {
		cfg.Share.AllowFiles = v
		model.FeaturePolicy.ShareAllowFiles = v
	}
	if v, ok := shareMap["allow_sessions"].(bool); ok {
		cfg.Share.AllowSessions = v
		model.FeaturePolicy.ShareAllowSessions = v
	}
	if v, ok := shareMap["allow_meetings"].(bool); ok {
		cfg.Share.AllowMeetings = v
		model.FeaturePolicy.ShareAllowMeetings = v
	}
	if v, ok := shareMap["allow_download"].(bool); ok {
		cfg.Share.AllowDownload = v
		model.FeaturePolicy.ShareAllowDownload = v
	}
	if v, ok := shareMap["link_ttl_days"].(float64); ok {
		cfg.Share.LinkTTLDays = int(v)
	}
	if v, ok := shareMap["show_project_name"].(bool); ok {
		cfg.Share.ShowProjectName = v
		model.FeaturePolicy.ShareShowProjectName = v
	}
}

// Share (public share links). Defaults to ON: turning it off is destructive (it
// revokes every issued link), so it must never happen merely because the key is
// absent from an older config.yaml. Per-kind rules and download also default ON.
func defaultShare(cfg *model.Config, present func(string) bool) {
	if !present("share.enabled") {
		cfg.Share.Enabled = true
	}
	if !present("share.allow_files") {
		cfg.Share.AllowFiles = true
	}
	if !present("share.allow_sessions") {
		cfg.Share.AllowSessions = true
	}
	if !present("share.allow_meetings") {
		cfg.Share.AllowMeetings = true
	}
	if !present("share.allow_download") {
		cfg.Share.AllowDownload = true
	}
	if !present("share.show_project_name") {
		cfg.Share.ShowProjectName = true
	}
	// 0 is MEANINGFUL ("never expire"), so only a negative value is clamped.
	if cfg.Share.LinkTTLDays < 0 {
		cfg.Share.LinkTTLDays = 0
	}
	if cfg.Share.LinkTTLDays > model.MaxShareLinkTTLDays {
		cfg.Share.LinkTTLDays = model.MaxShareLinkTTLDays
	}
}
