import { describe, expect, it } from 'vitest'
import { highlightRanges, mergeRanges, parentDirOf } from '@/utils/contentSearchMark'

describe('mergeRanges', () => {
  it('sorts ranges by start', () => {
    expect(mergeRanges([{ start: 10, end: 12 }, { start: 2, end: 4 }], 20))
      .toEqual([{ start: 2, end: 4 }, { start: 10, end: 12 }])
  })

  it('merges overlapping ranges', () => {
    expect(mergeRanges([{ start: 0, end: 5 }, { start: 3, end: 8 }], 20))
      .toEqual([{ start: 0, end: 8 }])
  })

  it('merges touching ranges into one span', () => {
    expect(mergeRanges([{ start: 0, end: 3 }, { start: 3, end: 6 }], 20))
      .toEqual([{ start: 0, end: 6 }])
  })

  it('keeps disjoint ranges separate', () => {
    expect(mergeRanges([{ start: 0, end: 3 }, { start: 5, end: 8 }], 20))
      .toEqual([{ start: 0, end: 3 }, { start: 5, end: 8 }])
  })

  it('clamps ranges to the text length', () => {
    expect(mergeRanges([{ start: 0, end: 999 }], 4)).toEqual([{ start: 0, end: 4 }])
  })

  it('drops inverted and empty ranges', () => {
    expect(mergeRanges([{ start: 5, end: 5 }, { start: 9, end: 3 }], 20)).toEqual([])
  })
})

describe('highlightRanges', () => {
  it('escapes HTML when there are no ranges', () => {
    expect(highlightRanges('<script>', [])).toBe('&lt;script&gt;')
  })

  it('wraps the matched span in mark', () => {
    expect(highlightRanges('hello world', [{ start: 6, end: 11 }]))
      .toBe('hello <mark>world</mark>')
  })

  it('escapes surrounding text', () => {
    // 'a<b>needle' — rune offsets: a=0, <=1, b=2, >=3, n=4, so the match at
    // rune 4 is "needle".
    expect(highlightRanges('a<b>needle', [{ start: 4, end: 10 }]))
      .toBe('a&lt;b&gt;<mark>needle</mark>')
  })

  it('escapes the matched text itself', () => {
    // The match is the literal "<x>" — it must not become markup.
    expect(highlightRanges('<x>', [{ start: 0, end: 3 }]))
      .toBe('<mark>&lt;x&gt;</mark>')
  })

  it('handles multiple ranges', () => {
    expect(highlightRanges('aXbYc', [{ start: 1, end: 2 }, { start: 3, end: 4 }]))
      .toBe('a<mark>X</mark>b<mark>Y</mark>c')
  })

  it('merges overlapping ranges instead of nesting marks', () => {
    expect(highlightRanges('aaa', [{ start: 0, end: 2 }, { start: 1, end: 3 }]))
      .toBe('<mark>aaa</mark>')
  })

  it('uses RUNE offsets, not UTF-16 indices (CJK)', () => {
    // "中文needle": the match starts at rune 2. A UTF-16/byte interpretation
    // would land in the middle of a character.
    expect(highlightRanges('中文needle', [{ start: 2, end: 8 }]))
      .toBe('中文<mark>needle</mark>')
  })

  it('uses RUNE offsets across surrogate pairs (emoji)', () => {
    // The emoji is one code point but two UTF-16 units.
    expect(highlightRanges('😀ab', [{ start: 1, end: 3 }]))
      .toBe('😀<mark>ab</mark>')
  })

  it('returns escaped text when ranges are undefined', () => {
    expect(highlightRanges('a & b', undefined)).toBe('a &amp; b')
  })

  it('ignores out-of-bounds ranges', () => {
    expect(highlightRanges('ab', [{ start: 50, end: 60 }])).toBe('ab')
  })
})

describe('parentDirOf', () => {
  it('returns the directory portion', () => {
    expect(parentDirOf('src/a.go')).toBe('src')
  })

  it('returns empty for a root-level file', () => {
    expect(parentDirOf('a.go')).toBe('')
  })

  it('handles nested paths', () => {
    expect(parentDirOf('a/b/c.go')).toBe('a/b')
  })

  it('returns empty for a leading-slash path with no directory', () => {
    // lastIndexOf('/') === 0 is the "no directory" case.
    expect(parentDirOf('/a.go')).toBe('')
  })
})
