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

describe('permission-result chip uses the status-chip type tier', () => {
  for (const path of [INLINE, OVERLAY]) {
    it(`${path}: 11px tier, not the 13px body tier`, () => {
      const decls = declsOf(path, '')
      expect(decls).toMatch(/font-size:\s*var\(--font-size-xs\)/)
      // The regression was --font-size-md (13px), the default body tier.
      expect(decls).not.toMatch(/font-size:\s*var\(--font-size-md\)/)
    })

    it(`${path}: shares the geometry of .permission-detail-label`, () => {
      const decls = declsOf(path, '')
      expect(decls).toMatch(/border-radius:\s*var\(--radius-xs\)/)
      expect(decls).toMatch(/font-weight:\s*var\(--font-weight-semibold\)/)
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
