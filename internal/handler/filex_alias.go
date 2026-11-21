package handler

import "clawbench/internal/filex"

// 文件读取的公共工具已经从 handler 搬到 internal/filex，因为分享功能
// （internal/plugin/share）要复用同一套规则，而插件不能 import handler。
//
// 这里保留同名别名，是为了不动本包内几百个调用点。
type (
	DirEntry    = filex.DirEntry
	FileContent = filex.FileContent
)

const (
	mimeOctetStream   = filex.MimeOctetStream
	maxGetFileBytes   = filex.MaxGetFileBytes
	maxForceTextSize  = filex.MaxForceTextSize
	maxBinaryTextSize = filex.MaxBinaryTextSize
	binarySniffSize   = filex.BinarySniffSize
)

var (
	mimeTypes = filex.MimeTypes

	mimeForLocalFile    = filex.MimeForLocalFile
	sniffBinaryContent  = filex.SniffBinaryContent
	resolveLinkTarget   = filex.ResolveLinkTarget
	responsePath        = filex.ResponsePath
	sanitizeTextContent = filex.SanitizeTextContent
	hasBinaryContent    = filex.HasBinaryContent
)
