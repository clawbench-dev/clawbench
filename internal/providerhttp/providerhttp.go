// Package providerhttp carries request conventions that belong to a provider
// ENDPOINT rather than to a wire protocol.
//
// The wire layer (OpenAI chat completions, Anthropic messages) is the same for
// every gateway; a few gateways additionally demand headers of their own. Those
// rules live here so every in-process caller — the built-in agent, the
// summarizer and the settings-page probe — sends the same request.
package providerhttp

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"clawbench/internal/version"
)

// OpenCodeSessionHeader is the routing header OpenCode's inference endpoints
// require. Without it Go answers 400 MissingSessionID before the request ever
// reaches a model.
const OpenCodeSessionHeader = "x-opencode-session"

// userAgent identifies this client. OpenCode's Go plan explicitly asks callers
// not to present a generic HTTP-library name, and Go's default is exactly that
// ("Go-http-client/1.1").
var userAgent = "clawbench/" + version.Get()

// openCodePresets are the models.dev provider ids served by OpenCode's
// inference API. The preset is the authoritative signal when the row came from
// the catalog; the host check below also covers a hand-written endpoint.
var openCodePresets = map[string]bool{
	"opencode":    true,
	"opencode-go": true,
}

// IsOpenCode reports whether an endpoint belongs to OpenCode's inference API,
// either through its catalog preset id or through its host.
func IsOpenCode(baseURL, presetID string) bool {
	if openCodePresets[strings.ToLower(strings.TrimSpace(presetID))] {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "opencode.ai" || strings.HasSuffix(host, ".opencode.ai")
}

// SessionID returns the value for the session header. A caller that has a
// stable conversation id passes it through; a one-off caller passes "" and gets
// a fresh id. Callers that issue more than one request MUST resolve the id once
// and reuse it — a new id per request defeats the routing and prompt cache the
// header exists for.
func SessionID(stable string) string {
	if s := strings.TrimSpace(stable); s != "" {
		return s
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A random id is a routing hint, not a credential: an installation-wide
		// constant is still a valid value and keeps the request working.
		return "ses_clawbench"
	}
	return "ses_" + hex.EncodeToString(b[:])
}

// Apply sets the headers the endpoint requires in addition to authentication.
// It is a no-op for endpoints with no extra requirements, so callers may invoke
// it unconditionally.
func Apply(req *http.Request, baseURL, presetID, sessionID string) {
	if !IsOpenCode(baseURL, presetID) {
		return
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(OpenCodeSessionHeader, SessionID(sessionID))
}
