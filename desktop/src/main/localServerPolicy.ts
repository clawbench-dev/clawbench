import path from 'node:path'

/**
 * Pure decisions for the integrated desktop build (the "整合包": client and
 * server in one package).
 *
 * In this build the desktop app starts a ClawBench server of its own from inside
 * its install directory, logs in with the password that server generated, and
 * leaves it running when the app quits. Everything that is a DECISION lives here
 * so it can be tested without Electron, a spawned process or a network — the
 * same split as `splashPolicy.ts` / `loadFailure.ts`. The Electron-facing half is
 * `localServer.ts`.
 */

/** Where the server lives inside the app's install directory. */
export const EMBEDDED_SERVER_DIR = 'server'

/** Executable name of the bundled server on `platform`. */
export function embeddedServerExe(platform: NodeJS.Platform): string {
  return platform === 'win32' ? 'clawbench.exe' : 'clawbench'
}

/**
 * Where the embedded server binary lives for a given app executable.
 *
 * Windows/Linux: electron-builder's `extraFiles` puts it at `<install root>/server/`
 * (outside `resources/`, which a desktop payload update replaces wholesale).
 * macOS: it is a SIBLING of `ClawBench.app`, not inside it — adding a file to a
 * signed bundle invalidates the signature, so the integrated zip carries
 * `server/` next to the bundle and this walks up from `Contents/MacOS/` to the
 * folder that holds the `.app`.
 *
 * Kept here as a pure function so the path can be asserted without Electron.
 */
export function embeddedServerBinaryPath(
  execPath: string,
  platform: NodeJS.Platform = process.platform,
): string {
  // Use the target platform's separator rules, not the host's: the tests run on
  // Linux and still assert the Windows path.
  const p = platform === 'win32' ? path.win32 : path.posix
  const execDir = p.dirname(execPath)
  const contentDir = platform === 'darwin' ? p.resolve(execDir, '..', '..', '..') : execDir
  return p.join(contentDir, EMBEDDED_SERVER_DIR, embeddedServerExe(platform))
}

/**
 * Default port. Matches the standalone server's default so an embedded install
 * and a hand-run one are the same address when only one of them is up.
 */
export const EMBEDDED_DEFAULT_PORT = 20000

/**
 * How long the server may take to answer /api/health before the app gives up.
 *
 * First launch is the slow case: the server creates its SQLite schema, extracts
 * the user guide and probes installed agents before it starts listening. 60s is
 * generous on purpose — giving up early on a slow disk would turn a slow start
 * into a broken install.
 */
export const EMBEDDED_STARTUP_TIMEOUT_MS = 60_000

/** How often to probe /api/health while waiting. */
export const EMBEDDED_HEALTH_INTERVAL_MS = 250

/**
 * How long to wait for a graceful exit after closing the server's stdin before
 * killing it. The server's own worst-case drain is ~8s (finalize + ACP grace),
 * plus HTTP drain; killing earlier would leave a half-written last reply.
 */
export const EMBEDDED_EXIT_GRACE_MS = 15_000

/**
 * Ports an embedded server must never be started on.
 *
 * - Every saved server row: an embedded server bound to the same port would
 *   collide with the remote one whenever that machine is also local — and worse,
 *   the app's saved URL for it would then silently resolve to the embedded
 *   server (a different data set behind the same address). The built-in
 *   server's OWN address is never in this list (it is not a saved row), which is
 *   what keeps its port stable across launches.
 * - The hub's SFTP/frp SSH port pair is out of scope: those are server-side and
 *   the embedded server owns its own.
 */
export function reservedPortsForEmbedded(savedServerUrls: readonly string[]): number[] {
  const out = new Set<number>()
  for (const raw of savedServerUrls) {
    try {
      const u = new URL(raw)
      const port = u.port ? Number(u.port) : u.protocol === 'https:' ? 443 : u.protocol === 'http:' ? 80 : NaN
      if (Number.isInteger(port) && port > 0 && port <= 65535) out.add(port)
    } catch {
      // A malformed saved URL is reported by the login page; it cannot reserve
      // anything here.
    }
  }
  return [...out]
}

