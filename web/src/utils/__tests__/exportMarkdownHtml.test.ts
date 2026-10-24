import { describe, expect, it, vi, beforeEach } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

// Mock fetch for image embedding + path verification + KaTeX font requests.
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

import { exportMarkdownToHtml, imageIssueReasonCode } from '@/utils/exportMarkdownHtml.ts'
import { configureMarkedRenderer } from '@/utils/markedConfig.ts'

// The real app calls configureMarkedRenderer() once at startup (main.ts). It
// registers the heading-id + code (mermaid/ highlight) renderer hooks; without
// it headings have no id and mermaid fenced blocks stay plain <pre>.
configureMarkedRenderer()

// Mock the lazily-imported mermaid renderer: the real one needs a real layout
// engine (getBBox etc.) that jsdom does not provide. Produce a deterministic
// container with an <svg> like mermaid.ts does on success.
vi.mock('@/utils/mermaid.ts', () => ({
  renderMermaidInElement: vi.fn(async (el: HTMLElement, prefix = 'mermaid', specificBlocks?: NodeList) => {
    // Mirrors the real renderer: after a successful render it arms the figure
    // (armMermaidFigureForContext → armMermaidFigure). The failed branch does
    // NOT arm — matching production, where the error container is left bare for
    // handleFailedMermaid to replace.
    const { armMermaidFigure } = await import('@/utils/mediaBlockFactory.ts')
    const blocks = specificBlocks || el.querySelectorAll('pre.mermaid:not([data-rendered])')
    for (const block of Array.from(blocks)) {
      const pre = block as HTMLElement
      pre.setAttribute('data-rendered', '1')
      const container = document.createElement('div')
      container.className = 'mermaid'
      container.id = `${prefix}-0`
      const source = pre.textContent || ''
      if (/FAIL/.test(source)) {
        // Simulate a render failure: mermaid.ts sets data-mermaid-error + retry HTML.
        container.setAttribute('data-mermaid', source)
        container.setAttribute('data-mermaid-error', '1')
        container.innerHTML = '<pre class="mermaid-error-pre">Mermaid Error: syntax</pre><button class="mermaid-retry-btn" type="button">Retry</button>'
      } else {
        container.setAttribute('data-mermaid', source)
        // Mermaid's real output shape: an inline max-width carrying its natural
        // size, plus width="100%" so it can grow to the container.
        container.innerHTML = '<svg class="mermaid-svg" width="100%" viewBox="0 0 1008.125 70" style="max-width: 1008.125px;"><g /></svg>'
      }
      pre.replaceWith(container)
      if (!container.dataset.mermaidError) armMermaidFigure(container, { attach: false })
    }
  }),
  initMermaid: vi.fn(),
  reRenderMermaid: vi.fn(),
}))

