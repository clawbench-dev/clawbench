/**
 * Pure helpers for the CodeMirror sticky-scroll feature.
 *
 * Kept free of heavy imports (no @codemirror, no @lezer, no i18n/api) so they are
 * trivially unit-testable in isolation and don't pull module-load side effects
 * into the test bundle.
 *
 * Geometry is supplied through the `StickyView` adapter so tests can pass a mock
 * instead of a real CodeMirror EditorView.
 */

// ─── Types ────────────────────────────────────────────────────────────────────

export interface ScopeSymbol {
  name: string
  kind: string
  line: number
  endLine: number
  level: number
}

export interface StickyLine {
  /** 1-based line number of the pinned definition */
  lineNum: number
  /** pixel offset from the overlay top (accumulated across pinned rows) */
  top: number
  /** rendered height of the definition line (supports wrapped lines) */
  height: number
}

export interface HighlightRange {
  from: number
  to: number
  classes: string
}

/** Minimal view-like geometry adapter used by the pure helpers (testable). */
export interface StickyView {
  lineBlockAtHeight(height: number): { from: number }
  lineBlockAt(pos: number): { top: number; height: number }
  state: {
    doc: {
      lineAt(pos: number): { number: number }
      line(n: number): { from: number }
    }
  }
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Escape text for safe injection into innerHTML. */
export function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/**
 * Build highlighted HTML for a run of text given non-overlapping highlight ranges.
 * Ranges are 0-based, document-relative and must be within [0, text.length].
 * Gaps between ranges are emitted as plain escaped text.
 */
export function buildHighlightedHtml(text: string, ranges: HighlightRange[], escape: (s: string) => string = escapeHtml): string {
  let html = ''
  let cursor = 0
  for (const r of ranges) {
    if (r.from > cursor) html += escape(text.slice(cursor, r.from))
    html += `<span class="${r.classes}">${escape(text.slice(r.from, r.to))}</span>`
    cursor = r.to
  }
  if (cursor < text.length) html += escape(text.slice(cursor))
  return html
}

/** The first visible line number for a given scroller scrollTop. */
export function firstVisibleLineNumber(view: StickyView, scrollTop: number): number {
  const top = Math.max(0, scrollTop)
  const block = view.lineBlockAtHeight(top + 1)
  return view.state.doc.lineAt(block.from).number
}

/** All scopes enclosing `lineNumber`, sorted outermost-first (by scope width). */
export function findEnclosingScopes(symbols: ScopeSymbol[], lineNumber: number): ScopeSymbol[] {
  return symbols
    .filter((s) => s.line <= lineNumber && s.endLine >= lineNumber)
    .sort((a, b) => (b.endLine - b.line) - (a.endLine - a.line))
}

/**
 * Compute the sticky lines to pin for a given scroll position.
 * Only scopes whose definition line has scrolled above the viewport top are kept,
 * capped at `maxSticky`. `top` accumulates by each previous row's rendered height.
 */
export function buildStickyLines(view: StickyView, symbols: ScopeSymbol[], scrollTop: number, maxSticky = 5): StickyLine[] {
  const firstVisible = firstVisibleLineNumber(view, scrollTop)
  const enclosing = findEnclosingScopes(symbols, firstVisible)
  const result: StickyLine[] = []
  let accTop = 0
  for (const sym of enclosing) {
    if (result.length >= maxSticky) break
    const from = view.state.doc.line(sym.line).from
    const block = view.lineBlockAt(from)
    if (block.top < scrollTop) {
      result.push({ lineNum: sym.line, top: accTop, height: block.height })
      accTop += block.height
    }
  }
  return result
}
