import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guard for the git-history panel chrome.
 *
 * Why this exists: the commit-list header bar, its title, the count badge and
 * the header icon buttons are rendered by THREE components —
 * GitCommitList.vue (commit list), GitHistoryContent.vue (history dock tab) and
 * GitHistoryDrawer.vue (mobile file-history sheet). Each used to declare its own
 * copy in a `<style scoped>` block, so the same bar had three sources of truth
 * and they had already drifted (a 50% round button here, hard-coded colours and
 * a 14px padding there).
 *
 * The chrome now lives once, globally, in css/components.css, aligned with the
 * forge panel header (.forge-header / .forge-header-btn) so a panel header looks
 * the same whichever tab it belongs to. This spec pins:
 *   1. every shared class has a BASE rule in the global stylesheet;
 *   2. none of the three components re-declares it (a scoped rule compiles to
 *      `.cls[data-v-x]` and silently outranks the global one);
 *   3. the values that make it match forge — in particular the transparent bar
 *      on a bg-primary page root, and the accent-driven (not hard-coded) pulse.
 *
 * jsdom has no CSS engine, so these are source-sniffing checks — the same
 * pattern as forgeDetailChrome.css.test.ts / countBadge.css.test.ts.
 * css/ lives outside the vitest source root, and cwd differs between a bare
 * `vitest` run (web/) and scripts/vitest-run.sh (repo root), so probe both.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

const componentsCss = readWebFile('css/components.css')

/** Classes whose styling is shared by more than one git-history component. */
const SHARED_CHROME = [
  'drilldown-page',
  'drilldown-header',
  'drilldown-title',
  'drilldown-title-icon',
  'drilldown-count',
  'drilldown-refresh-btn',
  'drilldown-body',
]

const COMPONENTS = [
  'src/components/git/GitCommitList.vue',
  'src/components/git/GitHistoryContent.vue',
  'src/components/git/GitHistoryDrawer.vue',
]

/** Strip comments so a commented-out rule can never satisfy an assertion. */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** The declaration block of the first rule whose selector is exactly `.cls`. */
function baseRule(css: string, cls: string): string | null {
  const m = stripComments(css).match(
    new RegExp(`(?:^|[},])\\s*\\.${cls}\\s*\\{([^}]*)\\}`),
  )
  return m ? m[1] : null
}

