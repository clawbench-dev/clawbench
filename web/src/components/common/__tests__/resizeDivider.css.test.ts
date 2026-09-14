import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Guard tests for the shared resize-divider stylesheet.
 *
 * WHY THIS FILE EXISTS
 * The two draggable pane separators (SplitDivider.vue, TocDock.vue) used to
 * carry copy-pasted copies of the line rules, and the copies drifted: the
 * expanded band narrowed its inner line to a crisp 2px centre stripe for
 * `:active`/`--dragging`, but the desktop-only `:hover` state was added later
 * and never got that rule. Hovering on a PC therefore left the line at
 * `inset: 0` — it filled the whole 12px band while the accent rule recoloured
 * it fully opaque, i.e. a solid 12px block. Touch never hovers, so it looked
 * correct there, which is exactly how the bug survived.
 *
 * The rules now live in one file. These tests pin the invariants that the
 * duplication broke:
 *   1. every trigger of a state declares the SAME declarations (byte-for-byte
 *      after whitespace normalisation), so a trigger cannot be tinted without
 *      also being narrowed;
 *   2. `:hover` is confined to a `(hover: hover)` block, so a touch tap cannot
 *      leave a sticky tint behind;
 *   3. both components import this file and neither re-declares the line rules.
 *
 * jsdom has no CSS engine, so these are source-sniffing tests — the same
 * pattern designTokens.css.test.ts and flashReducedMotion.css.test.ts use.
 */

/** Read a source file, probing both cwds (bare `vitest` runs from web/,
 *  scripts/vitest-run.sh runs from the repo root). */
function readFromRepo(relPath: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(relPath + ' not found from cwd: ' + process.cwd())
}

const css = readFromRepo('src/assets/resize-divider.css')

/** Comment-free copy for assertions that must not be fooled by prose (the file
 *  documents the very selectors it defines). */
