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
  versionsMatch,
  type HealthBody,
} from './localServerPolicy'

/**
 * The embedded server of an integrated package (客户端和服务端装在一起).
 *
 * Lifecycle, in order:
 *
 *  1. If a server from a previous run is still alive, ADOPT it: probe its
 *     health, confirm it is the process named by our `server.pid`, plant the
 *     session cookie and reuse its address. This is what makes "quit the
 *     desktop, keep the server running" work — the next launch must not spawn a
 *     second server that can only fail on the data-directory lock.
 *  2. Otherwise pick a port (the last one if free — browser storage is keyed by
 *     origin) and spawn `server/clawbench[.exe]` DETACHED, with its output going
 *     to `desktop-server.log`. The child is unreferenced, so it survives the
 *     shell exiting or crashing.
 *  3. Poll /api/health until it identifies itself as ClawBench.
 *  4. Log in silently with the password the server generated for itself and
 *     plant the session cookie, so the app never shows the admin login form.
 *  5. Remember the URL, then let the normal window/navigation take over.
 *
 * Quitting the shell does NOT stop the server. Stopping it is an explicit
 * action (close dialog, tray menu, login page), because the whole point is that
 * phones and other clients keep working after the desktop is closed.
 *
 * Only one server per data directory is allowed (the server locks it), which is
 * why a stale server from a crashed run must be replaced before spawning: the
 * new one would wait out the 30s lock window and fail. Killing it is safe here
 * and only here — the data directory belongs to the app package, and no other
 * ClawBench install in its right mind points at it.
 */

/** Store key: the port the embedded server used last time. */
export const EMBEDDED_PORT_KEY = 'embeddedServerPort'

/** File the server appends its own log to while running in the background. */
const SERVER_LOG = 'desktop-server.log'

let child: ChildProcess | null = null
/**
 * PID of the server this app manages. Set for a server we spawned AND for one
 * adopted from a previous run (where `child` is null — there is no handle to a
 * process started by an older shell).
 */
let managedPid = 0
let serverUrl = ''
let stopping = false
let embeddedActive = false
/** Cached `clawbench --version` of the bundled binary ('' when unreadable). */
let bundledVersion: string | null = null

/**
 * Whether this session runs against the embedded server (spawned or adopted).
 *
 * Set by index.ts after the server answered, and read by the failure path (a
 * dead built-in server gets its own explanation) and the login page (running /
 * not running). Deliberately does NOT require `child`: an adopted background
 * server has no child handle but is very much running.
 *
 * NOT the way to ask "is this URL ours" — see {@link isEmbeddedServerUrl}: a
 * integrated build keeps this server running while the user is on another
 * machine.
 */
export function isEmbeddedServerActive(): boolean {
  return embeddedActive && !stopping
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
 * isEmbeddedServerActive}: an integrated build keeps its own server running
 * while the user is connected to another machine, and "we run a local server"
 * must not make that other server look like ours (the version gate, the failure
 * dialog and the login page all key off this).
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
  // The app's content directory, not `resources/`: electron-builder's
  // `extraFiles` puts the server there precisely so a payload update (which
  // replaces `resources/`) can never remove it. Path arithmetic lives in the
  // policy module so it can be tested without Electron.
  return embeddedServerBinaryPathFor(process.execPath, process.platform)
}

/**
 * Whether this install carries its own server (an integrated package).
 *
 * Separate from {@link getEmbeddedServerInfo}: a server that never started still
 * makes this an integrated install, and the difference matters to the upgrade
 * path — replacing it with a thin package would delete the only server it has.
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
      return out.toLowerCase().includes('clawbench.exe')
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

/** The port a stored server URL points at, with the usual defaults. */
function portOf(url: string): number {
  try {
    const u = new URL(url)
    if (u.port) return Number(u.port)
    return u.protocol === 'https:' ? 443 : 80
  } catch {
    return EMBEDDED_DEFAULT_PORT
  }
}

/**
 * Ports worth probing for a server left running by a previous launch.
 *
 * The recorded address is first (that is where it was last seen); the preferred
 * port and the default cover a store that lost the address but not the data
 * directory.
 */
