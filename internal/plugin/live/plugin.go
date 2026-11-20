// Package live 是会议记录 / 直播信令功能的插件声明。
//
// 纯偏好型：没有服务端总开关，恒 enabled=true。注册是为了让 /api/features
// 能回答"这个功能存在且可用"——前端不再需要按页签 id 硬编码一份清单。
// 可用性（能力）仍在客户端探测：端口映射有没有可用传输、原生能力位这些
// 只有客户端知道。
package live

import (
	"clawbench/internal/handler"
	"clawbench/internal/plugins"
)

// Register 把会议记录功能登记进注册表。
func Register() {
	plugins.Register(&plugins.Plugin{
		ID:        "live",
		DefaultOn: true,
		// 会议记录与信令。会议"公开纪要"那条开关属于 share，不在这个前缀下。
		APIPrefixes: []string{"/api/live-share/"},
		MountRoutes: handler.MountLiveRoutes,
	})
}
