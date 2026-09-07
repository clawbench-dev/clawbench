// Centralized handling for expired session cookies.
//
// When the clawbench_session cookie expires (7-day MaxAge), every subsequent
// /api/* request is rejected by middleware.Auth with a 401 whose JSON body is
// exactly { error: "unauthorized", code: 401 } (see internal/model/errors.go —
// model.Unauthorized + WriteError). Historically each call site surfaced its
// own error toast and the user was left stranded in a broken app. This module
// wraps window.fetch once and, on that specific 401, performs a full-page
// redirect to /login so the app re-runs its mount-time auth flow (Android App
// mode auto-relogs with the saved password; web mode shows the login page).
//
// The redirect is gated on the response BODY, not the URL, because a status
// code alone is ambiguous:
//   - middleware.Auth expiry/invalid-cookie 401 → { error: "unauthorized" }
//     (internal/middleware/auth.go:59-60) — THIS is the redirect trigger.
//   - POST /login wrong password → 401 { ok: false } (handler/auth.go:236)
//   - POST /api/config/password wrong current password → 401
//     { error: "wrong_password", ... } (handler/settings.go:1582)
//
// Body-gating means future business-401 endpoints never accidentally gain
// "kick to login" semantics — the interceptor only fires on the auth-marker
// error string.
//
// Redirect is armed ONLY after the main app has authenticated
// (setAuthRedirectEnabled(true), wired to App.vue's isAuthenticated watch), so
// the mount-time /api/me check itself and requests made while on the login
// page never trigger a redirect.
import { appLog } from '@/utils/appLog'

const TAG = 'AuthExpiry'

// Module-level state — the interceptor stays installed for the whole SPA
// lifetime; the enabled flag flips with the isAuthenticated gate.
let enabled = false

/** Arm/disarm the 401-redirect. Call with true once the user is authenticated. */
export function setAuthRedirectEnabled(on: boolean): void {
    enabled = on
}

/** Test-only accessor. */
export function isAuthRedirectEnabled(): boolean {
    return enabled
}

/**
 * The middleware.Auth 401 response body. Business-401 endpoints use other
 * shapes ({ error: "wrong_password" }, { ok: false }, ...), so matching this
 * exact error string is a reliable "session cookie invalid" signal.
 */
function isAuthExpiredBody(body: unknown): boolean {
    if (!body || typeof body !== 'object') return false
    const rec = body as Record<string, unknown>
    return rec.error === 'unauthorized'
}

/**
 * Redirect to /login. Returns true if a redirect was initiated, false if the
 * call was a no-op (disabled, not an auth-expiry body). The caller clears the
 * inspecting flag in a finally block, so a navigation that is interrupted or
 * deferred simply lets the next auth-expiry 401 retry — no sticky latch.
 */
export function handleUnauthorized(url: RequestInfo | URL, body: unknown): boolean {
    if (!enabled) return false
    if (!isAuthExpiredBody(body)) return false
    appLog.w(TAG, `Session expired (401 on ${String(url)}) — redirecting to /login`)
    window.location.href = '/login'
    return true
}

declare global {
    interface Window {
        __cbAuthExpiryInstalled?: boolean
    }
}

/**
 * Wrap window.fetch so every API request (including the ~89 raw fetch calls
 * across components/composables and the api.ts helpers) is observed for an
 * auth-expiry 401. The original response is always returned — callers keep
 * their own error handling; this only adds the redirect side effect.
 *
 * The body is read eagerly because it must be inspected before the caller
 * consumes it; the response is then reconstructed with that body preserved so
 * downstream consumers still see the same body. Responses whose body cannot be
 * read (network abort/error, already-consumed body, opaque/redirect responses)
 * are passed through untouched — never redirect on an unknown body.
 */
export function installAuthRedirectInterceptor(): void {
    if (window.__cbAuthExpiryInstalled) return
    window.__cbAuthExpiryInstalled = true

    const origFetch = window.fetch.bind(window)
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const resp = await origFetch(input, init)
        if (resp.status !== 401) return resp

        // Only treat a 401 as session expiry if it came from our own origin.
        // A cross-origin call (proxy/third-party endpoint) that happens to
        // return 401 with { error: "unauthorized" } must not kick the user.
        try {
            if (new URL(resp.url || String(input), window.location.origin).origin !== window.location.origin) {
                return resp
            }
        } catch {
            return resp
        }

        // Inspect the body without consuming it for the caller.
        const cloned = resp.clone()
        let body: unknown
        try {
            body = await cloned.json().catch(() => cloned.text().catch(() => null))
        } catch {
            return resp // opaque / unreadable body — cannot confirm, do not redirect
        }

        if (handleUnauthorized(input, body)) {
            // Reconstruct so the caller can still read a body after the redirect
            // is initiated (page unload is async; callers may briefly continue).
            return new Response(JSON.stringify(body), {
                status: resp.status,
                statusText: resp.statusText,
                headers: resp.headers,
            })
        }
        return resp
    }
}
