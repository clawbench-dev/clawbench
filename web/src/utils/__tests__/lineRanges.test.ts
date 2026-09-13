import { describe, expect, it } from 'vitest'
import {
  parseLineRanges,
  serializeLineRanges,
  firstLineTarget,
  flattenLineNumbers,
  clampRanges,
  LINE_SUFFIX_RE,
  LINE_FRAGMENT_RE,
  MAX_LINE_RANGES,
} from '@/utils/lineRanges'

describe('parseLineRanges', () => {
  it('parses a single line', () => {
    expect(parseLineRanges('10')).toEqual([{ start: 10, end: 10 }])
  })

  it('parses a single range', () => {
    expect(parseLineRanges('10-20')).toEqual([{ start: 10, end: 20 }])
  })

  it('parses the canonical multi-range example', () => {
    expect(parseLineRanges('90-91,309,324,343,938-943')).toEqual([
      { start: 90, end: 91 },
      { start: 309, end: 309 },
      { start: 324, end: 324 },
      { start: 343, end: 343 },
      { start: 938, end: 943 },
    ])
  })

  it('tolerates whitespace after commas', () => {
    expect(parseLineRanges('90-91, 309,  324 ,343')).toEqual([
      { start: 90, end: 91 },
      { start: 309, end: 309 },
      { start: 324, end: 324 },
      { start: 343, end: 343 },
    ])
  })

  it('accepts the L prefix on either side of a range', () => {
    expect(parseLineRanges('L90-L91,L309')).toEqual([
      { start: 90, end: 91 },
      { start: 309, end: 309 },
    ])
    expect(parseLineRanges('L90-91')).toEqual([{ start: 90, end: 91 }])
    expect(parseLineRanges('l10')).toEqual([{ start: 10, end: 10 }])
  })

  it('sorts unsorted input ascending', () => {
    expect(parseLineRanges('343,90-91,309')).toEqual([
      { start: 90, end: 91 },
      { start: 309, end: 309 },
      { start: 343, end: 343 },
    ])
  })

  it('merges overlapping and adjacent ranges', () => {
    expect(parseLineRanges('10-20,15-25')).toEqual([{ start: 10, end: 25 }])
    // Adjacent (end + 1) collapses too, so flashing does not double-add a line.
    expect(parseLineRanges('10-20,21-30')).toEqual([{ start: 10, end: 30 }])
  })

  it('de-duplicates identical tokens', () => {
    expect(parseLineRanges('5,5,5')).toEqual([{ start: 5, end: 5 }])
  })

  it('degrades an inverted range to a single line at the start', () => {
    expect(parseLineRanges('20-10')).toEqual([{ start: 20, end: 20 }])
  })

  it('drops zero and malformed tokens individually', () => {
    expect(parseLineRanges('0')).toEqual([])
    expect(parseLineRanges('0,10')).toEqual([{ start: 10, end: 10 }])
    expect(parseLineRanges('10,foo,20')).toEqual([
      { start: 10, end: 10 },
      { start: 20, end: 20 },
    ])
  })

  it('returns empty for empty/nullish input and trailing commas', () => {
    expect(parseLineRanges('')).toEqual([])
    expect(parseLineRanges(undefined)).toEqual([])
    expect(parseLineRanges(null)).toEqual([])
    expect(parseLineRanges('10,')).toEqual([{ start: 10, end: 10 }])
  })

  it('caps the number of ranges at MAX_LINE_RANGES', () => {
    // Spaced out so adjacent tokens never merge.
    const suffix = Array.from({ length: MAX_LINE_RANGES + 50 }, (_, i) => String(i * 2 + 1)).join(',')
    expect(parseLineRanges(suffix)).toHaveLength(MAX_LINE_RANGES)
  })
})

describe('serializeLineRanges', () => {
  it('round-trips the canonical form', () => {
    const ranges = parseLineRanges('90-91,309,324,343,938-943')
    expect(serializeLineRanges(ranges)).toBe('90-91,309,324,343,938-943')
  })

  it('normalizes unsorted input to canonical order', () => {
    expect(serializeLineRanges(parseLineRanges('343,90-91'))).toBe('90-91,343')
  })
})

describe('firstLineTarget', () => {
  it('returns the earliest range with lineEnd for a span', () => {
    expect(firstLineTarget(parseLineRanges('90-91,309,938-943'))).toEqual({ lineStart: 90, lineEnd: 91 })
  })

  it('omits lineEnd for a single line', () => {
    expect(firstLineTarget(parseLineRanges('309,938-943'))).toEqual({ lineStart: 309 })
  })

  it('returns an empty object for no ranges', () => {
    expect(firstLineTarget([])).toEqual({})
  })
})

describe('flattenLineNumbers', () => {
  it('returns ascending de-duplicated line numbers', () => {
    expect(flattenLineNumbers(parseLineRanges('3-5,4,10'))).toEqual([3, 4, 5, 10])
  })

  it('honors the cap', () => {
    const lines = flattenLineNumbers(parseLineRanges('1-1000'), 10)
    expect(lines).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10])
  })
})

describe('clampRanges', () => {
  it('intersects ranges with a window', () => {
    expect(clampRanges(parseLineRanges('90-91,309,938-943'), 100, 400)).toEqual([
      { start: 309, end: 309 },
    ])
  })

  it('splits a range straddling the window edges', () => {
    expect(clampRanges(parseLineRanges('50-200'), 100, 150)).toEqual([{ start: 100, end: 150 }])
  })

  it('returns empty when nothing intersects', () => {
    expect(clampRanges(parseLineRanges('90-91'), 500, 600)).toEqual([])
  })
})

describe('LINE_SUFFIX_RE / LINE_FRAGMENT_RE', () => {
  it('strips a trailing multi-range suffix', () => {
    expect('src/main.go:90-91,309'.replace(LINE_SUFFIX_RE, '')).toBe('src/main.go')
  })

  it('strips a trailing L-prefixed range', () => {
    expect('src/main.go:L10-L20'.replace(LINE_SUFFIX_RE, '')).toBe('src/main.go')
  })

  it('matches a bare token list as a hash fragment', () => {
    expect(LINE_FRAGMENT_RE.test('L90-L91,309')).toBe(true)
    expect(LINE_FRAGMENT_RE.test('section')).toBe(false)
  })

  it('does not treat prose after a comma as a range', () => {
    // `:10, and then` — the comma group fails (no digits), so only `:10` matches.
    const m = 'src/main.go:10, and then'.match(LINE_SUFFIX_RE)
    expect(m).toBeNull()
    expect('src/main.go:10'.replace(LINE_SUFFIX_RE, '')).toBe('src/main.go')
  })
})
