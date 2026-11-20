// Package notifications 是通知中心功能的插件声明。
//
// 纯偏好型：通知中心是只追加的事件日志，隐藏页签只影响入口，服务端照常
// 写入。没有 MountRoutes——/api/notifications 的路由仍在
// handler.RegisterRoutes 里。
package notifications

import "clawbench/internal/plugins"

// Register 把通知中心功能登记进注册表。
func Register() {
	plugins.Register(&plugins.Plugin{
		ID:        "notifications",
		DefaultOn: true,
		APIPrefixes: []string{
			"/api/notifications",
		},
	})
}
