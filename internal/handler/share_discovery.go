package handler

// Machine-readable discovery for the public share pages.
//
// The share SPA is a client-rendered shell: `share.html` ships an empty
// `#app` plus a module script, so fetching a share URL yields ~2.3 KB of markup
// with no conversation, no file content and no indication that either exists
// somewhere else. A browser runs the script and everything appears; an agent (or
// `curl`, or a link preview) gets nothing.
//
// The data is reachable — `/api/share/{token}/session` and
// `/api/share/{token}` both serve it unauthenticated on the same origin — but
// nothing on the page says so, and an agent has no way to guess a token-scoped
// path. So the server injects the pointers into the shell before sending it.
//
// Three channels, because agents read pages differently:
//
//   - `<link rel="alternate">` — the conventional discovery mechanism, picked up
//     by scrapers that parse <head>.
//   - `<script type="application/ld+json">` — schema.org Conversation/CreativeWork
//     for extractors that look for structured data.
//   - `<noscript>` — plain text. An agent that only reads visible text (or a
//     reader with JS off) still gets a title and a literal URL to fetch.
//
// The injected title also fixes a plain bug: every share page used to be titled
// "ClawBench Share", so a tab, a bookmark or a link unfurl could not tell two
// shares apart.
//
// IMPORTANT — injection is TEXTUAL and the token is attacker-influenced. The
// token comes from the URL, so it is validated (see validShareToken) and every
// value interpolated into the document is escaped for its context. A token that
// fails validation serves the shell UNMODIFIED rather than a broken page.

import (
	"encoding/json"
	"html"
	"strconv"
	"strings"
)

// shareDiscovery describes what the injected block should say.
type shareDiscovery struct {
	// Token is the validated share token. It is placed into URLs.
	Token string
	// Title is human-readable (file name or conversation title). Rendered as
	// text and as the JSON-LD `name`, so it is escaped in both contexts.
	Title string
	// Kind selects the schema.org type and the description wording.
	Kind string
	// MessageCount is the conversation's message count (0 for file shares).
	MessageCount int
}

// validShareToken reports whether a token is safe to interpolate into the
// injected HTML.
//
// Tokens are generated server-side as 32 lowercase hex characters
// (crypto/rand), so anything else is not a token we issued — most importantly,
// it cannot contain `"`, `<`, `>` or a quote-breaking payload. Rejecting rather
// than escaping is deliberate: an unexpected shape means the request is not one
// we understand, and silently "sanitizing" it into a working URL would hide the
// mismatch.
func validShareToken(token string) bool {
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

// injectShareDiscovery inserts the discovery block into a share.html document.
//
// The insertion point is the first `</head>`; a document without one is
// returned unchanged (the caller serves it as-is rather than failing — the page
// still works for browsers, and a malformed shell is a build problem, not a
// request problem).
func injectShareDiscovery(doc string, d shareDiscovery) string {
	if !validShareToken(d.Token) {
		return doc
	}
	const marker = "</head>"
	idx := strings.Index(doc, marker)
	if idx < 0 {
		return doc
	}
	return doc[:idx] + buildShareDiscoveryBlock(d) + doc[idx:]
}

// buildShareDiscoveryBlock renders the head block for one share.
func buildShareDiscoveryBlock(d shareDiscovery) string {
	// URLs are built from the validated token alone, so they need no escaping —
	// but they are still passed through urlPathEscape-free concatenation to keep
	// the shape obvious: /api/share/{token}/... on the same origin as the page.
	dataURL := "/api/share/" + d.Token + "/session"
	if d.Kind != shareKindSession {
		// A file share's content endpoint has no sub-path: /api/share/{token}.
		dataURL = "/api/share/" + d.Token
	}
	metaURL := "/api/share/" + d.Token + "/meta"

	title := d.Title
	if title == "" {
		title = "ClawBench Share"
	}
	// Escaped once for HTML text/attribute contexts, once more inside JSON-LD
	// (a JSON string literal additionally must not contain a raw `</script`).
	titleHTML := html.EscapeString(title)
	titleJSON, _ := json.Marshal(title) // escapes <, >, & to \u003c etc.

	schemaType := "CreativeWork"
	desc := "A file shared from ClawBench."
	if d.Kind == shareKindSession {
		schemaType = "Conversation"
		desc = "A conversation shared from ClawBench."
	}

	var b strings.Builder
	b.WriteString("\n    <!-- Machine-readable discovery (injected by the server).\n")
	b.WriteString("         The share SPA renders client-side, so this document alone carries no\n")
	b.WriteString("         content. These point at the same data over the same origin, using the\n")
	b.WriteString("         token already in the page URL — no second link is needed. -->\n")
	b.WriteString(`    <link rel="alternate" type="application/json" href="` + dataURL + `" title="` + titleHTML + ` (JSON)">` + "\n")
	b.WriteString(`    <link rel="alternate" type="application/json" href="` + metaURL + `" title="Share metadata">` + "\n")

	b.WriteString(`    <script type="application/ld+json">`)
	b.WriteString(`{"@context":"https://schema.org","@type":"` + schemaType + `","name":` + string(titleJSON))
	if d.Kind == shareKindSession && d.MessageCount > 0 {
		b.WriteString(`,"description":"` + desc + `","numberOfItems":` + strconv.Itoa(d.MessageCount))
	} else {
		b.WriteString(`,"description":"` + desc + `"`)
	}
	b.WriteString(`,"url":"` + dataURL + `"}`)
	b.WriteString("</script>\n")

	// Plain-text fallback. Deliberately readable prose: an agent that extracts
	// visible text should find both the subject and the URL without parsing
	// markup.
	b.WriteString("    <noscript>\n")
	b.WriteString("      <h1>" + titleHTML + "</h1>\n")
	b.WriteString("      <p>" + desc + " The full content is available as JSON at\n")
	b.WriteString(`         <a href="` + dataURL + `">` + dataURL + "</a>.</p>\n")
	b.WriteString("    </noscript>\n")

	// Title: replaces the generic "ClawBench Share" so tabs, bookmarks and link
	// unfurls identify the share. Done by a separate replacement (see
	// replaceShareTitle) because the marker lives elsewhere in the document.
	return b.String()
}

// replaceShareTitle swaps the shell's placeholder <title> for the share's own.
//
// Kept separate from the head-block injection so each concern has one
// implementation, and so a document whose title markup changes loses the title
// improvement without losing the discovery block.
func replaceShareTitle(doc, title string) string {
	if title == "" {
		return doc
	}
	const placeholder = "<title>ClawBench Share</title>"
	if !strings.Contains(doc, placeholder) {
		return doc
	}
	return strings.Replace(doc, placeholder, "<title>"+html.EscapeString(title)+"</title>", 1)
}
