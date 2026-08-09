import { describe, it, expect } from 'vitest'
import { EditorState } from '@codemirror/state'
import { getWordRangeAt } from '../codeWordRange'

const state = EditorState.create({ doc: 'const fooBar = 123\nreturn fooBar.baz\n' })

describe('getWordRangeAt', () => {
  it('returns the whole identifier enclosing a position', () => {
    // 'fooBar' occupies doc offsets 6..12 on line 1
    expect(state.sliceDoc(6, 12)).toBe('fooBar')
    expect(state.sliceDoc(getWordRangeAt(state, 8)!.from, getWordRangeAt(state, 8)!.to)).toBe('fooBar')
  })

  it('selects an identifier that ends exactly at a non-word char', () => {
    // line 2 'fooBar' occupies offsets 26..32, '.baz' follows
    expect(state.sliceDoc(26, 32)).toBe('fooBar')
    const r = getWordRangeAt(state, 31) // last char of fooBar
    expect(state.sliceDoc(r!.from, r!.to)).toBe('fooBar')
  })

  it('returns null on whitespace between non-adjacent words', () => {
    const s = EditorState.create({ doc: 'foo + bar\n' })
    expect(getWordRangeAt(s, 5)).toBeNull() // space between '+' and 'bar'
  })

  it('returns null on a lone punctuation position', () => {
    const s = EditorState.create({ doc: 'a + + b\n' })
    expect(getWordRangeAt(s, 4)).toBeNull() // second '+'
  })

  it('handles a position at a line boundary', () => {
    const s = EditorState.create({ doc: 'foo\n' })
    // pos 3 is the newline right after 'foo' → still resolves to 'foo'
    expect(s.sliceDoc(getWordRangeAt(s, 3)!.from, getWordRangeAt(s, 3)!.to)).toBe('foo')
  })
})
