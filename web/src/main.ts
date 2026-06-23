import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import { LongPressDirective } from './directives/longPress.ts'
import { marked, hljs } from './utils/globals.ts'
import { slugify } from './utils/toc.ts'
import { escapeHtml } from './utils/html.ts'

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
            // Fast path: known language → direct highlight (cheap)
            // Per-line rendering with line numbers is only used by CodePreview
            // (see codeRender.ts) — chat messages don't need line numbers
            // and the per-line split is ~250x slower than a simple wrap,
            // which causes the main thread to freeze on sessions with many
            // code blocks (e.g. 124 blocks in a single session).
            if (lang && hljs.getLanguage(lang)) {
                const highlighted = hljs.highlight(code, { language: lang, ignoreIllegals: true }).value
                return '<pre><code class="language-' + lang + '">' + highlighted + '</code></pre>'
            }
            // No language or unknown language: escapeHtml only.
            // highlightAuto() is extremely expensive (tries all ~190 languages)
            // and the result is rarely useful for chat messages — it causes
            // significant jank on pages with many code blocks.
            const langClass = lang ? ' class="language-' + lang + '"' : ''
            return '<pre><code' + langClass + '>' + escapeHtml(code) + '</code></pre>'
        },
    },
})

createApp(App).use(i18n).directive('long-press', LongPressDirective).mount('#app')
