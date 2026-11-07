import { describe, expect, it } from 'vitest'
import { normalizeDp } from '@/testUtils/designLength'
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
      return normalizeDp(readFileSync(join(base, relPath), 'utf8'))
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
    expect(readWebFile(COMPONENTS[0])).toMatch(/class="drilldown-header\b/)
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
    expect(decls).toMatch(/(width):\s*(?:calc\(\s*28\s*\*\s*var\(--dp\)\s*\)|28px)/)
    expect(decls).toMatch(/(height):\s*(?:calc\(\s*28\s*\*\s*var\(--dp\)\s*\)|28px)/)
    expect(decls).toMatch(/border-radius:\s*var\(--radius-lg\)/)
    expect(decls).toMatch(/background:\s*var\(--bg-secondary\)/)
    expect(decls).toMatch(/color:\s*var\(--text-secondary\)/)
    // `border: none` matters: RefreshButton renders a bare <button>, which keeps
    // the UA default border that a round radius then shows as a stray ring.
    expect(decls).toMatch(/border:\s*none/)
  })

  it('leaves the hover treatment to the shared ActionBar rules', () => {
    // The forge header's own rules are scoped to ForgePanelContent, so the
    // reference values are read from there rather than from the global sheet.
    //
    // Anchor the selector: an unanchored /\.forge-header-btn\s*\{/ matches the
    // `.forge-header.show-labels .forge-header-btn` variant first and compares
    // the wrong declaration block. The base FILL is deliberately not compared —
    // forge sits on a transparent header and the git bar on bg-secondary (see
    // the "transparent bar" case above), so only the hover pair is common.
    // Comments are stripped first: the anchor needs `}` (or start of file)
    // immediately before the selector, and a comment sitting between the
    // previous rule and this one would otherwise break the match.
    const forgePanel = stripComments(readWebFile('src/components/forge/ForgePanelContent.vue'))
    const forgeBase = forgePanel.match(
      /(?:^|[},])\s*\.forge-header-btn\s*\{([^}]*)\}/,
    )
    expect(forgeBase, '.forge-header-btn must exist in ForgePanelContent').not.toBeNull()

    // The contract is now "both defer to the shared row rules", so neither may
    // paint its own resting colour — that is what made the refresh button read
    // as a different control from the buttons beside it. The hover pair lives
    // once, in the ActionBar block, and is checked here rather than per class.
    expect(forgeBase![1], 'forge must not declare its own resting colour').not.toMatch(
      /(^|;)\s*color:/,
    )
    // The git bar's own rule keeps the standalone fallback (a drill-down header
    // with no ActionBar still needs a colour); inside a bar the shared rule wins
    // on specificity, which is what the assertion above pins for forge.
    expect(baseRule(componentsCss, 'drilldown-refresh-btn')).not.toBeNull()

    const shared = stripComments(componentsCss).match(
      /\.ab--header > button:not\(\[data-ab-more\]\):hover:not\(:disabled\)[^{]*\{([^}]*)\}/,
    )
    expect(shared, 'the shared ActionBar hover rule must exist').not.toBeNull()
    expect(shared![1]).toMatch(/color:\s*var\(--text-primary\)/)
    expect(shared![1]).toMatch(/color-mix\(in srgb, var\(--text-primary\) 12%/)
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

describe('the toolbar has exactly one branch glyph', () => {
  // The header used to be "⑂ + count" with a second row below carrying another
  // ⑂ on the branch picker: the same glyph twice, meaning two different things
  // ("this is commit history" / "this is the branch you are reading"). The
  // toolbar now leads with the picker, so the glyph lives there and nowhere
  // else, and the title block is gone.
  const src = readWebFile('src/components/git/GitCommitList.vue')

  it('puts the glyph on the branch picker', () => {
    const picker = src.match(/<div v-if="showBranchBar" class="branch-picker">([\s\S]*?)<\/button>/)
    expect(picker, 'the branch picker must exist').not.toBeNull()
    expect(picker![1]).toContain('<GitBranch')
    expect(picker![1]).toContain('class="branch-chip-name"')
  })

  it('leaves no second title block behind', () => {
    expect(src).not.toContain('class="drilldown-title"')
    expect(src).not.toContain('drilldown-title-icon')
  })
})

describe('every toolbar button can become a menu item', () => {
  it('gives each button an .action-label', () => {
    // ActionBar builds the overflow menu from the buttons' `.action-label` (or
    // their title). A button without one folds into the menu as an empty row,
    // which is invisible until the bar actually gets narrow.
    const src = readWebFile('src/components/git/GitCommitList.vue')
    const bar = src.match(/<ActionBar[\s\S]*?<\/ActionBar>/)
    expect(bar, 'the toolbar must use ActionBar').not.toBeNull()
    const buttons = bar![0].match(/<(?:button|RefreshButton)\b[\s\S]*?<\/(?:button|RefreshButton)>/g) || []
    expect(buttons.length, 'the bar is expected to hold search/refresh/merge/manage').toBeGreaterThanOrEqual(4)
    for (const b of buttons) {
      expect(b, 'a toolbar button is missing its .action-label').toContain('class="action-label"')
    }
  })
})

describe('the header search field is borderless and fills the free space', () => {
  const src = readWebFile('src/components/git/GitCommitList.vue')

  it('wraps SearchInput instead of passing the class through', () => {
    // Regression: the field used to take `class="commit-search-input"` directly.
    // A fallthrough class merges onto the child's ROOT element — which is
    // .search-pill itself — so `.commit-search-input :deep(.search-pill)`
    // compiled to a descendant selector (`.commit-search-input[data-v-x]
    // .search-pill`) that can never match: the two classes are on one element.
    // The override silently did nothing and the field kept its shared border.
    // Every other call site in the repo wraps for this reason.
    const at = src.indexOf('class="commit-search"')
    expect(at, 'the .commit-search wrapper must exist').toBeGreaterThan(-1)
    // The wrapper must contain the SearchInput, and its element must not be the
    // SearchInput itself.
    const wrapper = src.slice(at, src.indexOf('</div>', at))
    expect(wrapper).toContain('<SearchInput')
    // The class must NOT be passed through to the component itself.
    expect(src).not.toMatch(/<SearchInput[^>]*class="commit-search-input"/)
  })

  it('drops the shared pill border and focus border', () => {
    const base = src.match(/\.commit-search :deep\(\.search-pill\)\s*\{([^}]*)\}/)
    expect(base, 'the borderless override must exist').not.toBeNull()
    expect(base![1]).toMatch(/border:\s*none/)

    const focused = src.match(/\.commit-search :deep\(\.search-pill\.focused\)\s*\{([^}]*)\}/)
    expect(focused, 'the focused override must exist').not.toBeNull()
    expect(focused![1]).toMatch(/border-color:\s*transparent/)
    expect(focused![1]).toMatch(/box-shadow:/)
  })

  it('lets the field grow to fill the remaining header space', () => {
    // The field must absorb the free space, and the title must stop growing so
    // there is free space to absorb.
    const wrapper = src.match(/\.commit-search\s*\{([^}]*)\}/)
    expect(wrapper, 'the .commit-search rule must exist').not.toBeNull()
    expect(wrapper![1]).toMatch(/flex:\s*1/)
    expect(wrapper![1], 'a max-width would cap the fill').not.toMatch(/max-width/)
  })

  it('tints the fill from --text-primary, not by jumping a background step', () => {
    // Red line 4: across the 36 themes the secondary↔tertiary contrast drops to
    // 1.06, so a --bg-tertiary fill can vanish. Mixing --text-primary into the
    // background is the direction-guaranteed recipe.
    const base = src.match(/\.commit-search :deep\(\.search-pill\)\s*\{([^}]*)\}/)!
    expect(base[1]).toMatch(/background:\s*color-mix\(in srgb,\s*var\(--text-primary\)/)
    expect(base[1], 'must not rely on a background step').not.toMatch(
      /background:\s*var\(--bg-(tertiary|elevated)/,
    )
  })
})

