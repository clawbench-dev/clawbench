import { ref, computed, watch } from 'vue'
import { apiGet, apiPost, apiPut, apiDelete } from '@/utils/api'
import { useAppMode } from './useAppMode.ts'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast.ts'
import { tunnelStatusFromPorts as tunnelStatusFromPortsUtil, buildPortUrl, isReversePort } from '@/utils/portForwardUtils.ts'
import type { PortDirection } from '@/utils/portForwardUtils.ts'
import { transportLabelKey } from '@/utils/tunnelTransport.ts'
import { store } from '@/stores/app'
import { useSessionIdentity } from './useSessionIdentity'
import { useSettingsConfig } from './useSettingsConfig'
import { getNative, reconnectTunnel as nativeReconnectTunnel } from '@/utils/clawbenchNative'
import type { ClawBenchNative } from '@/utils/clawbenchNative'
import { appLog } from '@/utils/appLog'

const TAG = 'PortForward'

interface ForwardedPort {
  port: number        // Target port (server-side for forward, client-side for reverse)
  localPort: number   // Listening port (client-side for forward, server-side for reverse)
  host: string
  name: string
  protocol: string
  direction?: PortDirection
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

/**
 * Tunnel transport: the concrete wire that carried the last connect, or `''`
 * when unknown (web mode, or a host that predates the bridge methods).
 *
 * There is no `'both'`: the automatic mode was removed, and no shipped host
 * reports it — Electron's `getActiveTransport()` is typed `'ssh' | 'h2'` and
 * migrates a stored `'both'` to `'h2'` on load, and Android derives the family
 * from the live session. (The server config still pins `port_forward.transport`
 * to `both`; that is a capability flag, not this value — see
 * `tunnelTransportAllowsH2`.)
 */
export type TunnelTransport = 'ssh' | 'h2' | ''

const TRANSPORTS: readonly string[] = ['ssh', 'h2']

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

// The transport that actually carried the tunnel ('ssh' | 'h2'), or '' when it
// cannot be determined (web mode, or a host that predates the bridge methods).
// Read from the native layer rather than the server config because only the
// native tunnel knows which wire it ended up on.
const activeTransport = ref<TunnelTransport>('')

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
// Non-empty when the last scan attempt failed. Kept separate from `hasScanned`
// so the UI can tell "the scan failed" apart from "the scan ran and found
// nothing" — both used to render the same empty state, which silently hid
// request failures (a 10s timeout on a busy host was indistinguishable from a
// genuinely empty port table).
const scanError = ref('')

// Port scans probe every listening port with a TLS handshake, so a host with
// 100+ ports can take far longer than the 10s default API timeout. The backend
// probes in parallel and bounds each handshake, but the ceiling still needs
// headroom for very busy hosts.
const SCAN_TIMEOUT_MS = 60_000

// Auto-refresh interval when tunnel is unhealthy
let tunnelPollTimer: ReturnType<typeof setInterval> | null = null

// Callback set by usePortForward() to handle port-forward-result events.
// We need this indirection because loadPorts() is defined inside usePortForward(),
// but the event listener is set up at module level.
//
// `reason` is optional (older hosts omit it; an absent value keeps the original
// "unreachable" copy). `requestedLocalPort` is the port the caller asked for,
// which differs from `localPort` when the listener had to bind a free neighbour
// — the callback re-keys the server registry in that case.
let onPortForwardResult: ((
  localPort: number,
  success: boolean,
  reason?: 'conflict' | 'unreachable',
  requestedLocalPort?: number,
) => void) | null = null

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
      const { localPort, success, reason, requestedLocalPort } = e.detail
      onPortForwardResult(localPort, success, reason, requestedLocalPort)
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

// Single-flight state for the reachability probe. Module-scoped on purpose: it
// protects the module-level `localReachable`, while usePortForward() is called
// from several places (the panel, App.vue's syncToNative, the localhost
// annotation handler). A closure-local flag would let two instances probe
// concurrently — each probe is a synchronous JS-bridge call that can take 500ms
// on Android, so overlapping rounds double the bridge load.
//
// A round requested while one is in flight is QUEUED, not dropped. Dropping it
// is what left a freshly created mapping red until a manual refresh: the first
// round runs from registerPort's fire-and-forget loadPorts BEFORE Android's
// async bind completes, records `false`, and the native success event's
// re-probe was discarded — so the stale `false` was what the dot rendered.
let probingReachability = false
let reprobeRequested = false

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
    // A reverse mapping has no client-side listener to probe: its `active` flag
    // comes from the server-side bind, which is already the honest answer.
    if (isReversePort(p)) return p
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
  // Reads `port_forward.transport` from `/api/config` for the web health gate
  // (`tunnelTransportAllowsH2()`): the server config is the only place that
  // says whether an h2 path exists when the SSH listener is off. The native
  // clients no longer consume this value — Android decides via its local
  // switch and Electron is pinned to SSH — so nothing here is pushed to the
  // bridge any more.
  const { getServerValue } = useSettingsConfig()

