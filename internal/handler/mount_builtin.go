package handler

import "clawbench/internal/plugins"

// mountBuiltinRoutes 挂载全部内置功能的 HTTP 接口。
//
// # 生产：注册表驱动
//
// 遍历 plugins.All()，调每个插件自己声明的 MountRoutes。加一个带接口的
// 功能时，插件包（internal/plugin/<id>）声明 MountRoutes，这里一行都不用改。
//
// # 测试：编译期兜底
//
// handler 的单测不加载插件包（handler 不能 import 它们，否则成环），注册表
// 为空。此时退回到下面这份手写清单，否则 206 个测试里凡是依赖路由的都会
// 因为"接口没挂上"而红——那种"只在测试里坏"的设计最难查。
//
// 兜底清单与插件包的 MountRoutes 引用的是**同一批导出函数**（MountMailRoutes
// 这类），所以两边不可能漂移。真正的漂移风险是"插件声明了 APIPrefixes 却
// 忘了写 MountRoutes"，由 cmd/server 的守卫测试拦下。
//
// 顺序无要求：http.ServeMux 按 pattern 具体程度匹配，更具体的优先，与调用
// 先后无关。这里按功能名排，纯粹为了稳定。
func mountBuiltinRoutes(m plugins.Mount) {
	if all := plugins.All(); len(all) > 0 {
		for _, p := range all {
			if p.MountRoutes != nil {
				p.MountRoutes(m)
			}
		}
		return
	}

	// 注册表为空：handler 单测路径。保持与插件包里的声明一一对应。
	MountForgeRoutes(m)
	MountKBRoutes(m)
	MountLiveRoutes(m)
	MountMailRoutes(m)
	MountShareRoutes(m)
	MountTerminalRoutes(m)
}
