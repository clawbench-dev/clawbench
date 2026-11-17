import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The notifications settings page must use the shared settings page surface.
 *
 * This custom plugin page replaces the standard `.settings-category` wrapper
 * (which paints `--panel-page-bg`), so it carries its own background. It used
 * to paint `--bg-secondary` — the CARD colour — across the whole page: the page
 * became one flat slab, the SettingsCard inside lost its contrast step, and the
 * tab read darker than every other settings tab (real-instance screenshot
 * comparison, 2026-11-17).
 *
 * jsdom has no layout engine, so this is a source-sniffing check (the pattern
 * used by notifFilterRow.css.test.ts). cwd differs between a bare `vitest` run
 * (web/) and scripts/vitest-run.sh (repo root), so probe both.
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

/** The `<style>` block of a .vue source (the template has braces that would confuse a rule regex). */
function styleBlock(sfc: string): string {
  const m = sfc.match(/<style[^>]*>([\s\S]*?)<\/style>/)
  return m ? m[1] : ''
}

/** The declaration block of the first rule whose selector is exactly `.cls`. */
function baseRule(sfc: string, cls: string): string | null {
  const target = `.${cls}`
  for (const m of stripComments(styleBlock(sfc)).matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    if (m[1].trim() === target) return m[2]
  }
  return null
}

const page = readWebFile('src/components/settings/NotificationsSetting.vue')

describe('notifications settings page uses the shared page surface', () => {
  it('paints --panel-page-bg, the surface SettingsCategory uses', () => {
    const decls = baseRule(page, 'notifications-setting')
    expect(decls, '.notifications-setting rule must exist').not.toBeNull()
    expect(decls, 'the page surface must be the shared --panel-page-bg').toMatch(
      /background:\s*var\(--panel-page-bg\)/,
    )
  })

  it('never paints the card colour across the whole page', () => {
    const decls = baseRule(page, 'notifications-setting')
    // Regression shape: `background: var(--bg-secondary)` on the page root.
    expect(decls).not.toMatch(/background:\s*var\(--bg-secondary\)/)
  })
})
