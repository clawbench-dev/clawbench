// Package endpointurl turns a user-configured provider base URL into the full
// endpoint URL a wire protocol actually posts to.
//
// "Base URL" is ambiguous in practice: the settings page accepts a bare host, a
// host plus "/v1", a gateway namespace like MiniMax's "/anthropic", or a
// complete endpoint. Each consumer used to complete it with its own copy of the
// rules, and the copies disagreed — the connectivity test passed while the real
// call 404'd. This package is the single implementation they all share.
package endpointurl

import (
	"net/url"
	"strings"
)

// Resolve completes baseURL with defaultPath (e.g. "/v1/messages"). Every shape
// a user might reasonably configure is accepted:
//
//   - "https://api.openai.com"                     → "https://api.openai.com/v1/chat/completions"
//   - "https://api.openai.com/v1"                  → "https://api.openai.com/v1/chat/completions"
//   - "https://openrouter.ai/api/v1"               → "https://openrouter.ai/api/v1/chat/completions"
//   - "https://api.openai.com/v1/chat/completions" → unchanged
//   - "https://api.minimax.cn/anthropic"           → "https://api.minimax.cn/anthropic/v1/messages"
//   - "https://host/v1/text/chatcompletion_v2"     → unchanged (custom endpoint)
//
// The rule: an empty path is a bare base; a path ending with defaultPath is
// already complete; a path ending with a leading run of defaultPath's segments
// (e.g. "/v1", "/api/v1") is a partial base and gets the remainder appended; a
// path ending with a bare version segment other than defaultPath's own
// ("/api/v3") is a base for the same endpoint shape under a different API
// version and gets the part after the version prefix appended; a path ending
// with defaultPath's last segment (Azure's
// "/openai/deployments/<id>/chat/completions") is an endpoint without the
// version prefix and is left alone; a path with no version segment at all is a
// namespace the API is mounted under and gets defaultPath appended; anything
// else is the user's explicit custom endpoint and is used verbatim — blindly
// appending would turn a provider-specific path like MiniMax's
// "/v1/text/chatcompletion_v2" into a 404.
func Resolve(baseURL, defaultPath string) string {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u == "" {
		return ""
	}
	dp := ensureLeadingSlash(defaultPath)
	parsed, err := url.Parse(u)
	if err != nil {
		// Let http.NewRequest surface the malformed URL rather than guessing.
		return u
	}
	path := parsed.Path

	// A bare host (no path) is a base to complete.
	if path == "" || path == "/" {
		return u + dp
	}
	// Already the target endpoint.
	if strings.HasSuffix(path, dp) {
		return u
	}
	// A partial base ending in a leading run of defaultPath's segments, e.g.
	// "/v1" or "/api/v1" for "/v1/chat/completions". Longest run wins so the
	// remainder appended is the shortest.
	segments := strings.Split(strings.TrimLeft(dp, "/"), "/")
	for i := len(segments) - 1; i >= 1; i-- {
		prefix := "/" + strings.Join(segments[:i], "/")
		if strings.HasSuffix(path, prefix) {
			return u + strings.TrimPrefix(dp, prefix)
		}
	}
	// A path ending in a bare version segment is a base, not an endpoint: the
	// version is an address namespace, so the endpoint shape is defaultPath
	// minus its own version prefix. Volcengine Ark publishes "/api/v3" and
	// serves "/api/v3/chat/completions"; returning "/api/v3" verbatim 404'd
	// every summary, recommendation, auto-title and /btw call.
	if _, rest, ok := splitVersionPrefix(dp); ok {
		if _, ok := trailingVersionSegment(path); ok {
			return u + rest
		}
	}
	// An endpoint that keeps the protocol's action segment but drops the version
	// prefix is still complete, e.g. Azure OpenAI's
	// "/openai/deployments/<deployment>/chat/completions".
	if last := segments[len(segments)-1]; last != "" && strings.HasSuffix(path, "/"+last) {
		return u
	}
	// A versionless path is a namespace the API is mounted under, not an
	// endpoint: every wire protocol here ends with a versioned path. MiniMax's
	// Anthropic gateway publishes "/anthropic" as its base_url and serves
	// "/anthropic/v1/messages"; posting to the namespace itself 404s.
	if !hasVersionSegment(path) {
		return u + dp
	}
	// A provider-specific endpoint the user typed in full.
	return u
}

// hasVersionSegment reports whether a URL path carries an API version segment
// ("v1", "v2", …). A versioned path is an address the user typed deliberately;
// a versionless one is a mount point to complete.
func hasVersionSegment(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if isVersionSegment(seg) {
			return true
		}
	}
	return false
}

// trailingVersionSegment returns the final path segment when it is a bare API
// version ("v1", "v2", …) — i.e. the path is a base, not an endpoint.
func trailingVersionSegment(path string) (string, bool) {
	seg := path[strings.LastIndex(path, "/")+1:]
	if !isVersionSegment(seg) {
		return "", false
	}
	return seg, true
}

// splitVersionPrefix splits a default endpoint path into its leading version
// segment and the remainder that identifies the endpoint shape:
// "/v1/chat/completions" → ("v1", "/chat/completions").
func splitVersionPrefix(defaultPath string) (version, rest string, ok bool) {
	seg, remainder, found := strings.Cut(strings.TrimPrefix(defaultPath, "/"), "/")
	if !found || !isVersionSegment(seg) {
		return "", "", false
	}
	return seg, "/" + remainder, true
}

// isVersionSegment reports whether seg is a bare API version: "v" followed by
// digits, optionally with a maturity suffix ("v1", "v2", "v1beta"). The digit
// is required so a vendor namespace such as "/vendor" is not mistaken for a
// version — that would make the resolver drop the segment and post to
// "/messages".
func isVersionSegment(seg string) bool {
	if len(seg) < 2 || seg[0] != 'v' || seg[1] < '0' || seg[1] > '9' {
		return false
	}
	for i := 2; i < len(seg); i++ {
		c := seg[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// ensureLeadingSlash prefixes path with "/" unless it already has one.
func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}
