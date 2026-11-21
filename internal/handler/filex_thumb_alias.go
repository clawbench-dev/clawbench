package handler

import "clawbench/internal/filex"

// 缩略图生成已经从 handler 搬到 internal/filex：会议纪要（live）要把会议
// 截图按缩略图返回，而插件不能 import handler。
//
// 只剩 FileThumb 还有调用点（/api/files/thumb 路由、文件列表）；其余同名
// 别名在搬迁后已无人引用，删掉了——留着会让人以为还有人用。
var FileThumb = filex.FileThumb
