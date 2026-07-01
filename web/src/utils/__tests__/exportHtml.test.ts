import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock mermaid - must use hoisted factory without referencing outer variables
vi.mock('@/utils/globals.ts', () => ({
  mermaid: {
    initialize: vi.fn(),
    render: vi.fn(),
  },
}))

// Mock fetch for inlineImages
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

import { exportRenderedHtml } from '@/utils/exportHtml.ts'
import { mermaid } from '@/utils/globals.ts'

describe('exportRenderedHtml', () => {
  beforeEach(() => {
    mockFetch.mockReset()
    ;(mermaid.initialize as ReturnType<typeof vi.fn>).mockReset()
    ;(mermaid.render as ReturnType<typeof vi.fn>).mockReset()
    // Default: batch-base64 returns empty results
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ results: {} }),
    })
    // Set a default theme
    document.documentElement.setAttribute('data-theme', 'light')
  })

  function createElement(html: string): HTMLElement {
    const div = document.createElement('div')
    div.className = 'markdown-body'
    div.innerHTML = html
    document.body.appendChild(div)
    return div
  }

  it('produces a valid HTML document with DOCTYPE', async () => {
    const el = createElement('<p>Hello world</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('<!DOCTYPE html>')
    expect(result.html).toContain('<html lang="en"')
    expect(result.html).toContain('</html>')
    expect(result.html).toContain('<body>')
    expect(result.html).toContain('</body>')
  })

  it('includes the title from fileName without .md extension', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'readme.md',
      fileName: 'readme.md',
    })
    el.remove()

    expect(result.html).toContain('<title>readme</title>')
  })

  it('escapes HTML in fileName for title', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'a&b.md',
      fileName: 'a&b.md',
    })
    el.remove()

    expect(result.html).toContain('<title>a&amp;b</title>')
  })

  it('uses current app theme as data-theme attribute', async () => {
    document.documentElement.setAttribute('data-theme', 'dark')
    const el = createElement('<p>dark mode</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('data-theme="dark"')
  })

  it('includes the cloned body content', async () => {
    const el = createElement('<p>Test content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('Test content')
  })

  it('removes script tags from cloned DOM', async () => {
    const el = createElement('<p>content</p><script>alert("xss")</script>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).not.toContain('alert("xss")')
  })

  it('removes iframe elements from cloned DOM', async () => {
    const el = createElement('<p>content</p><iframe src="https://evil.com"></iframe>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).not.toContain('iframe')
    expect(result.html).not.toContain('evil.com')
  })

  it('removes KaTeX MathML annotations', async () => {
    const el = createElement('<p>content</p><span class="katex-mathml"><math><mi>x</mi></math></span>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).not.toContain('katex-mathml')
  })

  it('includes theme toggle button', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('id="theme-toggle"')
    expect(result.html).toContain('theme-icon-moon')
    expect(result.html).toContain('theme-icon-sun')
  })

  it('includes theme toggle JavaScript', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('exported-html-theme')
  })

  it('includes code block interaction JavaScript', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('code-block-copy-btn')
    expect(result.html).toContain('copyText')
  })

  it('returns zero skipped/external images when no images present', async () => {
    const el = createElement('<p>no images</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.skippedImages).toBe(0)
    expect(result.externalImages).toBe(0)
  })

  it('counts external images (https://)', async () => {
    const el = createElement('<img src="https://example.com/img.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.externalImages).toBe(1)
    expect(result.skippedImages).toBe(0)
  })

  it('counts external images (//)', async () => {
    const el = createElement('<img src="//cdn.example.com/img.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.externalImages).toBe(1)
  })

  it('counts external images (http://)', async () => {
    const el = createElement('<img src="http://example.com/img.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.externalImages).toBe(1)
  })

  it('skips data URIs (already self-contained)', async () => {
    const el = createElement('<img src="data:image/png;base64,abc123">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.skippedImages).toBe(0)
    expect(result.externalImages).toBe(0)
  })

  it('inlines local images via batch-base64 API', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: {
          'images/photo.png': { mime: 'image/png', data: 'base64data' },
        },
      }),
    })

    const el = createElement('<img src="/api/local-file/images/photo.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/file/batch-base64',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      })
    )
    expect(result.skippedImages).toBe(0)
  })

  it('counts skipped images when API fails', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
    })

    const el = createElement('<img src="/api/local-file/images/photo.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.skippedImages).toBe(1)
  })

  it('counts skipped images on network error', async () => {
    mockFetch.mockRejectedValue(new Error('Network error'))

    const el = createElement('<img src="/api/local-file/images/photo.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.skippedImages).toBe(1)
  })

  it('counts skipped images when server skips paths', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ results: {} }),
    })

    const el = createElement('<img src="/api/local-file/images/missing.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.skippedImages).toBe(1)
  })

  it('handles multiple local images', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: {
          'a.png': { mime: 'image/png', data: 'aaa' },
          'b.png': { mime: 'image/png', data: 'bbb' },
        },
      }),
    })

    const el = createElement('<img src="/api/local-file/a.png"><img src="/api/local-file/b.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.skippedImages).toBe(0)
  })

  it('handles URL-encoded paths', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: {
          'my folder/photo.png': { mime: 'image/png', data: 'data' },
        },
      }),
    })

    const el = createElement('<img src="/api/local-file/my%20folder/photo.png">')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/file/batch-base64',
      expect.objectContaining({
        body: JSON.stringify({ paths: ['my folder/photo.png'] }),
      })
    )
    expect(result.skippedImages).toBe(0)
  })

  it('handles paths with query parameters', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: {
          'img.png': { mime: 'image/png', data: 'data' },
        },
      }),
    })

    const el = createElement('<img src="/api/local-file/img.png?t=12345">')
    await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/file/batch-base64',
      expect.objectContaining({
        body: JSON.stringify({ paths: ['img.png'] }),
      })
    )
  })

  it('replaces failed Mermaid blocks with error indicators', async () => {
    const el = createElement('<pre class="mermaid">graph TD; A--&gt;B</pre>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('mermaid-error')
    expect(result.html).toContain('Diagram failed to render')
  })

  it('keeps successfully rendered Mermaid blocks (with SVG)', async () => {
    const el = createElement('<div class="mermaid" data-mermaid="graph TD; A-->B"><svg>diagram</svg></div>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    // Should not contain error indicator since the Mermaid block has a rendered SVG
    expect(result.html).not.toContain('Diagram failed to render')
  })

  it('replaces failed div.mermaid blocks', async () => {
    const el = createElement('<div class="mermaid">graph TD; A--&gt;B</div>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('mermaid-error')
  })

  it('replaces failed code.mermaid blocks', async () => {
    const el = createElement('<code class="mermaid">graph TD</code>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('mermaid-error')
  })

  it('builds TOC from headings with IDs', async () => {
    const el = createElement('<h1 id="intro">Introduction</h1><h2 id="setup">Setup</h2><p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('toc-toggle')
    expect(result.html).toContain('toc-drawer')
    expect(result.html).toContain('Table of Contents')
    expect(result.html).toContain('#intro')
    expect(result.html).toContain('#setup')
    expect(result.html).toContain('Introduction')
    expect(result.html).toContain('Setup')
  })

  it('does not include TOC when no headings', async () => {
    const el = createElement('<p>Just a paragraph</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).not.toContain('toc-toggle')
    expect(result.html).not.toContain('toc-drawer')
  })

  it('does not include headings without IDs in TOC', async () => {
    const el = createElement('<h1>No ID heading</h1><h2 id="has-id">Has ID</h2>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('toc-drawer')
    expect(result.html).toContain('#has-id')
    const tocItems = result.html.match(/class="toc-item"/g)
    expect(tocItems).toHaveLength(1)
  })

  it('escapes HTML in TOC entries', async () => {
    const el = createElement('<h1 id="test">A &amp; B</h1>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('A &amp; B')
  })

  it('indents TOC items by heading level', async () => {
    const el = createElement('<h1 id="h1">H1</h1><h3 id="h3">H3</h3>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('padding-left: 8px')
    expect(result.html).toContain('padding-left: 40px')
  })

  it('includes CSS with var() references for theme support', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('var(--bg-primary)')
    expect(result.html).toContain('var(--text-primary)')
  })

  it('includes scrollbar styling CSS', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('::-webkit-scrollbar')
  })

  it('includes mermaid error styling', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('.mermaid-error')
  })

  it('includes dual-theme mermaid CSS', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('.mermaid-dual')
    expect(result.html).toContain('.mermaid-light')
    expect(result.html).toContain('.mermaid-dark')
  })

  it('includes localStorage theme restore script in head', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('localStorage.getItem')
    expect(result.html).toContain('exported-html-theme')
  })

  it('includes charset and viewport meta tags', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('charset="utf-8"')
    expect(result.html).toContain('viewport')
  })

  it('handles empty markdown body', async () => {
    const el = createElement('')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'empty.md',
      fileName: 'empty.md',
    })
    el.remove()

    expect(result.html).toContain('<!DOCTYPE html>')
    expect(result.skippedImages).toBe(0)
    expect(result.externalImages).toBe(0)
  })

  it('handles Mermaid dual-theme rendering', async () => {
    ;(mermaid.render as ReturnType<typeof vi.fn>).mockResolvedValue({ svg: '<svg>opposite</svg>' })

    const el = createElement('<div class="mermaid" data-mermaid="graph TD; A-->B"><svg>current</svg></div>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(mermaid.initialize).toHaveBeenCalled()
    expect(mermaid.render).toHaveBeenCalled()
    expect(result.html).toContain('mermaid-dual')
  })

  it('handles Mermaid render failure gracefully', async () => {
    ;(mermaid.render as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('Render failed'))

    const el = createElement('<div class="mermaid" data-mermaid="graph TD; A-->B"><svg>current</svg></div>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('<!DOCTYPE html>')
  })

  it('handles Mermaid block without data-mermaid attribute', async () => {
    const el = createElement('<div class="mermaid"><svg>diagram</svg></div>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('<!DOCTYPE html>')
  })

  it('handles dark theme as current theme', async () => {
    document.documentElement.setAttribute('data-theme', 'dark')
    ;(mermaid.render as ReturnType<typeof vi.fn>).mockResolvedValue({ svg: '<svg>light</svg>' })

    const el = createElement('<div class="mermaid" data-mermaid="graph TD"><svg>dark</svg></div>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('data-theme="dark"')
    expect(result.html).toContain('mermaid-dual')
  })

  it('handles table block interaction JS', async () => {
    const el = createElement('<p>content</p>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.html).toContain('table-block-copy-btn')
    expect(result.html).toContain('tableToText')
  })

  it('handles multiple images with mixed types', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: {
          'local.png': { mime: 'image/png', data: 'data' },
        },
      }),
    })

    const el = createElement(
      '<img src="data:image/png;base64,aaa">' +
      '<img src="https://example.com/ext.png">' +
      '<img src="/api/local-file/local.png">' +
      '<img src="/api/local-file/missing.png">'
    )
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.externalImages).toBe(1)
    expect(result.skippedImages).toBe(1)
  })

  it('handles img with no src attribute', async () => {
    const el = createElement('<img>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    expect(result.skippedImages).toBe(0)
    expect(result.externalImages).toBe(0)
  })

  it('strips scripts and iframes from opposite-theme Mermaid SVG', async () => {
    ;(mermaid.render as ReturnType<typeof vi.fn>).mockResolvedValue({
      svg: '<svg><script>alert(1)</script><iframe src="x"></iframe>diagram</svg>',
    })

    const el = createElement('<div class="mermaid" data-mermaid="graph TD"><svg>current</svg></div>')
    const result = await exportRenderedHtml({
      markdownBodyEl: el,
      filePath: 'test.md',
      fileName: 'test.md',
    })
    el.remove()

    // The opposite theme div should not contain script or iframe
    expect(result.html).toContain('mermaid-dual')
  })
})
