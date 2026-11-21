// Package mail 是邮件功能的插件声明。
package mail

import (
	"clawbench/internal/model"
	"clawbench/internal/plugin/config"
	"clawbench/internal/plugins"
	"clawbench/internal/service"
)

// Register 把邮件功能登记进注册表。
func Register() {
	plugins.Register(&plugins.Plugin{
		ID:        "mail",
		Section:   "mail",
		Enabled:   func() bool { return model.ConfigInstance.Mail.Enabled },
		DefaultOn: true,
		// 邮箱数据是全局的（属于人，不属于项目），前缀下没有任何项目路由。
		APIPrefixes:  []string{"/api/mail/"},
		MountRoutes:  MountMailRoutes,
		ConfigFields: config.Fields("mail"),
		// 后台收信。worker 每个周期也会自己看开关（第二道防线），这里的
		// Start/Stop 让"关掉"真的不占一个常驻 goroutine。
		Start: func(model.Config) { service.StartMailSyncWorker() },
		Stop:  service.StopMailSyncWorker,
	})
}
