package share

import "clawbench/internal/plugins"

// MountShareRoutes 挂载 share 功能的全部 HTTP 接口。
//
// 由 plugins 注册表驱动：handler.RegisterRoutes 构造好 Mount 之后遍历
// plugins.All()，调到本函数（见 handler/mount_builtin.go）。
//
// 注入的 m.Auth / m.Public / m.Raw 已经把鉴权中间件、功能门控、路由表登记
// 都包好了，这里只写 pattern 与 handler。
//
// 这一组接口原先写在 handler.go 的 RegisterRoutes 里。搬出来之后，删掉
// share 功能的代价是"删掉 internal/plugin/share/ 整个目录"，不必再去
// 那张巨型路由表里一片片找。
func MountShareRoutes(m plugins.Mount) {
	// OS share sheet entry. It must run the middleware chain but NOT
	// middleware.Auth: Auth writes a JSON 401, which the browser would render as
	// a bare error body after navigating here from the share sheet. The handler
	// re-checks authentication itself and bounces to the app, where the login
	// form lives.
	//
	// 退化到 Auth（不是 Public）：这个入口能写文件，装配方漏给 Raw 时必须仍然
	// 受保护，代价只是未登录时看到 JSON 401 而不是重定向。
	if m.Raw != nil {
		m.Raw("/share-target", ShareTarget)
	} else {
		m.Auth("/share-target", ShareTarget)
	}

	// Public file-share links. Management endpoints are auth-protected; the
	// public data endpoints (/api/share/{token}/...) and the share SPA page
	// (/share/{token}) are intentionally unauthenticated — the capability token
	// in the URL is the sole credential, and no token means a 404 (zero
	// exposure when the feature is unused).
	m.Auth("/api/share", ServeShareManage)
	m.Auth("/api/share/list", ServeShareList)
	// Conversation (session) share links. ServeMux prefers the more specific
	// pattern, so this exact path wins over the /api/share/ subtree regardless
	// of registration order; see ServeSessionShareManage.
	m.Auth("/api/share/session", ServeSessionShareManage)
	// Management list for conversation shares: GET lists this project's shares,
	// DELETE revokes one by token or all of them. Separate from
	// /api/share/session because that one operates on a single session id, and an
	// archived session has no addressable id left.
	m.Auth("/api/share/session/list", ServeSessionShareList)
	m.Public("/api/share/", ServeSharePublic)
	m.Public("/share/", ServeSharePage)
}
