package plugins

import (
	"encoding/json"
	"net/http"
)

// RejectWriter 写出本地化的拒绝响应体。
//
// 由 handler 注入（它拥有 i18n 与统一错误体），本包不认识这两者——否则
// plugins 就得 import handler，与 handler→plugins 成环。
type RejectWriter func(w http.ResponseWriter, r *http.Request, status int, msgKey string)

// Gate 在功能关闭时直接拒绝请求，不进入 handler。
//
// 它是"接口门控"的唯一实现：RegisterRoutes 按 ForRoute 命中的前缀自动套，
// 于是新增一个接口只要落在前缀下就自动被拦——不需要任何人记得写检查。
// 邮件曾经就是这样漏的（关闭只停了后台收信，/api/mail/send 照发）。
//
// 判定顺序：
//  1. 功能开着 → 放行（Enabled 为 nil 视为恒开）；
//  2. 命中 GateExempt → 放行（Forge 的 GET binding 要在关闭时返回空对象，
//     不是 409）；
//  3. 否则按 DisabledStatus（缺省 409）写出本地化错误，msgKey 为
//     DisabledKey。
//
// reject 为 nil 时写一个最小 JSON 体，便于单测直接调用；生产路径始终注入
// handler 的 writeLocalizedErrorf。
func Gate(p *Plugin, next http.HandlerFunc, reject RejectWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p == nil || p.IsEnabled() || p.IsGateExempt(r.Method, r.URL.Path) {
			next(w, r)
			return
		}
		status := p.DisabledHTTPStatus()
		key := p.DisabledKey()
		if reject != nil {
			reject(w, r, status, key)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  key,
			"code":   status,
			"msgKey": key,
		})
	}
}
