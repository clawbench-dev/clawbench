import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Design §1: the left column is ONE ordered list. There is no fixed head any
 * more — every tab (permanent ones and settings included) takes part in the
 * user's order — and the wide and narrow docks must render that same list, so
 * a tab can never sit in a different place depending on the viewport.
 *
 * App.vue has no component test (its only tests read it as source text), and
 * the risk here is silent: reintroducing a "primary head" would still render a
 * working dock, just one whose order disagrees with the manager.
 */
describe('dock renders a single ordered list', () => {
  const app = readWebFile('src/App.vue')
  const template = app.slice(0, app.indexOf('\n<script setup'))

  it('keeps no fixed-head concepts around', () => {
    for (const gone of [
      'primaryDockTabs',
      'secondaryDockTabs',
      'WIDE_SCREEN_PRIMARY_TABS',
      'visiblePrimaryTabs',
      'wideDockTabOrder',
    ]) {
      expect(app, `${gone} is a fixed-head leftover`).not.toContain(gone)
    }
  })

  it('renders exactly one loop in the wide dock, over visibleDockTabs', () => {
    const wide = template.slice(
      template.indexOf('class="wide-dock"'),
      template.indexOf('class="content-area"'),
    )
    expect(wide).toContain('v-for="tab in visibleDockTabs"')
    expect(wide.match(/v-for="tab in/g) ?? []).toHaveLength(1)
  })

  it('does not hard-code the old head buttons in the narrow dock', () => {
    // browse / view / history used to be literal buttons before the overflow
    // area; a leftover would double-render them (once ordered, once pinned).
    expect(template).not.toContain('data-tab="browse"')
    expect(template).not.toContain('data-tab="view"')
    expect(template).not.toContain('data-tab="history"')
  })

  it('feeds the narrow overflow from the same list, with only chat pinned', () => {
    const start = app.indexOf('} = useDockOverflow(')
    expect(start, 'useDockOverflow call not found').toBeGreaterThan(-1)
    const body = app.slice(start, start + 240)
    expect(body).toContain('visibleDockTabs.value')
    expect(body).toContain('primaryCount: 1')
    expect(template).toContain('v-for="tab in inlineOverflowTabs"')
  })

  it('positions both active indicators from the rendered order', () => {
    const wideStart = app.indexOf('const wideDockActiveIndex')
    expect(wideStart).toBeGreaterThan(-1)
    expect(app.slice(wideStart, wideStart + 260)).toContain(
      'visibleDockTabs.value.indexOf(leftTab.value)',
    )

    const narrowStart = app.indexOf('const dockActiveIndex')
    expect(narrowStart).toBeGreaterThan(-1)
    expect(app.slice(narrowStart, narrowStart + 260)).toContain('allInlineOverflowTabs.value')
  })
})
