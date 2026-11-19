/**
 * Generic composable for SSE/WebSocket reconnection with exponential backoff.
 * Extracts the common reconnect logic shared across useTerminalSession,
 * useChatStream, and useFileWatch.
 */
import { ref } from 'vue'

export interface ReconnectOptions {
  baseDelay?: number             // default 2000 (ms)
  maxDelay?: number              // default 15000 (ms), caps exponential backoff
  onReconnect: () => void        // callback to reconnect
  getFatalError?: () => boolean | null // return non-null = fatal, null = safe to reconnect
}

export interface ResetOptions {
  /**
   * Temporary base delay for the retry ladder that starts here. Omitted = fall
   * back to `options.baseDelay`.
   *
   * Exists for foreground resume: the user is looking at the screen, so a
   * disconnect discovered on return must not sit out the normal 2s/4s/8s ladder
   * before the first redial. It is deliberately an override on reset() rather
   * than a second scheduling path — the ladder itself stays the one algorithm,
   * and the next successful connect (which calls reset() with no arguments)
   * restores the normal cadence.
   */
  baseDelay?: number
}

export function useReconnect(options: ReconnectOptions) {
  let reconnectAttempts = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let disabled = false
  let baseDelayOverride: number | null = null

  // Reactive reconnecting state — true when a reconnect is scheduled or in-progress
  const reconnecting = ref(false)

  function scheduleReconnect() {
    reconnecting.value = true
    const cappedDelay = options.maxDelay ?? 15000
    const base = baseDelayOverride ?? options.baseDelay ?? 2000
    const delay = Math.min(base * Math.pow(2, reconnectAttempts), cappedDelay)
    reconnectTimer = setTimeout(() => {
      reconnectAttempts++
      options.onReconnect()
    }, delay)
  }

  function reset(overrides?: ResetOptions) {
    reconnectAttempts = 0
    disabled = false
    reconnecting.value = false
    baseDelayOverride = overrides?.baseDelay ?? null
    if (reconnectTimer) clearTimeout(reconnectTimer)
    reconnectTimer = null
  }

  function disable() {
    disabled = true
    reconnecting.value = false
    if (reconnectTimer) clearTimeout(reconnectTimer)
    reconnectTimer = null
  }

  /**
   * Drop a pending retry without touching the attempt counter or the disabled
   * flag.
   *
   * Called when a socket is dialled from somewhere other than the retry ladder
   * (foreground resume, an explicit connect). Without it, a retry scheduled a
   * moment earlier keeps its timer and fires while the new handshake is still
   * in flight — tearing down the connection it was meant to establish and
   * costing another full handshake. reset() cannot be used for this: it also
   * zeroes the attempt count, which would turn a failing ladder into a tight
   * loop.
   */
  function cancelPending() {
    if (reconnectTimer) clearTimeout(reconnectTimer)
    reconnectTimer = null
  }

  function shouldReconnect(): boolean {
    if (disabled) return false
    const fatalError = options.getFatalError?.()
    if (fatalError !== undefined && fatalError !== null) return false
    return true
  }

  return {
    reconnecting,
    scheduleReconnect,
    reset,
    disable,
    cancelPending,
    shouldReconnect,
    getAttempts: () => reconnectAttempts,
  }
}