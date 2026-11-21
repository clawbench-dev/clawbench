// Package tasks 是任务（定时执行）功能的插件声明。
//
// 它自己挂 /api/tasks 与 /api/tasks/（见 routes.go），并声明双向开关效果
// （见 feature_switch.go）：关掉要取消运行中的执行并丢弃排队事件，打开要
// 重排 cron。
//
// 前缀门控只按 method+path 识别"新建"这类动作，其余豁免（见 GateExempt 的
// 说明）——因为关闭任务后，列表、详情、执行历史仍要能看。
package tasks

import (
	"clawbench/internal/model"
	"clawbench/internal/plugins"
)

// Register 把任务功能登记进注册表。
func Register() {
	plugins.Register(&plugins.Plugin{
		ID:        "tasks",
		Section:   "tasks",
		Enabled:   func() bool { return model.TasksEnabled },
		DefaultOn: true,
		// 路由表里 "/api/tasks" 与 "/api/tasks/" 是两个 pattern，声明一次即可
		// 同时覆盖（ForRoute 抹平尾部斜杠后按段边界匹配）。
		APIPrefixes: []string{"/api/tasks"},
		MountRoutes: MountTasksRoutes,
		ConfigFields: map[string]bool{
			"tasks.enabled":           true,
			"tasks.retention_enabled": true,
			"tasks.retention_days":    true,
			"tasks.notify_on_success": true,
			"tasks.notify_on_failure": true,
			"tasks.timezone":          true,
		},
		// 双向开关：关闭要取消运行中的执行并丢弃排队事件，打开要重排 cron。
		// OnDisable 只表达"关"这一个方向，所以任务用 SwitchEffect（两个方向
		// 都会带着新值回调）。两者互斥，同一个插件只声明其中一个。
		SwitchEffect: TasksSwitchEffect,
		// 任务只有"新建"能被前缀门控——它是唯一靠 method+path 就能识别的操作。
		//
		// 其余全部豁免，因为**动作也走 PUT /api/tasks/{id}**，靠 body 里的
		// action 区分（cancel / resume / trigger / pause / read / stop /
		// deleteExecution），而路由层读不到 body：PUT 既可能是"整体更新"
		// （要拒），也可能是"取消运行中的执行"（必须放行——关闭开关会取消
		// 所有执行，用户得能看到并收拾残局）。所以 PUT 交给 handler 按 action
		// 判断（见 ServeTaskByID），这里整棵子树豁免。
		//
		// GET 列表（精确的 "/api/tasks"）也要豁免：关闭后仍要能看。
		GateExempt: []string{
			"GET /api/tasks",
			"/api/tasks/",
		},
	})
}
