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
