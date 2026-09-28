import { describe, expect, it } from 'vitest'
import { readAndroidBridge, androidBridgeExposes } from '@/testUtils/androidBridgeContract'

/**
 * Real Android host contract for the bridge methods the frontend ACTUALLY calls.
 *
 * Every other test that exercises these methods installs a hand-built fake host,
 * so on its own it only proves "the code works when the method is present". That
 * is precisely how the 2026 h2-toggle bug shipped green: the settings row read a
 * getter the shipped Android bridge never implemented, and no test noticed. The
 * assertions here read the real `MainActivity.java` instead, so a host that stops
 * exposing (or stops bridging) a method the frontend depends on fails loudly.
 *
 * Scope rule: a method belongs here iff the frontend consumes it. Optional
 * methods that only some hosts implement are covered by their own host-specific
 * contracts; and Android-only methods with no frontend consumer (e.g. the
 * `isFloatingWindowEnabled` / `isLiveUpdateEnabled` read accessors, which exist
 * for native use and are unused by the web app) are deliberately NOT asserted —
 * this file pins "what the frontend requires of the host", not "what the host
 * happens to expose".
 *
 * The two `getFloatingWindowEnabled` / `getLiveUpdateEnabled` declarations that
 * this file's sibling change removed from `clawbenchNative.ts` are intentionally
 * absent: they were dead (zero consumers, never bridged — the Android bridge
 * spells those reads `is*`). Asserting a dead declaration would only freeze the
 * mistake.
 */
describe('Android host contract: bridge methods the frontend consumes', () => {
  const src = readAndroidBridge()

  it('setFloatingWindowEnabled is bridged (called from useSettingsConfig.ts)', () => {
    // useSettingsConfig.ts:160 — the floatingStatusWindow settings row pushes
    // its value to native on change; a missing method silently no-ops there.
    expect(androidBridgeExposes(src, 'setFloatingWindowEnabled')).toBe(true)
  })

  it('setFloatingWindowEnabled delegates to the SharedPreferences writer', () => {
    // Pins that the bridge persists rather than dropping the value: a stub body
    // would pass the presence check above while changing nothing.
    expect(src).toContain('BackgroundService.setFloatingWindowEnabled(activity, enabled)')
  })

  it('setLiveUpdateEnabled is bridged (called from useSettingsConfig.ts)', () => {
    // useSettingsConfig.ts:170 — the liveUpdate settings row's write path.
    expect(androidBridgeExposes(src, 'setLiveUpdateEnabled')).toBe(true)
  })

  it('setLiveUpdateEnabled delegates to the SharedPreferences writer', () => {
    expect(src).toContain('BackgroundService.setLiveUpdateEnabled(activity, enabled)')
  })

  it('getTunnelTransportH2Enabled is bridged (read by the h2 settings row)', () => {
    // SettingsGroupPanel.vue:617 HIDES the h2 toggle when this getter is absent,
    // so without it the toggle can never be turned on in the app. This is the
    // exact method whose absence shipped the 2026 blocking bug.
    expect(androidBridgeExposes(src, 'getTunnelTransportH2Enabled')).toBe(true)
  })

  it('getTunnelTransportH2Enabled reads the SharedPreferences source of truth', () => {
    expect(src).toContain('BackgroundService.isTunnelTransportH2Enabled(activity)')
  })

  it('setTunnelTransportH2Enabled is bridged (written by the h2 settings row)', () => {
    // SettingsGroupPanel.vue:636 — the h2 toggle's write path.
    expect(androidBridgeExposes(src, 'setTunnelTransportH2Enabled')).toBe(true)
  })

  it('setTunnelTransportH2Enabled delegates to the SharedPreferences writer', () => {
    expect(src).toContain('BackgroundService.setTunnelTransportH2Enabled(activity, enabled)')
  })
})
