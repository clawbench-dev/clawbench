import { onMounted, onUnmounted } from 'vue'

/**
 * Reload the page when F5 is pressed and nothing else claimed it.
 *
 * F5 already means something in two places, and both must keep working:
 *
 *   - the terminal forwards F5 to the running TUI (vim/tmux use it to page),
 *     handled by xterm on its own container;
 *   - the file manager refreshes its listing, handled at document level but
 *     only while the browse tab is active and focused.
 *
 * So this cannot be a plain "F5 → reload" listener: whichever of the three is
 * registered first would win, and a document-level listener added before the
 * file manager's would reload the page out from under it.
 *
 * The reliable signal is `defaultPrevented`. The problem is ordering: for
 * events on the SAME target, listeners run in registration order, so a handler
 * that inspects `defaultPrevented` synchronously may run before the one that
 * sets it. Reading it on a later tick sidesteps that entirely — by then every
 * listener for the event, on every target, has run. Capture phase is used only
 * to get the event object early; the decision itself is deferred.
 *
 * Verified behaviour (headless Chrome, three scenarios):
 *   terminal focused      -> defaultPrevented true  -> no reload
 *   file manager active   -> defaultPrevented true  -> no reload
 *   anywhere else         -> defaultPrevented false -> reload
 *
 * A plain reload is intentional: it re-fetches what the page asks for but
 * keeps the HTTP cache. The cache-clearing variant is Ctrl+Shift+R, handled in
 * the main process.
 */
export function useF5Reload(): void {
  function onKeyDown(e: KeyboardEvent): void {
    if (e.key !== 'F5') return
    // Modifier combinations belong to the app shortcuts (Ctrl+Shift+R etc.).
    if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return
    const ev = e
    // Defer so every listener for this event has run before deciding. Without
    // the delay a document-level listener registered earlier than the file
    // manager's would see defaultPrevented === false and reload anyway.
    setTimeout(() => {
      if (ev.defaultPrevented) return
      window.location.reload()
    }, 0)
  }

  onMounted(() => document.addEventListener('keydown', onKeyDown, true))
  onUnmounted(() => document.removeEventListener('keydown', onKeyDown, true))
}
