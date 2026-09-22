import { Notification } from 'electron'
import { getMainWindow } from './window'
import { isRendererReady } from './navReady'
import type { NavChannel, NotificationNav } from '../shared/types'

let pendingNavigation: string | null = null

export function getPendingNavigationJson(): string | null {
  const n = pendingNavigation
  pendingNavigation = null
  return n
}

export function dispatchOpenSession(sessionId: string | null): void {
  const w = getMainWindow()
  if (!w) return
  w.webContents.send('clawbench-open-session', { sessionId })
  if (sessionId) revealWindow(w)
}

/**
 * Bring the window to the front.
 *
 * `focus()` alone is NOT enough: on a minimized window it is a no-op
 * (verified on Linux — isMinimized stays true and isVisible stays false), and
 * on a hidden window it does nothing either. Since the whole point of the
 * click is "take me to the thing that finished", the window must actually be
 * restored and shown.
 */
function revealWindow(w: Electron.BrowserWindow): void {
  if (w.isMinimized()) w.restore()
  if (!w.isVisible()) w.show()
  w.focus()
}

/** Pick the renderer channel a navigation belongs on. */
function channelFor(nav: NotificationNav): NavChannel {
  // forge first: forge notifications carry no session/task id, so the
  // sessionId/taskId branches below would never match and the click would
  // silently do nothing.
  if (nav.forge) return 'clawbench-open-forge'
  if (nav.taskId) return 'clawbench-open-task'
  return 'clawbench-open-session'
}

/** Deliver a navigation to the renderer, or defer it until the renderer is ready. */
function sendNavToRenderer(channel: NavChannel, nav: NotificationNav): void {
  const w = getMainWindow()
  if (!w) return
  if (!isRendererReady()) {
    // Cold start / mid-reload: stash it. The renderer picks it up via
    // getPendingNavigation() once it mounts.
    pendingNavigation = JSON.stringify(nav)
    return
  }
  w.webContents.send(channel, nav)
  revealWindow(w)
}

/**
 * Whether the app window is open on screen.
 *
 * Used to suppress OS notifications while the window is visible: the user has
 * the app open, so a notification for something it already shows on screen is
 * pure noise. The renderer tries to make the same call (`document.hasFocus()`),
 * but only the main process knows the truth — a renderer that is alive inside a
 * minimized or hidden window still reports itself visible and focused.
 *
 * Visibility alone is the criterion, deliberately NOT focus: a window sitting
 * on a second monitor (or behind another app) still counts as "the user has the
 * app open", and popping OS notifications on top of whatever they are actually
 * doing would be the noise this gate exists to prevent. Focus would also make
 * the behaviour flaky — merely clicking another window would start delivering
 * notifications again.
 *
 * Minimized counts as not-on-screen: the window exists but nothing of it is
 * displayed. That, and hidden/destroyed windows, are exactly when a
 * notification is worth showing.
 */
function isWindowOnScreen(): boolean {
  const w = getMainWindow()
  if (!w) return false
  if (w.isDestroyed()) return false
  if (w.isMinimized()) return false
  return w.isVisible()
}

/** Show a native OS notification. Clicking navigates to the session/task in the renderer. */
export function showTerminalNotification(title: string, body: string, nav?: NotificationNav): void {
  if (!Notification.isSupported()) return

  // The window is open on screen — the user already sees the app, so an OS
  // notification would only duplicate what the in-app completion card (and the
  // session list) show. This is the authoritative check: the renderer's own
  // visibility test cannot see a minimized or hidden window.
  if (isWindowOnScreen()) return

  const n = new Notification({ title, body })
  n.on('click', () => {
    if (nav) sendNavToRenderer(channelFor(nav), nav)
  })
  n.show()
}
