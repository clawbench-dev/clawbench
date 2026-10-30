import { ref } from 'vue'

/**
 * Full-screen ("landscape") table viewer state.
 *
 * The table header buttons are injected as raw HTML strings and clicked through
 * event delegation from five different containers (chat list, file preview,
 * task detail, task overview, BTW drawer, share page) — there is no Vue
 * instance at the click site to hold state. The overlay therefore lives in
 * module state and a single `<TableLandscape />` instance mounted at each app
 * root renders it, the same way `useDialog()` exposes a module-level store.
 */

export interface TableLandscapeState {
  /** Serialized `<table>` cloned from the source block, annotations stripped. */
  html: string
  /** Wrap mode of the source block — the overlay keeps the per-table choice. */
  wrapClass: 'word-wrap' | 'no-wrap'
  /** Element that opened the overlay; focus returns here when it closes. */
  trigger: HTMLElement | null
}

const state = ref<TableLandscapeState | null>(null)

/** Read-only handle for the overlay component. */
export const tableLandscapeState = state

/**
 * Open the overlay for a `.table-block-wrapper`.
 *
 * @returns false when the wrapper contains no table (nothing was opened).
 */
export function openTableLandscape(wrapper: Element, trigger: HTMLElement | null = null): boolean {
  const table = wrapper.querySelector('table')
  if (!table) return false

  const clone = table.cloneNode(true) as HTMLTableElement
  // The row viewer is a chat-container interaction keyed on `data-row-idx` /
  // `data-table-idx`. The overlay is read-only, so drop the hooks instead of
  // letting a cloned row look like a real row target.
  for (const el of [clone, ...Array.from(clone.querySelectorAll('[data-row-idx], [data-table-idx]'))]) {
    el.removeAttribute('data-row-idx')
    el.removeAttribute('data-table-idx')
  }

  state.value = {
    html: clone.outerHTML,
    wrapClass: wrapper.classList.contains('no-wrap') ? 'no-wrap' : 'word-wrap',
    trigger,
  }
  return true
}

export function closeTableLandscape(): void {
  state.value = null
}

/**
 * Rotate the table 90° only on a touch device held in portrait.
 *
 * A desktop window that merely happens to be narrow must not rotate — a
 * sideways table there is disorienting rather than helpful, and the toolbar
 * button is hidden at that size anyway. `pointer: coarse` is the discriminator
 * (see usePlatformDetect): a touch laptop reporting a fine pointer keeps its
 * upright table.
 */
export function shouldRotateTableLandscape(input: {
  isTouchPrimary: boolean
  width: number
  height: number
}): boolean {
  return input.isTouchPrimary && input.height > input.width
}
