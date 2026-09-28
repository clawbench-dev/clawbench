import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guards for the desktop splash overlay.
 *
 * `splash.ts`, `window.ts` and `bridge.ts` all import `electron` at module
 * scope, so no unit test can load them — and the overlay's whole failure mode is
 * silent: the preload's `dismissSplash` used to be an empty stub, which made the
 * app's existing calls a no-op with no error anywhere. These tests assert the
 * wiring exists in source, which is the only place a regression would show.
 *
 * This mirrors the repo's existing convention for electron-only modules (see
 * `theme.test.ts`, which parses the login pages it cannot import).
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

const PRELOAD = 'desktop/src/preload/index.ts'
const WINDOW = 'desktop/src/main/window.ts'
const BRIDGE = 'desktop/src/main/bridge.ts'
const LOGIN = 'desktop/assets/login.html'

describe('preload wires dismissSplash to a real channel', () => {
  it('sends native:dismiss-splash instead of being a no-op', () => {
    // THE regression this feature fixes: the stub meant the app's dismissSplash
    // calls did nothing, so nothing could ever take a native overlay down.
    const src = readRepoFile(PRELOAD)
    const m = src.match(/dismissSplash:\s*\(\)\s*=>\s*\{([^}]*)\}/)
    expect(m, 'dismissSplash must be declared as an arrow function').not.toBeNull()
    expect(m![1]).toContain("ipcRenderer.send('native:dismiss-splash')")
    expect(m![1], 'dismissSplash must not be a no-op stub').not.toMatch(/^\s*$/)
  })

  it('exposes cancelSplash for the overlay cancel button', () => {
    const src = readRepoFile(PRELOAD)
    expect(src).toMatch(/cancelSplash:\s*\(\)\s*=>\s*\{[^}]*ipcRenderer\.send\('native:splash-cancel'\)/)
  })
})

describe('bridge handles both splash channels', () => {
  it('shows the overlay before navigating on connect', () => {
    // Order matters: loadURL tears the login page down, so raising the overlay
    // afterwards would leave a blank frame.
    const src = readRepoFile(BRIDGE)
    const connect = src.match(/native:connect-to-server[\s\S]*?\n  \}\)/)
    expect(connect, 'connect-to-server handler not found').not.toBeNull()
    const body = connect![0]
    const showAt = body.indexOf('showSplashFor(url)')
    const loadAt = body.indexOf('w.loadURL(url)')
    expect(showAt, 'connect handler must call showSplashFor').toBeGreaterThan(-1)
    expect(loadAt, 'connect handler must still navigate').toBeGreaterThan(-1)
    expect(showAt, 'overlay must be raised before the navigation').toBeLessThan(loadAt)
  })

  it('registers handlers for dismiss and cancel', () => {
    const src = readRepoFile(BRIDGE)
    expect(src).toMatch(/ipcMain\.on\('native:dismiss-splash',\s*\(\)\s*=>\s*dismissSplash\(\)\)/)
    expect(src).toMatch(/ipcMain\.on\('native:splash-cancel',\s*\(\)\s*=>\s*cancelSplash\(\)\)/)
  })
})

