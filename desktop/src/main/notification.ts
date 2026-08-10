import { Notification } from 'electron'
import { getMainWindow } from './window'
import type { NotificationNav } from '../shared/types'

export function getPendingNavigationImpl(): string | null { return null }

export function dispatchOpenSession(sessionId: string | null): void {
  const w = getMainWindow()
  if (!w) return
  w.webContents.send('clawbench-open-session', { sessionId })
  if (sessionId) w.focus()
}

/** Show a native OS notification. Clicking navigates to the session/task in the renderer. */
export function showTerminalNotification(title: string, body: string, nav?: NotificationNav): void {
  if (!Notification.isSupported()) return
  const n = new Notification({ title, body })
  n.on('click', () => {
    const w = getMainWindow()
    if (!w) return
    if (nav?.taskId) {
      w.webContents.send('clawbench-open-task', nav)
    } else if (nav?.sessionId) {
      w.webContents.send('clawbench-open-session', { sessionId: nav.sessionId, projectPath: nav.projectPath })
    }
    w.focus()
  })
  n.show()
}
