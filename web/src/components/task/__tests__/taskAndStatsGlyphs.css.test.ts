import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source-sniffing guard for two panel-header/tab glyph changes.
 *
 * jsdom has no CSS engine and does not apply the stylesheet, so the layout
 * invariants below cannot be observed from a mounted component — they are read
 * off the source, the same pattern as forgeDetailChrome.css.test.ts.
 *
 * What is pinned:
 *  1. the task breadcrumb's root glyph holds its size in a scrolling flex row,
 *     and its crumb became a flex row (otherwise the icon and label sit on
 *     different baselines);
 *  2. the stats tab bar keeps its labels from wrapping now that each tab
 *     carries an icon — the bar is a fixed 34px strip, so a wrapping label is
 *     clipped mid-line instead of reflowing.
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

describe('task breadcrumb root glyph', () => {
  const src = readWebFile('src/components/task/TaskBreadcrumb.vue')

  it('renders the glyph on the root crumb only', () => {
    // One crumb-icon in the template: the root. Adding it to every crumb would
    // turn the row into a row of repeated icons.
    const count = (src.match(/class="crumb-icon"/g) || []).length
    expect(count, 'exactly one .crumb-icon is expected (the root crumb)').toBe(1)
  })

  it('makes the crumb a flex row so the glyph and label share a baseline', () => {
    const decls = baseRule(src, 'crumb')
    expect(decls, '.crumb rule must exist').not.toBeNull()
    expect(decls).toMatch(/display:\s*inline-flex/)
    expect(decls).toMatch(/align-items:\s*center/)
  })

  it('keeps the glyph from being squeezed by a long task name', () => {
    // The crumbs live in one overflow-x:auto row, so a flex item without
    // min-width:0/flex-shrink:0 would be compressed by a long neighbour.
    expect(baseRule(src, 'crumb-icon')).toMatch(/flex-shrink:\s*0/)
  })

  it('imports the glyph so the crumb is not empty', () => {
    // A missing import renders no svg at all, which a class-existence assertion
    // would not catch (concurrent_git_safety §9).
    const imports = src.match(/import\s*\{([^}]*)\}\s*from\s*'lucide-vue-next'/)
    expect(imports, 'a lucide import must exist').not.toBeNull()
    expect(imports![1]).toContain('Clock')
  })
})

describe('stats tab bar with icons', () => {
  const src = readWebFile('src/components/stats/StatsTabHost.vue')

  it('gives every section an icon in the registry', () => {
    // Declared as data, like the forge tab registry: a tab added without an icon
    // would render a bare label and be easy to miss.
    // Slice to the array BODY (after `= [`) — the preceding type annotation
    // contains the same `id`/`icon` keys and would inflate the counts.
    const start = src.indexOf('const sections')
    expect(start, 'the sections registry must exist').toBeGreaterThan(-1)
    const open = src.indexOf('= [', start)
    expect(open, 'the registry must be an array literal').toBeGreaterThan(-1)
    const end = src.indexOf('\n]', open)
    expect(end, 'the array must be terminated').toBeGreaterThan(-1)
    const body = src.slice(open + 3, end)
    const entries = body.match(/\{ id:/g) || []
    const icons = body.match(/icon:/g) || []
    expect(entries.length).toBe(3)
    expect(icons.length, 'every section needs an icon').toBe(entries.length)
  })

  it('imports each icon it renders', () => {
    const imports = src.match(/import\s*\{([^}]*)\}\s*from\s*'lucide-vue-next'/)
    expect(imports, 'a lucide import must exist').not.toBeNull()
    for (const name of ['Coins', 'Files', 'GitCommitHorizontal']) {
      expect(imports![1], `${name} must be imported`).toContain(name)
    }
  })

  it('renders the icon through the registry, not a hard-coded tag', () => {
    expect(src).toContain(':is="s.icon"')
  })

  it('ellipsises the label instead of wrapping in the fixed-height strip', () => {
    const decls = baseRule(src, 'stats-tab-label')
    expect(decls, '.stats-tab-label rule must exist').not.toBeNull()
    expect(decls).toMatch(/white-space:\s*nowrap/)
    expect(decls).toMatch(/text-overflow:\s*ellipsis/)
    expect(decls).toMatch(/overflow:\s*hidden/)
    // Without min-width:0 the flex item keeps its intrinsic width and the
    // ellipsis never engages.
    expect(decls).toMatch(/min-width:\s*0/)
  })

  it('keeps the tab icon from being squeezed', () => {
    expect(src).toMatch(/\.stats-tab\s*>\s*:deep\(svg\)[^}]*flex-shrink:\s*0/)
  })
})
