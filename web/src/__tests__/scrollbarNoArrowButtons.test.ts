import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'
import { buildSwaggerSrcdoc } from '@/utils/swaggerHtml.ts'

/**
 * Guard: scrollbars must keep the 4px themed bar and must NOT fall back to the
 * native one with its ▲/▼ arrow buttons.
 *
 * Chromium 121+ changed how the standard `scrollbar-color` / `scrollbar-width`
 * properties interact with the legacy `::-webkit-scrollbar-*` pseudo-elements:
 * setting either one makes the engine IGNORE every webkit scrollbar rule for
 * that element. In `base.css` that silently undid two things at once:
 *
 *   - the `width: 4px` custom bar (reverted to the ~15px native bar), and
 *   - `::-webkit-scrollbar-button { display: none }`, so the up/down arrow
 *     buttons came back — the exact symptom reported.
 *
 * The fix confines the standard properties to `@supports not
 * selector(::-webkit-scrollbar)` so they only reach engines without webkit
 * scrollbar pseudo-elements (Firefox), while Chromium keeps the custom bar.
 *
 * jsdom has no CSS engine, so this is a source-contract check. The invariant is
 * "no standard scrollbar property may live outside that gate" (except the
 * intentional `scrollbar-width: none`, which means 'no bar' either way) — which
 * is exactly what a future editor would break by pasting one back at the top
 * level. The same bug lived in the two generated-HTML helpers, which inline
 * their own copy of the scrollbar CSS, and in LoginView's server list.
 */

/** The gate that keeps the standard properties away from Chromium. */
const MARKER = '@supports not selector(::-webkit-scrollbar)'

/** Index just past the `}` closing the block whose `{` is at `open`. */
function matchingBraceEnd(text: string, open: number): number {
  let depth = 0
  for (let i = open; i < text.length; i++) {
    if (text[i] === '{') depth++
    else if (text[i] === '}' && --depth === 0) return i + 1
  }
  return -1
}

/** Drop comments so prose about the properties cannot satisfy/break the check. */
function stripComments(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '')
}

/** [start, end) ranges of every `@supports` gate block in `text`. */
function gateRanges(text: string): Array<[number, number]> {
  const ranges: Array<[number, number]> = []
  let i = 0
  for (;;) {
    const start = text.indexOf(MARKER, i)
    if (start === -1) break
    const open = text.indexOf('{', start + MARKER.length)
    if (open === -1) break
    const end = matchingBraceEnd(text, open)
    if (end === -1) break
    ranges.push([start, end])
    i = end
  }
  return ranges
}

/**
 * Every `scrollbar-color` / `scrollbar-width` declaration must sit inside a
 * gate. `scrollbar-width: none` is exempt: its whole point is to hide the bar,
 * so losing the webkit rules changes nothing.
 */
function assertAllStandardPropsGated(text: string, label: string) {
  const clean = stripComments(text)
  const ranges = gateRanges(clean)
  const re = /scrollbar-(color|width)\s*:\s*([^;}\n]+)/g
  const offenders: string[] = []
  let m: RegExpExecArray | null
  while ((m = re.exec(clean))) {
    if (m[1] === 'width' && /^(none|hidden)\b/.test(m[2].trim())) continue
    if (!ranges.some(([s, e]) => m!.index >= s && m!.index < e)) offenders.push(m[0].trim())
  }
  expect(offenders, `${label}: these must be inside "${MARKER}"`).toEqual([])
}