describe('exportMarkdownToHtml', () => {
  beforeEach(() => {
    mockFetch.mockReset()
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ results: {} }),
    })
    // Default theme
    document.documentElement.setAttribute('data-theme', 'github-light')
    document.documentElement.setAttribute('data-theme-base', 'light')
  })

  const opts = (extra: Record<string, unknown> = {}) => ({
    content: '',
    path: 'README.md',
    fileName: 'README.md',
    ...extra,
  })

  it('produces a valid HTML document with DOCTYPE', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'Hello world' }))
    expect(result.html).toContain('<!DOCTYPE html>')
    expect(result.html).toContain('<html lang="en"')
    expect(result.html).toContain('</html>')
    expect(result.html).toContain('<body>')
    expect(result.html).toContain('</body>')
  })

  /**
   * The export is a STATIC document — it ships no click layer, so a
   * `<span class="md-external-link" data-href>` would render as link-coloured,
   * underlined, non-clickable text. Every external link must be a real anchor
   * here. (In the app the span is correct; see utils/externalLinkNode.ts.)
   */
  it('restores external links to real anchors', async () => {
    const result = await exportMarkdownToHtml(opts({
      content: 'see [site](https://example.com/a) and github.com/foo/bar',
    }))
    expect(result.html).toContain('<a href="https://example.com/a"')
    expect(result.html).toContain('target="_blank"')
    expect(result.html).toContain('rel="noopener noreferrer"')
    expect(result.html).toContain('<a href="https://github.com/foo/bar"')
    // No dead link spans left behind.
    expect(result.html).not.toContain('md-external-link')
    expect(result.html).not.toContain('data-href')
  })

  it('wraps content in .markdown-body > .markdown-content like the preview', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'Hello world' }))
    expect(result.html).toContain('class="markdown-body"')
    expect(result.html).toContain('class="markdown-content"')
    expect(result.html).toMatch(/class="markdown-body"[^>]*data-file-path="README\.md"/)
  })

  it('includes base typography rules (font-size 15px + line-height 1.6)', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'Hi' }))
    expect(result.html).toContain('font-size: 15px')
    expect(result.html).toContain('line-height: 1.6')
  })

  it('exports the current app theme (dark) with data-theme + data-theme-base', async () => {
    document.documentElement.setAttribute('data-theme', 'github-dark')
    document.documentElement.setAttribute('data-theme-base', 'dark')
    const result = await exportMarkdownToHtml(opts({ content: 'dark' }))
    expect(result.html).toContain('data-theme="github-dark"')
    expect(result.html).toContain('data-theme-base="dark"')
  })

  it('infers data-theme-base from theme id when attribute is missing', async () => {
    document.documentElement.setAttribute('data-theme', 'nord')
    document.documentElement.removeAttribute('data-theme-base')
    const result = await exportMarkdownToHtml(opts({ content: 'nord' }))
    expect(result.html).toContain('data-theme="nord"')
    expect(result.html).toContain('data-theme-base="dark"')
  })

  it('renders markdown content through the real pipeline (headings, lists, emphasis)', async () => {
    const md = '# Title\n\nSome **bold** and `code`.\n\n- one\n- two'
    const result = await exportMarkdownToHtml(opts({ content: md }))
    expect(result.html).toContain('<h1')
    expect(result.html).toContain('bold')
    expect(result.html).toContain('<li>one</li>')
  })

  it('includes title from fileName without .md extension (escaped)', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'x', fileName: 'a&b.md' }))
    expect(result.html).toContain('<title>a&amp;b</title>')
  })

  it('does not include a theme toggle button or theme-switch JS', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'x' }))
    expect(result.html).not.toContain('id="theme-toggle"')
    expect(result.html).not.toContain('exported-html-theme')
    expect(result.html).not.toContain('localStorage.getItem')
  })

  it('renders fenced code blocks with language label and copy/wrap header', async () => {
    const md = '```js\nconst a = 1\n```'
    const result = await exportMarkdownToHtml(opts({ content: md }))
    expect(result.html).toContain('code-block-wrapper')
    expect(result.html).toContain('code-block-lang')
    expect(result.html).toContain('code-block-copy-btn')
    expect(result.html).toContain('data-action="wrap"')
    // Marked + hljs real pipeline — the language span class survives.
    expect(result.html).toContain('language-js')
  })

  it('positions the table copy menu in the exported document too', async () => {
    // The exported .html reuses css/code-block-header.css, whose menu is
    // position:fixed. Without the matching placement JS the menu would open at
    // the viewport origin. Assert BOTH halves exist so the pair cannot drift.
    const md = '| a | b |\n| --- | --- |\n| 1 | 2 |'
    const result = await exportMarkdownToHtml(opts({ content: md }))
    expect(result.html).toContain('table-block-copy-menu')
    expect(result.html).toContain('function positionTableMenu')
    expect(result.html).toContain('positionTableMenu(tableBtn, menu)')
  })

  it('includes code block interaction JS', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'x' }))
    expect(result.html).toContain('code-block-copy-btn')
    expect(result.html).toContain('copyText')
  })

  it('builds a share-style TOC shell from headings with IDs', async () => {
    const md = '# Intro\n\npara\n\n## Setup\n\nmore'
    const result = await exportMarkdownToHtml(opts({ content: md }))
    // ShareView shell: topbar + body row + 240px TOC rail + scrollable content.
    expect(result.html).toContain('share-topbar')
    expect(result.html).toContain('share-file-name')
    expect(result.html).toContain('Table of Contents')
    expect(result.html).toContain('share-toc')
    expect(result.html).toContain('data-target="#intro"')
    expect(result.html).toContain('data-target="#setup"')
    expect(result.html).toContain('IntersectionObserver')
    expect(result.html).toContain('rootMargin')
    // The shell root mirrors the share page (.share-view wrapper).
    expect(result.html).toContain('class="share-view"')
    // Topbar carries a TOC toggle (share-btn) but NO download action.
    expect(result.html).toContain('share-btn')
    expect(result.html).not.toContain('share-download')
  })

  it('localizes TOC title, copy feedback and word-wrap labels for zh', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '# A', locale: 'zh' }))
    expect(result.html).toContain('<html lang="zh-CN"')
    expect(result.html).toContain('目录')
    expect(result.html).not.toContain('Table of Contents')
  })

  it('does not include TOC when no headings', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'Just a paragraph' }))
    expect(result.html).not.toContain('toc-sidebar')
    expect(result.html).not.toContain('id="toc-toggle"')
  })

  it('escapes HTML in TOC entries', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '# A &amp; B' }))
    expect(result.html).toContain('A &amp; B')
  })

  it('indents TOC items by heading level', async () => {
    const md = '# H1\n\n## H2\n\n### H3'
    const result = await exportMarkdownToHtml(opts({ content: md }))
    expect(result.html).toContain('data-level="1"')
    expect(result.html).toContain('data-level="2"')
    expect(result.html).toContain('data-level="3"')
    // Share page indents items via inline padding-left (8 + (level-1)*14).
    expect(result.html).toContain('padding-left:8px')
    expect(result.html).toContain('padding-left:22px')
    expect(result.html).toContain('padding-left:36px')
  })

  it('serializes CSS rules that hit the exported DOM only', async () => {
    // Inject an unrelated app-chrome rule and a content rule into a real stylesheet.
    const style = document.createElement('style')
    style.textContent = `
      .app-container { display: flex; }
      .header { position: fixed; }
      .markdown-body h1 { font-size: 1.6em; }
    `
    document.head.appendChild(style)
    try {
      const result = await exportMarkdownToHtml(opts({ content: '# Title' }))
      // Content hit → included; app chrome → excluded.
      expect(result.html).toContain('.markdown-body h1')
      expect(result.html).toContain('font-size: 1.6em')
      expect(result.html).not.toContain('.app-container {')
      expect(result.html).not.toContain('.header {')
    } finally {
      style.remove()
    }
  })

  it('serializes interaction (state-pseudo) rules that target exported DOM', async () => {
    // Dynamic pseudo-classes (:hover/:active) do NOT throw in querySelector/matches —
    // they must be stripped and the base selector tested, or hover feedback
    // (link underline, copy-button hover, table-row hover) is lost in export.
    const style = document.createElement('style')
    style.textContent = `
      .markdown-body a:hover { text-decoration: underline; }
      .markdown-body .code-block-copy-btn:hover { color: red; }
      .markdown-body tbody tr[data-row-idx]:hover { background: blue; }
      .app-container:hover { color: green; }
    `
    document.head.appendChild(style)
    try {
      const md = '# T\n\n[link](/api/fs/raw/x.ts)\n\n```js\nconst a = 1\n```\n\n| a |\n|---|\n| 1 |'
      const result = await exportMarkdownToHtml(opts({ content: md }))
      expect(result.html).toContain('.markdown-body a:hover')
      expect(result.html).toContain('.code-block-copy-btn:hover')
      expect(result.html).toContain('tbody tr[data-row-idx]:hover')
      // App-chrome hover rule must NOT leak in.
      expect(result.html).not.toContain('.app-container:hover')
    } finally {
      style.remove()
    }
  })

  it('carries user-selected --font-ui/--font-mono from html inline style into export', async () => {
    const root = document.documentElement
    root.style.setProperty('--font-ui', '"My UI", sans-serif')
    root.style.setProperty('--font-mono', '"My Mono", monospace')
    try {
      const result = await exportMarkdownToHtml(opts({ content: 'text' }))
      expect(result.html).toContain('--font-ui: "My UI", sans-serif')
      expect(result.html).toContain('--font-mono: "My Mono", monospace')
    } finally {
      root.style.removeProperty('--font-ui')
      root.style.removeProperty('--font-mono')
    }
  })

  it('does not emit font vars when html has none set', async () => {
    const root = document.documentElement
    root.style.removeProperty('--font-ui')
    root.style.removeProperty('--font-mono')
    const result = await exportMarkdownToHtml(opts({ content: 'x' }))
    expect(result.html).not.toContain('--font-ui:')
  })

  it('uses a single topbar toggle as the sole TOC on/off control', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '# T' }))
    // No in-panel collapse button / old rail — TOC rail carries only its items.
    expect(result.html).not.toContain('id="toc-collapse"')
    expect(result.html).not.toContain('toc-rail-btn')
    // The topbar toggle is present and starts in the open state (share-btn).
    expect(result.html).toContain('id="toc-toggle"')
    expect(result.html).toContain('share-topbar')
    expect(result.html).toContain('share-btn')
    expect(result.html).toContain('aria-expanded="true"')
    expect(result.html).toContain('aria-controls="share-toc"')
    // Collapsed state simply hides the rail via the body attribute.
    expect(result.html).toContain('.share-body[data-toc-open="false"] .share-toc')
    expect(result.html).toContain('display: none')
    // The chrome comes from the shared share-chrome.css stylesheet.
    expect(result.html).toContain('.share-toc {')
    // Design px through --dp so the rail follows the UI scale.
    expect(result.html).toMatch(/width:\s*calc\(\s*240\s*\*\s*var\(--dp\)\s*\)/)
    expect(result.html).toContain('.share-view')
    // No topbar download button in the offline doc.
    expect(result.html).not.toContain('share-download')
  })

  it('embeds the brand logo as a data URI so the offline doc is self-contained', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '# T' }))
    // The exported .html is opened from file://, so the logo must be inlined —
    // a root-absolute /logo-64.png would 404. Relative `?inline` import is
    // required (Vite skips inlining imports that resolve inside publicDir).
    const logo = result.html.match(/<img class="share-logo" src="([^"]+)"/)
    expect(logo, 'topbar must carry the brand logo').not.toBeNull()
    expect(logo![1]).toMatch(/^data:image\/png;base64,/)
    // And no bare public-path reference survives.
    expect(result.html).not.toContain('src="/logo-64.png"')
  })

  it('flashes the jumped heading when a TOC item is clicked', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '# T' }))
    // The click handler adds .line-flash to the heading after scrolling.
    expect(result.html).toContain(`el.classList.add('line-flash')`)
    expect(result.html).toContain(`el.addEventListener('animationend', function() { el.classList.remove('line-flash'); }`)
    // The animation comes from the shared share-chrome.css (self-contained) —
    // exactly once: serializeCss must NOT re-serialize the line-flash keyframes
    // from the app stylesheets, or the export would carry a duplicated copy.
    const keyframes = result.html.match(/@keyframes line-flash/g) ?? []
    expect(keyframes).toHaveLength(1)
    expect(result.html).toContain('.line-flash {')
  })

  it('embeds the shared share-chrome.css (single source with the share SPA)', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '# T' }))
    // Rules that live in css/share-chrome.css (used by ShareView.vue too) must
    // be inlined verbatim — proving the export reuses the same style source
    // instead of a hand-copied duplicate.
    expect(result.html).toContain('Share chrome — the visual shell shared by')
    expect(result.html).toContain('.share-view')
    expect(result.html).toContain('.share-top-actions')
    expect(result.html).toContain('.share-toc-title')
    expect(result.html).toContain('.share-toc-item:hover')
  })

  it('flips the topbar TOC toggle state on click (open ⇄ collapsed)', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '# T' }))
    // Toggling updates the body attribute that hides/shows the rail.
    expect(result.html).toContain(`body.setAttribute('data-toc-open', next ? 'true' : 'false')`)
    expect(result.html).toContain(`toggle.setAttribute('aria-expanded', next ? 'true' : 'false')`)
    // The toggle is wired to setState (click listener on #toc-toggle).
    expect(result.html).toContain(`toggle.addEventListener('click', function() { setState(!open); });`)
    // Rail/item language mirrors the share page (share-toc / share-toc-item).
    expect(result.html).toContain('share-toc-item')
    expect(result.html).toContain('data-target')
  })

  it('includes CSS with var() references for theme support', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'x' }))
    expect(result.html).toContain('var(--bg-primary)')
  })

  it('rewrites hljs theme selectors to data-theme-base', async () => {
    const style = document.createElement('style')
    style.textContent = `
      [data-hljs-theme="light"] .hljs { color: #111; }
      [data-hljs-theme="dark"] .hljs { color: #eee; }
    `
    document.head.appendChild(style)
    try {
      const result = await exportMarkdownToHtml(opts({ content: 'x' }))
      expect(result.html).toContain('[data-theme-base="light"] .hljs')
      expect(result.html).toContain('[data-theme-base="dark"] .hljs')
      expect(result.html).not.toContain('data-hljs-theme')
    } finally {
      style.remove()
    }
  })

  it('counts external images (https://)', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '![x](https://example.com/a.png)' }))
    expect(result.externalImages).toBe(1)
    expect(result.skippedImages).toBe(0)
  })

  it('skips data URIs (already self-contained)', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '![x](data:image/png;base64,abc123)' }))
    expect(result.skippedImages).toBe(0)
    expect(result.externalImages).toBe(0)
  })

  it('inlines local images via batch-base64 API', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: {
          'sub/images/photo.png': { mime: 'image/png', data: 'base64data' },
        },
      }),
    })
    const md = '![p](images/photo.png)'
    const result = await exportMarkdownToHtml(opts({ content: md, path: 'sub/README.md' }))
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/file/batch-base64',
      expect.objectContaining({ method: 'POST' })
    )
    expect(result.skippedImages).toBe(0)
  })

  it('counts skipped images when the API fails', async () => {
    mockFetch.mockResolvedValue({ ok: false })
    const result = await exportMarkdownToHtml(opts({ content: '![p](/api/fs/raw/img.png)' }))
    expect(result.skippedImages).toBe(1)
  })

  it('chunks more than 50 images instead of failing the whole batch', async () => {
    // The endpoint rejects >50 paths with 400 TooManyPaths. Un-chunked, that one
    // 400 made EVERY local image keep its /api/fs/raw src (broken in the
    // standalone export). Each chunk must be fetched and applied independently.
    //
    // The markdown itself is the cost here: 120 image blocks go through the
    // whole marked + rewrite pipeline and measured ~4.2s, i.e. 84% of the 5s
    // default. Under the full suite's parallel load that tips over and the file
    // reports a timeout — which then drops every one of its 47 tests from the
    // coverage profile. A per-test timeout keeps the assertion (still 120
    // images, still 3 chunks) without racing the scheduler.
    const calls: string[][] = []
    mockFetch.mockImplementation((_url: string, init: RequestInit) => {
      const body = JSON.parse(String(init.body)) as { paths: string[] }
      calls.push(body.paths)
      const results: Record<string, { mime: string; data: string }> = {}
      for (const p of body.paths) results[p] = { mime: 'image/png', data: 'x' }
      return Promise.resolve({ ok: true, json: async () => ({ results }) })
    })

    const md = Array.from({ length: 120 }, (_, i) => `![p${i}](/api/fs/raw/img${i}.png)`).join('\n\n')
    const result = await exportMarkdownToHtml(opts({ content: md }))

    // Three requests of at most 50, and nothing was skipped.
    expect(calls.length).toBe(3)
    for (const c of calls) expect(c.length).toBeLessThanOrEqual(50)
    expect(result.skippedImages).toBe(0)
    // Every image was actually inlined.
    expect(result.html).not.toContain('/api/fs/raw/img0.png')
    expect(result.html).toContain('data:image/png;base64,x')
  }, 20_000)

  it('counts skipped images when server skips paths', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ results: {} }) })
    const result = await exportMarkdownToHtml(opts({ content: '![p](/api/fs/raw/missing.png)' }))
    expect(result.skippedImages).toBe(1)
  })

  it('collects server-reported skip reasons into issues', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({
        results: {},
        skipped: [{ path: 'images/huge.png', reason: 'exceeds 2MB limit' }],
      }),
    })
    const result = await exportMarkdownToHtml(opts({ content: '![p](/api/fs/raw/images/huge.png)' }))
    expect(result.issues[0]).toMatchObject({ reason: 'exceeds 2MB limit', kind: 'skipped' })
    expect(imageIssueReasonCode(result.issues[0].reason)).toBe('too_large')
  })

  it('replaces failed Mermaid blocks with error indicators', async () => {
    const md = '```mermaid\nFAIL graph TD; A-->B\n```'
    const result = await exportMarkdownToHtml(opts({ content: md }))
    expect(result.html).toContain('mermaid-error')
    expect(result.html).toContain('Diagram failed to render')
  })

  it('keeps successfully rendered Mermaid blocks (SVG)', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '<div class="mermaid"><svg>diagram</svg></div>' }))
    expect(result.html).toContain('diagram')
    expect(result.html).not.toContain('Diagram failed to render')
  })

  it('gives lightbox SVG (mermaid) the theme content background like the app lightbox', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '<div class="mermaid"><svg>diagram</svg></div>' }))
    // App Lightbox.vue applies `.lightbox-content svg { background: var(--bg-primary) }`;
    // the export lightbox must do the same so diagrams aren't transparent over the overlay.
    expect(result.html).toContain('.export-lightbox-view svg')
    expect(result.html).toContain('background: var(--bg-primary)')
  })

  it('includes lightbox zoom/pan interactions matching the in-app lightbox', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '<p>hi</p>' }))
    // Wheel zoom with the app's 0.85/1.2 factors.
    expect(result.html).toContain("e.deltaY > 0 ? 0.85 : 1.2")
    // Drag-to-pan only when zoomed past fit, pinch zoom, and a transform on content.
    expect(result.html).toContain('function canDrag()')
    expect(result.html).toContain('touchStartDist')
    expect(result.html).toContain("'translate(' + tx + 'px, ' + ty + 'px) scale(' + scale + ')'")
    // Backdrop click + Esc close (Esc handler present in the lightbox script).
    expect(result.html).toContain("if (e.target === overlay || e.target === view) closeLightbox()")
    expect(result.html).toContain("if (e.key === 'Escape') closeLightbox()")
  })

  it('produces syntactically valid inline script (no template-embedding breakage)', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '# T\n\n![x](https://a.com/i.png)\n\n<div class="mermaid"><svg><g /></svg></div>' }))
    const m = result.html.match(/<script>([\s\S]*?)<\/script>/)
    expect(m).toBeTruthy()
    // Compiling proves no stray backticks / ${ } / broken braces leaked into the
    // embedded JS from the surrounding TS template literal.
    expect(() => new Function(m![1])).not.toThrow()
  })

  it('removes script tags and iframes from the content DOM', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '<script>alert(1)</script><iframe src="https://evil.com"></iframe>' }))
    expect(result.html).not.toContain('alert(1)')
    expect(result.html).not.toContain('evil.com')
  })

  it('does not embed KaTeX fonts when no math present', async () => {
    const result = await exportMarkdownToHtml(opts({ content: 'No math' }))
    expect(result.html).not.toContain('data:font/woff2')
  })

  it('handles empty content', async () => {
    const result = await exportMarkdownToHtml(opts({ content: '' }))
    expect(result.html).toContain('<!DOCTYPE html>')
    expect(result.skippedImages).toBe(0)
  })

  it('does not depend on the live .markdown-body DOM (no clone from document)', async () => {
    // Remove any .markdown-body in document — export still works from content.
    document.body.innerHTML = ''
    const result = await exportMarkdownToHtml(opts({ content: '# Solo title' }))
    expect(result.html).toContain('<h1')
  })

  it('stamps inline svg figures with the ratio-driven fill-width sizing', async () => {
    // An inline <svg> block must be lifted into a figure and marked `.svg-fit`
    // with its aspect ratio, so the exported document sizes it exactly like the
    // in-app preview. The ratio is derived from the viewBox (no image load).
    const result = await exportMarkdownToHtml(
      opts({ content: '<svg viewBox="0 0 400 100"><rect width="400" height="100"></rect></svg>' }),
    )
    expect(result.html).toContain('image-block-wrapper svg-fit')
    expect(result.html).toContain('--svg-ar: 4')
  })

  it('carries the svg-fit layout rules into the standalone export', async () => {
    // The layout lives in media-block.css, which the hit-detection serializer
    // copies only for rules that match the exported DOM. A document with SVG
    // media must therefore bring the full-width frame and the contain-centring
    // along — otherwise the export would show the media at its intrinsic size
    // in a fit-content box.
    //
    // jsdom starts with NO stylesheets (the app links its CSS from index.html,
    // which the test never loads), so the real media-block.css is injected here
    // to exercise the production hit-detection path against the real rule text.
    const css = readWebFile('css/media-block.css')
    const style = document.createElement('style')
    style.textContent = css
    document.head.appendChild(style)
    try {
      const result = await exportMarkdownToHtml(
        opts({ content: '<svg viewBox="0 0 400 100"><rect width="400" height="100"></rect></svg>' }),
      )
      expect(result.html).toContain('.image-block-wrapper.svg-fit')
      // The media is sized from its own ratio (never a percentage of the
      // shrink-to-fit frame, which would be a cycle).
      expect(result.html).toContain('var(--svg-ar')
      // mermaid's inline natural-size cap must be overridden.
      expect(result.html).toContain('max-width: none !important')
    } finally {
      style.remove()
    }
  })

  it('sizes mermaid diagrams like other SVG media', async () => {
    // Mermaid emits `style="max-width: Npx"` inline (its natural size). The
    // exported document must stamp the figure and carry the layout rules, so a
    // wide flowchart fills the column instead of staying at its natural width.
    const css = readWebFile('css/media-block.css')
    const style = document.createElement('style')
    style.textContent = css
    document.head.appendChild(style)
    try {
      const result = await exportMarkdownToHtml(
        opts({ content: '```mermaid\ngraph LR; A-->B\n```' }),
      )
      expect(result.html).toContain('image-block-wrapper svg-fit')
      // Same ratio-derived sizing as the in-app preview.
      expect(result.html).toContain('var(--svg-ar')
    } finally {
      style.remove()
    }
  })
  it('ships the inline zoom script and its styles', async () => {
    // The exported document must behave like the in-app preview: hovering a
    // diagram zooms it in place. The script is hand-mirrored from
    // `inlineZoom.ts` (the export has no bundler), so this pins that it is
    // actually injected — and that the state classes it toggles have styles,
    // otherwise the script would run against an unstyled document.
    const css = readWebFile('css/media-block.css')
    const style = document.createElement('style')
    style.textContent = css
    document.head.appendChild(style)
    try {
      const result = await exportMarkdownToHtml(
        opts({ content: '```mermaid\ngraph TD; A-->B\n```' }),
      )

      // The pointer-device gate, matching the module's own contract.
      expect(result.html).toContain("(hover: hover) and (pointer: fine)")
      // Wheel zoom + pan + reset wiring.
      expect(result.html).toContain('.image-block-reset-btn')
      expect(result.html).toContain('is-zooming')
      // The styles the script toggles must be present too. Asserting only the
      // BASE `display: none` is not enough — it matches the resting state, so it
      // passes even when every `.is-zooming` rule was dropped and the reset
      // button could never appear. The reveal rule is the real contract.
      expect(result.html).toContain('.image-block-wrapper')
      expect(result.html).toContain('display: none')
      expect(result.html).toContain('.image-block-wrapper.is-zooming')
      expect(result.html).toMatch(/\.is-zooming \.image-block-reset-btn[\s\S]*?display:\s*inline-flex/)
      // Pan/zoom cursors and the no-easing state, all of which live in the same
      // media block and were dropped together.
      expect(result.html).toContain('is-panning')
    } finally {
      style.remove()
    }
  })

  it('does not enable inline zoom on touch in the exported document', async () => {
    // Same rule as the app: touch keeps page scrolling and uses the lightbox.
    // The gate returns early, so nothing below it can install.
    const result = await exportMarkdownToHtml(opts({ content: '```mermaid\ngraph TD; A-->B\n```' }))
    expect(result.html).toContain("if (!mq || !mq.matches) return;")
  })
})
