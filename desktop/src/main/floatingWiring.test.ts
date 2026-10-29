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

describe('startup fail-safe respects a dismissed capsule', () => {
  it('the fallback timer checks the revoked marker before showing', () => {
    // `initFloating` schedules a delayed show in case the main window never
    // emits 'show'. It used to run unconditionally, so a user who dismissed the
    // capsule (or turned the feature off) within those seconds got it back with
    // the tray already destroyed.
    //
    // The body is matched to the CLOSING `}, STARTUP_SHOW_FALLBACK_MS)` rather
    // than with a lazy `[\s\S]*?`: a lazy match runs past the timer body into
    // the rest of the function and finds `startupIntentRevoked` in the `once`
    // handler above, so deleting the guard still passes.
    const src = readRepoFile(FLOATING)
    // Anchored on the exact closing line (indentation included). A lazy
    // `[\s\S]*?` was not enough: the timer body contains no `},` of its own, so
    // the match ran on into the following `once('show')` handler and found
    // `startupIntentRevoked` there — deleting the guard still passed.
    const timer = src.match(/\n      setTimeout\(\(\) => \{([\s\S]*?)\n      \}, STARTUP_SHOW_FALLBACK_MS\)/)
    expect(timer, 'the startup fallback timer must exist').not.toBeNull()
    // Strip comments first: the guard's own explanatory comment names
    // `startupIntentRevoked`, and matching that would make the check pass with
    // the code deleted.
    const body = timer![1]
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/(^|\s)\/\/[^\n]*/g, '$1')
    expect(body, 'the fallback must bail when the user revoked the intent').toContain('startupIntentRevoked')
    expect(body, 'the fallback must bail when the feature was turned off').toContain('isEnabled()')
    // …and both checks must come BEFORE the show, or they guard nothing.
    const showAt = body.indexOf('showCapsule()')
    expect(showAt, 'the fallback must still show the capsule').toBeGreaterThan(-1)
    expect(body.indexOf('startupIntentRevoked'), 'the revoked check must precede showCapsule').toBeLessThan(showAt)
    expect(body.indexOf('isEnabled()'), 'the enabled check must precede showCapsule').toBeLessThan(showAt)
  })

  it('both dismissal paths set the marker', () => {
    // Hiding from the tray/capsule, and turning the feature off, are the two
    // ways the user can revoke the startup intent.
    const src = readRepoFile(FLOATING)
    const hide = src.match(/function hideCapsule\(\): void \{([\s\S]*?)\n\}/)
    expect(hide, 'hideCapsule not found').not.toBeNull()
    expect(hide![1]).toContain('startupIntentRevoked = true')

    const disable = src.match(/export function applyFloatingEnabled\([\s\S]*?\n\}/)
    expect(disable, 'applyFloatingEnabled not found').not.toBeNull()
    expect(disable![0]).toContain('startupIntentRevoked = true')
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

describe('capsule IPC is sender-checked', () => {
  it('every capsule channel validates the sender', () => {
    // The capsule shares an IPC namespace with the main window. An unchecked
    // `floating:quit` is an unauthenticated "quit the app" from any renderer
    // that can reach ipcRenderer.
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
      'floating:hide',
      'floating:quit',
      'floating:drag-start',
      'floating:drag-end',
    ]) {
      const start = lines.findIndex((l) => l.includes(`ipcMain.on('${channel}'`))
      expect(start, `${channel} handler not found`).toBeGreaterThan(-1)

      // Some handlers are one line (`ipcMain.on('floating:quit', (e) => { … })`),
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
