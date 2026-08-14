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
})