  // Set up the callback for native port-forward-result events.
  // This needs to be inside usePortForward() because it calls loadPorts()
  // which is defined here. The module-level event listener dispatches to this callback.
  if (!onPortForwardResult) {
    onPortForwardResult = (
      localPort: number,
      success: boolean,
      reason?: 'conflict' | 'unreachable',
      requestedLocalPort?: number,
    ) => {
      const requested = requestedLocalPort ?? localPort
      // The listener may have moved to a free port when the requested one was
      // occupied. Re-key the server so DELETE/PUT/enable and the UI URL follow
      // it, exactly like the desktop path does from the bind result.
      if (success && localPort !== requested) {
        void (async () => {
          if (await rebindServerPort(requested, localPort)) {
            connectingPorts.value.delete(requested)
            connectingPorts.value.add(localPort)
            connectingPorts.value = new Set(connectingPorts.value)
            const toast = useToast()
            toast.show(gt('portForward.portRebound', { requested, port: localPort }), { icon: '🔀', type: 'info' })
          } else {
            // The server refused the new key; release the orphaned listener.
            removeNativeForward(localPort)
            reportForwardFailure(requested, 'conflict')
          }
          loadPorts(true)
        })()
        return
      }
      connectingPorts.value.delete(localPort)
      connectingPorts.value = new Set(connectingPorts.value)
      // Refresh port list to pick up the new active state from backend
      loadPorts(true)
      if (!success) {
        reportForwardFailure(localPort, reason)
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
    // Single-flight with a queued re-run. Probes are sequential and can take
    // 500ms each, while loadPorts() is called from a 5s poll; running rounds
    // concurrently would double the bridge load, so an overlapping request is
    // remembered and honoured as soon as the current round finishes. Dropping
    // it instead (the old behaviour) let a stale pre-bind result outlive the
    // success event's re-probe and stick as a red dot.
    if (probingReachability) {
      reprobeRequested = true
      return
    }
    probingReachability = true
    try {
      do {
        // Clear before the round: a request arriving during this round must
        // schedule the NEXT one, not be swallowed by a stale flag.
        reprobeRequested = false
        const next = new Map<number, boolean>()
        for (const p of ports.value) {
          // Reverse mappings have no local listener — probing would always fail
          // and paint every reverse entry as tunnel-down.
          if (!p.enabled || isReversePort(p)) continue
          try {
            next.set(p.localPort, (await native.testPortReachable(p.localPort)) === true)
          } catch {
            next.set(p.localPort, false)
          }
        }
        localReachable.value = next
      } while (reprobeRequested)
    } finally {
      probingReachability = false
      reprobeRequested = false
    }
  }

  /**
   * Register a forward with the native layer and surface a hard failure.
   *
   * The bridge is heterogeneous and so are its success signals:
   *   - Current Electron: `AddForwardResult` — an explicit ok/reason, and the
   *     port actually bound (which may differ from `localPort` when it was
   *     already taken on this machine).
   *   - Legacy Electron: a bare boolean.
   *   - Android: `undefined` (a synchronous @JavascriptInterface void). Its
   *     verdict arrives later through the `clawbench-port-forward-result`
   *     CustomEvent, so an absent value is NOT success and NOT failure — the
   *     pending indicator must stay until the event lands.
   *
   * Only an explicit `false` (or `{ok:false}`) means failure: treating undefined
   * as failure would pop a false error on every Android call, and swallowing
   * false (the old behaviour) left a dead mapping looking healthy.
   *
   * Resolves to the port the listener actually bound (== `localPort` when it
   * did not move, or when the verdict is still pending), or null on failure.
   */
  async function addNativeForward(localPort: number, targetPort: number, host: string, direction?: PortDirection): Promise<number | null> {
    const native = getNative()
    const isReverse = direction === 'reverse'
    const method = isReverse ? native?.addReverseForwardedPort : native?.addForwardedPort
    if (!method) return localPort
    // For a reverse mapping the roles flip: `localPort` is the server-side bind
    // port and `targetPort` the local service to relay to.
    let raw: unknown
    try {
      raw = await Promise.resolve(method.call(native, localPort, targetPort, host || ''))
    } catch {
      reportForwardFailure(localPort)
      return null
    }
    const outcome = classifyForwardResult(raw, localPort)

    if (outcome.kind === 'failure') {
      reportForwardFailure(localPort, outcome.reason)
      return null
    }
    if (outcome.kind === 'pending') {
      // Android: the CustomEvent will clear the indicator and re-key if needed.
      return localPort
    }
    if (outcome.port !== localPort) {
      // The listener moved to a free port; re-key the server so DELETE/PUT/
      // enable and the UI URL follow it.
      const rebound = await rebindServerPort(localPort, outcome.port)
      if (rebound) {
        connectingPorts.value.delete(localPort)
        connectingPorts.value.add(outcome.port)
        connectingPorts.value = new Set(connectingPorts.value)
        loadPorts(true)
        return outcome.port
      }
      // The server refused (another mapping took the port between our bind and
      // this call). Release the listener we just bound and report the conflict —
      // never leave a listener the registry does not know about.
      removeNativeForward(outcome.port, direction)
      reportForwardFailure(localPort, 'conflict')
      return null
    }
    // Bound on the requested port: mirror onPortForwardResult.
    if (connectingPorts.value.delete(localPort)) {
      connectingPorts.value = new Set(connectingPorts.value)
    }
    loadPorts(true)
    return localPort
  }

  type ForwardOutcome =
    | { kind: 'success'; port: number }
    | { kind: 'pending' }
    | { kind: 'failure'; reason: 'conflict' | 'unreachable' }

  /**
   * Classify the native bind result.
   *
   * `undefined`/`null` is PENDING, not success: Android's method is a
   * synchronous void whose verdict arrives via the CustomEvent, so clearing the
   * pending indicator here would flash "connected" before the bind is known.
   */
  function classifyForwardResult(raw: unknown, requested: number): ForwardOutcome {
    if (raw === false) return { kind: 'failure', reason: 'unreachable' }
    if (raw === true) return { kind: 'success', port: requested }
    if (raw == null) return { kind: 'pending' }
    const r = raw as { ok?: boolean; port?: unknown; reason?: unknown }
    if (r.ok === false) {
      return { kind: 'failure', reason: r.reason === 'conflict' ? 'conflict' : 'unreachable' }
    }
    if (typeof r.port === 'number' && r.port > 0) return { kind: 'success', port: r.port }
    return { kind: 'success', port: requested }
  }

  /**
   * Move the server's registry key from `oldPort` to `newPort`.
   *
   * Returns false when the server refused (409 = another mapping took the port,
   * 404 = the mapping was deleted concurrently). The caller then releases the
   * listener rather than leaving it orphaned.
   */
  async function rebindServerPort(oldPort: number, newPort: number): Promise<boolean> {
    try {
      await apiPost('/api/proxy/ports/rebind', { localPort: oldPort, newLocalPort: newPort })
      return true
    } catch (e) {
      appLog.w(TAG, `rebind ${oldPort} -> ${newPort} failed`, e)
      return false
    }
  }

  /** Tear down a native forward of either direction. */
  function removeNativeForward(localPort: number, direction?: PortDirection): void {
    const native = getNative()
    const method = direction === 'reverse' ? native?.removeReverseForwardedPort : native?.removeForwardedPort
    if (!method) return
    Promise.resolve(method.call(native, localPort)).catch(() => {})
  }

  /**
   * Clear the pending indicator and tell the user the forward could not be set
   * up. `reason === 'conflict'` means the LOCAL port was occupied (retry on
   * another port); anything else is the tunnel/target being unreachable — which
   * is what the original copy was written for and must not be shown for a
   * client-side conflict.
   */
  function reportForwardFailure(localPort: number, reason?: 'conflict' | 'unreachable') {
    if (connectingPorts.value.delete(localPort)) {
      connectingPorts.value = new Set(connectingPorts.value)
    }
    const toast = useToast()
    if (reason === 'conflict') {
      toast.show(gt('portForward.portConflict'), { icon: '🚫', type: 'error' })
      return
    }
    toast.show(gt('portForward.portUnreachable'), { icon: '🚫', type: 'error' })
  }

  async function registerPort(port: number, name?: string, protocol?: string, host?: string, direction?: PortDirection): Promise<number> {
    const result = await apiPost<{ localPort: number }>('/api/proxy/ports', { port, host: host || '', name: name || '', protocol: protocol || 'http', direction: direction || 'forward' })
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
    // Register with the native layer: pass localPort, targetPort, host.
    // Await it so we can return the port the listener ACTUALLY bound: when the
    // requested port is taken on this machine the desktop shell binds the next
    // free one and re-keys the server, and the caller (a localhost-URL click)
    // must open THAT port. Android reports through the CustomEvent instead, so
    // it returns the requested port here and self-corrects on the next load.
    let actualPort = localPort
    if (isAppMode.value) {
      const bound = await addNativeForward(localPort, port, host || '', direction)
      if (bound !== null) actualPort = bound
    }
    // Fire-and-forget: refresh port list and SSH info in the background.
    loadPorts(true).catch(() => {})
    loadSSHInfo().catch(() => {})
    return actualPort
  }

  async function updatePort(localPort: number, port: number, host: string, name: string, protocol: string, direction?: PortDirection) {
    await apiPut('/api/proxy/ports', { localPort, port, host, name, protocol, direction: direction || 'forward' })
    // Re-sync native layer after update: remove old, add new with correct localPort.
    // Fire-and-forget: the caller awaits the server write, not the local bind.
    if (isAppMode.value) {
      removeNativeForward(localPort, direction)
      void addNativeForward(localPort, port, host || '', direction)
    }
    await Promise.all([loadPorts(true), loadSSHInfo()])
  }

  async function unregisterPort(localPort: number) {
    // Look up the direction BEFORE deleting, so the native teardown targets the
    // right mapping (forward and reverse have separate native maps).
    const existing = ports.value.find(p => p.localPort === localPort)
    await apiDelete(`/api/proxy/ports?port=${localPort}`)
    if (isAppMode.value) {
      removeNativeForward(localPort, existing?.direction)
    }
    await Promise.all([loadPorts(true), loadSSHInfo()])
  }

  async function detectPorts() {
    scanning.value = true
    scanError.value = ''
    try {
      const data = await apiGet<{ ports: DetectedPort[] }>('/api/proxy/detect', { timeoutMs: SCAN_TIMEOUT_MS })
      detectedPorts.value = data.ports || []
      hasScanned.value = true
    } catch (e) {
      // Swallowed on purpose. Callers invoke this unawaited (handleOpenScan) or
      // from a @click handler (rescanPorts), so rethrowing would surface as an
      // unhandledrejection that main.ts only logs. The failure is reported
      // through scanError instead.
      //
      // hasScanned deliberately stays false: it gates the first-open auto-scan,
      // so setting it here would disable retries after a single failure.
      detectedPorts.value = []
      scanError.value = e instanceof Error ? e.message : String(e)
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
      if (enabled && p) {
        void addNativeForward(p.localPort, p.port, p.host || '', p.direction)
      } else if (!enabled) {
        removeNativeForward(localPort, p?.direction)
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
        const current: Array<{ port?: number; host?: string; direction?: string }> = JSON.parse((await native.getForwardedPorts()) || '[]')
        for (const item of current) {
          const lp = item && item.port
          if (lp && !enabledLocalPorts.has(lp)) {
            removeNativeForward(lp, item.direction as PortDirection | undefined)
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
      const method = isReversePort(p) ? native.addReverseForwardedPort : native.addForwardedPort
      if (!method) {
        // Host predates reverse forwarding — nothing to bind natively.
        if (isReversePort(p)) continue
        reportForwardFailure(p.localPort)
        continue
      }
      // Route through the shared helper so a reconciliation that lands on a
      // different free port re-keys the server exactly like a fresh add does.
      await addNativeForward(p.localPort, p.port, p.host || '', p.direction)
    }
    // Re-probe now that the forwards have been (re)established, so the dots
    // reflect reality instead of the pre-sync state.
    await refreshLocalReachability()
    // Enabled ports exist (we returned early otherwise), so run a health check:
    // if the tunnel is down it arms the 5s poll, which asks the native layer to
    // reconnect. Secondary to the shell's own monitor, but it keeps recovery
    // working even if that monitor is not armed.
    await checkTunnelHealth()
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

  /**
   * Whether the configured transport can carry traffic over h2, i.e. the tunnel
   * does NOT depend on the SSH listener.
   *
   * Only 'h2' and 'both' qualify. Anything else — including a missing value
   * before `/api/config` resolves, and a value from a newer build this client
   * cannot interpret — is treated as ssh-only: assuming h2 would probe a
   * transport the operator never enabled. The gate is an OR with SSH, so a
   * conservative false only skips the check when SSH is off too.
   */
  function tunnelTransportAllowsH2(): boolean {
    let raw: unknown
    try {
      raw = getServerValue('port_forward.transport')
    } catch {
      return false
    }
    return raw === 'h2' || raw === 'both'
  }

  /**
   * Refresh `activeTransport` from the native tunnel.
   *
   * Prefers `getActiveTunnelTransport()` (the wire the session actually came up
   * on) and falls back to the configured preference when nothing has connected
   * yet. Both bridge methods are optional, so an older host simply leaves the
   * value unknown — never throws, never invents a transport.
   */
  async function refreshActiveTransport(): Promise<void> {
    if (!isAppMode.value) {
      activeTransport.value = ''
      return
    }
    const native = getNative()
    if (!native) {
      activeTransport.value = ''
      return
    }
    // Bound with .call(native) like the other bridge reads above: Android's
    // @JavascriptInterface methods are host objects, and extracting them
    // unbound would drop the receiver.
    const read = async (method?: unknown): Promise<TunnelTransport> => {
      if (typeof method !== 'function') return ''
      try {
        const value = await (method as () => unknown).call(native)
        return typeof value === 'string' && TRANSPORTS.includes(value)
          ? (value as TunnelTransport)
          : ''
      } catch {
        return ''
      }
    }
    const active = await read(native.getActiveTunnelTransport)
    activeTransport.value = active || await read(native.getTunnelTransport)
  }

  /**
   * Parenthesized transport annotation for status copy, or `''` when no single
   * wire is known.
   *
   * Only a concrete `'ssh'` / `'h2'` yields a label; `''` (unknown) stays empty
   * so the caller renders the neutral wording rather than guessing. The bracket
   * style lives in the `proxy.transportAnnotation` message so zh gets full-width
   * brackets and en half-width ones without a locale branch here.
   */
  function transportAnnotation(): string {
    const key = transportLabelKey(activeTransport.value)
    return key ? gt('proxy.transportAnnotation', { transport: gt(key) }) : ''
  }

  /**
   * Reactive view of `tunnelTransportAllowsH2()` for the UI.
   *
   * Reads through `getServerValue`, whose `serverConfig` ref is a real
   * dependency, so this recomputes when `/api/config` resolves — the same
   * reason the health gate can call the raw function synchronously inside an
   * async check. Used by the panel's "port forwarding unavailable" banner:
   * an h2-capable install forwards ports even with no SSH listener, so the
   * banner must not key off SSH alone.
   */
  const transportAllowsH2 = computed(() => tunnelTransportAllowsH2())

  /** Check SSH tunnel health and determine status */
  async function checkTunnelHealth() {
    tunnelChecking.value = true
    tunnelStatus.value = 'unknown'
    tunnelMessage.value = ''
    tunnelError.value = ''
    tunnelErrorType.value = ''

    await Promise.all([loadPorts(), loadSSHInfo(), refreshActiveTransport()])

    const info = sshInfo.value
    // SSH not configured does NOT mean the tunnel is unavailable: an h2-only
    // install carries the same forwards over the main HTTP server without any
    // SSH listener. Skip only when neither wire can be used — otherwise the
    // whole health check (and with it the status banner and 5s recovery poll)
    // would be silently skipped on h2 installs.
    if (!info?.enabled && !tunnelTransportAllowsH2()) {
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
          tunnelMessage.value = gt('portForward.tunnelDegraded', { transport: transportAnnotation() })
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
        tunnelMessage.value = gt('portForward.tunnelDisconnected', { transport: transportAnnotation() })
        tunnelChecking.value = false
        startTunnelPoll()
        return
      }
    }

    // Native status unavailable — fall back to server-side connection stats.
    // `info` can be null here (the SSH info fetch failed) while h2 still lets
    // the check proceed: those stats are SSH-only, so their absence is the
    // same as "no stats" below, not a reason to skip the h2 path above.
    const stats = info?.connectionStats
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
      tunnelMessage.value = gt('portForward.tunnelDisconnected', { transport: transportAnnotation() })
      tunnelChecking.value = false
      startTunnelPoll()
      return
    }

    // SSH is connected — check if any ports have active backends
    const hasPorts = ports.value.length > 0
    if (tunnelStatusFromPorts(hasPorts) === 'degraded') {
      tunnelStatus.value = 'degraded'
      tunnelMessage.value = gt('portForward.tunnelDegraded', { transport: transportAnnotation() })
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
          tunnelMessage.value = gt('portForward.tunnelDegraded', { transport: transportAnnotation() })
        }
        return
      }

      // Native reports the tunnel down while ports are still wanted: ask it to
      // reconnect. The desktop shell self-heals via its own monitor, but doing
      // it here too means recovery does not depend on that monitor being armed.
      // Only for an explicit `false` — `null` means "no native status", where a
      // reconnect call would be meaningless.
      if (nativeConnected === false && ports.value.some(p => p.enabled)) {
        await nativeReconnectTunnel()
        await loadPorts()
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
          tunnelMessage.value = gt('portForward.tunnelDegraded', { transport: transportAnnotation() })
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
        toast.show(gt('portForward.tunnelReconnected', { transport: transportAnnotation() }), { icon: '🔗', type: 'success' })
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
        toast.show(gt('portForward.tunnelReconnected', { transport: transportAnnotation() }), { icon: '🔗', type: 'success' })
        await loadPorts(true)
        return
      }

      // Step 2: Port unreachable — reconnect tunnel (non-blocking)
      const reconnected = await nativeReconnectTunnel()

      if (reconnected) {
        const reachableAfter = await native.testPortReachable(localPort)
        if (reachableAfter) {
          toast.show(gt('portForward.tunnelReconnected', { transport: transportAnnotation() }), { icon: '🔗', type: 'success' })
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

  /**
   * Copy a reverse mapping's server-side address to the clipboard.
   *
   * Reverse mappings bind the server's loopback, so there is no browser to open
   * on this device — the useful action is handing the user the address to use in
   * a shell on the server host.
   */
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
    // Only forward mappings can satisfy a localhost URL click: a reverse entry
    // with a matching client-side port exposes it on the SERVER, not here.
    const existing = ports.value.find(p => p.port === port && p.host === (host || '') && !isReversePort(p))
    if (existing) {
      if (!existing.enabled) {
        await setPortEnabled(existing.localPort, true)
      }
      return existing.localPort
    }
    return registerPort(port, '', protocol, host, 'forward')
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
    activeTransport,
    transportAllowsH2,
    connectingPorts,
    localReachable,
    scanDrawerOpen,
    hasScanned,
    scanning,
    scanError,
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
    refreshActiveTransport,
    transportAnnotation,
    openPort,
    openPortWithCheck,
    openInExternalBrowser,
    reconnectPort,
    ensurePortRegistered,
  }
}
