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
 * The set asserted here is deliberately narrow: the four h2-toggle bridge
 * methods, plus `getTunnelTransport` / `getActiveTunnelTransport` (consumed by
 * usePortForward.refreshActiveTransport). The latter two are optional in the TS
 * contract and degrade to a hidden status line, so they are not load-bearing
 * like the h2 toggle — but they are still consumed, and this file's own rule
 * ("a method belongs here iff the frontend consumes it") puts them in scope.
 * The many other cross-platform methods the frontend calls (getAppVersion,
 * isTunnelConnected, getForwardedPorts, testPortReachable, the download and
 * share actions, reconnectTunnel, …) are NOT asserted here: they are
 * long-shipped and each is already pinned by a host-specific test on the
 * Android side (e.g. MainActivityTunnelBridgeTest's annotation reflection) or
 * by the method's own behaviour spec. This file exists to catch the h2-era
 * additions that slipped through, not to re-encode the whole bridge.
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

  it('getTunnelTransportH2Enabled is bridged (local fallback for the transport)', () => {
    // The server's port_forward.transport is authoritative now; this preference
    // is only consulted when /api/ssh/info could not be read. Keep the bridge:
    // BackgroundService.shouldUseH2() falls back to it, and older web bundles
    // still call it.
    expect(androidBridgeExposes(src, 'getTunnelTransportH2Enabled')).toBe(true)
  })

  it('getTunnelTransportH2Enabled reads the SharedPreferences source of truth', () => {
    expect(src).toContain('BackgroundService.isTunnelTransportH2Enabled(activity)')
  })

  it('setTunnelTransportH2Enabled is bridged (writes the local fallback)', () => {
    // Writes the preference shouldUseH2() falls back to when the server cannot
    // be asked. The server-side row no longer calls it.
    expect(androidBridgeExposes(src, 'setTunnelTransportH2Enabled')).toBe(true)
  })

  it('setTunnelTransportH2Enabled delegates to the SharedPreferences writer', () => {
    expect(src).toContain('BackgroundService.setTunnelTransportH2Enabled(activity, enabled)')
  })

  it('getTunnelTransport is bridged (kept for hosts and older web bundles)', () => {
    // The settings/status rows now read the server's port_forward.transport, so
    // this is no longer their source. It stays bridged because the shell and
    // older cached bundles still call it.
    expect(androidBridgeExposes(src, 'getTunnelTransport')).toBe(true)
  })

  it('getTunnelTransport prefers the server-reported transport', () => {
    // The server decides and the app follows; the local toggle is only the
    // fallback when /api/ssh/info has not been read yet. A bridge that reported
    // the local preference alone would describe a wire the server turned off.
    expect(src).toContain('BackgroundService.getServerTransport()')
    expect(src).toContain('BackgroundService.isTunnelTransportH2Enabled(activity) ? "h2" : "ssh"')
  })

  it('getActiveTunnelTransport is bridged (kept for diagnostics)', () => {
    // No longer read by usePortForward (the row shows the server's value), but
    // the bridge stays: it is how the live wire can be inspected on a device.
    expect(androidBridgeExposes(src, 'getActiveTunnelTransport')).toBe(true)
  })

  it('getActiveTunnelTransport delegates to the live-session accessor', () => {
    // The empty-string answer (no live h2 session) is what lets the frontend
    // fall back to getTunnelTransport(); a hardcoded "h2" would break that.
    expect(src).toContain('BackgroundService.getActiveTunnelTransport()')
  })
})
