package handler

import "clawbench/internal/plugins"

// mountBuiltinRoutes 挂载全部内置功能的 HTTP 接口。
//
// 只有一个来源：plugins 注册表。遍历 plugins.All()，调每个插件自己声明的
// MountRoutes。加一个带接口的功能时，插件包（internal/plugin/<id>/routes.go）
// 声明 MountRoutes，这里一行都不用改。
//
// # 注册表为空时什么都不挂，这是有意的
//
// handler 自己的单测跑在空注册表下（handler 不 import 插件包，否则「删掉一个
// 插件目录」就会连累 handler 编译）。这时只挂核心路由（chat / 文件 / 设置 /
// agent），插件接口不可达——这是**测试环境的既定语义**，不是降级：
//
//   - 插件接口的测试都在插件包自己里（internal/plugin/<id>/*_test.go）；
//   - 需要完整路由表的用例（路由表 vs OpenAPI、鉴权标记）显式调用
//     handler 测试里的 registerAllPluginsForTest()。
//
// 曾经这里有一份「编译期兜底清单」重复挂一遍插件路由。它必须删掉：兜底清单
// 要求 handler import 插件包，那会让「删掉 internal/plugin/mail」直接编译失败，
// 与「删一个目录等于删一个功能」的目标相反。
//
// 顺序无要求：http.ServeMux 按 pattern 具体程度匹配，更具体的优先，与调用
// 先后无关。
func mountBuiltinRoutes(m plugins.Mount) {
	for _, p := range plugins.All() {
		if p.MountRoutes != nil {
			p.MountRoutes(m)
		}
	}
}
