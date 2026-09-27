import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { APP_USER_MODEL_ID } from './identity'

const yml = readFileSync(resolve(__dirname, '../../electron-builder.yml'), 'utf8')
const pkg = JSON.parse(readFileSync(resolve(__dirname, '../../package.json'), 'utf8')) as {
  desktopName?: string
}

/** Read a top-level `key: value` scalar out of electron-builder.yml. */
function ymlScalar(key: string): string | null {
  const m = yml.match(new RegExp(`^${key}:\\s*(\\S+)\\s*$`, 'm'))
  return m ? m[1] : null
}

/**
 * The Windows toast identity is declared in two places that cannot see each
 * other at runtime:
 *   - desktop/electron-builder.yml `appId` — baked into the Start Menu shortcut
 *     and the exe's version resource at build time
 *   - APP_USER_MODEL_ID — passed to app.setAppUserModelId() at startup
 *
 * electron-builder.yml is NOT shipped inside app.asar, so the app cannot read
 * it back; and Windows only shows a toast for an AUMID that resolves to an
 * installed shortcut. If the two drift, notifications silently stop appearing
 * on Windows with no error anywhere. Hence this guard.
 */
describe('Windows toast identity', () => {
  it('APP_USER_MODEL_ID matches the electron-builder appId', () => {
    const appId = ymlScalar('appId')
    expect(appId, 'electron-builder.yml must declare a top-level appId').not.toBeNull()

    expect(appId).toBe(APP_USER_MODEL_ID)
  })

  it('is a reverse-DNS style id (a bare word would collide across apps)', () => {
    // Windows uses the AUMID to attribute and group notifications; a generic id
    // like "ClawBench" is not unique and can collide with another install.
    expect(APP_USER_MODEL_ID).toMatch(/^[a-z0-9]+(\.[a-z0-9-]+)+$/)
    expect(APP_USER_MODEL_ID).toContain('.')
  })
})

/**
 * Linux desktop identity.
 *
 * GNOME associates a window with its `.desktop` entry through the window's
 * app_id / WM_CLASS. It tries, in order, the entry's `StartupWMClass`, then a
 * `.desktop` file whose NAME equals the window's app_id
 * (`shell-window-tracker.c` `get_app_from_window_wmclass`). When none of those
 * hit, the dock falls back to a generic placeholder icon — which is exactly
 * what happened here: the window reported the npm-name-derived
 * `xulongzhe-clawbench-desktop` (Electron slugs `@xulongzhe/clawbench-desktop`),
 * while the entry declared `StartupWMClass=ClawBench` and was named
 * `clawbench-desktop.desktop`. Nothing matched, so the desktop file's icon was
 * never used for the running window.
 *
 * `desktopName` in package.json is the lever: Electron's own docs require it to
 * "match the name of the app's actual `.desktop` file". Without it Electron
 * derives the app_id from `name`, which is a scoped npm name no `.desktop` file
 * can sensibly be called.
 *
 * These two values live in files that never see each other at runtime
 * (package.json ships inside app.asar; the `.desktop` file is installed by the
 * user/distro), so the invariant is asserted here rather than left to chance.
 */
describe('Linux desktop identity', () => {
  it('declares desktopName so the app_id is not the scoped npm name', () => {
    expect(
      pkg.desktopName,
      'desktop/package.json must set desktopName; otherwise the app_id is the ' +
        'slugged npm name (@xulongzhe/clawbench-desktop -> xulongzhe-clawbench-desktop)',
    ).toBeTruthy()
  })

  it('desktopName names a .desktop file', () => {
    expect(pkg.desktopName).toMatch(/\.desktop$/)
  })

  it('desktopName stem matches executableName, so the shipped entry and the window agree', () => {
    // The desktop entry is named after the executable (clawbench-desktop.desktop
    // launching clawbench-desktop), and Electron turns desktopName into the
    // window's app_id by stripping the .desktop suffix. Requiring the two to be
    // equal keeps the file we ship and the identity the window reports in sync.
    const exec = ymlScalar('executableName')
    expect(exec, 'electron-builder.yml must declare a top-level executableName').not.toBeNull()
    expect(pkg.desktopName, 'desktop/package.json must set desktopName').toBeTruthy()

    expect(pkg.desktopName!.replace(/\.desktop$/, '')).toBe(exec)
  })
})
