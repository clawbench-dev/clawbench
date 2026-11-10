import { describe, it, expect } from 'vitest'
import { isLocalMediaSrc, parkUnresolvedImagesIn, UNRESOLVED_SRC_ATTR } from '@/utils/mediaSrc'

/**
 * `isLocalMediaSrc` is the ONE classifier shared by the turn-end rewrite
 * (`resolveMediaSrc` in chatRenderUtils) and the streaming render's decision to
 * park a destination instead of requesting it. These cases pin the boundary:
 * a src that is wrong on either side costs either a guaranteed 404 per chunk or
 * an image that stays invisible for the whole turn.
 */
describe('isLocalMediaSrc', () => {
  it('treats a project-relative or absolute filesystem path as local', () => {
    expect(isLocalMediaSrc('docs/chart.png')).toBe(true)
    expect(isLocalMediaSrc('./chart.png')).toBe(true)
    expect(isLocalMediaSrc('/tmp/chart.png')).toBe(true)
    expect(isLocalMediaSrc('/proj/assets/a.png')).toBe(true)
  })

  it('leaves every src the browser can fetch on its own alone', () => {
    expect(isLocalMediaSrc('https://example.com/a.png')).toBe(false)
    expect(isLocalMediaSrc('http://example.com/a.png')).toBe(false)
    // Protocol-relative: inherits the page's scheme.
    expect(isLocalMediaSrc('//cdn.example.com/a.png')).toBe(false)
    // Inline bytes — no file behind them.
    expect(isLocalMediaSrc('data:image/png;base64,AAAA')).toBe(false)
    // A blob URL has no path to rewrite; treating it as relative used to
    // produce /api/fs/raw/blob:… which cannot exist.
    expect(isLocalMediaSrc('blob:https://example.com/abcd')).toBe(false)
    // Already served by our own endpoint (a re-render sees this form).
    expect(isLocalMediaSrc('/api/fs/raw/docs/a.png')).toBe(false)
    expect(isLocalMediaSrc('/api/fs/thumb?target=docs/a.png')).toBe(false)
  })

  it('does not classify an empty src as local', () => {
    expect(isLocalMediaSrc('')).toBe(false)
  })
})

describe('parkUnresolvedImagesIn', () => {
  const run = (html: string) => {
    const doc = new DOMParser().parseFromString(html, 'text/html')
    parkUnresolvedImagesIn(doc)
    return doc
  }

  it('moves a local destination off src, so no request can start', () => {
    // `src` is what makes the browser fetch; a src-less <img> never does. The
    // value is kept so the placeholder can still name the file.
    const doc = run('<p><img src="docs/chart.png" alt="图"></p>')
    const img = doc.querySelector('img')!
    expect(img.hasAttribute('src')).toBe(false)
    expect(img.getAttribute(UNRESOLVED_SRC_ATTR)).toBe('docs/chart.png')
    expect(img.getAttribute('alt')).toBe('图')
  })

  it('leaves images the browser can actually load untouched', () => {
    const doc = run(
      '<p><img src="https://example.com/a.png"><img src="data:image/png;base64,AAAA">'
      + '<img src="/api/fs/raw/docs/a.png"></p>',
    )
    const [remote, inline, served] = Array.from(doc.querySelectorAll('img'))
    expect(remote.getAttribute('src')).toBe('https://example.com/a.png')
    expect(inline.getAttribute('src')).toContain('data:image/png')
    expect(served.getAttribute('src')).toBe('/api/fs/raw/docs/a.png')
    expect(remote.hasAttribute(UNRESOLVED_SRC_ATTR)).toBe(false)
  })
})