describe('the manage button does not repeat the branch glyph', () => {
  it('uses a different icon from the picker', () => {
    // The picker carries GitBranch. The manage button opens the branch/worktree/
    // tag panel, so it originally reused GitBranch too — two identical icons in
    // one bar. It then moved to MoreVertical, which stopped working once the
    // bar gained ActionBar's own ⋮ overflow button. ListTree is the one glyph
    // that says "a panel of three lists" and collides with neither.
    const src = readWebFile('src/components/git/GitCommitList.vue')
    const picker = src.match(/<div v-if="showBranchBar" class="branch-picker">([\s\S]*?)<\/button>/)
    expect(picker, 'the branch picker must exist').not.toBeNull()
    const pickerIcon = picker![1].match(/<([A-Z][A-Za-z0-9]*)\s+:size/)
    expect(pickerIcon, 'the picker must render an icon').not.toBeNull()
    expect(pickerIcon![1]).toBe('GitBranch')

    // Anchor on the button's own attribute: an unanchored `<button[\s\S]*?`
    // starts at the FIRST button in the header, so any new toolbar button placed
    // before this one makes the block start elsewhere and the icon check reads
    // the wrong glyph.
    const manageBtn = src.match(
      /<button\b[^>]*data-action="manage"[\s\S]*?<\/button>/,
    )
    expect(manageBtn, 'the manage button must exist').not.toBeNull()
    const manageIcon = manageBtn![0].match(/<([A-Z][A-Za-z0-9]*)\s+:size/)
    expect(manageIcon, 'the manage button must render an icon').not.toBeNull()

    expect(
      manageIcon![1],
      'the manage button must not reuse the branch glyph',
    ).not.toBe(pickerIcon![1])
    // NOT MoreVertical: the bar owns an overflow button that already wears ⋮,
    // and two of them a few pixels apart for different actions is unreadable.
    expect(manageIcon![1]).toBe('ListTree')
  })

  it('imports the manage icon so the button is not empty', () => {
    // A missing import renders an empty <svg>-less button that no existence
    // assertion would catch — see concurrent_git_safety §9.
    const src = readWebFile('src/components/git/GitCommitList.vue')
    const imports = src.match(/import\s*\{([^}]*)\}\s*from\s*'lucide-vue-next'/)
    expect(imports, 'a lucide import must exist').not.toBeNull()
    expect(imports![1]).toContain('ListTree')
    expect(imports![1], 'the ⋮ glyph now belongs to the overflow button').not.toContain('MoreVertical')
  })
})
