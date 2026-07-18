import { ref, watch } from 'vue'
import { apiGet, apiPost, apiPut, apiDelete } from '@/utils/api'
import { useAppMode } from './useAppMode.ts'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast.ts'
import { tunnelStatusFromPorts as tunnelStatusFromPortsUtil, buildPortUrl } from '@/utils/portForwardUtils.ts'
import { store } from '@/stores/app'
import { appLog } from '@/utils/appLog'

const TAG = 'PortForward'

// Java forceReconnectAsync timeout (15s) + 1s buffer
const RECONNECT_ASYNC_TIMEOUT_MS = 16000
// Global callback name for async reconnect (must match Java-side callbackJsExpr)
const RECONNECT_CALLBACK_NAME = '__clawbenchReconnectResult'

/** Shared async reconnect helper — prefers reconnectTunnelAsync (non-blocking)
 *  to avoid ANR on the JavaBridge thread, falls back to blocking reconnectTunnel
 *  for old APKs.
 *  Note: window[RECONNECT_CALLBACK_NAME] is cleaned up by `done()`, called either
 *  by the native callback or the safety timeout. Concurrent calls may overwrite
 *  this global — the `settled` flag prevents double-resolution. */
async function reconnectTunnelAsync(native: AndroidNativeBridge): Promise<boolean> {
  if (native?.reconnectTunnelAsync) {
    return new Promise<boolean>((resolve) => {
      let settled = false
      const done = (success: boolean) => {
        if (settled) return
        settled = true
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        delete (window as any)[RECONNECT_CALLBACK_NAME]
        resolve(success)
      }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      ;(window as any)[RECONNECT_CALLBACK_NAME] = (success: boolean) => done(success)
      // Safety timeout in case the native callback never fires
      setTimeout(() => done(false), RECONNECT_ASYNC_TIMEOUT_MS)
      native.reconnectTunnelAsync!()
    })
  } else if (native?.reconnectTunnel) {
    // Fallback: blocking version (for old APKs without reconnectTunnelAsync)
    return native.reconnectTunnel()
  }
  return false
}

interface ForwardedPort {
  port: number        // Target port on remote host
  localPort: number   // Local listening port (auto-assigned)
  host: string
  name: string
  protocol: string
  active: boolean
}

interface DetectedPort {
  port: number
  protocol: string
  processName: string
  processArgs: string
}

interface AndroidNativeBridge {
  addForwardedPort?: (localPort: number, targetPort: number, host: string) => void
  removeForwardedPort?: (localPort: number) => void
  stopBackgroundService?: () => void
  isTunnelConnected?: () => boolean | null
  getTunnelError?: () => string
  getTunnelErrorType?: () => string
  setVolumeKeyMode?: (enabled: boolean) => void
  openInSandbox?: (localPort: number, protocol: string, host: string, path: string) => void
  openInBrowser?: (localPort: number, protocol: string, host: string, path: string) => void
  testPortReachable?: (localPort: number) => boolean
  reconnectTunnel?: () => boolean
  reconnectTunnelAsync?: () => void
}

/** Get the Android native bridge from window, if available. */
function getAndroidNative(): AndroidNativeBridge | undefined {
  return (window as unknown as { AndroidNative?: AndroidNativeBridge }).AndroidNative
}

export interface SSHConnectionStats {
  connected: boolean
  clientCount: number
  activeChannels: number
  lastConnectedAt?: string
}

export interface SSHInfo {
  enabled: boolean
  host: string
  port: number
  username: string
  fingerprint: string
  command: string
  connectionStats: SSHConnectionStats | null
}

export type TunnelStatus = 'unknown' | 'ok' | 'disconnected' | 'degraded'

export type TunnelErrorType = 'auth' | 'network' | 'hostkey' | 'unknown' | ''

// Module-level shared state
const ports = ref<ForwardedPort[]>([])
const detectedPorts = ref<DetectedPort[]>([])
const loading = ref(false)
const sshInfo = ref<SSHInfo | null>(null)
const tunnelStatus = ref<TunnelStatus>('unknown')
const tunnelMessage = ref('')
const tunnelChecking = ref(false)
const tunnelError = ref('')
const tunnelErrorType = ref<TunnelErrorType>('')

// Ports that are newly registered and waiting for SSH tunnel to become reachable.
// These show a yellow blinking dot instead of green/grey.
const connectingPorts = ref(new Set<number>())

// Auto-refresh interval when tunnel is unhealthy
let tunnelPollTimer: ReturnType<typeof setInterval> | null = null

