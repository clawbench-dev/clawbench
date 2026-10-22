package handler

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Response validators for assembled (non-file) bodies.
//
// The repo has exactly one other conditional-request implementation
// (file_thumb.go), which compares If-None-Match with plain string equality. That
// is enough for a value it generates itself and always receives back verbatim,
// but not for a public endpoint: browsers and proxies legitimately send weak
// validators (W/"x"), comma-separated lists, and "*". A plain == silently fails
// all three, turning every revalidation into a full re-download.

// bodyETag derives a strong validator from an assembled response body.
//
// The first 16 bytes of the SHA-256 are plenty for a per-response validator and
// keep the header short.
func bodyETag(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf(`"%x"`, sum[:16])
}

// etagMatches reports whether an If-None-Match header selects etag.
//
// Handles the three forms RFC 9110 allows: "*" (any current representation
// matches), a comma-separated list, and the weak prefix W/ — which is ignored
// because our validators are always strong and the comparison is for cache
// revalidation only.
func etagMatches(ifNoneMatch, etag string) bool {
	value := strings.TrimSpace(ifNoneMatch)
	if value == "" || etag == "" {
		return false
	}
	if value == "*" {
		return true
	}
	for _, part := range strings.Split(value, ",") {
		candidate := strings.TrimSpace(part)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}
