import { ref } from 'vue'

// Module-level singleton — shared between TerminalPanelContent (writer) and App.vue (reader)
const keyboardHeight = ref(0)

// Whether the browser is in adjustResize mode (Android native WebView).
// In adjustResize, window.innerHeight shrinks when the keyboard opens,
// so position:fixed containers auto-adjust — no CSS bottom compensation needed.
// In non-adjustResize (PWA standalone, iOS WKWebView), innerHeight stays
// the same, so we must compensate with CSS bottom shrink.
const isAdjustResize = ref(false)

// Capture the full-screen innerHeight at module load time (before any keyboard opens).
// This is critical for Android adjustResize where innerHeight shrinks when the
// soft keyboard appears — if we only start watching when the terminal activates,
// the keyboard may already be opening and we'd record a shrunken baseline.
const fullScreenHeight = window.innerHeight

/**
 * Reactive soft-keyboard height for the terminal.
 *
 * TerminalPanelContent writes keyboardHeight via useTerminalViewport.
 * App.vue reads it to conditionally hide AppHeader + Dock + shrink app container.
 *
 * Using a module-level ref (not defineExpose) ensures reactive tracking
 * works across component boundaries — template ref + defineExpose does NOT
 * propagate reactivity to the parent's computed/watch.
 */
export function useTerminalKeyboard() {
  function setKeyboardHeight(h: number) {
    keyboardHeight.value = h
  }

  function setAdjustResize(v: boolean) {
    isAdjustResize.value = v
  }

  return { keyboardHeight, setKeyboardHeight, isAdjustResize, setAdjustResize, fullScreenHeight }
}
