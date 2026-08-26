import { describe, expect, it, beforeEach } from 'vitest'
import { renderMarkdown } from '@/composables/useMarkdownRenderer.ts'
import { store } from '@/stores/app.ts'
import { _setIsPCForTest, _resetPlatformForTest } from '@/composables/usePlatformDetect.ts'

/**
 * Regression tests for relative markdown image handling.
 *
 * Root cause fixed: DOMPurify's custom ALLOWED_URI_REGEXP used to strip the
 * `src` of RELATIVE image paths containing a slash (e.g. `img/logo.png`),
 * leaving `<img alt="a">` with no src. That broke thumbnails AND the lightbox
 * (img.src was empty) for chat, markdown preview and the table-row modal.
 *
 * The regex now matches a full relative path, so relative images survive
 * sanitize and then get thumbnail compression + lightbox classes from the
 * chat/markdown image rewrite step.
 */
describe('renderMarkdown relative image handling', () => {
  beforeEach(() => {
    store.state.projectRoot = '/proj'
    store.state.homeDir = '/home'
    _resetPlatformForTest()
    _setIsPCForTest(true)
  })

  it('keeps src and applies thumbnail + lightbox to a relative image', () => {
    const r = renderMarkdown('![a](img/logo.png)', {})
    expect(r.html).toContain('src="/api/file/thumb?path=img/logo.png&amp;w=1200"')
    expect(r.html).toContain('data-full-src="/api/local-file/img/logo.png"')
    expect(r.html).toContain('class="chat-img lightbox-img"')
    expect(r.html).toContain('lightbox-expand-icon')
  })

  it('enhances relative images inside a markdown table cell (table-row modal source)', () => {
    const r = renderMarkdown('| 列 |\n|---|\n| ![a](img/logo.png) |', {})
    expect(r.html).toContain('src="/api/file/thumb?path=img/logo.png&amp;w=1200"')
    expect(r.html).toContain('data-full-src="/api/local-file/img/logo.png"')
    expect(r.html).toContain('class="chat-img lightbox-img"')
  })

  it('keeps src for a bare relative filename', () => {
    const r = renderMarkdown('![a](logo.png)', {})
    expect(r.html).toContain('src="/api/file/thumb?path=logo.png&amp;w=1200"')
    expect(r.html).toContain('data-full-src="/api/local-file/logo.png"')
  })

  it('uses mobile thumbnail width when device is not PC', () => {
    _setIsPCForTest(false)
    const r = renderMarkdown('![a](img/logo.png)', {})
    expect(r.html).toContain('src="/api/file/thumb?path=img/logo.png&amp;w=640"')
    expect(r.html).toContain('data-full-src="/api/local-file/img/logo.png"')
  })

  it('does not rewrite external http(s) images but keeps src + lightbox styling', () => {
    const r = renderMarkdown('![a](https://example.com/img.png)', {})
    expect(r.html).toContain('src="https://example.com/img.png"')
    expect(r.html).toContain('class="chat-img lightbox-img"')
    expect(r.html).not.toContain('/api/file/thumb')
  })

  it('still strips dangerous javascript: src at sanitize (XSS preserved)', () => {
    // markdown won't emit a javascript: src, but a raw HTML image must be stripped
    const r = renderMarkdown('<img src="javascript:alert(1)" alt="x">', {})
    expect(r.html).not.toContain('javascript:')
  })

  it('wraps inline svg in lightbox wrapper (full pipeline)', () => {
    const r = renderMarkdown('<svg viewBox="0 0 10 10"><rect></rect></svg>', {})
    expect(r.html).toContain('class="lightbox-svg-wrap"')
    expect(r.html).toContain('class="lightbox-svg"')
    expect(r.html).toContain('class="lightbox-expand-icon"')
  })

  it('keeps file-path annotation intact and does not wrap the UI button svg (B1 regression)', () => {
    // wrapInlineSvgs must run AFTER annotation steps, so paths inside an
    // inline svg are still annotated. The annotation button's lucide icon
    // svg must NOT be wrapped in a lightbox wrapper.
    const r = renderMarkdown('<svg viewBox="0 0 10 10"><text>src/main.go</text></svg>', {})
    expect(r.html).toContain('chat-file-path')
    expect(r.html).toContain('data-file-path="src/main.go"')
    // Exactly one lightbox-svg-wrap: the content svg, not the button icon
    expect(r.html.match(/class="lightbox-svg-wrap"/g)).toHaveLength(1)
    // The button keeps its raw svg child (no wrapper span injected inside)
    expect(r.html).toMatch(/<button class="chat-file-open-btn"[\s\S]*?<svg viewBox="0 0 24 24"[\s\S]*?<\/svg><\/button>/)
  })

  it('wraps nested inline svg through full pipeline without corruption', () => {
    const r = renderMarkdown('<svg viewBox="0 0 10 10"><g><svg viewBox="0 0 5 5"><rect></rect></svg></g></svg>', {})
    expect(r.html).toContain('lightbox-svg-wrap')
    // Inner svg must still be balanced (no mangled tags)
    expect(r.html).toMatch(/<svg viewBox="0 0 5 5"[\s\S]*?<\/svg>/)
  })
})