describe('window wires the overlay into the main window', () => {
  it('creates the controller on the main window', () => {
    const src = readRepoFile(WINDOW)
    expect(src).toMatch(/splash\s*=\s*createSplashController\(mainWindow,/)
  })

  it('shows the overlay on a cold start with a saved server', () => {
    const src = readRepoFile(WINDOW)
    const coldStart = src.match(/const serverUrl = getStore\(\)\.get\('serverUrl'\)[\s\S]*?\n  \}(?=\n\n)/)
    expect(coldStart, 'cold-start branch not found').not.toBeNull()
    const body = coldStart![0]
    expect(body).toContain('showSplashFor(serverUrl)')
    expect(body.indexOf('showSplashFor(serverUrl)')).toBeLessThan(body.indexOf('mainWindow.loadURL(serverUrl)'))
  })

  it('does not show the overlay for the local first-run login page', () => {
    // The login page is a local file with nothing to wait for; an overlay there
    // would be a one-frame flash of the logo.
    const src = readRepoFile(WINDOW)
    const coldStart = src.match(/const serverUrl = getStore\(\)\.get\('serverUrl'\)[\s\S]*?\n  \}(?=\n\n)/)![0]
    const elseBranch = coldStart.slice(coldStart.indexOf('} else {'))
    expect(elseBranch).toContain('loadFile(loginPagePath())')
    expect(elseBranch, 'the login-page branch must not show the overlay').not.toContain('showSplashFor')
  })

  it('dismisses the overlay on the unreachable-server fallback', () => {
    const src = readRepoFile(WINDOW)
    const failLoad = src.match(/did-fail-load[\s\S]*?setTimeout\(showWindow, 2000\)/)
    expect(failLoad, 'did-fail-load handler not found').not.toBeNull()
    expect(failLoad![0]).toContain('dismissSplash()')
  })

  it('dismisses the overlay when navigating back to the login page', () => {
    const src = readRepoFile(WINDOW)
    const fn = src.match(/export function showLoginPage\(\)[\s\S]*?\n\}/)
    expect(fn, 'showLoginPage not found').not.toBeNull()
    expect(fn![0]).toContain('dismissSplash()')
  })

  it('tears the overlay down with the window', () => {
    const src = readRepoFile(WINDOW)
    const closed = src.match(/mainWindow\.on\('closed'[\s\S]*?\n  \}\)/)
    expect(closed, "the window's closed handler not found").not.toBeNull()
    expect(closed![0]).toContain('splash?.destroy()')
  })
})

describe('splash timers follow Android semantics', () => {
  it('cancels the connection deadline once the page has loaded', () => {
    // A page that loaded means the connection succeeded, so the deadline must
    // not fire during a slow app boot. Android pairs cancelConnectionTimeout()
    // with startSplashFailSafe() here; without the cancel, a boot past 90s
    // bounces the user back to login despite the server having answered.
    const src = readRepoFile('desktop/src/main/splash.ts')
    // Anchor on `win.` — the overlay's own webContents has a did-finish-load
    // handler too, and an unanchored pattern would pin the wrong one.
    const handler = src.match(/win\.webContents\.on\('did-finish-load'[\s\S]*?\n  \}\)/)
    expect(handler, 'did-finish-load handler not found').not.toBeNull()
    const body = handler![0]
    expect(body, 'must cancel the connection deadline on load').toContain('connectionTimer = clearTimer(connectionTimer)')
    expect(body, 'must still arm the boot fail-safe').toContain('armFailSafe()')
    expect(body.indexOf('clearTimer(connectionTimer)')).toBeLessThan(body.indexOf('armFailSafe()'))
  })

  it('uses the Android timeout values', () => {
    const src = readRepoFile('desktop/src/main/splashPolicy.ts')
    expect(src).toMatch(/SPLASH_FAILSAFE_MS = 15_000/)
    expect(src).toMatch(/CONNECTION_TIMEOUT_MS = 90_000/)
  })
})

