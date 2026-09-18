import { ref, watch, onUnmounted } from 'vue'
import { useGlobalEvents } from '@/composables/useGlobalEvents'

export interface CPUInfo {
  percent: number
  core_count: number
}

export interface MemoryInfo {
  used: number
  total: number
  percent: number
}

export interface DiskInfo {
  used: number
  total: number
  percent: number
}

export interface NetworkInfo {
  upload_rate: number
  download_rate: number
}

export interface DiskIOInfo {
  read_rate: number
  write_rate: number
}

export interface LoadInfo {
  load1: number
  load5: number
  load15: number
}

export interface SystemResources {
  cpu: CPUInfo
  memory: MemoryInfo
  disk: DiskInfo
  disk_io: DiskIOInfo
  network: NetworkInfo
  load: LoadInfo
  errors?: string[]
}

/** Foreground rate: the resources panel is open. */
const FOREGROUND_INTERVAL_MS = 1000
/** Background rate: tab visible but the panel is closed. */
const BACKGROUND_INTERVAL_MS = 5000

let activeCount = 0
let backgroundCount = 0

const resources = ref<SystemResources>({
  cpu: { percent: 0, core_count: 0 },
  memory: { used: 0, total: 0, percent: 0 },
  disk: { used: 0, total: 0, percent: 0 },
  disk_io: { read_rate: 0, write_rate: 0 },
  network: { upload_rate: 0, download_rate: 0 },
  load: { load1: 0, load5: 0, load15: 0 },
})

// useGlobalEvents exposes module-level singletons, so calling it at module
// scope is safe (it registers no lifecycle hooks).
const { connected, onEvent, sendWsMessage } = useGlobalEvents()

/**
 * What the server should push right now. A hidden tab needs no data at all —
 * this preserves the polling version's "hidden = stopped" semantics.
 */
function currentRate(): { enabled: boolean; intervalMs: number } {
  if (document.hidden) return { enabled: false, intervalMs: 0 }
  if (activeCount > 0) return { enabled: true, intervalMs: FOREGROUND_INTERVAL_MS }
  if (backgroundCount > 0) return { enabled: true, intervalMs: BACKGROUND_INTERVAL_MS }
  return { enabled: false, intervalMs: 0 }
}

// Last rate actually sent, so refcount churn does not spam the socket.
let lastDeclared: { enabled: boolean; intervalMs: number } | null = null
let unsubscribe: (() => void) | null = null

function declareRate() {
  // send() drops silently when the socket is not open. Bailing here (instead of
  // recording the intent) is what lets the reconnect watcher re-declare it.
  if (!connected.value) return

  const want = currentRate()
  if (lastDeclared && lastDeclared.enabled === want.enabled && lastDeclared.intervalMs === want.intervalMs) {
    return
  }

  sendWsMessage(
    want.enabled
      ? { type: 'metrics_preference', metrics_enabled: true, metrics_interval_ms: want.intervalMs }
      : { type: 'metrics_preference', metrics_enabled: false },
  )
  lastDeclared = want
}

function onMetricsEvent(event: string, data: unknown) {
  if (event !== 'system_resources') return
  if (!data) return
  resources.value = data as SystemResources
}

// Re-register on every (re)connect. A one-time module-level guard is NOT enough
// here: useGlobalEvents.destroy() clears the shared handler array on project
// switch / logout, and unlike frp_status there is no fallback fetch to recover
// — the panel would simply freeze forever.
watch(connected, (isConnected) => {
  if (!isConnected) return
  unsubscribe?.() // no-op if the array was already wiped
  unsubscribe = onEvent(onMetricsEvent)
  // The server clears the preference on every new connection (it cannot know
  // the new connection's intent), so re-declare unconditionally.
  lastDeclared = null
  declareRate()
})

// Pause/resume with tab visibility. In browser mode the socket stays open, so
// the explicit disable message is what stops server-side sampling; in App mode
// useGlobalEvents disconnects the socket, which drops demand server-side
// (a disconnected client contributes none).
function onVisibilityChange() {
  declareRate()
}

let visibilityListenerAttached = false

function attachVisibilityListener() {
  if (visibilityListenerAttached) return
  visibilityListenerAttached = true
  document.addEventListener('visibilitychange', onVisibilityChange)
}

function detachVisibilityListener() {
  if (!visibilityListenerAttached) return
  visibilityListenerAttached = false
  document.removeEventListener('visibilitychange', onVisibilityChange)
}

function startPolling() {
  activeCount++
  attachVisibilityListener()
  declareRate()
}

function stopPolling() {
  activeCount = Math.max(0, activeCount - 1)
  if (activeCount + backgroundCount === 0) detachVisibilityListener()
  declareRate()
}

function startBackgroundPolling() {
  backgroundCount++
  attachVisibilityListener()
  declareRate()
}

function stopBackgroundPolling() {
  backgroundCount = Math.max(0, backgroundCount - 1)
  if (activeCount + backgroundCount === 0) detachVisibilityListener()
  declareRate()
}

export function useSystemResources() {
  let isForeground = false
  let isBackground = false

  onUnmounted(() => {
    if (isForeground) stopPolling()
    if (isBackground) stopBackgroundPolling()
  })

  const wrappedStartPolling = () => {
    isForeground = true
    startPolling()
  }
  const wrappedStopPolling = () => {
    isForeground = false
    stopPolling()
  }
  const wrappedStartBackgroundPolling = () => {
    isBackground = true
    startBackgroundPolling()
  }
  const wrappedStopBackgroundPolling = () => {
    isBackground = false
    stopBackgroundPolling()
  }

  return {
    resources,
    startPolling: wrappedStartPolling,
    stopPolling: wrappedStopPolling,
    startBackgroundPolling: wrappedStartBackgroundPolling,
    stopBackgroundPolling: wrappedStopBackgroundPolling,
  }
}
