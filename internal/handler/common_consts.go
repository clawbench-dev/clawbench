package handler

// 这几个常量与工具原先定义在 forge_endpoints.go 里，但 handler 的其他文件
// （agent_sessions / notifications / chat_session / group / skills）也在用。
// forge 功能搬进 internal/plugin/forge 之后，那边保留了自己的一份，这里补回
// handler 需要的同名定义。
//
// 它们都是通用 API 词汇，不属于某个功能；下次谁再用到请直接引用这里，不要
// 再往功能文件里塞。

const (
	jsonItems   = "items"
	jsonHasMore = "hasMore"
	jsonSlug    = "slug"

	// 这两个常量原先定义在会议纪要的挂载文件里，但 settings / providers 也在用。
	liveNoteTextKey       = "text"
	relayProxySchemeHTTP  = "http"
	relayProxySchemeHTTPS = "https"
)

// containsString reports whether target is in list.
func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}