describe('login page supports splash mode', () => {
  it('detects the ?splash=1 query flag', () => {
    const src = readRepoFile(LOGIN)
    expect(src).toMatch(/SPLASH_MODE\s*=\s*\/.*splash=1/)
    expect(src).toContain("document.body.classList.add('splash-mode')")
  })

  it('exposes the stage and fade hooks the main process calls', () => {
    // The main process drives these through executeJavaScript; a rename on
    // either side would silently freeze the stage text.
    const src = readRepoFile(LOGIN)
    expect(src).toContain('window.__splashSetStage = function')
    expect(src).toContain('window.__splashFadeOut = function')
  })

  it('maps every stage to an i18n key, with both languages present', () => {
    const src = readRepoFile(LOGIN)
    const stages = ['connecting', 'loading', 'rendering', 'initializing']
    for (const stage of stages) {
      expect(src, `stage ${stage} must be mapped`).toContain(`${stage}: 'splash_${stage}'`)
      // One key per language: a missing translation renders the bare key.
      const occurrences = src.split(`splash_${stage}:`).length - 1
      expect(occurrences, `splash_${stage} needs an en and a zh string`).toBe(2)
    }
    expect(src.split('splash_cancel:').length - 1).toBe(2)
  })

  it('resets the fade state on every show, not just the first', () => {
    // The overlay page is REUSED across connects, so the `is-fading` class the
    // previous dismissal added would otherwise make the second connect render
    // fully transparent (measured: opacity stayed 0 on re-show).
    const src = readRepoFile(LOGIN)
    expect(src).toContain('window.__splashReset = function')
    const resetFn = src.match(/window\.__splashReset = function[\s\S]*?\n  \};/)
    expect(resetFn, 'reset hook not found').not.toBeNull()
    expect(resetFn![0]).toContain("classList.remove('is-fading')")
  })

  it('calls the reset hook from the main process on show', () => {
    // Declaring the hook is not enough; a show path that skips it still leaves
    // the overlay invisible on the second connect.
    const src = readRepoFile('desktop/src/main/splash.ts')
    expect(src).toContain('window.__splashReset && window.__splashReset()')
  })

  it('wires the cancel button to the native bridge', () => {
    const src = readRepoFile(LOGIN)
    const handler = src.match(/getElementById\('splashCancel'\)[\s\S]*?\}\)/)
    expect(handler, 'splash cancel handler not found').not.toBeNull()
    expect(handler![0]).toContain('ClawBenchNative.cancelSplash()')
  })

  it('does not load the login form in splash mode', () => {
    // Splash mode is display-only; running the login page's data loading would
    // issue needless requests and could repaint the form over the overlay.
    const src = readRepoFile(LOGIN)
    const domReady = src.match(/window\.addEventListener\('DOMContentLoaded'[\s\S]*?\n  \}\);/)
    expect(domReady, 'DOMContentLoaded handler not found').not.toBeNull()
    const body = domReady![0]
    const splashBranch = body.slice(body.indexOf('if (SPLASH_MODE) {'))
    expect(splashBranch).toContain('return;')
    // The branch must return before the form's loading runs, so the overlay
    // never issues login requests.
    expect(splashBranch.indexOf('return;')).toBeLessThan(splashBranch.indexOf('loadServerList()'))
  })
})

/**
 * Guards for the version-mismatch gate.
 *
 * The gate reuses the splash overlay's view but must NOT be dismissible by the
 * app's normal `dismissSplash()` call (the app finishes booting behind the
 * gate), and it must not be hidden by the boot fail-safe. Both are silent
 * failures — the overlay would just vanish — so they are pinned at the source.
 */
