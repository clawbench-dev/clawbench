// Diff rendering utilities

import { hljs } from './globals.ts'
import { escapeHtml } from './html.ts'
import { getFileType } from './fileType.ts'

export function detectLang(filePath: string): string {
    if (!filePath) return 'plaintext'
    return getFileType(filePath).lang
}

export function highlightLine(line: string, lang: string): string {
    if (!line) return ''
    try {
        return hljs.highlight(line, { language: lang, ignoreIllegals: true }).value
    } catch {
        return escapeHtml(line)
    }
}

export interface DiffLine {
    type: 'add' | 'del' | 'ctx'
    content: string
    oldLine: number | null
    newLine: number | null
    /** True for collapsed-gap ellipsis lines (not real file content) */
    isEllipsis?: boolean
}

interface Hunk {
    header: string
    lines: DiffLine[]
}

/**
 * Parse a unified diff string into structured DiffLine objects.
 * Pure function — no rendering, no syntax highlighting.
 */
export function parseDiffLines(raw: string): DiffLine[] {
    const lines = raw.split('\n')
    const result: DiffLine[] = []
    let oldLineNum = 0
    let newLineNum = 0
    let inHunk = false

    for (const line of lines) {
        if (line.startsWith('@@')) {
            const header = parseHunkHeader(line)
            if (header) {
                oldLineNum = header.oldStart
                newLineNum = header.newStart
                inHunk = true
            }
        } else if (line.startsWith(' ') && inHunk) {
            result.push({ type: 'ctx', content: line.substring(1), oldLine: oldLineNum++, newLine: newLineNum++ })
        } else if (line.startsWith('+') && !line.startsWith('+++') && inHunk) {
            result.push({ type: 'add', content: line.substring(1), oldLine: null, newLine: newLineNum++ })
        } else if (line.startsWith('-') && !line.startsWith('---') && inHunk) {
            result.push({ type: 'del', content: line.substring(1), oldLine: oldLineNum++, newLine: null })
        }
    }
    return result
}

export function parseHunkHeader(line: string): {
    oldStart: number; oldCount: number;
    newStart: number; newCount: number;
    text: string
} | null {
    const m = line.match(/^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)/)
    if (!m) return null
    return {
        oldStart: parseInt(m[1]),
        oldCount: parseInt(m[2] || '1'),
        newStart: parseInt(m[3]),
        newCount: parseInt(m[4] || '1'),
        text: m[5].trim(),
    }
}

export function renderDiff(raw: string, filePath: string): string {
    const lang = detectLang(filePath)
    const lines = raw.split('\n')
    const hunks: Hunk[] = []
    let currentHunk: Hunk | null = null
    let oldLineNum = 0
    let newLineNum = 0

    for (const line of lines) {
        if (line.startsWith('@@')) {
            const header = parseHunkHeader(line)
            if (header) {
                if (currentHunk && currentHunk.lines.length > 0) {
                    hunks.push(currentHunk)
                }
                currentHunk = { header: header.text, lines: [] }
                oldLineNum = header.oldStart
                newLineNum = header.newStart
            }
        } else if (line.startsWith(' ') && currentHunk) {
            currentHunk.lines.push({
                type: 'ctx',
                content: line.substring(1),
                oldLine: oldLineNum++,
                newLine: newLineNum++,
            })
        } else if (line.startsWith('+') && !line.startsWith('+++') && currentHunk) {
            currentHunk.lines.push({
                type: 'add',
                content: line.substring(1),
                oldLine: null,
                newLine: newLineNum++,
            })
        } else if (line.startsWith('-') && !line.startsWith('---') && currentHunk) {
            currentHunk.lines.push({
                type: 'del',
                content: line.substring(1),
                oldLine: oldLineNum++,
                newLine: null,
            })
        } else if (/^(diff |index |---|\+\+\+)/.test(line)) {
            // skip meta lines
        }
    }
    if (currentHunk && currentHunk.lines.length > 0) {
        hunks.push(currentHunk)
    }

    if (hunks.length === 0) {
        if (raw.trim().length === 0) return ''
        const clean = lines
            .filter(l => !/^(diff |index |---|\+\+\+)/.test(l))
            .map(l => l.replace(/^[+-]{2}/, ''))
            .join('\n')
        return `<div class="diff-view"><pre class="diff-raw">${escapeHtml(clean)}</pre></div>`
    }

    let html = `<div class="diff-view diff-unified-view">`
    for (const hunk of hunks) {
        html += `<div class="diff-hunk">`
        if (hunk.header) {
            html += `<div class="diff-hunk-header">${escapeHtml(hunk.header)}</div>`
        }
        html += `<div class="diff-hunk-body">`
        html += `<table class="diff-table">`
        for (const dl of hunk.lines) {
            const prefix = dl.type === 'add' ? '+' : dl.type === 'del' ? '-' : ' '
            html += `<tr class="diff-line diff-line-${dl.type}">`
            html += `<td class="diff-linum diff-linum-old">${dl.oldLine ?? ''}</td>`
            html += `<td class="diff-linum diff-linum-new">${dl.newLine ?? ''}</td>`
            html += `<td class="diff-prefix">${escapeHtml(prefix)}</td>`
            html += `<td class="diff-content">${highlightLine(dl.content, lang)}</td>`
            html += `</tr>`
        }
        html += `</table></div></div>`
    }
    return html + '</div>'
}
