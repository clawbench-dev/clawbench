/**
 * Android WebView textarea recovery.
 *
 * Captured from a real device (ClawBench Android app) in the rename dialog,
 * which calls `select()` and so always has a full selection: replacing that
 * selection in one go makes the WebView IME emit an *empty insert*
 * (`beforeinput` with `inputType === 'insertText'` and `data === ''`) rather
 * than a `deleteContentBackward`. After that the InputConnection is dead: every
 * later keystroke is dropped without any event reaching the page, so the field
 * stays empty no matter what the user types.
 *
 * The dialog is only where it was first noticed — the trigger is "an empty
 * insert replaced a non-collapsed selection", which any textarea can hit
 * (the chat input among them). Both consumers share this predicate.
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
 * Whether a `beforeinput` event matches the Android "replace the selection with
 * an empty insert" signature.
 *
 * Despite the name this is NOT specific to a select-all: any non-collapsed
 * selection takes this path. Verified in a real browser — a partial selection
 * (e.g. chars 2-5) produces the same `insertText` + `data=""` event, and the
 * Android failure mode is "an empty insert replaced a selection", not "the
 * whole value was selected". Do not tighten this to `selectionLength ===
 * value.length`: that would drop the partial-selection cases the bug also
 * affects.
 *
 * A collapsed caret is excluded: an empty insert with nothing selected is not a
 * selection replacement, and the IME emits no event at all in that case.
 */
export function isSelectAllDeleteSignature(
  e: { inputType?: string; data?: string | null; isComposing?: boolean },
  selectionLength: number,
): boolean {
  return e.inputType === 'insertText'
    && (e.data === '' || e.data == null)
    && selectionLength > 0
    // Defensive, not a fix for an observed case: composition events arrive as
    // `insertCompositionText` with isComposing === true, so the inputType check
    // above already excludes them (verified in a real browser). This guards the
    // unobserved case of an IME emitting a bare `insertText` at a composition
    // boundary — rebuilding then would tear the element out mid-composition.
    && !e.isComposing
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
