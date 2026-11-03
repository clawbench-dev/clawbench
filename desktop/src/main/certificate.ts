import { app, BrowserWindow, dialog } from 'electron'
import { record } from './clientLog'
import { resolveNativeLanguage, certificateLabels } from './labels'
import { decideCertificateAction, isRememberedCertificate, originOf } from './certificatePolicy'
import { rememberedCertificates } from './certificateTrust'
import { getStore } from './store'

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
 * then remembers the answer.
 *
 * "For that connection" means every request to the origin, not just the
 * navigation that raised the prompt. Verification runs per socket, so the
 * WebSocket and any fetch that opens a new connection raises its own
 * `certificate-error` — those must be answered from the trust set too, or the
 * window loads and then every API call fails.
 *
 * The answer is REMEMBERED across connections, keyed by origin and bound to the
 * certificate's FINGERPRINT (see `trustedCertificates` in store.ts). A server
 * whose certificate changed — reinstalled, rotated, or actually swapped by a
 * man-in-the-middle — no longer matches and prompts again, so remembering never
 * degrades into "stop checking this host". `forgetTrustedCertificates()` is the
 * user-facing undo, exposed in Settings → Security.
 */

const TAG = 'Cert'

/**
 * Remember the certificate the user just accepted.
 *
 * Stored by origin rather than hostname so a different port is a different
 * server, and `originOf` folds `wss://` onto `https://` so the WebSocket of a
 * trusted page is covered by the same entry.
 *
 * The read side lives in `certificateTrust.ts`: the upgrade check needs the
 * same answer in a context that cannot import this module (it pulls in
 * `electron`).
 */
function rememberCertificate(url: string, fingerprint: string): void {
  const origin = originOf(url)
  if (!origin || !fingerprint) return
  getStore().set('trustedCertificates', { ...rememberedCertificates(), [origin]: fingerprint })
  record('I', TAG, `remembered certificate for ${origin} (${fingerprint})`)
}

/**
 * Forget every remembered certificate, so the next connection asks again.
 *
 * Returns how many entries were dropped, for the caller's confirmation toast.
 */
export function forgetTrustedCertificates(): number {
  const count = Object.keys(rememberedCertificates()).length
  getStore().set('trustedCertificates', {})
  record('I', TAG, `forgot ${count} remembered certificate(s)`)
  return count
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
  app.on('certificate-error', (event, webContents, url, error, certificate, callback, isMainFrame) => {
    const remembered = isRememberedCertificate(rememberedCertificates(), url, certificate.fingerprint)
    const action = decideCertificateAction(url, isMainFrame, remembered)
    if (action === 'ignore') return

    if (action === 'auto-trust') {
      // Already accepted for this origin (same fingerprint), or a localhost
      // tunnel whose cert can never match. This branch MUST also cover
      // subresources: each new socket re-runs verification, so answering only
      // the navigation leaves the WebSocket and most API calls rejected.
      event.preventDefault()
      if (remembered && !isMainFrame) record('D', TAG, `trusted subresource ${url} (${error})`)
      callback(true)
      return
    }

    // Hold the navigation and ask. `preventDefault()` stops Chromium's default
    // rejection so our asynchronous answer is what decides.
    event.preventDefault()
    record('W', TAG, `certificate error for ${url}: ${error} — prompting`)
    void promptTrustCertificate(url, error, BrowserWindow.fromWebContents(webContents))
      .then((trusted) => {
        if (trusted) {
          rememberCertificate(url, certificate.fingerprint)
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
