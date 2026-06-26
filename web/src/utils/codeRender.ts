/**
 * Shared code rendering utilities.
 *
 * Used by both CodePreview.vue (standalone file viewer) and the marked
 * code renderer (markdown rendered code blocks) to produce the same
 * per-line HTML structure:
 *
 *   <div class="code-line" data-line="N">
 *     <span class="line-num">N</span>
 *     <span class="code-text">highlighted html</span>
 *   </div>
 */

import { hljs } from '@/utils/globals.ts'
import { escapeHtml } from '@/utils/html.ts'
import type { CodeDiffMarkerInfo } from '@/composables/useMarkdownDiff.ts'

// ─── HTML line splitting ───

/**
 * Split a highlighted HTML string into per-line HTML strings,
 * properly handling `<span>` tags that cross line boundaries.
 *
 * Algorithm:
 * 1. Split the HTML by `\n`
 * 2. Track which `<span>` tags are open at the end of each line
 * 3. Close open spans at end of line, reopen at start of next line
 */
export function splitHighlightedHtml(html: string): string[] {
    if (!html) return ['']

    const lines = html.split('\n')
    if (lines.length === 1) return lines

    const result: string[] = []
    let openSpans: string[] = []

    for (let i = 0; i < lines.length; i++) {
        let line = lines[i]

        // Re-open spans carried from previous line
        if (openSpans.length > 0) {
            line = openSpans.join('') + line
        }

        // Walk through the line tracking open spans
        const tempSpans = [...openSpans]
        let pos = 0

        while (pos < line.length) {
            const openIdx = line.indexOf('<span', pos)
            const closeIdx = line.indexOf('</span>', pos)

            if (openIdx === -1 && closeIdx === -1) break

            if (openIdx !== -1 && (closeIdx === -1 || openIdx < closeIdx)) {
                const endIdx = line.indexOf('>', openIdx)
                if (endIdx !== -1) {
                    tempSpans.push(line.substring(openIdx, endIdx + 1))
                    pos = endIdx + 1
                } else {
                    pos = openIdx + 5
                }
            } else {
                if (tempSpans.length > 0) tempSpans.pop()
                pos = closeIdx + 7
            }
        }

        // Close remaining open spans at end of line
        if (tempSpans.length > 0) {
            line += '</span>'.repeat(tempSpans.length)
        }

        result.push(line)
        openSpans = tempSpans
    }

    return result
}

// ─── Per-line HTML builder ───

/**
 * Build a single code-line div HTML string.
 */
function codeLineHtml(lineNum: number, contentHtml: string, showLineNumbers: boolean, markerInfo?: CodeDiffMarkerInfo): string {
    const lineNumHtml = showLineNumbers ? `<span class="line-num">${lineNum}</span>` : ''
    let markerHtml = ''
    if (markerInfo) {
        const heightStyle = (markerInfo.lineCount && markerInfo.lineCount > 1)
            ? ` style="height: calc(${markerInfo.lineCount} * var(--code-line-height, 20.8px))"`
            : ''
        markerHtml = `<span class="diff-marker diff-marker-inline diff-marker-${markerInfo.type}" data-marker-id="${markerInfo.id}" role="button" tabindex="0" aria-label="${markerInfo.type} line ${lineNum}"${heightStyle}>${markerInfo.label}</span>`
    }
    return `<div class="code-line" data-line="${lineNum}">${lineNumHtml}<span class="code-text">${contentHtml}</span>${markerHtml}</div>`
}

/**
 * Build per-line HTML from already-highlighted code.
 * Used by the marked code renderer (highlight whole block first, then split).
 *
 * @param highlightedHtml  The full highlighted HTML from hljs.highlight()
 * @param showLineNumbers  Whether to include line numbers
 * @returns HTML string of code-line divs
 */
export function buildCodeLinesFromHighlighted(highlightedHtml: string, showLineNumbers = true): string {
    const lines = splitHighlightedHtml(highlightedHtml)
    return lines.map((lineHtml, i) => codeLineHtml(i + 1, lineHtml, showLineNumbers)).join('')
}

/**
 * Build per-line HTML from raw (unhighlighted) escaped code.
 * Used by the marked code renderer for unknown languages.
 *
 * @param escapedHtml  The HTML-escaped raw code string
 * @param showLineNumbers  Whether to include line numbers
 * @returns HTML string of code-line divs
 */
export function buildCodeLinesFromEscaped(escapedHtml: string, showLineNumbers = true): string {
    return escapedHtml.split('\n').map((line, i) => codeLineHtml(i + 1, line, showLineNumbers)).join('')
}

/**
 * Render code content into per-line HTML with syntax highlighting.
 * Used by CodePreview.vue.
 *
 * @param content   Raw code content
 * @param lang      Language for syntax highlighting
 * @param showNums  Whether to show line numbers
 * @param flashMap  Optional: line number → flash ranges for change highlighting
 * @param flashCls  CSS class for flash spans ('char-flash-add' or 'char-flash-delete')
 * @param markerMap Optional: line number → diff marker info for inline markers
 * @returns HTML string of code-line divs
 */
export function renderCodeLines(
    content: string,
    lang: string,
    showNums: boolean,
    flashMap?: Map<number, Array<{ start: number; end: number }>>,
    flashCls?: string,
    markerMap?: Map<number, CodeDiffMarkerInfo>,
): string {
    return content.split('\n').map((rawLine, i) => {
        const lineNum = i + 1
        const lineRanges = flashMap?.get(lineNum)
        const markerInfo = markerMap?.get(lineNum)
        let h: string

        if (lineRanges && flashCls) {
            h = applyFlashToLine(rawLine, lineRanges, flashCls, lang)
        } else {
            try {
                h = hljs.highlight(rawLine, { language: lang, ignoreIllegals: true }).value
            } catch {
                h = escapeHtml(rawLine)
            }
            h = h.replace(/^<span class="line">/, '').replace(/<\/span>\s*$/, '')
        }

        return codeLineHtml(lineNum, h, showNums, markerInfo)
    }).join('')
}

/**
 * Wrap flash ranges in highlighted spans within a line's raw text.
 * Flash segments get escaped (no syntax highlighting — the flash
 * animation background makes them visually distinct), non-flash
 * segments get syntax-highlighted individually.
 */
function applyFlashToLine(
    rawLine: string,
    lineRanges: Array<{ start: number; end: number }>,
    flashCls: string,
    lang: string,
): string {
    const sorted = [...lineRanges].sort((a, b) => a.start - b.start)
    const segments: Array<{ text: string; flash: boolean }> = []
    let pos = 0

    for (const r of sorted) {
        const start = Math.min(r.start, rawLine.length)
        const end = Math.min(r.end, rawLine.length)
        if (start > pos) segments.push({ text: rawLine.slice(pos, start), flash: false })
        if (end > start) segments.push({ text: rawLine.slice(start, end), flash: true })
        pos = end
    }
    if (pos < rawLine.length) segments.push({ text: rawLine.slice(pos), flash: false })

    let result = ''
    for (const seg of segments) {
        if (seg.flash) {
            result += `<span class="${flashCls}">${escapeHtml(seg.text)}</span>`
        } else {
            try {
                result += hljs.highlight(seg.text, { language: lang, ignoreIllegals: true }).value
            } catch {
                result += escapeHtml(seg.text)
            }
        }
    }
    return result
}
