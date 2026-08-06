import type { Extension } from '@codemirror/state'
import type { Language } from '@codemirror/language'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { yaml } from '@codemirror/lang-yaml'
import { xml } from '@codemirror/lang-xml'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { markdown } from '@codemirror/lang-markdown'
import { go } from '@codemirror/lang-go'
import { python } from '@codemirror/lang-python'
import { rust } from '@codemirror/lang-rust'
import { java } from '@codemirror/lang-java'
import { cpp } from '@codemirror/lang-cpp'
import { sql } from '@codemirror/lang-sql'
import { php } from '@codemirror/lang-php'

const LANG_EXT: Record<string, () => Extension> = {
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
    rust: () => rust(),
    java: () => java(),
    c: () => cpp(),
    cpp: () => cpp(),
    sql: () => sql(),
    php: () => php(),
}

/**
 * Markdown with nested syntax highlighting inside fenced code blocks.
 * Mirrors the browse mode where hljs highlights code fences inside markdown.
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
        rust: () => rust(),
        rs: () => rust(),
        java: () => java(),
        c: () => cpp(),
        cpp: () => cpp(),
        'c++': () => cpp(),
        sql: () => sql(),
        php: () => php(),
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

export function buildLangExtension(fileLang: string): Extension {
    const factory = LANG_EXT[fileLang]
    return factory ? factory() : []
}
