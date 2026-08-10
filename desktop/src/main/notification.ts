import { Notification } from 'electron'
import { getMainWindow } from './window'

export function getPendingNavigationImpl(): string | null { return null }

export function dispatchOpenSession(sessionId: string | null): void {
  const w = getMainWindow()
  if (!w) return
  w.webContents.send('clawbench-open-session', { sessionId })
  if (sessionId) w.focus()
}

export function showTerminalNotification(title: string, body: string, sessionId: string): void {
  if (!Notification.isSupported()) return
  const n = new Notification({ title, body })
  n.on('click', () => {
    dispatchOpenSession(sessionId)
  })
  n.show()
}
