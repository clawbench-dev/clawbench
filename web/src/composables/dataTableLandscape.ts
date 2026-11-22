/**
 * The one link between a mounted `DataTable` and the fullscreen overlay.
 *
 * Why a registry: `openBlockLandscape()` only receives the DOM element that was
 * clicked. Cloning that element is how raw markdown tables / code blocks get
 * fullscreen, but a clone of a Vue component is an inert picture — the mode
 * switch, sort arrows, search box and pager all stop responding. So a
 * DataTable registers a handle here on mount, and the overlay uses it to render
 * a LIVE instance instead.
 *
 * Deliberately a leaf module (imports nothing): `DataTable.vue` is loaded by
 * component tests that only partially mock `vue-i18n`, and pulling
 * `useBlockLandscape` in statically would drag the app's i18n singleton along
 * (see the note on `openLandscape` in DataTable.vue).
 */

import type { TableMathCells } from '@/utils/tableData'

/** The source table state, captured when the overlay opens. */
export interface BlockLandscapeTable {
  headers: string[]
  rows: string[][]
  mathCells?: TableMathCells
  /** The mode the live table was showing, so fullscreen does not flip it. */
  mode: 'normal' | 'data'
  /** The per-block wrap toggle, so fullscreen does not silently re-wrap. */
  wrap: boolean
  pageSize: number
}

/** Live back-channel to the DataTable that opened the overlay. */
export interface BlockLandscapeTableHandle {
  snapshot(): BlockLandscapeTable
  setMode(mode: 'normal' | 'data'): void
  setWrap(wrap: boolean): void
}

/**
 * Root element → handle. A WeakMap so a table that unmounts (message edited,
 * session switched) cannot keep the registry alive.
 */
const tableHandles = new WeakMap<Element, BlockLandscapeTableHandle>()

export function registerBlockLandscapeTable(root: Element, handle: BlockLandscapeTableHandle): void {
  tableHandles.set(root, handle)
}

export function unregisterBlockLandscapeTable(root: Element): void {
  tableHandles.delete(root)
}

export function getBlockLandscapeTable(root: Element): BlockLandscapeTableHandle | null {
  return tableHandles.get(root) ?? null
}
