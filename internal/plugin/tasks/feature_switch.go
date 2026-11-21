package tasks

import "clawbench/internal/service"

// TasksSwitchEffect 是任务总开关的双向效果：关闭要取消运行中的执行并丢弃
// 排队事件，打开要重排 cron。
//
// Scheduler.SetEnabled 自带 nil 判断（调度器在测试或初始化失败时可能是
// nil），所以这里不再加一层"否则静默返回"——那会让"调度器根本没起来"表现为
// 开关拨了但什么也没发生。
func TasksSwitchEffect(enabled bool) []string {
	service.GlobalScheduler.SetEnabled(enabled)
	return nil
}
