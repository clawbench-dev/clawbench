package stats

import "clawbench/internal/plugins"

// MountStatsRoutes 挂载用量统计功能的 HTTP 接口。
//
// 由 plugins 注册表驱动：handler.RegisterRoutes 构造好 Mount 之后遍历
// plugins.All()，调到本函数（见 handler/mount_builtin.go）。
func MountStatsRoutes(m plugins.Mount) {
	m.Auth("/api/usage/stats", ServeUsageStats)
	m.Auth("/api/usage/meter", ServeUsageMeter)
	m.Auth("/api/git/stats", ServeGitStats)
	m.Auth("/api/git/cloc", ServeGitCloc)
}
