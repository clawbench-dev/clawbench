import { describe, expect, it, vi, beforeEach } from 'vitest'
import { renderKatexInString, renderMarkdown, renderMarkdownHtml, renderMermaidInElement, useMarkdownRenderer, INLINE_MATH_RE } from '@/composables/useMarkdownRenderer'

// Mock globals
const mockMarkedParse = vi.fn((s: string) => `<p>${s}</p>`)
const mockKatexRenderToString = vi.fn((math: string, opts: any) => {
  if (math.includes('ERROR')) throw new Error('KaTeX error')
  return `<span class="katex">${opts.displayMode ? 'display' : 'inline'}:${math}</span>`
})
const mockDOMPurifySanitize = vi.fn((html: string) => html)
const mermaidRender = vi.fn()

vi.mock('@/utils/globals', () => ({
  marked: { parse: (...args: any[]) => mockMarkedParse(...args) },
  katex: { renderToString: (...args: any[]) => mockKatexRenderToString(...args) },
  DOMPurify: { sanitize: (...args: any[]) => mockDOMPurifySanitize(...args) },
  highlightCode: (code: string, _lang: string) => code,
}))

vi.mock('@/utils/mermaid', () => ({
  renderMermaidInElement: vi.fn(async (el: HTMLElement, prefix = 'mermaid', specificBlocks?: NodeList) => {
    const blocks = specificBlocks || el.querySelectorAll('pre.mermaid:not([data-rendered])')
    for (const block of Array.from(blocks)) {
      (block as HTMLElement).setAttribute('data-rendered', '1')
      const container = document.createElement('div')
      container.className = 'mermaid'
      container.id = `${prefix}-0`
      try {
        await mermaidRender((block as HTMLElement).textContent, container)
      } catch {
        container.innerHTML = `<pre>Mermaid Error</pre>`
      }
      ;(block as Element).replaceWith(container)
    }
  }),
  initMermaid: vi.fn(),
  reRenderMermaid: vi.fn(),
}))

vi.mock('@/utils/html', () => ({
  escapeHtml: (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;'),
}))

vi.mock('@/utils/tableRowExpand', () => ({
  injectTableRowAttrs: (html: string) => html,
}))

vi.mock('@/composables/useCodeBlockHeader', () => ({
  annotateCodeBlockHeaders: (html: string) => html,
  annotateTableBlockHeaders: (html: string) => html,
}))

vi.mock('@/utils/chatRenderUtils', () => ({
  rewriteImageUrls: (html: string) => html,
  convertAudioLinks: (html: string) => html,
  convertVideoLinks: (html: string) => html,
  getThumbWidth: () => 800,
  parseAskQuestionContent: vi.fn(),
}))

vi.mock('@/composables/useFilePathAnnotation', () => ({
  annotateFilePaths: (html: string) => ({ html, detectedPaths: [] }),
  useFilePathAnnotation: () => ({ verifyFilePaths: vi.fn(), openFilePath: vi.fn() }),
}))

vi.mock('@/composables/useCommitHashAnnotation', () => ({
  annotateCommitHashes: (html: string) => ({ html, detectedSHAs: [] }),
  useCommitHashAnnotation: () => ({ verifyCommitHashes: vi.fn() }),
}))

vi.mock('@/composables/useWorktreeAnnotation', () => ({
  annotateWorktreePaths: (html: string) => ({ html }),
  useWorktreeAnnotation: () => ({}),
}))

vi.mock('@/composables/useLocalhostAnnotation', () => ({
  annotateLocalhostUrls: (html: string) => html,
  useLocalhostAnnotation: () => ({}),
}))

vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/test', homeDir: '/home/test' } },
}))

// --- renderKatexInString ---

