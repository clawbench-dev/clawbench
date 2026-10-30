import { shallowRef } from 'vue'
import { gt } from '@/composables/useLocale'

/**
 * Full-screen ("landscape") viewer state for Markdown blocks — tables AND code
 * blocks.
 *
 * The block header buttons are injected as raw HTML strings and clicked through
 * event delegation from five different containers (chat list, file preview,
 * task detail, task overview, BTW drawer, share page) — there is no Vue
 * instance at the click site to hold state. The overlay therefore lives in
 * module state and a single `<BlockLandscape />` instance mounted at each app
 * root renders it, the same way `useDialog()` exposes a module-level store.
 */

export interface BlockLandscapeState {
  /** Serialized clone of the whole `.table-block-wrapper` / `.code-block-wrapper`. */
  html: string
  /** Element that opened the overlay; focus returns here when it closes. */
  trigger: HTMLElement | null
  /**
   * The ORIGINAL wrapper. Navigation clicks inside the overlay are replayed
   * onto the matching element here, so the container's own handlers (file path,
   * commit, external link) run exactly as they do outside the overlay.
   */
  source: Element
}

// `shallowRef`: the state carries live DOM nodes, which must not be made
// reactive.
const state = shallowRef<BlockLandscapeState | null>(null)

/** Read-only handle for the overlay component. */
export const blockLandscapeState = state

/** Lucide `x`, as an inline SVG string (same shape as the copy/check glyphs). */
const CLOSE_ICON_SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>'

/**
 * Open the overlay for a `.table-block-wrapper` or a `.code-block-wrapper`.
 *
 * The WHOLE wrapper is cloned — header included — so copy, the copy-format
 * menu and the wrap toggle keep working untouched inside the overlay. The one
 * control that must change is the landscape button itself: inside the overlay
 * it becomes the close button (same slot, same styling, different action).
 *
 * @returns false when the wrapper holds no renderable block (nothing opened).
 */
export function openBlockLandscape(wrapper: Element, trigger: HTMLElement | null = null): boolean {
  if (!wrapper.querySelector('table, pre')) return false

  const isCode = wrapper.classList.contains('code-block-wrapper')
  const clone = wrapper.cloneNode(true) as HTMLElement
  const landscapeBtn = clone.querySelector('.table-block-landscape-btn, .code-block-landscape-btn')
  if (landscapeBtn) {
    const closeLabel = gt(isCode ? 'codeBlock.landscapeClose' : 'tableBlock.landscapeClose')
    landscapeBtn.setAttribute('data-action', 'landscape-close')
    landscapeBtn.setAttribute('title', closeLabel)
    landscapeBtn.setAttribute('aria-label', closeLabel)
    landscapeBtn.innerHTML = CLOSE_ICON_SVG
  }

  // No elements are added or removed: the clone keeps a 1:1 element structure
  // with the source, which is what lets a navigation click be replayed by index
  // path (see BlockLandscape.vue).
  state.value = { html: clone.outerHTML, trigger, source: wrapper }
  return true
}

export function closeBlockLandscape(): void {
  state.value = null
}

/**
 * Rotate the block 90° only on a touch device held in portrait.
 *
 * A desktop window that merely happens to be narrow must not rotate — a
 * sideways table there is disorienting rather than helpful, and the toolbar
 * button is hidden at that size anyway. `pointer: coarse` is the discriminator
 * (see usePlatformDetect): a touch laptop reporting a fine pointer keeps its
 * upright block.
 */
export function shouldRotateBlockLandscape(input: {
  isTouchPrimary: boolean
  width: number
  height: number
}): boolean {
  return input.isTouchPrimary && input.height > input.width
}
