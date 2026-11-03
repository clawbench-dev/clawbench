import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source-contract guard: the terminal's scrollbar must be as thick as every
 * other scrollbar in the app.
 *
 * xterm paints its own bar (`div.xterm-scrollbar` / `.scrollbar.vertical`), so
 * the shared `::-webkit-scrollbar` rules in `web/css/scrollbar.css` never reach
 * it — the terminal has to restate both width tiers in its own scoped style
 * block. Restating them is exactly where they drift (it used to be a 2px
 * hairline while the rest of the app was 10px), so the invariant is pinned
 * against the shared file rather than against a literal: change the global
 * width and this test tells you the terminal is now the odd one out.
 *
 * jsdom has no CSS engine, so the geometry cannot be observed from a mounted
 * component; the source is the only place to read it (same pattern as
 * wideDockIconSize.css.test.ts).
 */

const cssDir = join(__dirname, '..', '..', '..', 'css')
const globalScrollbarCss = readFileSync(join(cssDir, 'scrollbar.css'), 'utf8')
const terminalVue = readFileSync(
  join(__dirname, '..', 'terminal', 'TerminalPanelContent.vue'),
  'utf8',
)

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Body of `@media (<query>) { … }`, brace-matched. Empty when absent. */
function mediaBlock(css: string, query: string): string {
  const at = css.indexOf(`@media (${query})`)
  if (at < 0) return ''
  const open = css.indexOf('{', at)
  if (open < 0) return ''
  let depth = 0
  for (let i = open; i < css.length; i++) {
    if (css[i] === '{') depth++
    else if (css[i] === '}') {
      depth--
      if (depth === 0) return css.slice(open + 1, i)
    }
  }
  return ''
}

/** Every `width` value declared inside `css` (`!important` stripped).
 *  The prefix class keeps `scrollbar-width: thin` (Firefox-only tier) out —
 *  it is a different property, not a bar width. */
function widths(css: string): string[] {
  return [...stripComments(css).matchAll(/(?:[\s;{}]|^)width:\s*([^;]+);/g)]
    .map(m => m[1].replace(/!important/g, '').trim())
}

/** Declarations of the first rule that starts with `selector`. */
function ruleDecls(css: string, selector: string): string {
  const at = css.indexOf(selector)
  expect(at, `${selector} rule must exist`).toBeGreaterThanOrEqual(0)
  const open = css.indexOf('{', at)
  const close = css.indexOf('}', open)
  return css.slice(open + 1, close)
}

const globalCoarseBlock = mediaBlock(globalScrollbarCss, 'pointer: coarse')
const globalBaseWidths = widths(globalScrollbarCss.replace(globalCoarseBlock, ''))
const globalCoarseWidths = widths(globalCoarseBlock)

describe('terminal scrollbar width', () => {
  it('has a single, whole-pixel width tier in the shared scrollbar file', () => {
    // Guard the reference values themselves: the equality below is only
    // meaningful if scrollbar.css really carries one width per tier.
    expect(globalBaseWidths).toEqual(['10px'])
    expect(globalCoarseWidths).toEqual(['4px'])
  })

  it('matches the app-wide scrollbar width for mouse / trackpad', () => {
    const bar = ruleDecls(
      terminalVue,
      '.terminal-container :deep(.xterm-scrollable-element > .scrollbar.vertical)',
    )
    expect(widths(bar)).toContain(globalBaseWidths[0])
  })

  it('keeps the slider as wide as the bar it rides in', () => {
    const slider = ruleDecls(
      terminalVue,
      '.terminal-container :deep(.xterm-scrollable-element > .scrollbar > .slider)',
    )
    expect(widths(slider)).toContain(globalBaseWidths[0])
  })

  it('drops to the same 4px tier on touch / pen', () => {
    const coarse = mediaBlock(terminalVue, 'pointer: coarse')
    const terminalCoarse = widths(coarse)
    expect(terminalCoarse.length, 'the coarse override must exist').toBeGreaterThan(0)
    for (const w of terminalCoarse) expect(w).toBe(globalCoarseWidths[0])
  })

  it('does not scale with the UI', () => {
    // scrollbar.css deliberately keeps the bar at browser-chrome size; a
    // `calc(N * var(--dp))` here would make the terminal's bar grow with the
    // UI scale while every other bar stays put — the exact drift this guards.
    const bar = ruleDecls(
      terminalVue,
      '.terminal-container :deep(.xterm-scrollable-element > .scrollbar.vertical)',
    )
    expect(widths(bar).join(' ')).not.toMatch(/var\(--dp\)/)
    expect(widths(mediaBlock(terminalVue, 'pointer: coarse')).join(' ')).not.toMatch(/var\(--dp\)/)
  })
})
