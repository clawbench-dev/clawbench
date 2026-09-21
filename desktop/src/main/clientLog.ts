import { app, session } from 'electron'
import fs from 'node:fs'
import http from 'node:http'
import https from 'node:https'
import path from 'node:path'
import { getStore } from './store'

/**
 * Client log relay for the Electron shell.
 *
 * The renderer's `appLog` already relays page logs to the server (as
 * `source="js"`), but the MAIN process had no channel at all: `native:log` was
 * a no-op stub and uncaught exceptions only reached stdout, which is gone as
 * soon as the terminal closes. That made exactly the failures worth
 * diagnosing — SSH/tunnel errors, failed upgrades, crashes — the least
 * observable part of the app.
 *
 * Two destinations, mirroring the Android/JS split:
 *   - `{userData}/desktop.log`, the local file (also receives renderer console
 *     output while capture is on, so one file holds the whole shell story)
 *   - `POST /api/client-log` with `source="electron"`, so shell logs land in
 *     the same server-side `client.log` as `[js]` and `[android]` lines
 *
 * Everything here is best-effort: logging must never be able to break the app,
 * so every failure path is swallowed.
 */

const LOG_ENDPOINT_PATH = '/api/client-log'
const FLUSH_INTERVAL_MS = 2000
const BUFFER_CAPACITY = 200
const FLUSH_TIMEOUT_MS = 5000
const MAX_PER_REQUEST = 200

export type LogLevel = 'D' | 'I' | 'W' | 'E'

interface Entry {
  level: LogLevel
  tag: string
  msg: string
  ts: number
  source: 'electron'
}

let fileStream: fs.WriteStream | null = null
let flushTimer: ReturnType<typeof setInterval> | null = null
let buffer: Entry[] = []
let flushing = false
let enabled = false
let consoleListener: ((event: Electron.Event, level: number, message: string) => void) | null = null

function logFilePath(): string {
  return path.join(app.getPath('userData'), 'desktop.log')
}

function safeStringify(v: unknown): string {
  if (typeof v === 'string') return v
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  if (v instanceof Error) return v.stack || v.message
  try {
    return JSON.stringify(v)
  } catch {
    return String(v)
  }
}

/** Append a line to the local desktop.log. Best-effort. */
function writeLocal(level: LogLevel, tag: string, msg: string): void {
  if (!fileStream) return
  try {
    // Newlines in the message would split one entry across lines and let a
    // logged object forge additional records — the same reason the server
    // escapes every field.
    const flat = msg.replace(/[\r\n]+/g, '\\n')
    fileStream.write(`${new Date().toISOString()} [${level}] ${tag}: ${flat}\n`)
  } catch {
    // Disk full / stream closed — never let logging break the caller.
  }
}

/**
 * Read the session cookie for the configured server.
 *
 * The main process has no `document.cookie`; the cookie lives in Electron's
 * cookie jar. The name is port-scoped on the server (`cb<port>_clawbench_session`
 * unless the port is 20000, where it is the bare name), so match by suffix
 * rather than hardcoding one form.
 */
async function getSessionCookie(): Promise<string | null> {
  try {
    const cookies = await session.defaultSession.cookies.get({})
    const hit = cookies.find((c) => c.name === 'clawbench_session' || c.name.endsWith('_clawbench_session'))
    return hit ? `${hit.name}=${hit.value}` : null
  } catch {
    return null
  }
}

/**
 * POST one batch. Returns silently on any failure — the server may be
 * unreachable, and a log relay must not surface errors of its own (nor retry
 * into a storm).
 */
async function postBatch(entries: Entry[]): Promise<void> {
  const serverUrl = getStore().get('serverUrl')
  if (!serverUrl) return

  let url: URL
  try {
    url = new URL(serverUrl)
  } catch {
    return
  }

  const cookie = await getSessionCookie()
  if (!cookie) return // not logged in yet — nothing to authenticate with

  const body = JSON.stringify({ entries })
  const lib = url.protocol === 'https:' ? https : http

  await new Promise<void>((resolve) => {
    let settled = false
    const done = () => { if (!settled) { settled = true; resolve() } }

    let req: http.ClientRequest
    try {
      req = lib.request(
        {
          protocol: url.protocol,
          hostname: url.hostname,
          port: url.port || (url.protocol === 'https:' ? 443 : 80),
          path: LOG_ENDPOINT_PATH,
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Content-Length': Buffer.byteLength(body),
            Cookie: cookie,
          },
          // The desktop shell already talks to this server this way (see
          // tunnel.ts fetchSshInfo): a self-hosted instance commonly uses a
          // self-signed certificate, and refusing it here would silently drop
          // every log from exactly those installs.
          rejectUnauthorized: false,
          timeout: FLUSH_TIMEOUT_MS,
        },
        (res) => {
          res.resume() // drain
          res.on('end', done)
          res.on('error', done)
        },
      )
    } catch {
      return done()
    }

    req.on('error', done)
    req.on('timeout', () => { req.destroy(); done() })
    req.on('close', done)
    req.end(body)
  })
}

