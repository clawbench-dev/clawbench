import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guards for the desktop floating status capsule.
 *
 * `floating.ts` imports `electron` at module scope, so no unit test can load it.
 * Its failure modes are all silent — a bridge method under the wrong name is a
 * switch that does nothing, a stopped poll is a frozen tray tooltip, a fail-safe
 * timer is a capsule that comes back after being dismissed. These tests assert
 * the wiring in source, which is where a regression shows.
 *
 * Same convention as `splashWiring.test.ts` / `windowGeometryWiring.test.ts`.
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

const PRELOAD = 'desktop/src/preload/index.ts'
const FLOATING = 'desktop/src/main/floating.ts'
const NATIVE_CONTRACT = 'web/src/utils/clawbenchNative.ts'
const SETTINGS_MAP = 'web/src/components/settings/settingsFieldMap.ts'
const SETTINGS_CONFIG = 'web/src/composables/useSettingsConfig.ts'

describe('the feature has a reachable on/off switch', () => {
  it('the desktop preload implements the name the web app calls', () => {
    // `useSettingsConfig` calls `setFloatingWindowEnabled` for BOTH hosts. The
    // preload used to expose only `setFloatingEnabled`, so the settings row was
    // a switch that silently did nothing on desktop — and the tray's "turn off"
    // then left no way back.
    const preload = readRepoFile(PRELOAD)
    expect(preload).toMatch(/setFloatingWindowEnabled:\s*\(/)
    expect(preload).toContain("ipcRenderer.send('native:floating-enabled'")
    // The old, unused name must not linger as a second, divergent entry point.
    expect(preload).not.toMatch(/\bsetFloatingEnabled:/)
  })

  it('the web app declares both methods on the shared bridge contract', () => {
    const contract = readRepoFile(NATIVE_CONTRACT)
    expect(contract).toContain('setFloatingWindowEnabled(enabled: boolean): void')
    // The read accessor is what lets the page adopt a tray-initiated change.
    expect(contract).toMatch(/isFloatingWindowEnabled\?\(\):\s*boolean/)
  })

  it('the settings row is not Android-only', () => {
    // `androidOnly` hides the row in Electron, which is precisely where the
    // desktop capsule needs a switch. `appOnly` keeps it out of browser mode
    // (no native host there) while rendering it in both native shells.
    const map = readRepoFile(SETTINGS_MAP)
    const row = map.match(/\{[^{}]*key:\s*'floatingStatusWindow'[^{}]*\}/)
    expect(row, 'floatingStatusWindow row not found in settingsFieldMap').not.toBeNull()
    expect(row![0], 'floatingStatusWindow must not be androidOnly').not.toContain('androidOnly')
    expect(row![0], 'floatingStatusWindow must be appOnly').toContain('appOnly: true')
  })

  it('the feature flag is read from the host, not just pushed to it', () => {
    // Push-only leaves the switch claiming "on" after the tray turned it off,
    // and the next loadConfig pushes `true` straight back.
    const src = readRepoFile(SETTINGS_CONFIG)
    expect(src).toContain('isFloatingWindowEnabled')
    expect(src).toContain("'clawbench-floating-enabled-changed'")
  })
})

describe('the capsule follows the main window, not the feature flag', () => {
  // The settings row promises "while the app is in the background". The capsule
  // used to be created as soon as the feature was on — so it sat on top of the
  // window the user was working in, permanently.
  it('visibility is derived from the main window being out of sight', () => {
    const src = readRepoFile(FLOATING)
    const sync = src.match(/function syncCapsuleToMainWindow\(\): void \{([\s\S]*?)\n\}/)
    expect(sync, 'syncCapsuleToMainWindow not found').not.toBeNull()
    expect(sync![1], 'must branch on the main window, not show unconditionally').toContain('mainWindowOutOfSight()')
    // The hide half matters as much as the show half: restoring the window has
    // to take the capsule away again.
    expect(sync![1], 'must hide when the window is back').toContain('hideCapsule()')

    const predicate = src.match(/function mainWindowOutOfSight\(\): boolean \{([\s\S]*?)\n\}/)
    expect(predicate, 'mainWindowOutOfSight not found').not.toBeNull()
    // A minimized window still reports `isVisible()` on Windows, so the two
    // conditions are not interchangeable.
    expect(predicate![1], 'a minimized window counts as out of sight').toContain('isMinimized()')
    expect(predicate![1], 'a hidden window counts as out of sight').toContain('!w.isVisible()')
    // …and a window that has never been shown must NOT count: `createMainWindow`
    // builds with `show: false`, so the startup window would otherwise read as
    // "backgrounded" and the capsule would pop up at every launch.
    expect(predicate![1], 'a not-yet-shown window is not "hidden"').toContain('mainWindowShown')
  })

  it('every main-window transition that changes visibility is wired', () => {
    const src = readRepoFile(FLOATING)
    const wire = src.match(/function wireMainWindow\(w: BrowserWindow\): void \{([\s\S]*?)\n\}/)
    expect(wire, 'wireMainWindow not found').not.toBeNull()
    for (const event of ['show', 'hide', 'minimize', 'restore', 'closed']) {
      expect(wire![1], `the main window's '${event}' event must resync the capsule`).toContain(`'${event}'`)
    }
    expect(wire![1], 'the shown flag must be set when the window appears').toContain('mainWindowShown = true')

    // A re-created window (macOS activate, the capsule's own "open the app")
    // must be wired too, which is why the hook lives in window.ts.
    expect(src, 'must subscribe to main-window creation').toContain('onMainWindowCreated(wireMainWindow)')
    const win = readRepoFile('desktop/src/main/window.ts')
    expect(win, 'window.ts must expose the hook').toContain('export function onMainWindowCreated')
    expect(win, 'window.ts must notify on every creation').toContain('notifyMainWindowCreated(mainWindow)')
  })

  it('turning the feature on does not put the capsule on screen', () => {
    // The switch is flipped from the settings row, i.e. with the window in
    // front. Showing the capsule there is the exact behaviour being fixed.
    const src = readRepoFile(FLOATING)
    const enable = src.match(/export function applyFloatingEnabled\([\s\S]*?\n\}/)
    expect(enable, 'applyFloatingEnabled not found').not.toBeNull()
    expect(enable![0], 'arming the feature must go through the window-state sync').toContain('syncCapsuleToMainWindow()')
    expect(enable![0], 'arming the feature must not show the capsule outright').not.toContain('showCapsule()')
  })

  it('startup arms the feature without showing anything', () => {
    // The old startup path waited for the main window's `show` and then created
    // the capsule, with a timer as a fail-safe. Both are gone: at startup the
    // window is (about to be) in front of the user, so there is nothing to show.
    const src = readRepoFile(FLOATING)
    expect(src, 'the startup show timer must be gone').not.toContain('STARTUP_SHOW_FALLBACK_MS')
    const init = src.match(/export function initFloating\(\): void \{([\s\S]*?)\n\}/)
    expect(init, 'initFloating not found').not.toBeNull()
    expect(init![1], 'startup must sync instead of showing').toContain('syncCapsuleToMainWindow()')
    expect(init![1], 'startup must not show the capsule outright').not.toContain('showCapsule()')
    // The listener is registered even when the feature is off, or turning it on
    // later would leave the (already created) window unwired.
    expect(init![1], 'the main window must be wired regardless of the flag').toContain('onMainWindowCreated')
  })

  it('the tray keeps the counters moving with no capsule window at all', () => {
    // Polling used to start in `createWindow`, which now only runs on the first
    // minimize — the tray tooltip would sit at 0/0/0 until then.
    const src = readRepoFile(FLOATING)
    const tray = src.match(/function ensureTray\(\): void \{([\s\S]*?)\n\}/)
    expect(tray, 'ensureTray not found').not.toBeNull()
    expect(tray![1], 'the tray must start polling on its own').toContain('startPolling()')
  })
})

describe('the capsule surround stays fully transparent', () => {
  // A transparent window is still a rectangle to the compositor, and the window
  // is only 4px larger than the pill. Any shadow with a blur radius is clipped
  // at the window edge and paints that margin — which the user sees as a grey
  // semi-transparent box framing the capsule on light wallpapers.
  const HTML = 'desktop/assets/floating.html'

  /** Split a `box-shadow` value on top-level commas (rgba() carries its own). */
  function layers(decl: string): string[] {
    const out: string[] = []
    let depth = 0
    let cur = ''
    for (const ch of decl) {
      if (ch === '(') depth++
      else if (ch === ')') depth--
      if (ch === ',' && depth === 0) {
        out.push(cur.trim())
        cur = ''
        continue
      }
      cur += ch
    }
    if (cur.trim()) out.push(cur.trim())
    return out
  }

  /** The numeric lengths of one layer, with colour functions stripped out. */
  function lengthsOf(layer: string): number[] {
    // Colour functions carry their own numbers (`rgba(0, 0, 0, .38)`), and
    // offsets are commonly unitless zeros — so drop parenthesised content first
    // rather than trying to tell `0` from a colour channel by its suffix.
    let stripped = ''
    let depth = 0
    for (const ch of layer) {
      if (ch === '(') depth++
      if (depth === 0) stripped += ch
      if (ch === ')') depth--
    }
    return (stripped.match(/-?\d+(?:\.\d+)?/g) ?? []).map(Number)
  }

  it('no shadow layer has a blur radius', () => {
    const src = readRepoFile(HTML)
    const decls = src.match(/box-shadow:[^;]*;/g) ?? []
    expect(decls.length, 'the capsule should still draw its inset ring').toBeGreaterThan(0)
    for (const decl of decls) {
      for (const layer of layers(decl.replace(/^box-shadow:/, '').replace(/;$/, ''))) {
        // A layer is `offsetX offsetY blur spread colour`; the third length is
        // the blur. `0 0 0 1px …` (ring) passes, `0 6px 22px …` does not.
        const blur = lengthsOf(layer)[2] ?? 0
        expect(blur, `"${decl.trim()}" blurs into the transparent margin`).toBe(0)
      }
    }
  })
})

describe('polling outlives the capsule window', () => {
  it('polling is gated on the tray, not on the capsule being visible', () => {
    // The tray tooltip shows the same three counters and survives hiding the
    // capsule. Stopping the poll on capsule teardown froze it at the values it
    // had at that moment.
    const src = readRepoFile(FLOATING)
    const wants = src.match(/function wantsPolling\(\): boolean \{([\s\S]*?)\n\}/)
    expect(wants, 'wantsPolling not found').not.toBeNull()
    expect(wants![1], 'the tray must keep polling alive').toContain('tray')
    expect(wants![1], 'the capsule must keep polling alive').toContain('isFloatingAlive()')
  })

  it('teardown uses the tray-aware stop, not an unconditional one', () => {
    const src = readRepoFile(FLOATING)
    expect(src).toContain('stopPollingIfIdle()')
    expect(src).not.toMatch(/function stopPolling\(\)/)
  })

  it('a stats refresh no longer requires a live capsule window', () => {
    // The old guard `!isAlive()` returned early, so a hidden capsule meant the
    // numbers never moved even though the tray was still there.
    const src = readRepoFile(FLOATING)
    const refresh = src.match(/export async function refreshFloatingStats\([\s\S]*?\n\}/)
    expect(refresh, 'refreshFloatingStats not found').not.toBeNull()
    expect(refresh![0], 'refreshFloatingStats must not bail on a hidden capsule').not.toContain('!isAlive()')
  })

  it('activity events are not routed through the notification handler', () => {
    // `native:notify` is gated by the desktopNotification setting and by page
    // focus, so hanging the capsule refresh off it meant a user with
    // notifications off never got an event-driven update.
    const src = readRepoFile(FLOATING)
    expect(src).toContain('export function noteFloatingActivity')
    expect(src).toContain('wantsPolling()')

    const bridge = readRepoFile('desktop/src/main/bridge.ts')
    expect(bridge, 'bridge must not gate the capsule on native:notify').not.toContain('noteFloatingActivity')

    // A dedicated channel exists, and the preload exposes it under the name the
    // web app calls.
    expect(src).toContain("ipcMain.on('native:floating-activity'")
    const preload = readRepoFile(PRELOAD)
    expect(preload).toMatch(/notifyFloatingActivity:\s*\(\)\s*=>/)
    expect(preload).toContain("ipcRenderer.send('native:floating-activity')")
  })

  it('the web app reports activity from its own WS handler, not from notify', () => {
    // The report must happen on the terminal-event path itself: putting it
    // behind showBrowserNotification would reintroduce the same gate.
    const src = readRepoFile('web/src/composables/useGlobalEvents.ts')
    expect(src).toContain('notifyFloatingActivity()')
    // …and it must sit outside the `showBrowserNotification` call's own body.
    const report = src.match(/function notifyFloatingActivity\(\)[\s\S]*?\n\}/)
    expect(report, 'notifyFloatingActivity helper not found').not.toBeNull()
    expect(report![0], 'the helper must not call nativeNotify').not.toContain('nativeNotify')
  })
})

