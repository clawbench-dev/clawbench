import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import { marked, hljs } from './utils/globals.ts'
import { slugify } from './utils/toc.ts'
import { escapeHtml } from './utils/html.ts'
import { buildCodeLinesFromHighlighted, buildCodeLinesFromEscaped } from './utils/codeRender.ts'

// Configure marked (moved from inline script in index.html)
marked.use({
    renderer: {
        heading(token: { text?: string; depth: number }): string {
            const text = marked.parseInline(token.text || '')
            const level = token.depth
            const id = slugify(token.text || '')
            return `<h${level} id="${id}">${text}</h${level}>`
        },
        code(token: { text?: string; lang?: string }): string {
            const code = token.text || ''
            const lang = token.lang || ''
            if (lang === 'mermaid') {
                return '<pre class="mermaid">' + escapeHtml(code) + '</pre>'
            }
            // Per-line structure with line numbers (same as CodePreview)
            if (lang && hljs.getLanguage(lang)) {
                const highlighted = hljs.highlight(code, { language: lang, ignoreIllegals: true }).value
                const lines = buildCodeLinesFromHighlighted(highlighted)
                return '<pre class="code-block-pre" data-language="' + lang + '"><code>' + lines + '</code></pre>'
            }
            // Unknown language: escape and split into lines
            const lines = buildCodeLinesFromEscaped(escapeHtml(code))
            const langAttr = lang ? ' data-language="' + lang + '"' : ''
            return '<pre class="code-block-pre"' + langAttr + '><code>' + lines + '</code></pre>'
        },
    },
})

createApp(App).use(i18n).mount('#app')
