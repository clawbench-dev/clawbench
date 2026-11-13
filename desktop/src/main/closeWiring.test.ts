import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guards for the close-behaviour flow.
 *
 * `closeFlow.ts` and `window.ts` import Electron at module scope, so the parts
 * that can only go wrong at runtime are asserted in source — the same convention
 * as `floatingWiring.test.ts` / `splashWiring.test.ts`. The failure modes are
 * all silent: an un-intercepted `close` event quits instead of asking, a
 * preload/bridge name mismatch makes the settings row a dead select, and a tray
 * destroyed while "minimize to tray" is selected leaves the app unreachable.
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

const WINDOW = 'desktop/src/main/window.ts'
const FLOATING = 'desktop/src/main/floating.ts'
const STORE = 'desktop/src/main/store.ts'
const BRIDGE = 'desktop/src/main/bridge.ts'
const PRELOAD = 'desktop/src/preload/index.ts'
const NATIVE_CONTRACT = 'web/src/utils/clawbenchNative.ts'
const SETTINGS_MAP = 'web/src/components/settings/settingsFieldMap.ts'
const SETTINGS_CONFIG = 'web/src/composables/useSettingsConfig.ts'

describe('every close goes through the flow', () => {
  it('the window intercepts its own close event', () => {
    // The header button only reaches the main process over IPC; Alt+F4, the
    // taskbar's "close" and the window manager's X fire this event directly.
    // Without the interception those paths quit outright while the button asks.
    const src = readRepoFile(WINDOW)
    const fn = src.match(/function registerCloseInterception\(win: BrowserWindow\): void \{([\s\S]*?)\n\}/)
    expect(fn, 'registerCloseInterception not found').not.toBeNull()
    expect(fn![1], 'must listen to the window close event').toContain("win.on('close'")
    expect(fn![1], 'must prevent the default close').toContain('event.preventDefault()')
    expect(fn![1], 'must hand the request to the flow').toContain('handleCloseRequest(win)')
    // A real quit re-enters this handler; it must step aside or `app.quit()`
    // would be turned into a dialog and never complete.
    expect(fn![1], 'a quit in progress must not be intercepted').toContain('isAppQuitting()')
    // macOS keeps its native frame and its red button already means "close the
    // window, keep the app" — asking there would be a second, contradictory UX.
    expect(fn![1], 'macOS must be left alone').toContain("'darwin'")
    // Windows ends a session by closing every window; that is not a user close.
    expect(fn![1], 'an OS shutdown must not raise the dialog').toContain("'query-session-end'")
    expect(src, 'the handler must be registered on the main window').toContain('registerCloseInterception(mainWindow)')
  })

  it('a window that refuses to unload also cancels the pending quit', () => {
    // Meeting guard: the user picked "quit", the renderer vetoed, the user chose
    // to stay. Leaving the flag set would make the NEXT close quit silently.
    const src = readRepoFile(WINDOW)
    const guard = src.match(/function registerUnloadGuard\(win: BrowserWindow\): void \{([\s\S]*?)\n\}/)
    expect(guard, 'registerUnloadGuard not found').not.toBeNull()
    expect(guard![1], 'must clear the quit state when the user stays').toContain('cancelPendingQuit()')
    // …and only on the stay branch: the leave branch unloads for real.
    expect(guard![1].indexOf('cancelPendingQuit')).toBeGreaterThan(guard![1].indexOf('choice === 0'))
  })

  it('the stored default is "ask"', () => {
    expect(readRepoFile(STORE)).toContain("closeAction: 'ask'")
  })
})

