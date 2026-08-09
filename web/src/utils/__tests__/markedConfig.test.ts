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

    /**
     * Regression: marked@4 (redoc transitive) drops <strong> when bold wraps
     * inline code and is immediately followed by CJK punctuation.
     * Must keep marked@18+ so chat/file preview renders bold correctly.
     */
    describe('bold + inline code + CJK punctuation (marked@4 regression)', () => {
        it('keeps strong when **…`code`** is followed by fullwidth comma', () => {
            const md = '5. **媒体缓存 bust 靠 query `t`**，依赖前端记得刷新 timestamp'
            const html = marked.parse(md) as string
            expect(html).toContain('<strong>')
            expect(html).toContain('</strong>')
            expect(html).toContain('<code>t</code>')
            expect(html).toMatch(/<strong>[\s\S]*媒体缓存[\s\S]*<code>t<\/code>[\s\S]*<\/strong>/)
            // Literal asterisks must not leak into rendered HTML
            expect(html).not.toContain('**媒体')
            expect(html).not.toContain('`t`**')
        })

        it('keeps strong for **text `code`** + various CJK punctuation', () => {
            const puncts = ['，', '。', '；', '：', '）', '、', '！']
            for (const p of puncts) {
                const md = '**query `t`**' + p + 'x'
                const html = marked.parse(md) as string
                expect(html, `failed for punct ${JSON.stringify(p)}`).toContain('<strong>')
                expect(html).toContain('<code>t</code>')
                expect(html).not.toMatch(/\*\*query/)
            }
        })

        it('keeps strong in multi-item list matching real chat content', () => {
            const md = [
                '1. **扩展名表多处维护**：`app.ts`',
                "2. **绝对路径靠 `path.startsWith('/')` 判断**，Windows",
                '3. **文件操作 handler 偏厚**：`file.go`',
                '4. **`/api/files` 全量 Walk** 对大仓库危险，主路径用的是 `/api/dir`',
                '5. **媒体缓存 bust 靠 query `t`**，依赖前端记得刷新 timestamp；watcher 路径走 `refreshCurrentFile`，列表缩略图（`file_thumb.go`）是否同步要另看',
            ].join('\n')
            const html = marked.parse(md) as string
            const items = [...html.matchAll(/<li>[\s\S]*?<\/li>/g)].map((m) => m[0])
            expect(items).toHaveLength(5)
            for (let i = 0; i < 5; i++) {
                expect(items[i], `item ${i + 1} missing <strong>`).toContain('<strong>')
            }
            expect(items[4]).toMatch(/<strong>[\s\S]*媒体缓存[\s\S]*<\/strong>/)
            expect(items[4]).not.toContain('**媒体')
        })
    })
})
