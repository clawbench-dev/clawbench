/**
 * Global drag-vs-click guard.
 *
 * A drag that selects text inside a clickable row still ends with a `click` on
 * that row: mousedown and mouseup share the row as their common ancestor, so the
 * browser dispatches a click even though the user was selecting text. Every
 * clickable row containing selectable text therefore had to guard its own
 * handler, and only four ever did — the rest fired their action (navigate, open
 * a dialog, switch branch) whenever the user tried to copy a name.
 *
 * Rather than repeat that guard at ~50 call sites (and forget it at the 51st),
 * this installs ONE document-level capture listener: a pointer that travelled
 * more than a few pixels between press and release was a drag, not a click, so
 * the click is swallowed before it reaches the row.
 *
 * Deliberate properties:
 *
 *   - The distance is measured between the PRESS position and the `click`
 *     event's own coordinates. A click event carries the release coordinates,
 *     so no separate pointerup bookkeeping is needed — and none should be added:
 *     overwriting the press position at release would make every distance zero
 *     and silently disable the guard.
 *   - Only USER clicks are suppressed. Keyboard activation
 *     (`useMenuKeyboard` → `el.click()`) dispatches a synthetic click with
 *     `detail === 0`; a real click carries the click count (1, 2, …). That is
 *     the DOM-defined discriminator, and unlike `isTrusted` it is observable in
 *     tests (a dispatched event is always untrusted) and correct in production.
 *     The press record is also cleared on every click, so a stale position can
 *     never suppress a later click.
 *   - The threshold is per pointer type. A finger jitters during a tap, so touch
 *     gets the platform's usual touch slop; a mouse does not.
 *
 * Registered once from App.vue; returns a disposer.
 */

/** Movement (px) past which a press/release pair is a drag rather than a click. */
export const DRAG_CLICK_THRESHOLD_MOUSE = 5
/** Touch needs a wider window: a tap routinely drifts a few pixels. */
export const DRAG_CLICK_THRESHOLD_TOUCH = 10

interface PressRecord {
  x: number
  y: number
  pointerType: string
}

/** The pending press, or null when there is none. */
let lastPress: PressRecord | null = null

/**
 * Whether a release at `up` is far enough from a press at `down` to be a drag.
 *
 * Exported so the rule is testable on its own and any caller can apply the same
 * judgement.
 */
export function isDragRelease(
  down: { x: number; y: number; pointerType?: string },
  up: { x: number; y: number },
): boolean {
  const threshold = down.pointerType === 'touch'
    ? DRAG_CLICK_THRESHOLD_TOUCH
    : DRAG_CLICK_THRESHOLD_MOUSE
  return Math.abs(up.x - down.x) > threshold || Math.abs(up.y - down.y) > threshold
}

/** Record a press. Ignored for non-primary buttons. */
export function recordPress(x: number, y: number, pointerType: string, button = 0): void {
  // A secondary button (right/middle) never produces the row-activating click we
  // care about, and recording it would let a right-drag suppress a later left
  // click.
  if (button !== 0) return
  lastPress = { x, y, pointerType }
}

/** Drop any pending press (pointer cancelled, or the guard is being torn down). */
export function clearPress(): void {
  lastPress = null
}

/**
 * Decide whether a click should be suppressed, consuming the pending press.
 *
 * Every click consumes the record: it describes THIS interaction only. Leaving
 * it behind would let an earlier drag suppress a later, unrelated click.
 */
export function shouldSuppressClick(
  e: { clientX: number; clientY: number; detail?: number },
): boolean {
  const press = lastPress
  lastPress = null
  if (!press) return false
  // A programmatic click (keyboard activation) has detail 0 and no preceding
  // trusted press. Without this, Enter/Space on a guarded row would stop
  // working after any drag. `detail` is the DOM-defined discriminator and, unlike
  // `isTrusted`, is observable in tests (dispatched events are always untrusted).
  if (!e.detail) return false
  return isDragRelease(press, { x: e.clientX, y: e.clientY })
}

/**
 * Install the guard. Returns a disposer that removes every listener.
 *
 * `target` defaults to `document` and exists so tests can drive a detached node
 * without touching global state.
 */
export function installDragClickGuard(target: Document | HTMLElement = document): () => void {
  const root = target as unknown as EventTarget

  const onPointerDown = (e: Event) => {
    const pe = e as PointerEvent
    recordPress(pe.clientX, pe.clientY, pe.pointerType || 'mouse', pe.button)
  }

  // Fallback for WebViews without Pointer Events: without it a touch drag would
  // go unrecorded and the guard would silently do nothing there.
  const onTouchStart = (e: Event) => {
    const te = e as TouchEvent
    const touch = te.touches?.[0]
    if (!touch) return
    recordPress(touch.clientX, touch.clientY, 'touch')
  }

  const onCancel = () => clearPress()

  const onClickCapture = (e: Event) => {
    const me = e as MouseEvent
    if (!shouldSuppressClick(me)) return
    // It was a drag. Swallow the click so no row acts on a text selection.
    // Capture phase + stopPropagation keeps it from reaching the row's own
    // handler; other document-level listeners still run.
    e.stopPropagation()
    e.preventDefault()
  }

  const opts: AddEventListenerOptions = { capture: true, passive: true }
  root.addEventListener('pointerdown', onPointerDown, opts)
  root.addEventListener('pointercancel', onCancel, opts)
  root.addEventListener('touchstart', onTouchStart, opts)
  root.addEventListener('touchcancel', onCancel, opts)
  // Not passive: this one calls preventDefault.
  root.addEventListener('click', onClickCapture, true)

  return () => {
    clearPress()
    root.removeEventListener('pointerdown', onPointerDown, true)
    root.removeEventListener('pointercancel', onCancel, true)
    root.removeEventListener('touchstart', onTouchStart, true)
    root.removeEventListener('touchcancel', onCancel, true)
    root.removeEventListener('click', onClickCapture, true)
  }
}
