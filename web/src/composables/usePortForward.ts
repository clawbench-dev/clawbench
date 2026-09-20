import { ref, watch } from 'vue'
import { apiGet, apiPost, apiPut, apiDelete } from '@/utils/api'
import { useAppMode } from './useAppMode.ts'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast.ts'
import { tunnelStatusFromPorts as tunnelStatusFromPortsUtil, buildPortUrl } from '@/utils/portForwardUtils.ts'
import { store } from '@/stores/app'
import { useSessionIdentity } from './useSessionIdentity'
import { getNative, reconnectTunnel as nativeReconnectTunnel } from '@/utils/clawbenchNative'
import type { ClawBenchNative } from '@/utils/clawbenchNative'

interface ForwardedPort {
  port: number        // Target port on remote host
  localPort: number   // Local listening port (auto-assigned)
  host: string
  name: string
  protocol: string
  active: boolean
  enabled: boolean
}

interface DetectedPort {
  port: number
  protocol: string
  processName: string
  processArgs: string
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

// Whether the LOCAL listener for each localPort is actually accepting
// connections on this device. The server-side `active` flag probes the TARGET
// port on the server, so it stays green even when the local tunnel is dead —
// in app mode this map is the only honest source for the status dot.
// Empty in web mode (no native bridge to probe with), where `active` is used.
const localReachable = ref(new Map<number, boolean>())

// Port scan drawer state: whether a scan has ever completed (drives first-open auto-scan),
// the current scanning flag, and the open state of the scan drawer.
const scanDrawerOpen = ref(false)
const hasScanned = ref(false)
const scanning = ref(false)

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

/** Returns true if any enabled port has an active backend. */
function hasActivePorts(): boolean {
  return effectivePorts().some(p => p.enabled && p.active)
}

// The app-mode flag is a module-level singleton ref, so reading it here keeps
// the module-level helpers below (effectivePorts, tunnelStatusFromPorts) in
// sync with the value usePortForward() exposes.
const { isAppMode } = useAppMode()

/**
 * Ports with their `active` flag corrected for the local device.
 *
 * The server computes `active` by probing the target port ON THE SERVER, which
 * says nothing about whether this device's tunnel listener exists. In app mode
 * we therefore override it with the locally probed result: a port is only
 * "active" when the local listener answers AND the server sees the target up.
 * `undefined` (never probed) falls back to the server's value.
 *
 * Web mode is untouched — there is no local tunnel to probe, and `active` is
 * already the right answer there.
 */
function effectivePorts(): ForwardedPort[] {
  if (!isAppMode.value || localReachable.value.size === 0) return ports.value
  return ports.value.map(p => {
    const reachable = localReachable.value.get(p.localPort)
    if (reachable === undefined) return p
    return { ...p, active: reachable && p.active }
  })
}

// Sync enabled port count to global store for dock badge.
// Counts ENABLED ports (not just connected ones) so the badge stays visible
// even when the tunnel/backends are temporarily down.
watch(ports, () => {
  store.state.portForwardEnabledCount = ports.value.filter(p => p.enabled).length
}, { deep: true })

/**
 * Determines tunnel status from port state (delegates to pure utility).
 */
function tunnelStatusFromPorts(_hasPorts: boolean): 'ok' | 'degraded' {
  return tunnelStatusFromPortsUtil(effectivePorts())
}

/**
 * Manages port forwarding state: list of forwarded ports, CRUD operations,
 * auto-detection, and registration with Android native layer.
 */
export function usePortForward() {
  const { currentSessionId } = useSessionIdentity()

  // Guards against overlapping probe rounds (see refreshLocalReachability).
  let probingReachability = false

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
        toast.show(gt('portForward.portUnreachable'), { icon: '🚫', type: 'error' })
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
      // Refresh local reachability so the status dots reflect THIS device's
      // tunnel rather than only the server's view of the target port.
      await refreshLocalReachability()
    } finally {
      if (!silent) loading.value = false
    }
  }

  /**
   * Probe each enabled port's LOCAL listener on this device and record the
   * result in `localReachable`.
   *
   * Sequential on purpose: on Android each probe crosses the JS bridge and can
   * take up to 500ms, so firing them all at once would stall the bridge. The
   * whole map is replaced each round, which also prunes ports that disappeared.
   * Web mode clears the map — there is no local tunnel to probe, and `active`
   * already reflects the right thing there.
   */
  async function refreshLocalReachability() {
    const native = getNative()
    if (!isAppMode.value || typeof native?.testPortReachable !== 'function') {
      if (localReachable.value.size > 0) localReachable.value = new Map()
      return
    }
    // Probes are sequential and can take 500ms each, while loadPorts() is
    // called from a 5s poll. Without this guard the rounds overlap and a stale
    // round can overwrite a newer one's results.
    if (probingReachability) return
    probingReachability = true
    try {
      const next = new Map<number, boolean>()
      for (const p of ports.value) {
        if (!p.enabled) continue
        try {
          next.set(p.localPort, (await native.testPortReachable(p.localPort)) === true)
        } catch {
          next.set(p.localPort, false)
        }
      }
      localReachable.value = next
    } finally {
      probingReachability = false
    }
  }

  /**
   * Register a forward with the native layer and surface a hard failure.
   *
   * The bridge is heterogeneous: Android returns undefined (synchronous void),
   * Electron returns Promise<boolean>. Only an explicit `false` means "the
   * listener could not be bound" — treating undefined as failure would pop a
   * false error on every Android call, and swallowing false (the old behaviour)
   * left a dead mapping looking healthy.
   */
  function addNativeForward(localPort: number, targetPort: number, host: string): void {
    const native = getNative()
    if (!native?.addForwardedPort) return
    Promise.resolve(native.addForwardedPort(localPort, targetPort, host || ''))
      .then(ok => { if (ok === false) reportForwardFailure(localPort) })
      .catch(() => reportForwardFailure(localPort))
  }

  /** Clear the pending indicator and tell the user the port could not be bound. */
  function reportForwardFailure(localPort: number) {
    if (connectingPorts.value.delete(localPort)) {
      connectingPorts.value = new Set(connectingPorts.value)
    }
    const toast = useToast()
    toast.show(gt('portForward.portUnreachable'), { icon: '🚫', type: 'error' })
  }

  async function registerPort(port: number, name?: string, protocol?: string, host?: string): Promise<number> {
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
      addNativeForward(localPort, port, host || '')
    }
    // Fire-and-forget: refresh port list and SSH info in the background.
    // Do NOT await — the caller needs localPort immediately to open the WebView.
    loadPorts(true).catch(() => {})
    loadSSHInfo().catch(() => {})
    return localPort
  }

  async function updatePort(localPort: number, port: number, host: string, name: string, protocol: string) {
    await apiPut('/api/proxy/ports', { localPort, port, host, name, protocol })
    // Re-sync native layer after update: remove old, add new with correct localPort
    if (isAppMode.value) {
      Promise.resolve(getNative()?.removeForwardedPort?.(localPort)).catch(() => {})
      addNativeForward(localPort, port, host || '')
    }
    await Promise.all([loadPorts(true), loadSSHInfo()])
  }

  async function unregisterPort(localPort: number) {
    await apiDelete(`/api/proxy/ports?port=${localPort}`)
    if (isAppMode.value) {
      Promise.resolve(getNative()?.removeForwardedPort?.(localPort)).catch(() => {})
    }
    await Promise.all([loadPorts(true), loadSSHInfo()])
  }

  async function detectPorts() {
    scanning.value = true
    try {
      const data = await apiGet<{ ports: DetectedPort[] }>('/api/proxy/detect')
      detectedPorts.value = data.ports || []
      hasScanned.value = true
    } finally {
      scanning.value = false
    }
  }

  /** Enable or disable a forwarded port on the backend, then refresh the list.
   *  In app mode, also sync the native SSH tunnel so disabling actually stops
   *  forwarding (otherwise the native layer keeps counting it in the notification). */
  async function setPortEnabled(localPort: number, enabled: boolean) {
    await apiPut('/api/proxy/ports/enabled', { localPort, enabled })
    await loadPorts(true)
    if (isAppMode.value) {
      const p = ports.value.find(x => x.localPort === localPort)
      const native = getNative()
      if (enabled && p) {
        addNativeForward(p.localPort, p.port, p.host || '')
      } else if (!enabled) {
        Promise.resolve(native?.removeForwardedPort?.(localPort)).catch(() => {})
      }
    }
  }

  /** Open the scan drawer, auto-running a scan the first time it is opened. */
  async function openScanDrawer() {
    scanDrawerOpen.value = true
    if (!hasScanned.value && !scanning.value) {
      await detectPorts()
    }
  }

  /** Close the scan drawer. */
  function closeScanDrawer() {
    scanDrawerOpen.value = false
  }

  /** Re-run a scan from within the drawer. */
  async function rescanPorts() {
    await detectPorts()
  }

  async function syncToNative() {
    if (!isAppMode.value) return
    await loadPorts()
    const native = getNative()
    if (!native) return

    const enabledPorts = ports.value.filter(p => p.enabled)
    if (enabledPorts.length === 0) {
      // No enabled ports on server — stop the native service (avoids idle foreground
      // service draining battery on Android; no-op on desktop).
      native.stopBackgroundService?.()
      return
    }

    const enabledLocalPorts = new Set(enabledPorts.map(p => p.localPort))

    if (typeof native.getForwardedPorts === 'function') {
      try {
        const current: Array<{ port?: number; host?: string }> = JSON.parse((await native.getForwardedPorts()) || '[]')
        for (const item of current) {
          const lp = item && item.port
          if (lp && !enabledLocalPorts.has(lp)) {
            Promise.resolve(native.removeForwardedPort?.(lp)).catch(() => {})
          }
        }
      } catch {
        // Ignore parse errors — reconciliation is best-effort.
      }
    }

    for (const p of enabledPorts) {
      // Sequential: the native layer shares one SSH tunnel, and concurrent
      // connects used to cancel each other (each add cancelled the in-flight
      // one), leaving every mapping dead. Awaiting keeps them strictly ordered.
      const ok = await Promise.resolve(native.addForwardedPort?.(p.localPort, p.port, p.host || '')).catch(() => false)
      if (ok === false) reportForwardFailure(p.localPort)
    }
    // Re-probe now that the forwards have been (re)established, so the dots
    // reflect reality instead of the pre-sync state.
    await refreshLocalReachability()
  }

  /**
   * Fetch SSH tunnel connection info from server.
   *
   * Uses the authenticated endpoint: the public `/api/ssh/info` now returns only
   * {enabled, port} for the Android pre-login port probe. The web UI needs the
   * command, fingerprint and connection stats, so it must use the full one.
   */
  async function loadSSHInfo() {
    try {
      const data = await apiGet<SSHInfo>('/api/ssh/info/full')
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
      const nativeConnected = await getNativeTunnelStatus()
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
        tunnelError.value = await getNativeTunnelError()
        tunnelErrorType.value = await getNativeTunnelErrorType()
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
  async function getNativeTunnelStatus(): Promise<boolean | null> {
    if (!isAppMode.value) return null
    const native = getNative()
    if (!native || typeof native.isTunnelConnected !== 'function') return null
    try {
      const result = await native.isTunnelConnected()
      return typeof result === 'boolean' ? result : null
    } catch {
      return null
    }
  }

  /**
   * Query Android native layer for the last SSH tunnel error.
   * Returns the error message string, or empty string if no error.
   */
  async function getNativeTunnelError(): Promise<string> {
    if (!isAppMode.value) return ''
    const native = getNative()
    if (!native || typeof native.getTunnelError !== 'function') return ''
    try {
      const result = await native.getTunnelError()
      return typeof result === 'string' ? result : ''
    } catch {
      return ''
    }
  }

  /**
   * Query Android native layer for the last SSH tunnel error type.
   * Returns one of: 'auth', 'network', 'hostkey', 'unknown', or ''.
   */
  async function getNativeTunnelErrorType(): Promise<TunnelErrorType> {
    if (!isAppMode.value) return ''
    const native = getNative()
    if (!native || typeof native.getTunnelErrorType !== 'function') return ''
    try {
      const result = await native.getTunnelErrorType()
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
      const nativeConnected = await getNativeTunnelStatus()
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
  function doOpen(native: ClawBenchNative | undefined, localPort: number, protocol?: string, hostArg?: string, path?: string) {
    if (native?.openInSandbox) {
      Promise.resolve(native.openInSandbox(localPort, protocol === 'https' ? 'https' : 'http', hostArg || '', path || '', currentSessionId.value || '')).catch(() => {})
    } else if (native?.openInBrowser) {
      Promise.resolve(native.openInBrowser(localPort, protocol === 'https' ? 'https' : 'http', hostArg || '', path || '')).catch(() => {})
    }
  }

  /** Open a forwarded port — in app mode opens sandbox browser, otherwise window.open.
   *  ALWAYS opens WebView immediately in app mode. BrowserActivity's tunnel-wait
   *  mechanism (30s polling at 500ms intervals) handles waiting for the SSH tunnel
   *  to become ready before loading the page.
   *  Used by localhost URL click handler where the user expects immediate WebView. */
  function openPort(localPort: number, protocol?: string, host?: string, path?: string) {
    if (isAppMode.value) {
      const native = getNative()
      doOpen(native, localPort, protocol, host || '', path)
    } else {
      window.open(buildPortUrl(localPort, protocol, path), '_blank')
    }
  }

  /** Open a forwarded port with reachability check and tunnel reconnect.
   *  Used by the port forwarding panel where the user expects feedback about
   *  whether the port is actually reachable before opening the WebView.
   *
   *  Flow:
   *  1. If port is reachable → open immediately
   *  2. If port is in connecting state → open directly (WebView will wait)
   *  3. If port is unreachable → attempt tunnel reconnect, then open or show error
   */
  async function openPortWithCheck(localPort: number, protocol?: string, host?: string, path?: string) {
    if (!isAppMode.value) {
      window.open(buildPortUrl(localPort, protocol, path), '_blank')
      return
    }

    const native = getNative()
    const hostArg = host || ''

    if (native?.testPortReachable) {
      // Port is in connecting state — open directly, BrowserActivity will wait
      if (connectingPorts.value.has(localPort)) {
        doOpen(native, localPort, protocol, hostArg, path)
        return
      }

      // Port is reachable — open immediately
      if (await native.testPortReachable(localPort)) {
        doOpen(native, localPort, protocol, hostArg, path)
        return
      }

      // Port unreachable — attempt tunnel reconnect
      const reconnected = await nativeReconnectTunnel()
      const toast = useToast()
      if (reconnected && (await native.testPortReachable(localPort))) {
        toast.show(gt('portForward.tunnelReconnected'), { icon: '🔗', type: 'success' })
        doOpen(native, localPort, protocol, hostArg, path)
        return
      }

      toast.show(gt('portForward.portUnreachable'), { icon: '🚫', type: 'error' })
      return
    }

    // No testPortReachable (old APK) — open directly
    doOpen(native, localPort, protocol, hostArg, path)
  }

  /** Reconnect a specific forwarded port: test reachability, reconnect tunnel if needed.
   *  Used by the per-port reconnect button in the port forwarding panel.
   *  The caller tracks which ports are reconnecting and shows a spinning icon.
   *  Shows toast on success or failure.
   *  Uses reconnectTunnelAsync (non-blocking) to avoid ANR on Android. */
  async function reconnectPort(localPort: number) {
    const native = getNative()
    const toast = useToast()

    // Yield to let Vue render the spinning button before any bridge calls
    await new Promise(r => setTimeout(r, 50))

    if (isAppMode.value && native?.testPortReachable) {
      // Step 1: Test if the port is already reachable
      const reachable = await native.testPortReachable(localPort)
      if (reachable) {
        toast.show(gt('portForward.tunnelReconnected'), { icon: '🔗', type: 'success' })
        await loadPorts(true)
        return
      }

      // Step 2: Port unreachable — reconnect tunnel (non-blocking)
      const reconnected = await nativeReconnectTunnel()

      if (reconnected) {
        const reachableAfter = await native.testPortReachable(localPort)
        if (reachableAfter) {
          toast.show(gt('portForward.tunnelReconnected'), { icon: '🔗', type: 'success' })
        } else {
          toast.show(gt('portForward.portUnreachable'), { icon: '🚫', type: 'error' })
        }
      } else {
        toast.show(gt('portForward.portUnreachable'), { icon: '🚫', type: 'error' })
      }
    }

    // Refresh port list — spinning button stops when caller sees this resolve
    await loadPorts(true)
  }

  /** Open a forwarded port in external/system browser */
  function openInExternalBrowser(localPort: number, protocol?: string, host?: string) {
    if (isAppMode.value) {
      const native = getNative()
      if (native?.openInBrowser) {
        Promise.resolve(native.openInBrowser(localPort, protocol === 'https' ? 'https' : 'http', host || '', '')).catch(() => {})
      }
    } else {
      window.open(buildPortUrl(localPort, protocol), '_blank')
    }
  }

  /**
   * Ensure a port is registered for forwarding, registering it if needed.
   * Returns the localPort that was assigned (may differ from target port).
   * Idempotent: if already registered with the same (port, host), returns existing localPort.
   * If the existing port is currently disabled, it is re-enabled so the caller
   * (e.g. the localhost URL click handler) can actually open it.
   * Used by localhost URL click handler to auto-setup port forwarding.
   */
  async function ensurePortRegistered(port: number, protocol: string, host?: string): Promise<number> {
    const existing = ports.value.find(p => p.port === port && p.host === (host || ''))
    if (existing) {
      if (!existing.enabled) {
        await setPortEnabled(existing.localPort, true)
      }
      return existing.localPort
    }
    return registerPort(port, '', protocol, host)
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
    localReachable,
    scanDrawerOpen,
    hasScanned,
    scanning,
    loadPorts,
    registerPort,
    updatePort,
    unregisterPort,
    setPortEnabled,
    detectPorts,
    openScanDrawer,
    closeScanDrawer,
    rescanPorts,
    syncToNative,
    loadSSHInfo,
    checkTunnelHealth,
    openPort,
    openPortWithCheck,
    openInExternalBrowser,
    reconnectPort,
    ensurePortRegistered,
  }
}
