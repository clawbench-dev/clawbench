import { ref, computed } from 'vue'

/** Dock layout constants (must match App.vue CSS) */
export const DOCK_BTN_WIDTH = 34
export const DOCK_GAP = 12
export const DOCK_STEP = DOCK_BTN_WIDTH + DOCK_GAP // 46
const PRIMARY_COUNT = 3 // chat, browse, history

/**
 * Minimum dock content width: 3 primary + overflow_btn = 4 buttons, 3 gaps
 * = 4 * 34 + 3 * 12 = 172px
 */
const MIN_DOCK_CONTENT_WIDTH = 4 * DOCK_BTN_WIDTH + 3 * DOCK_GAP

/**
 * Composable for responsive dock overflow logic.
 * Observes the dock element width and computes how many overflow items
 * can be promoted to inline dock buttons.
 *
 * Pure responsive: width enough → inline in overflowTabs order,
 * width not enough → go to overflow menu. No slot4, no priority.
 *
 * @param getDockEl - getter for the .bottom-dock element (template ref)
 * @param getOverflowTabs - getter for the list of all available overflow tab IDs
 *   (order matters: first items are promoted first)
 */
export function useDockOverflow(
  getDockEl: () => HTMLElement | null,
  getOverflowTabs: () => string[],
) {
  /** Available content width inside .bottom-dock (padding excluded) */
  const dockContentWidth = ref(0)

  let resizeObserver: ResizeObserver | null = null

  /**
   * Number of overflow items that can be shown as inline dock buttons
   * given the current dock width.
   */
  const inlineOverflowCount = computed(() => {
    const width = dockContentWidth.value
    if (width <= 0) return 0

    const remainingSpace = width - MIN_DOCK_CONTENT_WIDTH
    if (remainingSpace < 0) return 0

    // Each inline overflow tab needs DOCK_STEP (46px) more space
    const maxInline = Math.floor(remainingSpace / DOCK_STEP)

    // Cannot exceed total available overflow tabs
    const available = getOverflowTabs().length
    return Math.max(0, Math.min(maxInline, available))
  })

  /** Overflow tabs shown inline in the dock (in overflowTabs order) */
  const inlineOverflowTabs = computed(() => {
    return getOverflowTabs().slice(0, inlineOverflowCount.value)
  })

  /** Overflow tabs remaining in the popup (not promoted to inline) */
  const popupOverflowTabs = computed(() => {
    return getOverflowTabs().slice(inlineOverflowCount.value)
  })

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
    let count = PRIMARY_COUNT + inlineOverflowTabs.value.length
    if (singleDirectTab.value) count += 1
    if (showOverflowButton.value) count += 1
    return count
  })

  /** Start observing dock element size. Call in onMounted. Idempotent. */
  function startObserving() {
    stopObserving() // Clean up any prior observer
    const el = getDockEl()
    if (!el) return

    // Initial measurement
    const style = getComputedStyle(el)
    const padLeft = parseFloat(style.paddingLeft) || 0
    const padRight = parseFloat(style.paddingRight) || 0
    dockContentWidth.value = el.clientWidth - padLeft - padRight

    resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        dockContentWidth.value = entry.contentRect.width
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
    dockContentWidth,
    inlineOverflowCount,
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
