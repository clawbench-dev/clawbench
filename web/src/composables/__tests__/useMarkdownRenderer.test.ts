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

// --- renderMarkdown ---

describe('renderMarkdown', () => {
  beforeEach(() => {
    mockMarkedParse.mockClear()
    mockKatexRenderToString.mockClear()
    mockDOMPurifySanitize.mockClear()
  })

  it('calls marked.parse with trimmed content', () => {
    mockMarkedParse.mockReturnValue('<p>hello</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('  hello  ')

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
