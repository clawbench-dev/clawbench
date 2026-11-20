package mail

import "clawbench/internal/plugins"

// MountMailRoutes 挂载 mail 功能的全部 HTTP 接口。
//
// 由 handler.mountBuiltinRoutes 调用（RegisterRoutes 里构造好 Mount 之后）。
// 不经过 plugins 注册表：路由是编译期事实，注册表是运行时状态，见
// mount_builtin.go 的头注释。m.Auth / m.Public 已经把鉴权中间件、功能门控、
// 路由表登记都包好了，这里只写 pattern 与 handler。
//
// 这一组接口原先写在 handler.go 的 RegisterRoutes 里。搬出来之后，删掉
// mail 功能的代价是"删掉它自己的 Plugin 字面量 + 这个文件"，不必再去
// 那张巨型路由表里一片片找。
func MountMailRoutes(m plugins.Mount) {
	// Built-in mail client: accounts, cached folders/messages, send, drafts.
	// Data is global (a mailbox belongs to the person, not to a project).
	// The whole prefix is gated by the mail plugin's master switch (see
	// register above) — /api/mail/send used to stay reachable while mail was
	// off, because the switch only stopped the background sync.
	m.Auth("/api/mail/presets", ServeMailPresets)
	m.Auth("/api/mail/accounts", ServeMailAccounts)
	m.Auth("/api/mail/accounts/", ServeMailAccountSubRoutes)
	m.Auth("/api/mail/folders", ServeMailFolders)
	m.Auth("/api/mail/folders/", ServeMailFolderSubRoutes)
	m.Auth("/api/mail/messages", ServeMailMessages)
	m.Auth("/api/mail/messages/", ServeMailMessageSubRoutes)
	m.Auth("/api/mail/unread", ServeMailUnread)
	m.Auth("/api/mail/send", ServeMailSend)
	m.Auth("/api/mail/attachments", ServeMailAttachments)
	m.Auth("/api/mail/drafts", ServeMailDrafts)
	m.Auth("/api/mail/drafts/", ServeMailDraftSubRoutes)
}
