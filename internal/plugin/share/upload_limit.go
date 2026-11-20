package share

import "clawbench/internal/model"

// maxUploadSize returns the maximum allowed upload size in bytes from config.
// 与 handler/upload.go 的同名函数一致——分享的 OS 分享面板走同一套上限。
func maxUploadSize() int64 {
	mb := model.UploadMaxSizeMB
	if mb <= 0 {
		mb = 10
	}
	return int64(mb) * 1024 * 1024
}
