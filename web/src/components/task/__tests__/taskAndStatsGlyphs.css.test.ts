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
 *  2. the stats tab strip clips its own overflow and scrolls sideways now that
 *     each tab carries an icon — a narrow panel must not paint the last tab
 *     over the refresh button and the panel's window controls.
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

  it('scrolls the strip sideways instead of squeezing or overflowing it', () => {
    // A narrow panel used to let the row overflow its box, painting the last
    // tab over the refresh button and under the window controls. The strip now
    // owns the overflow: it clips at its own edge and scrolls.
    const list = baseRule(src, 'stats-tab-list')
    expect(list, '.stats-tab-list rule must exist').not.toBeNull()
    expect(list).toMatch(/overflow-x:\s*auto/)
    expect(list).toMatch(/overflow-y:\s*hidden/)
    expect(list).toMatch(/scrollbar-width:\s*none/)
    expect(src).toContain('.stats-tab-list::-webkit-scrollbar')

    // Tabs keep their natural width — shrinking is what squeezed the labels.
    const tab = baseRule(src, 'stats-tab')
    expect(tab, '.stats-tab rule must exist').not.toBeNull()
    expect(tab).toMatch(/flex:\s*0 0 auto/)
    expect(tab).toMatch(/white-space:\s*nowrap/)
  })

  it('keeps the tab icon from being squeezed', () => {
    expect(src).toMatch(/\.stats-tab\s*>\s*:deep\(svg\)[^}]*flex-shrink:\s*0/)
  })
})