describe('the capsule has no in-body close button', () => {
  // Hiding the capsule is a right-click action only. A close button inside the
  // capsule put a 16px target on the drag surface, and every pointerdown on it
  // had to be stopped from starting a drag.
  const HTML = 'desktop/assets/floating.html'
  const PRELOAD_FLOATING = 'desktop/src/preload/floating.ts'

  it('the document renders no close control', () => {
    const src = readRepoFile(HTML)
    expect(src, 'no #close element').not.toContain('id="close"')
    expect(src, 'no #close styling').not.toContain('#close')
    expect(src, 'no close label wiring').not.toContain('closeLabel')
  })

  it('the right-click menu is still the way to hide it', () => {
    // Removing the button must not remove the capability: the context menu
    // built in the main process carries it.
    //
    // `host.context()` alone is not enough — that call existing does not prove
    // the handler still routes to the menu. Pin the handler body too, and pin
    // that the right-click path does NOT go through the removed hide verb.
    const src = readRepoFile(HTML)
    const handler = src.match(/addEventListener\('contextmenu', function \(e\) \{([\s\S]*?)\n  \}\)/)
    expect(handler, 'the capsule must still have a contextmenu handler').not.toBeNull()
    expect(handler![1], 'right-click must open the native menu').toContain('host.context()')
    expect(handler![1], 'right-click must not call the removed hide verb').not.toContain('host.hide')

    const main = readRepoFile(FLOATING)
    expect(main, 'the context menu must still offer hiding the capsule').toContain('t.hide')
  })

  it('no dead hide/quit channel survives on either side', () => {
    // A preload verb with no control behind it is a channel nothing can use;
    // leaving it would invite someone to re-add a button that bypasses the menu.
    //
    // Comments are stripped first: the removal is documented in the preload with
    // the very names being asserted absent.
    const strip = (src: string) => src
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/(^|\s)\/\/[^\n]*/g, '$1')

    const preload = strip(readRepoFile(PRELOAD_FLOATING))
    expect(preload).not.toMatch(/\bhide\s*\(\)/)
    expect(preload).not.toMatch(/\bquit\s*\(\)/)

    const main = strip(readRepoFile(FLOATING))
    expect(main).not.toContain("ipcMain.on('floating:hide'")
    expect(main).not.toContain("ipcMain.on('floating:quit'")
  })
})

