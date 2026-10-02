/**
 * useChangeNav — prev/next navigation over a list of change positions.
 *
 * Surface-agnostic: the caller supplies the ordered target positions (markdown:
 * block tops in px; code: line numbers) and a scrollTo callback. Index state,
 * clamping and scroll-derived highlighting live here so both preview surfaces
 * behave identically.
 *
 * `resetKey` (optional) is the current file path: a different file's changes are
 * an unrelated list, so the index resets to its first change instead of carrying
 * the previous file's position (which would read e.g. "3/4" on a 4-change file
 * and make the next jump start from the wrong change).
 */
import { ref, watch, type Ref } from 'vue'

export function useChangeNav(
  targets: Ref<number[]>,
  scrollTo: (index: number) => void,
  resetKey?: Ref<unknown>,
) {
  const index = ref(0)
  const count = ref(targets.value.length)

  watch(targets, (list) => {
    count.value = list.length
    if (index.value >= list.length) index.value = Math.max(0, list.length - 1)
  }, { deep: true })

  if (resetKey) {
    watch(resetKey, () => { index.value = 0 })
  }

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
