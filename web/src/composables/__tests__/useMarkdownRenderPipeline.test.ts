import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { configureMarkedRenderer } from '@/utils/markedConfig'
import { _setIsPCForTest } from '@/composables/usePlatformDetect'
import {
  buildMarkdownPreviewDom,
  createFixLocalImagePaths,
} from '@/composables/useMarkdownRenderPipeline'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { setShareToken } from '@/share/shareMode'

configureMarkedRenderer()

describe('createFixLocalImagePaths', () => {
  it('resolves relative image srcs against the markdown file dir', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 42, isPC: true })
    const html = '<p><img src="assets/a.png" alt="a"></p>'
    const out = fix(html)
    expect(out).toContain('src="/api/file/thumb?path=docs/assets/a.png&w=1200"')
    expect(out).toContain('data-full-src="/api/local-file/docs/assets/a.png?t=42"')
    // data-attach-src carries the resolved project-relative path for re-drag
    expect(out).toContain('data-attach-src="docs/assets/a.png"')
  })

  it('keeps external URLs untouched', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    for (const src of ['https://x.com/a.png', '//cdn.x.com/a.png', '/abs/a.png', 'data:image/png;base64,abc']) {
      const out = fix(`<img src="${src}">`)
      expect(out).toContain(`src="${src}"`)
      expect(out).not.toContain('/api/')
      // External / data: images have no local file to re-drag — no attach data
      expect(out).not.toContain('data-attach-src')
    }
  })

  it('serves non-thumbnailable formats from the original full URL', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 7, isPC: false })
    const out = fix('<img src="anim.gif">')
    expect(out).toContain('src="/api/local-file/docs/anim.gif?t=7"')
    expect(out).not.toContain('/api/file/thumb')
    expect(out).toContain('data-attach-src="docs/anim.gif"')
    // Mobile width 640 for non-PC
    const pc = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 7, isPC: true })
    expect(pc('<img src="p.png">')).toContain('w=1200')
  })

  it('normalizes dot/.. segments and encodes CJK/space segments', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'a/b', imageTimestamp: 1, isPC: true })
    const out = fix('<img src="../c/图 d.png">')
    // ../ popped → a/c; CJK/space percent-encoded by segment.
    expect(out).toContain('path=a/c/%E5%9B%BE%20d.png')
    expect(out).not.toContain('../')
    // data-attach-src is the DECODED project-relative path (FileEntry.path form)
    expect(out).toContain('data-attach-src="a/c/图 d.png"')
  })

  it('wraps every image in a lightbox span', () => {
    const fix = createFixLocalImagePaths({ baseDir: '', imageTimestamp: 1, isPC: true })
    const out = fix('<img src="x.png"><img src="https://y.com/z.png">')
    expect(out).toContain('lightbox-img-wrap')
    expect(out.match(/lightbox-img-wrap/g)).toHaveLength(2)
  })

  it('injects the attach badge only for local images (data-attach-src)', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    const out = fix('<img src="a.png"><img src="https://x.com/b.png"><img src="data:image/png;base64,abc">')
    // Local raster → thumbnail; wrapper contains exactly one attach badge.
    expect(out.match(/img-attach-badge/g)).toHaveLength(1)
    // External / data: images get no badge.
    const localWrap = out.slice(0, out.indexOf('https://x.com'))
    expect(localWrap).toContain('img-attach-badge')
    expect(out.slice(out.indexOf('https://x.com'))).not.toContain('img-attach-badge')
  })

  it('HTML-escapes data-attach-src so decoded filenames cannot break the attribute', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    // A percent-encoded quote+onerror decodes into the path attribute. It must
    // stay escaped inside data-attach-src — never break out into new attributes.
    const out = fix('<img src="we%22onerror%3D%22alert(1).png">')
    expect(out).toContain('data-attach-src="docs/we&quot;onerror=&quot;alert(1).png"')
    // The src stays segment-encoded (quotes not present), so no literal
    // attribute breakout can occur anywhere in the rewritten tag.
    expect(out).toContain('src="/api/local-file/docs/we%22onerror%3D%22alert(1).png')
    expect(out).not.toContain('onerror="')
  })

  it('emits token-scoped full-size URLs and skips thumbnails in share mode', () => {
    setShareToken('tokabc')
    try {
      const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 42, isPC: true })
      const out = fix('<img src="assets/a.png" alt="a">')
      // Relative ref resolves against the markdown dir → token-scoped local endpoint.
      expect(out).toContain('src="/api/share/tokabc/local/docs/assets/a.png?t=42"')
      expect(out).not.toContain('/api/file/thumb')
      expect(out).not.toContain('/api/local-file/')
    } finally {
      setShareToken(null)
    }
  })

  it('resolves shared docs with an absolute dir via ?path= (cross-dir images)', () => {
    setShareToken('tokabs')
    try {
      // Share SPA always renders with an absolute file path, e.g.
      // /home/user/proj/test/markdown/images-demo.md referencing ../images/…
      const fix = createFixLocalImagePaths({ baseDir: '/home/user/proj/test/markdown', imageTimestamp: 7, isPC: true })
      const out = fix('<img src="../images/pic.jpg" alt="p">')
      expect(out).toContain('src="/api/share/tokabs/local?path=%2Fhome%2Fuser%2Fproj%2Ftest%2Fmarkdown%2F..%2Fimages%2Fpic.jpg&t=7"')
      expect(out).not.toContain('/local-file')
      expect(out).not.toContain('/file/thumb')
    } finally {
      setShareToken(null)
    }
  })

  it('keeps external URLs untouched in share mode with absolute dirs', () => {
    setShareToken('tokabs')
    try {
      const fix = createFixLocalImagePaths({ baseDir: '/home/user/proj/test/markdown', imageTimestamp: 1, isPC: true })
      const out = fix('<img src="https://x.com/a.png">')
      expect(out).toContain('src="https://x.com/a.png"')
    } finally {
      setShareToken(null)
    }
  })
})

