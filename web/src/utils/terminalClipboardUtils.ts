/**
 * Terminal copy-shortcut decisions, extracted for testability.
 *
 * Background: xterm.js binds Ctrl+C to sending ETX (\x03, SIGINT) and calls
 * preventDefault() on it, which suppresses the browser's native copy command.
 * So a plain Ctrl+C never copies — even with an active selection — and users
 * coming from other terminals (where Ctrl+C copies when text is selected) are
 * left with no keyboard way to copy. Ctrl+Insert happens to work because xterm
 * leaves that combination alone, but it is undiscoverable.
 *
 * Policy implemented here (matching the common terminal convention):
 * - With a selection: the copy chord copies and does NOT reach the PTY.
 * - Without a selection: Ctrl+C falls through to xterm so it still sends SIGINT.
 */

/** Minimal shape of the keyboard event fields this decision needs. */
export interface CopyKeyEvent {
  type: string
  key: string
  ctrlKey: boolean
  metaKey: boolean
  altKey: boolean
  shiftKey: boolean
  /** Legacy numeric code — some IMEs/browsers report Insert without a usable `key`. */
  keyCode?: number
}

/**
 * Whether this event is a "copy the selection" chord.
 *
 * Only `keydown` is considered — xterm routes keydown/keypress/keyup through the
 * custom handler, and acting on all three would copy up to three times.
 *
 * Chords:
 * - Windows/Linux: Ctrl+C, Ctrl+Shift+C
 * - macOS: Cmd+C, Cmd+Shift+C (Ctrl+C stays the SIGINT key on macOS, as in
 *   Terminal.app/iTerm)
 *
 * Alt combinations are excluded: on Windows AltGr arrives as Ctrl+Alt and must
 * keep producing the composed character rather than copying.
 */
export function isCopySelectionShortcut(
  ev: CopyKeyEvent,
  hasSelection: boolean,
  isMac: boolean,
): boolean {
  if (ev.type !== 'keydown') return false
  if (!hasSelection) return false
  if (ev.altKey) return false
  if (ev.key.toLowerCase() !== 'c') return false
  // Require the platform's primary modifier. On macOS Ctrl+C must keep sending
  // SIGINT, and a bare Cmd+C is the native copy chord; on Windows/Linux the
  // reverse. Demanding the primary modifier also rejects the plain 'c' keypress.
  return isMac ? ev.metaKey : ev.ctrlKey
}

/**
 * Whether this event is the "paste into the terminal" chord: Shift+Insert.
 *
 * Why intercept it instead of leaving it to the browser: xterm only forwards
 * Insert WITHOUT Shift to the PTY and lets every other modifier combination
 * fall through to the browser's default action. That default is paste in
 * Chromium and Firefox, but it is not guaranteed in Safari, Android WebViews or
 * an Electron window whose menu swallows the accelerator — exactly the hosts
 * this app runs in. Claiming the chord here makes the behaviour identical
 * everywhere, and lets the caller decide what "paste" means (the live selection
 * first, the clipboard when nothing is selected).
 *
 * Shift is required and every other modifier is rejected:
 * - Ctrl+Insert is the copy chord, not paste.
 * - Alt/Meta combinations keep their normal terminal meaning.
 *
 * Only `keydown` counts, for the same reason as copy: acting on keydown,
 * keypress and keyup would paste the same text up to three times.
 */
export function isPasteSelectionShortcut(ev: CopyKeyEvent): boolean {
  if (ev.type !== 'keydown') return false
  if (!ev.shiftKey) return false
  if (ev.ctrlKey || ev.altKey || ev.metaKey) return false
  // `key` is 'Insert' in every current browser; `keyCode` is the fallback for
  // environments that report an empty key for non-character keys.
  return ev.key === 'Insert' || ev.keyCode === 45
}
