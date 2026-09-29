import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards for the shared theme-picker row styling.
 *
 * Both pickers (app-header quick theme picker + terminal toolbar theme picker)
 * are teleported to <body>, so their row rules cannot be scoped and live in
 * `src/assets/theme-picker.css`. The regression these tests exist for: the rows
 * used to paint themselves with the *previewed theme's own* background and
 * foreground, which turned the menu into a stack of unrelated solid blocks that
 * clashed with the active theme — and left light themes with unreadable text.
 *
 * A pure-CSS bug like this cannot be caught by mounting the components, so the
 * assertions are made against the stylesheet source.
 */

/** Strip comments so prose mentioning a property cannot satisfy an assertion. */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

const sharedCss = stripComments(readWebFile('src/assets/theme-picker.css'))

/** Declarations of the first rule whose selector list contains `selector`. */
function declarationsFor(css: string, selector: string): string {
  const clean = stripComments(css)
  for (const m of clean.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const selectors = m[1].split(',').map(s => s.trim())
    if (selectors.includes(selector)) return m[2]
  }
  return ''
}

/**
 * Collect rules from every *unscoped* `<style>` block of a component.
 * A `.vue` file can hold several style blocks (scoped + global); reading only
 * the first would miss the global one.
 */
function readGlobalStyleBlocks(relPath: string): string {
  const src = readWebFile(relPath)
  const blocks = [...src.matchAll(/<style([^>]*)>([\s\S]*?)<\/style>/g)]
  return blocks
    .filter(b => !/\bscoped\b/.test(b[1]))
    .map(b => b[2])
    .join('\n')
}

const APP_HEADER = 'src/components/common/AppHeader.vue'
const TERMINAL_PANEL = 'src/components/terminal/TerminalPanelContent.vue'

describe('theme picker shared stylesheet', () => {
  it('keeps the row surface neutral instead of painting the previewed theme', () => {
    const decls = declarationsFor(sharedCss, '.theme-item')

    // The row background must not be driven by the previewed theme's colours.
    expect(decls).not.toContain('--tterm-preview-bg')
    expect(decls).not.toContain('--tterm-preview-fg')
    // Text uses the CURRENT theme's token, so it stays readable everywhere.
    expect(decls).toContain('color: var(--text-primary)')
  })

  it('does not tint the active or hover row with the previewed theme', () => {
    // Every rule that sets a row background must stay theme-neutral. A leftover
    // `background: var(--tterm-preview-bg)` on :hover or .active was exactly the
    // defect, and only checking the base rule would miss it.
    const clean = stripComments(sharedCss)
    const rowRules = [...clean.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
      .filter(m => /\.theme-item\b/.test(m[1]) && /\bbackground\s*:/.test(m[2]))
    expect(rowRules.length).toBeGreaterThan(0)
    for (const m of rowRules) {
      expect(m[2], `selector: ${m[1].trim()}`).not.toContain('--tterm-preview-')
    }
  })

  it('previews each theme through a swatch and the accent-tinted indicator', () => {
    // The two carriers that replace the old full-row tinting must still exist.
    expect(sharedCss).toContain('.theme-swatch')
    expect(declarationsFor(sharedCss, '.theme-swatch')).toContain('--tterm-preview-bg')
    expect(declarationsFor(sharedCss, '.theme-item-base-icon')).toContain('--tterm-preview-accent')
  })

  it('gives the swatch an inset hairline so near-white/near-black swatches stay visible', () => {
    // Without the inset ring a #ffffff swatch vanishes on a light panel and a
    // #000000 one on a dark panel.
    const decls = declarationsFor(sharedCss, '.theme-swatch')
    expect(decls).toContain('box-shadow')
    expect(decls).toContain('--text-primary')
  })

  it('declares the auto swatch after the base swatch so the split wins', () => {
    // Both selectors are single-class, so source order decides. If the auto rule
    // came first, `background` on the base rule would override the gradient.
    const baseIdx = sharedCss.indexOf('.theme-swatch {')
    const autoIdx = sharedCss.indexOf('.theme-swatch--auto')
    expect(baseIdx).toBeGreaterThanOrEqual(0)
    expect(autoIdx).toBeGreaterThan(baseIdx)
    expect(declarationsFor(sharedCss, '.theme-swatch--auto')).toContain('linear-gradient')
  })

  it('carries selection on the rail + tint + weight, with no check mark', () => {
    // The rail is the load-bearing signal: measured across all 36 app themes it
    // stays at >=2.04 contrast, whereas the 12% tint alone drops to 1.09 (below
    // the ~1.20 a user can reliably notice) in 19 of 36 themes. Removing the
    // rail and keeping only the tint would make the selected row ambiguous.
    expect(declarationsFor(sharedCss, '.theme-item.active::before')).toContain('var(--accent-color)')
    expect(declarationsFor(sharedCss, '.theme-item.active')).toContain('color-mix')
    expect(declarationsFor(sharedCss, '.theme-item.active .theme-item-name')).toContain('font-weight')

    // The check mark was the fourth signal for one state and is gone for good.
    expect(sharedCss).not.toContain('theme-item-check')
  })
})

describe('theme picker rows in both callers', () => {
  it.each([
    ['app header', APP_HEADER],
    ['terminal panel', TERMINAL_PANEL],
  ])('%s renders a swatch per row and no longer defines row styling locally', (_name, path) => {
    const src = readWebFile(path)

    // Each row carries a colour swatch (auto rows use the split variant).
    expect(src).toContain('class="theme-swatch')
    expect(src).toMatch(/theme-swatch--auto/)

    // The duplicated global copies were removed in favour of the shared file,
    // which is what kept the two pickers drifting apart.
    const globalCss = stripComments(readGlobalStyleBlocks(path))
    expect(globalCss).not.toContain('.theme-item + .theme-item')
    expect(globalCss).not.toMatch(/\.theme-item\s*\{/)
    expect(globalCss).not.toMatch(/\.theme-item\.active/)
  })

  it('does not set the removed --tterm-preview-fg custom property', () => {
    // The row no longer consumes the previewed theme's foreground colour, so
    // producing it is dead weight that invites the old behaviour back.
    for (const path of [APP_HEADER, TERMINAL_PANEL]) {
      expect(readWebFile(path)).not.toContain('--tterm-preview-fg')
    }
  })

  it('renders no check mark in either picker', () => {
    for (const path of [APP_HEADER, TERMINAL_PANEL]) {
      const src = readWebFile(path)
      expect(src).not.toContain('theme-item-check')
    }
  })
})
