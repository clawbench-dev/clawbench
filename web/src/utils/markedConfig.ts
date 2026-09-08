import { marked, highlightCode } from '@/utils/globals.ts'
import type { Token, Tokens } from 'marked'
import { addTokenPositions } from 'marked-token-position'
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

/** Position meta attached by marked-token-position. Line numbers are 0-based. */
interface TokenPositionMeta {
    position?: { start?: { line?: number } }
}

/**
 * Read the 1-based source line from a token annotated by marked-token-position
 * (the renderMarkdown lexer → addTokenPositions pipeline). Returns '' when the
 * token has no position (callers using the plain marked.parse path) so output
 * stays byte-identical to the default renderer in that case.
 */
function sourceLineAttr(token: unknown): string {
    const line = (token as TokenPositionMeta | null)?.position?.start?.line
    return typeof line === 'number' && line >= 0 ? ` data-source-line="${line + 1}"` : ''
}

/** Inner value of a marked token (after the v18 single-object convention). */
type TokVal = Record<string, unknown>

/**
 * Configure marked's custom renderer.
 *
 * A processAllTokens hook annotates every token with its source position
 * (marked-token-position), and the block-level renderers below emit
 * data-source-line="N" from that position. Callers that bypass this hook (no
 * position on tokens) get byte-identical default output — verified against
 * lib/marked.esm.js — so there is no visual regression.
 *
 * Call once at app startup (from main.ts). Idempotent: repeat calls (tests,
 * HMR) must not double-wrap the hooks/renderers.
 */
let markedRendererConfigured = false
export function configureMarkedRenderer(): void {
    if (markedRendererConfigured) return
    markedRendererConfigured = true

    marked.use({
        // Add a source position to every token during marked.parse so the
        // block renderers can emit data-source-line (1-based source line).
        // Token positions survive protectMarkdown because it keeps the row
        // count identical to the source (code is restored multi-line; display
        // math placeholders are padded with newlines to span the same rows).
        hooks: {
            processAllTokens(tokens: Token[]): Token[] {
                return addTokenPositions(tokens)
            },
        },
        renderer: {
            heading(...args: unknown[]): string {
                // v18: heading({ text, depth })  |  v4: heading(text, depth)
                const token = args[0]
                const isObj = token != null && typeof token === 'object'
                const text = isObj ? String((token as TokVal).text || '') : String(token || '')
                const depth = isObj ? (token as TokVal).depth : args[1]
                const baseId = slugify(text)
                // Deduplicate: first occurrence keeps base ID, subsequent get -2, -3, etc.
                const count = (headingIdCounts[baseId] || 0) + 1
                headingIdCounts[baseId] = count
                const id = count > 1 ? `${baseId}-${count}` : baseId
                // Render inline content from the token's parsed tokens (not a
                // re-parse of the raw text) so reference-style links and other
                // constructs resolve exactly like the default renderer.
                const tokens = isObj && Array.isArray((token as TokVal).tokens) ? (token as TokVal).tokens as Token[] : []
                const body = tokens.length ? this.parser.parseInline(tokens) : text
                return `<h${depth} id="${id}"${sourceLineAttr(token)}>${body}</h${depth}>`
            },
            code(...args: unknown[]): string {
                // v18: code({ text, lang })  |  v4: code(text, lang)
                const token = args[0]
                const isObj = token != null && typeof token === 'object'
                const code = isObj ? (String((token as TokVal).text || '')) : String(token || '')
                const lang = isObj ? (String((token as TokVal).lang || '')) : (String(args[1] || ''))
                const attr = sourceLineAttr(token)
                if (lang === 'mermaid') {
                    return '<pre class="mermaid"' + attr + '>' + escapeHtml(code) + '</pre>'
                }
                const highlighted = highlightCode(code, lang || '')
                const langClass = lang ? ' class="language-' + lang + '"' : ''
                return '<pre' + attr + '><code' + langClass + '>' + highlighted + '</code></pre>'
            },
            paragraph(...args: unknown[]): string {
                const token = args[0] as TokVal | undefined
                const body = this.parser.parseInline((token?.tokens as Token[]) || [])
                return `<p${sourceLineAttr(token)}>${body}</p>\n`
            },
            blockquote(...args: unknown[]): string {
                const token = args[0] as TokVal | undefined
                const body = this.parser.parse((token?.tokens as Token[]) || [])
                return `<blockquote${sourceLineAttr(token)}>\n${body}</blockquote>\n`
            },
            hr(...args: unknown[]): string {
                return `<hr${sourceLineAttr(args[0])}>\n`
            },
            list(...args: unknown[]): string {
                const token = args[0] as TokVal | undefined
                const ordered = !!token?.ordered
                const start = typeof token?.start === 'number' ? (token.start as number) : 1
                let body = ''
                for (const item of (token?.items as Tokens.ListItem[]) || []) {
                    body += this.listitem(item)
                }
                const type = ordered ? 'ol' : 'ul'
                const startAttr = ordered && start !== 1 ? ` start="${start}"` : ''
                return `<${type}${startAttr}${sourceLineAttr(token)}>\n${body}</${type}>\n`
            },
            listitem(...args: unknown[]): string {
                const token = args[0] as TokVal | undefined
                const body = this.parser.parse((token?.tokens as Token[]) || [])
                return `<li>${body}</li>\n`
            },
            table(...args: unknown[]): string {
                const token = args[0] as TokVal | undefined
                let header = ''
                for (const cell of (token?.header as Tokens.TableCell[]) || []) {
                    header += this.tablecell(cell)
                }
                header = this.tablerow({ text: header })
                let body = ''
                for (const row of (token?.rows as Tokens.TableCell[][]) || []) {
                    let cells = ''
                    for (const cell of row) {
                        cells += this.tablecell(cell)
                    }
                    body += this.tablerow({ text: cells })
                }
                // Match marked v18 default byte-for-byte: thead cells are wrapped
                // in a <tr>, tbody has no leading newline.
                const tbody = body ? `<tbody>${body}</tbody>` : ''
                return `<table${sourceLineAttr(token)}>\n<thead>\n${header}</thead>\n${tbody}</table>\n`
            },
            tablerow(...args: unknown[]): string {
                const text = (args[0] as { text?: string } | undefined)?.text ?? ''
                return `<tr>\n${text}</tr>\n`
            },
            tablecell(...args: unknown[]): string {
                const token = args[0] as TokVal | undefined
                const content = this.parser.parseInline((token?.tokens as Token[]) || [])
                const tag = token?.header ? 'th' : 'td'
                const alignAttr = typeof token?.align === 'string' ? ` align="${token.align}"` : ''
                return `<${tag}${alignAttr}>${content}</${tag}>\n`
            },
        },
    })
}
