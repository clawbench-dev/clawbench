/**
 * useChangeNav — prev/next navigation over a list of change positions.
 *
 * Surface-agnostic: the caller supplies the ordered target positions (markdown:
 * block tops in px; code: line numbers) and a scrollTo callback. Index state,
 * clamping and scroll-derived highlighting live here so both preview surfaces
 * behave identically.
 */
import { ref, watch, type Ref } from 'vue'

export function useChangeNav(targets: Ref<number[]>, scrollTo: (index: number) => void) {
  const index = ref(0)
  const count = ref(targets.value.length)

  watch(targets, (list) => {
    count.value = list.length
    if (index.value >= list.length) index.value = Math.max(0, list.length - 1)
  }, { deep: true })

  function next() {
    if (index.value >= targets.value.length - 1) return
    index.value += 1
    scrollTo(index.value)
  }

  function prev() {
    if (index.value <= 0) return
    index.value -= 1
    scrollTo(index.value)
  }

  /** Highlight the change whose position is the last one at or above `pos`. */
  function syncIndexFromScroll(pos: number) {
    const list = targets.value
    if (list.length === 0) return
    let found = 0
    for (let i = 0; i < list.length; i++) {
      if (list[i] <= pos) found = i
      else break
    }
    index.value = found
  }

  return { index, count, next, prev, syncIndexFromScroll }
}
