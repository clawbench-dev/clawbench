import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Source-contract guard: the floating preview card has ONE width for every
 * target type.
 *
 * Media cards used to widen to 960px while code cards stayed at 720px, so
 * clicking a PNG and then the file beside it resized the card mid-read. The
 * width now lives only in the base `.code-link-preview-floating` rule and
 * `.is-media` overrides nothing but `max-height`.
 *
 * jsdom has no CSS engine, so a mounted component cannot observe this — the
 * declarations are read off the stylesheet (same pattern as
 * wideDockIconSize.css.test.ts).
 */

const css = readFileSync(
  join(__dirname, '..', 'code-link-preview.css'),
  'utf8',
)

function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Declaration block of the first rule whose selector is exactly `sel`. */
function ruleDecls(sel: string): string | null {
  const escaped = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = stripComments(css).match(
    new RegExp(`(?:^|[},])\\s*${escaped}\\s*\\{([^}]*)\\}`),
  )
  return m ? m[1] : null
}

describe('code link preview card width', () => {
  it('defines the single card width on the base rule', () => {
    const decls = ruleDecls('.code-link-preview-floating')
    expect(decls, 'base card rule must exist').not.toBeNull()
    expect(decls).toMatch(/width:\s*min\(\s*calc\(\s*720\s*\*\s*var\(--dp\)\s*\)\s*,\s*calc\(100vw - calc\(\s*24\s*\*\s*var\(--dp\)\s*\)\)\s*\)/)
  })

  it('does not give media cards a wider width than code cards', () => {
    const decls = ruleDecls('.code-link-preview-floating.is-media')
    expect(decls, '.is-media rule must exist').not.toBeNull()
    // Width must come from the base rule only. A `width` declaration here is
    // exactly the regression this guard exists for.
    expect(decls).not.toMatch(/(^|;)\s*width\s*:/)
  })

  it('keeps the taller media height cap', () => {
    const decls = ruleDecls('.code-link-preview-floating.is-media')
    expect(decls).toMatch(/max-height:\s*min\(88vh,\s*calc\(100vh - calc\(\s*24\s*\*\s*var\(--dp\)\s*\)\)\)/)
  })
})
