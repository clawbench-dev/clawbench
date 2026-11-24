package config

import "clawbench/internal/model"

// Forge 的配置声明。
var Forge = &Feature{
	ID: "forge",
	Fields: map[string]bool{
		"forge.enabled":           true,
		"forge.insecure_tls":      true, // 自建 GitLab 常用自签证书，必须能关掉校验
		"forge.pause_event_tasks": true, // 事件触发任务的总暂停
		// 放过凭据账号自己产生的事件（默认关，见 model.ForgeConfig 的说明）
		"forge.allow_self_authored_events": true,
		"forge.notify.opened":              true,
		"forge.notify.closed":              true,
		"forge.notify.merged":              true,
		"forge.notify.reopened":            true,
		"forge.notify.commented":           true,
		"forge.notify.pipeline":            true,
	},
	Defaults: defaultForge,
	Apply:    applyForge,
}

func applyForge(cfg *model.Config, patch map[string]any) {
	forgeMap, ok := patch["forge"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := forgeMap["enabled"].(bool); ok {
		cfg.Forge.Enabled = v
	}
	if v, ok := forgeMap["insecure_tls"].(bool); ok {
		cfg.Forge.InsecureTLS = v
	}
	if v, ok := forgeMap["pause_event_tasks"].(bool); ok {
		cfg.Forge.PauseEventTasks = v
	}
	if v, ok := forgeMap["allow_self_authored_events"].(bool); ok {
		cfg.Forge.AllowSelfAuthoredEvents = v
	}
	if notify, ok := forgeMap["notify"].(map[string]any); ok {
		applyBool := func(key string, target *bool) {
			if v, ok := notify[key].(bool); ok {
				*target = v
			}
		}
		applyBool("opened", &cfg.Forge.Notify.Opened)
		applyBool("closed", &cfg.Forge.Notify.Closed)
		applyBool("merged", &cfg.Forge.Notify.Merged)
		applyBool("reopened", &cfg.Forge.Notify.Reopened)
		applyBool("commented", &cfg.Forge.Notify.Commented)
		applyBool("pipeline", &cfg.Forge.Notify.Pipeline)
	}
}

// Forge (GitHub / GitLab integration). The master switch defaults to ON: a bool
// zero value would silently disable the integration on every install that never
// wrote the key. An explicit `enabled: false` is respected.
func defaultForge(cfg *model.Config, present func(string) bool) {
	if !present("forge.enabled") {
		cfg.Forge.Enabled = true
	}
	// Notification toggles default to ENABLED. Presence distinguishes "user wrote
	// false" from "user omitted the field": absent means default (true).
	defaultTrue := func(key string, target *bool) {
		if !present(key) {
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
