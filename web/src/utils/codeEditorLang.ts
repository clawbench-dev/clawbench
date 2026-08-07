import type { Extension } from '@codemirror/state'
import type { Language } from '@codemirror/language'
// High-frequency languages: static imports (always needed)
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { yaml } from '@codemirror/lang-yaml'
import { xml } from '@codemirror/lang-xml'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { markdown } from '@codemirror/lang-markdown'
import { go } from '@codemirror/lang-go'
import { python } from '@codemirror/lang-python'

/** Lazy-loaded language factory: returns a Promise of Extension. */
type LangFactory = () => Extension | Promise<Extension>

const LANG_EXT: Record<string, LangFactory> = {
    // Static imports (high-frequency)
    javascript: () => javascript(),
    typescript: () => javascript({ typescript: true }),
    json: () => json(),
    yaml: () => yaml(),
    xml: () => xml(),
    html: () => html(),
    css: () => css(),
    markdown: () => buildMarkdownExtension(),
    go: () => go(),
    python: () => python(),
    // Lazy imports (official @codemirror/lang-*)
    rust: () => import('@codemirror/lang-rust').then(m => m.rust()),
    java: () => import('@codemirror/lang-java').then(m => m.java()),
    c: () => import('@codemirror/lang-cpp').then(m => m.cpp()),
    cpp: () => import('@codemirror/lang-cpp').then(m => m.cpp()),
    sql: () => import('@codemirror/lang-sql').then(m => m.sql()),
    php: () => import('@codemirror/lang-php').then(m => m.php()),
    vue: () => import('@codemirror/lang-vue').then(m => m.vue()),
    less: () => import('@codemirror/lang-less').then(m => m.less()),
    sass: () => import('@codemirror/lang-sass').then(m => m.sass()),
    liquid: () => import('@codemirror/lang-liquid').then(m => m.liquid()),
    angular: () => import('@codemirror/lang-angular').then(m => m.angular()),
    wast: () => import('@codemirror/lang-wast').then(m => m.wast()),
    // Lazy imports (community packages)
    bash: () => import('@codincod/codemirror-lang-shell').then(m => m.shell()),
    shell: () => import('@codincod/codemirror-lang-shell').then(m => m.shell()),
    lua: () => import('@codincod/codemirror-lang-lua').then(m => m.lua()),
    swift: () => import('@codincod/codemirror-lang-swift').then(m => m.swift()),
    kotlin: () => import('@codincod/codemirror-lang-kotlin').then(m => m.kotlin()),
    scala: () => import('@codincod/codemirror-lang-scala').then(m => m.scala()),
    ruby: () => import('codemirror-lang-ruby').then(m => m.ruby()),
    diff: () => import('codemirror-lang-diff').then(m => m.diff()),
    csharp: () => import('@replit/codemirror-lang-csharp').then(m => m.csharp()),
    perl: () => import('codemirror-lang-perl').then(m => m.perl()),
    makefile: () => import('codemirror-lang-makefile').then(m => m.makefile()),
    r: () => import('codemirror-lang-r').then(m => m.r()),
}

/**
 * Markdown with nested syntax highlighting inside fenced code blocks.
 * Mirrors the browse mode where hljs highlights code fences inside markdown.
 * Only uses static imports for code-fence languages (available immediately).
 */
export function buildMarkdownExtension(): Extension {
    const codeLangs: Record<string, () => Extension> = {
        javascript: () => javascript(),
        typescript: () => javascript({ typescript: true }),
        js: () => javascript(),
        ts: () => javascript({ typescript: true }),
        json: () => json(),
        yaml: () => yaml(),
        yml: () => yaml(),
        xml: () => xml(),
        html: () => html(),
        css: () => css(),
        go: () => go(),
        golang: () => go(),
        python: () => python(),
        py: () => python(),
        markdown: () => markdown(),
        md: () => markdown(),
    }
    return markdown({
        codeLanguages: (info) => {
            const factory = codeLangs[info.toLowerCase()]
            const lang = factory ? factory() : null
            return lang && 'language' in lang ? (lang as { language: Language }).language : null
        },
    })
}

/**
 * Build the language extension for a given file language identifier.
 * Returns a Promise because most language packages are lazy-loaded.
 * Resolves to an empty array (plain text fallback) for unknown languages.
 */
export async function buildLangExtension(fileLang: string): Promise<Extension> {
    const factory = LANG_EXT[fileLang]
    if (!factory) return []
    return factory()
}
