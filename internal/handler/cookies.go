// Application cookies that must survive a protocol switch.
//
// The server listens on exactly one scheme at a time — HTTP or HTTPS, decided
// at startup by whether a certificate exists. Users do switch: generating a
// certificate turns HTTPS on, deleting it turns HTTPS off, and changing ports
// or putting the server behind a proxy does the same. A cookie marked Secure
// is invisible over plain HTTP, and the browser refuses to let a plain-HTTP
// response overwrite it. So after switching back to HTTP the user stays logged
// out with no project selected, every project-scoped request answers
// 403 NoProjectSelected, and re-logging in does not help — that Set-Cookie is
// rejected for the same reason.
//
// Hence: none of these cookies carry Secure. What that trades away is narrow.
// The cookie only travels in cleartext if something makes the browser request
// the plain-HTTP origin while the server is serving HTTPS — the same attacker
// who could already read the password — and over plain HTTP the cookie was
// never protected to begin with.
package handler

import (
	"net/http"

	"clawbench/internal/model"
)

// setAppCookie writes one of the application's own cookies (login session,
// current project, current chat session) with the attributes every call site
// shares.
//
// Keep every such cookie going through here. A stray Secure flag on a cookie
// built elsewhere reintroduces the protocol-switch lockout for that one cookie
// only, which is far harder to diagnose than a uniform policy; cookies_test.go
// guards both the flag and direct http.SetCookie calls.
func setAppCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     model.ScopedCookieName(name),
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
