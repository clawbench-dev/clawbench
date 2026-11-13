import { describe, expect, it } from 'vitest'
import { resolveMarkdownImageSrc } from './markdownImageSrc'

describe('resolveMarkdownImageSrc', () => {
    it('leaves remote, data and already-served URLs untouched', () => {
        expect(resolveMarkdownImageSrc('https://example.com/a.png', 'docs/x.md')).toBe('https://example.com/a.png')
        expect(resolveMarkdownImageSrc('//cdn.example.com/a.png', 'docs/x.md')).toBe('//cdn.example.com/a.png')
        expect(resolveMarkdownImageSrc('data:image/png;base64,AAA', 'docs/x.md')).toBe('data:image/png;base64,AAA')
        expect(resolveMarkdownImageSrc('/api/fs/raw/a.png', 'docs/x.md')).toBe('/api/fs/raw/a.png')
    })

    it('resolves a relative path against the markdown file directory', () => {
        const url = resolveMarkdownImageSrc('screenshots-mobile/a.png', 'docs/user-guide/app.md')
        expect(url).toMatch(/^\/api\/fs\/raw\/docs\/user-guide\/screenshots-mobile\/a\.png\?t=\d+$/)
    })

    it('collapses . and .. segments', () => {
        expect(resolveMarkdownImageSrc('./img/../a.png', 'docs/x.md')).toMatch(/^\/api\/fs\/raw\/docs\/a\.png\?/)
        expect(resolveMarkdownImageSrc('../shared/a.png', 'docs/sub/x.md')).toMatch(/^\/api\/fs\/raw\/docs\/shared\/a\.png\?/)
    })

    it('decodes a percent-encoded CJK filename before re-encoding', () => {
        // marked writes the encoded form; encoding it again would target
        // a file literally named `%E5%9B%BE…`.
        const url = resolveMarkdownImageSrc('%E5%9B%BE%E7%89%87/a.png', 'docs/x.md')
        expect(decodeURIComponent(url.split('?')[0])).toBe('/api/fs/raw/docs/图片/a.png')
    })

    it('routes an absolute filesystem path through the ?target= form', () => {
        const url = resolveMarkdownImageSrc('/tmp/chart.png', 'docs/x.md')
        expect(url).toMatch(/^\/api\/fs\/raw\/\?target=%2Ftmp%2Fchart\.png&t=\d+$/)
    })

    it('returns an empty src unchanged', () => {
        expect(resolveMarkdownImageSrc('', 'docs/x.md')).toBe('')
        expect(resolveMarkdownImageSrc(null, 'docs/x.md')).toBe('')
    })
})