/** The full invariant for a site that carries its own webkit scrollbar rules. */
function assertScrollbarSite(text: string, label: string) {
  assertAllStandardPropsGated(text, label)

  // The gate must actually carry the theming, or Firefox loses it silently.
  const ranges = gateRanges(stripComments(text))
  expect(ranges.length, `${label}: missing "${MARKER}" gate`).toBeGreaterThan(0)
  const gated = ranges.map(([s, e]) => stripComments(text).slice(s, e)).join('\n')
  expect(gated, `${label}: gate should carry scrollbar-color`).toContain('scrollbar-color')

  // The rule the gate is protecting — without it the arrows come back even
  // with the gate in place.
  expect(text, `${label}: ::-webkit-scrollbar-button must stay hidden`).toMatch(
    /::-webkit-scrollbar-button\s*{\s*display:\s*none/,
  )
}

describe('scrollbars keep the custom bar without arrow buttons', () => {
  it('base.css gates the standard properties so Chromium keeps ::-webkit-scrollbar', () => {
    assertScrollbarSite(readWebFile('css/base.css'), 'base.css')
  })

  it('Swagger preview srcdoc gates the standard properties', () => {
    assertScrollbarSite(buildSwaggerSrcdoc('{"openapi":"3.0.0"}'), 'swaggerHtml')
  })

  it('Markdown/HTML export gates the standard properties', () => {
    assertScrollbarSite(readWebFile('src/utils/exportMarkdownHtml.ts'), 'exportMarkdownHtml')
  })

  it('LoginView server list gates its scrollbar-width: thin', () => {
    assertAllStandardPropsGated(readWebFile('src/components/LoginView.vue'), 'LoginView.vue')
  })
})

/**
 * Guard: the bar is a UNIFORM thin 4px everywhere.
 *
 * It was briefly split (thick on mouse / wide-screen surfaces) to make it easier
 * to grab next to a pane divider, but that was reverted — one size keeps the UI
 * consistent across devices, and the divider conflict is handled on the divider
 * side instead (see SplitDivider.vue).
 *
 * The regression this catches is a reintroduced split: any second width for the
 * webkit scrollbar, or a stray `--scrollbar-size` token, means the bar silently
 * differs between machines again. Both the app CSS and the two generated-HTML
 * helpers (which inline their own copy) are checked.
 */
describe('scrollbar thickness is uniform', () => {
  it('base.css declares a single 4px bar on both axes', () => {
    const base = readWebFile('css/base.css')
    const decls = base.match(/::-webkit-scrollbar\s*\{[^}]*\}/g) || []
    expect(decls.length, '::-webkit-scrollbar base rule must exist').toBeGreaterThan(0)
    const first = decls[0]
    expect(first).toMatch(/width:\s*4px/)
    expect(first).toMatch(/height:\s*4px/)
  })

  it('no scrollbar-size token or media-query split survives anywhere', () => {
    // A token (or a second width rule) is exactly how the split would come back.
    for (const [label, text] of [
      ['variables.css', readWebFile('css/variables.css')],
      ['base.css', readWebFile('css/base.css')],
      ['swaggerHtml', buildSwaggerSrcdoc('{"openapi":"3.0.0"}')],
      ['exportMarkdownHtml', readWebFile('src/utils/exportMarkdownHtml.ts')],
    ] as const) {
      const clean = stripComments(text)
      expect(clean, `${label}: --scrollbar-size must not exist`).not.toContain('--scrollbar-size')
      // Any 12px scrollbar width would be the thick variant coming back.
      expect(clean, `${label}: no thick (12px) scrollbar`).not.toMatch(
        /::-webkit-scrollbar[^{]*\{[^}]*width:\s*12px/,
      )
    }
  })

  it('generated-HTML helpers keep the same uniform 4px bar', () => {
    for (const [label, text] of [
      ['swaggerHtml', buildSwaggerSrcdoc('{"openapi":"3.0.0"}')],
      ['exportMarkdownHtml', readWebFile('src/utils/exportMarkdownHtml.ts')],
    ] as const) {
      expect(text, `${label}: needs a 4px webkit bar`).toMatch(
        /::-webkit-scrollbar\s*\{\s*width:\s*4px/,
      )
    }
  })

  it('Firefox approximates the thin bar inside the webkit-support gate', () => {
    // Firefox has no px control — only auto|thin|none — so `thin` is the
    // closest match to 4px and must stay inside the gate.
    for (const [label, text] of [
      ['base.css', readWebFile('css/base.css')],
      ['swaggerHtml', buildSwaggerSrcdoc('{"openapi":"3.0.0"}')],
      ['exportMarkdownHtml', readWebFile('src/utils/exportMarkdownHtml.ts')],
    ] as const) {
      const gated = gateRanges(stripComments(text)).map(([s, e]) => stripComments(text).slice(s, e)).join('\n')
      expect(gated, `${label}: scrollbar-width: thin must be inside the gate`).toMatch(
        /scrollbar-width:\s*thin/,
      )
    }
  })
})
