import { ref, computed, unref } from 'vue'
import type { ComputedRef, Ref } from 'vue'
import { readAppliedUIScale, UI_SCALE_EVENT } from '@/utils/uiScale'

/**
 * Default constants matching toolbar CSS, in DESIGN px.
 *
 * They must be scaled before being compared with a measured width: the toolbar
 * is measured with `getBoundingClientRect()`/`clientWidth`, which already
 * include the UI scale, while these are the numbers from the stylesheet's
 * design units. Comparing them directly over-counts how many buttons fit.
 */
const DEFAULT_BTN_WIDTH = 26
const DEFAULT_GAP = 6
const MAX_SEARCH_WIDTH = 140

/**
 * The applied UI scale, kept current through `UI_SCALE_EVENT`.
 *
 * Module-level and reactive: `inlineIds` is a `computed`, so a plain read would
 * leave the button count pinned to whatever the scale was at setup.
 */
const uiScale = ref(1)
if (typeof window !== 'undefined') {
  const sync = () => { uiScale.value = readAppliedUIScale() }
  sync()
  window.addEventListener(UI_SCALE_EVENT, sync)
}

/** Convert DESIGN px to CSS px at the current scale. */
function scaleLayout(designPx: number): number {
  return designPx * uiScale.value
}

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
  const hasSearch = opts?.hasSearch ?? false

  const contentWidth = ref(0)
  let resizeObserver: ResizeObserver | null = null

  const inlineIds = computed(() => {
    const width = contentWidth.value
    if (width <= 0) return getDemotableIds() // not yet measured, show all

    const ids = getDemotableIds()
    if (ids.length === 0) return []

    // Scaled inside the computed so a scale change invalidates it. The measured
    // `width` is already in CSS px; these constants are design px.
    const btnWidth = scaleLayout(opts?.btnWidth ?? DEFAULT_BTN_WIDTH)
    const gap = scaleLayout(opts?.gap ?? DEFAULT_GAP)
    const step = btnWidth + gap

    const inlineCount = unref(opts?.inlineCount ?? 0)
    // Fixed space: always-inline buttons + gaps between all inline items + search
    const fixedBtns = inlineCount
    const searchSpace = hasSearch ? scaleLayout(MAX_SEARCH_WIDTH) : 0
    // Gaps: between always-inline buttons (N-1), plus 1 between last inline and first demotable
    const minGaps = Math.max(0, fixedBtns - 1) * gap
    const fixedSpace = fixedBtns * btnWidth + minGaps + searchSpace
    // Deduct the gap before the first demotable button (it's not in step which covers gap *after* each demotable)
    const bridgeGap = ids.length > 0 ? gap : 0

    const availableSpace = width - fixedSpace - bridgeGap
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
