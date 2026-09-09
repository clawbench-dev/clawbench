import { marked, highlightCode } from '@/utils/globals.ts'
import type { Token, Tokens } from 'marked'
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

/** Position meta attached by the local source-line annotator. Line numbers are 0-based. */
interface TokenPositionMeta {
    position?: {
        /** Start position of every line spanned by the token (unused; kept for shape parity). */
        lines?: unknown[]
        /** Position at the beginning of the token. */
        start?: { offset?: number; line?: number; column?: number }
        /** Position at the end of the token (0-based line of the last row). */
        end?: { offset?: number; line?: number; column?: number }
    }
}

/**
 * Read the 1-based source line from a token annotated by our structural
 * annotator (the renderMarkdown lexer → annotateSourceLines pipeline). Returns
 * '' when the token has no position (callers using the plain marked.parse
 * path) so output stays byte-identical to the default renderer in that case.
 */
function sourceLineAttr(token: unknown): string {
    const line = (token as TokenPositionMeta | null)?.position?.start?.line
    return typeof line === 'number' && line >= 0 ? ` data-source-line="${line + 1}"` : ''
}

/**
 * Emit both the start and the inclusive end source line of a block token
 * (1-based). End is derived from the annotator's end position, which spans the
 * block's raw source including any closing fence — so a code fence / table's
 * last row is covered exactly. Falls back to start-only when no end is known.
 */
function sourceRangeAttr(token: unknown): string {
    const base = sourceLineAttr(token)
    if (!base) return ''
    const end = (token as TokenPositionMeta | null)?.position?.end?.line
    return typeof end === 'number' && end >= 0 ? `${base} data-source-end="${end + 1}"` : base
}

/**
 * Count newlines in a string (position advancement primitive). A token's raw
 * always contains exactly the newlines its source span covered, even when
 * inline content was normalized (e.g. table cells dropping `\|` escape
 * backslashes, list items expanding tabs to spaces) — so line numbers stay
 * exact regardless of character-level rewrites.
 */
function countNewlines(s: string): number {
    let n = 0
    for (let i = 0; i < s.length; i++) if (s[i] === '\n') n++
    return n
}

/** Inner value of a marked token (after the v18 single-object convention). */
type TokVal = Record<string, unknown>

/**
 * Structural source-line annotator (replaces marked-token-position).
 *
 * marked-token-position locates each token by verbatim-searching its raw text
 * in the source, and throws "Cannot find … in …" whenever marked normalizes a
 * token's raw (escaped `\|` → `|` inside table cells, tab → spaces inside list
 * items). That exception escapes marked.parse() and blanks the whole render.
 *
 * The only consumers of these positions are the block renderers emitting
 * data-source-line (scroll-to-source, TOC/quote linking) — a 1-based line
 * number computed from the row count. So instead of matching content, we walk
 * the block token tree and advance a running line counter by the newlines in
 * each token's raw. Rendered/text normalization never changes newline counts,
 * so every block's start line is exact, the annotator cannot throw, and crash
 * cases (tables with `\|`, tab-indented nested lists) now render with correct
 * line attributes.
 *
 * Only block tokens are annotated; inline tokens (which carry no renderer
 * position consumers and are the ones whose raw gets rewritten) are skipped —
 * this is what makes the walk total: block raw text is never normalized by
 * marked, so it is always present in the token tree as written.
 *
 * Nested structure walk order mirrors how rendered markup nests:
 *   list_item   tokens start at the item's own line
 *   blockquote  children start at the quote's own line
 * Multi-line items advance the counter so later items land on their own rows.
 */
function annotateSourceLines(tokens: Token[], startLine: number): void {
    let line = startLine
    for (const token of tokens) {
        ;(token as Token & TokenPositionMeta).position = {
            lines: [], // consumers only read start.line
            start: { offset: 0, line, column: 0 },
            end: { offset: 0, line: line + countNewlines(token.raw), column: 0 },
        }
        if (token.type === 'list') {
            let itemLine = line
            for (const item of (token as Tokens.List).items) {
                if (item.tokens) annotateSourceLines(item.tokens, itemLine)
                itemLine += countNewlines(item.raw)
            }
        } else if (token.type === 'blockquote') {
            annotateSourceLines((token as Tokens.Blockquote).tokens, line)
        }
        line += countNewlines(token.raw)
    }
}

/**
 * Configure marked's custom renderer.
 *
 * A processAllTokens hook annotates every block token with its source position
 * (via our local structural annotator — see annotateSourceLines), and the
 * block-level renderers below emit data-source-line="N" from that position.
 * Callers that bypass this hook (no position on tokens) get byte-identical
 * default output — verified against lib/marked.esm.js — so there is no visual
 * regression.
 *
 * Call once at app startup (from main.ts). Idempotent: repeat calls (tests,
 * HMR) must not double-wrap the hooks/renderers.
 */
let markedRendererConfigured = false
export function configureMarkedRenderer(): void {
    if (markedRendererConfigured) return
    markedRendererConfigured = true

    marked.use({
        // Add a source position to every block token during marked.parse so
        // the block renderers can emit data-source-line (1-based source line).
        // Token positions survive protectMarkdown because it keeps the row
        // count identical to the source (code is restored multi-line; display
        // math placeholders are padded with newlines to span the same rows).
        // Unlike marked-token-position this never throws on normalized
        // content, so tables containing `\|` / nested tab lists render fine.
        hooks: {
            processAllTokens(tokens: Token[]): Token[] {
                annotateSourceLines(tokens, 0)
                return tokens
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
                const attr = sourceRangeAttr(token)
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
                return `<table${sourceRangeAttr(token)}>\n<thead>\n${header}</thead>\n${tbody}</table>\n`
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
