package share

// 通用 API JSON key。handler 包里也有同名常量，但分享搬进独立包后无法引用。
// 它们属于对外 API 契约，改动必须前后端同步。
const (
	entryTypeFile = "file"
	entryTypeDir  = "dir"
)