// Callback set by usePortForward() to handle port-forward-result events.
// We need this indirection because loadPorts() is defined inside usePortForward(),
// but the event listener is set up at module level.
let onPortForwardResult: ((localPort: number, success: boolean) => void) | null = null

// Module-level listener for port forward result callbacks from Android native layer.
// The native BackgroundService calls notifyPortForwardResult() which dispatches
// a 'clawbench-port-forward-result' CustomEvent after each addPortForward completes.
// This replaces the old polling-based startPortConnectCheck approach.
let portForwardListenerInitialized = false

function ensurePortForwardListener() {
  if (portForwardListenerInitialized) return
  portForwardListenerInitialized = true

  window.addEventListener('clawbench-port-forward-result', ((e: CustomEvent) => {
    if (onPortForwardResult) {
      const { localPort, success } = e.detail
      onPortForwardResult(localPort, success)
    }
  }) as EventListener)
}

/** Returns true if any registered port has an active backend */
function hasActivePorts(): boolean {
  return ports.value.some(p => p.active)
}

// Sync active port count to global store for dock badge
watch(ports, () => {
  store.state.portForwardActiveCount = ports.value.filter(p => p.active).length
}, { deep: true })

/**
 * Determines tunnel status from port state (delegates to pure utility).
 */
function tunnelStatusFromPorts(_hasPorts: boolean): 'ok' | 'degraded' {
  return tunnelStatusFromPortsUtil(ports.value)
}

/**
 * Manages port forwarding state: list of forwarded ports, CRUD operations,
 * auto-detection, and registration with Android native layer.
 */
