import { app, ipcMain, shell, clipboard, nativeTheme } from 'electron'
import { DEFAULT_THEME_ID, isDarkThemeId, normalizeThemeId } from '../shared/theme'
import path from 'node:path'
import os from 'node:os'
import fs from 'node:fs'
import { getStore, initStore } from './store'
import { getPassword, savePassword } from './secrets'
import { addForwardedPort, removeForwardedPort as rmFwd, addReverseForwardedPort, removeReverseForwardedPort as rmReverseFwd,
  getForwardedPorts, isTunnelConnected, getTunnelError, getTunnelErrorType, testPortReachable, reconnectTunnel } from './tunnel'
import { getMainWindow, createMainWindow, openSandboxWindow, showLoginPage } from './window'
import { downloadFileByPath, downloadFileByPathTo, downloadByUrl, downloadBlob } from './download'
import { setKeepScreenOnImpl } from './powersave'
import { dispatchOpenSession, getPendingNavigationJson, showTerminalNotification } from './notification'
import { markRendererReady } from './navReady'
import { clearCacheAndReload } from './session'
import { record, recordError, startClientLog, stopClientLog } from './clientLog'
import { classifyUrl } from './urlPolicy'

export function registerBridge(): void {
  initStore()

  ipcMain.on('native:get-language', (e) => {
    // Prefer the language the user picked in the web UI; fall back to the OS
    // locale on first run (before the web app has reported a preference).
    const stored = getStore().get('language')
    e.returnValue = stored || (app.getLocale().split(/[-_]/)[0] || 'en').toLowerCase()
  })
  ipcMain.on('native:set-language', (_e, lang: string) => {
    if (typeof lang === 'string' && lang) getStore().set('language', lang.toLowerCase())
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
    // Ensure the connected server is in the saved list so the login page shows it.
    const servers = getStore().get('servers')
    if (!servers.some(s => s.url === url)) {
      servers.unshift({ url, password: password || '' })
      getStore().set('servers', servers)
    }
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
  ipcMain.handle('native:add-reverse-forwarded-port', (_e, s: number, t: number, h: string) => addReverseForwardedPort(s, t, h))
  ipcMain.handle('native:remove-reverse-forwarded-port', (_e, s: number) => rmReverseFwd(s))
  ipcMain.handle('native:reconnect-tunnel', () => reconnectTunnel())
  ipcMain.handle('native:get-pending-navigation', () => getPendingNavigationJson())

  ipcMain.handle('native:download-file', (_e, filePath: string) => downloadFileByPath(filePath))
  ipcMain.handle('native:download-url', (_e, url: string, fileName: string) => downloadByUrl(url, fileName))
  ipcMain.handle('native:download-blob', (_e, b64: string, fileName: string) => downloadBlob(b64, fileName))
  ipcMain.handle('native:open-in-browser', (_e, port: number, protocol: string, host: string, p: string) => {
    shell.openExternal(`${protocol}://localhost:${port}${p || '/'}`)
  })
  // Open an http(s) URL in the default browser. The renderer routes the
  // settings "About" links (project homepage / issue tracker) through here.
  // The scheme is re-checked in the main process: `shell.openExternal` hands
  // the URL to the OS, so a renderer-side check alone would make any future
  // injection an arbitrary protocol-handler launcher. Only 'block' is refused
  // — an allow-list of origins here would silently swallow the link.
  ipcMain.handle('native:open-external-url', (_e, url: string) => {
    if (classifyUrl(url, getStore().get('serverUrl')) !== 'block') {
      void shell.openExternal(url)
    }
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
    // Opens the local desktop.log, mirrors the renderer console into it, and
    // arms the HTTP relay to /api/client-log (source="electron").
    startClientLog(w ? w.webContents : null)
    return Promise.resolve()
  })
  ipcMain.handle('native:stop-log-capture', () => {
    const w = getMainWindow()
    stopClientLog(w ? w.webContents : null)
    return Promise.resolve()
  })
  ipcMain.handle('native:reload-app', () => clearCacheAndReload())
  ipcMain.handle('native:notify', (_e, title: string, body: string, nav?: unknown) => {
    showTerminalNotification(title, body, nav as { sessionId?: string; taskId?: string; executionId?: string; projectPath?: string } | undefined)
    return Promise.resolve()
  })

  ipcMain.on('native:show-server-dialog', () => showLoginPage())
  ipcMain.on('native:open-session', (_e, id: string) => dispatchOpenSession(id))
  // The renderer signals that its notification-click listeners are registered.
  // Until then a clicked notification is stashed rather than sent into a page
  // that would drop it.
  ipcMain.on('native:renderer-ready', () => markRendererReady())
  ipcMain.on('native:set-push-enabled', (_e, enabled: boolean) => getStore().set('nativePushEnabled', enabled))
  ipcMain.on('native:update-last-seen', (_e, id: string) => { /* desktop has no SharedPreferences */ })
  ipcMain.on('native:keep-screen-on', (_e, on: boolean) => setKeepScreenOnImpl(on))
  // Normalise on read: an older build may have persisted a collapsed
  // 'dark'/'light', which matches no [data-theme] rule and would leave the
  // login page with every colour variable undefined.
  ipcMain.on('native:get-theme', (e) => { e.returnValue = normalizeThemeId(getStore().get('theme')) })
  ipcMain.on('native:set-theme', (_e, theme: string) => {
    // Persist the full theme ID (e.g. 'github-dark', 'nord'), NOT a collapsed
    // 'dark'/'light'. The login page resolves colours from
    // [data-theme="<id>"] and has no rule for a bare 'dark', so collapsing here
    // made the login page fall back to its defaults and never match the main
    // UI. Android stores the raw ID for the same reason (ThemePalette).
    const id = theme || DEFAULT_THEME_ID
    getStore().set('theme', id)
    // nativeTheme only understands light/dark, so derive that from the ID while
    // keeping the full ID in the store.
    nativeTheme.themeSource = isDarkThemeId(id) ? 'dark' : 'light'
  })
  // Renderer-side appLog relayed through the native bridge. Android sends these
  // to logcat; the desktop shell has no such sink, so they go to desktop.log
  // (and the server relay while capture is on) instead of being dropped.
  ipcMain.on('native:log', (_e, level: string, tag: string, msg: string) => {
    const letter = (['D', 'I', 'W', 'E'].includes(level) ? level : 'I') as 'D' | 'I' | 'W' | 'E'
    record(letter, tag || 'Renderer', msg)
  })
}
