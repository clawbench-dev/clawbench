import { describe, expect, it } from 'vitest'
import { toDisplayEntry, escapeHtml, highlightName } from './fileSearchMark'

describe('fileSearchMark — toDisplayEntry', () => {
  it('adapts a file result with its project-relative path and parent dir', () => {
    const r = { name: 'main.go', path: 'cmd/main.go', type: 'file' as const, matchedIndices: [0, 1, 2, 3] }
    expect(toDisplayEntry(r)).toEqual({
      name: 'main.go',
      type: 'file',
      path: 'cmd/main.go',
      parentDir: 'cmd',
      matchedIndices: [0, 1, 2, 3],
    })
  })

  it('normalizes a root-level result to an empty parent dir', () => {
    const r = { name: 'main.go', path: 'main.go', type: 'file' as const, matchedIndices: [] }
    expect(toDisplayEntry(r).parentDir).toBe('')
  })

  it('maps image-tagged results to the file display type (thumb by ext)', () => {
    const r = { name: 'a.png', path: 'assets/a.png', type: 'image' as const, matchedIndices: [0] }
    expect(toDisplayEntry(r).type).toBe('file')
  })

  it('keeps dir results as dir', () => {
    const r = { name: 'cmd', path: 'cmd', type: 'dir' as const, matchedIndices: [0, 1, 2] }
    expect(toDisplayEntry(r).type).toBe('dir')
    expect(toDisplayEntry(r).parentDir).toBe('')
  })
})

describe('fileSearchMark — escapeHtml / highlightName', () => {
  it('escapes angle brackets and ampersands in filenames', () => {
    expect(escapeHtml('<a&b>"c"')).toBe('&lt;a&amp;b&gt;&quot;c&quot;')
  })

  it('returns plain escaped text when no match indices are given', () => {
    expect(highlightName('a<b.go', undefined)).toBe('a&lt;b.go')
    expect(highlightName('a<b.go', [])).toBe('a&lt;b.go')
  })

  it('wraps matched characters in <mark> while escaping everything else', () => {
    const out = highlightName('<main>.go', [1, 2, 3, 4])
    // '<' at index 0 is escaped but not marked; 'main' chars marked
    expect(out).toBe('&lt;<mark>m</mark><mark>a</mark><mark>i</mark><mark>n</mark>&gt;.go')
  })

  it('never leaves unescaped HTML even for matched special characters', () => {
    const out = highlightName('<&', [0])
    // Both must be escaped; the match only marks, never re-emits raw markup
    expect(out).toBe('<mark>&lt;</mark>&amp;')
    expect(out).not.toContain('<mark><')
  })
})
