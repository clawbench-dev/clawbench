// Package terminal 是网页终端功能的插件声明。
//
// 它是唯一需要构造参数的插件（终端 Manager 要知道服务端口），所以 Register
// 带一个 port 参数；其余插件都是无参的。internal/plugin/all 负责把 port 传进来。
package terminal

import (
	"log/slog"

	"clawbench/internal/model"
	"clawbench/internal/plugins"
	"clawbench/internal/terminal"
)

// Register 把终端功能登记进注册表。
func Register(port int) {
	plugins.Register(&plugins.Plugin{
		ID:        "terminal",
		Section:   "terminal",
		Enabled:   func() bool { return model.ConfigInstance.Terminal.Enabled },
		DefaultOn: true,
		// 终端关掉报 503 是既有行为，保留：那是"运行时没起来"而不是"被关掉了"。
		DisabledStatus: 503,
		Available: func() bool {
			mgr := GetTerminalManager()
			return mgr != nil && mgr.IsPlatformSupported()
		},
		APIPrefixes: []string{"/api/terminal/"},
		MountRoutes: MountTerminalRoutes,
		ConfigFields: map[string]bool{
			"terminal.enabled":      true,
			"terminal.idle_timeout": true,
			"terminal.max_sessions": true,
			"terminal.buffer_lines": true,
		},
		// 状态端点是只读能力探测，关闭时**必须**放行：前端靠它区分"功能关了"
		// 与"这台机器跑不了终端"（设置页在 platform_supported=false 时把开关
		// 置灰）。整段门控会让它 503，前端 catch 里把平台能力一起置 false，
		// 于是重新打开终端后开关变成"已开但置灰"、页签也不出现，只能刷新。
		GateExempt: []string{"/api/terminal/status"},
		// 三个钩子就是原 hotReloadTerminal 的三条分支，拆成"进程级起停"与
		// "热更新调参"：开关翻转走 Start/Stop，开关不变走 Reconfigure。
		Start: func(cfg model.Config) {
			if GetTerminalManager() != nil {
				return // 幂等：重复 Start 不叠加 Manager
			}
			SetTerminalManager(terminal.NewManager(cfg.Terminal, port))
			slog.Info(
				"terminal manager initialized",
				slog.String("idle_timeout", func() string {
					if cfg.Terminal.IdleTimeout == "" || cfg.Terminal.IdleTimeout == "0" {
						return "never"
					}
					return cfg.Terminal.IdleTimeout
				}()),
				slog.Int("buffer_lines", cfg.Terminal.BufferLines),
			)
		},
		Stop: func() {
			mgr := GetTerminalManager()
			if mgr == nil {
				return // 幂等
			}
			mgr.CloseAllSessions()
			SetTerminalManager(nil)
			slog.Info("terminal manager closed")
		},
		Reconfigure: func(cfg model.Config) {
			if mgr := GetTerminalManager(); mgr != nil {
				mgr.Reconfigure(cfg.Terminal)
			}
		},
	})
}