describe('buildMarkdownPreviewDom', () => {
  beforeEach(() => _setIsPCForTest(true))
  afterEach(() => vi.restoreAllMocks())

  it('renders headings with deduplicated ids (like markedConfig)', () => {
    const md = '# Intro\n\n# Intro\n\n## Setup'
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'README.md' }, { isPC: true, imageTimestamp: 1 })
    // ids are preserved; opening tags may carry a data-source-line attribute
    expect(html).toContain('id="intro"')
    expect(html).toContain('id="intro-2"')
    expect(html).toContain('id="setup"')
  })

  it('wraps tables and injects row attributes', () => {
    const md = '| a | b |\n|---|---|\n| 1 | 2 |'
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'README.md' }, { isPC: true, imageTimestamp: 1 })
    expect(html).toContain('class="table-wrap"')
    expect(html).toContain('data-table-idx="0"')
    expect(html).toContain('data-row-idx="0"')
  })

  it('annotates code blocks with headers (language + copy/wrap)', () => {
    const md = '```ts\nconst x: number = 1\n```'
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'README.md' }, { isPC: true, imageTimestamp: 1 })
    expect(html).toContain('code-block-wrapper')
    expect(html).toContain('code-block-header')
    expect(html).toContain('code-block-lang')
    expect(html).toContain('code-block-copy-btn')
  })

  it('renders mermaid fenced blocks as pre.mermaid', () => {
    const md = '```mermaid\ngraph TD; A-->B\n```'
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'README.md' }, { isPC: true, imageTimestamp: 1 })
    // opening tag may carry a data-source-line attribute
    expect(html).toMatch(/<pre class="mermaid"( data-source-line="\d+")?>/)
  })

  it('resolves relative image paths through fixImagePaths + lightbox wrap', () => {
    const md = '![img](img/x.png)'
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'README.md' }, { isPC: true, imageTimestamp: 5 })
    expect(html).toContain('lightbox-img-wrap')
    expect(html).toContain('/api/file/thumb?path=img/x.png&amp;w=1200')
    expect(html).toContain('data-attach-src="img/x.png"')
  })

  it('reports detected file paths for later verification', () => {
    // A relative path in text that looks like a file gets annotated.
    const md = 'Open `src/main.ts:10` for details'
    const { detectedPaths } = buildMarkdownPreviewDom(
      { content: md, path: 'README.md', projectRoot: '', homeDir: '' },
      { isPC: true, imageTimestamp: 1 }
    )
    expect(detectedPaths.length).toBeGreaterThan(0)
  })

  it('skips file-path annotation entirely in share mode', () => {
    setShareToken('tokshare')
    try {
      const md = 'Open `src/main.ts:10` for details'
      const { html, detectedPaths } = buildMarkdownPreviewDom(
        { content: md, path: 'README.md', projectRoot: '', homeDir: '' },
        { isPC: true, imageTimestamp: 1 }
      )
      expect(detectedPaths).toHaveLength(0)
      expect(html).not.toContain('chat-file-open-btn')
      expect(html).not.toContain('data-file-path')
    } finally {
      setShareToken(null)
    }
  })

  it('rewrites relative images to the token endpoint in share mode', () => {
    setShareToken('tokshare')
    try {
      const md = '![img](img/x.png)'
      const { html } = buildMarkdownPreviewDom({ content: md, path: 'README.md' }, { isPC: true, imageTimestamp: 5 })
      expect(html).toContain('src="/api/share/tokshare/local/img/x.png?t=5"')
      expect(html).not.toContain('/api/file/thumb')
    } finally {
      setShareToken(null)
    }
  })
})

