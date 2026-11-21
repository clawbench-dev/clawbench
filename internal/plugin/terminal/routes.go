package terminal

import "clawbench/internal/plugins"

// MountTerminalRoutes 挂载 terminal 功能的全部 HTTP 接口。
//
// 由 plugins 注册表驱动：handler.RegisterRoutes 构造好 Mount 之后遍历
// plugins.All()，调到本函数（见 handler/mount_builtin.go）。
//
// 注入的 m.Auth / m.Public 已经把鉴权中间件、功能门控、路由表登记都包好了，
// 这里只写 pattern 与 handler。
//
// 这一组接口原先写在 handler.go 的 RegisterRoutes 里。搬出来之后，删掉
// terminal 功能的代价是"删掉 internal/plugin/terminal/ 整个目录"，不必再去
// 那张巨型路由表里一片片找。
func MountTerminalRoutes(m plugins.Mount) {
	// Terminal (interactive web terminal with PTY + WebSocket + xterm.js)
	m.Auth("/api/terminal/ws", TerminalWebSocket)
	m.Auth("/api/terminal/status", TerminalStatus)
	m.Auth("/api/terminal/close", TerminalClose)
	m.Auth("/api/terminal/quick-commands", ServeQuickCommands)
	m.Auth("/api/terminal/quick-commands/", ServeQuickCommandByID)
	m.Auth("/api/terminal/history", ServeTerminalHistory)
	m.Auth("/api/terminal/history/", ServeTerminalHistoryByID)
	m.Auth("/api/terminal/key-config", ServeKeyConfig)
}
