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
 * Convert DESIGN px to CSS px at the current scale.
 *
 * Per-instance and reactive (takes the scale as an argument) rather than the
 * shared `designToCss()`: `inlineIds` is a `computed`, so a scale change has to
 * invalidate it, and `designToCss()` reads the DOM once so it cannot be a
 * dependency.
 */
function scaleLayout(designPx: number, scale: number): number {
  return designPx * scale
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

  /**
   * The applied UI scale, kept current through `UI_SCALE_EVENT`.
   *
   * Per instance, not module-level: the module-level ref plus its module-level
   * listener leaked the previous test's scale into the next one and was never
   * removed. Each toolbar is a single instance, so the extra subscription is
   * free.
   *
   * Subscribed from `startObserving()`: this is a plain function called from
   * `setup()`, and the callers own the measure lifecycle. Tying the listener to
   * the same lifecycle makes "observing" and "listening" a single state.
   */
  const uiScale = ref(readAppliedUIScale())
  const syncScale = () => { uiScale.value = readAppliedUIScale() }

  const inlineIds = computed(() => {
    const width = contentWidth.value
    if (width <= 0) return getDemotableIds() // not yet measured, show all

    const ids = getDemotableIds()
    if (ids.length === 0) return []

    // Scaled inside the computed so a scale change invalidates it. The measured
    // `width` is already in CSS px; these constants are design px.
    const scale = uiScale.value
    const btnWidth = scaleLayout(opts?.btnWidth ?? DEFAULT_BTN_WIDTH, scale)
    const gap = scaleLayout(opts?.gap ?? DEFAULT_GAP, scale)
    const step = btnWidth + gap

    const inlineCount = unref(opts?.inlineCount ?? 0)
    // Fixed space: always-inline buttons + gaps between all inline items + search
    const fixedBtns = inlineCount
    const searchSpace = hasSearch ? scaleLayout(MAX_SEARCH_WIDTH, scale) : 0
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
    // Re-read before the first measure so the toolbar is not sized against a
    // factor captured at setup (settings load / project switch can change it
    // in between).
    syncScale()
    if (typeof window !== 'undefined') window.addEventListener(UI_SCALE_EVENT, syncScale)
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
    if (typeof window !== 'undefined') window.removeEventListener(UI_SCALE_EVENT, syncScale)
  }

  return {
    contentWidth,
    inlineIds,
    collapsedIds,
    startObserving,
    stopObserving,
  }
}
