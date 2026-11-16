import { describe, expect, it } from 'vitest'
import { formatCreditsLabel } from '@/utils/modelCredits'

describe('formatCreditsLabel', () => {
  it('renders the compact form the agent client shows', () => {
    // CodeBuddy sends "x0.06 credits"; its own picker drops the word whenever
    // the column is narrow, and our row is narrower still.
    expect(formatCreditsLabel('x0.06 credits')).toBe('x0.06')
    // The digits are kept verbatim, exactly like the agent's own picker does.
    expect(formatCreditsLabel('x1.00 credits')).toBe('x1.00')
    expect(formatCreditsLabel('X2.5 CREDITS')).toBe('x2.5')
  })

  it('accepts the multiplication-sign spelling', () => {
    expect(formatCreditsLabel('×0.29 credits')).toBe('x0.29')
  })

  it('returns empty for a missing or blank multiplier', () => {
    expect(formatCreditsLabel(undefined)).toBe('')
    expect(formatCreditsLabel(null)).toBe('')
    expect(formatCreditsLabel('')).toBe('')
    expect(formatCreditsLabel('   ')).toBe('')
  })

  it('passes unrecognized text through unchanged', () => {
    // Inventing a number from an unknown shape would put a wrong multiplier in
    // front of the user; showing the agent's own words is the honest option.
    expect(formatCreditsLabel('1.5x')).toBe('1.5x')
    expect(formatCreditsLabel('included')).toBe('included')
  })
})
