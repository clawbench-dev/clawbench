import { marked, hljs } from '@/utils/globals.ts'
import { slugify } from '@/utils/toc.ts'
import { escapeHtml } from '@/utils/html.ts'

/**
 * Configure marked's custom renderer for code blocks and headings.
 *
 * CRITICAL: marked v4 passes positional args (code, lang) / (text, depth)
 * to renderer hooks, while marked v18+ passes a single token object
 * ({ text, lang }) / ({ text, depth }). The web/ sub-project may resolve
 * a different marked version than root (due to redoc's transitive dep),
 * so the renderer must handle both APIs.
 *
 * Call once at app startup (from main.ts).
 */
export function configureMarkedRenderer(): void {
    marked.use({
        renderer: {
            heading(...args: any[]): string {
                // v18: heading({ text, depth })  |  v4: heading(text, depth)
                const token = args[0]
                const isObj = token != null && typeof token === 'object'
                const text = isObj ? token.text : token
                const depth = isObj ? token.depth : args[1]
                const id = slugify(text || '')
                return `<h${depth} id="${id}">${marked.parseInline(text || '')}</h${depth}>`
            },
            code(...args: any[]): string {
                // v18: code({ text, lang })  |  v4: code(text, lang)
                const token = args[0]
                const isObj = token != null && typeof token === 'object'
                const code = isObj ? (token.text || '') : String(token || '')
                const lang = isObj ? (token.lang || '') : (args[1] || '')
                if (lang === 'mermaid') {
                    return '<pre class="mermaid">' + escapeHtml(code) + '</pre>'
                }
                if (lang && hljs.getLanguage(lang)) {
                    const highlighted = hljs.highlight(code, { language: lang, ignoreIllegals: true }).value
                    return '<pre><code class="language-' + lang + '">' + highlighted + '</code></pre>'
                }
                const langClass = lang ? ' class="language-' + lang + '"' : ''
                return '<pre><code' + langClass + '>' + escapeHtml(code) + '</code></pre>'
            },
        },
    })
}
