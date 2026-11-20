package handler

import "clawbench/internal/ws"

// wsOriginPatterns 的实现已经搬到 internal/ws（会议纪要也要用，而插件不能
// import handler）。保留同名别名，是为了不动本包的调用点。
var wsOriginPatterns = ws.OriginPatterns
