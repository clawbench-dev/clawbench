package live

// 通用 API JSON key。handler 包里也有同名常量，但会议纪要搬进独立包后无法
// 引用。它们属于对外 API 契约，改动必须前后端同步。
const (
	jsonSessions = "sessions"
	jsonFile     = "file"
	strContent   = "content"
	// detailKey 是「接口返回体里 detail 对象的键名」，小写 reason：
	// {"import":{"available":false,"reason":"..."}}。前端按 detail.reason 读。
	detailKey = "reason"
	// jsonKeyReason 是**另一回事**：它作为 templateData 的键传进
	// WriteLocalizedErrorf，落成 {"error":..., "detail":{"Reason":"..."}}。
	// 大小写与 detailKey 不同（历史原因），别合并、别互相替换。
	jsonKeyReason = "Reason"

	strMessages = "messages"
	strReqError = "error"
	sttWSType   = "type"
	messageKey  = "message"
)
