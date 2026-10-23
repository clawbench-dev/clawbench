package handler

// Discovery block injected into the public share shell.
//
// The share SPA is client-rendered, so fetching /share/{token} returns an empty
// `#app` and a script tag — a browser fills it in, an agent gets nothing. These
// tests pin the contract that makes the page legible to a non-browser client:
// the title, the data URLs, and the plain-text fallback all have to be present,
// correctly escaped, and scoped to the token in the URL.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shellDoc is a minimal stand-in for share.html: the pieces the injector looks
// for (a </head>, the placeholder title) plus enough surrounding markup to prove
// insertion lands in the right place.
const shellDoc = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>ClawBench Share</title>
    <style>html,body,#app{height:100%}</style>
</head>
<body>
    <div id="app"></div>
    <script type="module" src="/src/share/main.ts"></script>
</body>
</html>`

func TestInjectShareDiscovery_FileShare(t *testing.T) {
	out := injectShareDiscovery(shellDoc, shareDiscovery{
		Token: "0123456789abcdef0123456789abcdef",
		Title: "分镜稿.md",
		Kind:  shareKindFile,
	})

	// A file share's content endpoint is the bare token path — there is no
	// /session sub-path for files, and pointing there would 404.
	assert.Contains(t, out, `href="/api/share/0123456789abcdef0123456789abcdef"`)
	assert.NotContains(t, out, "/session")
	// The title must reach both the JSON-LD name and the plain-text fallback.
	assert.Contains(t, out, `"@type":"CreativeWork"`)
	assert.Contains(t, out, "分镜稿.md")
	// Insertion happens inside <head>, before the closing tag.
	assert.Less(t, strings.Index(out, `rel="alternate"`), strings.Index(out, "</head>"))
}

func TestInjectShareDiscovery_ConversationShare(t *testing.T) {
	out := injectShareDiscovery(shellDoc, shareDiscovery{
		Token:        "0123456789abcdef0123456789abcdef",
		Title:        "Fix the login bug",
		Kind:         shareKindSession,
		MessageCount: 3,
	})

	assert.Contains(t, out, `href="/api/share/0123456789abcdef0123456789abcdef/session"`)
	assert.Contains(t, out, `"@type":"Conversation"`)
	assert.Contains(t, out, `"numberOfItems":3`)
}

func TestInjectShareDiscovery_AlwaysOffersMeta(t *testing.T) {
	// /meta is the cheapest way to ask "what is this link?" without transferring
	// the content, and it works for both kinds.
	for _, kind := range []string{shareKindFile, shareKindSession} {
		out := injectShareDiscovery(shellDoc, shareDiscovery{Token: "abcdefabcdefabcdefabcdefabcdefab", Kind: kind})
		assert.Contains(t, out, `href="/api/share/abcdefabcdefabcdefabcdefabcdefab/meta"`, kind)
	}
}

// A token is attacker-controlled (it comes straight from the URL), so anything
// that is not a plausible token must be refused outright rather than escaped
// into a URL.
func TestInjectShareDiscovery_RejectsMalformedToken(t *testing.T) {
	malicious := []string{
		`abc"><script>alert(1)</script>`,
		"abc/../../etc/passwd",
		"abc?x=1",
		"abc#frag",
		"abc def",
		strings.Repeat("a", 65), // over the length bound
		"",
	}
	for _, tok := range malicious {
		out := injectShareDiscovery(shellDoc, shareDiscovery{Token: tok, Kind: shareKindFile})
		assert.Equal(t, shellDoc, out, "token %q must leave the document untouched", tok)
	}
}

// The title is author-supplied free text (a file name or a conversation title)
// and lands in three contexts: HTML text, an HTML attribute and a JSON string.
func TestInjectShareDiscovery_EscapesTitle(t *testing.T) {
	out := injectShareDiscovery(shellDoc, shareDiscovery{
		Token: "0123456789abcdef0123456789abcdef",
		Title: `<img src=x onerror=alert(1)> & "quoted" </script>`,
		Kind:  shareKindFile,
	})

	// No raw tag survives anywhere in the injected block.
	assert.NotContains(t, out, "<img src=x")
	assert.NotContains(t, out, `"quoted"</script>`)
	// The escaped forms are what actually appear.
	assert.Contains(t, out, "&lt;img src=x")
	// json.Marshal escapes <, > and & so the JSON-LD string cannot close the
	// script element early.
	assert.Contains(t, out, `\u003c/script\u003e`)
}

