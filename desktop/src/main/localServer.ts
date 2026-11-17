import { app, session, type Session } from 'electron'
import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { getStore } from './store'
import { getPasswordFor, savePasswordFor } from './secrets'
import { record, recordError } from './clientLog'
import { embeddedServerLabels, resolveNativeLanguage } from './labels'
import { portsInUse } from './portProbe'
import {
  EMBEDDED_DEFAULT_PORT,
  EMBEDDED_EXIT_GRACE_MS,
  EMBEDDED_HEALTH_INTERVAL_MS,
  EMBEDDED_SERVER_DIR,
  EMBEDDED_SERVER_EXE,
  EMBEDDED_STARTUP_TIMEOUT_MS,
  chooseEmbeddedPort,
  embeddedServerArgs,
  embeddedServerUrl,
  isClawBenchHealth,
  reservedPortsForEmbedded,
  sessionCookieName,
} from './localServerPolicy'

/**
 * The embedded server: an "all-in-one" desktop build starts a ClawBench server
 * of its own and quits it when the app quits.
 *
 * Lifecycle, in order:
 *
 *  1. Pick a port (the last one if free — browser storage is keyed by origin).
 *  2. Spawn `server/clawbench.exe` (next to the executable), holding its stdin. On Windows a
 *     child cannot be signalled gracefully, so the pipe IS the quit button: when
 *     this process closes it — deliberately, or by crashing — the server sees
 *     EOF and drains itself. No orphan survives the app.
 *  3. Poll /api/health until it identifies itself as ClawBench.
 *  4. Log in silently with the password the server generated for itself and
 *     plant the session cookie, so the app never shows the admin login form.
 *  5. Remember the URL, then let the normal window/navigation take over.
 *
 * Only one server per data directory is allowed (the server locks it), which is
 * why this also refuses to start a second copy: a stale server from a crashed
 * previous run would otherwise make every launch fail with "data directory in
 * use". Killing it is safe here and only here — the data directory belongs to
 * the app package, and no other ClawBench install in its right mind points at it.
 */

/** Store key: the port the embedded server used last time. */
export const EMBEDDED_PORT_KEY = 'embeddedServerPort'

let child: ChildProcess | null = null
let serverUrl = ''
let stopping = false
let embeddedActive = false

/**
 * Whether this session runs against the embedded server.
 *
 * Set by index.ts after the server answered, and read by the window/navigation
 * code to skip the login page and the desktop version gate. Kept separate from
 * `child` on purpose: a failed start leaves no child but must NOT look like
 * "embedded mode, no server URL" to the rest of the app — it falls back to the
 * ordinary login page.
 */
export function isEmbeddedServerActive(): boolean {
  return embeddedActive && !!child && !stopping
}

export function setEmbeddedServerActive(active: boolean): void {
  embeddedActive = active
}

/** The embedded server's base URL (e.g. `http://127.0.0.1:20000`), or ''. */
export function getEmbeddedServerUrl(): string {
  return serverUrl
}

/** Path of the server binary shipped with this app. */
export function embeddedServerBinaryPath(): string {
  // The app root, not `resources/`: electron-builder's `extraFiles` puts the
  // server next to the executable precisely so a payload update (which replaces
  // `resources/`) can never remove it.
  return path.join(path.dirname(process.execPath), EMBEDDED_SERVER_DIR, EMBEDDED_SERVER_EXE)
}

/**
 * Data directory for the embedded server: under the app's own userData, so it
 * never collides with a standalone install in `~/.clawbench` (two servers on one
 * data directory corrupt each other, and the lock would make one refuse to
 * start).
 */
export function embeddedDataDir(): string {
  return path.join(app.getPath('userData'), 'server')
}

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/** Read a whole small file, or '' when it is missing. */
function readTextQuietly(file: string): string {
  try {
    return fs.readFileSync(file, 'utf8').trim()
  } catch {
    return ''
  }
}

