import { describe, expect, it } from 'vitest'
import { highlightTextByPositions } from '../searchUtils'

describe('highlightTextByPositions', () => {
  it('returns escaped plain text when no positions', () => {
    expect(highlightTextByPositions('hello world', [])).toBe('hello world')
    expect(highlightTextByPositions('hello world', null as any)).toBe('hello world')
  })

  it('wraps single match position in <mark> tags', () => {
    expect(highlightTextByPositions('hello world', [{ start: 6, end: 11 }])).toBe(
      'hello <mark>world</mark>',
    )
  })

  it('wraps multiple non-overlapping positions in <mark> tags', () => {
    expect(
      highlightTextByPositions('foo bar baz', [
        { start: 0, end: 3 },
        { start: 8, end: 11 },
      ]),
    ).toBe('<mark>foo</mark> bar <mark>baz</mark>')
  })

  it('highlights Chinese text with rune positions correctly', () => {
    // "你好世界" — 4 runes at positions 0,1,2,3
    expect(highlightTextByPositions('你好世界', [{ start: 2, end: 4 }])).toBe(
      '你好<mark>世界</mark>',
    )
  })

  it('returns empty string for empty text', () => {
    expect(highlightTextByPositions('', [{ start: 0, end: 1 }])).toBe('')
  })

  it('clamps positions beyond text length safely', () => {
    // text is 5 chars "hello", position end=100 should be clamped
    expect(highlightTextByPositions('hello', [{ start: 0, end: 100 }])).toBe(
      '<mark>hello</mark>',
    )
    expect(highlightTextByPositions('hello', [{ start: 50, end: 100 }])).toBe('hello')
  })

  it('escapes HTML entities to prevent XSS', () => {
    expect(
      highlightTextByPositions('<script>alert(1)</script>', [{ start: 0, end: 25 }]),
    ).toBe('<mark>&lt;script&gt;alert(1)&lt;/script&gt;</mark>')
  })

  it('escapes unhighlighted portions too', () => {
    // '<b>' at rune positions 2-5 gets escaped; rest stays plain
    expect(
      highlightTextByPositions('a <b>bold</b> c', [{ start: 2, end: 5 }]),
    ).toBe('a <mark>&lt;b&gt;</mark>bold&lt;/b&gt; c')
  })

  it('handles emoji (surrogate pairs) correctly', () => {
    // "hi👋world" — "👋" is a single rune but 2 UTF-16 code units
    const text = 'hi\u{1F44B}world'
    // runes: h(0), i(1), 👋(2), w(3), o(4), r(5), l(6), d(7)
    // string indices: h=0, i=1, 👋=2, w=4, o=5, r=6, l=7, d=8
    expect(highlightTextByPositions(text, [{ start: 2, end: 3 }])).toBe(
      'hi<mark>\u{1F44B}</mark>world',
    )
  })

  it('handles start at beginning of text', () => {
    expect(highlightTextByPositions('hello', [{ start: 0, end: 2 }])).toBe(
      '<mark>he</mark>llo',
    )
  })

  it('handles match at end of text', () => {
    expect(highlightTextByPositions('hello', [{ start: 3, end: 5 }])).toBe(
      'hel<mark>lo</mark>',
    )
  })
})
