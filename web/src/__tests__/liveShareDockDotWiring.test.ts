import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * A running screen share is marked in two places: the panel header ("共享中")
 * and the dock tab you left behind. Both used the red token, which in this UI
 * means "error" or "recording" — a share is ordinary ongoing work, so both
 * moved to the theme accent, and the dock switched from tinting the whole icon
 * to a corner dot (a share has no count to show).
 *
 * The dot is no longer a special case in App.vue: the live plugin declares
 * `appearance.liveDot` and the shared DockTabButton draws it (suppressed on the
 * tab you are looking at). These assertions pin the declaration, the button's
 * suppression rule and the colours — the failure mode is silent, because a dot
 * that stops appearing looks exactly like "nothing is being shared".
 */
describe('live-share dock dot wiring', () => {
  const app = readWebFile('src/App.vue')
  const plugin = readWebFile('src/plugins/live/plugin.ts')
  const button = readWebFile('src/components/dock/DockTabButton.vue')
  const css = readWebFile('src/assets/dock.css')

  it('declares the dot on the plugin, not in the host', () => {
    expect(plugin).toContain('appearance:')
    expect(plugin).toContain('liveDot: () => liveState().value !== \'idle\'')
    expect(app).not.toContain('liveDotVisible')
  })

  it('draws the dot from the shared button, for every surface', () => {
    expect(button).toContain('dock-badge--live')
    expect(button).toContain('appearance.value?.liveDot?.()')
    // Every dock render site uses that one button.
    const sites = app.match(/<DockTabButton/g) ?? []
    expect(sites.length).toBeGreaterThanOrEqual(4)
  })

  it('hides the dot on the tab you are already looking at', () => {
    // A badge on the tab you are reading carries no information — same rule as
    // every count badge in the dock.
    expect(button).toContain('!suppressed.value')
    expect(button).toContain('const suppressed = computed(() => props.suppressBadge ?? props.active)')
  })

  it('no longer tints the live dock icon red', () => {
    // The old treatment: a scoped `.dock-btn.is-live` rule plus an `is-live`
    // entry in both dock class maps. Leaving either behind re-reddens the icon.
    expect(app).not.toContain('.dock-btn.is-live')
    expect(app).not.toContain("'is-live'")
  })

  it('draws the dot in the theme accent, not red', () => {
    const start = css.indexOf('.dock-badge--live {')
    expect(start, '.dock-badge--live rule not found in dock.css').toBeGreaterThan(-1)
    const body = css.slice(start, start + css.slice(start).indexOf('\n}'))
    // Colour comes from the shared .dock-badge rule; this modifier only adds
    // the pulse, so the accent must be declared there and not overridden.
    expect(body).not.toContain('--color-red')
    const base = css.indexOf('.dock-badge {')
    expect(base).toBeGreaterThan(-1)
    const baseBody = css.slice(base, base + css.slice(base).indexOf('\n}'))
    expect(baseBody).toContain('background: var(--accent-color')
  })

  it('freezes the pulse under reduced motion but keeps the dot visible', () => {
    // The dot is the information, the pulse is not — so the animation is
    // opt-out-able while the element itself must never be hidden.
    const start = css.indexOf('@media (prefers-reduced-motion: reduce) {')
    expect(start, 'reduced-motion guard not found for the live dot').toBeGreaterThan(-1)
    const block = css.slice(start, start + 200)
    expect(block).toContain('.dock-badge--live')
    expect(block).toContain('animation: none')
  })
})

describe('live-share panel header marker', () => {
  const panel = readWebFile('src/components/live/MeetingStage.vue')

  function ruleBody(selector: string): string {
    const start = panel.indexOf(`${selector} {`)
    expect(start, `${selector} rule not found in MeetingStage.vue`).toBeGreaterThan(-1)
    return panel.slice(start, start + panel.slice(start).indexOf('\n}'))
  }

  it('marks the running share with the theme accent', () => {
    const body = ruleBody('.live-panel__live')
    expect(body).toContain('color: var(--accent-color)')
    expect(body).toContain('color-mix(in srgb, var(--accent-color)')
    expect(body).not.toContain('--color-red')
  })

  it('keeps the destructive action out of the header', () => {
    // Red still means "this ends the meeting", which is exactly the semantic
    // the status marker must not borrow. That action is the footer's shared
    // danger pill (`.fbtn-danger`) now, so the header carries no red at all.
    expect(panel).not.toContain('.live-panel__stop')
    expect(panel).toContain('fbtn fbtn-danger')
  })
})
