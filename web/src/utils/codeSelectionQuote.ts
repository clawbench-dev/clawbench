/**
 * codeSelectionQuote — pure helpers to convert a DOM text selection inside a
 * CodeMirror 6 read-only editor into the quote-question payload (text + file +
 * accurate line range).
 *
 * Read-only CodeMirrorViewer relies on the browser's native text selection
 * (desktop drag, mobile long-press). CodeMirror keeps that selection outside
 * its own state, so we translate the DOM selection to document offsets via
 * EditorView.posAtDOM and compute the enclosing line numbers ourselves.
 */

import type { EditorView } from '@codemirror/view'

/** Minimal structural view of a browser Selection, decoupled from jsdom. */
export interface SelectionLike {
  anchorNode: Node | null
  anchorOffset: number
  focusNode: Node | null
  focusOffset: number
  isCollapsed: boolean
  toString(): string
}

export interface QuoteSource {
  filePath: string
  language: string
}

export interface QuoteData {
  text: string
  filePath: string
  language: string
  startLine: number
  endLine: number
}

/**
 * Convert a DOM selection to quote data if it is a non-empty selection wholly
 * inside the editor's content. Returns null for collapsed, empty, or external
 * selections so the caller can hide the quote bar.
 */
export function selectionToQuote(
  sel: SelectionLike | null,
  view: EditorView,
  source: QuoteSource,
): QuoteData | null {
  if (!sel || sel.isCollapsed || !sel.anchorNode || !sel.focusNode) return null
  const inContent = (node: Node) => node && view.contentDOM.contains(node)
  if (!inContent(sel.anchorNode) || !inContent(sel.focusNode)) return null

  const text = sel.toString().trim()
  if (!text) return null

  const from = view.posAtDOM(sel.anchorNode, sel.anchorOffset)
  const to = view.posAtDOM(sel.focusNode, sel.focusOffset)
  if (from == null || to == null) return null

  const start = Math.min(from, to)
  const end = Math.max(from, to)
  return {
    text,
    filePath: source.filePath,
    language: source.language,
    startLine: view.state.doc.lineAt(start).number,
    endLine: view.state.doc.lineAt(end).number,
  }
}
