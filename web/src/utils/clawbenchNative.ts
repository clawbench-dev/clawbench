/**
 * Platform-agnostic native bridge wrapper.
 *
 * The host app (Android WebView or Electron) injects a `window.ClawBenchNative`
 * object implementing this contract. All methods that read state are async so a
 * synchronous Android @JavascriptInterface and an asynchronous Electron
 * ipcRenderer.invoke both work under `await`.
 */

/** Full bridge contract shared by Android and Electron. */
export interface ClawBenchNative {
  // Sync (preload / JS-interface local values)
  isNativeApp(): boolean
  getLanguage(): string
  showServerDialog(): void
  openSession(sessionId: string): void
  setNativePushEnabled(enabled: boolean): void
  updateLastSeenEventId(id: string): void
  setKeepScreenOn(on: boolean): void
  log(level: string, tag: string, msg: string): void
  /** Dismiss the host splash overlay once the app is ready (Android; no-op on desktop). */
  dismissSplash(): void
  /** Stop the host background service when no ports are enabled (Android; no-op on desktop). */
  stopBackgroundService(): void
  /** Forward hardware volume keys to the terminal (Android; no-op on desktop). */
  setVolumeKeyMode(enabled: boolean): void
  /** Update the terminal session count shown in the host notification (Android; no-op on desktop). */
  setTerminalSessionCount(count: number): void
  /** Chinese OEM with aggressive background management (Android; no-op on desktop). */
  isChineseOem(): boolean
  getOemName(): string
  isOemAutoStartPrompted(): boolean
  setOemAutoStartPrompted(): void
  openOemAutoStartSettings(): boolean
  openOemBatterySettings(): boolean

  // Async reads (main-process / native state)
  getAppVersion(): Promise<string>
  getServerList(): Promise<string>
  getSavedServerConfig(): Promise<string>
  getServerUrl(): Promise<string>
  getPassword(): Promise<string>
  getForwardedPorts(): Promise<string>
  testPortReachable(localPort: number): Promise<boolean>
  isTunnelConnected(): Promise<boolean>
  getTunnelError(): Promise<string>
  getTunnelErrorType(): Promise<string>
  getPendingNavigation(): Promise<string>

  // Async writes / actions
  saveServer(url: string, password: string): Promise<void>
  removeServer(url: string): Promise<void>
  setSSHPassword(pwd: string): Promise<void>
  connectToServer(url: string, password: string): Promise<void>
  addForwardedPort(localPort: number, targetPort: number, host: string): Promise<void>
  removeForwardedPort(localPort: number): Promise<void>
  reconnectTunnel(): Promise<boolean>
  reconnectTunnelAsync(): Promise<void>
  downloadFile(path: string): Promise<void>
  downloadUrl(url: string, fileName: string): Promise<void>
  downloadBlob(base64: string, fileName: string): Promise<void>
  openInBrowser(port: number, protocol: string, host: string, path: string): Promise<void>
  openInSandbox(port: number, protocol: string, host: string, path: string, sessionId?: string): Promise<void>
  startLogCapture(): Promise<void>
  stopLogCapture(): Promise<void>
  shareText(text: string): Promise<void>
  shareFile(path: string, mime: string): Promise<void>
  shareFiles(paths: string, mimes: string): Promise<void>

  /** Optional (Electron): show a native OS notification. Click dispatches session/task navigation. */
  nativeNotify?(title: string, body: string, nav?: NotificationNav): Promise<void>
  /** Optional (Electron): sync native title bar / dialogs with the app theme. */
  setTheme?(theme: 'dark' | 'light'): void
}

/** Navigation target for a native notification click. */
export interface NotificationNav {
  sessionId?: string
  taskId?: string
  executionId?: string
  projectPath?: string
}

const bridgeWindow = window as unknown as { ClawBenchNative?: ClawBenchNative }

/** Get the injected bridge, or undefined when running as plain web. */
export function getNative(): ClawBenchNative | undefined {
  return bridgeWindow.ClawBenchNative
}

/** True when running inside a native host app (top-level frame). */
export function isNativeApp(): boolean {
  try {
    if (window !== window.top) return false
    return getNative()?.isNativeApp() === true
  } catch {
    return false
  }
}

/**
 * Call a method on the bridge, resolving undefined when the bridge is missing.
 * Works for both synchronous (Android) and asynchronous (Electron) results.
 */
export async function callNative<T>(fn: (n: ClawBenchNative) => T | Promise<T>): Promise<T | undefined> {
  const native = getNative()
  if (!native) return undefined
  return await fn(native)
}

const RECONNECT_CALLBACK_NAME = '__clawbenchReconnectResult'

/**
 * Reconnect the SSH tunnel, resolving with success boolean.
 * - Electron: native.reconnectTunnelAsync returns a Promise.
 * - Android legacy: fire-and-forget + global callback (window.__clawbenchReconnectResult).
 * - Android old: blocking native.reconnectTunnel returns boolean.
 */
export function reconnectTunnel(): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    const native = getNative()
    if (!native) return resolve(false)

    if (native.reconnectTunnelAsync) {
      let result: unknown
      try {
        result = native.reconnectTunnelAsync()
      } catch {
        return resolve(false)
      }
      // Electron-style Promise
      if (result && typeof (result as Promise<boolean>).then === 'function') {
        ;(result as Promise<boolean>).then(resolve).catch(() => resolve(false))
        return
      }
      // Android-style global callback + safety timeout
      let settled = false
      const done = (success: boolean) => {
        if (settled) return
        settled = true
        delete (window as unknown as Record<string, unknown>)[RECONNECT_CALLBACK_NAME]
        resolve(success)
      }
      ;(window as unknown as Record<string, unknown>)[RECONNECT_CALLBACK_NAME] = (success: boolean) => done(success)
      setTimeout(() => done(false), 16000)
      return
    }

    if (native.reconnectTunnel) {
      Promise.resolve(native.reconnectTunnel()).then(resolve).catch(() => resolve(false))
      return
    }
    resolve(false)
  })
}
