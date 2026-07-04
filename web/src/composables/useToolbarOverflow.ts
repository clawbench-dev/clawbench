import { ref, computed, unref } from 'vue'
import type { ComputedRef, Ref } from 'vue'

/** Default constants matching toolbar CSS */
const DEFAULT_BTN_WIDTH = 26
const DEFAULT_GAP = 6
const MAX_SEARCH_WIDTH = 200

/**
 * Composable for responsive toolbar overflow.
 * Observes the toolbar element width and computes which demotable buttons
 * should be shown inline vs collapsed into a More dropdown.
 *
 * @param getEl - getter for the toolbar container element
 * @param getDemotableIds - getter for ordered list of demotable button IDs
 *   (first = highest priority = collapse last; last = lowest priority = collapse first)
 * @param opts - configuration options
 */
export function useToolbarOverflow(
  getEl: () => HTMLElement | null,
  getDemotableIds: () => string[],
  opts?: {
    /** Fixed-width button size in px (default 26) */
    btnWidth?: number
    /** Gap between buttons in px (default 6) */
    gap?: number
    /** Always-inline button count (not in demotable list); supports reactive ref/computed */
    inlineCount?: number | Ref<number> | ComputedRef<number>
    /** Whether a SearchInput occupies remaining space (default false) */
    hasSearch?: boolean
  },
) {
  const btnWidth = opts?.btnWidth ?? DEFAULT_BTN_WIDTH
  const gap = opts?.gap ?? DEFAULT_GAP
  const hasSearch = opts?.hasSearch ?? false
  const step = btnWidth + gap

  const contentWidth = ref(0)
  let resizeObserver: ResizeObserver | null = null

  const inlineIds = computed(() => {
    const width = contentWidth.value
    if (width <= 0) return getDemotableIds() // not yet measured, show all

    const ids = getDemotableIds()
    if (ids.length === 0) return []

    const inlineCount = unref(opts?.inlineCount ?? 0)
    // Fixed space: always-inline buttons + gaps between all inline items + search
    const fixedBtns = inlineCount
    const searchSpace = hasSearch ? MAX_SEARCH_WIDTH : 0
    // Minimum gaps: between always-inline buttons (N-1 gaps for N buttons)
    const minGaps = Math.max(0, fixedBtns - 1) * gap
    const fixedSpace = fixedBtns * btnWidth + minGaps + searchSpace

    const availableSpace = width - fixedSpace
    if (availableSpace < 0) return []

    const maxInline = Math.floor(availableSpace / step)
    return ids.slice(0, Math.max(0, Math.min(maxInline, ids.length)))
  })

  const collapsedIds = computed(() => {
    const ids = getDemotableIds()
    return ids.slice(inlineIds.value.length)
  })

  function startObserving() {
    stopObserving()
    const el = getEl()
    if (!el) return

    const style = getComputedStyle(el)
    const padLeft = parseFloat(style.paddingLeft) || 0
    const padRight = parseFloat(style.paddingRight) || 0
    contentWidth.value = el.clientWidth - padLeft - padRight

    resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        contentWidth.value = entry.contentRect.width
      }
    })
    resizeObserver.observe(el)
  }

  function stopObserving() {
    resizeObserver?.disconnect()
    resizeObserver = null
  }

  return {
    contentWidth,
    inlineIds,
    collapsedIds,
    startObserving,
    stopObserving,
  }
}
