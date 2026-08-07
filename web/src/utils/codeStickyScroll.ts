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

export interface StickyOffsets {
  /** left offset of the code text relative to the sticky overlay */
  left: number
  /** width of the sticky row so it fills the whole content container */
  width: number
}

/**
 * Resolve the sticky row's horizontal geometry. The sticky overlay is a flex item
 * whose left offset varies depending on whether the line-number gutter was present
 * at initial layout or toggled later (it can sit at x=0 or x=gutterWidth). Offsets
 * are therefore measured relative to the overlay so the code text always aligns with
 * the content's left edge and the row fills the content's right edge — avoiding a
 * phantom "extra line-number column" when the gutter is on from initial load.
 */
export function computeStickyOffsets(overlayLeft: number, contentLeft: number, contentRight: number): StickyOffsets {
  return {
    left: Math.max(0, contentLeft - overlayLeft),
    width: Math.max(0, contentRight - overlayLeft),
  }
}

/**
 * Compute the sticky lines to pin for a given scroll position.
 *
 * Nested scopes stack at the top. A scope's definition line sticks as soon as it
 * reaches the bottom of the already-stuck rows (its relative top is within the
 * accumulated stack height), NOT only once it has scrolled past the viewport top —
 * otherwise inner definition lines would slide up underneath the outer stuck rows
 * before sticking.
 *
 * The sticky stack covers the top `accTop` px of the viewport, so the first visible
 * CONTENT line is below it. Each iteration re-derives that line at `scrollTop + accTop`
 * to reveal the next inner scope (whose def line may not enclose the raw viewport-top
 * line). Capped at `maxSticky`. `top` accumulates each row's rendered height.
 */
export function buildStickyLines(view: StickyView, symbols: ScopeSymbol[], scrollTop: number, maxSticky = 5): StickyLine[] {
  const result: StickyLine[] = []
  let accTop = 0
  while (result.length < maxSticky) {
    const firstVisible = firstVisibleLineNumber(view, scrollTop + accTop)
    const enclosing = findEnclosingScopes(symbols, firstVisible)
    // Outermost scope not yet stuck whose definition line has reached the stack bottom.
    let candidate: ScopeSymbol | null = null
    let candBlock: { top: number; height: number } | null = null
    for (const sym of enclosing) {
      if (result.some((r) => r.lineNum === sym.line)) continue
      const block = view.lineBlockAt(view.state.doc.line(sym.line).from)
      if (block.top - scrollTop <= accTop) {
        candidate = sym
        candBlock = block
        break
      }
    }
    if (!candidate || !candBlock) break
    result.push({ lineNum: candidate.line, top: accTop, height: candBlock.height })
    accTop += candBlock.height
  }
  return result
}
