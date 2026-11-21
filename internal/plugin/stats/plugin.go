// Package stats 是用量统计功能的插件声明。
//
// 纯偏好型：恒 enabled=true。接口（用量统计 + git 统计）由本包自己挂，
// 见 routes.go。
package stats

import "clawbench/internal/plugins"

// Register 把用量统计功能登记进注册表。
func Register() {
	plugins.Register(&plugins.Plugin{
		ID:        "stats",
		DefaultOn: true,
		// 用量统计页：token/积分账本 + git 统计。
		APIPrefixes: []string{"/api/usage/", "/api/git/stats", "/api/git/cloc"},
		MountRoutes: MountStatsRoutes,
	})
}
