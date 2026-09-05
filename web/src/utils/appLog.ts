/**
 * Unified frontend logger: always prints to browser console,
 * relays to host AppLog via ClawBenchNative.log() bridge when in app mode,
 * and relays to the server via HTTP POST when in web mode.
 *
 * Tag convention: use short PascalCase module name, e.g.
 *   'ClawBench', 'ChatStream', 'PortForward', 'Store', 'useDialog'
 *
 * Level mapping:
 *   appLog.d → console.log  / AppLog.d (D)
 *   appLog.i → console.info / AppLog.i (I)
 *   appLog.w → console.warn / AppLog.w (W)
 *   appLog.e → console.error/ AppLog.e (E)
 */

import { getNative, isNativeApp as nativeBridgeIsNativeApp } from '@/utils/clawbenchNative'

const LOG_ENDPOINT = '/api/client-log'
const FLUSH_INTERVAL_MS = 2000  // 2-second flush interval
const BUFFER_CAPACITY = 200     // max buffered entries
const FLUSH_TIMEOUT_MS = 5000   // abort a hung flush so isFlushing can never latch

interface LogEntry {
  level: string  // 'D' | 'I' | 'W' | 'E'
  tag: string
  msg: string
  ts: number     // epoch millis
  source: 'js'
}

function safeStringify(a: unknown): string {
  if (typeof a === 'string') return a
  if (typeof a === 'number' || typeof a === 'boolean') return String(a)
  try { return JSON.stringify(a) } catch { return String(a) }
}

// --- Android Native Bridge Relay ---

function relayToNative(level: string, tag: string, args: unknown[]): void {
  try {
    const native = getNative()
    if (!native || !native.log) return
    // Native-mode check to avoid false positives
    if (!isNativeApp()) return
    // Top-frame check to avoid iframe false positives
    if (window !== window.top) return
    const msg = args.map(safeStringify).join(' ')
    native.log(level, tag, msg)
  } catch {
    // bridge not available — silent
  }
}

// --- HTTP Relay (Buffered + Timed Flush) ---

const buffer: LogEntry[] = []
let flushTimer: ReturnType<typeof setInterval> | null = null
let isFlushing = false
// HTTP relay master switch. Relayed (per-token) appLog calls still hit the
// console and are appended to the ring buffer for diagnosis, but nothing is
// POSTed to /api/client-log while disabled. Controlled by the "Debug Log
// Capture" setting (SettingsCategory / App.vue) — a disabled relay must not
// keep firing requests just because logs are flowing.
let httpRelayEnabled = false

// Set whether HTTP relay may flush. The relay starts DISABLED and is only
// armed once the app boots with logCapture=true (or the user toggles it on).
export function setLogCaptureEnabled(enabled: boolean): void {
  httpRelayEnabled = enabled
  if (enabled) {
    startFlushTimer()
  } else {
    stopFlushTimer()
  }
}

function isNativeApp(): boolean {
  try {
    return nativeBridgeIsNativeApp()
  } catch {
    return false
  }
}

function enqueue(level: string, tag: string, args: unknown[]): void {
  // Native (App) mode no longer short-circuits here: when capture is ON the
  // JS logs are meant to be the single HTTP copy (console/native bridge are
  // skipped upstream), and when capture is OFF the doFlush gate + stopFlushTimer
  // buffer drop ensure nothing is ever POSTed. Buffering while disabled is
  // harmless (entries are discarded on stopFlushTimer).
  const msg = args.map(safeStringify).join(' ')
  buffer.push({ level, tag, msg, ts: Date.now(), source: 'js' })

  // Trim oldest entries when buffer overflows
  if (buffer.length > BUFFER_CAPACITY) {
    buffer.splice(0, buffer.length - BUFFER_CAPACITY)
  }
  // NOTE: no early/instant flush here. Entries are only sent on the single
  // fixed-interval timer (startFlushTimer, FLUSH_INTERVAL_MS). Any per-entry or
  // threshold-driven POST makes request rate track log rate, flooding the
  // connection pool with requests that pile up Pending in DevTools.
}

async function doFlush(): Promise<void> {
  // Hard gate: only the timer calls doFlush. A disabled relay must never send.
  if (!httpRelayEnabled || buffer.length === 0) return
  if (isFlushing) return
  isFlushing = true

  const toSend = buffer.splice(0, 200) // max 200 per request (server limit)
  try {
    // Abort hung requests: a fetch that never settles (dead keep-alive conn
    // silently dropped by an intermediary) would otherwise leave isFlushing
    // latched true and permanently stop the whole log relay.
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), FLUSH_TIMEOUT_MS)
    try {
      await fetch(LOG_ENDPOINT, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ entries: toSend }),
        keepalive: true,
        signal: ctrl.signal,
      })
    } catch {
      // Timeout (aborted) or server unreachable — discard, do not retry (avoid log storm)
    } finally {
      clearTimeout(timer)
    }
  } finally {
    // Always release the flush lock, even if fetch threw synchronously.
    isFlushing = false
  }
}

/** Start the periodic flush timer. Call once at app startup. */
export function startFlushTimer(): void {
  if (flushTimer !== null) return
  flushTimer = setInterval(doFlush, FLUSH_INTERVAL_MS)
}

/** Stop the periodic flush timer and flush remaining entries. Call at app teardown. */
export function stopFlushTimer(): void {
  if (flushTimer !== null) {
    clearInterval(flushTimer)
    flushTimer = null
  }
  // Disabling the relay must not send buffered entries afterwards — drop them.
  if (!httpRelayEnabled) {
    buffer.length = 0
  }
  doFlush()
}

// Flush on page visibility change (mobile tab switch, minimize, etc.)
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') doFlush()
  })
}

// --- Public API ---

type ConsoleMethod = 'log' | 'info' | 'warn' | 'error'

function emit(method: ConsoleMethod, level: 'D' | 'I' | 'W' | 'E', tag: string, args: unknown[]): void {
  // App (native) mode with capture ON: JS logs go to the server ONLY via the
  // HTTP relay — a single, structured ([js]-tagged) copy. Skipping console and
  // the native bridge avoids the duplicate "WebView:LOG" logcat line and the
  // lossy [object Object] serialization that goes with it.
  const singleHttp = isNativeApp() && httpRelayEnabled
  if (!singleHttp) {
    ;(console as Record<ConsoleMethod, (...a: unknown[]) => void>)[method](`[${tag}]`, ...args)
    relayToNative(level, tag, args)
  }
  enqueue(level, tag, args)
}

export const appLog = {
  d(tag: string, ...args: unknown[]) { emit('log', 'D', tag, args) },
  i(tag: string, ...args: unknown[]) { emit('info', 'I', tag, args) },
  w(tag: string, ...args: unknown[]) { emit('warn', 'W', tag, args) },
  e(tag: string, ...args: unknown[]) { emit('error', 'E', tag, args) },
}

/** Clear the HTTP relay buffer. For testing only. */
export function _clearBuffer(): void {
  buffer.length = 0
}
