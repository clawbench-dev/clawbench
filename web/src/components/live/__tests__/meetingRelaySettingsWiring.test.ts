import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * 创建会议 → 中继服务配置 的跳转接线。
 *
 * The relay is installation-wide, so its form lives in 设置 → 会议; the create
 * page only carries the jump. Two things make that jump usable, and both live
 * in code no component test can reach (App.vue's setup):
 *
 *  - the `live` origin, so Back (Android hardware / edge swipe / header)
 *    returns to the meeting tab instead of whatever tab preceded the jump;
 *  - the create-form return request, because the meeting panel's own rule is to
 *    drop a half-filled 创建 form on re-entry — which would throw away the name
 *    the user had already typed.
 *
 * The refresh of the relay state itself is behavioural: the form watches
 * `active` (see MeetingCreate.browser.test.ts).
 */
describe('meeting → relay settings wiring', () => {
  const app = readWebFile('src/App.vue')
  const panel = readWebFile('src/components/live/MeetingPanel.vue')
  const plugin = readWebFile('src/plugins/live/plugin.ts')

  function functionBody(name: string): string {
    const start = app.indexOf(`function ${name}(`)
    expect(start, `${name} not found in App.vue`).toBeGreaterThan(-1)
    const body = app.slice(start)
    return body.slice(0, body.indexOf('\n}'))
  }

  it('binds the panel’s request to the generic settings jump', () => {
    // The panel is rendered by the generic DockPanelHost, so the binding lives
    // in the live plugin's `panel.on` map (an object key, not a template
    // attribute — the emit is camelCase at runtime). The host provides ONE
    // settings jump for every plugin; the plugin adds what is meeting-specific.
    expect(plugin).toContain("relaySettings: () => {")
    expect(plugin).toContain("host.openSettingsCategory('meeting')")
    expect(plugin).toContain('setMeetingCreateReturn()')
    expect(app).toContain('function openSettingsCategory')
    expect(app).not.toContain('handleOpenMeetingRelaySettings')
    expect(app).not.toContain('openMeetingRelaySettings')
  })

  it('records the meeting origin before switching tabs', () => {
    const body = functionBody('openSettingsCategory')
    // Without this, Back lands on whatever tab happened to precede the jump.
    // The origin is the panel the user was on when they asked for settings —
    // the same thing the meeting handler used to hardcode as 'live'.
    expect(body).toMatch(/beginExternalJump\(origin/)
    // The settings TabPanel is mounted lazily, so the target travels through
    // the module-level pending category.
    expect(body).toContain('setPendingSettingsCategory(id)')
    expect(body).toContain("switchTab('settings')")
  })

  it('the panel consumes the parked return and forwards the request', () => {
    expect(panel).toContain('@relay-settings="emit(\'relaySettings\')"')
    expect(panel).toContain('takeMeetingCreateReturn()')
    // The form stays mounted across the jump, so it needs the tab's active
    // state to know when to re-read the relay configuration.
    expect(panel).toContain(':active="active"')
  })
})
