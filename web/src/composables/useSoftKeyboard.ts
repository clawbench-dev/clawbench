import { ref } from 'vue'

// Module-level singleton — installed once by App.vue, read reactively anywhere.
const isSoftKeyboardOpen = ref(false)

/**
 * Reactive "is the on-screen (soft) keyboard open?" flag, for any focused
 * editable — not just the chat input or the terminal.
 *
 * Why this exists: on a mobile *browser* the IME resizes only the visual
 * viewport, so a `position: fixed; inset: 0` layout (and the in-flow bottom
 * dock it contains) keeps its full layout height and the dock ends up behind
 * the keyboard. Android WebView uses `adjustResize`, which shrinks the layout
 * viewport and pushes the dock up instead. Compensating with a CSS `bottom`
 * offset was tried and left a residual overlap, so both platforms now simply
 * hide the dock while the keyboard is open (see App.vue).
 *
 * Detection mirrors `useTerminalViewport`, which is proven on Android WebViews:
 *   - visualViewport shrink → mobile browsers (no adjustResize)
 *   - layout-viewport shrink → Android WebView (adjustResize)
 * plus a poll, because some WebViews fire neither resize event even though the
 * viewport really changed.
 *
 * A keyboard is only reported when BOTH hold:
 *   1. an editable element has focus — keeps the flag from flipping on
 *      unrelated viewport changes (URL-bar show/hide, page scroll, rotation);
 *   2. the measured height clears KEYBOARD_MIN_HEIGHT — browser chrome is far
 *      shorter than a keyboard.
 *
 * A hardware keyboard (desktop) never clears the threshold, so PC is unaffected.
 */
export function useSoftKeyboard() {
  return { isSoftKeyboardOpen, install, uninstall }
}

// ── Internal ──

/** Minimum measured height (px) that counts as a keyboard, not browser chrome. */
const KEYBOARD_MIN_HEIGHT = 120
/** Poll cadence — the only signal some Android WebViews deliver. */
const POLL_INTERVAL_MS = 300
/** Grace after focusout so focus moving between two editables is not a close. */
const FOCUS_GRACE_MS = 150

let installed = false
let editableFocused = false
// Largest viewport height seen at the current width; the keyboard only ever
// shrinks it. Reset when the width changes (rotation / window resize), since
// the old baseline is then meaningless.
let baselineHeight = 0
let baselineWidth = 0
let pollTimer: ReturnType<typeof setInterval> | null = null
let graceTimer: ReturnType<typeof setTimeout> | null = null

function isEditableElement(el: Element | null): boolean {
  if (!el) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || (el as HTMLElement).isContentEditable === true
}

function viewportWidth(): number {
  const vv = window.visualViewport
  return vv ? vv.width : window.innerWidth
}

function viewportHeight(): number {
  const vv = window.visualViewport
  return vv ? vv.height : window.innerHeight
}

function measure() {
  if (!installed) return

  const width = viewportWidth()
  const height = viewportHeight()
  if (width !== baselineWidth) {
    baselineWidth = width
    baselineHeight = height
  } else if (height > baselineHeight) {
    baselineHeight = height
  }

  if (!editableFocused) {
    isSoftKeyboardOpen.value = false
    return
  }

  const vv = window.visualViewport
  // Mobile browser: innerHeight stays put while the visual viewport shrinks.
  const visualShrink = vv ? window.innerHeight - vv.height - vv.offsetTop : 0
  // Android WebView (adjustResize): both shrink together, so only the baseline
  // delta reveals the keyboard.
  const layoutShrink = baselineHeight - height
  const keyboardHeight = Math.max(visualShrink, layoutShrink, 0)
  isSoftKeyboardOpen.value = keyboardHeight >= KEYBOARD_MIN_HEIGHT
}

function clearGraceTimer() {
  if (graceTimer) {
    clearTimeout(graceTimer)
    graceTimer = null
  }
}

function onFocusIn(e: FocusEvent) {
  if (!isEditableElement(e.target as Element | null)) return
  clearGraceTimer()
  editableFocused = true
  measure()
}

function onFocusOut() {
  clearGraceTimer()
  graceTimer = setTimeout(() => {
    graceTimer = null
    // Re-check rather than assuming closed: focus may have moved straight into
    // another editable, in which case focusin already re-armed the flag.
    editableFocused = isEditableElement(document.activeElement)
    measure()
  }, FOCUS_GRACE_MS)
}

/** Start watching. Idempotent. */
function install() {
  if (installed) return
  installed = true
  editableFocused = isEditableElement(document.activeElement)
  baselineWidth = 0
  baselineHeight = 0
  measure()
  document.addEventListener('focusin', onFocusIn)
  document.addEventListener('focusout', onFocusOut)
  window.addEventListener('resize', measure)
  window.visualViewport?.addEventListener('resize', measure)
  pollTimer = setInterval(measure, POLL_INTERVAL_MS)
}

/** Stop watching and clear the flag. Idempotent. */
function uninstall() {
  if (!installed) return
  installed = false
  clearGraceTimer()
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  document.removeEventListener('focusin', onFocusIn)
  document.removeEventListener('focusout', onFocusOut)
  window.removeEventListener('resize', measure)
  window.visualViewport?.removeEventListener('resize', measure)
  editableFocused = false
  isSoftKeyboardOpen.value = false
}
