package forge

import "clawbench/internal/plugins"

// MountForgeRoutes 挂载 forge 功能的全部 HTTP 接口。
//
// 由 plugins 注册表驱动：handler.RegisterRoutes 构造好 Mount 之后遍历
// plugins.All()，调到本函数（见 handler/mount_builtin.go）。
//
// 注入的 m.Auth / m.Public 已经把鉴权中间件、功能门控、路由表登记都包好了，
// 这里只写 pattern 与 handler。
//
// 这一组接口原先写在 handler.go 的 RegisterRoutes 里。搬出来之后，删掉
// forge 功能的代价是"删掉 internal/plugin/forge/ 整个目录"，不必再去
// 那张巨型路由表里一片片找。
func MountForgeRoutes(m plugins.Mount) {
	// GitHub / GitLab integration (read-only issue & PR browsing).
	m.Auth("/api/forge/credentials", ServeForgeCredentials)
	m.Auth("/api/forge/verify-token", ServeForgeVerifyToken)
	m.Auth("/api/forge/items", ServeForgeItems)
	m.Auth("/api/forge/item", ServeForgeItem)
	m.Auth("/api/forge/comments", ServeForgeComments)
	m.Auth("/api/forge/pipelines", ServeForgePipelines)
	m.Auth("/api/forge/pipeline", ServeForgePipeline)
	m.Auth("/api/forge/item-pipelines", ServeForgeItemPipelines)
	m.Auth("/api/forge/binding", ServeForgeBinding)
	m.Auth("/api/forge/remotes", ServeForgeRemotes)
	m.Auth("/api/forge/test", ServeForgeTest)
	m.Auth("/api/forge/unread", ServeForgeUnread)
	m.Auth("/api/forge/unread-items", ServeForgeUnreadItems)
	m.Auth("/api/forge/read", ServeForgeMarkRead)
}