async function flush(): Promise<void> {
  if (!enabled || flushing || buffer.length === 0) return
  flushing = true
  const batch = buffer.splice(0, MAX_PER_REQUEST)
  try {
    await postBatch(batch)
  } catch {
    // never propagate
  } finally {
    flushing = false
  }
}

/** Record one entry: local file always, server when the relay is armed. */
export function record(level: LogLevel, tag: string, msg: string): void {
  writeLocal(level, tag, msg)

  // Buffer regardless of `enabled` so arming later still has recent context,
  // but bound it so a long disabled stretch cannot grow without limit.
  buffer.push({ level, tag, msg, ts: Date.now(), source: 'electron' })
  if (buffer.length > BUFFER_CAPACITY) {
    buffer.splice(0, buffer.length - BUFFER_CAPACITY)
  }
}

/** Log an error/thrown value from the main process. */
export function recordError(tag: string, err: unknown): void {
  record('E', tag, safeStringify(err))
}

/** Map Chromium's numeric console level onto appLog's letters. */
function consoleLevelToLetter(level: number): LogLevel {
  switch (level) {
    case 0: return 'D' // verbose
    case 1: return 'I' // info
    case 2: return 'W' // warning
    case 3: return 'E' // error
    default: return 'I'
  }
}

/**
 * Mirror the renderer's console into desktop.log while capture is on.
 *
 * Note this only sees output that actually reaches the console. `appLog`
 * deliberately skips `console.*` on Android (to avoid a duplicate logcat
 * line), but on desktop that skip would leave this listener with nothing to
 * capture — hence the matching change in appLog.ts, which keeps the console
 * alive in the Electron shell.
 */
export function attachRendererConsole(wc: Electron.WebContents): void {
  if (consoleListener) return
  consoleListener = (_event, level, message) => {
    writeLocal(consoleLevelToLetter(level), 'Renderer', message)
  }
  wc.on('console-message', consoleListener)
}

export function detachRendererConsole(wc: Electron.WebContents | null): void {
  if (wc && consoleListener) wc.removeListener('console-message', consoleListener)
  consoleListener = null
}

/** Arm the relay: open the local file, start the flush timer, capture console. */
export function startClientLog(wc: Electron.WebContents | null): void {
  if (enabled) return
  enabled = true

  try {
    fileStream = fs.createWriteStream(logFilePath(), { flags: 'a' })
    // A write error on the stream (disk full, permissions) must not throw into
    // the main process; record() already guards, this covers async errors.
    fileStream.on('error', () => { fileStream = null })
  } catch {
    fileStream = null
  }

  if (wc) attachRendererConsole(wc)
  if (flushTimer === null) flushTimer = setInterval(() => { void flush() }, FLUSH_INTERVAL_MS)
}

/** Disarm: stop the timer, flush what is pending, close the file. */
export function stopClientLog(wc: Electron.WebContents | null): void {
  enabled = false
  detachRendererConsole(wc)

  if (flushTimer !== null) {
    clearInterval(flushTimer)
    flushTimer = null
  }
  // Disabled: drop the buffer rather than posting it afterwards, matching the
  // renderer relay's behaviour.
  buffer = []

  if (fileStream) {
    try { fileStream.end() } catch { /* ignore */ }
    fileStream = null
  }
}

/** Flush pending entries on shutdown so a crash report is not lost. */
export async function flushOnShutdown(): Promise<void> {
  if (flushTimer !== null) {
    clearInterval(flushTimer)
    flushTimer = null
  }
  await flush()
}

/** Test helper — reset module state. */
export function _resetForTesting(): void {
  enabled = false
  buffer = []
  flushing = false
  if (flushTimer !== null) { clearInterval(flushTimer); flushTimer = null }
  fileStream = null
  consoleListener = null
}

/** Test helper — expose the buffered entries. */
export function _bufferForTesting(): ReadonlyArray<Entry> {
  return buffer
}

/**
 * Test helper — wait for pending local writes to hit disk.
 *
 * `fs.createWriteStream` writes asynchronously, so a test that reads
 * desktop.log right after record() sees an empty file even though the write
 * succeeded. Production never reads the file itself, so this exists purely to
 * make the tests deterministic.
 */
export function _flushLocalForTesting(): Promise<void> {
  if (!fileStream) return Promise.resolve()
  return new Promise((resolve) => {
    fileStream!.write('', () => resolve())
  })
}
