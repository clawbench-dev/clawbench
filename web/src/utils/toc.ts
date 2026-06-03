// Table of Contents extraction utilities

export interface TocItem {
    level: number
    text: string
    id: string
    line: number
}

// Generate a slug from text (compatible with markdown anchor links)
export function slugify(text: string): string {
    return text
        .toLowerCase()
        .replace(/[^\w\u4e00-\u9fa5]+/g, '-')  // Keep Chinese, letters, digits, replace others with -
        .replace(/^-+|-+$/g, '');  // Remove leading/trailing dashes
}

export function extractToc(content: string, lang: string): TocItem[] {
    if (lang === 'markdown') return extractTocMarkdown(content)
    return extractTocForCode(content, lang)
}

function extractTocMarkdown(content: string): TocItem[] {
    const toc: TocItem[] = []
    const headerRegex = /^(#{1,6})\s+(.+)$/gm
    let match
    while ((match = headerRegex.exec(content)) !== null) {
        const level = match[1].length
        const text = match[2].trim()
        const id = slugify(text)
        const line = content.substring(0, match.index).split('\n').length
        toc.push({ level, text, id, line })
    }
    return toc
}

// Patterns for code languages: [regex, lineOffset(0-based), level]
// Only includes languages NOT supported by the backend tree-sitter API.
// Languages with tree-sitter tags query (go, python, typescript, rust, java, c, cpp,
// ruby, swift, kotlin, scala, lua, css, dart, zig, nim, r, etc.) are handled by the
// backend /api/file/symbols endpoint — these regex patterns serve as fallback only.
type CodePattern = [RegExp, number, number]

const CODE_TOC_PATTERNS: Record<string, CodePattern[]> = {
    php: [
        [/^(?:abstract\s+)?(?:class|interface|trait)\s+(\S+)/gm, 1, 1],
        [/^(?:public|private|protected)\s+static\s+function\s+(\S+)\s*\(/gm, 1, 2],
        [/^function\s+(\S+)\s*\(/gm, 1, 2],
    ],
    bash: [
        [/^(?:function\s+)?(\S+)\s*\(\)/gm, 1, 1],
    ],
    sql: [
        [/^(?:CREATE|ALTER|DROP)\s+(?:OR\s+REPLACE\s+)?(?:TABLE|VIEW|INDEX|FUNCTION|PROCEDURE|TRIGGER)\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?[`"]?(\S+)[`"]?/gim, 1, 1],
    ],
    makefile: [
        [/^(\S+):\s*$/gm, 1, 1],
    ],
    nginx: [
        [/^\s*(server)\b/gm, 1, 1],
        [/^\s*(?:location|upstream)\s+(\S+)/gm, 1, 2],
    ],
    ini: [
        [/^\[([^\]]+)\]/gm, 1, 1],
    ],
    dockerfile: [
        [/^(FROM)\s+(\S+)/gm, 2, 1],
        [/^(RUN|CMD|ENTRYPOINT|COPY|ADD|EXPOSE|ENV|ARG|WORKDIR|VOLUME|LABEL|HEALTHCHECK)\b/gm, 1, 2],
    ],
    vue: [
        [/^<(template|script|style)/gm, 1, 1],
    ],
    graphql: [
        [/^(?:type|interface|enum|input|union|scalar|directive)\s+(\S+)/gm, 1, 1],
        [/^\s+(\S+)\s*[(:]/gm, 1, 2],
    ],
    yaml: [
        [/^(\S+)\s*:/gm, 1, 1],
        [/^\s{2}(\S+)\s*:/gm, 1, 2],
        [/^\s{4}(\S+)\s*:/gm, 1, 3],
    ],
    toml: [
        [/^\[\[([^\]]+)\]\]/gm, 1, 1],
        [/^\[([^\]]+)\]/gm, 1, 1],
    ],
    json: [
        [/^\s{0}"([^"]+)"\s*:/gm, 1, 1],
        [/^\s{2,4}"([^"]+)"\s*:/gm, 1, 2],
        [/^\s{6,8}"([^"]+)"\s*:/gm, 1, 3],
    ],
}

function extractLine(content: string, offset: number): number {
    return content.substring(0, offset).split('\n').length
}

function extractTocForCode(content: string, lang: string): TocItem[] {
    const patterns = CODE_TOC_PATTERNS[lang]
    if (!patterns) return extractTocGeneric(content)

    const seen = new Set<string>()
    const toc: TocItem[] = []
    for (const [regex, , level] of patterns) {
        regex.lastIndex = 0
        let match
        while ((match = regex.exec(content)) !== null) {
            const text = match[1].replace(/[{].*$/, '').replace(/[<(].*$/, '').trim()
            if (!text || seen.has(text)) continue
            seen.add(text)
            const line = extractLine(content, match.index)
            toc.push({ level, text, id: 'toc-l' + line, line })
        }
    }
    // Sort by line number
    toc.sort((a, b) => a.line - b.line)
    return toc
}

function extractTocGeneric(content: string): TocItem[] {
    // For JSON/YAML/TOML/XML: extract keys/sections by indentation (up to 3 levels)
    const lines = content.split('\n')
    const toc: TocItem[] = []
    const MAX_INDENT = 6 // indent <= 6 means up to 3 levels (0,2,4,6)

    for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        const trimmed = line.trim()
        if (!trimmed || trimmed.startsWith('//') || trimmed.startsWith('#') || trimmed.startsWith('<!--')) continue

        const indent = line.search(/\S/)
        if (trimmed.endsWith(':') || trimmed.endsWith(',') || trimmed.endsWith('{') || trimmed.endsWith('[') || trimmed.endsWith('(')) {
            // Extract key name
            const keyMatch = trimmed.match(/^["']?([^"':{\[\s,]+)["']?\s*[:{[\(,]/)
            if (keyMatch) {
                const key = keyMatch[1].trim()
                if (key.length < 2 || key === '{' || key === '[') continue
                if (indent <= MAX_INDENT) {
                    toc.push({ level: Math.floor(indent / 2) + 1, text: key, id: 'toc-l' + (i + 1), line: i + 1 })
                }
            }
        }
    }
    return toc.slice(0, 150)
}
