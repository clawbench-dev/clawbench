/**
 * Drag-to-scroll for a horizontally overflowing strip.
 *
 * WHY THIS EXISTS
 *
 * The chat action bar keeps its scrollbar hidden (a visible bar in a compact
 * input row is noise) and only overflows when the pane is too narrow for every
 * button. With the bar hidden, a plain mouse wheel scrolls the PAGE and the
 * buttons past the edge are unreachable — a trackpad swipe or a touch pan works,
 * but a mouse has no way to move the strip sideways. So: press anywhere on the
 * strip and drag left/right to scroll it.
 *
 * Deliberate properties:
 *
 *   - Only when the strip ACTUALLY overflows. A strip that fits must not turn a
 *     press-and-drag into a no-op that still swallows the gesture, and it must
 *     not grow a grab cursor for nothing.
 *   - Touch is skipped: an overflow-x element already pans natively under a
 *     finger, with momentum. Hijacking it would fight the platform gesture.
 *   - Pointer capture happens only once the gesture has become a DRAG, not on
 *     pointerdown. Capturing on pointerdown retargets the compatibility mouse
 *     events too, so a plain click on a button would never reach the button.
 *   - The subsequent `click` on whatever button the drag ended over is not
 *     suppressed here: the app-wide `dragClickGuard` already swallows any click
 *     whose press/release pair travelled more than a few pixels, and this
 *     module's threshold is at least as large. One guard, one owner.
 *
 * Registered by the component that owns the strip; returns a disposer.
 */

/**
 * Movement (px) past which a press becomes a drag.
 *
 * Kept at the mouse threshold of `dragClickGuard` so any gesture that scrolls
 * the strip is also recognised there as a drag (and its click suppressed).
 */
export const DRAG_SCROLL_THRESHOLD = 5

/**
 * Whether a pointer press should begin a drag-scroll.
 *
 * Exported so the rule is testable on its own and any caller applies the same
 * judgement.
 */
export function canDragScroll(
  pointerType: string,
  button: number,
  scrollWidth: number,
  clientWidth: number,
): boolean {
  // Secondary buttons never drag-scroll, and recording them would let a
  // right-drag move the strip under a context menu.
  if (button !== 0) return false
  // Touch pans natively; see the header.
  if (pointerType === 'touch') return false
  // 1px tolerance for sub-pixel rounding, mirroring horizontalWheelScroll.
  return scrollWidth - clientWidth > 1
}

/** Clamp a scrollLeft target into the element's scrollable range. */
export function clampScrollLeft(value: number, scrollWidth: number, clientWidth: number): number {
  const maxScrollLeft = scrollWidth - clientWidth
  if (maxScrollLeft <= 0) return 0
  return Math.max(0, Math.min(maxScrollLeft, value))
}

/**
 * Install drag-to-scroll on `el`. Returns a disposer that removes every
 * listener and drops any in-flight drag state.
 */
export function attachDragScroll(el: HTMLElement): () => void {
  let pointerId: number | null = null
  let startX = 0
  let startScrollLeft = 0
  let dragging = false

  const onPointerMove = (e: PointerEvent) => {
    if (pointerId === null || e.pointerId !== pointerId) return
    const dx = e.clientX - startX
    if (!dragging) {
      if (Math.abs(dx) < DRAG_SCROLL_THRESHOLD) return
      dragging = true
      el.classList.add('is-dragging')
      // Capture only now (see the header): the gesture is unambiguously a drag,
      // so retargeting pointerup to the strip cannot break a button click.
      try {
        el.setPointerCapture?.(pointerId)
      } catch {
        // Capture is an optimisation; window listeners already track the drag.
      }
    }
    el.scrollLeft = clampScrollLeft(startScrollLeft - dx, el.scrollWidth, el.clientWidth)
  }

  const endDrag = (e: PointerEvent) => {
    if (pointerId === null || e.pointerId !== pointerId) return
    const id = pointerId
    pointerId = null
    dragging = false
    el.classList.remove('is-dragging')
    try {
      el.releasePointerCapture?.(id)
    } catch {
      // Ignore: the capture may never have been taken (or was already lost).
    }
    window.removeEventListener('pointermove', onPointerMove)
    window.removeEventListener('pointerup', endDrag)
    window.removeEventListener('pointercancel', endDrag)
  }

  const onPointerDown = (e: PointerEvent) => {
    if (pointerId !== null) return
    if (!canDragScroll(e.pointerType, e.button, el.scrollWidth, el.clientWidth)) return
    pointerId = e.pointerId
    startX = e.clientX
    startScrollLeft = el.scrollLeft
    dragging = false
    // Move/up on window, not el: without capture the pointer can leave the
    // strip mid-gesture and the drag must keep tracking it.
    window.addEventListener('pointermove', onPointerMove)
    window.addEventListener('pointerup', endDrag)
    window.addEventListener('pointercancel', endDrag)
  }

  el.addEventListener('pointerdown', onPointerDown)

  return () => {
    if (pointerId !== null) {
      try {
        el.releasePointerCapture?.(pointerId)
      } catch {
        // Ignore.
      }
    }
    pointerId = null
    dragging = false
    el.classList.remove('is-dragging')
    el.removeEventListener('pointerdown', onPointerDown)
    window.removeEventListener('pointermove', onPointerMove)
    window.removeEventListener('pointerup', endDrag)
    window.removeEventListener('pointercancel', endDrag)
  }
}
