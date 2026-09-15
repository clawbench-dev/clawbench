import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Contract for the shared `.count-badge` class in css/components.css.
 *
 * Why this exists: ~17 numeric count badges used to each declare their own
 * shape. They had drifted into five different corner radii (--radius-xs/sm/md,
 * 50%) at the same two sizes, so badges sitting side by side read as different
 * components. The shared class now owns shape + geometry + font size, and each
 * call site keeps only its colour and font weight.
 *
 * The trap this guards: a component's `<style scoped>` rule compiles to
 * `.foo[data-v-xxx]` — specificity (0,2,0) — which OUTRANKS the global
 * `.count-badge` at (0,1,0). So re-adding `border-radius` (or any geometry) in
 * a scoped rule silently wins and squares the badge off again, with nothing
 * failing. That is a silent visual regression, so it is asserted here.
 *
 * jsdom has no CSS engine, so these are source-sniffing checks — the same
 * pattern as wallpaperSurfaceTransparency.css.test.ts. components.css lives
 * outside the vitest source root (web/css), so it is read off disk; the cwd
 * differs between a bare `vitest` run (web/) and scripts/vitest-run.sh (repo
 * root), so probe both.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

const css = readWebFile('css/components.css')

/** Declarations of the first `<selector> {` rule. */
function declsOf(selector: string): string {
  const m = css.match(
    new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}'),
  )
  expect(m, `${selector} rule must exist in components.css`).not.toBeNull()
  return m![1]
}

/** Numeric px value of a `prop: Npx` declaration, or NaN. */
function px(decls: string, prop: string): number {
  const m = decls.match(new RegExp(`(?:^|;)\\s*${prop}:\\s*(-?[\\d.]+)px`))
  return m ? Number(m[1]) : NaN
}

describe('.count-badge is a pill, not a rounded rectangle', () => {
  it('uses the pill radius token', () => {
    expect(declsOf('.count-badge')).toMatch(/border-radius:\s*var\(--radius-full\)/)
  })

  it('the pill token is at least half the badge height', () => {
    // Guards the shape rather than the token name: retuning --radius-full down
    // to a corner-ish value would silently square every badge in the app.
    const radius = Number(
      readWebFile('css/variables.css').match(/--radius-full:\s*(\d+)px/)?.[1] ??
        NaN,
    )
    const height = px(declsOf('.count-badge'), 'line-height')
    expect(height).toBeGreaterThan(0)
    expect(radius).toBeGreaterThanOrEqual(height / 2)
  })
})

describe('.count-badge geometry', () => {
  it('centres a single short number', () => {
    const decls = declsOf('.count-badge')
    expect(decls).toMatch(/text-align:\s*center/)
    expect(decls).toMatch(/font-variant-numeric:\s*tabular-nums/)
  })

  it('does not clip, and never shrinks below its min-width', () => {
    const decls = declsOf('.count-badge')
    // nowrap + flex-shrink:0 keep a two-digit count from wrapping or being
    // squeezed by a flex row; min-width keeps a single digit circular.
    expect(decls).toMatch(/white-space:\s*nowrap/)
    expect(decls).toMatch(/flex-shrink:\s*0/)
    expect(px(decls, 'min-width')).toBeGreaterThan(0)
  })

  it('derives its height from line-height so richer content can grow', () => {
    // `.drilldown-count` can hold a spinner plus a label; a fixed `height`
    // would clip that. line-height gives the tier height for a single line
    // while still allowing growth.
    const decls = declsOf('.count-badge')
    expect(decls).not.toMatch(/(?:^|;)\s*height:/)
    expect(px(decls, 'line-height')).toBeGreaterThan(0)
  })

  it('offers the two tiers the call sites actually used', () => {
    // The 18px tier is the dock overflow badge and the chat file-change count.
    const base = declsOf('.count-badge')
    const md = declsOf('.count-badge--md')
    expect(px(md, 'line-height')).toBeGreaterThan(px(base, 'line-height'))
    expect(px(md, 'min-width')).toBeGreaterThanOrEqual(px(base, 'min-width'))
  })
})