describe('renderKatexInString', () => {
  beforeEach(() => {
    mockKatexRenderToString.mockClear()
  })

  it('INLINE_MATH_RE does not use regex lookbehind (Safari < 16.4 compatibility)', () => {
    // Lookbehind (?<= / (?<!) is only supported from Safari/iPadOS 16.4.
    // A lookbehind regex literal throws SyntaxError at parse time on older
    // Safari, killing the entire bundle → white screen.
    expect(INLINE_MATH_RE.source).not.toMatch(/\(\?<[=!]/)
  })

  it('renders display math with $$ delimiters', () => {
    const input = '<p>$$x^2 + y^2$$</p>'
    const result = renderKatexInString(input)
    expect(result).toContain('display:x^2 + y^2')
    expect(mockKatexRenderToString).toHaveBeenCalledWith('x^2 + y^2', expect.objectContaining({ displayMode: true }))
  })

  it('renders display math with \\[...\\] delimiters', () => {
    const input = '<p>\\[x^2\\]</p>'
    const result = renderKatexInString(input)
    expect(result).toContain('display:x^2')
  })

  it('renders inline math with $ delimiters', () => {
    const input = '<p>the $x^2$ equation</p>'
    const result = renderKatexInString(input)
    expect(result).toContain('inline:x^2')
    expect(mockKatexRenderToString).toHaveBeenCalledWith('x^2', expect.objectContaining({ displayMode: false }))
  })

  it('renders inline math with \\(...\\) delimiters', () => {
    const input = '<p>the \\(x^2\\) equation</p>'
    const result = renderKatexInString(input)
    expect(result).toContain('inline:x^2')
  })

  it('returns input unchanged when no math delimiters', () => {
    const input = '<p>no math here</p>'
    const result = renderKatexInString(input)
    expect(result).toBe(input)
  })

  it('returns input unchanged when empty string', () => {
    expect(renderKatexInString('')).toBe('')
  })

  it('handles KaTeX errors gracefully in display math', () => {
    const input = '<p>$$ERROR_MATH$$</p>'
    const result = renderKatexInString(input)
    expect(result).toBeDefined()
  })

  it('handles KaTeX errors gracefully in inline math', () => {
    const input = '<p>the $ERROR$ equation</p>'
    const result = renderKatexInString(input)
    expect(result).toBeDefined()
  })

  it('trims whitespace in math expressions', () => {
    const input = '<p>$$  x^2  $$</p>'
    renderKatexInString(input)
    expect(mockKatexRenderToString).toHaveBeenCalledWith('x^2', expect.any(Object))
  })

  it('does not match $$ inside display math', () => {
    const input = '<p>$$x^2 + y^2$$</p>'
    renderKatexInString(input)
    expect(mockKatexRenderToString).toHaveBeenCalledWith('x^2 + y^2', expect.objectContaining({ displayMode: true }))
  })

  it('does not match prices like $5 and $10', () => {
    const input = '<p>花费 $5 和 $10，共 $15。</p>'
    const result = renderKatexInString(input)
    expect(result).toBe(input)
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
  })

  it('does not match $5 at start of text (digit after $)', () => {
    const input = '<p>$5 is the price</p>'
    const result = renderKatexInString(input)
    expect(result).toBe(input)
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
  })

  it('does not match when digit precedes $ (like 5$)', () => {
    const input = '<p>total 5$</p>'
    const result = renderKatexInString(input)
    expect(result).toBe(input)
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
  })

  it('handles escaped \\$ as literal dollar sign', () => {
    const input = '<p>cost \\$5 and \\$10</p>'
    const result = renderKatexInString(input)
    expect(result).toContain('$5')
    expect(result).toContain('$10')
    expect(result).not.toContain('\\$')
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
  })

  it('does not match math inside <code> blocks', () => {
    const input = '<p>use <code>$cost</code> and <code>$$total$$</code></p>'
    const result = renderKatexInString(input)
    expect(result).toContain('<code>$cost</code>')
    expect(result).toContain('<code>$$total$$</code>')
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
  })

  it('still matches math outside <code> blocks', () => {
    const input = '<p>formula $x^2$ inside, code <code>$cost</code></p>'
    const result = renderKatexInString(input)
    expect(result).toContain('inline:x^2')
    expect(result).toContain('<code>$cost</code>')
  })

  it('protects multi-line <code> blocks from KaTeX', () => {
    const input = '<p>before</p>\n<code>$$formula$$\n$inline$</code>\n<p>after</p>'
    const result = renderKatexInString(input)
    expect(result).toContain('<code>$$formula$$\n$inline$</code>')
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
  })
})

// --- Math block protection (extractMathBlocks before marked.parse) ---

describe('Math block protection (issue #384)', () => {
  beforeEach(() => {
    mockMarkedParse.mockClear()
    mockKatexRenderToString.mockClear()
    mockDOMPurifySanitize.mockImplementation((s: string) => s)
  })

  it('protects LaTeX _ subscripts from marked emphasis parsing', () => {
    // Issue #384: a^{0}_{i} + b^{0}_{j} → marked would produce <em> without protection
    const input = '$a^{0}_{i} + b^{0}_{j}$'
    const result = renderMarkdown(input)

    // Math content should be passed to KaTeX intact (with _ not mangled)
    expect(mockKatexRenderToString).toHaveBeenCalledWith(
      'a^{0}_{i} + b^{0}_{j}',
      expect.objectContaining({ displayMode: false })
    )
    // No <em> tags should appear in output
    expect(result.html).not.toContain('<em>')
    expect(result.html).toContain('inline:a^{0}_{i} + b^{0}_{j}')
  })

  it('protects display math with mixed super/subscripts', () => {
    const input = '$$\\mathcal{B}=\\{(x_{i},\\tau^{0}_{i},y^{0}_{i})\\}_{i=1}^{n}$$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith(
      expect.stringContaining('\\tau^{0}_{i}'),
      expect.objectContaining({ displayMode: true })
    )
    expect(result.html).not.toContain('<em>')
  })

  it('preserves displayMode for display math vs inline math', () => {
    const input = 'Display: $$x^2$$ and inline $y^2$'
    renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('x^2', expect.objectContaining({ displayMode: true }))
    expect(mockKatexRenderToString).toHaveBeenCalledWith('y^2', expect.objectContaining({ displayMode: false }))
  })

  it('handles \\[...\\] display math with subscripts', () => {
    const input = '\\[a^{0}_{i}\\]'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a^{0}_{i}', expect.objectContaining({ displayMode: true }))
    expect(result.html).not.toContain('<em>')
  })

  it('handles \\(...\\) inline math with subscripts', () => {
    const input = 'text \\(a^{0}_{i}\\) more'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a^{0}_{i}', expect.objectContaining({ displayMode: false }))
    expect(result.html).not.toContain('<em>')
  })

  it('extracts math before marked.parse so marked never sees $ delimiters', () => {
    const input = '$x^{2}_{i}$'
    renderMarkdown(input)

    // marked.parse should receive the placeholder, not the raw $ delimiters
    const markedInput = mockMarkedParse.mock.calls[0][0] as string
    expect(markedInput).not.toContain('$x^{2}_{i}$')
    expect(markedInput).toMatch(/\x00MATH/)
  })

  it('does not extract prices as math blocks', () => {
    const input = 'cost $5 and $10'
    renderMarkdown(input)

    // No math should be extracted
    const markedInput = mockMarkedParse.mock.calls[0][0] as string
    expect(markedInput).toContain('$5')
    expect(markedInput).toContain('$10')
    expect(markedInput).not.toMatch(/\x00MATH/)
  })

  it('handles mixed math and non-math content correctly', () => {
    const input = 'Before $a_{i}$ middle $b_{j}$ after'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a_{i}', expect.objectContaining({ displayMode: false }))
    expect(mockKatexRenderToString).toHaveBeenCalledWith('b_{j}', expect.objectContaining({ displayMode: false }))
    expect(result.html).not.toContain('<em>')
  })

  it('protects * from marked strong/emphasis in display math', () => {
    // * is also used in LaTeX (e.g., \*, multiplication), protect from marked
    const input = '$$a * b + c_{i}$$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a * b + c_{i}', expect.objectContaining({ displayMode: true }))
    expect(result.html).not.toContain('<strong>')
    expect(result.html).not.toContain('<em>')
  })

  it('does not extract math inside backtick code spans', () => {
    // Code spans must be protected before math extraction.
    // Verify that marked.parse receives input without $ delimiters inside code spans.
    const input = 'use `$a_{i}$` and `$b_{j}$` in code'
    renderMarkdown(input)

    const markedInput = mockMarkedParse.mock.calls[0][0] as string
    // The $ delimiters inside backtick code spans should NOT be extracted as math
    // (they remain as backtick-wrapped code in the protected markdown)
    expect(markedInput).toContain('`')
    expect(markedInput).not.toMatch(/\x00MATH/)
  })

  it('does not extract math inside fenced code blocks', () => {
    const input = '```\n$a_{i} + b^{0}_{j}$\n```'
    renderMarkdown(input)

    const markedInput = mockMarkedParse.mock.calls[0][0] as string
    expect(markedInput).not.toMatch(/\x00MATH/)
  })

  it('extracts math outside code spans while preserving code content', () => {
    const input = 'formula $x^{2}_{i}$ and code `$cost`'
    const result = renderMarkdown(input)

    // Math outside code should be rendered
    expect(mockKatexRenderToString).toHaveBeenCalledWith('x^{2}_{i}', expect.objectContaining({ displayMode: false }))
    expect(result.html).not.toContain('<em>')
  })

  it('strips math placeholders when skipKatex=true (no NUL bytes in output)', () => {
    const input = 'formula $a^{0}_{i}$ and display $$x^2$$'
    const result = renderMarkdown(input, { skipKatex: true })

    // No KaTeX rendering
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
    // No NUL bytes or garbage "MATHD0"/"MATHI0" text in output
    expect(result.html).not.toContain('\x00')
    expect(result.html).not.toContain('MATHD')
    expect(result.html).not.toContain('MATHI')
    // Should contain escaped raw math delimiters
    expect(result.html).toContain('a^{0}_{i}')
    expect(result.html).toContain('x^2')
  })

  it('no garbage text when skipKatex=true and no math present', () => {
    const input = 'plain text without math'
    const result = renderMarkdown(input, { skipKatex: true })

    expect(result.html).not.toContain('\x00')
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
  })

  // --- Additional edge-case scenarios ---

  it('handles adjacent inline math blocks with space separator', () => {
    // Adjacent $a_{i}$$b_{j}$ without space: the second $ after } is preceded by $,
    // so the inline math regex (^|[^$\d\\]) won't match it as a new math start.
    // This is correct behavior — users should add a space between adjacent inline formulas.
    const input = 'text $a_{i}$ $b_{j}$ more'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a_{i}', expect.objectContaining({ displayMode: false }))
    expect(mockKatexRenderToString).toHaveBeenCalledWith('b_{j}', expect.objectContaining({ displayMode: false }))
    expect(result.html).not.toContain('<em>')
  })

  it('adjacent inline math without space only matches first block', () => {
    // $a_{i}$$b_{j}$ — the }$ at end of first block means the next $ is preceded by $,
    // so the regex excludes it. Only the first block is matched.
    const input = '$a_{i}$$b_{j}$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a_{i}', expect.objectContaining({ displayMode: false }))
    // The second math block is not extracted (preceding $ excluded by regex)
    expect(mockKatexRenderToString).not.toHaveBeenCalledWith('b_{j}', expect.any(Object))
    expect(result.html).not.toContain('<em>')
  })

  it('handles ambiguous $x$$y^2$ — only first inline $x$ is extracted', () => {
    // $x$$y^2$ — display $$ regex looks for $$..$$ but there's no closing $$.
    // The inline regex matches $x$ (first pair). The remaining $y^2$ is NOT
    // extracted because its opening $ is preceded by $ (excluded by the regex).
    const input = '$x$$y^2$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('x', expect.objectContaining({ displayMode: false }))
    expect(result.html).not.toContain('<em>')
  })

  it('handles whitespace-only math blocks gracefully', () => {
    // $$ $$ and $ $ — after trim, math is empty string; KaTeX should handle or error gracefully
    const input = 'display $$ $$ and inline $ $'
    const result = renderMarkdown(input)

    // Should not crash; empty math is passed to KaTeX which handles it
    expect(result).toBeDefined()
    expect(result.html).not.toContain('<em>')
  })

  it('single subscript without superscript is unaffected', () => {
    // $x_{i}$ — a single _ is not paired by marked, so it would work even without protection.
    // But with protection, it should still work correctly.
    const input = '$x_{i}$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('x_{i}', expect.objectContaining({ displayMode: false }))
    expect(result.html).not.toContain('<em>')
  })

  it('single subscript followed by letter is unaffected by marked (word-internal _)', () => {
    // $x_{i}+y_{j}$ — _ followed by a letter is word-internal, marked ignores it.
    // Verify it still works correctly with protection.
    const input = '$x_{i}+y_{j}$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('x_{i}+y_{j}', expect.objectContaining({ displayMode: false }))
    expect(result.html).not.toContain('<em>')
  })

  it('display math with both * and _ together', () => {
    // Combined emphasis attack: * for strong, _ for em, in same formula
    const input = '$$a * b_{i} + c^{0}_{j}$$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a * b_{i} + c^{0}_{j}', expect.objectContaining({ displayMode: true }))
    expect(result.html).not.toContain('<em>')
    expect(result.html).not.toContain('<strong>')
  })

  it('does not extract math inside tilde fenced code blocks', () => {
    const input = '~~~\n$x_{i} + b^{0}_{j}$\n~~~'
    renderMarkdown(input)

    const markedInput = mockMarkedParse.mock.calls[0][0] as string
    expect(markedInput).not.toMatch(/\x00MATH/)
  })

  it('does not extract math inside fenced code blocks with info string', () => {
    const input = '```python\n$x_{i} + b^{0}_{j}$\n```'
    renderMarkdown(input)

    const markedInput = mockMarkedParse.mock.calls[0][0] as string
    expect(markedInput).not.toMatch(/\x00MATH/)
  })

  it('handles escaped \\$ before math block (not treated as math start)', () => {
    // \$ before $ means the first $ is escaped, so the math block starts at the second $
    const input = 'cost \\$5 and formula $x_{i}$'
    renderMarkdown(input)

    // Only $x_{i}$ should be extracted as math; \$5 is a literal dollar + price
    expect(mockKatexRenderToString).toHaveBeenCalledWith('x_{i}', expect.objectContaining({ displayMode: false }))
    // Price $5 should not be extracted (digit after $)
    const mathCalls = mockKatexRenderToString.mock.calls.map(c => c[0])
    expect(mathCalls).not.toContain('5')
  })

  it('stripMathPlaceholders restores $$ delimiters for display math when skipKatex=true', () => {
    const input = '$$a^{0}_{i}$$'
    const result = renderMarkdown(input, { skipKatex: true })

    expect(mockKatexRenderToString).not.toHaveBeenCalled()
    expect(result.html).not.toContain('\x00')
    // Display math should be wrapped in $$ delimiters in the stripped output
    expect(result.html).toContain('$$')
    expect(result.html).toContain('a^{0}_{i}')
  })

  it('skipKatex=true shows raw formula source for complex subscript expression', () => {
    const input = 'Result: $\\tau^{0}_{i} + y^{0}_{i}$'
    const result = renderMarkdown(input, { skipKatex: true })

    expect(result.html).not.toContain('\x00')
    expect(result.html).not.toContain('MATHD')
    expect(result.html).not.toContain('MATHI')
    expect(result.html).toContain('\\tau^{0}_{i}')
  })

  it('handles multiple display math blocks in same content', () => {
    const input = '$$a^{0}_{i}$$ text $$b^{0}_{j}$$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a^{0}_{i}', expect.objectContaining({ displayMode: true }))
    expect(mockKatexRenderToString).toHaveBeenCalledWith('b^{0}_{j}', expect.objectContaining({ displayMode: true }))
    expect(result.html).not.toContain('<em>')
  })

  it('handles mixed display and inline math with subscripts', () => {
    const input = 'Inline $a^{0}_{i}$ and display $$b^{0}_{j}$$ and another $c_{k}$'
    const result = renderMarkdown(input)

    expect(mockKatexRenderToString).toHaveBeenCalledWith('a^{0}_{i}', expect.objectContaining({ displayMode: false }))
    expect(mockKatexRenderToString).toHaveBeenCalledWith('b^{0}_{j}', expect.objectContaining({ displayMode: true }))
    expect(mockKatexRenderToString).toHaveBeenCalledWith('c_{k}', expect.objectContaining({ displayMode: false }))
    expect(result.html).not.toContain('<em>')
  })

  it('extractCodeAndMath does not modify content without math or code', () => {
    const input = 'plain text with _emphasis_ and *strong*'
    renderMarkdown(input)

    const markedInput = mockMarkedParse.mock.calls[0][0] as string
    // No extraction happened; marked receives the original content
    expect(markedInput).toBe(input)
    expect(markedInput).toContain('_emphasis_')
    expect(markedInput).toContain('*strong*')
  })

  it('code span with display math $$ is protected from extraction', () => {
    const input = 'use `$$x^{2}_{i}$$` in template'
    renderMarkdown(input)

    const markedInput = mockMarkedParse.mock.calls[0][0] as string
    expect(markedInput).not.toMatch(/\x00MATH/)
  })
})