describe('capsule IPC is sender-checked', () => {
  it('every capsule channel validates the sender', () => {
    // The capsule shares an IPC namespace with the main window. An unchecked
    // channel is a native action any renderer could trigger, without going
    // through the capsule at all.
    //
    // Matched line by line rather than with a lazy `[\s\S]*?` span: a span runs
    // past the handler's end into the next one and finds `fromCapsule(e)` there,
    // so deleting a single check still passed.
    const src = readRepoFile(FLOATING)
    const lines = src.split('\n')

    for (const channel of [
      'floating:ready',
      'floating:activate',
      'floating:context',
      'floating:drag-start',
      'floating:drag-end',
    ]) {
      const start = lines.findIndex((l) => l.includes(`ipcMain.on('${channel}'`))
      expect(start, `${channel} handler not found`).toBeGreaterThan(-1)

      // Some handlers are one line (`ipcMain.on('floating:activate', (e) => { … })`),
      // others span several. Taking "lines until the next closing brace" missed
      // the single-line form entirely — the scan walked past it into the next
      // handler, which does check its sender. A single-line handler is one that
      // is already a complete call (ends with `)`).
      const first = lines[start]
      const body = first.trimEnd().endsWith(')')
        ? first
        : lines
            .slice(start, lines.findIndex((l, i) => i > start && /^\s{2}\}\)?;?\s*$/.test(l)) + 1)
            .join('\n')
      expect(body, `${channel} must check the sender`).toContain('fromCapsule(e)')
    }

    // `handle` (invoke) gets the same treatment.
    const resizeStart = lines.findIndex((l) => l.includes("ipcMain.handle('floating:resize'"))
    expect(resizeStart, 'floating:resize handler not found').toBeGreaterThan(-1)
    const resizeEnd = lines.findIndex((l, i) => i > resizeStart && /^\s{2}\}\)?;?\s*$/.test(l))
    const resizeBody = lines.slice(resizeStart, resizeEnd + 1).join('\n')
    expect(resizeBody).toContain('fromCapsule(e)')
  })
})

describe('the feature-state read answers about the feature, not the window', () => {
  it('native:floating-is-enabled reports the stored flag', () => {
    // It used to return `isFloatingAlive()`, so a user who hid the capsule saw
    // the settings switch turn itself off and could not tell "off" from
    // "hidden".
    const src = readRepoFile(FLOATING)
    const handler = src.match(/ipcMain\.on\('native:floating-is-enabled'[\s\S]*?\n  \}\)/)
    expect(handler, 'native:floating-is-enabled handler not found').not.toBeNull()
    expect(handler![0]).toContain('isEnabled()')
    expect(handler![0], 'must not report capsule visibility').not.toContain('isFloatingAlive')
  })
})
