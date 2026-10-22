import { ref, watch, onScopeDispose, type Ref } from 'vue'

/**
 * Mirror a boolean flag, but only after it has stayed true for `delayMs`.
 *
 * Used to gate skeleton placeholders: swapping real content for a skeleton on
 * every request makes a fast query (a local SQLite aggregate answers in tens of
 * milliseconds) flash grey blocks — the placeholder then costs more attention
 * than the wait it describes. Waiting one beat means only genuinely slow loads
 * show it, while the stale-content window stays short enough that a slow load
 * is never mistaken for a frozen panel.
 *
 * A false source clears the flag immediately (no trailing skeleton after the
 * data lands).
 */
export function useDelayedFlag(source: Ref<boolean>, delayMs = 120): Ref<boolean> {
  const delayed = ref(false)
  let timer: ReturnType<typeof setTimeout> | null = null

  function clear() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  }

  watch(source, (active) => {
    clear()
    if (!active) {
      delayed.value = false
      return
    }
    timer = setTimeout(() => {
      timer = null
      delayed.value = true
    }, delayMs)
  }, { immediate: true })

  onScopeDispose(() => {
    clear()
    delayed.value = false
  })

  return delayed
}