function candidatePorts(): number[] {
  const out: number[] = []
  const stored = getStore().get('embeddedServerUrl')
  if (stored) out.push(portOf(stored))
  const preferred = Number(getStore().get(EMBEDDED_PORT_KEY)) || EMBEDDED_DEFAULT_PORT
  out.push(preferred, EMBEDDED_DEFAULT_PORT)
  return [...new Set(out)]
}

/**
 * Version of the server binary THIS package ships.
 *
 * The desktop shell's own `app.getVersion()` is not a reliable stand-in: a
 * release may reuse the previous shell (unchanged client protocol) while
 * shipping a new server, so the shell can legitimately report an older version
 * than the binary next to it. Asking the binary is the only way to know which
 * backend this launch is supposed to run.
 */
function bundledServerVersion(binary: string): string {
  if (bundledVersion === null) {
    try {
      bundledVersion = execFileSync(binary, ['--version'], { encoding: 'utf8', timeout: 5000 }).trim()
    } catch (e) {
      record('W', 'LocalServer', `cannot read the bundled server version: ${(e as Error).message}`)
      bundledVersion = ''
    }
  }
  return bundledVersion
}

/**
 * Take over the server a previous launch left running, or return null.
 *
 * The server now outlives the desktop shell, so this is the normal path on every
 * launch after the first. Ownership is proven by the PID file naming the process
 * that answered `/api/health`; a server we cannot identify is left alone (the
 * port walk handles it) rather than adopted or killed.
 *
 * A version mismatch means the app was upgraded while the old server kept
 * running. The old server is replaced: serving an older backend under a newer
 * shell is exactly the inconsistency the version gate exists to prevent.
 */
async function adoptRunningServer(binary: string): Promise<string | null> {
  const pid = previousServerPid()
  if (!pid || !isClawBenchProcess(pid)) return null
  const expected = bundledServerVersion(binary)
  for (const port of candidatePorts()) {
    const body = await probeHealth(port)
    if (!isClawBenchHealth(body)) continue
    const answeredPid = (body as HealthBody).pid
    if (typeof answeredPid === 'number' && answeredPid !== pid) continue
    const runningVersion = (body as HealthBody).version || ''
    if (expected && !versionsMatch(runningVersion, expected)) {
      record('W', 'LocalServer', `running server ${pid} is ${runningVersion}; this package ships ${expected}, replacing it`)
      await terminateServer(pid)
      return null
    }
    managedPid = pid
    serverUrl = embeddedServerUrl(port)
    record('I', 'LocalServer', `adopted the running server at ${serverUrl} (pid ${pid})`)
    return serverUrl
  }
  return null
}

/**
 * Stop a ClawBench server by PID: graceful where the platform has a catchable
 * signal, forced on Windows (a console child cannot receive one), then SIGKILL
 * if it overstays the grace period.
 *
 * The caller has already checked that the PID belongs to ClawBench; killing by
 * image name instead would take down a standalone install the user runs
 * elsewhere.
 */
async function terminateServer(pid: number): Promise<boolean> {
  record('I', 'LocalServer', `stopping the built-in server (pid ${pid})`)
  try {
    if (process.platform === 'win32') {
      execFileSync('taskkill', ['/F', '/T', '/PID', String(pid)], { stdio: 'ignore' })
    } else {
      process.kill(pid, 'SIGTERM')
    }
  } catch (e) {
    record('I', 'LocalServer', `stop: ${(e as Error).message}`)
  }
  const deadline = Date.now() + EMBEDDED_EXIT_GRACE_MS
  let killed = false
  while (Date.now() < deadline) {
    if (!isProcessAlive(pid)) return true
    if (!killed && Date.now() > deadline - EMBEDDED_EXIT_GRACE_MS / 3) {
      killed = true
      if (process.platform !== 'win32') {
        try { process.kill(pid, 'SIGKILL') } catch { /* gone */ }
      }
    }
    await wait(250)
  }
  return !isProcessAlive(pid)
}

