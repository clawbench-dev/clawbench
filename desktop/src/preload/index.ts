import { contextBridge, ipcRenderer } from 'electron'

const invoke = (channel: string, ...args: unknown[]) => ipcRenderer.invoke(channel, ...args)

contextBridge.exposeInMainWorld('ClawBenchNative', {
  // sync
  isNativeApp: () => true,
  getLanguage: () => (process.env.LANG || 'en').split('_')[0],
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
})
