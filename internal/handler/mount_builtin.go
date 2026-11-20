package handler

import "clawbench/internal/plugins"

// mountBuiltinRoutes 注册全部内置功能的 HTTP 接口。
//
// 这是"哪些功能的接口由谁挂"的**唯一清单**：每加一个带接口的功能，就在
// 这里加一行，并在 cmd/server/main.go 里注册对应插件（声明 APIPrefixes
// 用于门控）。两边由 cmd/server 的守卫测试核对，漏一边会红。
//
// 为什么是手写清单而不是遍历 plugins.All() 的 MountRoutes：
// 见 RegisterRoutes 里的注释——路由是编译期事实，注册表是运行时状态，
// 把路由挂载建立在注册表之上会让 handler 的单测（注册表为空）丢掉一半
// 接口，而生产上恰好正常。
//
// 顺序无要求：http.ServeMux 按 pattern 具体程度匹配，更具体的优先，
// 与调用先后无关。这里按功能名排，纯粹为了稳定。
func mountBuiltinRoutes(m plugins.Mount) {
	MountForgeRoutes(m)
	MountKBRoutes(m)
	MountLiveRoutes(m)
	MountMailRoutes(m)
	MountShareRoutes(m)
	MountTerminalRoutes(m)
}
