import { marked, highlightCode } from '@/utils/globals.ts'
import { slugify } from '@/utils/toc.ts'
import { escapeHtml } from '@/utils/html.ts'

/**
 * Heading ID deduplication counter.
 * Reset before each render pass to ensure duplicate headings within a single
 * document get unique IDs (e.g., two "## Introduction" → id="introduction" and id="introduction-2").
 * Cross-document persistence is fine because we reset before every renderMarkdown() call.
 */
export let headingIdCounts: Record<string, number> = {}

/** Reset heading ID counter — call before each marked.parse() */
export function resetHeadingIds(): void {
    headingIdCounts = {}
}

/**
 * Configure marked's custom renderer for code blocks and headings.
 *
 * marked v18+ passes a single token object ({ text, lang }) / ({ text, depth })
 * to renderer hooks.
 *
 * Call once at app startup (from main.ts).
 */
export function configureMarkedRenderer(): void {
    marked.use({
        renderer: {
            heading(...args: unknown[]): string {
                // v18: heading({ text, depth })  |  v4: heading(text, depth)
                const token = args[0]
                const isObj = token != null && typeof token === 'object'
                const text = isObj ? (token as Record<string, unknown>).text : token
                const depth = isObj ? (token as Record<string, unknown>).depth : args[1]
                const baseId = slugify(String(text || ''))
                // Deduplicate: first occurrence keeps base ID, subsequent get -2, -3, etc.
                const count = (headingIdCounts[baseId] || 0) + 1
                headingIdCounts[baseId] = count
                const id = count > 1 ? `${baseId}-${count}` : baseId
                return `<h${depth} id="${id}">${marked.parseInline(String(text || ''))}</h${depth}>`
            },
            code(...args: unknown[]): string {
                // v18: code({ text, lang })  |  v4: code(text, lang)
                const token = args[0]
                const isObj = token != null && typeof token === 'object'
                const code = isObj ? (String((token as Record<string, unknown>).text || '')) : String(token || '')
                const lang = isObj ? (String((token as Record<string, unknown>).lang || '')) : (String(args[1] || ''))
                if (lang === 'mermaid') {
                    return '<pre class="mermaid">' + escapeHtml(code) + '</pre>'
                }
                const highlighted = highlightCode(code, lang || '')
                const langClass = lang ? ' class="language-' + lang + '"' : ''
                return '<pre><code' + langClass + '>' + highlighted + '</code></pre>'
            },
        },
    })
}
