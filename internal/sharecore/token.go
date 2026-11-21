// Package sharecore 放分享功能的纯逻辑（不含 HTTP、不含存储）。
//
// 它存在的理由：分享 token 的形状校验被分享功能和会议纪要同时使用，而两者
// 是各自独立的插件包，不能互相 import 对方的所有东西。
package sharecore

// ValidToken reports whether token has the shape of a share token:
// 1–64 characters of [0-9A-Za-z_-]. Anything else cannot be a token this server
// issued, so callers can reject it before touching the database.
func ValidToken(token string) bool {
	if token == "" || len(token) > 64 {
		return false
	}
	for _, r := range token {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}
