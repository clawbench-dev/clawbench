import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { configureMarkedRenderer } from '@/utils/markedConfig'
import { _setIsPCForTest } from '@/composables/usePlatformDetect'
import {
  buildMarkdownPreviewDom,
  createFixLocalImagePaths,
} from '@/composables/useMarkdownRenderPipeline'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { setShareToken } from '@/share/shareMode'
import { escapeHtml } from '@/utils/html'

configureMarkedRenderer()

describe('createFixLocalImagePaths', () => {
  it('resolves relative image srcs against the markdown file dir', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 42, isPC: true })
    const html = '<p><img src="assets/a.png" alt="a"></p>'
    const out = fix(html)
    expect(out).toContain('src="/api/file/thumb?path=docs/assets/a.png&amp;w=1200"')
    expect(out).toContain('data-full-src="/api/local-file/docs/assets/a.png?t=42"')
    // data-attach-src carries the resolved project-relative path for re-drag
    expect(out).toContain('data-attach-src="docs/assets/a.png"')
    // Local image is lifted into an image block with a header bar (view /
    // attach / open buttons) — uniform across mobile and PC.
    expect(out).toContain('image-block-wrapper')
    expect(out).toContain('image-block-header')
    expect(out).toContain('image-block-view-btn')
    expect(out).toContain('image-block-attach-btn')
    expect(out).toContain('image-block-open-btn')
  })

  it('keeps remote and embedded URLs untouched', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    for (const src of ['https://x.com/a.png', '//cdn.x.com/a.png', 'data:image/png;base64,abc']) {
      const out = fix(`<img src="${src}">`)
      expect(out).toContain(`src="${src}"`)
      expect(out).not.toContain('/api/')
      // External / data: images have no local file to re-drag — no attach data
      expect(out).not.toContain('data-attach-src')
      // Still lifted into a block wrapper, but only the view button applies.
      expect(out).toContain('image-block-wrapper')
      expect(out).not.toContain('image-block-attach-btn')
    }
  })

  it('serves an absolute filesystem path instead of leaving it to 404 at the site root', () => {
    // An absolute src is a real path (the AI writing `![](/tmp/chart.png)`),
    // not a site-root URL. Left untouched, the browser would request it from
    // the site root and get a 404 — the reported "external image doesn't show"
    // bug. It must be served through the absolute forms of the local endpoints,
    // and carries no attach data (the attach flow speaks project-relative paths).
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    const out = fix('<img src="/abs/a.png">')
    expect(out).toContain('data-full-src="/api/local-file/?path=%2Fabs%2Fa.png&amp;t=1"')
    expect(out).toContain('path=%2Fabs%2Fa.png')
    expect(out).not.toContain('src="/abs/a.png"')
    expect(out).not.toContain('data-attach-src')
    expect(out).not.toContain('image-block-attach-btn')
  })

  it('serves a project-external absolute path without prefixing the document dir', () => {
    // baseDir must NOT be prepended to an absolute src: "docs/tmp/a.png" would
    // resolve against the project root and point at a different file.
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 3, isPC: true })
    const out = fix('<img src="/tmp/final_icon.png">')
    expect(out).toContain('?path=%2Ftmp%2Ffinal_icon.png')
    expect(out).not.toContain('docs%2Ftmp')
    expect(out).not.toContain('/api/local-file/docs')
  })

  it('does not re-wrap a src that is already served by a file endpoint', () => {
    // Markup can be rendered through this pipeline more than once (and export
    // re-renders content that may already carry served URLs). Wrapping an
    // already-served URL a second time produced
    // `?path=/api/local-file/images/a.png`, which the backend then treats as a
    // literal relative path and skips.
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    for (const src of [
      '/api/local-file/images/a.png',
      '/api/local-file/?path=%2Ftmp%2Fa.png',
      '/api/file/thumb?path=images/a.png&w=1200',
    ]) {
      const out = fix(`<img src="${src}">`)
      // The src is preserved verbatim (only "&" is HTML-escaped in the
      // attribute), and nothing is wrapped around it again.
      expect(out).toContain(escapeHtml(src))
      expect(out).not.toContain('data-full-src="/api/local-file/?path=%2Fapi')
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

  it('wraps every image in an image-block figure', () => {
    const fix = createFixLocalImagePaths({ baseDir: '', imageTimestamp: 1, isPC: true })
    const out = fix('<img src="x.png"><img src="https://y.com/z.png">')
    expect(out).toContain('image-block-wrapper')
    expect(out.match(/image-block-wrapper/g)).toHaveLength(2)
    // Each image keeps its lightbox-img class for lightbox/drag activation.
    expect(out.match(/lightbox-img-wrap/g)).toHaveLength(2)
  })

  it('injects attach/open buttons only for local images (data-attach-src)', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    const out = fix('<img src="a.png"><img src="https://x.com/b.png"><img src="data:image/png;base64,abc">')
    // Local raster → thumbnail; only its figure has attach + open buttons.
    expect(out.match(/image-block-attach-btn/g)).toHaveLength(1)
    expect(out.match(/image-block-open-btn/g)).toHaveLength(1)
    // External / data: images get only the view button.
    expect(out.match(/image-block-view-btn/g)).toHaveLength(3)
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
      // Share has no chat / local file actions — the block header carries only
      // the lightbox view button (no attach / open).
      expect(out).toContain('image-block-wrapper')
      expect(out).toContain('image-block-header')
      expect(out).toContain('image-block-view-btn')
      expect(out).not.toContain('image-block-attach-btn')
      expect(out).not.toContain('image-block-open-btn')
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
      expect(out).toContain('src="/api/share/tokabs/local?path=%2Fhome%2Fuser%2Fproj%2Ftest%2Fmarkdown%2F..%2Fimages%2Fpic.jpg&amp;t=7"')
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

  it('promotes a solo-paragraph image out of its <p> into a block figure', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    const out = fix('<p><img src="a.png" alt="a"></p>')
    // <p> removed entirely; the figure is a sibling block.
    expect(out).not.toContain('<p>')
    expect(out).toMatch(/^<div class="image-block-wrapper">/)
    expect(out).toContain('</div>')
  })

  it('splits a paragraph that has text on both sides of an image', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    const out = fix('<p>before <img src="a.png"> after</p>')
    // Leading text stays in the first <p>, trailing text becomes its own <p>.
    expect(out).toContain('<p>before </p>')
    expect(out).toContain('<p> after</p>')
    const figureStart = out.indexOf('image-block-wrapper')
    const leadEnd = out.indexOf('</p>')
    const trailStart = out.lastIndexOf('<p>')
    expect(figureStart).toBeGreaterThan(leadEnd)
    expect(trailStart).toBeGreaterThan(figureStart)
  })

  it('blockifies an image nested inside a table cell', () => {
    const fix = createFixLocalImagePaths({ baseDir: 'docs', imageTimestamp: 1, isPC: true })
    const out = fix('<table><tr><td><img src="a.png"></td></tr></table>')
    expect(out).toContain('<td><div class="image-block-wrapper">')
    expect(out).toContain('image-block-header')
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
    // opening tag may carry a data-source-line / data-source-end attribute
    expect(html).toMatch(/<pre class="mermaid"( data-source-line="\d+")?( data-source-end="\d+")?>/)
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
    expect(html).toContain('<pre data-source-line="12" data-source-end="14">')
    // table stays wrapped in .table-wrap despite carrying the attribute
    expect(html).toMatch(/table-wrap"><table data-source-line="8"/)
  })

  it('preserves line anchors for content that protectMarkdown rewrites (math, code)', () => {
    // fenced code and math are protected then restored with the same row count.
    const md = ['第一行', '', '```', '$x_i$', '```', '', '公式 $a_{i}$ 结尾'].join('\n')
    const { html } = buildMarkdownPreviewDom({ content: md, path: 'm.md' }, { isPC: true, imageTimestamp: 1 })
    // paragraph 1 at line 1, code block starts line 3 (ends line 5), math paragraph at line 7
    expect(html).toContain('<p data-source-line="1">')
    expect(html).toContain('<pre data-source-line="3" data-source-end="5">')
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
    expect(html).toContain('<pre data-source-line="3" data-source-end="6">')
    // c is on file line 8 (paragraph 1, blank 2, code 3-6, blank, c)
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

// --- Issue #473: KaTeX typography SVGs must not be lifted into media blocks ---

describe('KaTeX stretchy-delimiter svgs are not hijacked as media (issue #473)', () => {
  // KaTeX draws \underbrace / \overbrace / \sqrt / \xrightarrow / \vec with its
  // own internal <svg> glyph fragments nested under `.katex`. Before the fix,
  // markInlineSvgs marked them `lightbox-svg` and annotateMediaBlocks lifted
  // each into a block-level `.image-block-wrapper`, which tore the formula
  // apart and blew the message width out (measured 6012px in a 400px viewport).
  const cases: Array<[string, string]> = [
    ['\\underbrace', '$$\\underbrace{a}_{b}$$'],
    ['\\overbrace', '$$\\overbrace{a}^{b}$$'],
    ['\\sqrt', '$$\\sqrt{x}$$'],
    ['\\xrightarrow', '$$a \\xrightarrow{b} c$$'],
    ['\\vec', '$$\\vec{x}$$'],
  ]

  for (const [name, md] of cases) {
    it(`does not wrap the internal svg of ${name} in a media figure`, () => {
      const html = renderMarkdownHtml(md)
      expect(html).not.toContain('image-block-wrapper')
      expect(html).not.toContain('lightbox-svg-wrap')
      // The formula still rendered (KaTeX markup present, no error fallback).
      expect(html).toContain('class="katex"')
      expect(html).not.toContain('katex-error')
    })
  }

  it('keeps every svg of a dense multi-formula reply inside its formula', () => {
    // The reported reply: two \underbrace in one display formula, plus inline
    // math and bare-underscore notation on surrounding lines.
    const md = [
      '所有方法的原始梯度，最后都能整理成这个形状：',
      '',
      '$$\\sum_t \\underbrace{GC(q,o,t)}_{\\text{标量}} \\cdot \\underbrace{\\nabla_\\theta \\log \\pi_\\theta(o_t|\\cdot)}_{\\text{求导结果}}$$',
      '',
      '- SFT/RFT/DPO 的数据都从 π_sft 来',
      '- **PPO 的 $A_t$**：唯一**逐 token 不同**的 GC',
      '- **GRPO 的 $\\hat A_{i,t} + \\beta(\\frac{\\pi_{ref}}{\\pi_\\theta}-1)$**：KL 以梯度系数出现',
    ].join('\n')
    const html = renderMarkdownHtml(md)

    // No formula fragment was promoted to a content media block.
    expect(html).not.toContain('image-block-wrapper')
    expect(html).not.toContain('lightbox-svg-wrap')
    // All three formulas rendered (one display + two inline).
    expect(html.match(/class="katex"/g)).toHaveLength(3)
    expect(html).not.toContain('katex-error')
    // The stretchy brace glyphs survive inside the display formula.
    expect(html).toContain('class="stretchy"')
    // Bare underscores were never emphasis to begin with (CommonMark intraword
    // rule) — regression guard so a future "fix" cannot introduce <em> here.
    expect(html).not.toContain('<em>')
    expect(html).toContain('π_sft')
  })

  it('still lifts a genuine inline content svg returned by the AI', () => {
    // The KaTeX guard must not become a blanket svg veto.
    const md = '<svg viewBox="0 0 10 10" width="10" height="10"><rect width="10" height="10"></rect></svg>'
    const html = renderMarkdownHtml(md)
    expect(html).toContain('image-block-wrapper')
    expect(html).toContain('lightbox-svg-wrap')
  })
})
