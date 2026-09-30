import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards for the settled-permission status chip (`.permission-result` +
 * `-approved` / `-denied` / `-auto-approved`).
 *
 * Three things were wrong and each can silently come back:
 *
 *   1. LAYOUT — the chip is a child of `.permission-approval-view`, a
 *      column-flex container. It declared `display: inline-block`, which is
 *      blockified to `block` as a flex item, so it inherited
 *      `align-items: stretch` and became a FULL-WIDTH bar instead of hugging
 *      its label. Measured in Chrome: 600px of a 602px card. `align-self:
 *      flex-start` is the fix, and nothing else in the file supplies it.
 *   2. TYPE TIER — it used `--font-size-md` (13px, the default body tier),
 *      making it larger than the `.permission-detail-label` chip directly above
 *      it in the same column (10px) and every other status chip in the app
 *      (`.tool-output-status`, `.forge-state-badge`, `.error-source-chip`).
 *   3. THEME — the tints were fixed Tailwind hexes (#dcfce7 / #bbf7d0 …), so
 *      none of the 36 themes could adapt them.
 *
 * The chip is injected via `v-html` (renderToolDetail.ts emits the markup), so
 * the rules MUST live in a non-scoped block — a scoped rule compiles to
 * `.foo[data-v-x]` and can never match an injected node. That is asserted too,
 * since it fails silently (same trap as chatVhtmlStyleScope.test.ts).
 *
 * The rules exist in TWO files (ContentBlocks.vue for the inline card,
 * ToolDetailDrawer.vue for the overlay) and had already drifted apart — one
 * carried a border and `font-weight: medium`, the other did not. The drift
 * check below is therefore part of the contract, not a nicety.
 *
 * jsdom has no CSS engine, so this is a source contract.
 */

type Rule = { scoped: boolean; selector: string; decls: string }

/** Every `<style>` block, tagged scoped/unscoped. */
function styleBlocks(src: string): Array<{ scoped: boolean; body: string }> {
  const out: Array<{ scoped: boolean; body: string }> = []
  const re = /<style([^>]*)>([\s\S]*?)<\/style>/g
  let m: RegExpExecArray | null
  while ((m = re.exec(src)) !== null) {
    out.push({ scoped: /\bscoped\b/.test(m[1]), body: m[2] })
  }
  return out
}

/**
 * Find a rule by its exact selector. The trailing `\{` anchors the match, so
 * `.permission-result` does not also match `.permission-result-approved`.
 */
function rule(src: string, selector: string): Rule | undefined {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  for (const block of styleBlocks(src)) {
    // `[^}]*` is safe: no rule in these blocks nests braces.
    const m = block.body.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
    if (m) return { scoped: block.scoped, selector, decls: m[1] }
  }
  return undefined
}

const INLINE = 'src/components/chat/ContentBlocks.vue'
const OVERLAY = 'src/components/chat/ToolDetailDrawer.vue'

const VARIANTS = ['', '-approved', '-denied', '-auto-approved'] as const

/** Prefix differs per file; strip it so the two copies are comparable. */
function prefixOf(path: string): string {
  return path === INLINE ? '.content-blocks .tool-detail' : '.tool-detail-body'
}

function declsOf(path: string, variant: string): string {
  const selector = `${prefixOf(path)} .permission-result${variant}`
  const r = rule(readWebFile(path), selector)
  expect(r, `${path}: ${selector} rule must exist`).toBeDefined()
  return r!.decls
}

function scopedOf(path: string, variant: string): boolean {
  const selector = `${prefixOf(path)} .permission-result${variant}`
  return rule(readWebFile(path), selector)!.scoped
}

/** Every rule across all style blocks, with its source offset. */
function allRules(path: string) {
  const src = readWebFile(path)
  const out: Array<{ selector: string; decls: string; at: number }> = []
  const blockRe = /<style([^>]*)>([\s\S]*?)<\/style>/g
  let b: RegExpExecArray | null
  while ((b = blockRe.exec(src)) !== null) {
    const body = b[2].replace(/\/\*[\s\S]*?\*\//g, '')
    const ruleRe = /([^{}]+)\{([^{}]*)\}/g
    let m: RegExpExecArray | null
    while ((m = ruleRe.exec(body)) !== null) {
      out.push({ selector: m[1].trim(), decls: m[2], at: b.index + m.index })
    }
  }
  return out
}

/** The single rule whose selector is exactly `exact`. */
function ruleFor(path: string, exact: string) {
  const found = allRules(path).filter((r) => r.selector === exact)
  expect(found.length, `${path}: expected exactly one "${exact}"`).toBe(1)
  return found[0]
}

/** Per-file selector prefix. */
const P = (path: string) =>
  path === INLINE ? '.content-blocks .tool-detail' : '.tool-detail-body'

describe('permission-result chip: guards the guard', () => {
  it('parses both files and finds all four variants', () => {
    for (const path of [INLINE, OVERLAY]) {
      const blocks = styleBlocks(readWebFile(path))
      expect(blocks.length, `${path} must keep two <style> blocks`).toBeGreaterThanOrEqual(2)
      for (const v of VARIANTS) {
        const selector = `${prefixOf(path)} .permission-result${v}`
        expect(rule(readWebFile(path), selector), `${path}: missing ${selector}`).toBeDefined()
      }
    }
  })
})

describe('permission-result chip is styled from the non-scoped block', () => {
  // renderToolDetail.ts emits this markup through v-html, so a scoped rule
  // would silently never match.
  for (const path of [INLINE, OVERLAY]) {
    for (const v of VARIANTS) {
      it(`${path}: .permission-result${v}`, () => {
        expect(
          scopedOf(path, v),
          `.permission-result${v} is v-html-injected; a scoped rule can never match it`,
        ).toBe(false)
      })
    }
  }
})

describe('permission-result chip hugs its label instead of stretching', () => {
  for (const path of [INLINE, OVERLAY]) {
    it(`${path}: pins align-self so the column flex parent cannot stretch it`, () => {
      const decls = declsOf(path, '')
      expect(decls, 'must not rely on the parent align-items').toMatch(
        /align-self:\s*flex-start/,
      )
    })
  }
})

describe('permission-result chip matches the option buttons', () => {
  for (const path of [INLINE, OVERLAY]) {
    it(`${path}: same type tier and weight as the option cells`, () => {
      const decls = declsOf(path, '')
      // The settled chip replaces the buttons, so it must stay on the SAME type
      // scale they use. Both are --font-size-sm now; the chip was previously an
      // 11px badge, which read as a different component next to a 30px row.
      expect(decls).toMatch(/font-size:\s*var\(--font-size-sm\)/)
      expect(decls).toMatch(/font-weight:\s*var\(--font-weight-medium\)/)
      expect(decls, 'must not fall back to the small badge tier').not.toMatch(
        /font-size:\s*var\(--font-size-xs\)/,
      )

      // Pin the two to the same token rather than duplicating the value: if the
      // cells move to another tier, this fails instead of drifting silently.
      const cell = ruleFor(path, `${P(path)} .permission-btn-group .permission-btn`)
      const sizeOf = (d: string) => d.match(/font-size:\s*(var\([^)]+\))/)?.[1]
      expect(
        sizeOf(decls),
        'the chip and the option cells must use the same font-size token',
      ).toBe(sizeOf(cell.decls))
    })

    it(`${path}: same 30px height and radius family as a button`, () => {
      const decls = declsOf(path, '')
      expect(decls, 'must match the 30px control height').toMatch(/height:\s*30px/)
      expect(decls).toMatch(/border-radius:\s*var\(--radius-sm\)/)
    })
  }
})

describe('permission-result chip tints derive from the theme palette', () => {
  for (const path of [INLINE, OVERLAY]) {
    for (const v of VARIANTS) {
      it(`${path}: .permission-result${v} has no fixed colour`, () => {
        const decls = declsOf(path, v)
        // A literal hex cannot adapt to the other 35 themes.
        expect(decls, 'no hardcoded hex colours allowed').not.toMatch(/#[0-9a-f]{3,8}\b/i)
        expect(decls, 'no rgb()/rgba() literals allowed').not.toMatch(/\brgba?\(/)
      })
    }

    it(`${path}: variant tints are mixed from tokens over transparency`, () => {
      for (const v of ['-approved', '-denied', '-auto-approved'] as const) {
        const decls = declsOf(path, v)
        expect(decls, `${v} background must mix a theme token`).toMatch(
          /background:\s*color-mix\(in srgb,\s*var\(--[\w-]+\)\s*\d+%,\s*transparent\)/,
        )
        expect(decls, `${v} colour must come from a theme token`).toMatch(
          /color:\s*var\(--[\w-]+\)/,
        )
      }
    })
  }
})

describe('auto-approved reads as "nobody decided", not as an approval', () => {
  for (const path of [INLINE, OVERLAY]) {
    it(`${path}: neutral tint, not the success green`, () => {
      const decls = declsOf(path, '-auto-approved')
      expect(decls).toMatch(/color:\s*var\(--text-secondary\)/)
      // Approved is green; auto-approved is an unreviewed decision and must not
      // be visually identical to a human approval.
      expect(decls, 'auto-approved must not reuse the success colour').not.toMatch(
        /--color-success/,
      )
    })
  }
})

describe('the two copies do not drift', () => {
  it('declares identical declarations for each variant', () => {
    for (const v of VARIANTS) {
      // Comments are per-file prose and are stripped before comparing; only the
      // declarations themselves are the contract.
      const norm = (s: string) =>
        s
          .replace(/\/\*[\s\S]*?\*\//g, '')
          .split(';')
          .map((d) => d.replace(/\s+/g, ' ').trim())
          .filter(Boolean)
          .sort()
      expect(
        norm(declsOf(OVERLAY, v)),
        `ContentBlocks.vue and ToolDetailDrawer.vue disagree on .permission-result${v}`,
      ).toEqual(norm(declsOf(INLINE, v)))
    }
  })
})

/**
 * The permission card's option buttons render as ONE integrated group: a single
 * bordered container, equal-width cells and 1px dividers — not N separate pills.
 *
 * Two things here fail silently and so are pinned:
 *
 *   1. `min-width: 0` on the cells. A flex item defaults to `min-width: auto`,
 *      which refuses to shrink below its content: without it `text-overflow:
 *      ellipsis` NEVER triggers and the group pushes wider than the card
 *      instead of truncating. Nothing errors — it just overflows.
 *   2. Source ORDER. The base `.permission-options .permission-btn` rule sets
 *      padding + border-radius at the same specificity as the group's cell
 *      rule, so whichever comes LAST wins. Declared before it, the cells keep a
 *      6px radius and 14px padding and the group silently stops reading as one
 *      control. A pure "does the rule exist" assertion cannot catch this.
 */
describe('permission option buttons form one integrated group', () => {
  for (const path of [INLINE, OVERLAY]) {
    it(`${path}: the container owns the frame, the cells own nothing`, () => {
      const group = ruleFor(path, `${P(path)} .permission-btn-group`)
      // Natural width, not a full-bleed bar: inline-flex + content-sized cells
      // so a short row of short labels does not stretch across the card.
      expect(group.decls).toMatch(/display:\s*inline-flex/)
      // `display: inline-flex` alone does NOT hug: as a child of the column-flex
      // .permission-approval-view it inherits align-items: stretch, blockifies
      // and spans the full card (measured 738px of a 738px card). align-self is
      // what actually makes the frame hug its content.
      expect(
        group.decls,
        'without align-self the group stretches to the full card width',
      ).toMatch(/align-self:\s*flex-start/)
      expect(group.decls, 'must be capped so a long row cannot span the card').toMatch(
        /max-width:\s*100%/,
      )
      // Anchored so it cannot match the `100%` inside `max-width: 100%`.
      expect(group.decls, 'the old full-width behaviour must be gone').not.toMatch(
        /(?:^|;)\s*width:\s*100%/,
      )
      expect(group.decls).toMatch(/gap:\s*0/)
      expect(group.decls).toMatch(/border-radius:\s*var\(--radius-sm\)/)
      expect(group.decls).toMatch(/border:\s*1px solid/)
    })

    it(`${path}: cells size to their label but can still truncate`, () => {
      const cell = ruleFor(path, `${P(path)} .permission-btn-group .permission-btn`)
      // flex: 0 1 auto = "content width, may shrink" — the shrink half is what
      // lets the ellipsis engage once the row exceeds max-width.
      expect(cell.decls, 'must size to content and be allowed to shrink').toMatch(
        /flex:\s*0 1 auto/,
      )
      expect(cell.decls, 'must be able to shrink').toMatch(/min-width:\s*0/)
      expect(cell.decls).toMatch(/text-overflow:\s*ellipsis/)
      expect(cell.decls).toMatch(/overflow:\s*hidden/)
      expect(cell.decls).toMatch(/white-space:\s*nowrap/)
      // inline-flex would ignore text-overflow entirely.
      expect(cell.decls).toMatch(/display:\s*block/)
      // A step down from the .fbtn default (--font-size-md).
      expect(cell.decls).toMatch(/font-size:\s*var\(--font-size-sm\)/)
      // The container draws the frame; a per-cell border would double it.
      expect(cell.decls).toMatch(/border:\s*none/)
      expect(cell.decls).toMatch(/border-radius:\s*0/)
    })

    it(`${path}: dividers are box-shadow, not layout-affecting borders`, () => {
      const div = ruleFor(path, `${P(path)} .permission-btn-group > * + *`)
      expect(div.decls).toMatch(/box-shadow:\s*-1px 0 0 0/)
      expect(div.decls, 'a real border would shift the cell widths').not.toMatch(
        /border-(left|right):/,
      )
    })

    it(`${path}: the group cell rule comes AFTER the base button rule`, () => {
      // Same specificity => source order decides. Declared earlier, the base
      // rule's 6px radius + 14px padding silently win.
      const base = ruleFor(path, `${P(path)} .permission-options .permission-btn`)
      const cell = ruleFor(path, `${P(path)} .permission-btn-group .permission-btn`)
      expect(
        cell.at,
        'the group rule must come after the base rule or it loses the cascade',
      ).toBeGreaterThan(base.at)
    })
  }

  it('the renderer opts the permission card into the group', () => {
    const src = readWebFile('src/utils/renderToolDetail.ts')
    // The group only applies when the renderer emits the extra class.
    expect(src).toContain('permission-options permission-btn-group')
  })
})
