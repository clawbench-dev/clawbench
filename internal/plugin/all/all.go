// Package all 汇总全部内置功能插件。
//
// 这是"加插件要改的唯一一处"：新建 internal/plugin/<id> 之后，在这里加一行
// RegisterAll 调用。cmd/server 只 import 本包，不再认识任何具体插件。
//
// 为什么是显式 RegisterAll(port) 而不是各插件的 init() 自注册：
// terminal 的 Manager 需要服务端口，而 init() 拿不到构造参数。显式调用还让
// "哪些插件被加载"在编译期就一目了然，不必靠 import 副作用推断。
package all

import (
	"clawbench/internal/plugin/forge"
	"clawbench/internal/plugin/kb"
	"clawbench/internal/plugin/live"
	"clawbench/internal/plugin/mail"
	"clawbench/internal/plugin/notifications"
	"clawbench/internal/plugin/share"
	"clawbench/internal/plugin/stats"
	"clawbench/internal/plugin/tasks"
	"clawbench/internal/plugin/terminal"
)

// RegisterAll 把全部内置功能登记进注册表。
//
// 调用方（cmd/server）负责先 Reset（测试）或只调一次（生产）。
func RegisterAll(port int) {
	forge.Register()
	kb.Register()
	mail.Register()
	share.Register()
	tasks.Register()
	terminal.Register(port)
	live.Register()
	notifications.Register()
	stats.Register()
}
