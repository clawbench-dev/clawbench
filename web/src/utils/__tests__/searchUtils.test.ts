import { describe, expect, it } from 'vitest'
import { highlightText, highlightLineSyntax, markInHighlighted, searchRawContent, BLOCK_TAGS } from '../searchUtils'

describe('highlightText', () => {
  it('escapes HTML without query', () => {
    expect(highlightText('<script>alert(1)</script>', '')).toBe(
      '&lt;script&gt;alert(1)&lt;/script&gt;',
    )
  })

  it('wraps query match in <mark> tags', () => {
    const result = highlightText('hello world', 'world')
    expect(result).toBe('hello <mark>world</mark>')
  })

  it('highlights multiple occurrences', () => {
    const result = highlightText('foo bar foo', 'foo')
    expect(result).toBe('<mark>foo</mark> bar <mark>foo</mark>')
  })

  it('escapes HTML before highlighting', () => {
    const result = highlightText('<b>test</b>', 'test')
    expect(result).toBe('&lt;b&gt;<mark>test</mark>&lt;/b&gt;')
  })

  it('handles regex special characters in query', () => {
    const result = highlightText('price: $10.00', '$10.00')
    expect(result).toBe('price: <mark>$10.00</mark>')
  })

  it('handles empty text', () => {
    expect(highlightText('', 'foo')).toBe('')
  })

  it('handles empty query by returning escaped text', () => {
    expect(highlightText('hello', '')).toBe('hello')
  })

  it('handles query not found in text', () => {
    const result = highlightText('hello world', 'xyz')
    expect(result).toBe('hello world')
  })

  it('highlights case-insensitively preserving original case', () => {
    const result = highlightText('Hello World', 'hello')
    expect(result).toBe('<mark>Hello</mark> World')
  })

  it('highlights all case variants', () => {
    const result = highlightText('Error error ERROR', 'error')
    expect(result).toBe('<mark>Error</mark> <mark>error</mark> <mark>ERROR</mark>')
  })

  it('escapes HTML entities in text', () => {
    const result = highlightText('a & b < c', '')
    expect(result).toBe('a &amp; b &lt; c')
  })
})

describe('highlightLineSyntax', () => {
  it('returns highlighted code for known language', () => {
    const result = highlightLineSyntax('const x = 1', 'typescript')
    expect(result).toContain('const')
    expect(result).toContain('1')
  })

  it('returns escaped HTML for unknown language', () => {
    const result = highlightLineSyntax('<div>test</div>', 'plaintext')
    expect(result).toContain('test')
  })

  it('strips wrapping <span class="line"> tags', () => {
    // hljs may wrap output in <span class="line">...</span>
    // The function should strip these
    const result = highlightLineSyntax('hello', 'plaintext')
    expect(result).not.toMatch(/^<span class="line">/)
  })

  it('handles empty string input', () => {
    const result = highlightLineSyntax('', 'plaintext')
    expect(result).toBe('')
  })

  it('handles special characters gracefully', () => {
    const result = highlightLineSyntax('x = "hello"', 'python')
    expect(result).toBeDefined()
    expect(typeof result).toBe('string')
  })
})

describe('markInHighlighted', () => {
  it('returns unchanged HTML when query is empty', () => {
    const html = '<span class="hljs-keyword">const</span> x = 1'
    expect(markInHighlighted(html, '')).toBe(html)
  })

  it('marks query in text segments only, not inside HTML tags', () => {
    const html = '<span class="hljs-keyword">const</span> x = 1'
    const result = markInHighlighted(html, 'const')
    expect(result).toContain('<mark>const</mark>')
    // The class attribute should not be modified
    expect(result).toContain('class="hljs-keyword"')
  })

  it('does not break HTML tags when query matches tag content', () => {
    const html = '<span class="test">hello</span>'
    const result = markInHighlighted(html, 'span')
    // 'span' appears inside <span ...> tag, should NOT be marked inside tag
    expect(result).not.toContain('<<mark>span</mark>')
  })

  it('handles multiple text segments', () => {
    const html = '<b>foo</b> and <b>bar</b>'
    const result = markInHighlighted(html, 'foo')
    expect(result).toBe('<b><mark>foo</mark></b> and <b>bar</b>')
  })

  it('handles regex special characters in query', () => {
    const html = 'price: $10.00'
    const result = markInHighlighted(html, '$10.00')
    expect(result).toContain('<mark>$10.00</mark>')
  })

  it('marks multiple occurrences across segments', () => {
    const html = '<b>x</b> x <i>x</i>'
    const result = markInHighlighted(html, 'x')
    expect(result).toBe('<b><mark>x</mark></b> <mark>x</mark> <i><mark>x</mark></i>')
  })

  it('handles query not found', () => {
    const html = '<b>hello</b> world'
    expect(markInHighlighted(html, 'xyz')).toBe(html)
  })

  it('marks case-insensitively preserving original case', () => {
    const html = '<span class="hljs-keyword">Const</span> x = 1'
    const result = markInHighlighted(html, 'const')
    expect(result).toContain('<mark>Const</mark>')
  })
})

