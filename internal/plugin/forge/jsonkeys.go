package forge

// 通用 API JSON key。handler 包里也有同名常量（分散在
// config_test_connectivity.go / providers.go 等），但 forge 搬进独立包后
// 无法引用。它们属于对外 API 契约，改动必须前后端同步。
const (
	strReqError = "error"
	jsonName    = "name"
)