describe('git history chrome is declared globally', () => {
  it('gives every shared chrome class a base rule', () => {
    const missing = SHARED_CHROME.filter((c) => baseRule(componentsCss, c) === null)
    expect(
      missing,
      `these have no base rule in css/components.css, so an element carrying ` +
        `only this class is unstyled: ${missing.join(', ')}`,
    ).toEqual([])
  })

  it('declares the pulse keyframes globally', () => {
    // The rule lives globally; if the keyframes stayed in a scoped block the
    // animation would reference a name that no longer resolves.
    expect(componentsCss).toMatch(/@keyframes\s+refresh-pulse-glow\s*\{/)
  })

  it('does not leave a copy of the shared chrome in any component scoped block', () => {
    // A scoped declaration only applies to its own component — and outranks the
    // global rule, so a stale copy silently wins over the shared treatment.
    const offenders: string[] = []
    for (const rel of COMPONENTS) {
      const src = readWebFile(rel)
      const marker = '<style scoped>'
      const idx = src.indexOf(marker)
      if (idx === -1) continue
      const after = src.slice(idx + marker.length)
      const block = after.includes('</style>') ? after.slice(0, after.indexOf('</style>')) : after
      for (const cls of SHARED_CHROME) {
        if (new RegExp(`(?:^|[},])\\s*\\.${cls}\\s*\\{`).test(stripComments(block))) {
          offenders.push(`${rel}: .${cls}`)
        }
      }
    }
    expect(
      offenders,
      `these re-declare shared chrome in a scoped block:\n${offenders.join('\n')}`,
    ).toEqual([])
  })

  it('finds the components it is meant to guard', () => {
    // Self-check: if a component is renamed/moved the loop above would silently
    // stop guarding it.
    for (const rel of COMPONENTS) {
      expect(() => readWebFile(rel), `${rel} must exist`).not.toThrow()
    }
    expect(readWebFile(COMPONENTS[0])).toContain('class="drilldown-header"')
  })
})

describe('git history header matches the forge panel header', () => {
  it('is a transparent bar on the page surface, not a contrasting tint', () => {
    // Forge's header is bg-primary on a bg-primary page (.forge-panel), so only
    // the border separates them. The git bar must not paint its own fill — a
    // bg-secondary bar would also make the row hover tint (bg-secondary)
    // invisible against it.
    const decls = baseRule(componentsCss, 'drilldown-header')!
    expect(decls).toMatch(/background:\s*transparent/)
    expect(decls, 'the bar must not paint bg-secondary').not.toMatch(
      /background:\s*var\(--bg-secondary/,
    )
    // Same geometry as .forge-header.
    expect(decls).toMatch(/height:\s*var\(--header-height\)/)
    expect(decls).toMatch(/padding:\s*0 var\(--space-2\) 0 var\(--space-6\)/)
    expect(decls).toMatch(/gap:\s*var\(--space-3\)/)
  })

  it('gives the page root the surface the transparent bar sits on', () => {
    // Without an explicit bg-primary the tab-panel's bg-secondary shows through,
    // so the bar is not transparent in effect AND the row hover disappears.
    const content = readWebFile('src/components/git/GitHistoryContent.vue')
    const rule = content.match(/\.git-history-content\s*\{([^}]*)\}/)
    expect(rule, '.git-history-content rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/background:\s*var\(--bg-primary/)
  })

  it('uses the square-round 28px icon button, like .forge-header-btn', () => {
    const decls = baseRule(componentsCss, 'drilldown-refresh-btn')!
    expect(decls).toMatch(/width:\s*28px/)
    expect(decls).toMatch(/height:\s*28px/)
    expect(decls).toMatch(/border-radius:\s*var\(--radius-lg\)/)
    expect(decls).toMatch(/background:\s*var\(--bg-secondary\)/)
    expect(decls).toMatch(/color:\s*var\(--text-secondary\)/)
    // `border: none` matters: RefreshButton renders a bare <button>, which keeps
    // the UA default border that a round radius then shows as a stray ring.
    expect(decls).toMatch(/border:\s*none/)
  })

  it('keeps the hover treatment in step with the forge header button', () => {
    // The forge header's own rules are scoped to ForgePanelContent, so the
    // reference values are read from there rather than from the global sheet.
    const forgePanel = readWebFile('src/components/forge/ForgePanelContent.vue')
    const forgeBase = forgePanel.match(/\.forge-header-btn\s*\{([^}]*)\}/)
    expect(forgeBase, '.forge-header-btn must exist in ForgePanelContent').not.toBeNull()
    expect(forgeBase![1]).toMatch(/background:\s*var\(--bg-secondary\)/)
    expect(forgeBase![1]).toMatch(/color:\s*var\(--text-secondary\)/)

    const gitCss = stripComments(componentsCss)
    const hover = gitCss.match(
      /\.drilldown-refresh-btn:hover:not\(:disabled\)\s*\{([^}]*)\}/,
    )
    expect(hover, 'a hover rule must exist').not.toBeNull()
    expect(hover![1]).toMatch(/background:\s*var\(--bg-tertiary\)/)
    expect(hover![1]).toMatch(/color:\s*var\(--accent-color\)/)
  })
})

describe('stale-data pulse follows the theme', () => {
  it('drives the glow from the accent token, not a hard-coded colour', () => {
    // Regression: the glow was a literal rgba(74,144,217,…), so it stayed the
    // same blue under every theme instead of tracking the accent.
    const keyframes = componentsCss.match(
      /@keyframes\s+refresh-pulse-glow\s*\{([\s\S]*?)\n\}/,
    )
    expect(keyframes, 'refresh-pulse-glow keyframes must exist').not.toBeNull()
    expect(keyframes![1]).toContain('var(--accent-color)')
    expect(
      keyframes![1],
      'the glow must not hard-code a colour',
    ).not.toMatch(/rgba?\(\s*74\s*,/)
  })

  it('keeps the pulsing button accent-coloured', () => {
    const decls = baseRule(componentsCss, 'drilldown-refresh-btn')!
    expect(decls).not.toContain('refresh-pulse')
    const pulse = stripComments(componentsCss).match(
      /\.drilldown-refresh-btn\.refresh-pulse\s*\{([^}]*)\}/,
    )
    expect(pulse, 'a .refresh-pulse rule must exist').not.toBeNull()
    expect(pulse![1]).toMatch(/color:\s*var\(--accent-color\)/)
    expect(pulse![1]).toMatch(/animation:\s*refresh-pulse-glow/)
  })
})

describe('the header title carries its glyph', () => {
  it('renders a GitBranch icon in the title, like the forge header does', () => {
    // The title area used to be a bare count badge. The glyph is what makes it
    // read as the same "icon + title" header as the forge panel.
    const src = readWebFile('src/components/git/GitCommitList.vue')
    const title = src.match(/<div class="drilldown-title">([\s\S]*?)<\/div>/)
    expect(title, 'the title block must exist').not.toBeNull()
    expect(title![1]).toContain('<GitBranch')
    expect(title![1]).toContain('class="drilldown-title-icon"')
  })

  it('keeps the glyph from being squeezed by a long count', () => {
    expect(baseRule(componentsCss, 'drilldown-title-icon')).toMatch(/flex-shrink:\s*0/)
  })
})
