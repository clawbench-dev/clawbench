import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'
import { buildSwaggerSrcdoc } from '@/utils/swaggerHtml.ts'

/**
 * Guard: scrollbars keep the themed bar (no fallback to the native one with its
 * ▲/▼ arrow buttons) and the width has exactly ONE definition.
 *
 * PART 1 — the arrow-button regression.
 * Chromium 121+ changed how the standard `scrollbar-color` / `scrollbar-width`
 * properties interact with the legacy `::-webkit-scrollbar-*` pseudo-elements:
 * setting either one makes the engine IGNORE every webkit scrollbar rule for
 * that element. In `base.css` that silently undid two things at once:
 *
 *   - the custom bar's width (reverted to the ~15px native bar), and
 *   - `::-webkit-scrollbar-button { display: none }`, so the up/down arrow
 *     buttons came back — the exact symptom reported.
 *
 * The fix confines the standard properties to `@supports not
 * selector(::-webkit-scrollbar)` so they only reach engines without webkit
 * scrollbar pseudo-elements (Firefox), while Chromium keeps the custom bar.
 *
 * PART 2 — one definition.
 * The bar used to be copy-pasted into three documents (the app sheet, the
 * OpenAPI iframe, the markdown export) and they drifted: changing the width
 * meant editing three files and three sets of guards. It now lives ONCE in
 * `css/scrollbar.css`, which each document pulls in:
 *   - base.css            @import (also reaches the share SPA, which imports it)
 *   - swaggerHtml.ts      ?raw, inlined into the iframe srcdoc
 *   - exportMarkdownHtml  ?raw, inlined into the export
 * A CSS *variable* cannot be the single source: the iframe and the file://
 * export never see the app's :root, so a `--scrollbar-size` token would resolve
 * to nothing there and the width would silently drop out. Inlining the file is
 * what makes the convergence real.
 *
 * jsdom has no CSS engine, so this is a source-contract check.
 */

/** The gate that keeps the standard properties away from Chromium. */
const MARKER = '@supports not selector(::-webkit-scrollbar)'

/** The single source of the bar. */
const SHARED = 'css/scrollbar.css'

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

/** The full invariant for a document that carries the webkit scrollbar rules. */
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

