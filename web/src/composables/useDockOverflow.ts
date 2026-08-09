import { ref, computed } from 'vue'

/** Dock layout constants (must match App.vue CSS) */
export const DOCK_BTN_WIDTH = 34
export const DOCK_GAP = 12
export const DOCK_STEP = DOCK_BTN_WIDTH + DOCK_GAP // 46
const PRIMARY_COUNT = 4 // chat, browse, view, history

/** Minimum dock content width: 4 primary + overflow_btn = 5 buttons, 4 gaps = 218px */
const MIN_DOCK_CONTENT_WIDTH = 5 * DOCK_BTN_WIDTH + 4 * DOCK_GAP

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
  const dockContentWidth = ref(0)

  let resizeObserver: ResizeObserver | null = null

  /** How many overflow items can be inline given the current dock width */
  const inlineCount = computed(() => {
    const width = dockContentWidth.value
    if (width <= 0) return 0
    const remaining = width - MIN_DOCK_CONTENT_WIDTH
    if (remaining < 0) return 0
    return Math.min(Math.floor(remaining / DOCK_STEP), getOverflowTabs().length)
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
    return PRIMARY_COUNT + allInlineOverflowTabs.value.length + (showOverflowButton.value ? 1 : 0)
  })

  /** Start observing dock element size. Call in onMounted. Idempotent. */
  function startObserving() {
    stopObserving()
    const el = getDockEl()
    if (!el) return

    // Initial measurement — skip if hidden (display:none → clientWidth=0)
    // to preserve the last known good width
    const measured = el.clientWidth - (parseFloat(getComputedStyle(el).paddingLeft) || 0)
      - (parseFloat(getComputedStyle(el).paddingRight) || 0)
    if (measured > 0) dockContentWidth.value = measured

    resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        // Skip width=0 (element hidden via display:none / v-show) —
        // preserve last good width; observer fires again when visible
        if (entry.contentRect.width > 0) {
          dockContentWidth.value = entry.contentRect.width
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
    dockContentWidth,
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
