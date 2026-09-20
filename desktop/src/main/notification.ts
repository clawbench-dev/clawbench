import { Notification } from 'electron'
import { getMainWindow } from './window'
import type { NotificationNav } from '../shared/types'

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
  if (sessionId) w.focus()
}

/** Deliver a navigation to the renderer, or defer it if the window is still loading (cold start). */
function sendNavToRenderer(channel: 'clawbench-open-session' | 'clawbench-open-task', nav: NotificationNav): void {
  const w = getMainWindow()
  if (!w) return
  if (w.webContents.isLoading()) {
    pendingNavigation = JSON.stringify(nav)
  } else {
    w.webContents.send(channel, nav)
    w.focus()
  }
}

/** Show a native OS notification. Clicking navigates to the session/task in the renderer. */
export function showTerminalNotification(title: string, body: string, nav?: NotificationNav): void {
  if (!Notification.isSupported()) return
  const n = new Notification({ title, body })
  n.on('click', () => {
    if (nav?.taskId) sendNavToRenderer('clawbench-open-task', nav)
    else if (nav?.sessionId) sendNavToRenderer('clawbench-open-session', nav)
  })
  n.show()
}