/**
 * Password the embedded server uses.
 *
 * The server writes one to `auto-password` when config.yaml has none. The app
 * must therefore read it from disk on EVERY start rather than caching it: the
 * user can change the password in the web UI, which deletes that file and stores
 * a hash in config.yaml — from then on the only credential is the one the user
 * chose, and it has to be the one the desktop login uses too. That is why this
 * falls back to the saved credential for the embedded URL.
 */
function embeddedServerPassword(port: number): string {
  const fromFile = readTextQuietly(path.join(embeddedDataDir(), 'auto-password'))
  if (fromFile) return fromFile
  return getPasswordFor(embeddedServerUrl(port))
}

/** Probe /api/health once. Returns the parsed body, or null. */
async function probeHealth(port: number): Promise<unknown> {
  try {
    const res = await fetch(embeddedServerUrl(port) + '/api/health', { signal: AbortSignal.timeout(2000) })
    if (!res.ok) return null
    return await res.json()
  } catch {
    return null
  }
}

/**
 * The PID of the server a previous run left behind, or 0.
 *
 * Read from `server.pid`, which the server writes after taking the data
 * directory lock and removes on a graceful exit. A hard kill leaves it, which is
 * the only situation this is needed for.
 */
function previousServerPid(): number {
  const raw = readTextQuietly(path.join(embeddedDataDir(), 'server.pid'))
  const pid = Number(raw)
  return Number.isInteger(pid) && pid > 0 ? pid : 0
}

/**
 * Whether `pid` is a live ClawBench process.
 *
 * The PID file survives a crash, and a PID can be reused by an unrelated program
 * before the next launch. Signalling on the number alone would then kill a
 * stranger, so the executable name is checked first. Best-effort: a failed check
 * means "do not kill", and the port walk takes over.
 */
function isClawBenchProcess(pid: number): boolean {
  try {
    if (process.platform === 'win32') {
      const out = execFileSync('tasklist', ['/FI', `PID eq ${pid}`, '/FO', 'CSV', '/NH'], { encoding: 'utf8' })
      return out.toLowerCase().includes(EMBEDDED_SERVER_EXE)
    }
    const out = execFileSync('ps', ['-p', String(pid), '-o', 'comm='], { encoding: 'utf8' })
    return out.toLowerCase().includes('clawbench')
  } catch {
    return false
  }
}

/** Whether a PID is alive. Signal 0 performs the existence check only. */
function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

/**
 * Stop a server this app left behind (a crash or a force-kill skips the stdin
 * close, and the next launch would otherwise fail on the data directory lock).
 *
 * Only ever our own: the caller has already checked that the PID file names a
 * live process whose executable is ClawBench. Killing by image name instead
 * would take down a standalone install the user is running elsewhere.
 */
async function reapPreviousServer(pid: number): Promise<boolean> {
  record('W', 'LocalServer', `reaping a server left behind by a previous run (pid ${pid})`)
  try {
    if (process.platform === 'win32') {
      // No graceful signal on Windows; the next start repairs the interrupted
      // turn this leaves behind.
      execFileSync('taskkill', ['/F', '/T', '/PID', String(pid)], { stdio: 'ignore' })
    } else {
      process.kill(pid, 'SIGTERM')
    }
  } catch (e) {
    record('I', 'LocalServer', `reap: ${(e as Error).message}`)
  }
  for (let i = 0; i < 60; i++) {
    if (!isProcessAlive(pid)) return true
    if (i === 40 && process.platform !== 'win32') {
      // The graceful drain had its chance; stop waiting on it.
      try { process.kill(pid, 'SIGKILL') } catch { /* gone */ }
    }
    await wait(250)
  }
  return false
}

