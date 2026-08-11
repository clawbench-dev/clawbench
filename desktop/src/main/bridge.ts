import { app, ipcMain, shell, clipboard, nativeTheme } from 'electron'
import path from 'node:path'
import os from 'node:os'
import fs from 'node:fs'
import { getStore, initStore } from './store'
import { getPassword, savePassword } from './secrets'
import { addForwardedPort, removeForwardedPort as rmFwd,
  getForwardedPorts, isTunnelConnected, getTunnelError, getTunnelErrorType, testPortReachable, reconnectTunnel } from './tunnel'
import { getMainWindow, createMainWindow, openSandboxWindow, showLoginPage } from './window'
import { downloadFileByPath, downloadFileByPathTo, downloadByUrl, downloadBlob } from './download'
import { setKeepScreenOnImpl } from './powersave'
import { dispatchOpenSession, getPendingNavigationJson, showTerminalNotification } from './notification'

let logFileStream: fs.WriteStream | null = null
let logListener: ((event: Electron.Event, level: number, message: string) => void) | null = null

export function registerBridge(): void {
  initStore()

  ipcMain.on('native:get-language', (e) => {
    e.returnValue = (app.getLocale().split(/[-_]/)[0] || 'en').toLowerCase()
  })
  ipcMain.handle('native:get-app-version', () => app.getVersion())
  ipcMain.handle('native:get-server-list', () => JSON.stringify(getStore().get('servers')))
  ipcMain.handle('native:get-saved-server-config', () => {
    const u = getStore().get('serverUrl')
    if (!u) return '{}'
    const url = new URL(u)
    return JSON.stringify({ protocol: url.protocol.replace(':', ''), host: url.hostname, port: url.port || '', password: getPassword() })
  })
  ipcMain.handle('native:get-server-url', () => getStore().get('serverUrl'))
  ipcMain.handle('native:get-password', () => getPassword())

  ipcMain.handle('native:save-server', (_e, url: string, password: string) => {
    const servers = getStore().get('servers')
    const idx = servers.findIndex(s => s.url === url)
    if (idx >= 0) servers[idx].password = password
    else servers.unshift({ url, password })
    getStore().set('servers', servers)
  })
  ipcMain.handle('native:remove-server', (_e, url: string) => {
    getStore().set('servers', getStore().get('servers').filter(s => s.url !== url))
  })
  ipcMain.handle('native:set-ssh-password', (_e, p: string) => savePassword(p))
  ipcMain.handle('native:connect-to-server', (_e, url: string, password: string) => {
    getStore().set('serverUrl', url)
    if (password) savePassword(password)
    const w = getMainWindow()
    if (w) { w.loadURL(url) }
    else { createMainWindow() }
  })

  ipcMain.handle('native:get-forwarded-ports', () => JSON.stringify(getForwardedPorts()))
  ipcMain.handle('native:test-port-reachable', (_e, p: number) => testPortReachable(p))
  ipcMain.handle('native:is-tunnel-connected', () => isTunnelConnected())
  ipcMain.handle('native:get-tunnel-error', () => getTunnelError())
  ipcMain.handle('native:get-tunnel-error-type', () => getTunnelErrorType())
  ipcMain.handle('native:add-forwarded-port', (_e, l: number, t: number, h: string) => addForwardedPort(l, t, h))
  ipcMain.handle('native:remove-forwarded-port', (_e, l: number) => rmFwd(l))
  ipcMain.handle('native:reconnect-tunnel', () => reconnectTunnel())
  ipcMain.handle('native:get-pending-navigation', () => getPendingNavigationJson())

  ipcMain.handle('native:download-file', (_e, filePath: string) => downloadFileByPath(filePath))
  ipcMain.handle('native:download-url', (_e, url: string, fileName: string) => downloadByUrl(url, fileName))
  ipcMain.handle('native:download-blob', (_e, b64: string, fileName: string) => downloadBlob(b64, fileName))
  ipcMain.handle('native:open-in-browser', (_e, port: number, protocol: string, host: string, p: string) => {
    shell.openExternal(`${protocol}://localhost:${port}${p || '/'}`)
  })
  ipcMain.handle('native:open-in-sandbox', (_e, port: number, protocol: string, host: string, p: string, sessionId?: string) => {
    openSandboxWindow(port, protocol, host, p)
  })

  ipcMain.handle('native:share-text', (_e, text: string) => { clipboard.writeText(text); return Promise.resolve() })
  ipcMain.handle('native:share-file', async (_e, filePath: string, mime: string) => {
    const name = path.basename(filePath)
    const tmp = path.join(os.tmpdir(), `clawbench-share-${Date.now()}-${name}`)
    await downloadFileByPathTo(filePath, tmp)
    shell.openPath(tmp)
  })
  ipcMain.handle('native:share-files', async (_e, pathsJson: string) => {
    const paths: string[] = JSON.parse(pathsJson || '[]')
    if (paths.length === 0) return
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'clawbench-share-'))
    for (const p of paths) {
      try {
        await downloadFileByPathTo(p, path.join(tmpDir, path.basename(p)))
      } catch { /* skip unshareable */ }
    }
    shell.openPath(tmpDir)
  })
  ipcMain.handle('native:start-log-capture', () => {
    const w = getMainWindow()
    if (!w || logFileStream) return Promise.resolve()
    const file = path.join(app.getPath('userData'), 'desktop.log')
    logFileStream = fs.createWriteStream(file, { flags: 'a' })
    logListener = (_event: Electron.Event, level: number, message: string) => {
      logFileStream?.write(`[${new Date().toISOString()}] [${level}] ${message}\n`)
    }
    w.webContents.on('console-message', logListener)
    return Promise.resolve()
  })
  ipcMain.handle('native:stop-log-capture', () => {
    const w = getMainWindow()
    if (w && logListener) w.webContents.removeListener('console-message', logListener)
    logListener = null
    if (logFileStream) { logFileStream.end(); logFileStream = null }
    return Promise.resolve()
  })
  ipcMain.handle('native:notify', (_e, title: string, body: string, nav?: unknown) => {
    showTerminalNotification(title, body, nav as { sessionId?: string; taskId?: string; executionId?: string; projectPath?: string } | undefined)
    return Promise.resolve()
  })

  ipcMain.on('native:show-server-dialog', () => showLoginPage())
  ipcMain.on('native:open-session', (_e, id: string) => dispatchOpenSession(id))
  ipcMain.on('native:set-push-enabled', (_e, enabled: boolean) => getStore().set('nativePushEnabled', enabled))
  ipcMain.on('native:update-last-seen', (_e, id: string) => { /* desktop has no SharedPreferences */ })
  ipcMain.on('native:keep-screen-on', (_e, on: boolean) => setKeepScreenOnImpl(on))
  ipcMain.on('native:set-theme', (_e, theme: string) => {
    nativeTheme.themeSource = theme === 'light' ? 'light' : 'dark'
  })
  ipcMain.on('native:log', (_e, level: string, tag: string, msg: string) => { /* route to main log */ })
}
