/**
 * v-long-press directive — mobile long-press gesture detection
 *
 * Usage: <div v-long-press="(e) => onLongPress(item, e)">
 *
 * - Fires the callback after 450ms of sustained touch (no move beyond threshold)
 * - Calls e.preventDefault() on the touchend that follows a long-press,
 *   preventing the synthetic click from firing (no click-through)
 * - Self-contained per element: no shared state, no stale DOM references
 */

import type { DirectiveBinding } from 'vue'

const LONG_PRESS_MS = 450
const MOVE_THRESHOLD_PX = 10

function mounted(el: HTMLElement, binding: DirectiveBinding) {
  let timer: number | null = null
  let fired = false
  let startX = 0
  let startY = 0

  function onTouchStart(e: TouchEvent) {
    fired = false
    const touch = e.touches[0]
    startX = touch.clientX
    startY = touch.clientY
    timer = window.setTimeout(() => {
      timer = null
      fired = true
      binding.value?.(e)
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
  }

  function onTouchCancel() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
    fired = false
  }

  // touchstart/touchmove can be passive (no preventDefault needed)
  el.addEventListener('touchstart', onTouchStart, { passive: true })
  el.addEventListener('touchmove', onTouchMove, { passive: true })
  // touchend needs preventDefault to block click synthesis
  el.addEventListener('touchend', onTouchEnd, { passive: false })
  // touchcancel just cleans up — no preventDefault needed
  el.addEventListener('touchcancel', onTouchCancel, { passive: true })

  ;(el as any)._longPress_cleanup = () => {
    el.removeEventListener('touchstart', onTouchStart)
    el.removeEventListener('touchmove', onTouchMove)
    el.removeEventListener('touchend', onTouchEnd)
    el.removeEventListener('touchcancel', onTouchCancel)
    if (timer) clearTimeout(timer)
  }
}

function unmounted(el: HTMLElement) {
  ;(el as any)._longPress_cleanup?.()
  delete (el as any)._longPress_cleanup
}

export const LongPressDirective = { mounted, unmounted }
