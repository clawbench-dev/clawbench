package notifications

// 通用 API JSON key。handler 包里也有同名常量，但通知中心搬进独立包后无法
// 引用。它属于对外 API 契约，改动必须前后端同步。
const (
	strError    = "error"
	jsonItems   = "items"
	jsonHasMore = "hasMore"
)
