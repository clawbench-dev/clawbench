import { ref, computed } from 'vue'

/** Dock layout constants (must match App.vue CSS) */
export const DOCK_BTN_SIZE = 34
export const DOCK_GAP = 12
export const DOCK_STEP = DOCK_BTN_SIZE + DOCK_GAP // 46

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
  const { direction = 'horizontal', btnSize = DOCK_BTN_SIZE, gap = DOCK_GAP, primaryCount = 4 } = options
  const step = btnSize + gap
  // Minimum size to hold the fixed primary buttons + the overflow button.
  const minContent = (primaryCount + 1) * btnSize + primaryCount * gap

  const dockContentSize = ref(0)

  let resizeObserver: ResizeObserver | null = null

  const isVertical = direction === 'vertical'

  /** How many overflow items can be inline given the current dock size */
  const inlineCount = computed(() => {
    const size = dockContentSize.value
    if (size <= 0) return 0
    const remaining = size - minContent
    if (remaining < 0) return 0
    return Math.min(Math.floor(remaining / step), getOverflowTabs().length)
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
    const el = getDockEl()
    if (!el) return

    // Initial measurement — skip if hidden (display:none → clientHeight/Width=0)
    // to preserve the last known good size
    const measured = measureSize(el)
    if (measured > 0) dockContentSize.value = measured

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
    resizeObserver?.disconnect()
    resizeObserver = null
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
