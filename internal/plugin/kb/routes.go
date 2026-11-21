package kb

import "clawbench/internal/plugins"

// MountKBRoutes 挂载 kb 功能的全部 HTTP 接口。
//
// 由 plugins 注册表驱动：handler.RegisterRoutes 构造好 Mount 之后遍历
// plugins.All()，调到本函数（见 handler/mount_builtin.go）。
//
// 注入的 m.Auth / m.Public 已经把鉴权中间件、功能门控、路由表登记都包好了，
// 这里只写 pattern 与 handler。
//
// 这一组接口原先写在 handler.go 的 RegisterRoutes 里。搬出来之后，删掉
// kb 功能的代价是"删掉 internal/plugin/kb/ 整个目录"，不必再去
// 那张巨型路由表里一片片找。
func MountKBRoutes(m plugins.Mount) {
	// Knowledge base: multiple collections scoped globally or per project,
	// files copied into the data directory and indexed for keyword + semantic
	// search. Separate tables from the chat index above.
	m.Auth("/api/kb/collections", ServeKBCollections)
	m.Auth("/api/kb/documents", ServeKBDocuments)
	m.Auth("/api/kb/documents/upload", ServeKBDocumentUpload)
	m.Auth("/api/kb/documents/import", ServeKBDocumentImport)
	m.Auth("/api/kb/documents/url", ServeKBDocumentURL)
	m.Auth("/api/kb/documents/move", ServeKBDocumentMove)
	m.Auth("/api/kb/documents/usage", ServeKBDocumentUsage)
	m.Auth("/api/kb/documents/reindex", ServeKBDocumentReindex)
	m.Auth("/api/kb/documents/refresh", ServeKBDocumentRefresh)
	m.Auth("/api/kb/documents/progress", ServeKBDocumentProgress)
	m.Auth("/api/kb/search", ServeKBSearch)
	m.Auth("/api/kb/reindex", ServeKBReindex)
	m.Auth("/api/kb/status", ServeKBStatus)
}
