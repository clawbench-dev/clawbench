import { describe, expect, it } from 'vitest'
import { resolveChunkImages } from '@/utils/kbChunkMedia.ts'

/**
 * A passage keeps the ORIGINAL relative srcs of the document it was parsed
 * from, so they have to be resolved against the SOURCE FILE's directory. The
 * `data-attach-src` stamp matters as much as the URL: the media-block factory
 * reads it as "this is a local file" and that is what puts the attach /
 * open-file buttons on the figure's toolbar.
 */

const opts = { isWideScreen: true, stamp: 1234 }

describe('resolveChunkImages', () => {
  it('resolves a relative src against the source file directory', () => {
    const out = resolveChunkImages('<p><img src="images/a.png"></p>', '/docs/guide/intro.md', opts)
    expect(out).toContain(`target=${encodeURIComponent('/docs/guide/images/a.png')}`)
  })

  it('serves raster formats through the thumbnail endpoint and keeps the full URL', () => {
    const out = resolveChunkImages('<img src="a.png">', '/docs/intro.md', opts)
    expect(out).toContain('/api/fs/thumb?target=')
    expect(out).toContain('data-full-src="/api/fs/raw/')
    expect(out).toContain('w=1200')
  })

  it('does not send SVG through the raster thumbnail endpoint', () => {
    const out = resolveChunkImages('<img src="d.svg">', '/docs/intro.md', opts)
    expect(out).toContain('/api/fs/raw/?target=')
  })

  it('narrows the thumbnail on a phone-sized viewport', () => {
    const out = resolveChunkImages('<img src="a.jpg">', '/docs/x.md', { isWideScreen: false, stamp: 1 })
    expect(out).toContain('w=640')
  })

  it('marks every image as content media for the shared placeholder', () => {
    const out = resolveChunkImages('<img src="a.png">', '/docs/x.md', opts)
    expect(out).toContain('class="lightbox-img')
    expect(out).toContain('data-attach-src=')
  })

  it('keeps classes the renderer already put on the image', () => {
    const out = resolveChunkImages('<img class="foo" src="a.png">', '/docs/x.md', opts)
    expect(out).toMatch(/class="lightbox-img foo"/)
  })

  it('never rewrites remote and data srcs', () => {
    const remote = resolveChunkImages('<img src="https://example.com/a.png">', '/docs/x.md', opts)
    expect(remote).toContain('src="https://example.com/a.png"')
    expect(remote).not.toContain('/api/fs/')
    const inline = resolveChunkImages('<img src="data:image/png;base64,AAAA">', '/docs/x.md', opts)
    expect(inline).toContain('src="data:image/png;base64,AAAA"')
    expect(inline).not.toContain('/api/fs/')
  })

  it('resolves an already-encoded CJK file name', () => {
    const out = resolveChunkImages('<img src="%E5%9B%BE.png">', '/docs/x.md', opts)
    expect(out).toContain(encodeURIComponent('/docs/图.png'))
  })

  it('resolves an absolute src as-is', () => {
    const out = resolveChunkImages('<img src="/tmp/a.png">', '/docs/x.md', opts)
    expect(out).toContain(encodeURIComponent('/tmp/a.png'))
  })

  it('keeps `..` for the server to collapse, so an outside path stays outside', () => {
    const out = resolveChunkImages('<img src="../../shared/a.png">', '/docs/guide/x.md', opts)
    expect(out).toContain(encodeURIComponent('/docs/guide/../../shared/a.png'))
  })

  it('returns HTML untouched when there is no image', () => {
    expect(resolveChunkImages('<p>text</p>', '/docs/x.md', opts)).toBe('<p>text</p>')
  })
})
