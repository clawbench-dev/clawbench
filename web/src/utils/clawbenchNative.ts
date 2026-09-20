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
  /**
   * True only in the Electron desktop shell (Android returns false/undefined).
   * Both hosts report isNativeApp() === true, so this is what distinguishes
   * "window minimized" (desktop, keeps running) from "app backgrounded"
   * (Android, may be suspended).
   */
  isDesktopApp?(): boolean
  /** Optional (Electron): report that notification-click listeners are registered. */
  rendererReady?(): void
  getLanguage(): string
  /** Persist the language selected in the Web frontend to native prefs so native UI (splash, login page) follows it. */
  setLanguage?(lang: string): void
  showServerDialog(): void
  openSession(sessionId: string): void
  setNativePushEnabled(enabled: boolean): void
  /** Enable/disable the floating session status window (Android; no-op on desktop). */
  setFloatingWindowEnabled(enabled: boolean): void
  /** Read the persisted floating status window state (Android; no-op on desktop). */
  getFloatingWindowEnabled(): boolean
  /** Enable/disable the Android 16 Live Updates status chip (Android; no-op on desktop). */
  setLiveUpdateEnabled(enabled: boolean): void
  /** Read the persisted Live Updates chip state (Android; no-op on desktop). */
  getLiveUpdateEnabled(): boolean
  /** Whether the system can currently promote Live Updates for this app (Android; false on desktop). */
  canPostPromotedNotifications?(): boolean
  /** Open the system screen to enable Live Updates for this app (Android; no-op on desktop). */
  openLiveUpdateSettings?(): void
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

  /** Optional (Electron/Android): clear the HTTP cache and hard-reload the page. Used after upgrades. */
  reloadApp?(): void | Promise<void>
  /** Optional (Electron): show a native OS notification. Click dispatches session/task navigation. */
  nativeNotify?(title: string, body: string, nav?: NotificationNav): Promise<void>
  /** Optional (Electron/Android): sync native UI (status bar, splash, floating window) with the app theme. */
  setTheme?(themeId: string, bg?: string, text?: string, textSecondary?: string, accent?: string): void
  /** Optional (Android): get the persisted app theme ID. */
  getTheme?(): string
}

/** Navigation target for a native notification click. */
export interface NotificationNav {
  sessionId?: string
  taskId?: string
  executionId?: string
  projectPath?: string
  /**
   * Set for forge (GitHub/GitLab) change notifications. They carry no
   * session/task id — only `projectPath` — so the native shell needs this
   * explicit discriminator to route the click to the Issues & PRs tab.
   */
  forge?: boolean
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
 * True only in the Electron desktop shell. Android's bridge has no
 * isDesktopApp(), so the optional call safely resolves to false there — which
 * is the point: Android must keep its background-suspension behaviour.
 */
export function isDesktopApp(): boolean {
  try {
    if (window !== window.top) return false
    return getNative()?.isDesktopApp?.() === true
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
