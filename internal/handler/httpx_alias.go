package handler

import (
	"net/http"

	"clawbench/internal/httpx"
)

// 这些函数已经从 handler 搬到 internal/httpx，因为功能插件（internal/plugin/<id>）
// 需要它们，而插件不能 import handler——那会让依赖方向变成双向。
//
// 这里保留同名别名，是为了不动本包内几百个调用点。别名是函数值，没有额外
// 调用开销。
var (
	T                      = httpx.T
	writeLocalizedErrorf   = httpx.WriteLocalizedErrorf
	writeLocalizedError    = httpx.WriteLocalizedError
	requireProject         = httpx.RequireProject
	requireMethod          = httpx.RequireMethod
	writeJSON              = httpx.WriteJSON
	decodeJSON             = httpx.DecodeJSON
	decodeOptionalJSON     = httpx.DecodeOptionalJSON
	validateAndResolvePath = httpx.ValidateAndResolvePath
	resolveAbsPath         = httpx.ResolveAbsPath
	isPathUnderAnyRoot     = httpx.IsPathUnderAnyRoot
	isPathUnderBase        = httpx.IsPathUnderBase
)

// 编译期断言：别名签名与 httpx 一致（防止有人改了一边忘了另一边）。
var (
	_ func(http.ResponseWriter, int, any)                             = writeJSON
	_ func(http.ResponseWriter, *http.Request, any) bool              = decodeJSON
	_ func(*http.Request, any) error                                  = decodeOptionalJSON
	_ func(http.ResponseWriter, *http.Request, ...string) bool        = requireMethod
	_ func(http.ResponseWriter, *http.Request) (string, bool)         = requireProject
	_ func(http.ResponseWriter, *http.Request, string) (string, bool) = resolveAbsPath
	_ func(string) bool                                               = isPathUnderAnyRoot
	_ func(string, string) bool                                       = isPathUnderBase
)
