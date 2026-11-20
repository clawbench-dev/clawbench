// Package forgepoll 负责 GitHub / GitLab 变更轮询的接线。
//
// 它原先写在 cmd/server/main.go 里（startForgePoller + forgeNotifierAdapter），
// 因为「forge 功能的 Start 钩子」要引用它。插件包（internal/plugin/forge）
// 不能 import main，所以把它搬到这里——本包位于依赖链最上层（import handler /
// service / ws / push），不会被任何人反向依赖，不会成环。
package forgepoll

import (
	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/push/dingtalk"
	"clawbench/internal/push/feishu"
	"clawbench/internal/push/wechat"
	"clawbench/internal/service"
	"clawbench/internal/ws"
)

// notifierAdapter 把 forge 事件推给当前在跑的 IM 机器人。
type notifierAdapter struct{}

// PushForgeEvent renders the event and sends it to the active IM backend.
func (notifierAdapter) PushForgeEvent(event service.ForgeEvent, item forge.Item) bool {
	title, body := service.FormatForgeEventMessage(event, item)
	switch {
	case dingtalk.IsStarted():
		return dingtalk.PushForgeEvent(title, body)
	case feishu.IsStarted():
		return feishu.PushForgeEvent(title, body)
	case wechat.IsStarted():
		return wechat.PushForgeEvent(title, body)
	default:
		return false
	}
}

// Start wires the GitHub/GitLab change poller: event broadcasting over WS, the
// IM push sink, the event-triggered task sink and the notification-center
// recorder.
//
// The provider factory is supplied by the handler package (it owns the
// credential + TLS policy). Read-only, and it only talks to a platform when at
// least one project is bound to a repository.
// Deps 是插件注入的两个能力。
//
// 它们住在 internal/plugin/forge（凭据与 TLS 策略属于 forge 功能），而本包
// 在依赖链上层、被那个插件 import——如果反过来直接引用插件包就成环了。
type Deps struct {
	// NewProvider 构造一个仓库平台客户端（凭据 + TLS 策略由插件决定）。
	NewProvider func(service.ProjectForge) (forge.Provider, error)
	// CredentialLogin 返回某个 host 上凭据对应的登录名，供事件触发任务使用。
	CredentialLogin func(platform, host string) string
}

func Start(d Deps) {
	limiter := forge.NewLimiter(2, 5, 4)
	// Broadcast forge events over the existing WS channel, and push to the
	// active IM robot (dingtalk/feishu) when one is running.
	dispatcher := service.NewForgeEventDispatcher(
		func() model.Config { return model.ConfigInstance },
		func(msg any) {
			if m := ws.GetManager(); m != nil {
				m.BroadcastEvent(ws.ServerMessage{
					Type:  ws.MessageTypeEvent,
					ID:    ws.GenerateEventID(),
					Event: "forge_event",
					Data:  msg,
				})
			}
		},
		notifierAdapter{},
	)
	// Event-triggered AI tasks run off the same derived events. The trigger
	// owns queueing/debounce/kill-switch and the anti-recursion check; it is
	// wired as a second sink so notification and automation stay independent.
	trigger := service.NewForgeTaskTrigger(
		service.GlobalScheduler,
		func() model.Config { return model.ConfigInstance },
		func(repo service.ForgeRepoRef) string {
			return d.CredentialLogin(repo.Platform, repo.Host)
		},
	)
	// Exposed so the tasks master switch can discard queued events when it is
	// turned off (see Scheduler.SetEnabled).
	service.GlobalForgeTaskTrigger = trigger
	// Third sink: the notification-center log. Kept separate from the
	// dispatcher because it must record every change regardless of the
	// per-event IM toggles (a muted event still belongs in the history).
	recorder := service.NewForgeNotificationRecorder()
	syncer := service.NewForgeSyncer(d.NewProvider, service.NewForgeCompositeSink(dispatcher, trigger, recorder))
	service.StartForgePoller(syncer, limiter, func() model.Config { return model.ConfigInstance })
}
