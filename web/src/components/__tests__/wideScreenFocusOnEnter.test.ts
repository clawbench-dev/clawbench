import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Regression guard: entering wide-screen must focus the pane the user was
 * actually working in.
 *
 * The bug: the isWideScreen watcher first rewrote `activeTab` to a left-column
 * tab (via resolveLeftTabOnEnter / switchLeftTab) and THEN fed that same,
 * already-rewritten value to resolveActivePaneOnEnter. Its `chat` branch tests
 * `currentActiveTab === 'chat'`, which can no longer be true — so focus landed
 * on the left pane on EVERY entry into wide-screen. Because the chat shortcuts
 * are gated on `activePane === 'right'` (chatShortcutActive in App.vue),
 * Ctrl+K / Ctrl+U / Ctrl+← were dead from launch until the user clicked the
 * chat pane. Measured with a real server + browser: ignored before clicking,
 * claimed after.
 *
 * The fix resolves focus from the tab captured BEFORE the rewrite. This asserts
 * the captured value is what reaches the resolver, so a later reorder cannot
 * silently reintroduce the dead-shortcut state.
 *
 * Source-level because App.vue is a single large component with no mount-level
 * test (same approach as unreadNoAutoClear.test.ts / projectSwitchPanelGuard).
 */

/** Strip comments so an assertion cannot be satisfied by prose alone. */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/\/\/.*$/gm, '')           // line comments, whole-line or trailing
}

const code = stripComments(readWebFile('src/App.vue'))

/** The body of the isWideScreen watcher (brace-matched from its `watch(` call). */
function wideScreenWatcherBody(src: string): string {
  const at = src.indexOf('watch(isWideScreen,')
  if (at === -1) throw new Error('watch(isWideScreen, ...) not found in App.vue')
  const open = src.indexOf('{', at)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return src.slice(open, i + 1)
    }
  }
  throw new Error('unbalanced braces in the isWideScreen watcher')
}

describe('wide-screen entry focuses the pane the user came from', () => {
  const body = wideScreenWatcherBody(code)

  it('captures the entry tab before activeTab is rewritten', () => {
    const captureAt = body.indexOf('const enteredTab = activeTab.value')
    expect(captureAt, 'the entry tab is no longer captured').toBeGreaterThan(-1)

    // The capture must happen before the rewrite. `switchLeftTab` and the
    // `activeTab.value = next` assignment are the two writers that destroy the
    // 'chat' value the resolver needs.
    const switchAt = body.indexOf('switchLeftTab(next)')
    const assignAt = body.indexOf('activeTab.value = next')
    expect(switchAt, 'switchLeftTab(next) missing').toBeGreaterThan(-1)
    expect(assignAt, 'activeTab.value = next missing').toBeGreaterThan(-1)
    expect(captureAt, 'capture must precede switchLeftTab').toBeLessThan(switchAt)
    expect(captureAt, 'capture must precede the activeTab rewrite').toBeLessThan(assignAt)
  })

  it('feeds the captured tab to the pane resolver', () => {
    // The exact regression: passing the rewritten `activeTab.value` instead,
    // which can never be 'chat' at that point.
    expect(body).toMatch(/resolveActivePaneOnEnter\(enteredTab\)/)
    expect(body).not.toMatch(/resolveActivePaneOnEnter\(activeTab\.value\)/)
  })
})