/** Wait until the server answers as ClawBench, or throw with what it said. */
async function waitForHealth(port: number): Promise<void> {
  const deadline = Date.now() + EMBEDDED_STARTUP_TIMEOUT_MS
  let lastBody: unknown = null
  while (Date.now() < deadline) {
    if (child && child.exitCode !== null) break
    lastBody = await probeHealth(port)
    if (isClawBenchHealth(lastBody)) return
    await wait(EMBEDDED_HEALTH_INTERVAL_MS)
  }
  if (lastBody && !isClawBenchHealth(lastBody)) {
    throw new Error(`port ${port} answered, but it is not ClawBench`)
  }
  throw new Error(`the embedded server did not become ready within ${Math.round(EMBEDDED_STARTUP_TIMEOUT_MS / 1000)}s`)
}

/**
 * Name our own server in the server list on first run.
 *
 * A bare `http://127.0.0.1:20000` label is meaningless in the switcher when the
 * machine also reaches other servers, and "本机" is what the user will look for.
 * Only applied once (the entry is created here), so a user rename survives.
 */
function nameEmbeddedEntry(): void {
  const store = getStore()
  const servers = store.get('servers')
  const entry = servers.find((s) => s.url === serverUrl)
  if (!entry || entry.name) return
  entry.name = embeddedServerLabels(resolveNativeLanguage()).name
  store.set('servers', servers)
}

/**
 * Log in and plant the session cookie in Chromium's jar.
 *
 * Doing it here rather than in the page means the app's very first request is
 * already authenticated: no login form, no flash of "unauthorized", and nothing
 * for the renderer to coordinate with the server's startup.
 *
 * POST /login is public precisely so a client can obtain the cookie before it
 * has one; it answers Set-Cookie, and on success also re-writes it (7 days).
 */
async function plantSessionCookie(port: number, ses: Session): Promise<void> {
  const password = embeddedServerPassword(port)
  if (!password) {
    throw new Error(`the embedded server on port ${port} has no readable password`)
  }
  // Electron's net-backed fetch shares the session's cookie jar, so the answer
  // to /login lands in the jar as well as in this response. Both are kept: the
  // Set-Cookie is authoritative if the jar write is ever refused.
  const res = await ses.fetch(embeddedServerUrl(port) + '/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  if (!res.ok) {
    throw new Error(res.status === 401 ? 'the embedded server rejected its own password' : `login failed: HTTP ${res.status}`)
  }
  const name = sessionCookieName(port)
  const url = embeddedServerUrl(port)
  // The jar is the authority: `ses.fetch` already stored whatever Set-Cookie
  // carried, and that header can hold more than one cookie (the project cookie
  // is written on some responses). Reading the jar first avoids parsing the
  // wrong one out of the header and then declaring success while the session
  // cookie is missing.
  let jar = await ses.cookies.get({ url, name })
  if (!jar.some((c) => c.value)) {
    const setCookie = res.headers.get('set-cookie') || ''
    const match = new RegExp(`(?:^|[,\\s])${name}=([^;,\\s]+)`).exec(setCookie)
    if (match) {
      await ses.cookies.set({ url, name, value: match[1], path: '/', httpOnly: true, sameSite: 'lax' })
      jar = await ses.cookies.get({ url, name })
    }
  }
  if (!jar.some((c) => c.value)) {
    throw new Error('the session cookie could not be stored')
  }
  // Keep the credential for the next start (and for the SSH tunnel, which reads
  // the same per-server slot). After a password change this refreshes it.
  savePasswordFor(url, password)
}

/**
 * Start the embedded server and prepare the app to open it.
 *
 * Returns the URL to load. Rejects with a message meant for the failure screen.
 */
