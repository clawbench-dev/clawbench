import { app, session, type Session } from 'electron'
import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { getStore } from './store'
import { getEmbeddedPassword, getRowPasswordFor, saveEmbeddedPassword } from './secrets'
import { record, recordError } from './clientLog'
import { portsInUse } from './portProbe'
import {
  EMBEDDED_DEFAULT_PORT,
  EMBEDDED_EXIT_GRACE_MS,
  EMBEDDED_HEALTH_INTERVAL_MS,
  EMBEDDED_SERVER_EXE,
  EMBEDDED_STARTUP_TIMEOUT_MS,
  chooseEmbeddedPort,
  embeddedServerArgs,
  embeddedServerBinaryPath as embeddedServerBinaryPathFor,
  embeddedServerUrl,
  isClawBenchHealth,
  isHealthFromChild,
  reservedPortsForEmbedded,
  sameServerUrl,
  sessionCookieName,
  shouldAdoptEmbeddedServer,
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
 * Set by index.ts after the server answered, and read by the failure path (a
 * dead built-in server gets its own explanation) and the login page (running /
 * not running). Kept separate from `child` on purpose: a failed start leaves no
 * child but must NOT look like "embedded mode, no server URL" to the rest of the
 * app — it falls back to the ordinary login page.
 *
 * NOT the way to ask "is this URL ours" — see {@link isEmbeddedServerUrl}: a
 * fused build keeps this server running while the user is on another machine.
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

/**
 * Whether `url` is the server this build ships with.
 *
 * Deliberately a question about the ADDRESS, not about {@link
 * isEmbeddedServerActive}: a fused build keeps its own server running while the
 * user is connected to another machine, and "we run a local server" must not
 * make that other server look like ours (the version gate, the failure dialog
 * and the login page all key off this).
 */
export function isEmbeddedServerUrl(url: string): boolean {
  if (!url) return false
  if (serverUrl && sameServerUrl(url, serverUrl)) return true
  const stored = getStore().get('embeddedServerUrl')
  return !!stored && sameServerUrl(url, stored)
}

export interface EmbeddedServerInfo {
  /** Address the login page should select. */
  url: string
  /** Saved credential ('' when none yet — a first start has not happened). */
  password: string
  running: boolean
}

/**
 * What the login page needs to render the built-in server's row, or null when
 * this build never started one (a normal "thin" desktop build).
 */
export function getEmbeddedServerInfo(): EmbeddedServerInfo | null {
  const url = serverUrl || getStore().get('embeddedServerUrl')
  if (!url) return null
  return { url, password: embeddedServerPassword(), running: isEmbeddedServerActive() }
}

/** Path of the server binary shipped with this app. */
export function embeddedServerBinaryPath(): string {
  // The app root, not `resources/`: electron-builder's `extraFiles` puts the
  // server next to the executable precisely so a payload update (which replaces
  // `resources/`) can never remove it. Path arithmetic lives in the policy
  // module so it can be tested without Electron.
  return embeddedServerBinaryPathFor(process.execPath)
}

/**
 * Whether this install carries its own server (a fused/all-in-one package).
 *
 * Separate from {@link getEmbeddedServerInfo}: a server that never started still
 * makes this a fused install, and the difference matters to the upgrade path —
 * replacing it with a thin package would delete the only server it has.
 */
export function hasEmbeddedServerBinary(): boolean {
  return fs.existsSync(embeddedServerBinaryPath())
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
 * falls back to the credential the app saved for its own server.
 */
function embeddedServerPassword(): string {
  const fromFile = readTextQuietly(path.join(embeddedDataDir(), 'auto-password'))
  if (fromFile) return fromFile
  return getEmbeddedPassword()
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
  let exitCode: number | null = null
  while (Date.now() < deadline) {
    if (child && child.exitCode !== null) {
      exitCode = child.exitCode
      break
    }
    lastBody = await probeHealth(port)
    // "A ClawBench server answered" is not enough: an older instance on the
    // same port answers first and then exits, leaving the window pinned to an
    // address the new server may not use. Only OUR child counts as ready.
    if (isHealthFromChild(lastBody, child?.pid)) return
    await wait(EMBEDDED_HEALTH_INTERVAL_MS)
  }
  if (exitCode !== null) {
    // The server gave up on its own (port conflict, data-directory lock, a bad
    // config). Reporting a timeout here hid that reason and made every such
    // failure look identical — say what actually happened instead.
    throw new Error(`the embedded server exited before it became ready (code ${exitCode})`)
  }
  if (lastBody && isClawBenchHealth(lastBody) && !isHealthFromChild(lastBody, child?.pid)) {
    // Someone else's ClawBench server holds the port. Naming it is the only way
    // to tell this apart from a slow start.
    const pid = (lastBody as { pid?: number }).pid
    throw new Error(`port ${port} is answering as ClawBench from another process${pid ? ` (pid ${pid})` : ''}, not the one this app started`)
  }
  if (lastBody && !isClawBenchHealth(lastBody)) {
    throw new Error(`port ${port} answered, but it is not ClawBench`)
  }
  throw new Error(`the embedded server did not become ready within ${Math.round(EMBEDDED_STARTUP_TIMEOUT_MS / 1000)}s`)
}

/**
 * Move the server row older builds wrote for the built-in server into its own
 * store slot.
 *
 * Before the built-in server had a slot of its own it lived in `servers` like a
 * normal entry, which made it editable and deletable in the login page and — the
 * visible bug — let its own address reserve its own port, so every launch walked
 * to the next port and left the previous row behind as a dead entry.
 *
 * Only the address the previous run recorded is retired: a user's own saved
 * `http://127.0.0.1:20000` (a standalone install, a dev server) is a different
 * server and must survive.
 */
function adoptLegacyEmbeddedEntry(): void {
  const store = getStore()
  const legacyPort = Number(store.get(EMBEDDED_PORT_KEY)) || 0
  if (!legacyPort) return
  const legacyUrl = embeddedServerUrl(legacyPort)
  const servers = store.get('servers')
  const entry = servers.find((s) => sameServerUrl(s.url, legacyUrl))
  if (entry) {
    // Carry the credential over before the row disappears: the login page and
    // the SSH tunnel read it from the new slot from now on.
    const password = getRowPasswordFor(legacyUrl)
    if (password) saveEmbeddedPassword(password)
    store.set('servers', servers.filter((s) => s !== entry))
  }
  if (!store.get('embeddedServerUrl')) store.set('embeddedServerUrl', legacyUrl)
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
  const password = embeddedServerPassword()
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
    // The health probe above already proved the server is up, so a login that
    // never answers is a bug, not a slow start. Without a bound it would leave
    // the window on the loading ring forever.
    signal: AbortSignal.timeout(10_000),
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
  // the same slot). After a password change this refreshes it.
  saveEmbeddedPassword(password)
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

  // A server spawned by an EARLIER attempt in this process must not be left
  // running: it holds the data-directory lock (and possibly the port), so
  // spawning another one would only wait out the 30s lock window and fail. This
  // is the login page's retry path after a failed start — without this, every
  // retry added another server and none of them could work.
  if (child) stopEmbeddedServer()
  // `stopEmbeddedServer` sets this and never clears it; a fresh start is a fresh
  // lifecycle. Leaving it true makes isEmbeddedServerActive() report "not
  // running" for the rest of the session, which sends the login page down the
  // retry path above on every click.
  stopping = false

  fs.mkdirSync(embeddedDataDir(), { recursive: true })
  // Before anything reads the server list: retire the row older builds kept for
  // this server. It is what reserved its own port and pushed the app one port
  // further on every launch.
  adoptLegacyEmbeddedEntry()

  const store = getStore()
  const previousActive = store.get('serverUrl')
  // Read BEFORE the new address is written: this is the address the last run
  // used, which is what "the user chose the built-in server" must compare to.
  const previousEmbedded = store.get('embeddedServerUrl')
  const preferred = Number(store.get(EMBEDDED_PORT_KEY)) || EMBEDDED_DEFAULT_PORT
  // Only the user's own rows can collide with ours. The built-in server's own
  // address is never in this list (adoptLegacyEmbeddedEntry removed it), so its
  // preferred port stays usable — browser storage is keyed by origin, and a
  // moving port looks like a fresh install.
  const reserved = reservedPortsForEmbedded(store.get('servers').map((s) => s.url))

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
      embeddedActive = false
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

  try {
    await waitForHealth(port)
    await plantSessionCookie(port, session.defaultSession)
  } catch (err) {
    // Never leave the spawned server running after a failed start. It holds the
    // data-directory lock, so the next attempt (the login page's retry, or a
    // relaunch while this process lives on) would spawn a second server that can
    // only time out — the failure would stick until the whole app was killed.
    stopEmbeddedServer()
    throw err
  }

  getStore().set(EMBEDDED_PORT_KEY, port)
  getStore().set('embeddedServerUrl', serverUrl)
  // Adopt the built-in server only when it is what this launch should open:
  // nothing chosen yet, or the user was on it last time. A user who picked
  // another machine keeps that choice — otherwise "connect to my server" would
  // silently revert on every restart.
  if (shouldAdoptEmbeddedServer(previousActive, previousEmbedded)) {
    getStore().set('serverUrl', serverUrl)
  }
  return serverUrl
}

/**
 * Stop the embedded server: close its stdin (its quit signal), wait for a
 * graceful drain, then kill whatever is left.
 *
 * Synchronous where it can be, because it runs from `will-quit`, which
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
