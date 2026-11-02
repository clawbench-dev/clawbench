import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * A running screen share is marked in two places: the panel header ("共享中")
 * and the dock tab you left behind. Both used the red token, which in this UI
 * means "error" or "recording" — a share is ordinary ongoing work, so both
 * moved to the theme accent, and the dock switched from tinting the whole icon
 * to a corner dot (a share has no count to show).
 *
 * App.vue has no component test (its only tests read it as source text) and
 * `liveDotVisible` is a plain function inside `<script setup>`, so the wiring
 * is pinned as source text: the failure mode is silent — one of the three dock
 * render sites (wide dock, narrow inline overflow, narrow single-direct item)
 * keeping the old red class while the others move on.
 */
describe('live-share dock dot wiring', () => {
  const app = readWebFile('src/App.vue')
  const template = app.slice(0, app.indexOf('\n<script setup'))

  it('renders the state dot on every dock render site that can hold the live tab', () => {
    // Wide dock secondary tabs, narrow inline overflow tabs, narrow single
    // remaining item. Missing one leaves that layout with no indicator at all.
    const dots = template.match(/class="dock-badge dock-badge--live"/g) ?? []
    expect(dots.length).toBe(3)
  })

  it('gates each dot on the shared helper, with the surface\'s own active tab', () => {
    expect(template).toContain('liveDotVisible(tab, leftTab === tab)')
    expect(template).toContain('liveDotVisible(tab, activeTab === tab)')
    expect(template).toContain('liveDotVisible(singleDirectTab, activeTab === singleDirectTab)')
  })

  it('shows the dot only while sharing and only on the tab you are NOT looking at', () => {
    const start = app.indexOf('function liveDotVisible(')
    expect(start, 'liveDotVisible not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n}'))
    expect(body).toContain('tab === \'live\'')
    expect(body).toContain('liveShareActive.value')
    // A badge on the tab you are already looking at carries no information —
    // same rule as every count badge in the dock.
    expect(body).toContain('!isActive')
  })

  it('no longer tints the live dock icon red', () => {
    // The old treatment: a scoped `.dock-btn.is-live` rule plus an `is-live`
    // entry in both dock class maps. Leaving either behind re-reddens the icon.
    expect(app).not.toContain('.dock-btn.is-live')
    expect(app).not.toContain("'is-live'")
  })

  it('draws the dot in the theme accent, not red', () => {
    const start = app.indexOf('.dock-badge--live {')
    expect(start, '.dock-badge--live rule not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n}'))
    // Colour comes from the shared .dock-badge rule; this modifier only adds
    // the pulse, so the accent must be declared there and not overridden.
    expect(body).not.toContain('--color-red')
    const base = app.indexOf('.dock-badge {')
    expect(base).toBeGreaterThan(-1)
    const baseBody = app.slice(base, base + app.slice(base).indexOf('\n}'))
    expect(baseBody).toContain('background: var(--accent-color')
  })

  it('freezes the pulse under reduced motion but keeps the dot visible', () => {
    // The dot is the information, the pulse is not — so the animation is
    // opt-out-able while the element itself must never be hidden.
    const start = app.indexOf('@media (prefers-reduced-motion: reduce) {')
    expect(start, 'reduced-motion guard not found for the live dot').toBeGreaterThan(-1)
    const block = app.slice(start, start + 200)
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
