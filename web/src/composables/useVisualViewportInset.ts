import { ref } from 'vue'

/**
 * How much of the LAYOUT viewport is hidden by the on-screen keyboard.
 *
 * `bottomInset` is in viewport-space px — the same space as `window.innerHeight`
 * and `visualViewport.height` (so `getZoomedViewport()` / `toFixedCSS()` convert
 * it the same way they convert those). `topInset` is how far the visible region
 * starts below the layout top (`visualViewport.offsetTop`), which is non-zero
 * when the page scrolls to reveal a focused field.
 *
 * ## Why this exists
 *
 * The selection toolbar is `position: fixed`, and fixed offsets are measured
 * from the LAYOUT viewport. On a mobile BROWSER the soft keyboard shrinks only
 * the VISUAL viewport, so the layout bottom stays put — and a bar anchored to it
 * ends up behind the keyboard. Android's WebView uses `adjustResize`, which
 * shrinks the layout viewport as well, so its inset is 0 and `position: fixed`
 * already sits above the keyboard: no compensation, which is exactly right.
 *
 * ## Why it is not a boolean
 *
 * `useSoftKeyboard` answers "is the keyboard open?" for layout decisions (hide
 * the dock). Positioning needs the SIZE — how much room is actually left — so
 * this exposes the measurement instead of a flag. The two are deliberately
 * separate: this one is also safe to read while the keyboard is animating.
 *
 * ## Threshold
 *
 * A keyboard is only reported once it clears KEYBOARD_MIN_HEIGHT, mirroring
 * `useSoftKeyboard` / `useTerminalViewport`. The raw difference is NOT
 * keyboard-specific: a classic desktop horizontal scrollbar makes it ~15px, and
 * treating that as a keyboard would dock the toolbar away from its selection for
 * no reason. Three detectors, same threshold — see the note in useSoftKeyboard.
 */
export function useVisualViewportInset() {
  return { bottomInset, topInset, viewportVersion, install, uninstall }
}

// ── Internal ──

/** Minimum inset (px) that counts as a keyboard rather than browser chrome. */
const KEYBOARD_MIN_HEIGHT = 120
/**
 * Poll cadence. Android WebViews do NOT reliably dispatch window/visualViewport
 * 'resize' when the keyboard opens even though the metrics change, so the poll
 * is the only signal that reaches detection there (same reasoning as
 * useSoftKeyboard / useTerminalViewport).
 */
const POLL_INTERVAL_MS = 300

/** Keyboard height in viewport-space px; 0 when no keyboard is up. */
const bottomInset = ref(0)
/** Visible-region top offset in viewport-space px (0 unless the page scrolled). */
const topInset = ref(0)
/**
 * Bumped on every measurement, INCLUDING when the insets do not change.
 *
 * Consumers compute positions from `getZoomedViewport()`, which reads
 * `window.innerHeight` directly and is therefore NOT reactive. Under Android's
 * `adjustResize` the keyboard shrinks the LAYOUT viewport, so the insets stay 0
 * and `bottomInset` never changes — leaving a `bottom`-anchored bar at an offset
 * measured against the old, taller viewport. Depending on this counter makes the
 * recomputation happen on any viewport change, in both modes.
 */
const viewportVersion = ref(0)

let installed = false
let pollTimer: ReturnType<typeof setInterval> | null = null
/**
 * Last measured viewport signature. `measure()` runs on a 300ms poll, so bumping
 * `viewportVersion` unconditionally would re-render every consumer forever;
 * comparing against this makes the bump happen only on a real change.
 *
 * The signature tracks the LAYOUT viewport (`innerWidth`/`innerHeight`) as well
 * as the visual one, because that is exactly what consumers read through
 * `getZoomedViewport()`. The layout height is the only thing that moves under
 * Android's adjustResize, and the layout width is what the horizontal clamp is
 * computed against — so both must invalidate the position, not just the insets.
 */
let lastSignature = ''

function measure() {
  if (!installed) return
  const vv = window.visualViewport
  if (!vv) {
    // No visual viewport (old engines): nothing can be measured, and the layout
    // viewport is all we have. `position: fixed` is then already correct.
    bottomInset.value = 0
    topInset.value = 0
    bumpIfChanged('none', window.innerWidth, window.innerHeight)
    return
  }

  const scale = vv.scale ?? 1

  // Pinch-zoom shrinks `vv.height` too (height = innerHeight / scale), which
  // would otherwise look exactly like a keyboard. Treating it as one would yank
  // the bar into the top half of the screen the moment a user zooms in to read
  // small text and then selects some. iOS ignores `user-scalable=no`, so this is
  // reachable there.
  if (Math.abs(scale - 1) > 0.01) {
    bottomInset.value = 0
    topInset.value = Math.max(0, vv.offsetTop)
    bumpIfChanged('zoom', window.innerWidth, window.innerHeight, vv.height, vv.offsetTop, scale)
    return
  }

  // How much of the layout viewport lies BELOW the visible region. On Android
  // adjustResize both shrink together, so this stays ~0; on a mobile browser
  // only the visual viewport shrinks, so this is the keyboard height.
  const raw = window.innerHeight - (vv.offsetTop + vv.height)
  bottomInset.value = raw >= KEYBOARD_MIN_HEIGHT ? raw : 0
  // Reported raw: it is a position, not a keyboard signal, so the threshold
  // above does not apply.
  topInset.value = Math.max(0, vv.offsetTop)
  bumpIfChanged('normal', window.innerWidth, window.innerHeight, vv.height, vv.offsetTop, scale)
}

/**
 * Bump `viewportVersion` only when a viewport metric actually changed.
 *
 * The insets alone are not enough to drive recomputation: under Android's
 * adjustResize they stay 0 while the LAYOUT viewport still shrinks, and
 * `getZoomedViewport()` (which consumers call) reads `window.innerWidth` /
 * `innerHeight` non-reactively.
 */
function bumpIfChanged(kind: string, ...metrics: number[]) {
  const signature = `${kind}:${metrics.join(',')}`
  if (signature === lastSignature) return
  lastSignature = signature
  viewportVersion.value++
}

/** Start watching. Idempotent. */
function install() {
  if (installed) return
  installed = true
  // Reset so the first measurement always publishes (a fresh install must not
  // be suppressed by a signature left over from a previous install).
  lastSignature = ''
  measure()
  window.addEventListener('resize', measure)
  window.visualViewport?.addEventListener('resize', measure)
  // 'scroll' on the visual viewport fires as the keyboard animates and as the
  // page pans to reveal the focused field — both move the visible region.
  window.visualViewport?.addEventListener('scroll', measure)
  pollTimer = setInterval(measure, POLL_INTERVAL_MS)
}

/** Stop watching and clear the values. Idempotent. */
function uninstall() {
  if (!installed) return
  installed = false
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  window.removeEventListener('resize', measure)
  window.visualViewport?.removeEventListener('resize', measure)
  window.visualViewport?.removeEventListener('scroll', measure)
  bottomInset.value = 0
  topInset.value = 0
  lastSignature = ''
}
