/**
 * Search utilities — pure functions for file content searching and highlighting.
 *
 * Extracted from SearchDrawer.vue for testability.
 */

import { escapeHtml } from '@/utils/html.ts'
import { getFileType } from '@/utils/fileType.ts'
import { hljs } from '@/utils/globals.ts'

/** Block-level HTML tags used to find ancestor containers in rendered mode */
export const BLOCK_TAGS = new Set([
  'P', 'LI', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6',
  'BLOCKQUOTE', 'TD', 'TH', 'DT', 'DD', 'PRE', 'FIGCAPTION', 'DIV',
])

/**
 * Highlight a query string within plain text.
 * Escapes HTML first, then wraps matches with <mark> tags.
 * Case-insensitive: "Error" query will match "error", "ERROR", etc.
 */
export function highlightText(text: string, q: string): string {
  if (!q) return escapeHtml(text)
  const escaped = escapeHtml(text)
  const re = new RegExp(q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi')
  return escaped.replace(re, '<mark>$&</mark>')
}

/**
 * Apply syntax highlighting to a single line of code.
 * Strips any wrapping <span class="line"> that hljs adds.
 */
export function highlightLineSyntax(line: string, lang: string): string {
  try {
    let h = hljs.highlight(line, { language: lang, ignoreIllegals: true }).value
    h = h.replace(/^<span class="line">/, '').replace(/<\/span>\s*$/, '')
    return h
  } catch {
    return escapeHtml(line)
  }
}

/**
 * Highlight query matches inside syntax-highlighted HTML.
 * Only marks text outside of HTML tags to avoid breaking tag structure.
 * Case-insensitive: matches regardless of letter case.
 */
export function markInHighlighted(highlightedHtml: string, q: string): string {
  if (!q) return highlightedHtml
  const segments = highlightedHtml.split(/(<[^>]+>)/)
  const escaped = q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const re = new RegExp(escaped, 'gi')
  return segments
    .map((seg) => {
      if (seg.startsWith('<')) return seg // HTML tag, leave as-is
      return seg.replace(re, '<mark>$&</mark>')
    })
    .join('')
}

/**
 * Search raw text content for lines matching a query.
 * Returns matched lines with syntax highlighting and query marking.
 */
export function searchRawContent(
  q: string,
  content: string,
  fileName: string,
): Array<{ line: number; text: string; highlighted: string }> {
  if (!content || !q) return []
  const lang = getFileType(fileName)?.lang || 'plaintext'
  const lines = content.split('\n')
  const lowerQ = q.toLowerCase()
  const out = []
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].toLowerCase().includes(lowerQ)) {
      const highlighted = markInHighlighted(highlightLineSyntax(lines[i], lang), q)
      out.push({
        line: i + 1,
        text: lines[i],
        highlighted,
      })
    }
  }
  return out
}

/**
 * Highlight text at specified character positions using <mark> tags.
 * Positions are rune-based (character-level), matching backend MatchRange.
 * All text is escaped via escapeHtml to prevent XSS.
 */
export function highlightTextByPositions(text: string, positions: { start: number; end: number }[]): string {
  if (!text) return ''
  if (!positions || positions.length === 0) return escapeHtml(text)

  // Convert rune offsets to string indices (JavaScript strings are UTF-16)
  const runes = [...text]
  const runeToIndex: number[] = []
  let idx = 0
  for (let i = 0; i < runes.length; i++) {
    runeToIndex.push(idx)
    idx += runes[i].length // surrogate pairs occupy 2 UTF-16 units
  }
  runeToIndex.push(idx) // sentinel

  let result = ''
  let lastEnd = 0

  for (const pos of positions) {
    const byteStart = runeToIndex[Math.min(pos.start, runes.length)]
    const byteEnd = runeToIndex[Math.min(pos.end, runes.length)]

    if (byteStart > lastEnd) {
      result += escapeHtml(text.slice(lastEnd, byteStart))
    }
    if (byteEnd > byteStart) {
      result += '<mark>' + escapeHtml(text.slice(byteStart, byteEnd)) + '</mark>'
    }
    lastEnd = Math.max(lastEnd, byteEnd)
  }

  if (lastEnd < text.length) {
    result += escapeHtml(text.slice(lastEnd))
  }

  return result
}
