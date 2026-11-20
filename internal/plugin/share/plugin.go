// Package share 是分享功能的插件声明。
package share

import (
	"clawbench/internal/model"
	"clawbench/internal/plugins"
)

// Register 把分享功能登记进注册表。
func Register() {
	plugins.Register(&plugins.Plugin{
		ID:        "share",
		Section:   "share",
		Enabled:   func() bool { return model.ShareEnabled },
		DefaultOn: true,
		// "/api/share/" 规范化后同时覆盖精确的 "/api/share" 与 "/api/share/list"
		// 等子路径。公开的 /api/share/{token} 不在鉴权链上，注册在
		// registerPublic，永远不进 Gate——分享关闭时旧链接由服务端返回 404，
		// 而不是 409。
		APIPrefixes: []string{"/api/share/"},
		MountRoutes: MountShareRoutes,
		ConfigFields: map[string]bool{
			"share.enabled":           true,
			"share.allow_files":       true,
			"share.allow_sessions":    true,
			"share.allow_meetings":    true,
			"share.allow_download":    true,
			"share.link_ttl_days":     true,
			"share.show_project_name": true,
		},
		// 关闭即撤销：链接是服务端签发的凭证，关掉必须让旧链接立刻失效。
		// 重新打开不会恢复已撤销的链接——那是"撤销"唯一诚实的读法。
		OnDisable: ShareDisableEffect,
	})
}
