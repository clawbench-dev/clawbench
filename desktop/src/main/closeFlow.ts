import { app, dialog, type BrowserWindow } from 'electron'
import { getStore } from './store'
import { closeDialogLabels, resolveNativeLanguage } from './labels'
import { isEmbeddedServerActive, stopEmbeddedServer } from './localServer'
import {
  buildCloseDialogOptions,
  interpretCloseDialog,
  normalizeCloseAction,
  type CloseAction,
} from './closePolicy'

/**
 * What the window's close button does.
 *
 * Windows/Linux draw their own title bar (see `windowChrome.ts`), so the close
 * button is ours and it used to quit outright. Users of this kind of app expect
 * the choice — quit, or keep running in the tray — which is why the shell asks
 * once and can remember the answer.
 *
 * Every path that closes the main window lands here: the header button (over
 * IPC), Alt+F4, the taskbar's own "close", and the window manager's X. The
 * window's `close` event is intercepted in `window.ts`; this module owns the
 * decision, the dialog, the tray hand-off and quitting.
 */

/**
 * True from the moment a real quit begins.
 *
 * `before-quit` is what makes this reliable: quitting from the tray menu, from
 * `window-all-closed`, or from a self-upgrade all fire it before any window
 * closes, so the interception steps aside instead of turning a quit into a
 * dialog. It is also set directly by `quitNow()` — `app.quit()` fires
 * `before-quit` asynchronously and the window may close in between.
 */
let quitting = false

app.on('before-quit', () => { quitting = true })

/**
 * A dialog is already up. The dialog is modal to the window but not to the
 * process, so a second Alt+F4 while it is open would otherwise stack another.
 */
let prompting = false

export function isAppQuitting(): boolean {
  return quitting
}

/**
 * Forget a quit that was started but did not happen.
 *
 * `app.quit()` can be aborted: the renderer cancels `beforeunload` while a
 * meeting is publishing, and the user may then choose to stay. Without this the
 * flag would stay set, and the next close would skip the question and quit
 * outright — the exact decision the user just declined.
 */
export function cancelPendingQuit(): void {
  quitting = false
}

/**
 * The OS is ending the session (shutdown / restart / log off).
 *
 * Windows closes every window on its way down, which would otherwise be read as
 * a user close — the dialog would pop up with the machine already going away and
 * can delay the shutdown. Nothing to ask: let the windows close.
 */
export function noteOsShutdown(): void {
  quitting = true
}

/** The persisted choice, validated. */
export function getCloseAction(): CloseAction {
  try {
    return normalizeCloseAction(getStore().get('closeAction'))
  } catch {
    // Store unavailable (early startup): asking is always a safe answer.
    return 'ask'
  }
}

/**
 * Persist a choice and keep the shell in step with it.
 *
 * Returns the value actually stored so the IPC caller (and the settings row)
 * can echo the normalized form rather than the raw request.
 */
export function setCloseAction(raw: unknown): CloseAction {
  const next = normalizeCloseAction(raw)
  const prev = getCloseAction()
  try {
    getStore().set('closeAction', next)
  } catch {
    /* store unavailable */
  }
  if (next !== prev) afterActionChanged(next)
  return next
}

/**
 * React to the stored action changing: the tray exists for 'tray' only, and the
 * web settings row has to learn about a choice remembered in the dialog.
 *
 * Both are reached through a lazy import because `floating.ts` imports
 * `window.ts`, which imports this module — a static import here would close the
 * cycle. Everything else in this file is independent of the window, which is
 * what keeps the flow testable without booting Electron.
 */
function afterActionChanged(next: CloseAction): void {
  void import('./floating')
    .then((floating) => {
      floating.syncTrayForCloseAction()
      floating.notifyCloseActionChanged(next)
    })
    .catch(() => { /* shell surfaces unavailable (tests, shutdown) */ })
}

/** Quit for real: the close interception steps aside from here on. */
function quitNow(): void {
  quitting = true
  app.quit()
}

/**
 * Hide the window behind the tray icon.
 *
 * The icon is created first and the window is only hidden if it exists — a
 * platform without a usable tray would otherwise leave a hidden window with
 * nothing to click, i.e. an app that looks closed but cannot be reopened.
 */
async function hideToTray(win: BrowserWindow): Promise<void> {
  const floating = await import('./floating')
  floating.ensureAppTray()
  if (!floating.hasAppTray()) {
    const labels = closeDialogLabels(resolveNativeLanguage())
    if (!win.isDestroyed()) {
      await dialog.showMessageBox(win, {
        type: 'warning',
        buttons: [labels.ok],
        noLink: true,
        message: labels.trayUnavailable,
      })
    }
    return
  }
  if (!win.isDestroyed()) win.hide()
}

/**
 * Decide what a close request means, and carry it out.
 *
 * Called from the window's `close` handler with the event already prevented, so
 * "do nothing" (cancel) needs no further action.
 */
export async function handleCloseRequest(win: BrowserWindow): Promise<void> {
  if (quitting || prompting || win.isDestroyed()) return
  const action = getCloseAction()
  if (action === 'tray') {
    await hideToTray(win)
    return
  }
  if (action === 'exit') {
    quitNow()
    return
  }
  prompting = true
  try {
    const labels = closeDialogLabels(resolveNativeLanguage())
    // The bundled server keeps running after a plain quit, so an integrated
    // install gets a third button that stops it on the way out.
    const canStopServer = isEmbeddedServerActive()
    const result = await dialog.showMessageBox(win, buildCloseDialogOptions(labels, { canStopServer }))
    const { choice, next } = interpretCloseDialog(result, action, { canStopServer })
    if (next !== action) setCloseAction(next)
    if (choice === 'exit') quitNow()
    else if (choice === 'exit-stop') {
      // The signal is sent synchronously; the server drains on its own after
      // the shell is gone. Awaiting the full grace period here would freeze the
      // dialog for up to 15 seconds.
      void stopEmbeddedServer()
      quitNow()
    } else if (choice === 'tray') await hideToTray(win)
  } finally {
    prompting = false
  }
}