/**
 * Stop a server this app left behind (a crash or a force-kill skips the graceful
 * path, and the next launch would otherwise fail on the data directory lock).
 */
async function reapPreviousServer(pid: number): Promise<boolean> {
  record('W', 'LocalServer', `reaping a server left behind by a previous run (pid ${pid})`)
  return terminateServer(pid)
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

/** Remember the address this launch uses, without overriding a remote choice. */
function rememberServer(port: number): void {
  const store = getStore()
  // Read BEFORE the new address is written: this is the address the last run
  // used, which is what "the user chose the built-in server" must compare to.
  const previousActive = store.get('serverUrl')
  const previousEmbedded = store.get('embeddedServerUrl')
  store.set(EMBEDDED_PORT_KEY, port)
  store.set('embeddedServerUrl', serverUrl)
  // Adopt the built-in server only when it is what this launch should open:
  // nothing chosen yet, or the user was on it last time. A user who picked
  // another machine keeps that choice — otherwise "connect to my server" would
  // silently revert on every restart.
  if (shouldAdoptEmbeddedServer(previousActive, previousEmbedded)) {
    store.set('serverUrl', serverUrl)
  }
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
  if (child || managedPid) await stopEmbeddedServer()
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

  // A server left running by the previous launch is the normal case now. Reuse
  // it before touching the port or the lock.
  const adopted = await adoptRunningServer(binary)
  if (adopted) {
    const port = portOf(adopted)
    await plantSessionCookie(port, session.defaultSession)
    rememberServer(port)
    return adopted
  }

  const store = getStore()
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

  // The child is DETACHED and unreferenced: the server is meant to outlive this
  // shell so phones and browsers keep working after the desktop is closed. Its
  // output goes to a file because there is no pipe to read any more.
  let logFd: number | 'ignore' = 'ignore'
  try {
    logFd = fs.openSync(path.join(embeddedDataDir(), SERVER_LOG), 'a')
  } catch (e) {
    record('W', 'LocalServer', `cannot open ${SERVER_LOG}: ${(e as Error).message}`)
  }
  const spawned = spawn(binary, args, {
    stdio: ['ignore', logFd, logFd],
    windowsHide: true,
    cwd: embeddedDataDir(),
    detached: true,
  })
  child = spawned
  managedPid = spawned.pid ?? 0
  spawned.unref()
  // The child owns the descriptor now; keeping the parent's copy open would
  // hold the log file (and delay rotation/cleanup) for the shell's lifetime.
  if (typeof logFd === 'number') {
    try { fs.closeSync(logFd) } catch { /* already closed */ }
  }
  spawned.once('exit', (code, signal) => {
    record('W', 'LocalServer', `server exited code=${code} signal=${signal}`)
    if (!stopping) {
      // It went away on its own: the app has nothing to talk to, and silently
      // reloading would loop. Surface it the way a failed connect is surfaced.
      serverUrl = ''
      managedPid = 0
      embeddedActive = false
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
    await stopEmbeddedServer()
    throw err
  }

  rememberServer(port)
  return serverUrl
}

/**
 * Stop the embedded server: graceful signal (SIGTERM / taskkill), wait for the
 * drain, then force it if it overstays. Also works for a server adopted from a
 * previous launch, where there is no child handle.
 *
 * Quitting the shell does NOT call this — see the module comment. It backs the
 * three explicit stop actions (close dialog, tray menu, login page).
 */
export async function stopEmbeddedServer(): Promise<void> {
  const proc = child
  const pid = managedPid || proc?.pid || previousServerPid()
  stopping = true
  child = null
  managedPid = 0
  serverUrl = ''
  embeddedActive = false
  if (!pid) return
  if (proc && proc.exitCode !== null) return
  if (!isProcessAlive(pid)) return
  // A spawned child is ours by construction; an adopted PID must still prove it
  // is ClawBench before we signal it.
  if (!proc && !isClawBenchProcess(pid)) {
    record('W', 'LocalServer', `refusing to stop pid ${pid}: not a ClawBench process`)
    return
  }
  await terminateServer(pid)
}