export async function startEmbeddedServer(): Promise<string> {
  const binary = embeddedServerBinaryPath()
  if (!fs.existsSync(binary)) {
    throw new Error(`the server binary is missing from this build: ${binary}`)
  }

  fs.mkdirSync(embeddedDataDir(), { recursive: true })
  const preferred = Number(getStore().get(EMBEDDED_PORT_KEY)) || EMBEDDED_DEFAULT_PORT
  const reserved = reservedPortsForEmbedded(getStore().get('servers').map((s) => s.url))

  // A server this app left behind holds the data directory lock, so it must go
  // before the port walk — a different port would not help, the new server
  // would still wait 30s and fail. The PID file is only written by our own
  // server and only survives a crash, and the executable is checked before
  // signalling, so a standalone install is never touched.
  const stalePid = previousServerPid()
  if (stalePid && isClawBenchProcess(stalePid)) {
    await reapPreviousServer(stalePid)
  }
  // Probe the preferred port and the default; the walk itself finds what is
  // free, and a port it skips for being reserved is never probed. The default
  // is included because a preferred port below 1024 is rejected by the policy,
  // so the walk has to start from a port the probe already looked at.
  const inUse = await portsInUse([preferred, EMBEDDED_DEFAULT_PORT])
  const port = chooseEmbeddedPort({ preferred, inUse, reserved })
  if (!port) throw new Error('no free local port for the embedded server')

  serverUrl = embeddedServerUrl(port)
  const args = embeddedServerArgs({ port, dataDir: embeddedDataDir() })
  record('I', 'LocalServer', `starting ${binary} ${args.join(' ')}`)

  const spawned = spawn(binary, args, {
    // stdin is OUR channel to the server (see watchParentPipe on the server
    // side): it stays open for the life of the app and closing it is the quit.
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
    cwd: embeddedDataDir(),
  })
  child = spawned
  spawned.once('exit', (code, signal) => {
    record('W', 'LocalServer', `server exited code=${code} signal=${signal}`)
    if (!stopping) {
      // It went away on its own: the app has nothing to talk to, and silently
      // reloading would loop. Surface it the way a failed connect is surfaced.
      serverUrl = ''
    }
  })
  spawned.stderr?.on('data', (b: Buffer) => {
    for (const line of b.toString('utf8').split('\n')) {
      if (line.trim()) record('I', 'Server', line.trim())
    }
  })
  spawned.stdout?.on('data', (b: Buffer) => {
    for (const line of b.toString('utf8').split('\n')) {
      if (line.trim()) record('I', 'Server', line.trim())
    }
  })
  spawned.once('error', (err) => recordError('LocalServer', err))

  await waitForHealth(port)
  await plantSessionCookie(port, session.defaultSession)

  getStore().set(EMBEDDED_PORT_KEY, port)
  // Same URL the renderer will use; written before the window exists so the
  // existing cold-start path (window.ts reads serverUrl) just works.
  getStore().set('serverUrl', serverUrl)
  const servers = getStore().get('servers')
  if (!servers.some((s) => s.url === serverUrl)) {
    getStore().set('servers', [{ url: serverUrl }, ...servers])
    nameEmbeddedEntry()
  }
  return serverUrl
}

/**
 * Stop the embedded server: close its stdin (its quit signal), wait for a
 * graceful drain, then kill whatever is left.
 *
 * Synchronous where it can be, because it runs from `before-quit`, which
 * Electron does not await. The kill timer is `unref`d so a shutdown that already
 * finished does not keep the process alive for the full grace period.
 */
export function stopEmbeddedServer(): void {
  const proc = child
  if (!proc) return
  if (proc.exitCode !== null) {
    // It already exited (a crash, or the previous quit path): nothing to signal.
    child = null
    serverUrl = ''
    embeddedActive = false
    return
  }
  stopping = true
  child = null
  serverUrl = ''
  embeddedActive = false
  try {
    proc.stdin?.end()
  } catch {
    /* already gone */
  }
  const timer = setTimeout(() => {
    try {
      proc.kill()
    } catch {
      /* already gone */
    }
  }, EMBEDDED_EXIT_GRACE_MS)
  timer.unref()
  proc.once('exit', () => clearTimeout(timer))
}
