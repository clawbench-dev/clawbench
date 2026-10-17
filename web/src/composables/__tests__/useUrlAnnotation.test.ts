import { describe, expect, it, vi } from 'vitest'
import { annotateBareUrls, annotateBareUrlsIn } from '@/composables/useUrlAnnotation'

vi.mock('@/composables/useAppMode.ts', () => ({ useAppMode: () => ({ isAppMode: { value: false } }) }))

describe('annotateBareUrls', () => {
    it('wraps a schemeless domain with a path', () => {
        const out = annotateBareUrls('<p>见 github.com/foo/bar 仓库</p>')
        expect(out).toContain('href="https://github.com/foo/bar"')
        expect(out).toContain('>github.com/foo/bar</a>')
        // the surrounding prose is untouched
        expect(out).toContain('见 ')
        expect(out).toContain(' 仓库')
    })

    it('wraps a deep path', () => {
        const out = annotateBareUrls('<p>raw.githubusercontent.com/o/r/master/README.md</p>')
        expect(out).toContain('href="https://raw.githubusercontent.com/o/r/master/README.md"')
    })

    it('does NOT wrap a filename with no path (build.sh)', () => {
        // sh is a TLD, but there is no path segment — this is a real script name
        // in this repo and must stay a plain file reference.
        expect(annotateBareUrls('<p>跑 build.sh 脚本</p>')).not.toContain('<a ')
    })

    it('does NOT wrap other name.ext identifiers', () => {
        for (const s of ['cfg.FR', 'useSessionIdentity.app', 'quoteBar.com', 'internal/ai/factory_test.go']) {
            expect(annotateBareUrls(`<p>${s}</p>`)).not.toContain('<a ')
        }
    })

    it('does NOT touch text already inside an <a>', () => {
        const html = '<p><a href="https://x.com">github.com/foo/bar</a></p>'
        expect(annotateBareUrls(html)).toBe(html)
    })

    it('does NOT start a match mid-token', () => {
        // Preceded by a path separator or an '@' — this is the tail of a longer
        // token (a path, or an email address), not a bare URL.
        expect(annotateBareUrls('<p>src/github.com/foo/bar</p>')).not.toContain('<a ')
        expect(annotateBareUrls('<p>user@github.com/foo/bar</p>')).not.toContain('<a ')
    })

    it('handles several URLs in one paragraph', () => {
        const out = annotateBareUrls('<p>a github.com/x/y b example.com/p/q c</p>')
        expect(out.match(/<a /g)).toHaveLength(2)
        expect(out).toContain('href="https://github.com/x/y"')
        expect(out).toContain('href="https://example.com/p/q"')
    })

    it('annotates a bare URL inside <code> (matches the localhost annotator)', () => {
        const out = annotateBareUrls('<p><code>github.com/foo/bar</code></p>')
        expect(out).toContain('href="https://github.com/foo/bar"')
    })

    it('is idempotent', () => {
        const once = annotateBareUrls('<p>见 github.com/foo/bar 仓库</p>')
        expect(annotateBareUrls(once)).toBe(once)
    })

    it('returns empty input unchanged', () => {
        expect(annotateBareUrls('')).toBe('')
    })

    it('adds target/rel in web mode', () => {
        const out = annotateBareUrls('<p>github.com/foo/bar</p>')
        expect(out).toContain('target="_blank"')
        expect(out).toContain('rel="noopener noreferrer"')
    })

    it('reports whether anything was annotated', () => {
        const doc = new DOMParser().parseFromString('<p>github.com/foo/bar</p>', 'text/html')
        expect(annotateBareUrlsIn(doc)).toBe(true)
        const clean = new DOMParser().parseFromString('<p>nothing here</p>', 'text/html')
        expect(annotateBareUrlsIn(clean)).toBe(false)
    })
})

describe('bare URL boundaries', () => {
    it('does not swallow a sentence-final period', () => {
        // `.` is a legal path character (README.md, v1.2) AND the sentence
        // terminator, so it is trimmed only when it ends the match.
        const out = annotateBareUrls('<p>See github.com/foo/bar.</p>')
        expect(out).toContain('href="https://github.com/foo/bar"')
        expect(out).toContain('>github.com/foo/bar</a>.')
    })

    it('keeps a period that is part of the path', () => {
        const out = annotateBareUrls('<p>See github.com/o/r/master/README.md</p>')
        expect(out).toContain('href="https://github.com/o/r/master/README.md"')
    })

    it('stops at other terminators without trimming', () => {
        for (const [input, expected] of [
            ['See github.com/foo/bar, and more', 'href="https://github.com/foo/bar"'],
            ['See (github.com/foo/bar)', 'href="https://github.com/foo/bar"'],
            ['See github.com/foo/bar。', 'href="https://github.com/foo/bar"'],
        ] as const) {
            expect(annotateBareUrls(`<p>${input}</p>`), input).toContain(expected)
        }
    })

    it('does not linkify host:port', () => {
        // The path group requires a `/`, so `example.com:8080/api` cannot match
        // — a colon is not a path separator.
        expect(annotateBareUrls('<p>example.com:8080/api</p>')).not.toContain('<a ')
    })

    it('leaves a URL already inside an anchor alone', () => {
        const html = '<p><a href="https://x.com">github.com/a/b</a></p>'
        expect(annotateBareUrls(html)).toBe(html)
    })

    it('links every occurrence in one text node', () => {
        const out = annotateBareUrls('<p>a github.com/x/y b github.com/p/q c</p>')
        expect(out.match(/<a /g)).toHaveLength(2)
    })

    it('does not join a URL split across elements', () => {
        // Text nodes are processed independently; `github.com/` + `<b>a/b</b>`
        // is not one URL and must not be linkified.
        expect(annotateBareUrls('<p>github.com/<b>a/b</b></p>')).not.toContain('<a ')
    })

    it('does not let a quote in the text escape the href attribute', () => {
        // `textContent` + `setAttribute` are used, so markup in the source text
        // stays text. The quote must land OUTSIDE the generated anchor.
        const out = annotateBareUrls('<p>github.com/a/b&quot;onmouseover=&quot;x</p>')
        const anchor = /<a [^>]*>.*?<\/a>/.exec(out)?.[0] ?? ''
        expect(anchor).toBe(
            '<a href="https://github.com/a/b" target="_blank" rel="noopener noreferrer">github.com/a/b</a>',
        )
        expect(out.endsWith('"onmouseover="x</p>')).toBe(true)
    })
})
