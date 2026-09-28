/**
 * Visual (soft-wrapped) row geometry of a textarea caret.
 *
 * A textarea's LOGICAL rows (the count of `\n`) are not its VISUAL rows: a long
 * line with no newline still occupies several rows once it wraps. A caret guard
 * that only counts `\n` therefore misjudges a soft-wrapped draft — e.g. the chat
 * input's ArrowUp history navigation would steal the key while the caret still
 * had rows above it to move through.
 *
 * The DOM exposes no API for "which visual row is the caret on", so this
 * measures it with a hidden mirror <textarea> carrying the same width, font and
 * padding: the row count of the text before the caret, minus one, is the caret's
 * zero-based row. A mirror <textarea> (rather than a <div>) is deliberate — the
 * browser then applies the exact same wrapping algorithm to both.
 */

export interface CaretVisualRows {
  /** Zero-based visual row the caret sits on (soft wraps counted). */
  caretRow: number
  /** Total visual rows the whole value occupies. */
  totalRows: number
}

/**
 * Style properties copied onto the mirror. Anything affecting where a line
 * breaks or how tall it is must be here, or the mirror's wrapping silently
 * diverges from the real textarea's.
 */
const COPIED_STYLE_PROPS = [
  'fontFamily',
  'fontSize',
  'fontWeight',
  'fontStyle',
  'letterSpacing',
  'wordSpacing',
  'textTransform',
  'textIndent',
  'lineHeight',
  'tabSize',
  'whiteSpace',
  'wordBreak',
  'overflowWrap',
  'paddingTop',
  'paddingRight',
  'paddingBottom',
  'paddingLeft',
  'borderTopWidth',
  'borderRightWidth',
  'borderBottomWidth',
  'borderLeftWidth',
] as const

/**
 * Measure the caret's visual row and the value's total visual rows.
 *
 * Returns null when there is no layout to measure — a detached element, a zero
 * width (the textarea is hidden), or a non-pixel line-height such as jsdom's
 * `normal`. Callers must treat null as "unknown" and fall back to logical rows,
 * never as "caret is on the first row".
 */
export function measureCaretVisualRows(
  el: HTMLTextAreaElement | null | undefined
): CaretVisualRows | null {
  if (!el) return null
  const cs = getComputedStyle(el)
  const lineHeight = parseFloat(cs.lineHeight)
  // clientWidth is 0 for display:none / detached / jsdom, and a non-px
  // line-height parses to NaN — either way the row arithmetic below is
  // meaningless, so report "unmeasurable" instead of guessing.
  if (!lineHeight || !Number.isFinite(lineHeight) || !el.clientWidth) return null

  const paddingTop = parseFloat(cs.paddingTop) || 0
  const paddingBottom = parseFloat(cs.paddingBottom) || 0

  const mirror = document.createElement('textarea')
  const style = mirror.style
  style.position = 'absolute'
  style.top = '0'
  style.left = '-9999px'
  style.height = 'auto'
  style.visibility = 'hidden'
  style.resize = 'none'
  style.overflow = 'hidden'
  style.boxSizing = 'border-box'
  style.width = `${el.clientWidth}px`
  // rows=1 keeps the control's intrinsic floor at a single line, so scrollHeight
  // reflects content rather than a default two-row height.
  mirror.rows = 1
  for (const prop of COPIED_STYLE_PROPS) {
    ;(style as unknown as Record<string, string>)[prop] = (cs as unknown as Record<string, string>)[prop]
  }

  document.body.appendChild(mirror)
  try {
    const rowsOf = (value: string): number => {
      mirror.value = value
      const contentHeight = mirror.scrollHeight - paddingTop - paddingBottom
      return Math.max(1, Math.round(contentHeight / lineHeight))
    }
    const totalRows = rowsOf(el.value)
    const pos = el.selectionStart ?? el.value.length
    const caretRow = rowsOf(el.value.slice(0, pos)) - 1
    return { caretRow, totalRows }
  } finally {
    mirror.remove()
  }
}
