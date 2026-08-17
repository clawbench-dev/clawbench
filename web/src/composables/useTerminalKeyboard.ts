import { ref } from 'vue'

// Module-level singleton — shared between TerminalPanelContent (writer) and App.vue (reader)
const keyboardHeight = ref(0)

// Whether the browser is in adjustResize mode (Android native WebView).
// In adjustResize, window.innerHeight shrinks when the keyboard opens,
// so position:fixed containers auto-adjust — no CSS bottom compensation needed.
// In non-adjustResize (PWA standalone, iOS WKWebView), innerHeight stays
// the same, so we must compensate with CSS bottom shrink.
const isAdjustResize = ref(false)

// Baseline for Android adjustResize detection (keyboard height ≈ full height
// minus the shrunken innerHeight). Captured from the LARGEST innerHeight seen,
// because capturing it once at module load is unreliable: Android WebViews
// report innerHeight == 0 during early script execution before the layout
// viewport is sized. The keyboard only ever SHRINKS innerHeight, so tracking
// the max observed value yields the correct full-screen height and self-heals
// from a 0 baseline.
let fullScreenHeight = 0

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

  // Track the largest innerHeight seen as the full-screen baseline (see above).
  function noteFullScreenHeight(h: number) {
    if (h > fullScreenHeight) fullScreenHeight = h
  }

  return {
    keyboardHeight,
    setKeyboardHeight,
    isAdjustResize,
    setAdjustResize,
    getFullScreenHeight: () => fullScreenHeight,
    noteFullScreenHeight,
  }
}
