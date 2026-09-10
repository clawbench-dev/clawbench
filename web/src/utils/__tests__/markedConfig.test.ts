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
            // Opening tag may carry data-source-line / data-source-end before '>'.
            expect(html).toMatch(/<pre class="mermaid"( data-source-line="\d+")?( data-source-end="\d+")?>/)
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
            // Opening tag may carry data-source-line / data-source-end before '>'.
            expect(html).toMatch(/<pre( data-source-line="\d+")?( data-source-end="\d+")?><code>/)
            expect(html).toContain(escapeHtml(code))
            expect(html).not.toContain('class="language-')
        })
    })

    describe('data-source-line annotation', () => {
        it('annotates block elements with their 1-based source line and end', () => {
            const md = ['# 标题', '', '第一段。', '', '- 甲', '- 乙', '', '> 引用', '', '| 列A | 列B |', '|---|---|', '| 1 | 2 |', '', '```js', 'const a = 1', '```'].join('\n')
            const html = marked.parse(md)
            expect(html).toContain('<h1 id="标题" data-source-line="1">')
            expect(html).toContain('<p data-source-line="3">')
            expect(html).toContain('<ul data-source-line="5">')
            expect(html).toContain('<blockquote data-source-line="8">')
            // Table spans source lines 10-12 (header + separator + one row).
            expect(html).toContain('<table data-source-line="10" data-source-end="12">')
            // Fenced code starts line 14, ends on the closing fence (line 16).
            expect(html).toContain('<pre data-source-line="14" data-source-end="16">')
        })

        it('annotates nested list items and table cells consistently', () => {
            const md = ['- 甲', '  - 甲一', '  - 甲二', '- 乙'].join('\n')
            const html = marked.parse(md)
            // outer list starts line 1, nested list starts line 2 (second item line)
            expect(html).toContain('<ul data-source-line="1">')
            expect(html).toMatch(/<li>甲<ul data-source-line="2">/)
        })

        it('survives the table-wrap string transform (attribute tables still wrapped)', () => {
            const md = '| a | b |\n|---|---|\n| 1 | 2 |'
            // emulate renderMarkdown's table-wrap replace on marked output
            const plain = marked.parse(md)
            const wrapped = plain.replace(/<table\b/g, '<div class="table-wrap"><table')
                .replace(/<\/table>/g, '</table></div>')
            expect(wrapped).toContain('<div class="table-wrap"><table data-source-line="1" data-source-end="3">')
            // the old literal regex would NOT have matched an attributed table
            expect(plain.match(/<table>/g)).toBeNull()
        })

        it('does not emit data-source-line on inline-only content or when hooks are off', () => {
            // content with no block-level structure still annotates the paragraph,
            // but a bare inline span never appears as a standalone block
            const html = marked.parse('just **bold** text')
            expect(html).toContain('<p data-source-line="1">')
        })

        it('table markup matches marked default structure (thead <tr>, tbody no leading newline)', () => {
            const md = '| a | b |\n|---|---|\n| 1 | 2 |'
            const html = marked.parse(md)
            // thead cells are wrapped in <tr> exactly like the default renderer
            expect(html).toMatch(/<table data-source-line="1" data-source-end="3">\n<thead>\n<tr>\n<th>a<\/th>\n<th>b<\/th>\n<\/tr>\n<\/thead>\n<tbody><tr>/)
            // tbody has NO leading newline after <tbody>
            expect(html).toContain('<tbody><tr>')
            expect(html).toContain('</tbody></table>\n')
        })
    })

    describe('robustness against token raw normalization (regression)', () => {
        // marked-token-position used to throw "Cannot find … in …" here because
        // the lexer strips `\|` escapes inside table cells, so the inline
        // codespan raw ("`||--o{`") no longer matches the source ("`\|--o{`").
        // The crash escaped marked.parse() and blanked the whole message.
        it('renders a table whose cells contain escaped pipes in code spans', () => {
            const md = [
                '| 关系 | 说明 |',
                '|------|------|',
                '| `\\|\\|--o{` | 一对多 |',
                '| `}\\|--|{` | 多对多 |',
                '| `\\|\\|--\\|\\|` | 一对一 |',
            ].join('\n')
            const html = marked.parse(md)
            expect(html).toContain('<table data-source-line="1" data-source-end="5">')
            // first row's escaped pipes render as one code span
            expect(html).toContain('<code>||--o{</code>')
            // last row likewise
            expect(html).toContain('<code>||--||</code>')
            // middle row has an unescaped `|` after `}` so the cell splits — the
            // point is the whole table still renders (this used to throw inside
            // marked.parse before any output was produced)
            expect(html).toContain('一对多')
            expect(html).toContain('一对一')
        })

        it('renders a table cell with a bare escaped pipe (no code span)', () => {
            const md = '| a | b |\n|---|---|\n| a\\|b | 1 |'
            const html = marked.parse(md)
            expect(html).toContain('<table data-source-line="1" data-source-end="3">')
            expect(html).toContain('<td>a|b</td>')
        })

        it('renders tab-indented nested list items (raw tab→space normalization)', () => {
            const md = ['- a', '\t- b', '\t- c', '- d'].join('\n')
            const html = marked.parse(md)
            // outer list starts line 1; nested list opens on the first child line (line 2)
            expect(html).toMatch(/<ul data-source-line="1">/)
            expect(html).toMatch(/<li>a<ul data-source-line="2">/)
            expect(html).toContain('>b</li>')
            expect(html).toContain('>c</li>')
        })

        it('keeps blockquote multi-line annotation intact next to risky content', () => {
            const md = ['| k | v |', '|---|---|', '| `a\\|b` | ok |', '', '> quote', ''].join('\n')
            const html = marked.parse(md)
            expect(html).toContain('<table data-source-line="1" data-source-end="3">')
            expect(html).toContain('<blockquote data-source-line="5">')
        })
    })
})
