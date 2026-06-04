import { describe, expect, it } from 'vitest'
import {
  splitHighlightedHtml,
  buildCodeLinesFromHighlighted,
  buildCodeLinesFromEscaped,
  renderCodeLines,
} from '@/utils/codeRender.ts'

describe('splitHighlightedHtml', () => {
  it('returns single-element array for empty string', () => {
    expect(splitHighlightedHtml('')).toEqual([''])
  })

  it('returns single line unchanged', () => {
    expect(splitHighlightedHtml('hello')).toEqual(['hello'])
  })

  it('splits plain text by newlines', () => {
    expect(splitHighlightedHtml('line1\nline2\nline3')).toEqual(['line1', 'line2', 'line3'])
  })

  it('handles span crossing line boundary', () => {
    const html = '<span class="hl">line1\nline2</span>'
    const result = splitHighlightedHtml(html)
    expect(result).toHaveLength(2)
    expect(result[0]).toContain('<span class="hl">line1</span>')
    expect(result[1]).toContain('<span class="hl">line2</span>')
  })

  it('handles nested spans crossing lines', () => {
    const html = '<span class="a"><span class="b">line1\nline2</span></span>'
    const result = splitHighlightedHtml(html)
    expect(result).toHaveLength(2)
    // First line should close both spans
    expect(result[0]).toMatch(/<\/span>.*<\/span>$/)
    // Second line should reopen both spans
    expect(result[1]).toMatch(/^<span class="a">/)
  })

  it('handles multiple spans on same line', () => {
    const html = '<span class="a">hello</span> <span class="b">world</span>'
    const result = splitHighlightedHtml(html)
    expect(result).toHaveLength(1)
    expect(result[0]).toBe(html)
  })

  it('handles span without closing > gracefully', () => {
    const html = '<span class="a">text'
    const result = splitHighlightedHtml(html)
    expect(result).toHaveLength(1)
  })
})

describe('buildCodeLinesFromHighlighted', () => {
  it('builds code-line divs with line numbers', () => {
    const html = buildCodeLinesFromHighlighted('line1\nline2', true)
    expect(html).toContain('data-line="1"')
    expect(html).toContain('data-line="2"')
    expect(html).toContain('class="line-num"')
    expect(html).toContain('class="code-text"')
  })

  it('builds code-line divs without line numbers', () => {
    const html = buildCodeLinesFromHighlighted('line1\nline2', false)
    expect(html).not.toContain('class="line-num"')
    expect(html).toContain('class="code-text"')
  })

  it('handles single line', () => {
    const html = buildCodeLinesFromHighlighted('hello', true)
    expect(html).toContain('data-line="1"')
  })
})

describe('buildCodeLinesFromEscaped', () => {
  it('builds code-line divs from escaped HTML', () => {
    const html = buildCodeLinesFromEscaped('line1\nline2', true)
    expect(html).toContain('data-line="1"')
    expect(html).toContain('data-line="2"')
  })

  it('builds without line numbers', () => {
    const html = buildCodeLinesFromEscaped('hello', false)
    expect(html).not.toContain('class="line-num"')
  })
})

describe('renderCodeLines', () => {
  it('renders code with syntax highlighting', () => {
    const html = renderCodeLines('const x = 1', 'typescript', true)
    expect(html).toContain('data-line="1"')
    expect(html).toContain('class="code-text"')
  })

  it('renders code without line numbers', () => {
    const html = renderCodeLines('const x = 1', 'typescript', false)
    expect(html).not.toContain('class="line-num"')
  })

  it('renders multiple lines', () => {
    const html = renderCodeLines('line1\nline2\nline3', 'plaintext', true)
    expect(html).toContain('data-line="1"')
    expect(html).toContain('data-line="2"')
    expect(html).toContain('data-line="3"')
  })

  it('renders flash ranges with flash class', () => {
    const flashMap = new Map<number, Array<{ start: number; end: number }>>()
    flashMap.set(1, [{ start: 0, end: 5 }])
    const html = renderCodeLines('hello world', 'plaintext', true, flashMap, 'char-flash-add')
    expect(html).toContain('char-flash-add')
    expect(html).toContain('data-line="1"')
  })

  it('handles flash ranges that extend beyond line length', () => {
    const flashMap = new Map<number, Array<{ start: number; end: number }>>()
    flashMap.set(1, [{ start: 0, end: 100 }])
    const html = renderCodeLines('short', 'plaintext', true, flashMap, 'char-flash-add')
    expect(html).toContain('char-flash-add')
  })

  it('handles flash ranges with gap before range', () => {
    const flashMap = new Map<number, Array<{ start: number; end: number }>>()
    flashMap.set(1, [{ start: 6, end: 11 }])
    const html = renderCodeLines('hello world', 'plaintext', true, flashMap, 'char-flash-delete')
    expect(html).toContain('char-flash-delete')
  })

  it('handles multiple flash ranges on same line', () => {
    const flashMap = new Map<number, Array<{ start: number; end: number }>>()
    flashMap.set(1, [{ start: 0, end: 3 }, { start: 6, end: 9 }])
    const html = renderCodeLines('hello world', 'plaintext', true, flashMap, 'char-flash-add')
    const matches = html.match(/char-flash-add/g)
    expect(matches).toHaveLength(2)
  })

  it('falls back to escapeHtml for unknown language', () => {
    const html = renderCodeLines('const x = 1', 'totally_unknown_lang_xyz', true)
    expect(html).toContain('data-line="1"')
  })
})