const cssCode = css.replace(/\/\*[\s\S]*?\*\//g, '')

function norm(s: string): string {
  return s.replace(/\s+/g, ' ').trim()
}

/**
 * Parse `selector { declarations }` pairs. At-rule preludes are skipped so the
 * rules nested in a `@media` block are captured under their own clean
 * selector — which is what the parity assertions compare.
 */
function parseRules(source: string): Array<{ selector: string; decls: string }> {
  const rules: Array<{ selector: string; decls: string }> = []
  const re = /([^{}]+)\{([^{}]*)\}/g
  let m: RegExpExecArray | null
  while ((m = re.exec(source)) !== null) {
    const selector = m[1].trim()
    if (selector.startsWith('@')) continue
    rules.push({ selector, decls: m[2].trim() })
  }
  return rules
}

const RULES = parseRules(cssCode)

/** Every declaration block whose selector list contains `selector`. */
function declsOf(selector: string): string[] {
  const wanted = norm(selector)
  return RULES
    .filter((r) => norm(r.selector).split(',').map((s) => s.trim()).includes(wanted))
    .map((r) => norm(r.decls))
}

/**
 * Assert every selector resolves to one identical declaration block, and return
 * that normalised block. This is the anti-drift assertion: a trigger added
 * without the others (the exact shape of the hover bug) fails here.
 */
function expectIdenticalDecls(...selectors: string[]): string {
  const [first, ...rest] = selectors
  const base = declsOf(first)
  expect(base, `${first} must be declared exactly once`).toHaveLength(1)
  for (const selector of rest) {
    expect(declsOf(selector), `${selector} must declare the same as ${first}`).toEqual(base)
  }
  return base[0]
}

describe('resize-divider.css — resting line', () => {
  it('fills its host so the line thickness is the host width', () => {
    // Centring a 1px line inside a wider host (3px for SplitDivider under
    // `pointer: coarse`, 6px for TocDock) leaves an uneven gap either side that
    // reads as stray padding. `inset: 0` also keeps it on the pixel grid.
    const resting = declsOf('.resize-divider__line')
    expect(resting).toHaveLength(1)
    expect(resting[0]).toContain('inset: 0')
    expect(resting[0]).not.toContain('50%')
    expect(resting[0]).not.toContain('transform')
    // A fixed thickness would reintroduce the gap on the wider hosts.
    expect(resting[0]).not.toMatch(/\b(width|height):\s*1px/)
  })
})

describe('resize-divider.css — expanded state cannot drift between triggers', () => {
  // The bug this file replaces: `:active`/`--dragging` narrowed the line, and
  // `:hover` was bolted on separately with the colour but not the geometry.
  it('keeps the accent colour identical across :active / --expanded / :hover', () => {
    const decls = expectIdenticalDecls(
      '.resize-divider:active .resize-divider__line',
      '.resize-divider--expanded .resize-divider__line',
      '.resize-divider:hover .resize-divider__line',
    )
    expect(decls).toBe(norm('background: var(--accent-color, #0066cc);'))
  })

  it('keeps the vertical centre stripe identical across :active / --expanded / :hover', () => {
    const decls = expectIdenticalDecls(
      ".resize-divider[aria-orientation='vertical']:active .resize-divider__line",
      ".resize-divider[aria-orientation='vertical'].resize-divider--expanded .resize-divider__line",
      ".resize-divider[aria-orientation='vertical']:hover .resize-divider__line",
    )
    // 2px rather than 1px: a 12px band cannot centre a 1px line on a whole
    // pixel — (12 - 1) / 2 = 5.5, (12 - 2) / 2 = 5.
    expect(decls).toContain('calc((100% - 2px) / 2)')
    expect(decls).toContain('width: 2px')
    // Releasing the trailing edge is what lets the width shrink away from the
    // leading inset; without it the stripe stays full width.
    expect(decls).toContain('right: auto')
  })

  it('keeps the horizontal centre stripe identical across :active / --expanded / :hover', () => {
    const decls = expectIdenticalDecls(
      ".resize-divider[aria-orientation='horizontal']:active .resize-divider__line",
      ".resize-divider[aria-orientation='horizontal'].resize-divider--expanded .resize-divider__line",
      ".resize-divider[aria-orientation='horizontal']:hover .resize-divider__line",
    )
    expect(decls).toContain('calc((100% - 2px) / 2)')
    expect(decls).toContain('height: 2px')
    expect(decls).toContain('bottom: auto')
  })

  it('selects the centring axis from aria-orientation', () => {
    // The two consumers name their own orientation inversely (SplitDivider's
    // 'horizontal' means side-by-side; TocDock's 'vertical' means a vertical
    // line), so the ARIA attribute — which always describes the separator line
    // itself — is the only reliable signal.
    expect(cssCode).toMatch(/\[aria-orientation='vertical'\]/)
    expect(cssCode).toMatch(/\[aria-orientation='horizontal'\]/)
  })
})

describe('resize-divider.css — hover is gated on a real pointer', () => {
  it('declares every :hover rule inside a (hover: hover) block', () => {
    // A touch tap leaves a sticky `:hover` behind; ungated, it would keep the
    // line accent-coloured after the drag ends.
    const marker = '@media (hover: hover)'
    expect(cssCode).toContain(marker)
    const hoverBlock = cssCode.slice(cssCode.indexOf(marker))
    expect(hoverBlock).toContain('.resize-divider:hover .resize-divider__line')
    expect(cssCode.slice(0, cssCode.indexOf(marker))).not.toContain(':hover')
  })
})

describe('resize-divider.css — single source of truth', () => {
  const CONSUMERS = ['src/components/common/SplitDivider.vue', 'src/components/file/TocDock.vue']

  it('is imported by both divider components', () => {
    for (const rel of CONSUMERS) {
      expect(readFromRepo(rel), `${rel} must import the shared stylesheet`)
        .toContain("@import '@/assets/resize-divider.css'")
    }
  })

  it('is not re-declared inside either component', () => {
    // Re-adding line rules to a component is how the two copies drifted apart
    // in the first place — the shared file must stay the only definition.
    for (const rel of CONSUMERS) {
      const src = readFromRepo(rel)
      const start = src.indexOf('<style scoped>')
      const scoped = src.slice(start, src.indexOf('</style>', start))
      expect(scoped, `${rel} must not re-declare the shared line rules`)
        .not.toContain('resize-divider__line')
    }
  })

  /**
   * The HOST band geometry (width/offset + translucent tint) can't be shared:
   * SplitDivider grows symmetrically around a 1px flex item with
   * `margin: 0 -5.5px`, TocDock shifts one side of a 6px absolute box. What CAN
   * be enforced is that each host declares it ONCE for all three triggers —
   * `:active`, `--expanded` and `:hover`. Declaring it separately per trigger is
   * exactly how the line rules drifted, and the next drift would look the same.
   *
   * The tint is dropped before comparing: a host may need it to win (or lose)
   * against its own `width`/`margin` in the same block, so only the geometry
   * must match.
   */
  function geometryOf(block: string): string {
    return norm(
      block
        .split(';')
        .filter((d) => /^\s*(width|height|margin|margin-left|margin-top|margin-bottom)\s*:/.test(d))
        .join(';'),
    )
  }

  /** Collapse the selector list of a scoped SFC rule into its parts. Comments
   *  are stripped first — the SFCs are heavily commented, and an unstripped
   *  comment would be absorbed into the preceding selector's text. */
  function scopedRules(src: string): Array<{ selectors: string[]; decls: string }> {
    const style = src
      .slice(src.indexOf('<style scoped>'))
      .replace(/\/\*[\s\S]*?\*\//g, '')
    return [...style.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
      .filter((m) => !m[1].trim().startsWith('@'))
      .map((m) => ({
        selectors: norm(m[1]).split(',').map((s) => s.trim()),
        decls: m[2].trim(),
      }))
  }

  it.each([
    ['src/components/common/SplitDivider.vue', 'split-view__divider--horizontal'],
    ['src/components/common/SplitDivider.vue', 'split-view__divider--vertical'],
    ['src/components/file/TocDock.vue', 'toc-dock-divider'],
  ])('%s: %s declares its expanded band once per trigger', (rel, cls) => {
    const src = readFromRepo(rel)
    const isTrigger = (s: string) =>
      s.includes(':active') || s.includes('--expanded') || s.includes(':hover')

    // Bucket every triggering selector by (positioning variant, trigger). The
    // variant is whatever precedes the class token — for TocDock the
    // left-docked edge is a real variant that legitimately offsets the other
    // way (`.toc-dock--left .toc-dock-divider:hover { margin-left: 3px }`), so
    // parity is required within a variant, not across variants.
    const buckets: Record<string, Record<string, string[]>> = {}
    for (const rule of scopedRules(src)) {
      for (const sel of rule.selectors) {
        if (!sel.includes(cls) || !isTrigger(sel)) continue
        const variant = sel.slice(0, sel.indexOf(cls)).trim()
        const trigger = [':active', '--expanded', ':hover'].find((t) => sel.includes(t))!
        buckets[variant] ??= {}
        buckets[variant][trigger] ??= []
        buckets[variant][trigger].push(geometryOf(rule.decls))
      }
    }

    const variants = Object.keys(buckets)
    expect(variants.length, `${cls} must have expanded-band rules`).toBeGreaterThan(0)

    for (const [variant, byTrigger] of Object.entries(buckets)) {
      const where = `${cls}${variant ? ` (${variant})` : ''}`
      // Each trigger present must declare the same geometry as the others.
      const seen = Object.entries(byTrigger)
      for (const [trigger, blocks] of seen) {
        for (const b of blocks) {
          expect(b, `${where}: ${trigger} band geometry must match the other triggers`)
            .toBe(seen[0][1][0])
        }
      }
      // All three triggers must be covered — a missing one is the hover bug.
      expect(Object.keys(byTrigger).sort(), `${where} must cover every trigger`)
        .toEqual(['--expanded', ':active', ':hover'])
    }
  })

  it.each([
    ['src/components/common/SplitDivider.vue', 'split-view__divider'],
    ['src/components/file/TocDock.vue', 'toc-dock-divider'],
  ])('%s: %s mixes its band tint into an OPAQUE base', (rel, cls) => {
    // Regression: the band used to be `color-mix(accent 12%, transparent)`.
    // A translucent band composites with whatever is behind it, and the content
    // it straddles can be accent-coloured itself — the file manager's selected
    // row is `background: var(--accent-color)` and ends flush with the divider,
    // so 12%-accent-over-accent stayed fully accent while the half over the
    // white pane below was a pale tint. Measured live: the upper half was 8/8
    // solid accent pixels, the lower 0/7 — a solid bar on one side only, which
    // is the "solid hover bar pops out at a certain position" report.
    //
    // Mixing into `--bg-primary` makes the band opaque, so it renders the same
    // no matter what sits behind it.
    const src = readFromRepo(rel)
    const bandRules = scopedRules(src).filter((r) =>
      r.selectors.some((s) => s.includes(cls) && (s.includes(':active') || s.includes('--expanded') || s.includes(':hover'))),
    )
    const tints = bandRules
      .flatMap((r) => [...r.decls.matchAll(/background\s*:\s*([^;]+)/g)].map((m) => m[1].trim()))
    expect(tints.length, `${cls} must declare a band tint`).toBeGreaterThan(0)
    for (const tint of tints) {
      expect(tint, `${cls}: band tint must not be mixed into \`transparent\``)
        .not.toMatch(/,\s*transparent\s*\)/)
      expect(tint, `${cls}: band tint must mix into an opaque base`)
        .toMatch(/var\(--bg-primary/)
    }
  })
})
