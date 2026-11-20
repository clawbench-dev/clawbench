package handler

import "clawbench/internal/plugins"

// MountShareRoutes 挂载 share 功能的全部 HTTP 接口。
//
// 由 handler.mountBuiltinRoutes 调用（RegisterRoutes 里构造好 Mount 之后）。
// 不经过 plugins 注册表：路由是编译期事实，注册表是运行时状态，见
// mount_builtin.go 的头注释。m.Auth / m.Public 已经把鉴权中间件、功能门控、
// 路由表登记都包好了，这里只写 pattern 与 handler。
//
// 这一组接口原先写在 handler.go 的 RegisterRoutes 里。搬出来之后，删掉
// share 功能的代价是"删掉它自己的 Plugin 字面量 + 这个文件"，不必再去
// 那张巨型路由表里一片片找。
func MountShareRoutes(m plugins.Mount) {
	// Public file-share links. Management endpoints are auth-protected; the
	// public data endpoints (/api/share/{token}/...) and the share SPA page
	// (/share/{token}) are intentionally unauthenticated — the capability token
	// in the URL is the sole credential, and no token means a 404 (zero
	// exposure when the feature is unused).
	m.Auth("/api/share", ServeShareManage)
	m.Auth("/api/share/list", ServeShareList)
	// Conversation (session) share links. Exact path so it is matched ahead of
	// the public /api/share/ subtree; see ServeSessionShareManage.
	m.Auth("/api/share/session", ServeSessionShareManage)
	// Management list for conversation shares: GET lists this project's shares,
	// DELETE revokes one by token or all of them. Separate from
	// /api/share/session because that one operates on a single session id, and an
	// archived session has no addressable id left.
	m.Auth("/api/share/session/list", ServeSessionShareList)
	m.Public("/api/share/", ServeSharePublic)
	m.Public("/share/", ServeSharePage)
}
