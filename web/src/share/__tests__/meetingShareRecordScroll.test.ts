import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: a share view must own a DEFINITE height, or its lists cannot scroll.
 *
 * The share shell pins the document: share.html sets `html, body { height:
 * 100dvh; overflow: hidden !important }`, and `.share-root` (ShareRoot.vue) is
 * `height: 100%` but a plain BLOCK — not a flex container. So a view mounted in
 * it only gets a bounded height from its own `height: 100%`; a `flex: 1` basis
 * never applies.
 *
 * The meeting record relied on `flex: 1`. Its column therefore sized itself
 * from its content — measured 2425px tall inside an 800px window — and
 * `.share-meeting__body`'s `overflow-y: auto` had no bounded height to scroll
 * in. The document could not scroll either (body overflow is hidden), so a long
 * transcript / minutes / chat showed its first screenful only, with no way to
 * reach the rest. Reported as "分享出去的会议记录只看到前面一小部分，不能滚动".
 *
 * jsdom has no layout engine, so these read the stylesheets: the contract is
 * the declarations, and the numbers above come from a real Chromium
 * measurement of the same box chain.
 */

/** Strip comments so prose about a property is never mistaken for the property. */
const decls = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '')

/** Declarations of the rule whose selector is exactly `selector`. */
function ruleOf(css: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  // Selector at the start of a line: it may follow `<style scoped>` or a
  // stripped comment, so `^` / `}` alone would miss the first rule.
  const m = css.match(new RegExp(`(?:^|\\n)[ \\t]*${escaped}[ \\t]*\\{([^}]*)\\}`))
  if (!m) throw new Error(`rule not found: ${selector}`)
  return m[1]
}

function has(block: string, prop: string, value: string): boolean {
  const escaped = value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`(?:^|;)\\s*${prop}\\s*:\\s*${escaped}\\s*(?:;|$)`).test(block)
}

const meetingCss = decls(readWebFile('src/share/MeetingShareDetail.vue'))
const chromeCss = decls(readWebFile('css/share-chrome.css'))

describe('meeting share record: the page scrolls', () => {
  it('gives the record column a definite height (not a flex basis)', () => {
    const root = ruleOf(meetingCss, '.share-meeting')
    expect(root, 'the record column must fill .share-root, which is a block').toMatch(
      /(?:^|;)\s*height\s*:\s*100%/,
    )
    // `flex: 1` alone is the regression: it resolves to nothing under a block
    // parent, so the column grows past the viewport and gets clipped.
    expect(root).not.toMatch(/(?:^|;)\s*flex\s*:\s*1\s*;/)
  })

  it('makes the tab body the scroller inside that height', () => {
    const body = ruleOf(meetingCss, '.share-meeting__body')
    expect(has(body, 'flex', '1'), 'body must take the height left by the chrome').toBe(true)
    expect(has(body, 'min-height', '0'), 'without min-height:0 a flex item refuses to shrink').toBe(true)
    expect(has(body, 'overflow-y', 'auto'), 'the body is where a long record scrolls').toBe(true)
  })

  it('matches the other share views, which also own height:100%', () => {
    // `.share-view` is the root of the file / folder / session shares. Parity
    // matters: all four mount as siblings in `.share-root`.
    expect(has(ruleOf(chromeCss, '.share-view'), 'height', '100%')).toBe(true)
  })
})
