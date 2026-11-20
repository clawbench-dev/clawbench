package live

// 通用 API JSON key。handler 包里也有同名常量，但会议纪要搬进独立包后无法
// 引用。它们属于对外 API 契约，改动必须前后端同步。
const (
	jsonSessions  = "sessions"
	jsonFile      = "file"
	strContent    = "content"
	detailKey     = "reason"
	strMessages   = "messages"
	strReqError   = "error"
	sttWSType     = "type"
	messageKey    = "message"
	jsonKeyReason = "Reason"
)
