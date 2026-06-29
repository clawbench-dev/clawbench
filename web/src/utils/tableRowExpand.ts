/**
 * Table row expand utility — adds identifiers to markdown tables for row-form modal.
 * No button injection — clicking a row directly opens the modal.
 * Drag-vs-click detection and modal state management via useTableRowExpand composable.
 */

let nextTableIdx = 0

/**
 * Add data-table-idx to each <table> and data-row-idx to each <tbody><tr>
 * for row identification in event delegation.
 *
 * Uses DOMParser for robustness (same pattern as file-path/commit annotations).
 * No-op if HTML contains no <table> elements.
 */
export function injectTableRowAttrs(html: string): string {
  if (!html || !html.includes('<table')) return html

  const parser = new DOMParser()
  const doc = parser.parseFromString(html, 'text/html')
  const tables = doc.querySelectorAll('table')
  if (tables.length === 0) return html

  let changed = false

  for (const table of tables) {
    if (table.hasAttribute('data-table-idx')) continue
    table.setAttribute('data-table-idx', String(nextTableIdx++))
    changed = true

    // Add data-row-idx to each data row in tbody
    const dataRows = table.querySelectorAll('tbody tr')
    let rowIdx = 0
    for (const tr of dataRows) {
      tr.setAttribute('data-row-idx', String(rowIdx++))
    }
  }

  if (!changed) return html
  return doc.body.innerHTML
}

/**
 * Parse table data directly from a <table> DOM element.
 * Extracts headers (thead th) and rows (tbody tr > td innerHTML).
 */
export function parseTableDataFromElement(table: HTMLTableElement): { headers: string[], rows: string[][] } | null {
  const headers = Array.from(table.querySelectorAll('thead th'))
    .map(th => th.textContent?.trim() || '')

  const rows = Array.from(table.querySelectorAll('tbody tr'))
    .map(tr => {
      return Array.from(tr.querySelectorAll('td'))
        .map(td => td.innerHTML?.trim() || '')
    })

  if (headers.length === 0 && rows.length === 0) return null
  return { headers, rows }
}

// ── Drag-vs-click guard ──
// Track mousedown/touchstart position per table-wrap; if pointer moves >5px it's a drag, not a click.

const DRAG_THRESHOLD = 5
const dragStartMap = new WeakMap<HTMLElement, { x: number, y: number }>()

/**
 * Call on mousedown inside a .table-wrap to record start position.
 */
export function onTableMouseDown(event: MouseEvent) {
  const wrap = (event.target as HTMLElement).closest('.table-wrap') as HTMLElement | null
  if (!wrap) return
  dragStartMap.set(wrap, { x: event.clientX, y: event.clientY })
}

/**
 * Call on touchstart inside a .table-wrap to record touch start position.
 * Mobile scroll uses touch events, not mouse events — mousedown won't fire until
 * after the touch ends, so we must track touchstart separately.
 */
export function onTableTouchStart(event: TouchEvent) {
  const wrap = (event.target as HTMLElement).closest('.table-wrap') as HTMLElement | null
  if (!wrap || event.touches.length === 0) return
  const touch = event.touches[0]
  dragStartMap.set(wrap, { x: touch.clientX, y: touch.clientY })
}

/**
 * Call on click inside a .table-wrap. Returns true if this click was a drag (should be ignored).
 */
export function isTableDragClick(event: MouseEvent): boolean {
  const wrap = (event.target as HTMLElement).closest('.table-wrap') as HTMLElement | null
  if (!wrap) return false
  const start = dragStartMap.get(wrap)
  if (!start) return false
  const dx = Math.abs(event.clientX - start.x)
  const dy = Math.abs(event.clientY - start.y)
  return dx > DRAG_THRESHOLD || dy > DRAG_THRESHOLD
}