export interface ChooseEmbeddedPortInput {
  /** Port this data directory used last time, 0 when unknown. */
  preferred: number
  /** Ports already in use on this machine. */
  inUse: readonly number[]
  /** Ports that belong to saved remote servers (see reservedPortsForEmbedded). */
  reserved: readonly number[]
  /** Overrides the starting point; defaults to EMBEDDED_DEFAULT_PORT. */
  startAt?: number
}

/**
 * Pick the port for the embedded server.
 *
 * The preferred port comes first: the app's own browser storage (theme,
 * wallpaper, panel layout, the current project) is keyed by origin, so opening
 * on a different port looks like a fresh install. Only when it is taken or
 * reserved does the walk start, from the default upwards.
 *
 * A reserved port is skipped even when it is free: binding a port one of the
 * user's own saved servers uses would make the two indistinguishable on the next
 * launch.
 */
export function chooseEmbeddedPort(input: ChooseEmbeddedPortInput): number {
  const blocked = new Set([...input.inUse, ...input.reserved])
  if (isUsablePort(input.preferred, blocked) && !isPrivilegedPool(input.preferred)) return input.preferred
  const start = input.startAt ?? EMBEDDED_DEFAULT_PORT
  for (let p = start; p <= 65535; p++) {
    if (isUsablePort(p, blocked)) return p
  }
  // Nothing free above the default: scan below rather than fail outright. Ports
  // under 1024 are avoided (they need privilege on Unix, and the first block is
  // full of other services anyway).
  for (let p = 1024; p < start; p++) {
    if (isUsablePort(p, blocked)) return p
  }
  return 0
}

function isUsablePort(p: number, blocked: ReadonlySet<number>): boolean {
  return Number.isInteger(p) && p > 0 && p <= 65535 && !blocked.has(p)
}

// Ports under 1024 need privilege on Unix, so the fallback scan below never
// starts there. A PREFERRED port below 1024 is skipped too: it was recorded by
// an older build and re-using it would fail at bind time on macOS/Linux.
function isPrivilegedPool(p: number): boolean {
  return p < 1024
}

/** Build the URL the app loads and stores for the embedded server. */
export function embeddedServerUrl(port: number): string {
  return `http://127.0.0.1:${port}`
}

export interface EmbeddedArgsInput {
  port: number
  dataDir: string
  /**
   * Bind address. Defaults to `0.0.0.0`, i.e. reachable from other devices on
   * the LAN; the server keeps its generated password, so they still log in.
   */
  host?: string
}

/**
 * Command-line arguments for the embedded server.
 *
 * `--managed` stops the server from replacing its own binary or restarting
 * itself behind the app's back. There is deliberately no
 * `--exit-on-stdin-close`: the server now outlives the desktop app (it keeps
 * serving phones and other clients), so the shell does not hold a pipe whose
 * closure would stop it.
 */
export function embeddedServerArgs(input: EmbeddedArgsInput): string[] {
  return [
    '--port', String(input.port),
    '--data-dir', input.dataDir,
    '--host', input.host || '0.0.0.0',
    '--managed',
  ]
}

/**
 * Normalize a server URL for identity comparisons.
 *
 * A server row is identified by its origin: that is what the login page writes,
 * what the cookie jar keys on, and what makes two entries "the same server".
 * A trailing slash (or a host in another case) must not make the built-in
 * server look like a different machine.
 */
export function normalizeServerUrl(url: string): string {
  try {
    const u = new URL(url)
    return `${u.protocol}//${u.host}`
  } catch {
    return (url || '').trim().replace(/\/+$/, '')
  }
}

/** Whether two URLs name the same server (see {@link normalizeServerUrl}). */
export function sameServerUrl(a: string, b: string): boolean {
  if (!a || !b) return false
  return normalizeServerUrl(a) === normalizeServerUrl(b)
}

/**
 * Whether this launch should point the app at the embedded server.
 *
 * `previousActive` is the server the last run ended on (the persisted
 * `serverUrl`); `previousEmbedded` is the built-in address that run knew.
 *
 * A user who deliberately connected to another machine keeps that choice across
 * restarts — being dragged back to "This device" on every launch is exactly the
 * bug this encodes against. The built-in server is adopted only when nothing
 * has been chosen yet, or when it WAS the choice (which also carries the app
 * across a port change: the address it remembers is the previous run's).
 */