describe('the tray exists for "minimize to tray" alone', () => {
  it('the tray is wanted by either feature', () => {
    const src = readRepoFile(FLOATING)
    const fn = src.match(/function trayWanted\(\): boolean \{([\s\S]*?)\n\}/)
    expect(fn, 'trayWanted not found').not.toBeNull()
    expect(fn![1], 'the capsule feature still owns a tray').toContain('isEnabled()')
    expect(fn![1], 'the close behaviour needs one too').toContain('closeActionNeedsTray()')
  })

  it('turning the capsule off does not remove a tray the close behaviour needs', () => {
    // It used to destroy the tray unconditionally, which would strand a user
    // whose close behaviour is "minimize to tray" the moment they turned the
    // capsule off.
    const src = readRepoFile(FLOATING)
    const enable = src.match(/export function applyFloatingEnabled[\s\S]*?\n\}/)
    expect(enable, 'applyFloatingEnabled not found').not.toBeNull()
    expect(enable![0]).toContain('if (!trayWanted()) destroyTray()')
  })

  it('startup creates the icon before the first close can need it', () => {
    const src = readRepoFile(FLOATING)
    const init = src.match(/export function initFloating\(\): void \{([\s\S]*?)\n\}/)
    expect(init, 'initFloating not found').not.toBeNull()
    expect(init![1], 'close-to-tray must have an icon from the start').toContain('closeActionNeedsTray()')
  })

  it('the flow can create and probe the icon', () => {
    const src = readRepoFile(FLOATING)
    expect(src).toContain('export function ensureAppTray')
    expect(src).toContain('export function hasAppTray')
    expect(src).toContain('export function syncTrayForCloseAction')
    expect(src).toContain('export function notifyCloseActionChanged')
  })
})

describe('the settings row is wired end to end', () => {
  it('the preload implements the names the web app calls', () => {
    const preload = readRepoFile(PRELOAD)
    expect(preload).toMatch(/getCloseAction:\s*\(\)\s*=>/)
    expect(preload).toContain("ipcRenderer.sendSync('native:get-close-action')")
    expect(preload).toMatch(/setCloseAction:\s*\(action: string\)\s*=>/)
    expect(preload).toContain("ipcRenderer.send('native:set-close-action'")
    // The remembered choice has to reach the settings row, which cannot see IPC.
    expect(preload).toContain("ipcRenderer.on('clawbench-close-action-changed'")
  })

  it('the main process answers both channels', () => {
    const bridge = readRepoFile(BRIDGE)
    expect(bridge).toContain("ipcMain.on('native:get-close-action'")
    expect(bridge).toContain("ipcMain.on('native:set-close-action'")
  })

  it('the web app declares both methods on the shared bridge contract', () => {
    const contract = readRepoFile(NATIVE_CONTRACT)
    expect(contract).toMatch(/getCloseAction\?\(\):\s*string/)
    expect(contract).toContain('setCloseAction?(action: string): void')
  })

  it('the row is a desktop-only select with the three actions', () => {
    const map = readRepoFile(SETTINGS_MAP)
    // The row wraps over several lines (its options carry braces), so it is cut
    // at the start of the next row rather than by a brace-limited span.
    const start = map.indexOf("key: 'closeAction'")
    expect(start, 'closeAction row not found in settingsFieldMap').toBeGreaterThan(-1)
    const row = map.slice(start, map.indexOf('{ type:', start + 1))
    expect(row, 'must be a select').toContain("type: 'select'")
    expect(row, 'must be desktop-only').toContain('hideInAndroidApp: true')
    expect(row, 'must stay out of browser mode').toContain('appOnly: true')
    for (const value of ["'ask'", "'tray'", "'exit'"]) {
      expect(row, `missing option ${value}`).toContain(`value: ${value}`)
    }
  })

  it('the preference is pushed to the host and adopted back from it', () => {
    // Push-only leaves the row showing a stale value after a choice remembered
    // in the dialog; read-only leaves the row unable to change anything.
    const src = readRepoFile(SETTINGS_CONFIG)
    expect(src).toContain('setCloseAction')
    expect(src).toContain('getCloseAction')
    expect(src).toContain("'clawbench-close-action-changed'")
  })
})
