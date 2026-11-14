import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Every `.header-btn` must centre its icon and label.
 *
 * A <button> defaults to `display: inline-block`, so a lucide <svg> and a
 * `<span class="action-label">` inside it are laid out as inline content on the
 * text baseline. As long as the ActionBar has no room for labels nobody notices
 * (the icon is alone and centred by the fixed square size). The moment the bar
 * widens enough to show labels, the glyph sits visibly high next to the word —
 * the knowledge-base header shipped exactly that way, because its `.header-btn`
 * rule only declared `border` and `cursor` while every other adopter of the
 * shared class also declared the flex centring.
 *
 * jsdom has no CSS engine, so this is a source-sniffing guard (same pattern as
 * settingsHeaderAlignment.css.test.ts). cwd differs between a bare `vitest` run
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

function webSrcDir(): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      readdirSync(join(base, 'src/components'))
      return join(base, 'src')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error('src/ not found from cwd: ' + process.cwd())
}

function vueFilesUnder(dir: string): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      if (entry.name === '__tests__' || entry.name === 'node_modules') continue
      out.push(...vueFilesUnder(full))
    } else if (entry.name.endsWith('.vue')) {
      out.push(full)
    }
  }
  return out
}

/** True when a `class="…"` attribute carries `header-btn` as an exact token. */
function usesHeaderBtn(sfc: string): boolean {
  for (const m of sfc.matchAll(/\bclass="([^"]*)"/g)) {
    if (m[1].split(/\s+/).includes('header-btn')) return true
  }
  return false
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** The `<style>` block of a .vue source (templates would confuse the rule regex). */
function styleBlock(sfc: string): string {
  const m = sfc.match(/<style[^>]*>([\s\S]*?)<\/style>/)
  return m ? m[1] : ''
}

/**
 * The declaration block of the rule that targets `.cls`.
 *
 * Grouped selectors are accepted (`.create-btn, .header-btn { … }` is one rule
 * for both buttons), but a *compound* selector such as `.file-header-btn` or
 * `.crust-header-btn` is not the shared class and must not count.
 */
function baseRule(sfc: string, cls: string): string | null {
  const target = `.${cls}`
  for (const m of stripComments(styleBlock(sfc)).matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    const selectors = m[1].split(',').map((s) => s.trim())
    if (selectors.includes(target)) return m[2]
  }
  return null
}

const src = webSrcDir()
const adopters = vueFilesUnder(join(src, 'components')).filter((file) =>
  usesHeaderBtn(readFileSync(file, 'utf8')),
)

describe('ActionBar `.header-btn` icon/label alignment', () => {
  it('finds the adopters (guards against a silent empty scan)', () => {
    // A scan that matches nothing would make every assertion below vacuous.
    expect(adopters.length).toBeGreaterThanOrEqual(5)
  })

  it.each(adopters.map((file) => [file.slice(src.length + 1), file] as const))(
    '%s centres its button content',
    (_rel, file) => {
      const sfc = readFileSync(file, 'utf8')
      const decls = baseRule(sfc, 'header-btn')
      expect(decls, '.header-btn rule must exist').not.toBeNull()
      expect(decls, 'must be a flex box, not the inline-block default').toMatch(
        /display:\s*(inline-)?flex/,
      )
      expect(decls, 'must centre the icon against the label').toMatch(/align-items:\s*center/)
    },
  )
})
