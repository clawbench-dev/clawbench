import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guards for the built-in ("all-in-one") server's UI and lifecycle.
 *
 * `localServer.ts`, `window.ts`, `bridge.ts` and the login page all depend on
 * Electron, a spawned child process or a rendered document, so their failure
 * modes are silent rather than throwable in a unit test. Each assertion below
 * pins a specific bug that shipped:
 *
 *  - The built-in server was a normal `servers` row: its own address reserved
 *    its own port, so every launch walked one port further (a new origin each
 *    time — theme, wallpaper and panel layout all looked reset), and the login
 *    page offered to rename/delete a server the app re-created anyway.
 *  - `serverUrl` was rewritten to the built-in address on every launch, so a
 *    user who connected to another machine was dragged back on the next start.
 *  - "Is the built-in server running" was used as "is this URL the built-in
 *    server", which skipped the version gate for every remote server too.
 *
 * Same convention as `splashWiring.test.ts`: assert the wiring in source, since
 * the modules themselves cannot be imported here.
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

const LOGIN = 'desktop/assets/login.html'
const LOCAL_SERVER = 'desktop/src/main/localServer.ts'
const WINDOW = 'desktop/src/main/window.ts'

describe('login page keeps the built-in server out of the saved list', () => {
  it('renders it in a section of its own', () => {
    const src = readRepoFile(LOGIN)
    expect(src).toContain('id="embeddedSection"')
    expect(src).toContain('id="embeddedContainer"')
    expect(src).toContain('this_device_section')
    // The two lists are separate DOM containers: one shared list is exactly how
    // the app's own server acquired edit/delete buttons.
    expect(src).toContain('id="serverListContainer"')
  })

  it('gives its row a status label instead of edit/delete actions', () => {
    const src = readRepoFile(LOGIN)
    const fn = src.match(/function serverRowHtml\(srv, isEmbedded\) \{[\s\S]*?\n  \}/)
    expect(fn, 'serverRowHtml not found').not.toBeNull()
    const body = fn![0]
    const embeddedBranch = body.slice(body.indexOf('if (isEmbedded) {'), body.indexOf('} else {'))
    expect(embeddedBranch).toContain('server-item-status')
    // No pencil / × inside the embedded branch: those would either do nothing
    // (the app re-creates the entry) or manufacture a second, dead entry.
    expect(embeddedBranch).not.toContain('editServer(')
    expect(embeddedBranch).not.toContain('deleteServer(')
  })

  it('loads it through its own bridge call, not getServerList', () => {
    const src = readRepoFile(LOGIN)
    expect(src).toContain('ClawBenchNative.getEmbeddedServer')
    // The saved-list renderer must only walk savedServers; embeddedServer is a
    // separate variable precisely so a rename cannot reach it.
    const render = src.match(/function renderServerList\(\) \{[\s\S]*?\n  \}/)
    expect(render, 'renderServerList not found').not.toBeNull()
    expect(render![0]).toContain('savedServers.forEach')
    expect(render![0]).toContain('embeddedServer')
  })

  it('defaults the selection to the server the app is connected to', () => {
    const src = readRepoFile(LOGIN)
    const render = src.match(/function renderServerList\(\) \{[\s\S]*?\n  \}/)!
    // "First row" is the built-in one in a fused build, so returning from
    // "reconfigure server" would preselect it and one stray click switches back.
    expect(render![0]).toContain('currentServerUrl')
    expect(render![0]).toContain('findServer(currentServerUrl)')
  })
})

describe('the built-in server is not a saved row', () => {
  it('reserves only the user\'s own server ports', () => {
    // Passing the built-in address here is THE port-walk bug: it reserves its
    // own port, the preferred port is skipped, and the app moves to a new origin
    // on every launch.
    const src = readRepoFile(LOCAL_SERVER)
    const reserved = src.match(/const reserved = reservedPortsForEmbedded\((.*)\)\n/)
    expect(reserved, 'reserved-port computation not found').not.toBeNull()
    expect(reserved![1]).toContain("store.get('servers')")
    expect(reserved![1]).not.toContain('embeddedServerUrl')
  })

  it('writes the adopted serverUrl through the remembered-choice decision', () => {
    const src = readRepoFile(LOCAL_SERVER)
    const fn = src.match(/export async function startEmbeddedServer\(\)[\s\S]*?\n\}/)
    expect(fn, 'startEmbeddedServer not found').not.toBeNull()
    expect(fn![0]).toContain('shouldAdoptEmbeddedServer(previousActive, previousEmbedded)')
    // Unconditional assignment is the regression: the built-in address must not
    // overwrite a server the user chose.
    expect(fn![0]).not.toMatch(/\n  getStore\(\)\.set\('serverUrl', serverUrl\)/)
  })

  it('migrates the legacy row into the dedicated slots', () => {
    const src = readRepoFile(LOCAL_SERVER)
    const fn = src.match(/function adoptLegacyEmbeddedEntry\(\)[\s\S]*?\n\}/)
    expect(fn, 'adoptLegacyEmbeddedEntry not found').not.toBeNull()
    expect(fn![0]).toContain("store.get('servers')")
    expect(fn![0]).toContain("store.set('embeddedServerUrl'")
    expect(fn![0]).toContain('saveEmbeddedPassword')
  })
})

