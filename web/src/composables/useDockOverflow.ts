import { ref, computed } from 'vue'
import { readAppliedUIScale, UI_SCALE_EVENT } from '@/utils/uiScale'

/**
 * Dock layout constants, in DESIGN px (must match App.vue's CSS, which writes
 * them as `calc(34 * var(--dp))` and `var(--space-6)`).
 *
 * They are deliberately NOT screen px: the measured dock size comes back from
 * `getBoundingClientRect()` already multiplied by the UI scale, so comparing it
 * against an unscaled 34 would over-count how many buttons fit (at 150% the
 * dock holds 2/3 as many). `scaleLayout()` applies the factor below.
 */
export const DOCK_BTN_SIZE = 34
export const DOCK_GAP = 12
export const DOCK_STEP = DOCK_BTN_SIZE + DOCK_GAP // 46

/**
 * Convert DESIGN px to CSS px at the current scale.
 *
 * Kept as a per-instance helper rather than the shared `designToCss()` because
 * this has to be REACTIVE: `inlineCount` is a `computed`, so a scale change has
 * to invalidate it. `designToCss()` reads the DOM once and cannot be a
 * dependency.
 */
function scaleLayout(designPx: number, scale: number): number {
  return designPx * scale
}

export interface DockOverflowOptions {
  /** Which axis the dock lays out along and measures for overflow. */
  direction?: 'horizontal' | 'vertical'
  /** Size of a single dock button along the layout axis. */
  btnSize?: number
  /** Gap between dock buttons along the layout axis. */
  gap?: number
  /** Number of always-visible primary buttons (reserved before overflow items). */
  primaryCount?: number
}

/**
 * Composable for responsive dock overflow logic.
 * Observes a dock element's size along a given axis and computes how many
 * overflow items can be promoted to inline dock buttons.
 *
 * Pure responsive: space enough → inline in overflowTabs order,
 * space not enough → go to overflow menu. No slot4, no priority.
 *
 * Shared by the horizontal bottom dock (narrow screens) and the vertical
 * wide-screen dock — they only differ in direction, button size/gap and the
 * number of fixed primary buttons.
 *
 * @param getDockEl - getter for the dock element (template ref)
 * @param getOverflowTabs - getter for the list of all available overflow tab IDs
 *   (order matters: first items are promoted first)
 * @param options - layout tuning (direction, sizes, primary count)
 */
