package handler

import (
	"net/http"

	"clawbench/internal/plugins"
)

// featureEntry 是 GET /api/features 的一行。
//
// 字段刻意只有这五个：前端只需要"这个功能存不存在、开没开、能不能跑、
// 为什么不能跑"。具体配置项仍在 /api/config 里，这里不重复下发。
type featureEntry struct {
	ID string `json:"id"`
	// Enabled 是服务端功能开关（功能轴）。
	Enabled bool `json:"enabled"`
	// Available 是运行时可用性（能力轴），与 Enabled 正交：
	// 终端可以 enabled=true 而 available=false（平台没有 PTY）。
	Available bool `json:"available"`
	// Reason 是前端灰显/隐藏的依据："" / "disabled" / "capability"。
	Reason string `json:"reason"`
	// DefaultOn 让前端在请求未落地时有个合理的首屏判断（缺省当开）。
	DefaultOn bool `json:"defaultOn"`
}

const (
	// featureReasonDisabled：服务端功能被关掉。
	featureReasonDisabled = "disabled"
	// featureReasonCapability：功能开着，但这台服务器/宿主跑不了。
	featureReasonCapability = "capability"
)

// ServeFeatures 返回全部已注册功能的状态（GET /api/features）。
//
// 这是功能开关的**唯一权威**：前端不再维护一份默认值镜像，也不再各自去
// /api/config 里按形状掏 serverConfig.forge.enabled。
//
// 关闭优先级：disabled 压过 capability。一个关掉的功能哪怕宿主也跑不了，
// 对用户来说仍是"被关掉了"（他去设置里打开即可），所以 reason 报 disabled。
func ServeFeatures(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	all := plugins.All()
	// 非 nil 切片：没有功能时也要序列化成 []，不能是 null。
	features := make([]featureEntry, 0, len(all))
	for _, p := range all {
		entry := featureEntry{
			ID:        p.ID,
			Enabled:   p.IsEnabled(),
			Available: p.IsAvailable(),
			DefaultOn: p.DefaultOn,
		}
		switch {
		case !entry.Enabled:
			entry.Reason = featureReasonDisabled
		case !entry.Available:
			entry.Reason = featureReasonCapability
		}
		features = append(features, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{"features": features})
}