// --- renderMarkdown ---

describe('renderMarkdown', () => {
  beforeEach(() => {
    mockMarkedParse.mockClear()
    mockKatexRenderToString.mockClear()
    mockDOMPurifySanitize.mockClear()
  })

  it('calls marked.parse with trimmed content (no math/code extraction)', () => {
    mockMarkedParse.mockReturnValue('<p>hello</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('  hello  ')

    // With no math/code delimiters, extractCodeAndMath passes content through unchanged
    expect(mockMarkedParse).toHaveBeenCalledWith('hello')
  })

  it('handles empty content', () => {
    mockMarkedParse.mockReturnValue('')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    const result = renderMarkdown('')
    expect(result).toBeDefined()
  })

  it('handles null/undefined content gracefully', () => {
    mockMarkedParse.mockReturnValue('')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    const result = renderMarkdown(null as any)
    expect(result).toBeDefined()
  })

  it('wraps tables by default', () => {
    mockMarkedParse.mockReturnValue('<table><tr><td>data</td></tr></table>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    const result = renderMarkdown('table content')
    expect(result.html).toContain('table-wrap')
    expect(result.html).toContain('<table')
    expect(result.html).toContain('</table></div>')
  })

  it('skips table wrapping when wrapTables=false', () => {
    mockMarkedParse.mockReturnValue('<table><tr><td>data</td></tr></table>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    const result = renderMarkdown('table content', { wrapTables: false })
    expect(result.html).not.toContain('table-wrap')
  })

  it('calls fixImagePaths when provided', () => {
    mockMarkedParse.mockReturnValue('<img src="test.png">')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)
    const fixFn = vi.fn((html: string) => html)

    renderMarkdown('img', { fixImagePaths: fixFn })
    expect(fixFn).toHaveBeenCalled()
  })

  it('applies DOMPurify by default', () => {
    mockMarkedParse.mockReturnValue('<p>content</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content')
    expect(mockDOMPurifySanitize).toHaveBeenCalled()
  })

  it('skips DOMPurify when sanitize=false', () => {
    mockMarkedParse.mockReturnValue('<p>content</p>')

    renderMarkdown('content', { sanitize: false })
    expect(mockDOMPurifySanitize).not.toHaveBeenCalled()
  })

  it('passes ADD_TAGS and ADD_ATTR to DOMPurify', () => {
    mockMarkedParse.mockReturnValue('<p>content</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content')
    expect(mockDOMPurifySanitize).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ ADD_TAGS: expect.arrayContaining(['math', 'button']), ADD_ATTR: expect.arrayContaining(['data-action', 'aria-label', 'title']), ALLOWED_URI_REGEXP: expect.any(RegExp) }))
    const callArgs = mockDOMPurifySanitize.mock.calls[0][1]
    expect(callArgs.ALLOWED_URI_REGEXP.test('file:///Users/yuqing/foo.go')).toBe(true)
  })

  it('renders KaTeX before sanitizing when skipEnhancements=false', () => {
    mockMarkedParse.mockReturnValue('<p>$$x^2$$</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content')
    expect(mockKatexRenderToString).toHaveBeenCalled()
    expect(mockDOMPurifySanitize).toHaveBeenCalled()
  })

  it('renders KaTeX when skipEnhancements=true but skipKatex not set', () => {
    mockMarkedParse.mockReturnValue('<p>$$x^2$$</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content', { skipEnhancements: true })
    expect(mockKatexRenderToString).toHaveBeenCalled()
  })

  it('renders KaTeX when skipEnhancements=true and skipKatex=false (file preview)', () => {
    mockMarkedParse.mockReturnValue('<p>$$x^2$$</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content', { skipEnhancements: true, skipKatex: false })
    expect(mockKatexRenderToString).toHaveBeenCalled()
  })

  it('skips KaTeX when skipKatex=true regardless of skipEnhancements', () => {
    mockMarkedParse.mockReturnValue('<p>$$x^2$$</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content', { skipKatex: true })
    expect(mockKatexRenderToString).not.toHaveBeenCalled()
  })

  it('renders KaTeX by default (skipKatex not set)', () => {
    mockMarkedParse.mockReturnValue('<p>$$x^2$$</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content')
    expect(mockKatexRenderToString).toHaveBeenCalled()
  })

  it('returns RenderResult with html, detectedPaths, detectedSHAs', () => {
    mockMarkedParse.mockReturnValue('<p>content</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    const result = renderMarkdown('content')
    expect(result).toHaveProperty('html')
    expect(result).toHaveProperty('detectedPaths')
    expect(result).toHaveProperty('detectedSHAs')
  })
})

