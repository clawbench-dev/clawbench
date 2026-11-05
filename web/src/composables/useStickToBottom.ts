import { nextTick, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

/** How close to the bottom still counts as "reading the latest line". */
const SLACK_PX = 48

/**
 * Keeps a scrolling pane on its newest line, the way a chat or a live
 * transcript is read: follow while the reader is at the bottom, stop the
 * moment they scroll up to re-read, resume when they come back down.
 *
 * The part that is easy to get wrong — and the reason this is one
 * implementation rather than a copy per pane — is the two states a pane can
 * be in with no scroll happening at all:
 *
 * - **Mounted with content already in it.** A transcript tab opened in the
 *   middle of a meeting re-creates the list with the whole conversation in it.
 *   There is no "new line" event to follow, so the pane would start at the top
 *   of the meeting — and because following only ever scrolls a pane that is
 *   *already* at the bottom, it would never catch up.
 * - **Hidden.** A `v-show` tab or a closed side panel lays the box out at zero
 *   height, where `scrollTop`, `scrollHeight` and `clientHeight` all read 0.
 *   "The distance to the bottom" is then 0 — a lie. Measuring there also marks
 *   a reader who had scrolled up as "at the bottom", so the pane would jump to
 *   the end the moment it reappeared.
 *
 * `signature` is whatever changes when new content arrives: a row count, or a
 * row count plus the length of the last line for a block that grows in place
 * while the speaker keeps talking.
 */
export function useStickToBottom(
  target: Ref<HTMLElement | null>,
  signature: () => unknown,
): { onScroll: () => void } {
  /** Whether the newest line should be kept in view. */
  const following = ref(true)

  function atBottom(el: HTMLElement): boolean {
    return el.scrollHeight - el.scrollTop - el.clientHeight <= SLACK_PX
  }

  function scrollToBottom(): void {
    const el = target.value
    if (el) el.scrollTop = el.scrollHeight
  }

  /**
   * A real scroll — the reader's, or the one we just performed — is the only
   * trustworthy "where is the pane now": it fires after layout, so it also
   * covers content that grew between two frames.
   */
  function onScroll(): void {
    const el = target.value
    if (!el || el.clientHeight === 0) return
    following.value = atBottom(el)
  }

  // `pre` flush: the measurement describes where the reader was *before* the
  // new line was laid out, which is the position the decision is about.
  watch(
    signature,
    async () => {
      const el = target.value
      if (!el) return
      // Hidden: keep the remembered state instead of reading a fake "at the
      // bottom" from a box with no geometry. The observer below takes over
      // when the pane is shown again.
      if (el.clientHeight > 0) following.value = atBottom(el)
      if (!following.value) return
      await nextTick()
      scrollToBottom()
    },
    { flush: 'pre' },
  )

  let resizeObserver: ResizeObserver | null = null

  onMounted(() => {
    // Content that was already there when the pane mounted.
    if (following.value) void nextTick().then(scrollToBottom)
    if (typeof ResizeObserver === 'undefined') return
    const el = target.value
    if (!el) return
    resizeObserver = new ResizeObserver(() => {
      const box = target.value
      // Zero height means "not laid out" (still hidden), not "fits on screen".
      if (!box || box.clientHeight === 0) return
      // Also covers a pane that was resized (window, dock width): while
      // following, the latest line stays in view.
      if (following.value) scrollToBottom()
    })
    resizeObserver.observe(el)
  })

  onBeforeUnmount(() => {
    resizeObserver?.disconnect()
    resizeObserver = null
  })

  return { onScroll }
}
