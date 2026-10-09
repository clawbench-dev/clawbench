import { Notification } from 'electron'
import { getMainWindow } from './window'
import { isRendererReady } from './navReady'
import type { NavChannel, NotificationNav } from '../shared/types'

let pendingNavigation: string | null = null

/**
 * Retained OS notification handles, keyed by subject ("task:<id>" /
 * "session:<id>"). Electron gives no way to enumerate or remove a notification
 * without its handle, so dismissal requires holding one from `show()` time.
 */
const activeNotifications = new Map<string, Notification>()

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
  // Retain the handle so it can be dismissed later. Without a retained handle
  // there is no way to remove a notification that is still in the OS tray: the
  // user reads the session by opening the app (not by clicking the
  // notification), and the stale notification then re-dispatches its deep link
  // on a later click. Keyed like the Android side (task wins over session).
  const key = notificationKey(nav)
  if (key) {
    // Electron notifications have no tag, so posting a second one for the same
    // subject would leave BOTH in the tray and a later dismiss would only close
    // the newer. Close the previous handle to mirror Android's replace-by-id
    // semantics (a task emits both `running` and `completed`).
    const prev = activeNotifications.get(key)
    if (prev) {
      try { prev.close() } catch { /* already gone */ }
    }
    activeNotifications.set(key, n)
  }
  n.on('close', () => {
    // Only forget the key if this instance is still the current one — a
    // replaced (superseded) notification's close must not evict its successor.
    if (key && activeNotifications.get(key) === n) activeNotifications.delete(key)
  })
  n.show()
}

/**
 * Dismiss the retained notification for a session or task.
 *
 * Called when the renderer learns the subject was marked read (opening the
 * session/task detail). Symmetric with Android's cancelEventNotification.
 * Best-effort: an unknown key is a no-op.
 */
export function dismissTerminalNotification(taskId?: string, sessionId?: string): void {
  const key = taskId ? `task:${taskId}` : sessionId ? `session:${sessionId}` : ''
  if (!key) return
  const n = activeNotifications.get(key)
  if (!n) return
  activeNotifications.delete(key)
  try {
    n.close()
  } catch {
    // Already closed / dismissed by the OS — nothing to do.
  }
}

/**
 * Stable identity for a notification, matching the renderer's nav payload.
 *
 * A task notification carries both a taskId and a sessionId; the task must win,
 * or a task's notification would be keyed to its session and a later session
 * read would dismiss the wrong thing (and vice versa).
 */
function notificationKey(nav?: NotificationNav): string | null {
  if (!nav) return null
  if (nav.taskId) return `task:${nav.taskId}`
  if (nav.sessionId) return `session:${nav.sessionId}`
  return null
}

/**
 * Test-only: drop all retained handles without closing them.
 *
 * `activeNotifications` is module-level state that outlives a single test, so
 * suites must reset it or handles from earlier cases leak into later ones.
 */
export function _resetActiveNotificationsForTesting(): void {
  activeNotifications.clear()
}
