/**
 * Classify a thrown fetch error as a TRANSPORT-layer failure (no HTTP response
 * ever arrived) rather than an HTTP-layer one (the server answered, possibly
 * with an error status).
 *
 * Callers use this to decide whether a failure is worth surfacing. A transport
 * failure means the server was unreachable — on mobile that is the normal state
 * for the moment right after the app returns to the foreground (the radio has
 * not reconnected yet), and the connection recovers on its own. Connectivity
 * is already expressed by the global ConnectionOverlay and the header status
 * dot, so a toast describing it is noise. An HTTP-layer failure, by contrast,
 * means the request DID reach the server and it rejected it — that is
 * actionable and must stay visible.
 *
 * Kept in its own module (rather than `@/utils/api`) on purpose: `api.ts` is
 * partially mocked with hand-written whitelists in many test files, so a new
 * export added there is silently absent in all of them — and any test that
 * happens to reach this call would then crash with a confusing
 * "is not a function" instead of failing on its own subject.
 */

/**
 * Wording browsers use when `fetch()` rejects before an HTTP response exists —
 * i.e. the request never reached the server (offline, connection refused, DNS
 * failure, TLS handshake failure). Per-engine:
 *   - Chromium/Firefox: TypeError('Failed to fetch')
 *   - WebKit/Safari:    TypeError('Load failed')
 *   - Android WebView:  Error('NetworkError when attempting to fetch resource.')
 *   - React Native:     TypeError('Network request failed')
 */
const TRANSPORT_ERROR_RE = /failed to fetch|load failed|network ?error|network request failed/i

export function isTransportError(err: unknown): boolean {
  if (!(err instanceof Error)) return false
  // Aborts are intentional (request timeout or a superseded request) and are
  // handled by the callers' own `signal.aborted` checks, not by message
  // sniffing — never conflate them here.
  if (err.name === 'AbortError') return false
  // A numeric `status` means an HTTP response WAS received (apiGet/apiPost and
  // the loadHistory path attach it), so the server was reachable — never
  // classify those as transport failures. Structural, not textual: a proxy's
  // error body can itself contain "failed to fetch".
  if (typeof (err as Error & { status?: unknown }).status === 'number') return false
  return TRANSPORT_ERROR_RE.test(err.message)
}
