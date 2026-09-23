import { contextBridge, ipcRenderer } from 'electron'

const invoke = (channel: string, ...args: unknown[]) => ipcRenderer.invoke(channel, ...args)

// Notification clicks arrive from the main process over IPC. The renderer
// listens for window CustomEvents (App.vue), NOT for IPC — so forward each
// channel into a CustomEvent, mirroring what the Android shell does with
// `evaluateJavascript(window.dispatchEvent(new CustomEvent(...)))`. Without
// this bridge the main process's webContents.send() has no receiver and
// clicking a notification silently does nothing.
//
// The channel list is duplicated from shared/types.ts rather than imported:
// the main window uses Electron's default `sandbox: true`, and a sandboxed
// preload cannot require() local files ("module not found"). Importing it
// would throw at preload load time and take down the ENTIRE ClawBenchNative
// bridge, not just notifications. `notification.test.ts` guards the copy.
const NAV_CHANNELS = ['clawbench-open-session', 'clawbench-open-task', 'clawbench-open-forge']

for (const channel of NAV_CHANNELS) {
  ipcRenderer.on(channel, (_e, detail: unknown) => {
    window.dispatchEvent(new CustomEvent(channel, { detail }))
  })
}

contextBridge.exposeInMainWorld('ClawBenchNative', {
  // sync
  isNativeApp: () => true,
  // Distinguishes the desktop shell from the Android WebView. Both report
  // isNativeApp() === true, but only Android needs the background-tab
  // behaviour (drop the WebSocket when hidden). The desktop window is
  // minimized rather than backgrounded, and dropping the socket there means
  // no notification can ever arrive — the exact opposite of the point of the
  // desktop shell. See useAppMode / useGlobalEvents.
  isDesktopApp: () => true,
  // Tells the main process that the page's notification-click listeners are
  // registered. Until then a clicked notification is deferred instead of being
  // sent to a page that would drop it.
  rendererReady: () => { ipcRenderer.send('native:renderer-ready') },
  getLanguage: () => {
    try { return ipcRenderer.sendSync('native:get-language') } catch { return 'en' }
  },
  // Language chosen in the web UI. Native surfaces rendered outside the page
  // (context-menu labels, the first-run login page) read it back via
  // getLanguage(), so the shell follows the user's choice rather than the OS
  // locale. Optional in the shared interface, so a missing handler is safe.
  setLanguage: (lang: string) => { ipcRenderer.send('native:set-language', lang) },
  showServerDialog: () => { ipcRenderer.send('native:show-server-dialog') },
  openSession: (sessionId: string) => { ipcRenderer.send('native:open-session', sessionId) },
  setNativePushEnabled: (enabled: boolean) => { ipcRenderer.send('native:set-push-enabled', enabled) },
  updateLastSeenEventId: (id: string) => { ipcRenderer.send('native:update-last-seen', id) },
  setKeepScreenOn: (on: boolean) => { ipcRenderer.send('native:keep-screen-on', on) },
  log: (level: string, tag: string, msg: string) => { ipcRenderer.send('native:log', level, tag, msg) },
  dismissSplash: () => { /* desktop has no native splash overlay */ },
  stopBackgroundService: () => { /* desktop has no Android foreground service */ },
  setVolumeKeyMode: () => { /* desktop has no hardware volume keys */ },
  setTerminalSessionCount: () => { /* desktop has no status-bar terminal badge */ },
  isChineseOem: () => false,
  getOemName: () => '',
  isOemAutoStartPrompted: () => false,
  setOemAutoStartPrompted: () => {},
  openOemAutoStartSettings: () => false,
  openOemBatterySettings: () => false,

  // async reads
  getAppVersion: () => invoke('native:get-app-version'),
  getServerList: () => invoke('native:get-server-list'),
  getSavedServerConfig: () => invoke('native:get-saved-server-config'),
  getServerUrl: () => invoke('native:get-server-url'),
  getPassword: () => invoke('native:get-password'),
  getForwardedPorts: () => invoke('native:get-forwarded-ports'),
  testPortReachable: (p: number) => invoke('native:test-port-reachable', p),
  isTunnelConnected: () => invoke('native:is-tunnel-connected'),
  getTunnelError: () => invoke('native:get-tunnel-error'),
  getTunnelErrorType: () => invoke('native:get-tunnel-error-type'),
  getPendingNavigation: () => invoke('native:get-pending-navigation'),

  // async writes
  saveServer: (u: string, p: string) => invoke('native:save-server', u, p),
  removeServer: (u: string) => invoke('native:remove-server', u),
  setSSHPassword: (p: string) => invoke('native:set-ssh-password', p),
  connectToServer: (u: string, p: string) => invoke('native:connect-to-server', u, p),
  addForwardedPort: (l: number, t: number, h: string) => invoke('native:add-forwarded-port', l, t, h),
  removeForwardedPort: (l: number) => invoke('native:remove-forwarded-port', l),
  addReverseForwardedPort: (s: number, t: number, h: string) => invoke('native:add-reverse-forwarded-port', s, t, h),
  removeReverseForwardedPort: (s: number) => invoke('native:remove-reverse-forwarded-port', s),
  reconnectTunnel: () => invoke('native:reconnect-tunnel'),
  reconnectTunnelAsync: () => invoke('native:reconnect-tunnel'),
  downloadFile: (path: string) => invoke('native:download-file', path),
  downloadUrl: (url: string, fileName: string) => invoke('native:download-url', url, fileName),
  downloadBlob: (b64: string, fileName: string) => invoke('native:download-blob', b64, fileName),
  openInBrowser: (port: number, protocol: string, host: string, path: string) => invoke('native:open-in-browser', port, protocol, host, path),
  openInSandbox: (port: number, protocol: string, host: string, path: string, sessionId?: string) => invoke('native:open-in-sandbox', port, protocol, host, path, sessionId),
  startLogCapture: () => invoke('native:start-log-capture'),
  stopLogCapture: () => invoke('native:stop-log-capture'),
  shareText: (text: string) => invoke('native:share-text', text),
  shareFile: (path: string, mime: string) => invoke('native:share-file', path, mime),
  shareFiles: (paths: string, mimes: string) => invoke('native:share-files', paths, mimes),
  openExternalUrl: (url: string) => invoke('native:open-external-url', url),
  nativeNotify: (title: string, body: string, nav?: unknown) => invoke('native:notify', title, body, nav),
  reloadApp: () => invoke('native:reload-app'),
  setTheme: (theme: string, bg?: string, text?: string, textSecondary?: string, accent?: string) => {
    // Desktop has no floating window; only the theme id is meaningful here.
    ipcRenderer.send('native:set-theme', theme, bg ?? null, text ?? null, textSecondary ?? null, accent ?? null)
  },
  getTheme: () => { try { return ipcRenderer.sendSync('native:get-theme') } catch { return 'dark' } },
  // Native page zoom (appearance "auto scale"). Fire-and-forget: the main
  // process validates the factor and applies it to the window.
  setZoomFactor: (factor: number) => { ipcRenderer.send('native:set-zoom-factor', factor) },
})