export function shouldAdoptEmbeddedServer(
  previousActive: string,
  previousEmbedded: string,
): boolean {
  if (!previousActive) return true
  return sameServerUrl(previousActive, previousEmbedded)
}

/** True for urls that point at this machine (the embedded server). */
export function isLocalServerUrl(url: string): boolean {
  try {
    const u = new URL(url)
    return (
      (u.protocol === 'http:' || u.protocol === 'https:') &&
      (u.hostname === '127.0.0.1' || u.hostname === 'localhost' || u.hostname === '[::1]' || u.hostname === '::1')
    )
  } catch {
    return false
  }
}

/** Health response fields the app depends on. */
export interface HealthBody {
  app?: string
  version?: string
  /** PID of the answering server. Absent on builds that predate the field. */
  pid?: number
}

/**
 * Whether a /api/health body is really a ClawBench server. A 200 alone is not
 * enough: the preferred port may be held by an unrelated service, and loading
 * its page would show a stranger's UI instead of ours.
 */
export function isClawBenchHealth(body: unknown): body is HealthBody & { app: 'clawbench' } {
  return !!body && typeof body === 'object' && (body as HealthBody).app === 'clawbench'
}

/**
 * Whether the health answer came from the server THIS process spawned.
 *
 * The identity check above is not enough on a machine where an older ClawBench
 * instance is still listening on the preferred port (a server left behind by a
 * crashed run, or a standalone install the user started by hand). That instance
 * answers `/api/health` first, so the shell would treat "some ClawBench server
 * is up" as "our server is ready", plant a cookie against it and point the
 * window at its address. When it then exits, the page is pinned to a port the
 * new server may not use — and the WebSocket reconnect loop can never succeed;
 * only logging out and back in (which reads the new address from the store)
 * recovers.
 *
 * `pid` is additive: a build that predates it omits the field, and we accept
 * that rather than refusing to start every older embedded server.
 */
export function isHealthFromChild(body: unknown, childPid: number | undefined): boolean {
  if (!isClawBenchHealth(body)) return false
  if (typeof body.pid !== 'number') return true
  return childPid !== undefined && body.pid === childPid
}

/**
 * The session cookie name for a server on `port`.
 *
 * Mirrors the server's `ScopedCookieName`: cookies are prefixed with the port
 * unless it is the default one. The app needs the exact name to plant the login
 * cookie itself instead of driving the login form.
 */
export function sessionCookieName(port: number): string {
  return port === EMBEDDED_DEFAULT_PORT ? 'clawbench_session' : `cb${port}_clawbench_session`
}

/**
 * Normalize a version string to `major.minor.patch`.
 *
 * The server reports the release it was built from (`v0.145.0`,
 * `v0.145.0-12-gabc1234`), while the desktop shell reads `0.145.0` from its
 * package.json. Comparing the raw strings would say "different" on every launch
 * and make the app kill and restart a perfectly good background server each
 * time.
 */
export function normalizeVersion(raw: string): string {
  const match = /^v?(\d+\.\d+\.\d+)/.exec((raw || '').trim())
  return match ? match[1] : ''
}

/**
 * Whether a running background server belongs to this desktop build.
 *
 * Unknown/unparseable versions answer `true`: the alternative is an endless
 * stop-and-start loop against a server whose version format we do not recognize.
 * A genuine mismatch (the app was upgraded while the old server kept running)
 * answers `false`, and the caller replaces the server.
 */
export function versionsMatch(serverVersion: string, appVersion: string): boolean {
  const a = normalizeVersion(serverVersion)
  const b = normalizeVersion(appVersion)
  if (!a || !b) return true
  // `desktop/package.json` keeps 0.1.0 as its placeholder in the repository;
  // a local build therefore reports 0.1.0 against a real server version.
  // Treating that as a mismatch would kill and restart the server on every
  // development launch.
  if (b === '0.1.0') return true
  return a === b
}
