import type { EditorState } from '@codemirror/state'

// Identifier characters treated as part of a single "word" for double-click
// select+copy (letters, digits, underscore, and $ — the common identifier set).
const WORD_RE = /[A-Za-z0-9_$]/

/** The [from, to] range (inclusive of start, exclusive of end) of the word
 *  enclosing `pos` on its line, or null if `pos` sits on a non-word character
 *  (whitespace / punctuation) with no adjacent word characters. */
export function getWordRangeAt(state: EditorState, pos: number): { from: number; to: number } | null {
    const line = state.doc.lineAt(pos)
    const text = line.text
    const local = Math.max(0, Math.min(pos - line.from, text.length))
    let start = local
    let end = local
    while (start > 0 && WORD_RE.test(text[start - 1])) start--
    while (end < text.length && WORD_RE.test(text[end])) end++
    if (start === end) return null
    return { from: line.from + start, to: line.from + end }
}
