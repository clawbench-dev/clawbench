/**
 * v-long-press directive — mobile long-press gesture detection
 *
 * Usage: <div v-long-press="(e) => onLongPress(item, e)">
 *
 * - Fires the callback after 450ms of sustained touch (no move beyond threshold)
 * - Adds `long-pressing` CSS class to the element while long-press is active,
 *   so the UI can show visual feedback (highlight background, etc.)
 * - Calls e.preventDefault() on the touchend that follows a long-press,
 *   preventing the synthetic click from firing (no click-through)
 * - Blocks iOS Safari contextmenu (system callout / text selection) during long-press
 * - Self-contained per element: no shared state, no stale DOM references
 * - Supports binding updates: if the callback changes (e.g. v-for re-renders),
 *   the latest callback is used when the long-press fires
 */

import type { DirectiveBinding } from 'vue'

const LONG_PRESS_MS = 450
const MOVE_THRESHOLD_PX = 10
const LONG_PRESSING_CLASS = 'long-pressing'

function mounted(el: HTMLElement, binding: DirectiveBinding) {
  // Store binding on the element so the callback always reads the latest value.
  // This is critical for v-for loops: when the list re-renders and Vue patches
  // the same DOM node (same key), the directive's mounted() is NOT called again,
  // but the callback closure may capture a stale item reference. By storing the
  // binding on el and reading it at fire-time, we always get the up-to-date callback.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(el as any)._longPress_binding = binding

  let timer: number | null = null
  let fired = false
  let startX = 0
  let startY = 0

  function onTouchStart(e: TouchEvent) {
    fired = false
    const touch = e.touches[0]
    startX = touch.clientX
    startY = touch.clientY
    // Capture the data-session-id (or data-path etc.) at touchstart time so the
    // long-press callback can identify the row that was ACTUALLY pressed, even if
    // the DOM shifts / TransitionGroup reorders between touchstart and the 450ms
    // fire. Without this, reading the attribute at fire-time can pick up a
    // different (reused/moved) DOM node, causing the action to hit the wrong item.
    const idAttr = (el.getAttribute('data-session-id') || el.getAttribute('data-path') || '') as string
    timer = window.setTimeout(() => {
      timer = null
      fired = true
      el.classList.add(LONG_PRESSING_CLASS)
      // Read the latest binding value at fire-time, not at mount-time
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      ;(el as any)._longPress_binding?.value?.(e, idAttr)
    }, LONG_PRESS_MS)
  }

  function onTouchMove(e: TouchEvent) {
    if (!timer) return
    const touch = e.touches[0]
    if (
      Math.abs(touch.clientX - startX) > MOVE_THRESHOLD_PX ||
      Math.abs(touch.clientY - startY) > MOVE_THRESHOLD_PX
    ) {
      clearTimeout(timer)
      timer = null
    }
  }

  function onTouchEnd(e: TouchEvent) {
    if (timer) {
      // Short tap — clear timer, let click fire normally
      clearTimeout(timer)
      timer = null
      return
    }
    // Long-press was fired — prevent the synthetic click
    if (fired) {
      e.preventDefault()
    }
    fired = false
    el.classList.remove(LONG_PRESSING_CLASS)
  }

  function onTouchCancel() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    fired = false
    el.classList.remove(LONG_PRESSING_CLASS)
  }

  // iOS Safari fires a contextmenu event ~500ms into a long-press, which
  // triggers the system callout (copy/define/share). Prevent it when our
  // long-press was recognized so the custom action takes priority.
  function onContextMenu(e: Event) {
    if (fired) {
      e.preventDefault()
    }
  }

  // touchstart/touchmove can be passive (no preventDefault needed)
  el.addEventListener('touchstart', onTouchStart, { passive: true })
  el.addEventListener('touchmove', onTouchMove, { passive: true })
  // touchend needs preventDefault to block click synthesis
  el.addEventListener('touchend', onTouchEnd, { passive: false })
  // touchcancel just cleans up — no preventDefault needed
  el.addEventListener('touchcancel', onTouchCancel, { passive: true })
  // Block iOS system callout when long-press fires
  el.addEventListener('contextmenu', onContextMenu, { passive: false })

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(el as any)._longPress_cleanup = () => {
    el.removeEventListener('touchstart', onTouchStart)
    el.removeEventListener('touchmove', onTouchMove)
    el.removeEventListener('touchend', onTouchEnd)
    el.removeEventListener('touchcancel', onTouchCancel)
    el.removeEventListener('contextmenu', onContextMenu)
    if (timer) clearTimeout(timer)
    el.classList.remove(LONG_PRESSING_CLASS)
  }
}

function updated(el: HTMLElement, binding: DirectiveBinding) {
  // Sync the latest binding so the callback always references the current closure
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(el as any)._longPress_binding = binding
}

function unmounted(el: HTMLElement) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(el as any)._longPress_cleanup?.()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (el as any)._longPress_cleanup
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (el as any)._longPress_binding
}

export const LongPressDirective = { mounted, updated, unmounted }