describe('the shared scrollbar sheet is the single source', () => {
  const shared = readWebFile(SHARED)

  it('carries the full bar and gates the standard properties', () => {
    assertScrollbarSite(shared, SHARED)
  })

  it('declares a desktop 8px bar plus a coarse-pointer 4px override', () => {
    // Two widths by INPUT DEVICE, not by host or viewport: `(pointer: coarse)`
    // is the only discriminator that travels with the stylesheet (the OpenAPI
    // iframe and the file:// export carry no host info and no reliable viewport
    // width, but the browser evaluates the media feature per-document). It is
    // also the axis the resize divider uses for its touch grab band.
    const clean = stripComments(shared)
    const coarseAt = clean.indexOf('@media (pointer: coarse)')
    expect(coarseAt, 'a (pointer: coarse) block must exist').toBeGreaterThanOrEqual(0)

    // Base rule: desktop 8px, declared exactly once and OUTSIDE the media block.
    const beforeMedia = clean.slice(0, coarseAt)
    const base = beforeMedia.match(/::-webkit-scrollbar\s*\{[^}]*\}/g) || []
    expect(base, 'the shared sheet must define the base rule exactly once').toHaveLength(1)
    expect(base[0]).toMatch(/(width):\s*(?:calc\(\s*8\s*\*\s*var\(--dp\)\s*\)|8px)/)
    expect(base[0]).toMatch(/(height):\s*(?:calc\(\s*8\s*\*\s*var\(--dp\)\s*\)|8px)/)

    // Coarse override: 4px on both axes, inside the media block.
    const coarse = clean.slice(coarseAt)
    const override = coarse.match(/::-webkit-scrollbar\s*\{[^}]*\}/)
    expect(override, 'the coarse block must override the base rule').not.toBeNull()
    expect(override![0]).toMatch(/(width):\s*(?:calc\(\s*4\s*\*\s*var\(--dp\)\s*\)|4px)/)
    expect(override![0]).toMatch(/(height):\s*(?:calc\(\s*4\s*\*\s*var\(--dp\)\s*\)|4px)/)
  })

  it('takes its colours from variables with fallbacks, so each document keeps its palette', () => {
    // Every consumer is a standalone document that never sees the app's :root,
    // so each variable the shared sheet reads MUST have a fallback — otherwise
    // the declaration drops out and the bar reverts to the native one.
    const clean = stripComments(shared)
    for (const decl of clean.match(/var\(--scrollbar-[a-z-]+[^)]*\)/g) || []) {
      expect(decl, `${decl} must carry a fallback`).toMatch(/var\(--scrollbar-[a-z-]+,\s*[^)]+\)/)
    }
  })

  it('is imported by base.css (the app + share SPA)', () => {
    // `@import` must precede every rule, or the browser drops the whole sheet —
    // so a regression that moves it below another rule would silently remove
    // the bar. Assert it sits before the first declaration block.
    const clean = stripComments(readWebFile('css/base.css'))
    const importAt = clean.indexOf('@import')
    expect(importAt, 'base.css must import the shared sheet').toBeGreaterThanOrEqual(0)
    expect(clean.slice(importAt)).toMatch(/^@import\s+['"]\.\/scrollbar\.css['"]\s*;/)
    const firstBlockAt = clean.search(/[^{}@]+?\{/)
    expect(importAt, 'the @import must be the first rule in base.css').toBeLessThan(firstBlockAt)
  })

  it.each([
    ['swaggerHtml', 'src/utils/swaggerHtml.ts', 'scrollbarCss'],
    ['exportMarkdownHtml', 'src/utils/exportMarkdownHtml.ts', 'scrollbarCss'],
    ['htmlPreview', 'src/utils/htmlPreview.ts', 'scrollbarCss'],
  ])('%s inlines the shared sheet via ?raw', (_label, rel, ident) => {
    const src = readWebFile(rel)
    expect(src, `${rel} must ?raw-import the shared sheet`).toMatch(
      /import\s+\w+\s+from\s+['"][^'"]*css\/scrollbar\.css\?raw['"]/,
    )
    expect(src, `${rel} must use the imported sheet`).toContain(ident)
  })
})

describe('no document re-declares the bar', () => {
  it.each([
    ['base.css', 'css/base.css'],
    ['swaggerHtml', 'src/utils/swaggerHtml.ts'],
    ['exportMarkdownHtml', 'src/utils/exportMarkdownHtml.ts'],
    ['htmlPreview', 'src/utils/htmlPreview.ts'],
  ])('%s keeps no local ::-webkit-scrollbar base rule', (_label, rel) => {
    // A local copy is exactly how the copies drifted apart; the shared sheet
    // must stay the only place the width is written.
    const clean = stripComments(readWebFile(rel))
    expect(clean, `${rel} must not re-declare the webkit scrollbar geometry`).not.toMatch(
      /::-webkit-scrollbar\s*\{[^}]*\b(width|height)\s*:/,
    )
  })
})

describe('the HTML preview adopts the app bar', () => {
  it('installs the shared sheet into the previewed document', () => {
    // The preview loads the file by URL as a separate document, so it cannot
    // link the app's stylesheets. Without this it shows the browser's native
    // bar (with arrow buttons) instead of the app's.
    const src = readWebFile('src/utils/htmlPreview.ts')
    expect(src, 'installPreviewScrollbar must be exported').toContain('export function installPreviewScrollbar')
    expect(src, 'the sheet must be inserted into the preview document').toMatch(
      /insertBefore\(\s*style\s*,\s*doc\.head\.firstChild\s*\)/,
    )
  })

  it('inserts the sheet FIRST so the document\'s own scrollbar rules win', () => {
    // Contract: the app bar applies only where the previewed document has not
    // expressed its own preference. Inserting first (not appending) means any
    // rule the document defines — inline, <style>, or linked — comes later in
    // source order and overrides ours.
    //
    // Both installers share one helper (installPreviewStyle), so the assertion
    // has to be about the ARGUMENT the scrollbar installer passes, not about a
    // bare `doc.head.appendChild` in this file: the helper legitimately appends
    // for the search sheet, which is meant to win. Pinning the whole file
    // against appendChild would forbid that legitimate branch; pinning the call
    // site is what actually says "the scrollbar sheet goes first".
    const src = readWebFile('src/utils/htmlPreview.ts')
    const call = src.match(/installPreviewStyle\([^)]*\)/)
    expect(call, 'installPreviewScrollbar must delegate to the shared helper').not.toBeNull()
    expect(call![0], 'the scrollbar sheet must keep the default first=true').not.toMatch(/\bfalse\b/)
  })

  it('is wired into both preview surfaces and is idempotent', () => {
    // Both the full-screen viewer and the quick code-link pane render a preview
    // iframe; both must call the installer on load.
    for (const rel of ['src/components/file/FileViewer.vue', 'src/components/file/HtmlPreviewBody.vue']) {
      const src = readWebFile(rel)
      expect(src, `${rel} must call installPreviewScrollbar`).toContain('installPreviewScrollbar(')
      expect(src, `${rel} must bind it to the iframe load event`).toMatch(/@load="on[A-Za-z]*Load"/)
    }
    // A reload must not stack copies of the sheet. The attribute is a parameter
    // of the shared helper now, so the guard follows it there.
    expect(readWebFile('src/utils/htmlPreview.ts')).toMatch(
      /querySelector\(`style\[\$\{attr\}\]`\)/,
    )
  })
})

