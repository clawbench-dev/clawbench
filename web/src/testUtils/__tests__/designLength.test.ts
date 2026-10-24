import { describe, it, expect } from 'vitest'

import { designPx, normalizeDp } from '@/testUtils/designLength'

/**
 * The helpers themselves are test infrastructure, so their own contract has to
 * be pinned: a guard that silently returned NaN (or left the `--dp` form
 * unnormalised) would make every CSS guard built on it pass vacuously.
 */
describe('designPx', () => {
  it('reads the number out of the scaled form', () => {
    expect(designPx('.rail { width: calc(3 * var(--dp)); }', 'width')).toBe(3)
    expect(designPx('.rail { width: calc( 1.5 * var(--dp) ); }', 'width')).toBe(1.5)
  })

  it('prefers the scaled form when both are present', () => {
    const css = '.x { width: calc(4 * var(--dp)); width: 9px; }'
    expect(designPx(css, 'width')).toBe(4)
  })

  it('falls back to a literal px value', () => {
    // Hairlines and scrollbar widths deliberately do NOT scale, so they stay
    // literal and the helper must still read them.
    expect(designPx('.x { width: 1px; }', 'width')).toBe(1)
    expect(designPx('.x { width: -2px; }', 'width')).toBe(-2)
  })

  it('returns NaN when the property is absent', () => {
    expect(designPx('.x { height: calc(3 * var(--dp)); }', 'width')).toBeNaN()
  })

  it('is anchored on the property name', () => {
    // A `width` lookup must not be satisfied by `min-width` / `max-width`, or a
    // guard for one declaration would silently pass on a different one.
    expect(designPx('.x { min-width: calc(3 * var(--dp)); }', 'width')).toBeNaN()
    expect(designPx('.x { max-height: 4px; }', 'height')).toBeNaN()
    // …but a real match at the start of the declaration still works.
    expect(designPx('width: calc(3 * var(--dp));', 'width')).toBe(3)
    expect(designPx('.x { min-width: 5px; }', 'min-width')).toBe(5)
  })
})

describe('normalizeDp', () => {
  it('rewrites the scaled form as a plain px literal', () => {
    expect(normalizeDp('.x { padding: calc(9 * var(--dp)) calc(11 * var(--dp)); }')).toBe(
      '.x { padding: 9px 11px; }',
    )
  })

  it('rewrites every occurrence, not just the first', () => {
    const out = normalizeDp('a { margin: calc(1 * var(--dp)); } b { margin: calc(2 * var(--dp)); }')
    expect(out).toContain('margin: 1px')
    expect(out).toContain('margin: 2px')
    expect(out).not.toContain('--dp')
  })

  it('leaves literal px values alone', () => {
    // A value that deliberately does not scale must not be normalised into
    // agreement with a scaled one.
    expect(normalizeDp('.x { border: 1px solid; }')).toBe('.x { border: 1px solid; }')
  })

  it('collapses whitespace left behind by the substitution', () => {
    expect(normalizeDp('.x { gap: calc(3 * var(--dp))  calc(4 * var(--dp)); }')).toBe(
      '.x { gap: 3px 4px; }',
    )
  })

  it('drops a fractional design value to its numeric form', () => {
    expect(normalizeDp('width: calc(0.5 * var(--dp));')).toBe('width: 0.5px;')
  })
})
