import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The terminal page keeps a header row in the states that have no tab bar.
 *
 * With a session open the tab bar IS the header (icon + tabs + buttons). With
 * no session — or on a platform without a terminal — the page used to render
 * only a centred empty state, so its top edge was bare: every other dock tab
 * page has a header row there, and on narrow screens (no window-controls
 * overlay) the terminal had nothing at all.
 *
 * jsdom has no CSS engine, so these are source-sniffing checks (same pattern
 * as terminalTabBarAlignment.css.test.ts). The surface and horizontal inset
 * are asserted centrally in `src/__tests__/panelWindowControlsCoverage.test.ts`
 * (`.terminal-empty-header` is registered in both shared header rules there).
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

/** The `<style>` block of a .vue source (templates contain braces). */
function styleBlock(sfc: string): string {
  const m = sfc.match(/<style[^>]*>([\s\S]*?)<\/style>/)
  return m ? m[1] : ''
}

/** The declaration block of the first rule whose selector is exactly `.cls`. */
function baseRule(css: string, cls: string): string | null {
  const target = `.${cls}`
  for (const m of stripComments(css).matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    if (m[1].trim() === target) return m[2]
  }
  return null
}

const terminalPanel = readWebFile('src/components/terminal/TerminalPanelContent.vue')

describe('terminal header row without a tab bar', () => {
  it('renders the header for exactly the two tab-bar-less states', () => {
    // Guard the condition, not just the markup: a header that also shows next
    // to the tab bar would be a second row, which the one-row rule forbids.
    expect(terminalPanel).toMatch(
      /<div\s+v-if="platformUnsupported \|\| tabs\.length === 0"\s+class="terminal-empty-header"/,
    )
  })

  it('gives the header the section glyph and title like every other panel', () => {
    const header = terminalPanel.match(
      /<div v-if="platformUnsupported \|\| tabs\.length === 0" class="terminal-empty-header">([\s\S]*?)<\/div>/,
    )
    expect(header, 'the terminal empty header must exist').not.toBeNull()
    expect(header![1]).toContain('class="panel-header-icon"')
    expect(header![1]).toContain("t('terminal.title')")
  })

  it('owns its row geometry, since the wide-screen chrome rule is wide-only', () => {
    const decls = baseRule(styleBlock(terminalPanel), 'terminal-empty-header')
    expect(decls, '.terminal-empty-header rule must exist').not.toBeNull()
    expect(decls!).toMatch(/height:\s*var\(--header-height\)/)
    expect(decls!).toMatch(/border-bottom:\s*1px solid/)
    // Surface comes from the shared rule; a per-panel colour would drift again.
    expect(decls!).not.toMatch(/background:/)
  })
})