// --- renderMarkdownHtml ---

describe('renderMarkdownHtml', () => {
  beforeEach(() => {
    mockMarkedParse.mockClear()
    mockDOMPurifySanitize.mockImplementation((s: string) => s)
  })

  it('returns html string only', () => {
    mockMarkedParse.mockReturnValue('<p>test</p>')
    const result = renderMarkdownHtml('test')
    expect(typeof result).toBe('string')
  })
})

// --- renderMermaidInElement ---

describe('renderMermaidInElement', () => {
  beforeEach(() => {
    vi.mocked(mermaidRender).mockReset()
    mermaidRender.mockResolvedValue({ svg: '<svg>diagram</svg>' })
  })

  it('renders mermaid blocks and replaces with SVG', async () => {
    const el = document.createElement('div')
    const pre = document.createElement('pre')
    pre.className = 'mermaid'
    pre.textContent = 'graph TD; A-->B'
    el.appendChild(pre)

    await renderMermaidInElement(el)

    expect(el.querySelector('pre.mermaid')).toBeNull()
    expect(el.querySelector('div.mermaid')).toBeTruthy()
  })

  it('does nothing when no mermaid blocks', async () => {
    const el = document.createElement('div')
    el.innerHTML = '<p>no mermaid here</p>'

    await renderMermaidInElement(el)

    expect(mermaidRender).not.toHaveBeenCalled()
  })

  it('skips already-rendered blocks (data-rendered)', async () => {
    const el = document.createElement('div')
    const pre = document.createElement('pre')
    pre.className = 'mermaid'
    pre.setAttribute('data-rendered', '1')
    pre.textContent = 'graph TD; A-->B'
    el.appendChild(pre)

    await renderMermaidInElement(el)

    expect(mermaidRender).not.toHaveBeenCalled()
  })

  it('handles mermaid render error gracefully', async () => {
    mermaidRender.mockRejectedValue(new Error('mermaid syntax error'))

    const el = document.createElement('div')
    const pre = document.createElement('pre')
    pre.className = 'mermaid'
    pre.textContent = 'invalid mermaid'
    el.appendChild(pre)

    await renderMermaidInElement(el)

    expect(el.querySelector('pre.mermaid')).toBeNull()
    expect(el.querySelector('div.mermaid')).toBeTruthy()
  })

  it('supports specificBlocks parameter', async () => {
    const el = document.createElement('div')
    const pre = document.createElement('pre')
    pre.className = 'mermaid'
    pre.textContent = 'graph TD; A-->B'
    el.appendChild(pre)

    const nodeList = el.querySelectorAll('pre.mermaid')
    await renderMermaidInElement(el, 'mermaid', nodeList)

    expect(mermaidRender).toHaveBeenCalled()
  })
})

// --- useMarkdownRenderer composable ---

describe('useMarkdownRenderer', () => {
  beforeEach(() => {
    mockMarkedParse.mockClear()
    mockDOMPurifySanitize.mockImplementation((s: string) => s)
  })

  it('exposes renderMarkdown, renderMarkdownHtml and renderMermaidInElement', () => {
    const { renderMarkdown: rm, renderMarkdownHtml: rmh, renderMermaidInElement: rme } = useMarkdownRenderer()
    expect(typeof rm).toBe('function')
    expect(typeof rmh).toBe('function')
    expect(typeof rme).toBe('function')
  })

  it('renderMarkdown works through composable', () => {
    mockMarkedParse.mockReturnValue('<p>test</p>')
    const { renderMarkdown: rm } = useMarkdownRenderer()
    const result = rm('test')
    expect(result).toBeDefined()
  })
})
