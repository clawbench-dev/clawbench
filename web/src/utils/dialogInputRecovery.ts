/**
 * Android WebView prompt-input recovery.
 *
 * Captured from a real device (ClawBench Android app): the rename dialog calls
 * `select()`, which selects the whole value. Deleting that selection in one go
 * makes the WebView IME emit an *empty insert* (`beforeinput` with
 * `inputType === 'insertText'` and `data === ''`) rather than a
 * `deleteContentBackward`. After that the InputConnection is dead: every later
 * keystroke is dropped without any event reaching the page, so the field stays
 * empty no matter what the user types.
 *
 * Desktop Chromium never takes this path — it fires `deleteContentBackward` /
 * `deleteContentForward` with `data === null` (verified in a real browser), so
 * the signature below is Android-specific.
 *
 * Two earlier recovery attempts failed on device (both verified by log):
 * collapsing the caret and blur+focus. Both operate on the *same* element, which
 * reuses the already-broken InputConnection. The fix therefore rebuilds the
 * element itself — a fresh textarea gets a fresh InputConnection.
 */

/**
 * Whether a `beforeinput` event matches the Android "delete the whole
 * selection" signature: an empty insert issued while text was selected.
 *
 * A collapsed caret is excluded — an empty insert with nothing selected is not
 * a selection delete and must not trigger recovery.
 */
export function isSelectAllDeleteSignature(
  e: { inputType?: string; data?: string | null },
  selectionLength: number,
): boolean {
  return e.inputType === 'insertText'
    && (e.data === '' || e.data == null)
    && selectionLength > 0
}

/**
 * Whether the broken element should be rebuilt.
 *
 * Android-only by design: the failure is a WebView InputConnection bug, and the
 * rebuild costs a focus round-trip. Desktop keeps its current behaviour
 * untouched.
 */
export function shouldRebuildInputOnSelectAllDelete(
  isAndroid: boolean,
  signature: boolean,
): boolean {
  return isAndroid && signature
}
