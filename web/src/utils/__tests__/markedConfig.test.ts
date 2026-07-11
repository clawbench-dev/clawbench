import { describe, expect, it, beforeEach, beforeAll } from 'vitest'
import { headingIdCounts, resetHeadingIds, configureMarkedRenderer } from '@/utils/markedConfig.ts'
import { marked } from '@/utils/globals.ts'
import { slugify } from '@/utils/toc.ts'
import { escapeHtml } from '@/utils/html.ts'

describe('markedConfig', () => {
    beforeAll(() => {
        configureMarkedRenderer()
    })

    beforeEach(() => {
        resetHeadingIds()
    })

    describe('resetHeadingIds', () => {
        it('resets headingIdCounts to empty object', () => {
            // Populate counts by parsing a heading
            marked.parse('# Foo')
            expect(Object.keys(headingIdCounts).length).toBeGreaterThan(0)

            resetHeadingIds()
            expect(headingIdCounts).toEqual({})
        })
    })

    describe('heading rendering', () => {
        it('generates ID via slugify for a single heading', () => {
            const html = marked.parse('# Introduction')
            expect(html).toContain('id="introduction"')
            expect(html).toContain('<h1')
        })

        it('deduplicates two headings with same text', () => {
            const md = '# Intro\n## Intro'
            const html = marked.parse(md)
            expect(html).toContain('id="intro"')
            expect(html).toContain('id="intro-2"')
        })

        it('deduplicates three headings with same text', () => {
            const md = '# Intro\n## Intro\n### Intro'
            const html = marked.parse(md)
            expect(html).toContain('id="intro"')
            expect(html).toContain('id="intro-2"')
            expect(html).toContain('id="intro-3"')
        })

        it('resets counter between parses', () => {
            marked.parse('# Intro')
            // After first parse, intro count is 1
            expect(headingIdCounts['intro']).toBe(1)

            resetHeadingIds()
            const html = marked.parse('# Intro')
            // After reset, first occurrence should get base ID (not intro-2)
            expect(html).toContain('id="intro"')
            expect(html).not.toContain('id="intro-2"')
        })

        it('uses slugify for heading text', () => {
            const html = marked.parse('# Hello World')
            const expectedId = slugify('Hello World')
            expect(html).toContain(`id="${expectedId}"`)
        })
    })

    describe('code block rendering', () => {
        it('renders mermaid code block without hljs', () => {
            const code = 'graph TD; A-->B'
            const html = marked.parse('```mermaid\n' + code + '\n```')
            expect(html).toContain('<pre class="mermaid">')
            expect(html).toContain(escapeHtml(code))
            expect(html).not.toContain('hljs')
        })

        it('renders highlightable language with hljs', () => {
            const html = marked.parse('```javascript\nconsole.log("hi")\n```')
            expect(html).toContain('class="language-javascript"')
            expect(html).toContain('<code')
            // hljs highlight produces span tags
            expect(html).toContain('<span')
        })

        it('renders unknown language with escaped code and lang class', () => {
            const code = 'some unknown code'
            const html = marked.parse('```foobar\n' + code + '\n```')
            expect(html).toContain('class="language-foobar"')
            expect(html).toContain(escapeHtml(code))
        })

        it('renders code block with no language', () => {
            const code = 'plain text'
            const html = marked.parse('```\n' + code + '\n```')
            expect(html).toContain('<pre><code>')
            expect(html).toContain(escapeHtml(code))
            expect(html).not.toContain('class="language-')
        })
    })
})
