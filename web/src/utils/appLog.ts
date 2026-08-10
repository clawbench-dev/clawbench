/**
 * Unified frontend logger: always prints to browser console,
 * relays to Android AppLog via AndroidNative.log() bridge when in app mode,
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
const FLUSH_THRESHOLD = 50      // flush early when threshold reached

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

function isNativeApp(): boolean {
  try {
    return nativeBridgeIsNativeApp()
  } catch {
    return false
  }
}

function enqueue(level: string, tag: string, args: unknown[]): void {
  // Skip HTTP relay in Android app — logs already go via native bridge
  if (isNativeApp()) return

  const msg = args.map(safeStringify).join(' ')
  buffer.push({ level, tag, msg, ts: Date.now(), source: 'js' })

  // Trim oldest entries when buffer overflows
  if (buffer.length > BUFFER_CAPACITY) {
    buffer.splice(0, buffer.length - BUFFER_CAPACITY)
  }

  // Flush early when threshold reached
  if (buffer.length >= FLUSH_THRESHOLD) {
    scheduleFlush()
  }
}

function scheduleFlush(): void {
  if (isFlushing) return
  // Use microtask to coalesce multiple enqueues in the same event loop
  Promise.resolve().then(doFlush)
}

async function doFlush(): Promise<void> {
  if (isFlushing || buffer.length === 0) return
  isFlushing = true

  const toSend = buffer.splice(0, 200) // max 200 per request (server limit)
  try {
    await fetch(LOG_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ entries: toSend }),
      keepalive: true,
    })
  } catch {
    // Server unreachable — discard, do not retry (avoid log storm)
  }
  isFlushing = false

  // If more entries accumulated during flush, continue
  if (buffer.length > 0) {
    scheduleFlush()
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
  doFlush()
}

// Flush on page visibility change (mobile tab switch, minimize, etc.)
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') doFlush()
  })
}

// --- Public API ---

export const appLog = {
  d(tag: string, ...args: unknown[]) { console.log(`[${tag}]`, ...args); relayToNative('D', tag, args); enqueue('D', tag, args) },
  i(tag: string, ...args: unknown[]) { console.info(`[${tag}]`, ...args); relayToNative('I', tag, args); enqueue('I', tag, args) },
  w(tag: string, ...args: unknown[]) { console.warn(`[${tag}]`, ...args); relayToNative('W', tag, args); enqueue('W', tag, args) },
  e(tag: string, ...args: unknown[]) { console.error(`[${tag}]`, ...args); relayToNative('E', tag, args); enqueue('E', tag, args) },
}

/** Clear the HTTP relay buffer. For testing only. */
export function _clearBuffer(): void {
  buffer.length = 0
}
