package filex

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// 归档下载的 Content-Disposition 构造。原先在 handler/file_archive.go，
// 分享的下载端点也要用，而插件不能 import handler。

// sanitizeArchiveName removes or replaces characters that could break
// the Content-Disposition header (quotes, backslashes, control chars).
func SanitizeArchiveName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 0x20 {
			return '_'
		}
		return r
	}, name)
}

// contentDispositionAttachment builds a Content-Disposition header value
// with both filename (ASCII fallback) and filename* (RFC 5987/6267).
// This ensures CJK and other non-ASCII filenames display correctly
// across browsers, proxies, and the Android WebView.
func ContentDispositionAttachment(name string) string {
	safe := SanitizeArchiveName(name)
	encoded := rfc5987Encode(name)
	return fmt.Sprintf(`attachment; filename=%q; filename*=UTF-8''%s`, safe, encoded)
}

// rfc5987Encode percent-encodes a string per RFC 5987.
// ASCII alphanums and !#$&+-.^_`|~ are left unencoded.
func rfc5987Encode(name string) string {
	var encoded strings.Builder
	encoded.Grow(len(name) * 3)
	for _, r := range name {
		if isRFC5987Unreserved(r) {
			encoded.WriteRune(r)
		} else {
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], r)
			for i := range n {
				fmt.Fprintf(&encoded, "%%%02X", buf[i])
			}
		}
	}
	return encoded.String()
}

// rfc5987Unreserved is the set of unreserved characters per RFC 5987
// (ASCII alphanums plus !#$&+-.^_`|~).
var rfc5987Unreserved = func() map[rune]bool {
	m := make(map[rune]bool, 26+26+10+15)
	for r := 'a'; r <= 'z'; r++ {
		m[r] = true
	}
	for r := 'A'; r <= 'Z'; r++ {
		m[r] = true
	}
	for r := '0'; r <= '9'; r++ {
		m[r] = true
	}
	for _, r := range "!#$&+-.^_`|~" {
		m[r] = true
	}
	return m
}()

// isRFC5987Unreserved reports whether r is an unreserved character per RFC 5987.
func isRFC5987Unreserved(r rune) bool {
	return rfc5987Unreserved[r]
}