describe('splash controller supports the version gate', () => {
  it('defers dismiss while the gate is up, and records the request', () => {
    const src = readRepoFile('desktop/src/main/splash.ts')
    const fn = src.match(/function dismiss\(\): void \{[\s\S]*?\n  \}/)
    expect(fn, 'dismiss not found').not.toBeNull()
    const body = fn![0]
    // The app's dismissSplash() must not reveal the app under the gate.
    expect(body, 'dismiss must return early while gating').toMatch(/if \(mode === 'gate'\) return/)
    // But the request is still recorded, so the overlay does not reappear later.
    expect(body, 'dismiss must record the request').toContain('dismissRequested = true')
    expect(body.indexOf('dismissRequested = true')).toBeLessThan(body.indexOf("if (mode === 'gate') return"))
  })

  it('never arms the fail-safe while gating', () => {
    const src = readRepoFile('desktop/src/main/splash.ts')
    const fn = src.match(/function armFailSafe\(\): void \{[\s\S]*?\n  \}/)
    expect(fn, 'armFailSafe not found').not.toBeNull()
    expect(fn![0], 'fail-safe must be mode-guarded').toMatch(/if \(mode !== 'splash'\) return/)
    // And the timer body re-checks, so a timer armed just before the gate cannot
    // still fire and reveal the app.
    expect(fn![0]).toContain("if (mode !== 'splash' || !visible || !isAlive()) return")
  })

  it('cancels pending timers when raising the gate', () => {
    // A 200ms fade left by the app's dismiss would otherwise hide the gate right
    // after it appears.
    const src = readRepoFile('desktop/src/main/splash.ts')
    const fn = src.match(/function showVersionMismatch\(info: GateOverlayInfo\): void \{[\s\S]*?\n  \}/)
    expect(fn, 'showVersionMismatch not found').not.toBeNull()
    const body = fn![0]
    expect(body, 'must cancel pending timers').toContain('cancelTimers()')
    expect(body, 'must switch to gate mode').toContain("mode = 'gate'")
    expect(body.indexOf('cancelTimers()')).toBeLessThan(body.indexOf("mode = 'gate'"))
    // Already gating: keep the first gate (a reconnect must not restart it).
    expect(body).toMatch(/if \(mode === 'gate'\) return/)
  })

  it('never lets the loading splash clobber the gate', () => {
    const src = readRepoFile('desktop/src/main/splash.ts')
    const show = src.match(/show\(url: string\): boolean \{[\s\S]*?\n    \},/)
    expect(show, 'show() not found').not.toBeNull()
    expect(show![0], 'show must refuse to cover the gate').toMatch(/if \(mode === 'gate'\) return false/)
  })

  it('replays a gate that arrived before the overlay page was ready', () => {
    const src = readRepoFile('desktop/src/main/splash.ts')
    const handler = src.match(/view\.webContents\.on\('did-finish-load'[\s\S]*?\n  \}\)/)
    expect(handler, 'overlay did-finish-load not found').not.toBeNull()
    const body = handler![0]
    expect(body, 'must replay the pending gate').toContain('pendingMismatch')
    expect(body, 'must apply the mismatch on load').toContain('applyMismatch(info)')
  })

  it('re-enables cancel only for the loading splash, not the gate', () => {
    const src = readRepoFile('desktop/src/main/splash.ts')
    const cancel = src.match(/cancel\(\): void \{[\s\S]*?\n    \},/)
    expect(cancel, 'cancel() not found').not.toBeNull()
    expect(cancel![0], 'cancel must be ignored while gating').toMatch(/if \(mode === 'gate'\) return/)
  })
})

describe('version gate is wired end to end', () => {
  it('checks on cold start and on connect', () => {
    const windowSrc = readRepoFile('desktop/src/main/window.ts')
    // Cold start: after the overlay is raised and the navigation starts.
    const cold = windowSrc.match(/const serverUrl = getStore\(\)\.get\('serverUrl'\)[\s\S]*?\n  \}(?=\n\n)/)
    expect(cold, 'cold-start branch not found').not.toBeNull()
    expect(cold![0]).toContain('checkVersionGateFor(serverUrl)')

    const bridgeSrc = readRepoFile('desktop/src/main/bridge.ts')
    const connect = bridgeSrc.match(/native:connect-to-server[\s\S]*?\n  \}\)/)
    expect(connect, 'connect-to-server handler not found').not.toBeNull()
    expect(connect![0], 'connect must check the version').toContain('checkVersionGateFor(url)')
  })

  it('registers both gate IPC channels', () => {
    const src = readRepoFile('desktop/src/main/bridge.ts')
    expect(src).toMatch(/ipcMain\.on\('native:version-continue',\s*\(\)\s*=>\s*continueVersionGate\(\)\)/)
    expect(src).toMatch(/ipcMain\.on\('native:version-download',\s*\(\)\s*=>\s*\{\s*void downloadVersionFromGate\(\)\s*\}\)/)
  })

  it('exposes both gate actions to the overlay page', () => {
    const src = readRepoFile('desktop/src/preload/index.ts')
    expect(src).toMatch(/versionContinue:\s*\(\)\s*=>\s*\{\s*ipcRenderer\.send\('native:version-continue'\)/)
    expect(src).toMatch(/versionDownload:\s*\(\)\s*=>\s*\{\s*ipcRenderer\.send\('native:version-download'\)/)
  })

  it('removes the old OS-dialog upgrade path', () => {
    // Otherwise the OS dialog and the overlay would both prompt.
    const src = readRepoFile('desktop/src/main/index.ts')
    expect(src, 'promptAndInstallUpdate must be gone').not.toContain('promptAndInstallUpdate')
    expect(src, 'the 3s upgrade timer must be gone').not.toContain('checkForUpdate')
  })

  it('abandons the gate when the connection fails or the login page is shown', () => {
    // A gate for an abandoned navigation must not cover the login page.
    const src = readRepoFile('desktop/src/main/window.ts')
    const failLoad = src.match(/did-fail-load[\s\S]*?setTimeout\(showWindow, 2000\)/)
    expect(failLoad, 'did-fail-load handler not found').not.toBeNull()
    expect(failLoad![0], 'unreachable server must abort the gate').toContain('abortVersionGate()')
    const loginPage = src.match(/export function showLoginPage\(\)[\s\S]*?\n\}/)
    expect(loginPage, 'showLoginPage not found').not.toBeNull()
    expect(loginPage![0], 'login page must abort the gate').toContain('abortVersionGate()')
  })
})

