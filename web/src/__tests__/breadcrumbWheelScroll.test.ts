import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: every breadcrumb strip must accept a vertical mouse wheel as
 * horizontal scrolling.
 *
 * WHY THIS EXISTS
 *
 * All breadcrumbs share one CSS shape: `display: flex` + `overflow-x: auto` +
 * `scrollbar-width: none` (a visible scrollbar inside a header row is noise).
 * With the bar hidden, a plain wheel over the strip scrolls the PAGE — the
 * browser only maps a wheel to the nearest VERTICALLY scrollable ancestor — so
 * on desktop the tail of a long path (the current directory, plus the copy
 * button) is unreachable. Touch and trackpads are unaffected (they produce
 * `deltaX` / native touch scrolling), which is exactly why this slips through
 * manual testing on a phone.
 *
 * The fix is one binding per component (`@wheel="onHorizontalWheel"`, from
 * `utils/horizontalWheelScroll`). This test pins the pairing so a new
 * breadcrumb — or a refactor that drops the handler — cannot silently regress
 * to "hidden scrollbar with no way to scroll".
 */

/** Breadcrumbs that hide their scrollbar, i.e. that depend on the wheel mapping. */
const BREADCRUMBS = [
  'src/components/file/DirBreadcrumb.vue',
  'src/components/git/GitBreadcrumb.vue',
  'src/components/task/TaskBreadcrumb.vue',
  'src/components/settings/SettingsBreadcrumb.vue',
]

const WHEEL_BINDING = /@wheel="onHorizontalWheel"/

function hidesScrollbar(source: string): boolean {
  return /overflow-x:\s*auto/.test(source) && /scrollbar-width:\s*none/.test(source)
}

describe('breadcrumb strips scroll with a plain mouse wheel', () => {
  it.each(BREADCRUMBS)('%s binds the shared wheel handler', (rel) => {
    const source = readWebFile(rel)
    // Precondition: if the strip stops hiding its scrollbar the rule below is
    // no longer load-bearing, and this test should be revisited rather than
    // left to pass vacuously.
    expect(hidesScrollbar(source)).toBe(true)
    expect(source).toMatch(WHEEL_BINDING)
  })

  it('the binding regex can fail (not a tautology)', () => {
    const source = readWebFile(BREADCRUMBS[0]).replace('@wheel="onHorizontalWheel"', '')
    expect(hidesScrollbar(source)).toBe(true)
    expect(source).not.toMatch(WHEEL_BINDING)
  })
})
