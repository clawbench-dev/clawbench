import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guard: the forge drill-down chrome must be declared GLOBALLY.
 *
 * Why this test exists: ForgePipelineDetail originally reused the class names of
 * ForgeDetail's drill-down chrome (header, back button, icon buttons, status
 * dots, error card, title block) but those rules lived in ForgeDetail's
 * `<style scoped>` block. A scoped rule only applies to the component that
 * declares it, so the whole pipeline detail page rendered unstyled — no header
 * bar, uncoloured back button, 0x0 status dots. Unit tests passed because they
 * asserted text content only, so nothing caught it.
 *
 * The check is deliberately narrow and robust: it asserts that the shared class
 * names appear as the SUBJECT of a global rule (in web/css/*.css), and that they
 * are NOT left behind in a component's scoped block. It does not attempt to
 * fully resolve every class a template mentions.
 */

/** Classes whose styling is shared by more than one forge view. */
const SHARED_CHROME = [
  // Drill-down shell + header
  'forge-detail',
  'forge-detail-header',
  'forge-back',
  'forge-detail-actions',
  'forge-icon-btn',
  // Body / title block
  'forge-detail-body',
  'forge-detail-title-row',
  'forge-detail-title',
  'forge-detail-meta',
  'forge-detail-number',
  'forge-meta-sep',
  // Status affordances
  'forge-state-dot',
  'forge-state-badge',
  // Async states
  'forge-loading',
  'forge-error-card',
  'forge-error-icon',
  'forge-error-text',
  'forge-error-title',
  'forge-error-body',
  // CI row metadata
  'forge-pipeline-ref',
  'forge-pipeline-sha',
  'forge-pipeline-event',
  // List chrome shared by the issue/PR list, the pipeline list and the
  // overview list. All three render rows into the same panel shell.
  'forge-list',
  // The row's BASE rule and its primary text. These were the third occurrence of
  // this bug class: the modifier rules (.forge-row.unread, .forge-row-time, …)
  // were already global, but the base geometry was left in ForgePanelContent's
  // scoped block — so the activity tab's rows had no flex layout, padding,
  // separator or ellipsis at all. A modifier rule does NOT satisfy the base
  // class, which is why this list must name the base explicitly.
  'forge-row',
  'forge-row-text',
  'forge-row-main',
  'forge-row-title',
  'forge-row-meta',
  'forge-row-time',
  'forge-row-chevron',
  'forge-state',
  'forge-empty-card',
  'forge-empty-icon',
  'forge-empty-title',
  'forge-empty-hint',
  'forge-overview-count-badge',
  // Filter toolbar + chips. The activity tab renders its own toolbar from
  // ForgeOverviewList, the other tabs render theirs from ForgePanelContent, so
  // these belong to no single component.
  'forge-toolbar',
  'forge-chips',
  'forge-chips-scroll',
  'forge-chips-divider',
  'forge-chip',
]

/** `pipeline-<status>` modifiers, used by both the dot and the badge. */
const STATUS_MODIFIERS = ['success', 'failure', 'running', 'cancelled', 'skipped', 'unknown']

/**
 * Classes a stylesheet DECLARES (i.e. gives the element its own styling).
 *
 * Two cases must be told apart, and the difference is whitespace:
 *
 *   `.forge-state-dot.pipeline-success { }`  — a single compound subject: this
 *       rule styles both classes, so both count.
 *   `html.wallpaper-active .forge-detail-header { }` — a descendant override:
 *       it only re-tints the element under a condition, so it must NOT count as
 *       declaring the class. Counting it would hide a missing base rule, which
 *       is precisely the false negative this guard exists to prevent.
 *
 * So: only a selector with NO ancestor part (one whitespace-free compound) is
 * treated as declaring its classes.
 */
function declaredSubjects(source: string): Set<string> {
  const out = new Set<string>()
  const css = source.replace(/\/\*[\s\S]*?\*\//g, '')
  for (const m of css.matchAll(/([^{}]+)\{/g)) {
    for (const selector of m[1].split(',')) {
      const trimmed = selector.trim()
      if (!trimmed) continue
      // Any combinator means this is a conditional override, not a declaration.
      if (/[\s>+~]/.test(trimmed)) continue
      for (const cls of trimmed.matchAll(/\.([a-z][a-z0-9-]*)/g)) out.add(cls[1])
    }
  }
  return out
}

/**
 * Classes with a BASE rule — a selector that is exactly `.class`.
 *
 * Stricter than declaredSubjects, and the distinction matters. `declaredSubjects`
 * accepts `.forge-row.unread { }` as "declaring" forge-row, because it is a
 * single whitespace-free compound. But a compound rule only applies to elements
 * that ALSO carry the modifier, so it can never stand in for the base rule: an
 * element with just `class="forge-row"` gets nothing from it.
 *
 * That gap is exactly how the activity tab's rows ended up unstyled while
 * `.forge-row.unread` was already global. A base-rule check catches it; the
 * looser one cannot.
 */
function baseRuleClasses(source: string): Set<string> {
  const out = new Set<string>()
  const css = source.replace(/\/\*[\s\S]*?\*\//g, '')
  for (const m of css.matchAll(/([^{}]+)\{/g)) {
    for (const selector of m[1].split(',')) {
      const trimmed = selector.trim()
      if (/^\.[a-z][a-z0-9-]*$/.test(trimmed)) out.add(trimmed.slice(1))
    }
  }
  return out
}

function read(rel: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, rel), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const GLOBAL_STYLESHEETS = [
  'css/components.css',
  'css/base.css',
  'css/layout.css',
  'css/content.css',
  'css/markdown-common.css',
  'css/wide-screen.css',
]

const SCOPED_COMPONENTS = [
  'src/components/forge/ForgeDetail.vue',
  'src/components/forge/ForgePanelContent.vue',
  'src/components/forge/ForgePipelineDetail.vue',
]

describe('forge drill-down chrome is declared globally', () => {
  const globalSubjects = (() => {
    const out = new Set<string>()
    for (const rel of GLOBAL_STYLESHEETS) {
      try {
        for (const c of declaredSubjects(read(rel))) out.add(c)
      } catch {
        // optional stylesheet
      }
    }
    return out
  })()

  it('declares every shared chrome class in a global stylesheet', () => {
    const missing = SHARED_CHROME.filter(c => !globalSubjects.has(c))
    expect(missing, `these must be declared globally, not scoped: ${missing.join(', ')}`).toEqual([])
  })

  it('gives every shared chrome class a BASE rule, not just a modifier', () => {
    // Regression: the activity tab's rows rendered with no flex layout, padding
    // or ellipsis because `.forge-row` had no base rule anywhere global — only
    // `.forge-row.unread` and friends were global, and `.forge-row` itself was
    // left in ForgePanelContent's scoped block.
    //
    // The assertion above did not catch it: `declaredSubjects` accepts
    // `.forge-row.unread` as declaring forge-row. A compound rule only matches
    // elements that also carry the modifier, so it can never substitute for the
    // base rule. This test pins the stricter property.
    const baseClasses = (() => {
      const out = new Set<string>()
      for (const rel of GLOBAL_STYLESHEETS) {
        try {
          for (const c of baseRuleClasses(read(rel))) out.add(c)
        } catch {
          // optional stylesheet
        }
      }
      return out
    })()

    const missing = SHARED_CHROME.filter(c => !baseClasses.has(c))
    expect(
      missing,
      `these are only styled by a modifier/compound rule, so an element with just ` +
        `this class gets no styling: ${missing.join(', ')}`,
    ).toEqual([])
  })

  it('keeps the row base geometry in the global stylesheet', () => {
    // Anchored to the properties that DEFINE a row, so this fails if the base
    // rule is ever moved back into a scoped block or deleted.
    const components = read('css/components.css')
    const rule = components.match(/\.forge-row\s*\{([\s\S]*?)\}/)
    expect(rule, '.forge-row base rule must exist in components.css').not.toBeNull()
    for (const prop of [
      'display: flex',
      'align-items: flex-start',
      'padding: 11px var(--space-6)',
      'border-bottom: 1px solid var(--border-color)',
      'cursor: pointer',
    ]) {
      expect(rule![1], `.forge-row must declare ${prop}`).toContain(prop)
    }
  })

  it('declares every CI status modifier globally', () => {
    // Both the dot and the badge are driven by `pipeline-<status>`; a missing
    // variant renders an invisible dot / untinted pill.
    const missing = STATUS_MODIFIERS.filter(s => !globalSubjects.has(`pipeline-${s}`))
    expect(missing, `unstyled CI statuses: ${missing.join(', ')}`).toEqual([])
  })

  it('does not leave a full copy of the shared chrome in a scoped block', () => {
    // A scoped declaration only applies to its own component, which is exactly
    // how ForgePipelineDetail ended up unstyled.
    //
    // The check is limited to the properties that DEFINE the shared chrome's
    // geometry. A deliberate local override (the list panel wanting a tighter
    // error-card margin) is legitimate and must not be flagged, and other
    // classes that happen to share a property value (.forge-header also uses
    // --header-height) are their own rules, not copies.
    //
    // Anchored per-class so only a re-declaration OF THAT CLASS trips it.
    const mustBeGlobal: Array<{ cls: string; props: string[] }> = [
      { cls: 'forge-detail-header', props: ['justify-content: space-between', 'height: var(--header-height)'] },
      { cls: 'forge-back', props: ['color: var(--accent-color)'] },
      { cls: 'forge-icon-btn', props: ['width: 28px', 'border-radius: var(--radius-lg)'] },
      { cls: 'forge-detail-body', props: ['overflow-y: auto'] },
      { cls: 'forge-state-dot', props: ['border-radius: 50%'] },
      { cls: 'forge-state-badge', props: ['border-radius: var(--radius-full)'] },
      { cls: 'forge-error-card', props: ['align-items: center'] },
      { cls: 'forge-loading', props: ['justify-content: center'] },
    ]

    for (const rel of SCOPED_COMPONENTS) {
      const src = read(rel)
      const marker = '<style scoped>'
      if (!src.includes(marker)) continue
      const after = src.slice(src.indexOf(marker) + marker.length)
      const block = after.includes('</style>') ? after.slice(0, after.indexOf('</style>')) : after

      for (const { cls, props } of mustBeGlobal) {
        const rule = block.match(new RegExp(`\\.${cls}\\s*\\{([\\s\\S]*?)\\}`))
        if (!rule) continue
        for (const prop of props) {
          expect(
            rule[1].includes(prop),
            `${rel} re-declares .${cls} (${prop}) — it belongs to the shared chrome`,
          ).toBe(false)
        }
      }
    }
  })
})

/**
 * Regression guard: forge tab labels must ellipsise, not wrap.
 *
 * The tab bar is a fixed 34px strip (`.forge-tabs { height: 34px }`), so a label
 * that does not fit has nowhere to wrap to: the second line is clipped mid-glyph
 * (or the bar grows and breaks the panel's fixed header geometry). Truncating is
 * what makes a too-narrow tab read as a tab.
 *
 * The label is a flex item, so `min-width: 0` is required too — without it a flex
 * item refuses to shrink below its content width and the ellipsis never engages.
 */
describe('forge tab label truncation', () => {
  const panel = readFileSync(
    join(__dirname, '..', 'ForgePanelContent.vue'),
    'utf8',
  )

  /** The declaration block for one selector, or '' when absent. */
  function block(selector: string): string {
    const i = panel.indexOf(`${selector} {`)
    if (i === -1) return ''
    return panel.slice(i, panel.indexOf('}', i))
  }

  it('gives the label a dedicated class in the template', () => {
    expect(panel).toContain('class="forge-tab-label"')
  })

  it('ellipsises instead of wrapping', () => {
    const b = block('.forge-tab-label')
    expect(b, '.forge-tab-label rule must exist').not.toBe('')
    expect(b).toContain('white-space: nowrap')
    expect(b).toContain('text-overflow: ellipsis')
    expect(b).toContain('overflow: hidden')
  })

  it('lets the label shrink below its content width', () => {
    // Without min-width:0 the flex item keeps its intrinsic width and the
    // ellipsis never triggers — the label would overflow the tab instead.
    expect(block('.forge-tab-label')).toContain('min-width: 0')
  })

  it('keeps the icon from being squeezed', () => {
    // The icon must hold its size; only the text shrinks.
    expect(panel).toMatch(/\.forge-tab\s*>\s*:deep\(svg\)[^}]*flex-shrink:\s*0/)
  })
})