export function usePortForward() {
  const { isAppMode } = useAppMode()

  // Set up the callback for native port-forward-result events.
  // This needs to be inside usePortForward() because it calls loadPorts()
  // which is defined here. The module-level event listener dispatches to this callback.
  if (!onPortForwardResult) {
    onPortForwardResult = (localPort: number, success: boolean) => {
      connectingPorts.value.delete(localPort)
      connectingPorts.value = new Set(connectingPorts.value)
      // Refresh port list to pick up the new active state from backend
      loadPorts(true)
      if (!success) {
        const toast = useToast()
        toast.show(gt('portForward.portUnreachable'), { type: 'error' })
      }
    }
  }

  async function loadPorts(silent = false) {
    if (!silent) loading.value = true
    try {
      const data = await apiGet<{ ports: ForwardedPort[] }>('/api/proxy/ports')
      ports.value = data.ports || []
      // Clear connectingPorts when backend reports a port as active.
      // In web mode this is the ONLY path (no native callback).
      // In app mode this is a safety net: the native clawbench-port-forward-result
      // callback may arrive BEFORE connectingPorts.add() runs (the await in
      // registerPort yields to the event loop, allowing the CustomEvent to fire
      // while connectingPorts is still empty), so the delete is a no-op and the
      // port gets stuck yellow forever. Checking here on every loadPorts() ensures
      // the yellow dot always clears once the backend confirms the port is active.
      if (connectingPorts.value.size > 0) {
        let changed = false
        for (const p of ports.value) {
          if (p.active && connectingPorts.value.has(p.localPort)) {
            connectingPorts.value.delete(p.localPort)
            changed = true
          }
        }
        if (changed) {
          connectingPorts.value = new Set(connectingPorts.value)
        }
      }
    } finally {
      if (!silent) loading.value = false
    }
  }

  async function registerPort(port: number, name?: string, protocol?: string, host?: string) {
    const result = await apiPost<{ localPort: number }>('/api/proxy/ports', { port, host: host || '', name: name || '', protocol: protocol || 'http' })
    // PRIVILEGED PORT POLICY: localPort may differ from port when the target port is
    // privileged (< 1024) — the backend remaps it to >= 1024 for Android/non-root.
    // Do NOT change this to assume localPort === port.
    const localPort = result?.localPort ?? port
    // Mark as "connecting" BEFORE calling native or awaiting anything.
    // The native clawbench-port-forward-result callback can fire at any time
    // after addForwardedPort (it runs on a background thread and dispatches
    // via runOnUiThread + evaluateJavascript). If we add to connectingPorts
    // AFTER the callback arrives, the delete in onPortForwardResult is a
    // no-op and the port gets stuck yellow forever.
    ensurePortForwardListener()
    connectingPorts.value.add(localPort)
    connectingPorts.value = new Set(connectingPorts.value)
    // Register with Android native layer: pass localPort, targetPort, host
    if (isAppMode.value) {
      appLog.d(TAG, 'registerPort: localPort=' + localPort + ', targetPort=' + port + ', host=' + (host || ''))
      ;getAndroidNative()?.addForwardedPort?.(localPort, port, host || '')
    }
    // Silent refresh: don't flicker the port list with loading state
    await Promise.all([loadPorts(true), loadSSHInfo()])
  }

  async function updatePort(localPort: number, port: number, host: string, name: string, protocol: string) {
    await apiPut('/api/proxy/ports', { localPort, port, host, name, protocol })
    // Re-sync native layer after update: remove old, add new with correct localPort
    if (isAppMode.value) {
      ;getAndroidNative()?.removeForwardedPort?.(localPort)
      ;getAndroidNative()?.addForwardedPort?.(localPort, port, host || '')
    }
    await Promise.all([loadPorts(true), loadSSHInfo()])
  }

  async function unregisterPort(localPort: number) {
    await apiDelete(`/api/proxy/ports?port=${localPort}`)
    if (isAppMode.value) {
      ;getAndroidNative()?.removeForwardedPort?.(localPort)
    }
    await Promise.all([loadPorts(true), loadSSHInfo()])
  }

  async function detectPorts() {
    const data = await apiGet<{ ports: DetectedPort[] }>('/api/proxy/detect')
    detectedPorts.value = data.ports || []
  }

  /** Sync all registered ports to Android native on initial load.
   *  If the server has no registered ports, stop the native service
   *  to avoid an idle foreground service draining battery. */
  async function syncToNative() {
    if (!isAppMode.value) return
    await loadPorts()
    if (ports.value.length === 0) {
      // No ports on server — stop the native service (clears stale SharedPreferences)
      ;getAndroidNative()?.stopBackgroundService?.()
      return
    }
    for (const p of ports.value) {
      ;getAndroidNative()?.addForwardedPort?.(p.localPort, p.port, p.host || '')
    }
  }

  /** Fetch SSH tunnel connection info from server */
  async function loadSSHInfo() {
    try {
      const data = await apiGet<SSHInfo>('/api/ssh/info')
      sshInfo.value = data
    } catch {
      sshInfo.value = null
    }
  }

  /** Check SSH tunnel health and determine status */
  async function checkTunnelHealth() {
    tunnelChecking.value = true
    tunnelStatus.value = 'unknown'
    tunnelMessage.value = ''
    tunnelError.value = ''
    tunnelErrorType.value = ''

    await Promise.all([loadPorts(), loadSSHInfo()])

    const info = sshInfo.value
    // No SSH configured — skip tunnel check (web mode without SSH)
    if (!info?.enabled) {
      tunnelChecking.value = false
      return
    }

    // In app mode: prefer native SSH tunnel status
    if (isAppMode.value) {
      const nativeConnected = getNativeTunnelStatus()
      if (nativeConnected === true) {
        // Native says connected — trust it regardless of server-side connCount
        const hasPorts = ports.value.length > 0
        const status = tunnelStatusFromPorts(hasPorts)
        if (status === 'degraded') {
          tunnelStatus.value = 'degraded'
          tunnelMessage.value = gt('portForward.tunnelDegraded')
          tunnelChecking.value = false
          startTunnelPoll()
          return
        }
        tunnelStatus.value = 'ok'
        tunnelChecking.value = false
        stopTunnelPoll()
        return
      } else if (nativeConnected === false) {
        // Query native layer for specific error details
        tunnelError.value = getNativeTunnelError()
        tunnelErrorType.value = getNativeTunnelErrorType()
        tunnelStatus.value = 'disconnected'
        tunnelMessage.value = gt('portForward.tunnelDisconnected')
        tunnelChecking.value = false
        startTunnelPoll()
        return
      }
    }

    // Native status unavailable — fall back to server-side connection stats
    const stats = info.connectionStats
    if (!stats) {
      tunnelChecking.value = false
      return
    }

    if (!stats.connected) {
      // Server says disconnected, but check if any ports are actually active
      // (health check passes = tunnel is working despite connCount=0)
      if (hasActivePorts()) {
        tunnelStatus.value = 'ok'
        tunnelChecking.value = false
        stopTunnelPoll()
        return
      }
      tunnelStatus.value = 'disconnected'
      tunnelMessage.value = gt('portForward.tunnelDisconnected')
      tunnelChecking.value = false
      startTunnelPoll()
      return
    }

    // SSH is connected — check if any ports have active backends
    const hasPorts = ports.value.length > 0
    if (tunnelStatusFromPorts(hasPorts) === 'degraded') {
      tunnelStatus.value = 'degraded'
      tunnelMessage.value = gt('portForward.tunnelDegraded')
      tunnelChecking.value = false
      startTunnelPoll()
      return
    }

    tunnelStatus.value = 'ok'
    tunnelChecking.value = false
    stopTunnelPoll()
  }

  /**
   * Query Android native layer for SSH tunnel connection status.
   * Returns true (connected), false (disconnected), or null (unavailable/not app mode).
   */
  function getNativeTunnelStatus(): boolean | null {
    if (!isAppMode.value) return null
    const native = getAndroidNative()
    if (!native || typeof native.isTunnelConnected !== 'function') return null
    try {
      const result = native.isTunnelConnected()
      if (typeof result === 'boolean') return result
      return null
    } catch {
      return null
    }
  }

  /**
   * Query Android native layer for the last SSH tunnel error.
   * Returns the error message string, or empty string if no error.
   */
  function getNativeTunnelError(): string {
    if (!isAppMode.value) return ''
    const native = getAndroidNative()
    if (!native || typeof native.getTunnelError !== 'function') return ''
    try {
      const result = native.getTunnelError()
      return typeof result === 'string' ? result : ''
    } catch {
      return ''
    }
  }

  /**
   * Query Android native layer for the last SSH tunnel error type.
   * Returns one of: 'auth', 'network', 'hostkey', 'unknown', or ''.
   */
  function getNativeTunnelErrorType(): TunnelErrorType {
    if (!isAppMode.value) return ''
    const native = getAndroidNative()
    if (!native || typeof native.getTunnelErrorType !== 'function') return ''
    try {
      const result = native.getTunnelErrorType()
      if (typeof result === 'string' && ['auth', 'network', 'hostkey', 'unknown', ''].includes(result)) {
        return result as TunnelErrorType
      }
      return ''
    } catch {
      return ''
    }
  }

  /** Start polling tunnel health every 5s while unhealthy */
  function startTunnelPoll() {
    if (tunnelPollTimer) return
    tunnelPollTimer = setInterval(async () => {
      // Check native status first (fast, no network)
      const nativeConnected = getNativeTunnelStatus()
      if (nativeConnected === true) {
        await loadPorts()
        const hasPorts = ports.value.length > 0
        if (tunnelStatusFromPorts(hasPorts) === 'ok') {
          tunnelStatus.value = 'ok'
          tunnelMessage.value = ''
          stopTunnelPoll()
        } else {
          tunnelStatus.value = 'degraded'
          tunnelMessage.value = gt('portForward.tunnelDegraded')
        }
        return
      }

      // Fall back to server-side check
      await loadSSHInfo()
      const info = sshInfo.value
      const stats = info?.connectionStats
      if (stats?.connected) {
        // Re-check full health (ports + ssh)
        await loadPorts()
        const hasPorts = ports.value.length > 0
        if (tunnelStatusFromPorts(hasPorts) === 'ok') {
          tunnelStatus.value = 'ok'
          tunnelMessage.value = ''
          stopTunnelPoll()
        } else {
          tunnelStatus.value = 'degraded'
          tunnelMessage.value = gt('portForward.tunnelDegraded')
        }
      } else {
        // Server says disconnected — still check if ports are actually active
        await loadPorts()
        if (hasActivePorts()) {
          tunnelStatus.value = 'ok'
          tunnelMessage.value = ''
          stopTunnelPoll()
        }
      }
    }, 5000)
  }

  /** Stop the tunnel health polling */
  function stopTunnelPoll() {
    if (tunnelPollTimer) {
      clearInterval(tunnelPollTimer)
      tunnelPollTimer = null
    }
  }

  /** Internal helper: actually open the port in sandbox or external browser */
  function doOpen(native: AndroidNativeBridge | undefined, localPort: number, protocol?: string, hostArg?: string, path?: string) {
    if (native?.openInSandbox) {
      native.openInSandbox(localPort, protocol === 'https' ? 'https' : 'http', hostArg || '', path || '')
    } else if (native?.openInBrowser) {
      native.openInBrowser(localPort, protocol === 'https' ? 'https' : 'http', hostArg || '', path || '')
    }
  }

  /** Open a forwarded port — in app mode opens sandbox browser, otherwise window.open.
   *  In app mode: tests if the port is reachable, waits briefly if not,
   *  then attempts SSH tunnel reconnect if still unreachable.
   *  Shows toast on success or failure after reconnection attempt.
   *  Uses reconnectTunnelAsync (non-blocking) to avoid ANR on Android. */
  async function openPort(localPort: number, protocol?: string, host?: string, path?: string) {
    appLog.d(TAG, 'openPort: localPort=' + localPort + ', protocol=' + protocol + ', host=' + (host || '') + ', path=' + (path || ''))

    if (isAppMode.value) {
      const native = getAndroidNative()
      const hostArg = host || ''

      if (native?.testPortReachable) {
        // Test if the local port is reachable
        const reachable = native.testPortReachable(localPort)
        appLog.d(TAG, 'openPort: testPortReachable(' + localPort + ') = ' + reachable)
        if (reachable) {
          doOpen(native, localPort, protocol, hostArg, path)
          return
        }

        // Port unreachable — attempt SSH tunnel reconnect.
        appLog.d(TAG, 'openPort: port ' + localPort + ' unreachable, attempting tunnel reconnect')
        const reconnected = await reconnectTunnelAsync(native)
        appLog.d(TAG, 'openPort: reconnectTunnelAsync() = ' + reconnected)

        const toast = useToast()
        if (reconnected) {
          const reachableAfter = native.testPortReachable(localPort)
          appLog.d(TAG, 'openPort: after reconnect, testPortReachable(' + localPort + ') = ' + reachableAfter)
          if (reachableAfter) {
            toast.show(gt('portForward.tunnelReconnected'), { type: 'success' })
            doOpen(native, localPort, protocol, hostArg, path)
            return
          }
        }

        // Still unreachable after reconnect — show error
        toast.show(gt('portForward.portUnreachable'), { type: 'error' })
        return
      }

      // Fallback: no testPortReachable available (old APK) — open directly
      doOpen(native, localPort, protocol, hostArg, path)
    } else {
      window.open(buildPortUrl(localPort, protocol, path), '_blank')
    }
  }

  /** Reconnect a specific forwarded port: test reachability, reconnect tunnel if needed.
   *  Used by the per-port reconnect button in the port forwarding panel.
   *  The caller tracks which ports are reconnecting and shows a spinning icon.
   *  Shows toast on success or failure.
   *  Uses reconnectTunnelAsync (non-blocking) to avoid ANR on Android. */
  async function reconnectPort(localPort: number) {
    const native = getAndroidNative()
    const toast = useToast()

    // Yield to let Vue render the spinning button before any bridge calls
    await new Promise(r => setTimeout(r, 50))

    if (isAppMode.value && native?.testPortReachable) {
      // Step 1: Test if the port is already reachable
      const reachable = native.testPortReachable(localPort)
      if (reachable) {
        toast.show(gt('portForward.tunnelReconnected'), { type: 'success' })
        await loadPorts(true)
        return
      }

      // Step 2: Port unreachable — reconnect tunnel (non-blocking)
      const reconnected = await reconnectTunnelAsync(native)

      if (reconnected) {
        const reachableAfter = native.testPortReachable(localPort)
        if (reachableAfter) {
          toast.show(gt('portForward.tunnelReconnected'), { type: 'success' })
        } else {
          toast.show(gt('portForward.portUnreachable'), { type: 'error' })
        }
      } else {
        toast.show(gt('portForward.portUnreachable'), { type: 'error' })
      }
    }

    // Refresh port list — spinning button stops when caller sees this resolve
    await loadPorts(true)
  }

  /** Open a forwarded port in external/system browser */
  function openInExternalBrowser(localPort: number, protocol?: string, host?: string) {
    if (isAppMode.value) {
      const native = getAndroidNative()
      if (native?.openInBrowser) {
        native.openInBrowser(localPort, protocol === 'https' ? 'https' : 'http', host || '', '')
      }
    } else {
      window.open(buildPortUrl(localPort, protocol), '_blank')
    }
  }

  /**
   * Ensure a port is registered for forwarding, registering it if needed,
   * and wait for it to appear in the ports list (max 5s).
   * Returns the localPort that was assigned (may differ from target port).
   * Used by localhost URL tag click handler to auto-setup port forwarding.
   */
  async function ensurePortRegistered(port: number, protocol: string): Promise<number> {
    const existing = ports.value.find(p => p.port === port)
    if (existing) return existing.localPort
    await registerPort(port, '', protocol)
    // Wait for port to appear in the list (max 5s, poll every 200ms)
    for (let i = 0; i < 25; i++) {
      await new Promise(r => setTimeout(r, 200))
      const found = ports.value.find(p => p.port === port)
      if (found) return found.localPort
    }
    throw new Error(`Port ${port} did not appear in forwarding list after 5s`)
  }

  return {
    ports,
    detectedPorts,
    loading,
    isAppMode,
    sshInfo,
    tunnelStatus,
    tunnelMessage,
    tunnelChecking,
    tunnelError,
    tunnelErrorType,
    connectingPorts,
    loadPorts,
    registerPort,
    updatePort,
    unregisterPort,
    detectPorts,
    syncToNative,
    loadSSHInfo,
    checkTunnelHealth,
    openPort,
    openInExternalBrowser,
    reconnectPort,
    ensurePortRegistered,
  }
}
