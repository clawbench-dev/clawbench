import { app, BrowserWindow, dialog } from 'electron'
import { record } from './clientLog'
import { resolveNativeLanguage, certificateLabels } from './labels'
import { decideCertificateAction, originOf } from './certificatePolicy'

/**
 * TLS certificate trust for the desktop shell.
 *
 * Chromium rejects a server whose certificate it cannot verify (self-signed,
 * wrong hostname, expired), and Electron's default is to fail the navigation.
 * Before this, that surfaced as a silent bounce back to the login page with a
 * bare "ERR_CERT_AUTHORITY_INVALID" line — the user had no way to proceed,
 * even against their own self-hosted server.
 *
 * Android already prompts ("Trust and continue" / "Cancel connection") and
 * retries with a trusting client once the user confirms. This is the desktop
 * counterpart: an `app.on('certificate-error')` handler that asks the user and
 * trusts the certificate for the rest of THAT CONNECTION only.
 *
 * The trust is deliberately NOT persisted and is cleared on every connect: the
 * user re-confirms each time, matching Android, so a certificate that later
 * changes is re-prompted rather than silently accepted forever.
 */

const TAG = 'Cert'

/**
 * Origins the user has trusted for the CURRENT connection.
 *
 * Keyed by origin (`https://host:port`) rather than hostname, so a different
 * port is a different server and gets its own prompt. Cleared by
 * `resetCertificateTrust()` at the start of every connect.
 */
const trustedOrigins = new Set<string>()

/**
 * Forget every previously trusted origin. Called before each navigation so the
 * "trust this certificate" answer applies to one connection only.
 */
export function resetCertificateTrust(): void {
  if (trustedOrigins.size > 0) {
    record('I', TAG, `clearing ${trustedOrigins.size} trusted certificate origin(s) for a new connection`)
  }
  trustedOrigins.clear()
}

/**
 * The dialog a `certificate-error` event raises. Returns the user's decision.
 *
 * `showMessageBox` returns the index of the clicked button; the affirmative
 * button is first, so `response === 0` means "trust and continue". `cancelId`
 * is set to the negative button so closing the dialog (Esc / window close)
 * counts as "cancel", never as trust.
 */
async function promptTrustCertificate(url: string, error: string, parent: BrowserWindow | null): Promise<boolean> {
  const labels = certificateLabels(resolveNativeLanguage())
  const parentArg = parent ?? undefined
  const { response } = await dialog.showMessageBox(parentArg as BrowserWindow, {
    type: 'warning',
    buttons: [labels.trust, labels.cancel],
    defaultId: 1,
    cancelId: 1,
    noLink: true,
    title: labels.title,
    message: labels.message,
    detail: `${url}\n(${error})`,
  })
  return response === 0
}

/**
 * Register the app-level certificate-error handler.
 *
 * The listener is async (it awaits the dialog) while Electron's callback is
 * synchronous. That is fine: the navigation stays paused until `callback` is
 * invoked, so deferring the call into the promise is the documented pattern —
 * the event is simply prevented from taking its default (reject) path until we
 * answer.
 */
export function registerCertificateHandler(): void {
  app.on('certificate-error', (event, webContents, url, error, _certificate, callback, isMainFrame) => {
    const action = decideCertificateAction(url, isMainFrame)
    if (action === 'ignore') return

    const origin = originOf(url)
    if (action === 'auto-trust' || (origin && trustedOrigins.has(origin))) {
      // Already answered for this connection (a later subresource on the same
      // origin), or a localhost tunnel whose cert can never match.
      event.preventDefault()
      callback(true)
      return
    }

    // Hold the navigation and ask. `preventDefault()` stops Chromium's default
    // rejection so our asynchronous answer is what decides.
    event.preventDefault()
    record('W', TAG, `certificate error for ${url}: ${error} — prompting`)
    void promptTrustCertificate(url, error, BrowserWindow.fromWebContents(webContents))
      .then((trusted) => {
        if (trusted && origin) {
          trustedOrigins.add(origin)
          record('I', TAG, `user trusted ${origin} for this connection`)
        } else {
          record('I', TAG, `user declined the certificate for ${url}`)
        }
        callback(trusted)
      })
      .catch((err) => {
        // A failed dialog must not leave the navigation hanging: deny.
        record('E', TAG, `certificate prompt failed: ${String(err)}`)
        callback(false)
      })
  })
}
