package handler

import "clawbench/internal/filex"

// 缩略图生成已经从 handler 搬到 internal/filex：会议纪要（live）要把会议
// 截图按缩略图返回，而插件不能 import handler。
//
// 保留同名别名，是为了不动本包的调用点（/api/files/thumb 路由、文件列表）。
var (
	FileThumb        = filex.FileThumb
	serveThumbOfFile = filex.ServeThumbOfFile
	isThumbDecodable = filex.IsThumbDecodable
	clampInt         = filex.ClampInt
)

const thumbMaxFileSize = filex.ThumbMaxFileSize
