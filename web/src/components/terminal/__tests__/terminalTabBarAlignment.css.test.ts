import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The terminal tab bar's right-hand icon buttons must stay vertically centred.
 *
 * The bar's height no longer comes from this component: the shared panel
 * header rule in components.css forces `.terminal-tab-bar` to
 * `--header-height` (36dp) in BOTH layouts so the window-controls overlay
 * lines up with every other panel header. The bar still lays its children out
 * with `align-items: stretch`, and a flex item that keeps a definite cross
 * size is aligned at the START of a taller line — so the buttons' old
 * `height: calc(28 * var(--dp))` left them pinned to the top, ~4dp above the
 * row's centre (visible as icons riding high next to the centred − / □
 * controls).
 *
 * jsdom has no CSS engine, so these are source-sniffing checks (same pattern
 * as settingsHeaderAlignment.css.test.ts). cwd differs between a bare
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

describe('terminal tab bar right-side buttons', () => {
  it('fills the row instead of carrying a fixed height', () => {
    const decls = baseRule(styleBlock(terminalPanel), 'terminal-tab-add')
    expect(decls, '.terminal-tab-add rule must exist').not.toBeNull()
    // A fixed height is what top-aligned the icons inside the taller bar.
    expect(decls, 'must not pin the button to a fixed height').not.toMatch(/[^-]height:\s*calc\(/)
    expect(decls, 'must not pin the button to a fixed height').not.toMatch(/[^-]height:\s*\d/)
    expect(decls, 'must stretch to the bar height').toMatch(/align-self:\s*stretch/)
  })
})
