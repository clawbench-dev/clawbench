import { ref, nextTick } from 'vue'
import { isAndroidUA } from '@/composables/usePlatformDetect'
import {
  isSelectAllDeleteSignature,
  shouldRebuildInputOnSelectAllDelete,
} from '@/utils/dialogInputRecovery'

/**
 * Android WebView textarea recovery.
 *
 * The IME implements "delete the whole selection" as an *empty insert*
 * (`beforeinput` with `inputType === 'insertText'` and `data === ''`). After
 * that the WebView's InputConnection is dead and every later keystroke is
 * dropped without any event reaching the page — the field is bricked until it
 * is rebuilt.
 *
 * Bind the returned handlers to the textarea and use `inputEpoch` as its `:key`:
 * bumping the key makes Vue replace the element, which is the only way to get a
 * fresh InputConnection. Collapsing the caret and blur+focus were both tried on
 * device and both failed — they operate on the same element and reuse the dead
 * connection.
 *
 * Timing is load-bearing: detection happens in `beforeinput` but the rebuild is
 * deferred to `input`. Touching the selection (or replacing the element) inside
 * `beforeinput` cancels the pending edit, so the old text never gets deleted —
 * verified in a real browser.
 *
 * Android-only: the failure is a WebView bug and the rebuild costs a focus
 * round-trip, so desktop behaviour must stay untouched.
 */
export function useSelectAllDeleteRecovery(opts: {
  getElement: () => HTMLTextAreaElement | null
  /** Called after the element has been rebuilt, refocused and had its caret restored. */
  onRebuilt?: (el: HTMLTextAreaElement) => void
}) {
  const inputEpoch = ref(0)

  // Set when a `beforeinput` carried the signature; read by the `input` handler
  // that follows, so the edit itself is never cancelled.
  let pending = false
  // Caret to restore after the rebuild, captured before the selection is
  // replaced (the old element is gone by the time we can read it again).
  let caret = 0

  function onBeforeInput(e: InputEvent): void {
    // Each beforeinput opens a fresh detection window, so a signature whose
    // input event never arrives cannot leak into a later keystroke.
    pending = false

    const el = opts.getElement()
    if (!el) return
    const start = el.selectionStart ?? 0
    caret = start
    const selectionLength = (el.selectionEnd ?? 0) - start
    if (!shouldRebuildInputOnSelectAllDelete(isAndroidUA, isSelectAllDeleteSignature(e, selectionLength))) return

    pending = true
  }

  function onInput(): void {
    if (!pending) return
    pending = false

    // Bump the key: Vue replaces the element, so the WebView builds a fresh
    // InputConnection. Focus is lost with the old node, so restore it once the
    // new one is mounted.
    inputEpoch.value++
    void nextTick(() => {
      const el = opts.getElement()
      if (!el) return
      el.focus()
      // The caret sits where the replaced selection began. Clamp it: the new
      // value may be shorter than the old one (e.g. a trailing selection).
      const pos = Math.min(caret, el.value.length)
      el.setSelectionRange(pos, pos)
      opts.onRebuilt?.(el)
    })
  }

  /** Drop any detection that never got its matching input event. */
  function reset(): void {
    pending = false
  }

  return { inputEpoch, onBeforeInput, onInput, reset }
}
