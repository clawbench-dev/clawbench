/**
 * True when the user currently has a non-empty text selection.
 *
 * Row-level click guards need this: dragging to select text inside a clickable
 * row ends with a `click` on the row, because mousedown and mouseup share the
 * row as their common ancestor. Without the guard, selecting a branch name (to
 * copy it) fired the row's action and opened the switch confirmation.
 *
 * A plain click is unaffected — the browser collapses any existing selection on
 * mousedown, so by the time `click` fires the selection is empty. Clicking
 * *inside* an existing selection is the one ambiguous case; the browser keeps
 * that selection until mouseup, so it is treated as a selection interaction
 * rather than a click.
 */
export function hasActiveTextSelection(): boolean {
  const sel = window.getSelection?.()
  return !!sel && sel.toString().length > 0
}
