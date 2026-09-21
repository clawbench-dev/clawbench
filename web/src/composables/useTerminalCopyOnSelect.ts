/**
 * Terminal "copy on select" (选中即复制) — matches mainstream terminals
 * (GNOME Terminal, Windows Terminal, Termius): the selection lands on the
 * clipboard as soon as the gesture that produced it ends, while the highlight
 * stays visible so the user can still see (and right-click) what was copied.
 *
 * Why a settle delay on top of xterm's `onSelectionChange`: for a real mouse
 * drag xterm only fires the event on mouseup, but our touch selection mode
 * calls `term.select()` on every touchmove — and every programmatic `select()`
 * fires the very same event. Copying directly from the event would therefore
 * write the clipboard (and flash a toast) dozens of times during one drag.
 * Waiting for the selection to stop changing collapses that to one copy per
 * gesture, which is the mouseup semantics we want on every input method.
 */

/** How long the selection must stop changing before the copy fires. */
export const AUTO_COPY_SETTLE_MS = 150

/**
 * Normalize a selection so that two copies which differ only in xterm's
 * line-ending / trailing-whitespace handling are recognized as the same copy.
 */
export function selectionFingerprint(text: string): string {
  return text.replace(/\r/g, '').trim()
}

/**
 * Whether this selection change should schedule an auto-copy.
 *
 * - An empty selection never copies: clearing a selection must not overwrite
 *   the clipboard with ''.
 * - Disabled (user turned the setting off) never copies.
 * - A selection whose fingerprint was already copied is skipped, because xterm
 *   re-fires the event for selections that are equivalent but not identical
 *   (e.g. a right-click re-anchoring a word selection).
 */
export function shouldAutoCopySelection(
  enabled: boolean,
  text: string,
  lastFingerprint: string,
): boolean {
  if (!text) return false
  if (!enabled) return false
  return selectionFingerprint(text) !== lastFingerprint
}

export function useTerminalCopyOnSelect(opts: {
  /** Read at call time — the setting can be toggled while a terminal is open. */
  isEnabled: () => boolean
  /** Perform the copy and surface feedback to the user. */
  copy: (text: string) => void
  delayMs?: number
}) {
  let lastCopiedFingerprint = ''
  let timer: ReturnType<typeof setTimeout> | null = null

  function cancelPending() {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  /** Call on every selection change; copies once the selection settles. */
  function onSelectionChanged(text: string) {
    cancelPending()
    if (!text) {
      // An empty selection ends the previous copy context, so re-selecting the
      // exact same text later copies it again (the clipboard may have been
      // replaced in between by another app).
      lastCopiedFingerprint = ''
      return
    }
    if (!shouldAutoCopySelection(opts.isEnabled(), text, lastCopiedFingerprint)) return
    const fingerprint = selectionFingerprint(text)
    timer = setTimeout(() => {
      timer = null
      // Record before copying: a clipboard that rejects the write must not
      // retry on every later selection change.
      lastCopiedFingerprint = fingerprint
      opts.copy(text)
    }, opts.delayMs ?? AUTO_COPY_SETTLE_MS)
  }

  /** Drop a pending copy (tab switch / unmount). */
  function dispose() {
    cancelPending()
  }

  return { onSelectionChanged, dispose }
}
