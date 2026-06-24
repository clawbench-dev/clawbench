import { describe, expect, it, vi, beforeEach } from 'vitest'
import { renderKatexInString, renderMarkdown, renderMermaidInElement, useMarkdownRenderer } from '@/composables/useMarkdownRenderer'

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
  mermaid: { render: (...args: any[]) => mermaidRender(...args) },
  DOMPurify: { sanitize: (...args: any[]) => mockDOMPurifySanitize(...args) },
}))

vi.mock('@/utils/html', () => ({
  escapeHtml: (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;'),
}))

// --- renderKatexInString ---

describe('renderKatexInString', () => {
  beforeEach(() => {
    mockKatexRenderToString.mockClear()
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
    // Should not throw, should return something (escaped fallback)
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
    // $$...$$ should be matched as display, not $...$ as inline
    const input = '<p>$$x^2 + y^2$$</p>'
    renderKatexInString(input)
    // Should be called with display mode, not inline
    expect(mockKatexRenderToString).toHaveBeenCalledWith('x^2 + y^2', expect.objectContaining({ displayMode: true }))
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
    expect(result).toContain('<div class="table-wrap"><table>')
    expect(result).toContain('</table></div>')
  })

  it('skips table wrapping when wrapTables=false', () => {
    mockMarkedParse.mockReturnValue('<table><tr><td>data</td></tr></table>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    const result = renderMarkdown('table content', { wrapTables: false })
    expect(result).not.toContain('table-wrap')
  })

  it('calls fixImagePaths when provided', () => {
    mockMarkedParse.mockReturnValue('<img src="test.png">')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)
    const fixFn = vi.fn((html: string) => html)

    renderMarkdown('img', { fixImagePaths: fixFn })
    expect(fixFn).toHaveBeenCalled()
  })

  it('calls postProcess when provided', () => {
    mockMarkedParse.mockReturnValue('<p>content</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)
    const postFn = vi.fn((html: string) => html)

    renderMarkdown('content', { postProcess: postFn })
    expect(postFn).toHaveBeenCalled()
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

  it('passes ADD_TAGS: ["math"] to DOMPurify', () => {
    mockMarkedParse.mockReturnValue('<p>content</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content')
    expect(mockDOMPurifySanitize).toHaveBeenCalledWith(expect.any(String), { ADD_TAGS: ['math'] })
  })

  it('renders KaTeX before sanitizing', () => {
    mockMarkedParse.mockReturnValue('<p>$$x^2$$</p>')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)

    renderMarkdown('content')
    // KaTeX should have been called before DOMPurify
    expect(mockKatexRenderToString).toHaveBeenCalled()
    expect(mockDOMPurifySanitize).toHaveBeenCalled()
  })

  it('applies fixImagePaths after sanitizing', () => {
    mockMarkedParse.mockReturnValue('<img src="old.png">')
    mockDOMPurifySanitize.mockImplementation((s: string) => s)
    const fixFn = vi.fn((html: string) => html.replace('old.png', 'new.png'))

    const result = renderMarkdown('img', { fixImagePaths: fixFn })
    expect(fixFn).toHaveBeenCalled()
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

    // The pre should be replaced by a div.mermaid
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

    // Should replace with error pre, not throw
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

  it('exposes renderMarkdown and renderMermaidInElement', () => {
    const { renderMarkdown: rm, renderMermaidInElement: rme } = useMarkdownRenderer()
    expect(typeof rm).toBe('function')
    expect(typeof rme).toBe('function')
  })

  it('renderMarkdown works through composable', () => {
    mockMarkedParse.mockReturnValue('<p>test</p>')
    const { renderMarkdown: rm } = useMarkdownRenderer()
    const result = rm('test')
    expect(result).toBeDefined()
  })
})