describe('searchRawContent', () => {
  it('returns empty array for empty content', () => {
    expect(searchRawContent('test', '', 'file.txt')).toEqual([])
  })

  it('returns empty array for empty query', () => {
    expect(searchRawContent('', 'some content', 'file.txt')).toEqual([])
  })

  it('finds matching lines with line numbers', () => {
    const content = 'line one\nline two\nline three'
    const results = searchRawContent('two', content, 'test.txt')

    expect(results).toHaveLength(1)
    expect(results[0].line).toBe(2)
    expect(results[0].text).toBe('line two')
  })

  it('finds multiple matching lines', () => {
    const content = 'foo bar\nbaz\nfoo qux'
    const results = searchRawContent('foo', content, 'test.txt')

    expect(results).toHaveLength(2)
    expect(results[0].line).toBe(1)
    expect(results[1].line).toBe(3)
  })

  it('includes highlighted version with <mark> tags', () => {
    const content = 'hello world'
    const results = searchRawContent('world', content, 'test.txt')

    expect(results[0].highlighted).toContain('<mark>world</mark>')
  })

  it('handles query not found in content', () => {
    const content = 'hello world'
    const results = searchRawContent('xyz', content, 'test.txt')

    expect(results).toHaveLength(0)
  })

  it('finds matches case-insensitively', () => {
    const content = 'Hello World\nhello world\nHELLO WORLD'
    const results = searchRawContent('hello', content, 'test.txt')

    expect(results).toHaveLength(3)
    expect(results[0].line).toBe(1)
    expect(results[1].line).toBe(2)
    expect(results[2].line).toBe(3)
  })

  it('highlights case-insensitively preserving original case', () => {
    const content = 'Hello World'
    const results = searchRawContent('hello', content, 'test.txt')

    expect(results).toHaveLength(1)
    expect(results[0].highlighted).toContain('<mark>Hello</mark>')
  })

  it('handles single-line content with match', () => {
    const content = 'only line'
    const results = searchRawContent('only', content, 'test.txt')

    expect(results).toHaveLength(1)
    expect(results[0].line).toBe(1)
  })

  it('handles empty lines that do not match', () => {
    const content = 'hello\n\nworld'
    const results = searchRawContent('test', content, 'test.txt')

    expect(results).toHaveLength(0)
  })

  it('detects language from filename for syntax highlighting', () => {
    const content = 'const x = 1'
    const results = searchRawContent('x', content, 'app.ts')

    // Should have highlighted version (syntax highlighted)
    expect(results[0].highlighted).toBeDefined()
    expect(results[0].highlighted.length).toBeGreaterThan(0)
  })

  it('handles special regex characters in query', () => {
    const content = 'price: $10.00'
    const results = searchRawContent('$10.00', content, 'test.txt')

    expect(results).toHaveLength(1)
    expect(results[0].highlighted).toContain('<mark>$10.00</mark>')
  })

  it('preserves original text in text field', () => {
    const content = '  indented line  '
    const results = searchRawContent('indented', content, 'test.txt')

    expect(results[0].text).toBe('  indented line  ')
  })
})

describe('BLOCK_TAGS', () => {
  it('contains standard block-level elements', () => {
    expect(BLOCK_TAGS.has('P')).toBe(true)
    expect(BLOCK_TAGS.has('DIV')).toBe(true)
    expect(BLOCK_TAGS.has('H1')).toBe(true)
    expect(BLOCK_TAGS.has('BLOCKQUOTE')).toBe(true)
    expect(BLOCK_TAGS.has('PRE')).toBe(true)
    expect(BLOCK_TAGS.has('LI')).toBe(true)
  })

  it('does not contain inline elements', () => {
    expect(BLOCK_TAGS.has('SPAN')).toBe(false)
    expect(BLOCK_TAGS.has('A')).toBe(false)
    expect(BLOCK_TAGS.has('STRONG')).toBe(false)
    expect(BLOCK_TAGS.has('EM')).toBe(false)
  })

  it('contains table cell elements', () => {
    expect(BLOCK_TAGS.has('TD')).toBe(true)
    expect(BLOCK_TAGS.has('TH')).toBe(true)
  })

  it('is a Set instance', () => {
    expect(BLOCK_TAGS).toBeInstanceOf(Set)
  })
})