describe('data-source-line through the full markdown preview pipeline', () => {
  it('annotates block elements with their 1-based source lines', () => {
    const md = [
      '# 标题', '',
      '第一段。', '',
      '- 甲', '- 乙', '',
      '| a | b |', '|---|---|', '| 1 | 2 |', '',
      '```js', 'const x = 1', '```', '',
      '尾部',
    ].join('\n')
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'x.md' }, { isPC: true, imageTimestamp: 1 })
    expect(html).toContain('data-source-line="1"')
    expect(html).toContain('<p data-source-line="3">')
    expect(html).toContain('<ul data-source-line="5">')
    expect(html).toContain('table data-source-line="8"')
    expect(html).toContain('<pre data-source-line="12">')
    // table stays wrapped in .table-wrap despite carrying the attribute
    expect(html).toMatch(/table-wrap"><table data-source-line="8"/)
  })

  it('preserves line anchors for content that protectMarkdown rewrites (math, code)', () => {
    // fenced code and math are protected then restored with the same row count.
    const md = ['第一行', '', '```', '$x_i$', '```', '', '公式 $a_{i}$ 结尾'].join('\n')
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'm.md' }, { isPC: true, imageTimestamp: 1 })
    // paragraph 1 at line 1, code block starts line 3, math paragraph at line 7
    expect(html).toContain('<p data-source-line="1">')
    expect(html).toContain('<pre data-source-line="3">')
    expect(html).toContain('data-source-line="7"')
    expect(html).not.toContain('\x00')
    expect(html).not.toContain('MATH')
  })

  it('keeps line numbers correct after MULTI-LINE display math (row-count preservation)', () => {
    // 8 source lines; the $$..$$ block spans lines 3-6.
    const md = ['标题', '', '$$', 'a=b', 'c=d', '$$', '', '结尾段落'].join('\n')
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'm.md' }, { isPC: true, imageTimestamp: 1 })
    expect(html).toContain('<p data-source-line="1">')
    // The block after the formula must be line 8 (not shifted by the 3 rows
    // the placeholder would otherwise have collapsed).
    expect(html).toContain('data-source-line="8"')
    expect(html).not.toContain('\x00')
  })

  it('keeps line numbers correct after multi-line \\[...\\] display math', () => {
    const md = ['a', '', '\\[', 'x=y', '\\]', '', 'b'].join('\n')
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'm.md' }, { isPC: true, imageTimestamp: 1 })
    expect(html).toContain('data-source-line="1"')
    expect(html).toContain('data-source-line="7"')
    expect(html).not.toContain('\x00')
  })

  it('keeps line numbers aligned with the file when the source starts with blank lines', () => {
    // 2 leading blank lines: a real file line 3 is the first heading.
    const md = ['', '', '# 标题', '', '第二行内容'].join('\n')
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'lead.md' }, { isPC: true, imageTimestamp: 1 })
    expect(html).toContain('<h1 id="标题" data-source-line="3">')
    expect(html).toContain('<p data-source-line="5">')
  })

  it('keeps correct lines for fenced code that contains math (math inside code is not folded)', () => {
    // The $$ inside a fenced code block is code text — protectMarkdown leaves it
    // in the restored multi-line code, so following lines must not shift.
    const md = ['a', '', '```', '$$x=y$$', 'b', '```', '', 'c'].join('\n')
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'c.md' }, { isPC: true, imageTimestamp: 1 })
    expect(html).toContain('<p data-source-line="1">')
    expect(html).toContain('<pre data-source-line="3">')
    // c is on file line 8 (paragraph 1, blank 2, code 3-7, blank, c)
    expect(html).toContain('data-source-line="8"')
  })

  it('keeps correct lines for display math directly after a heading', () => {
    const md = ['## H', '', '$$', 'x', 'y', '$$', '', 'z'].join('\n')
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'd.md' }, { isPC: true, imageTimestamp: 1 })
    expect(html).toContain('<h2 id="h" data-source-line="1">')
    // z is on file line 8.
    expect(html).toContain('data-source-line="8"')
    expect(html).not.toContain('\x00')
  })

  it('skipKatex streaming keeps line anchors and leaks no NULs on multi-line display math', () => {
    const md = ['a', '', '$$', 'x', 'y', '$$', '', 'b'].join('\n')
    const html = renderMarkdownHtml(md, { skipKatex: true })
    // Multi-line formula is restored to escaped source with row padding intact.
    expect(html).not.toContain('\x00')
    expect(html).not.toContain('MATH')
    // The block after the formula is on file line 8 ($$..$$ spans lines 3-6).
    expect(html).toContain('data-source-line="8"')
  })
})