describe('login page renders the version gate', () => {
  it('exposes the show/reset hooks the main process calls', () => {
    const src = readRepoFile(LOGIN)
    expect(src).toContain('window.__versionMismatchShow = function')
    expect(src).toContain('window.__versionMismatchReset = function')
    expect(src).toContain("classList.add('gate-mode')")
    expect(src).toContain("classList.remove('gate-mode')")
  })

  it('words the direction rather than assuming the app is older', () => {
    // A newer client downloads an OLDER server build; calling that "update"
    // would be wrong.
    const src = readRepoFile(LOGIN)
    expect(src).toContain('version_mismatch_newer')
    expect(src).toContain('version_mismatch_older')
    const show = src.match(/window\.__versionMismatchShow = function[\s\S]*?\n  \};/)
    expect(show, '__versionMismatchShow not found').not.toBeNull()
    expect(show![0]).toMatch(/info\.direction === 'newer'/)
  })

  it('wires both gate buttons to the native bridge', () => {
    const src = readRepoFile(LOGIN)
    const cont = src.match(/getElementById\('gateContinue'\)[\s\S]*?\}\)/)
    expect(cont, 'continue handler not found').not.toBeNull()
    expect(cont![0]).toContain('ClawBenchNative.versionContinue()')
    const dl = src.match(/getElementById\('gateDownload'\)[\s\S]*?\}\)/)
    expect(dl, 'download handler not found').not.toBeNull()
    expect(dl![0]).toContain('ClawBenchNative.versionDownload()')
  })

  it('renders the gate above the splash it replaces', () => {
    // Both can be display:flex at once (the page is reused), so the gate must
    // win the paint order or it is invisible behind the splash.
    const src = readRepoFile(LOGIN)
    const splashZ = src.match(/\.splash \{[\s\S]*?z-index:\s*(\d+)/)
    const gateZ = src.match(/body\.gate-mode \.gate \{[\s\S]*?z-index:\s*(\d+)/)
    expect(splashZ, 'splash z-index not found').not.toBeNull()
    expect(gateZ, 'gate z-index not found').not.toBeNull()
    expect(Number(gateZ![1])).toBeGreaterThan(Number(splashZ![1]))
  })

  it('has both languages for every gate string', () => {
    const src = readRepoFile(LOGIN)
    const keys = [
      'version_mismatch_title',
      'version_mismatch_body',
      'version_mismatch_older',
      'version_mismatch_newer',
      'version_mismatch_download',
      'version_mismatch_continue',
    ]
    for (const key of keys) {
      // One key per language: a missing translation renders the bare key.
      const occurrences = src.split(`${key}:`).length - 1
      expect(occurrences, `${key} needs an en and a zh string`).toBe(2)
    }
  })
})
