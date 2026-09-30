import { ref } from 'vue'

// Module-level singleton — shared between ChatInputBar (activator) and App.vue (reader)
const chatKeyboardHeight = ref(0)

/**
 * Reactive soft-keyboard height for the chat input.
 *
 * On iOS WKWebView there is no adjustResize — window.innerHeight stays the same
 * when the keyboard opens, so fixed-position elements extend behind the keyboard.
 * This composable uses the visualViewport API to detect the keyboard height and
 * expose it reactively so App.vue can compensate.
 *
 * On Android (adjustResize) and desktop, this always returns 0 — those platforms
 * handle keyboard avoidance natively.
 */
export function useChatKeyboard() {
  function activate() {
    clearDeactivateTimer()
    startWatching()
  }

  /**
   * Debounced deactivate — wait a short period after blur before clearing
   * the keyboard height. This prevents a flash where the keyboard is still
   * animating closed (visualViewport still reports a reduced height) but
   * we've already set height=0, causing a brief layout jump.
   */
  function debounceDeactivate() {
    clearDeactivateTimer()
    deactivateTimer = setTimeout(() => {
      deactivateTimer = null
      deactivate()
    }, DEACTIVATE_DELAY_MS)
  }

  function deactivate() {
    clearDeactivateTimer()
    stopWatching()
    chatKeyboardHeight.value = 0
  }

  return { chatKeyboardHeight, activate, deactivate, debounceDeactivate }
}

// ── Internal ──

let watching = false
let deactivateTimer: ReturnType<typeof setTimeout> | null = null
const DEACTIVATE_DELAY_MS = 150

function clearDeactivateTimer() {
  if (deactivateTimer) {
    clearTimeout(deactivateTimer)
    deactivateTimer = null
  }
}

/**
 * Minimum measured height (px) that counts as a keyboard.
 *
 * The raw difference is NOT keyboard-specific. On desktop it is normally 0, but
 * a classic horizontal scrollbar makes it ~15px and a pinch-zoom makes it far
 * larger. Without this gate the value is applied as `.chat-keyboard-open
 * { bottom: <n>px }` on .app-container, which shrinks (and clears) the animated
 * wallpaper canvas — reported as "the dynamic wallpaper flashes when I focus
 * the chat input". The file-manager search input never flashed because only the
 * chat input activates this composable.
 *
 * Mirrors useSoftKeyboard's KEYBOARD_MIN_HEIGHT: browser chrome and scrollbars
 * are far shorter than a real keyboard.
 */
const KEYBOARD_MIN_HEIGHT = 120

function updateKeyboardHeight() {
  const vv = window.visualViewport
  if (!vv) return

  // keyboardHeight = space taken by the keyboard relative to the layout viewport
  const height = window.innerHeight - vv.height - vv.offsetTop
  const next = height >= KEYBOARD_MIN_HEIGHT ? height : 0
  chatKeyboardHeight.value = next
}

function onVisualViewportResize() {
  updateKeyboardHeight()
}

function startWatching() {
  if (watching) return
  watching = true
  updateKeyboardHeight()
  if (window.visualViewport) {
    window.visualViewport.addEventListener('resize', onVisualViewportResize)
  }
}

function stopWatching() {
  if (!watching) return
  watching = false
  if (window.visualViewport) {
    window.visualViewport.removeEventListener('resize', onVisualViewportResize)
  }
}