describe('a failed start does not poison every later attempt', () => {
  it('stops the server a previous attempt left running, and clears the stopping flag', () => {
    // Retrying from the login page used to spawn a SECOND server while the first
    // one still held the data-directory lock: the new one waited out the 30s lock
    // window and failed too, so "cannot connect" stuck until the app was killed.
    const src = readRepoFile(LOCAL_SERVER)
    const fn = src.match(/export async function startEmbeddedServer\(\)[\s\S]*?\n\}/)
    expect(fn, 'startEmbeddedServer not found').not.toBeNull()
    expect(fn![0]).toContain('if (child) stopEmbeddedServer()')
    expect(fn![0]).toContain('stopping = false')
  })

  it('stops the spawned server when the start fails', () => {
    // Same leak from the other side: a timeout left the child running, and the
    // child is exactly what holds the lock.
    const src = readRepoFile(LOCAL_SERVER)
    const fn = src.match(/export async function startEmbeddedServer\(\)[\s\S]*?\n\}/)
    expect(fn, 'startEmbeddedServer not found').not.toBeNull()
    expect(fn![0]).toContain('await waitForHealth(port)')
    expect(fn![0]).toMatch(/catch \(err\) \{[\s\S]*?stopEmbeddedServer\(\)[\s\S]*?throw err/)
  })
})

describe('the server outlives an aborted quit', () => {
  it('stops it on will-quit, never on before-quit', () => {
    // `before-quit` runs even when the quit is then aborted (the renderer vetoes
    // the unload while a meeting is publishing and the user stays). Stopping the
    // server there left a live app with a dead backend — "cannot connect", and a
    // relaunch only revealed the same window because of the single-instance
    // lock.
    const src = readRepoFile('desktop/src/main/index.ts')
    const willQuit = src.match(/app\.on\('will-quit',[\s\S]*?\n\}\)/)
    expect(willQuit, 'will-quit handler not found').not.toBeNull()
    expect(willQuit![0]).toContain('stopEmbeddedServer()')

    const beforeQuit = src.match(/app\.on\('before-quit',[\s\S]*?\n\}\)/)
    if (beforeQuit) expect(beforeQuit[0]).not.toContain('stopEmbeddedServer()')
  })
})

describe('built-in detection keys on the address', () => {
  it('does not gate the version check on "a built-in server is running"', () => {
    const src = readRepoFile(WINDOW)
    const fn = src.match(/export function checkVersionGateFor\(url: string\)[\s\S]*?\n\}/)
    expect(fn, 'checkVersionGateFor not found').not.toBeNull()
    expect(fn![0]).toContain('isEmbeddedServerUrl(url)')
    // The old guard skipped every remote server whenever the fused build's own
    // server happened to be running — i.e. always.
    expect(fn![0]).not.toContain('isEmbeddedServerActive()')
  })

  it('explains a stopped built-in server only when that was the target', () => {
    const src = readRepoFile(WINDOW)
    const fn = src.match(/async function serveLoginOrExplain\(reason: string\)[\s\S]*?\n\}/)
    expect(fn, 'serveLoginOrExplain not found').not.toBeNull()
    expect(fn![0]).toContain('isEmbeddedServerUrl(target)')
    // And it must still reach the login page: the old branch returned after the
    // dialog, leaving the user with no way to pick another server.
    expect(fn![0]).toContain('loadFile(loginPagePath())')
  })
})

describe('a thin client does not start a server it does not ship', () => {
  it('returns before starting when the bundled binary is absent', () => {
    // The thin package (every download except the `-local-` one) has no
    // `server/clawbench.exe`. Without this guard the startup path called
    // startEmbeddedServer() anyway, whose first line throws "the server binary
    // is missing from this build" — so every launch of a thin client opened the
    // "ClawBench 自带的本机服务没能启动" error dialog before the window existed.
    const src = readRepoFile('desktop/src/main/index.ts')
    expect(src).toMatch(/import \{[^}]*hasEmbeddedServerBinary[^}]*\} from '\.\/localServer'/)

    const fn = src.match(/async function startEmbeddedServerIfPresent\(\)[\s\S]*?\n\}/)
    expect(fn, 'startEmbeddedServerIfPresent not found').not.toBeNull()
    const body = fn![0]
    const guardAt = body.indexOf('hasEmbeddedServerBinary()')
    const startAt = body.indexOf('startEmbeddedServer()')
    expect(guardAt, 'the guard must be the first thing the function does').toBeGreaterThan(-1)
    expect(guardAt).toBeLessThan(startAt)
    // Early return, not a fall-through into the failure dialog.
    expect(body.slice(guardAt, startAt)).toContain('return')
  })

  it('still starts the bundled server when the build ships one', () => {
    // The other direction of the same guard: a fused install must keep starting
    // its server, and a real failure there must still be reported.
    const src = readRepoFile('desktop/src/main/index.ts')
    const fn = src.match(/async function startEmbeddedServerIfPresent\(\)[\s\S]*?\n\}/)
    expect(fn, 'startEmbeddedServerIfPresent not found').not.toBeNull()
    expect(fn![0]).toContain('await startEmbeddedServer()')
    expect(fn![0]).toContain('showMessageBox')
  })
})

describe('the version gate cannot replace a fused install', () => {
  it('refuses the thin package when this client carries its own server', () => {
    // Every downloadable package is the thin one; installing it would drop
    // `server/` and the next launch would have no built-in server at all. The
    // gate now runs against remote servers too, so this guard is what keeps its
    // "download" button from wrecking a fused install.
    const src = readRepoFile('desktop/src/main/versionGate.ts')
    const fn = src.match(/export async function installServerVersion\([\s\S]*?\n\}/)
    expect(fn, 'installServerVersion not found').not.toBeNull()
    expect(fn![0]).toContain('hasEmbeddedServerBinary()')
    const guardAt = fn![0].indexOf('hasEmbeddedServerBinary()')
    const installAt = fn![0].indexOf('installPayload(')
    expect(guardAt, 'guard must run before any install attempt').toBeLessThan(installAt)
  })
})
