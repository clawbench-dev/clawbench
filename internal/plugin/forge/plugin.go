// Package forge 是 GitHub / GitLab 集成功能的插件声明。
//
// 接口实现仍在 internal/handler（mount_forge.go 与 forge_*.go）；这里只声明
// 「我是谁、我管哪些路由、我有哪些配置项、开关怎么生效」。加一个新功能时照
// 这个文件的形状建一个新包，再在 internal/plugin/all 里加一行即可。
package forge

import (
	"clawbench/internal/forgepoll"
	"clawbench/internal/model"
	"clawbench/internal/plugin/config"
	"clawbench/internal/plugins"
	"clawbench/internal/service"
)

// pluginID is the plugin's identifier: it names the config section, the route
// prefix owner and the plugin registry entry, so all of them stay in sync.
const pluginID = "forge"

// Register 把 forge 功能登记进注册表。
func Register() {
	plugins.Register(&plugins.Plugin{
		ID:        pluginID,
		Section:   pluginID,
		Enabled:   func() bool { return model.ConfigInstance.Forge.Enabled },
		DefaultOn: true,
		// 整个前缀归 forge 管。GET /api/forge/binding 例外：前端靠它判断
		// "未绑定"，关闭时它必须返回空对象而不是 409（见 serveForgeBindingGet
		// 里的关闭分支），所以按方法豁免——POST/DELETE 仍然要被拒。
		APIPrefixes: []string{"/api/forge/"},
		GateExempt:  []string{"GET /api/forge/binding"},
		MountRoutes: MountForgeRoutes,
		// 可修改的配置字段（PATCH /api/config）。清单住在
		// internal/plugin/config/forge.go，handler 的白名单读同一份，
		// 所以不存在"插件声明了但 PATCH 不认"的窗口。
		ConfigFields: config.Fields(pluginID),
		Start: func(model.Config) {
			forgepoll.Start(forgepoll.Deps{
				NewProvider:     NewForgeProvider,
				CredentialLogin: ForgeCredentialLogin,
			})
		},
		Stop: service.StopForgePoller,
		// 关闭即撤销：绑定是"会对外发请求"的东西，清掉它才让"关闭"等于
		// "不再和 GitHub/GitLab 说话"，而不只是隐藏入口。轮询器每个周期也会
		// 自己看开关（这里是第二道防线，覆盖清空提交前的窗口）。
		// 时机由 handler 保证：writeConfigYAML 成功之后才调用。
		OnDisable: ForgeDisableEffect,
	})
}
