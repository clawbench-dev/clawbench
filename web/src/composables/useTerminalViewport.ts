import { ref, type Ref } from 'vue'
import type { Terminal } from '@xterm/xterm'

/**
 * Terminal viewport sizing/fit only.
 *
 * Soft-keyboard detection is NOT this composable's job anymore — it lives in
 * useTerminalKeyboardDetect (owned by App.vue), so the terminal's layout is
 * decoupled from the Dock-hide state that used to be synced through a shared
 * module singleton here.
 */
export function useTerminalViewport(terminal: Ref<Terminal | null>, containerRef: Ref<HTMLElement | null>) {
  const viewportHeight = ref(0)

  let fitTimer: ReturnType<typeof setTimeout> | null = null
  const FIT_DEBOUNCE_MS = 100

  function updateViewport() {
    if (!containerRef.value) return

    const vv = window.visualViewport
    viewportHeight.value = vv ? vv.height : containerRef.value.clientHeight

    // Debounce fit() to prevent duplicate lines during keyboard animation.
    // Without debounce, each resize event during the keyboard slide-up
    // triggers fit() → PTY resize → SIGWINCH → shell redraws prompt,
    // duplicating the current line.
    scheduleFit()
  }

  function scheduleFit() {
    if (fitTimer) clearTimeout(fitTimer)
    fitTimer = setTimeout(() => {
      fitTimer = null
      fitTerminal()
    }, FIT_DEBOUNCE_MS)
  }

  function fitTerminal() {
    if (!terminal.value || !containerRef.value) return
    try {
      // @ts-expect-error — FitAddon is loaded dynamically
      terminal.value.fitAddon?.fit()
    } catch {
      // fit() can fail if terminal is not visible
    }
  }

  let resizeObserver: ResizeObserver | null = null

  function startWatching() {
    updateViewport()

    // Watch container size changes (e.g. the container shrinks when the soft
    // keyboard opens and the app container is compensated).
    if (containerRef.value) {
      resizeObserver = new ResizeObserver(() => {
        updateViewport()
      })
      resizeObserver.observe(containerRef.value)
    }

    // Watch visualViewport for keyboard changes.
    if (window.visualViewport) {
      window.visualViewport.addEventListener('resize', updateViewport)
      // Don't watch scroll — it fires on every keyboard animation frame
      // and causes excessive fit() calls that duplicate terminal content.
    }
  }

  function stopWatching() {
    if (fitTimer) {
      clearTimeout(fitTimer)
      fitTimer = null
    }
    resizeObserver?.disconnect()
    resizeObserver = null

    if (window.visualViewport) {
      window.visualViewport.removeEventListener('resize', updateViewport)
    }
  }

  return {
    viewportHeight,
    fitTerminal,
    startWatching,
    stopWatching,
  }
}