describe('call sites do not re-declare what .count-badge owns', () => {
  /** Every .vue under src/, with its path relative to web/. */
  function collectVueSources(): { path: string; text: string }[] {
    const roots = [process.cwd(), join(process.cwd(), 'web')]
    let root = ''
    for (const base of roots) {
      try {
        readFileSync(join(base, 'css/components.css'))
        root = base
        break
      } catch {
        // next
      }
    }
    expect(root, 'web root must be discoverable').not.toBe('')

    const out: { path: string; text: string }[] = []
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const full = join(dir, e.name)
        if (e.isDirectory()) {
          if (['node_modules', 'dist', '__tests__', 'vendor-build'].includes(e.name)) continue
          walk(full)
        } else if (e.name.endsWith('.vue')) {
          out.push({ path: full.slice(root.length + 1), text: readFileSync(full, 'utf8') })
        }
      }
    }
    walk(join(root, 'src'))
    return out
  }

  const sources = collectVueSources()

  /**
   * Class attributes that contain `count-badge` as a whole class token.
   *
   * The word boundaries matter: `forge-overview-count-badge` is a text label
   * ("3 events"), not one of these numeric badges, and a loose substring match
   * would drag it into the checks below.
   */
  function countBadgeClassAttrs(text: string): string[] {
    return [...text.matchAll(/class="([^"]*)"/g)]
      .map((m) => m[1])
      .filter((cls) => cls.split(/\s+/).includes('count-badge'))
  }

  /**
   * Selectors that legitimately pair with .count-badge and are allowed to keep
   * a `border-radius`.
   *
   * The dock badge is the one two-rule case: `.dock-badge` is the 8px unread
   * DOT (always `border-radius: 50%`), and `.dock-badge-count` is the numeric
   * variant layered on the same element. The count rule re-declares the pill
   * because the dot rule is scoped and would otherwise win. Both are deliberate;
   * the dock's own spec (dockBadgeAnim.test.ts) pins that override.
   */
  const RADIUS_ALLOWED = new Set(['.dock-badge', '.dock-badge-count'])

  it('finds the sources it is meant to guard', () => {
    expect(sources.length).toBeGreaterThan(50)
  })

  it('no scoped rule re-adds a corner radius to a .count-badge element', () => {
    const offenders: string[] = []
    for (const { path, text } of sources) {
      // Only rules whose selector also carries .count-badge in the template are
      // relevant; find those class names first.
      const badgeClasses = new Set<string>()
      for (const attr of countBadgeClassAttrs(text)) {
        for (const cls of attr.split(/\s+/)) {
          if (cls && cls !== 'count-badge' && !cls.startsWith('count-badge--')) {
            badgeClasses.add(cls)
          }
        }
      }
      for (const cls of badgeClasses) {
        if (RADIUS_ALLOWED.has(`.${cls}`)) continue
        const rule = text.match(
          new RegExp(
            `\\.${cls.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*\\{([^}]*)\\}`,
          ),
        )
        if (!rule) continue
        if (/border-radius:/.test(rule[1])) {
          offenders.push(`${path}: .${cls} re-declares border-radius`)
        }
      }
    }
    expect(
      offenders,
      `these scoped rules outrank .count-badge and would defeat the pill:\n${offenders.join('\n')}`,
    ).toEqual([])
  })

  it('every element carrying .count-badge also carries its call-site class', () => {
    // Guards the reverse mistake: stripping the call-site class (which holds the
    // colour) while adding .count-badge would leave an unstyled badge.
    const offenders: string[] = []
    for (const { path, text } of sources) {
      for (const attr of countBadgeClassAttrs(text)) {
        const hasCallSite = attr
          .split(/\s+/)
          .some((c) => c !== 'count-badge' && !c.startsWith('count-badge--'))
        if (!hasCallSite) offenders.push(`${path}: class="${attr}"`)
      }
    }
    expect(offenders, `badge with no colour class:\n${offenders.join('\n')}`).toEqual([])
  })
})
