import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Two sizing rules the running meeting owes the rest of the UI.
 *
 * Both are invisible to every other kind of test: jsdom applies no stylesheets
 * (so a component test cannot see them), the type checker does not read CSS,
 * and the failure mode is cosmetic-but-real — a badge that spills out of its
 * pill once the interface scale goes up, and row buttons a thumb cannot hit.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')

function styleOf(file: string): string {
  const src = readFileSync(join(ROOT, 'web/src/components/live', file), 'utf8')
  const start = src.indexOf('<style')
  if (start < 0) throw new Error(`${file} has no <style> block`)
  return src.slice(start)
}

/** Comments carry braces and the words we search for; drop them first. */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

function ruleDecls(css: string, selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`(?:^|[\\s,}])${escaped}\\s*\\{([^}]*)\\}`))
  if (!m) throw new Error(`rule ${selector} not found`)
  const out: Record<string, string> = {}
  for (const decl of m[1].split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    const prop = decl.slice(0, i).trim()
    const value = decl.slice(i + 1).trim().replace(/\s+/g, ' ')
    if (prop) out[prop] = value
  }
  return out
}

/** The body of one `@media <query>` block (brace-matched, comments removed). */
function mediaBlock(css: string, query: string): string {
  const at = css.indexOf(`@media ${query}`)
  if (at < 0) throw new Error(`no @media ${query} block`)
  const open = css.indexOf('{', at)
  let depth = 0
  for (let j = open; j < css.length; j++) {
    if (css[j] === '{') depth += 1
    else if (css[j] === '}') {
      depth -= 1
      if (depth === 0) return css.slice(open + 1, j)
    }
  }
  throw new Error(`unbalanced @media ${query} block`)
}

/** `44 * var(--dp)` → 44; the unit is the token's job, not the number's. */
function dpOf(value: string): number | null {
  const m = value.match(/^calc\(\s*([\d.]+)\s*\*\s*var\(--dp\)\s*\)$/)
  return m ? Number(m[1]) : null
}

describe('unread badge follows the interface scale', () => {
  // The font size was already in --dp while the box was literal px, so at a
  // scale of 1.3 the number overflowed its capsule.
  it('sizes the .live-count pill in --dp, like its font', () => {
    const decls = ruleDecls(stripComments(styleOf('MeetingStage.vue')), '.live-count')

    expect(decls['font-size']).toBe('calc(10 * var(--dp))')
    for (const prop of ['min-width', 'padding', 'margin-left', 'line-height']) {
      const value = decls[prop]
      expect(value, `${prop} must scale with the root font size`).toContain('var(--dp)')
      // Every geometry in the value, not just the first: `padding: 0 3px` would
      // otherwise pass on the leading zero.
      for (const token of value.split(/\s+(?![^()]*\))/)) {
        if (token === '0') continue
        expect(token, `${prop}: "${token}" is a fixed size`).not.toMatch(/^[\d.]+px$/)
      }
    }
  })
})

describe('participant row actions are a touch target', () => {
  // Four icon buttons sit side by side with a 2px gap: a thumb aiming at
  // 「移出成员」 lands on 「停止共享」 instead, and that one acts immediately.
  it('grows .mp-action to 44dp on a coarse pointer', () => {
    const css = stripComments(styleOf('MeetingParticipants.vue'))
    const decls = ruleDecls(mediaBlock(css, '(pointer: coarse)'), '.mp-action')

    expect(dpOf(decls['min-width'] ?? '')).toBeGreaterThanOrEqual(44)
    expect(dpOf(decls['height'] ?? '')).toBeGreaterThanOrEqual(44)
  })
})