describe('the rendered documents keep the custom bar without arrows', () => {
  it('Swagger preview srcdoc inlines the shared bar and gates the standard properties', () => {
    const html = buildSwaggerSrcdoc('{"openapi":"3.0.0"}')
    assertScrollbarSite(html, 'swaggerHtml')
    // The inlined sheet, not a copy: the width and the arrow-hiding rule must
    // be present in the output the iframe actually renders.
    expect(html).toMatch(/::-webkit-scrollbar\s*\{\s*width:\s*8px/)
    expect(html).toContain('--scrollbar-thumb: #c1c1c1')
  })

  it('Swagger preview keeps its per-document colours', () => {
    const html = buildSwaggerSrcdoc('{"openapi":"3.0.0"}', false, '#484f58', '#21262d')
    expect(html).toContain('#484f58')
    expect(html).toContain('#21262d')
  })

  it('LoginView server list gates its scrollbar-width: thin', () => {
    assertAllStandardPropsGated(readWebFile('src/components/LoginView.vue'), 'LoginView.vue')
  })
})

describe('the width is uniform and has no token', () => {
  it('no scrollbar-size token survives anywhere', () => {
    // A token is exactly how a second, machine-dependent width would come back
    // (and it cannot cross into the iframe / export anyway).
    for (const [label, text] of [
      ['variables.css', readWebFile('css/variables.css')],
      [SHARED, readWebFile(SHARED)],
      ['base.css', readWebFile('css/base.css')],
    ] as const) {
      expect(stripComments(text), `${label}: --scrollbar-size must not exist`).not.toContain('--scrollbar-size')
    }
  })

  it('the only webkit scrollbar widths in the sources are the shared pair', () => {
    // Any width outside the shared sheet means the copies are back. `display:
    // none` rules (hidden scrollbars) are not widths and are fine. The
    // lookbehind keeps `scrollbar-width` (the Firefox property) from matching
    // as a width.
    const widths: Array<[string, string]> = []
    for (const rel of [
      SHARED,
      'css/base.css',
      'src/utils/swaggerHtml.ts',
      'src/utils/exportMarkdownHtml.ts',
      'src/utils/htmlPreview.ts',
    ]) {
      const clean = stripComments(readWebFile(rel))
      for (const m of clean.matchAll(/::-webkit-scrollbar[^{]*\{[^}]*(?<![-\w])width\s*:\s*([^;}]+)/g)) {
        widths.push([rel, m[1].trim()])
      }
    }
    // Desktop base + coarse-pointer override, both in the shared sheet.
    expect(widths, 'only the shared sheet may declare the webkit scrollbar width').toEqual([
      [SHARED, '8px'],
      [SHARED, '4px'],
    ])
  })
})
