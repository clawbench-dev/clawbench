import { ref } from 'vue'

export interface ListNavOptions {
  /** Number of list items (reactive-safe: pass a getter that reads reactive state). */
  getCount: () => number
  /** Called on Enter/confirm with the resolved item index (falls back to 0). */
  onConfirm: (index: number) => void
  /** Called after the active index changes (e.g. to scroll the item into view). */
  onActiveChange?: (index: number) => void
  /** Whether moving past the ends wraps around (default true). */
  wrap?: boolean
}

/**
 * Keyboard ↑/↓ + Enter navigation for a list of selectable items.
 *
 * Active index is `-1` (nothing highlighted) until the first arrow press.
 * ArrowDown from an unset index selects the first item; ArrowUp selects the last.
 * Enter confirms the highlighted item, or the first item if none is highlighted.
 */
export function useListNav(options: ListNavOptions) {
  const { getCount, onConfirm, onActiveChange, wrap = true } = options

  const activeIndex = ref(-1)

  function moveTo(index: number) {
    const n = getCount()
    if (n <= 0) {
      activeIndex.value = -1
      return
    }
    const clamped = Math.max(0, Math.min(n - 1, index))
    activeIndex.value = clamped
    onActiveChange?.(clamped)
  }

  function down() {
    const n = getCount()
    if (n <= 0) return
    if (activeIndex.value < 0) {
      moveTo(0)
      return
    }
    const next = activeIndex.value + 1
    moveTo(wrap ? next % n : Math.min(next, n - 1))
  }

  function up() {
    const n = getCount()
    if (n <= 0) return
    if (activeIndex.value < 0) {
      moveTo(n - 1)
      return
    }
    const prev = activeIndex.value - 1
    moveTo(wrap ? (prev + n) % n : Math.max(prev, 0))
  }

  function confirm() {
    const n = getCount()
    if (n <= 0) return
    const index = activeIndex.value >= 0 && activeIndex.value < n ? activeIndex.value : 0
    onConfirm(index)
  }

  /** Clear the highlight (call when the list contents change). */
  function reset() {
    activeIndex.value = -1
  }

  function setActive(index: number) {
    moveTo(index)
  }

  return { activeIndex, down, up, confirm, reset, setActive }
}
