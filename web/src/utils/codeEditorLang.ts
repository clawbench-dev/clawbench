import type { Extension } from '@codemirror/state'
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
    markdown: () => markdown(),
    go: () => go(),
    python: () => python(),
    rust: () => rust(),
    java: () => java(),
    c: () => cpp(),
    cpp: () => cpp(),
    sql: () => sql(),
    php: () => php(),
}

export function buildLangExtension(fileLang: string): Extension {
    const factory = LANG_EXT[fileLang]
    return factory ? factory() : []
}