// A shell without a </head> is a build problem, not a request problem: serve it
// untouched rather than corrupting it.
func TestInjectShareDiscovery_NoHeadLeavesDocumentAlone(t *testing.T) {
	doc := "<html><body>no head here</body></html>"
	out := injectShareDiscovery(doc, shareDiscovery{Token: "0123456789abcdef0123456789abcdef", Kind: shareKindFile})
	assert.Equal(t, doc, out)
}

func TestReplaceShareTitle(t *testing.T) {
	out := replaceShareTitle(shellDoc, "分镜稿.md")
	assert.Contains(t, out, "<title>分镜稿.md</title>")
	assert.NotContains(t, out, "<title>ClawBench Share</title>")
	// The rest of the document is untouched.
	assert.Contains(t, out, `<div id="app"></div>`)
}

func TestReplaceShareTitle_EmptyKeepsPlaceholder(t *testing.T) {
	assert.Equal(t, shellDoc, replaceShareTitle(shellDoc, ""))
}

// A document whose placeholder markup changed must lose only the title
// improvement — the discovery block is injected by a different function.
func TestReplaceShareTitle_MissingPlaceholderIsNoop(t *testing.T) {
	doc := "<html><head><title>Something else</title></head></html>"
	assert.Equal(t, doc, replaceShareTitle(doc, "new title"))
}

func TestShareTokenFromPagePath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/share/0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdef"},
		// A bare /share/ is not a share page.
		{"/share/", ""},
		{"/share", ""},
		// Extra segments are not page URLs (they would be data paths).
		{"/share/abc/def", ""},
		{"/other/abc", ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, shareTokenFromPagePath(c.path), c.path)
	}
}

// The served page must actually carry the block, and a HEAD must not.
func TestServeSharePage_InjectsDiscoveryForRealShare(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/notes.md", "# notes\n")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodGet, "/share/"+token, nil)
	w := callHandler(ServeSharePage, req)
	require.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	require.Contains(t, body, "</head>", "the shell must have been read")
	assert.Contains(t, body, `href="/api/share/`+token+`"`)
	assert.Contains(t, body, "notes.md", "the file name must reach the page title/description")
	// Cache policy: the shell references content-hashed assets.
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
}

func TestServeSharePage_HeadCarriesHeadersButNoBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/head.md", "# head\n")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodHead, "/share/"+token, nil)
	w := callHandler(ServeSharePage, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Empty(t, w.Body.String(), "HEAD must not carry a body")
}

// An unknown token still serves the shell (the SPA renders its own not-found
// state); injecting pointers to endpoints that cannot resolve would be worse
// than injecting nothing.
func TestServeSharePage_UnknownTokenGetsNoDiscovery(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/share/ffffffffffffffffffffffffffffffff", nil)
	w := callHandler(ServeSharePage, req)
	require.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.NotContains(t, body, `rel="alternate"`)
	assert.NotContains(t, body, `application/ld+json`)
}

// The data endpoints must answer HEAD: it is the standard way to probe a URL,
// and a 405 there makes a working share look broken to clients and agents.
func TestServeSharePublic_AllowsHead(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "docs/head-data.md", "# head\n")
	token := createShareViaAPI(t, env, absPath)

	for _, p := range []string{"/api/share/" + token, "/api/share/" + token + "/meta"} {
		req := newRequest(t, http.MethodHead, p, nil)
		w := callHandler(ServeSharePublic, req)
		assert.Equal(t, http.StatusOK, w.Code, p)
	}
}

// The discovery block must be valid JSON-LD: a parser reading it should not
// choke, and the values should survive the round trip.
func TestInjectShareDiscovery_JSONLDIsParseable(t *testing.T) {
	out := injectShareDiscovery(shellDoc, shareDiscovery{
		Token:        "0123456789abcdef0123456789abcdef",
		Title:        `He said "hi" & left <b>`,
		Kind:         shareKindSession,
		MessageCount: 7,
	})

	const open = `<script type="application/ld+json">`
	start := strings.Index(out, open)
	require.Greater(t, start, -1)
	start += len(open)
	end := strings.Index(out[start:], "</script>")
	require.Greater(t, end, -1)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(out[start:start+end]), &parsed))
	assert.Equal(t, "Conversation", parsed["@type"])
	assert.Equal(t, `He said "hi" & left <b>`, parsed["name"], "escaping must round-trip")
	assert.Equal(t, float64(7), parsed["numberOfItems"])
}