export function useDockOverflow(
  getDockEl: () => HTMLElement | null,
  getOverflowTabs: () => string[],
  options: DockOverflowOptions = {},
) {
  const { direction = 'horizontal', primaryCount = 4 } = options

  /**
   * The applied UI scale, kept current through `UI_SCALE_EVENT`.
   *
   * Per instance, not module-level: a module-level ref plus a module-level
   * listener meant the previous test's scale leaked into the next one (the
   * tests had to call `applyUIScale(1)` in `afterEach` to compensate), and the
   * listener was never removed. The dock and the toolbar each have exactly one
   * instance, so the extra subscription costs nothing.
   *
   * Subscribed from `startObserving()` rather than `onMounted()`: this is a
   * plain function called from `setup()`, and callers drive the measure
   * lifecycle themselves (they may start observing long after mount, e.g. when
   * the dock first becomes visible). Tying the listener to that same lifecycle
   * keeps "is it observing?" and "is it listening?" one decision.
   */
  const uiScale = ref(readAppliedUIScale())
  const syncScale = () => { uiScale.value = readAppliedUIScale() }

  // Scale the geometry before any arithmetic: the measured dock size is in CSS
  // px (already scaled), so the per-button footprint has to be too.
  //
  // `computed`, not plain numbers: they read `uiScale`, so a scale change has
  // to invalidate `inlineCount` below. Computed once at setup, a scale change
  // would keep the old footprint while the dock itself grew, and the overflow
  // decision would be wrong until something else re-triggered it.
  const btnSize = computed(() => scaleLayout(options.btnSize ?? DOCK_BTN_SIZE, uiScale.value))
  const gap = computed(() => scaleLayout(options.gap ?? DOCK_GAP, uiScale.value))
  const step = computed(() => btnSize.value + gap.value)
  // Minimum size to hold the fixed primary buttons + the overflow button.
  const minContent = computed(
    () => (primaryCount + 1) * btnSize.value + primaryCount * gap.value,
  )

  const dockContentSize = ref(0)

  let resizeObserver: ResizeObserver | null = null
  let settleRaf = 0

  const isVertical = direction === 'vertical'

  /** How many overflow items can be inline given the current dock size */
  const inlineCount = computed(() => {
    const size = dockContentSize.value
    if (size <= 0) return 0
    const remaining = size - minContent.value
    if (remaining < 0) return 0
    return Math.min(Math.floor(remaining / step.value), getOverflowTabs().length)
  })

  /** Overflow tabs shown inline in the dock */
  const inlineOverflowTabs = computed(() => getOverflowTabs().slice(0, inlineCount.value))

  /** Overflow tabs remaining in the popup */
  const popupOverflowTabs = computed(() => getOverflowTabs().slice(inlineCount.value))

  /** When popup has exactly 1 item, show it directly instead of overflow menu */
  const singleDirectTab = computed(() =>
    popupOverflowTabs.value.length === 1 ? popupOverflowTabs.value[0] : null
  )

  /** Whether the overflow button should be shown (popup has >1 items) */
  const showOverflowButton = computed(() => popupOverflowTabs.value.length > 1)

  /** All overflow tabs that are inline (promoted + singleDirect) */
  const allInlineOverflowTabs = computed(() => {
    const tabs = [...inlineOverflowTabs.value]
    if (singleDirectTab.value) tabs.push(singleDirectTab.value)
    return tabs
  })

  /** Total number of visible dock buttons (for indicator index bound) */
  const totalDockButtons = computed(() => {
    return primaryCount + allInlineOverflowTabs.value.length + (showOverflowButton.value ? 1 : 0)
  })

  /** Measure the dock element's content size along the layout axis. */
  function measureSize(el: HTMLElement): number {
    const style = getComputedStyle(el)
    if (isVertical) {
      return el.clientHeight - (parseFloat(style.paddingTop) || 0) - (parseFloat(style.paddingBottom) || 0)
    }
    return el.clientWidth - (parseFloat(style.paddingLeft) || 0) - (parseFloat(style.paddingRight) || 0)
  }

  /** Start observing dock element size. Call in onMounted. Idempotent. */
  function startObserving() {
    stopObserving()
    // Re-read before the first measure: the applier may have run between setup
    // and this call, and the dock would otherwise be sized against a stale
    // factor until the next UI_SCALE_EVENT.
    syncScale()
    if (typeof window !== 'undefined') window.addEventListener(UI_SCALE_EVENT, syncScale)
    const el = getDockEl()
    if (!el) return

    // Measure the settled size once layout has completed. A single nextTick can
    // run before the browser has reflowed (fonts, CSS variables, CSS zoom on
    // <html> all apply around the same frame), and ResizeObserver may not re-fire
    // for the final size under CSS zoom. Two rAF frames guarantee the first
    // layout is done so overflow is computed from the true available size rather
    // than a transient too-small value (which would wrongly collapse items into
    // the overflow button on first open).
    const applyMeasure = () => {
      const measured = measureSize(el)
      if (measured > 0) dockContentSize.value = measured
    }
    // Initial measurement — skip if hidden (display:none → clientHeight/Width=0)
    // to preserve the last known good size
    applyMeasure()
    settleRaf = requestAnimationFrame(() => {
      applyMeasure()
      settleRaf = requestAnimationFrame(() => {
        applyMeasure()
      })
    })

    resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        // Skip size=0 (element hidden via display:none / v-show) —
        // preserve last good size; observer fires again when visible
        const size = isVertical ? entry.contentRect.height : entry.contentRect.width
        if (size > 0) {
          dockContentSize.value = size
        }
      }
    })
    resizeObserver.observe(el)
  }

  /** Stop observing. Call in onBeforeUnmount. */
  function stopObserving() {
    cancelAnimationFrame(settleRaf)
    settleRaf = 0
    resizeObserver?.disconnect()
    resizeObserver = null
    if (typeof window !== 'undefined') window.removeEventListener(UI_SCALE_EVENT, syncScale)
  }

  return {
    dockContentSize,
    inlineOverflowTabs,
    popupOverflowTabs,
    singleDirectTab,
    showOverflowButton,
    allInlineOverflowTabs,
    totalDockButtons,
    startObserving,
    stopObserving,
  }
}
