export interface TerminalAutoRefocusContext {
  /** True while the user is actively dragging to scroll terminal output. */
  isScrolling?: boolean
  /** True while an upper-layer terminal drawer, popup menu, or modal is open. */
  hasOverlayOpen?: boolean
}

/**
 * Overlay / interactive selectors inside or above the terminal container that
 * represent upper-layer UI rather than the terminal canvas itself.
 */
const UPPER_LAYER_SELECTOR = [
  'button',
  'a',
  'input',
  'textarea:not(.xterm-helper-textarea)',
  'select',
  '[role="button"]',
  '[role="dialog"]',
  '[role="menu"]',
  '.terminal-rebuild-overlay',
  '.terminal-error-overlay',
  '.gesture-hint',
  '.drop-overlay',
  '.bottom-sheet-overlay',
  '.bottom-sheet',
  '.popup-menu',
  '.context-menu',
  '.modal-overlay',
].join(', ')

/**
 * Decide whether an event target belongs to the interactive terminal surface
 * itself (inside `container` and not an upper-layer overlay/control element).
 *
 * Pressing an upper-layer page element (floating button, drawer, modal, error
 * overlay, toolbar, etc.) must return false so it never triggers terminal
 * textarea focus or blur-refocus recovery.
 */
export function isTerminalSurfaceTarget(
  target: EventTarget | null,
  container: HTMLElement | null,
): boolean {
  if (!container || !target) return false
  // Handle Element (and Text nodes inside an element).
  const el = target instanceof Element
    ? target
    : (target instanceof Node ? target.parentElement : null)
  if (!el || !container.contains(el)) return false
  if (typeof el.closest === 'function' && el.closest(UPPER_LAYER_SELECTOR)) {
    return false
  }
  return true
}

/**
 * Decide whether the terminal should re-focus its xterm textarea after it blurs.
 *
 * Android WebView quirk: touching the terminal surface blurs the focused xterm
 * textarea BEFORE `touchstart` is dispatched (the keyboard collapses), and xterm
 * re-focuses on the synthesized mousedown (the keyboard reopens) — a visible
 * collapse-then-reopen on every tap. This blur can't be prevented with
 * `preventDefault` because it happens before the touch event. Instead we restore
 * focus as soon as the textarea blurs.
 *
 * We only auto-refocus when:
 * - the terminal panel is still active,
 * - focus did not move to a real control (button/input/textarea),
 * - the user is NOT actively scrolling terminal output, AND
 * - no upper-layer terminal drawer, popup menu, or modal is open.
 */
export function shouldAutoRefocusTerminal(
  terminalActive: boolean,
  nextActive: Element | null,
  context?: TerminalAutoRefocusContext,
): boolean {
  if (!terminalActive) return false
  if (context?.isScrolling) return false
  if (context?.hasOverlayOpen) return false
  if (!nextActive) return true
  return nextActive.tagName === 'BODY'
}

/**
 * Whether the terminal blur-refocus workaround should be installed at all.
 *
 * The workaround exists only for the Android WebView soft-keyboard quirk (see
 * the comment above) — so the question is HOST, not input: it must be on for
 * the Android shell and off for the Electron shell, even though both are native
 * hosts and both usually have no fine pointer.
 *
 * On any other host it is not only unnecessary but harmful: in the wide-screen
 * split layout the terminal panel stays active while the chat pane is on the
 * right, so a blur on the xterm textarea can re-claim focus from the chat input
 * in the same tick, making the chat textarea unfocusable.
 *
 * (This used to take `isPC` — the conflated predicate — which asked "is this
 * not a desktop?" and so answered `true` for Android by accident. Naming the
 * host directly makes the intent checkable.)
 */
export function shouldInstallTerminalBlurRefocus(isAndroidApp: boolean): boolean {
  return isAndroidApp
}
