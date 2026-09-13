import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Drift guard for the typography tokens in `css/variables.css`.
 *
 * Why this exists: component tests that assert a font declaration run under
 * jsdom, which does NOT resolve `var()`. `getComputedStyle(el).fontSize` hands
 * back the literal string `var(--font-size-2xl)`, so those assertions only
 * prove *which token name* was written — not that the token means 16px. The
 * invariant they are really protecting ("the textarea stays 16px so mobile
 * Safari does not zoom on focus") would silently break if someone retuned the
 * token, and every component test would still pass.
 *
 * This spec closes that gap by pinning the token values themselves. It reads
 * the raw stylesheet, following the pattern already used for the font stacks
 * in `utils/__tests__/fontConfig.test.ts`.
 *
 * If a value here needs to change, change it deliberately — and expect to
 * revisit the components whose layout depends on it.
 */

function readCss(relPath: string): string {
  // cwd differs between a bare `vitest` run (web/) and scripts/vitest-run.sh
  // (repo root), so probe both, as other source-sniffing specs here do.
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${relPath} not found from cwd: ${process.cwd()}`)
}

const css = readCss('css/variables.css')

/** The value of a custom property as declared in the `:root` block. */
function token(name: string): string {
  const m = css.match(new RegExp(`${name}:\\s*([^;]+);`))
  expect(m, `token ${name} should be defined`).not.toBeNull()
  return m![1].trim()
}

describe('typography tokens (variables.css)', () => {
  it('defines the seven font-size steps at their documented px values', () => {
    expect(token('--font-size-2xs')).toBe('10px')
    expect(token('--font-size-xs')).toBe('11px')
    expect(token('--font-size-sm')).toBe('12px')
    expect(token('--font-size-md')).toBe('13px')
    expect(token('--font-size-lg')).toBe('14px')
    expect(token('--font-size-xl')).toBe('15px')
    expect(token('--font-size-2xl')).toBe('16px')
  })

  it('keeps --font-size-xl equal to the html/body base size', () => {
    // base.css sizes the document from this token; a mismatch would make every
    // rem-free px value in the app render at an unexpected scale.
    const base = readCss('css/base.css')
    expect(base).toMatch(/font-size:\s*var\(--font-size-xl\)/)
    expect(token('--font-size-xl')).toBe('15px')
  })

  it('defines the three font-weight steps', () => {
    expect(token('--font-weight-medium')).toBe('500')
    expect(token('--font-weight-semibold')).toBe('600')
    expect(token('--font-weight-bold')).toBe('700')
  })

  it('defines the four line-height steps as unitless values', () => {
    expect(token('--line-height-tight')).toBe('1.2')
    expect(token('--line-height-snug')).toBe('1.4')
    expect(token('--line-height-normal')).toBe('1.5')
    expect(token('--line-height-relaxed')).toBe('1.6')
  })

  it('does not redeclare typography tokens per theme', () => {
    // These are layout/typography values, not colours: they live once in
    // :root. A theme block redefining them would be a copy-paste slip that
    // makes one theme render at a different scale.
    for (const name of [
      '--font-size-md',
      '--font-weight-semibold',
      '--line-height-normal',
    ]) {
      const occurrences = css.split('\n').filter((l) => l.trim().startsWith(`${name}:`))
      expect(occurrences, `${name} should be declared exactly once`).toHaveLength(1)
    }
  })
})
