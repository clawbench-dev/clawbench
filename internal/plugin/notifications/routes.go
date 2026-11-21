package notifications

import "clawbench/internal/plugins"

// MountNotificationsRoutes 挂载通知中心的 HTTP 接口。
//
// 由 plugins 注册表驱动：handler.RegisterRoutes 构造好 Mount 之后遍历
// plugins.All()，调到本函数（见 handler/mount_builtin.go）。
func MountNotificationsRoutes(m plugins.Mount) {
	m.Auth("/api/notifications", ServeNotificationsOrClear)
}
