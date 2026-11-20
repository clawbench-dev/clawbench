package handler

import "clawbench/internal/plugins"

// builtinConfigFields 是内置功能**允许被 PATCH /api/config 修改**的配置字段。
//
// 与 PatchableConfigPaths（无主字段）互补：那张表放不属于任何功能的字段，
// 这张表放"属于某个功能"的字段。
//
// # 为什么在这里，而不是由 plugins.Plugin.ConfigFields 提供
//
// 与 mountBuiltinRoutes 同一条理由：配置字段是**编译期事实**，注册表是
// 运行时状态。若白名单建立在插件注册表之上，handler 的单测（注册表为空）
// 会把每一个功能字段都判成非法，几十个测试同时红——而生产上恰好正常。
// 那种"只在测试里坏"的设计会逼着每个配置测试手写一遍插件注册，最终没人
// 愿意加测试。
//
// 插件侧仍然保留 ConfigFields，职责是**声明归属**：哪个字段属于哪个功能，
// 供 /api/features 与守卫测试核对。两份清单的一致性由 cmd/server 的
// TestPluginRegistry_ConfigFieldsMatchHandler 钉住——漏一边会红。
//
// 值是 true 而不是说明文字：说明属于文档，写进字符串会被 i18n 守卫当成
// "未翻译的用户可见文案"拦下（Go 源码里的中文要么进 locales，要么登记为
// 非用户可见）。字段名大多自解释；真正需要解释的例外见下面行内注释。
var builtinConfigFields = map[string]bool{
	// ── forge ──────────────────────────────────────────────────────────
	"forge.enabled":           true,
	"forge.insecure_tls":      true, // 自建 GitLab 常用自签证书，必须能关掉校验
	"forge.pause_event_tasks": true, // 事件触发任务的总暂停
	"forge.notify.opened":     true,
	"forge.notify.closed":     true,
	"forge.notify.merged":     true,
	"forge.notify.reopened":   true,
	"forge.notify.commented":  true,
	"forge.notify.pipeline":   true,

	// ── kb ─────────────────────────────────────────────────────────────
	"kb.enabled":               true,
	"kb.auto_inject":           true,
	"kb.inject_top_n":          true,
	"kb.inject_max_chars":      true, // 防止一篇长文吃掉整个上下文窗口
	"kb.inject_min_similarity": true,
	"kb.max_file_mb":           true,
	"kb.max_text_mb":           true,
	"kb.poll_interval":         true,
	"kb.embed_batch_size":      true,

	// ── mail ───────────────────────────────────────────────────────────
	"mail.enabled":               true,
	"mail.notify":                true,
	"mail.sync_interval_minutes": true,
	"mail.sync_window":           true,
	"mail.body_prefetch":         true,

	// ── share ──────────────────────────────────────────────────────────
	"share.enabled":           true,
	"share.allow_files":       true,
	"share.allow_sessions":    true,
	"share.allow_meetings":    true,
	"share.allow_download":    true,
	"share.link_ttl_days":     true,
	"share.show_project_name": true, // 隐私：链接页是否显示项目名

	// ── tasks ──────────────────────────────────────────────────────────
	"tasks.enabled":           true, // 双向：关掉取消运行中的执行，打开重排 cron
	"tasks.retention_enabled": true,
	"tasks.retention_days":    true,
	"tasks.notify_on_success": true,
	"tasks.notify_on_failure": true,
	"tasks.timezone":          true, // 决定「每天 9 点」到底是什么时候

	// ── terminal ───────────────────────────────────────────────────────
	"terminal.enabled":      true,
	"terminal.idle_timeout": true, // 0 表示不断开
	"terminal.max_sessions": true,
	"terminal.buffer_lines": true,
}

// IsBuiltinConfigField 报告 path 是否是某个内置功能声明的可改字段。
func IsBuiltinConfigField(path string) bool {
	return builtinConfigFields[path]
}

// AllPatchablePaths 返回全部可 PATCH 的配置路径（三个来源的并集）。
//
// 供守卫测试做"某个对象下面有没有可改字段"这类前缀判断——只查
// PatchableConfigPaths 会漏掉功能字段，把 forge.notify 这类中间层判成孤儿。
func AllPatchablePaths() map[string]bool {
	out := make(map[string]bool, len(PatchableConfigPaths)+len(builtinConfigFields))
	for p := range PatchableConfigPaths {
		out[p] = true
	}
	for p := range builtinConfigFields {
		out[p] = true
	}
	for p := range plugins.ConfigPaths() {
		out[p] = true
	}
	return out
}
