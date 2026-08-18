import { ref, type Ref } from 'vue'
import type { Terminal } from '@xterm/xterm'
import { useTerminalKeyboard } from './useTerminalKeyboard'

/**
 * Terminal viewport: measures the active container and re-fits xterm on resize,
 * AND drives soft-keyboard detection via the container ResizeObserver + the
 * visualViewport listener.
 *
 * Why the container ResizeObserver matters (Android): with windowSoftInputMode
 * adjustResize, opening the keyboard shrinks innerHeight → the terminal container
 * resizes → the ResizeObserver fires. Android WebViews do NOT reliably dispatch
 * window/visualViewport 'resize', so the container observer is the signal that
 * actually reaches detection there. Detection therefore lives here, alongside
 * the terminal's own layout, not in App.vue.
 *
 * The shared singleton is written so App.vue can hide the Dock / shrink the
 * app container without depending on the terminal panel's lifecycle.
 */
export function useTerminalViewport(terminal: Ref<Terminal | null>, containerRef: Ref<HTMLElement | null>) {
  const viewportHeight = ref(0)
  const keyboardHeight = ref(0)

  let fitTimer: ReturnType<typeof setTimeout> | null = null
  const FIT_DEBOUNCE_MS = 100
  // Track the last shared keyboard height so we only schedule fit() when it
  // changes (not on every poll tick). undefined means "first call — always fit".
  let lastSharedKeyboardHeight: number | undefined = undefined
  // Poll the actual innerHeight/visualViewport every N ms. Android WebViews do
  // NOT dispatch window/viewport/ResizeObserver events when the soft keyboard
  // opens even though innerHeight & visualViewport.height DO change — so the
  // poll is the only signal that reliably reaches detection there.
  const POLL_INTERVAL_MS = 300
  let pollTimer: ReturnType<typeof setInterval> | null = null

  // Use the full-screen height captured at app startup (before any keyboard)
  // as the baseline for detecting keyboard appearance on Android adjustResize.
  const { getFullScreenHeight, setKeyboardHeight: setSharedKeyboardHeight, setAdjustResize, noteFullScreenHeight } = useTerminalKeyboard()

  function updateViewport() {
    if (!containerRef.value) {
      // No active terminal container (e.g. all session tabs were closed while
      // the keyboard was open). Clear any stale shared height so the Dock is
      // not left hidden forever. (This was the original "Dock can't recover
      // after closing all tabs" bug.)
      setSharedKeyboardHeight(0)
      setAdjustResize(false)
      return
    }

    const currentInnerHeight = window.innerHeight
    // Track the largest innerHeight seen as the full-screen baseline. The
    // keyboard only shrinks innerHeight, so max == full screen height, which
    // self-heals the unreliable module-load baseline (0 on Android WebViews).
    noteFullScreenHeight(currentInnerHeight)
    const vv = window.visualViewport

    if (vv) {
      // Method 1 (works in non-adjustResize browsers / desktop):
      // keyboardHeight = innerHeight - visualViewport.height - offsetTop
      const vvKeyboard = window.innerHeight - vv.height - vv.offsetTop

      // Method 2 (works in Android adjustResize where innerHeight shrinks):
      // keyboardHeight = fullScreenHeight - currentInnerHeight
      const resizeKeyboard = getFullScreenHeight() - currentInnerHeight

      // Detect adjustResize: if innerHeight actually shrunk, the browser
      // is in adjustResize mode (Android native WebView). In this mode
      // position:fixed containers auto-adjust, so no CSS compensation needed.
      setAdjustResize(resizeKeyboard > 0)

      // Use whichever gives a larger value — covers both scenarios
      keyboardHeight.value = Math.max(vvKeyboard, resizeKeyboard, 0)
      viewportHeight.value = vv.height
    } else {
      viewportHeight.value = containerRef.value.clientHeight
      keyboardHeight.value = 0
    }

    // Sync to module-level singleton so App.vue can react
    const prevShared = lastSharedKeyboardHeight
    setSharedKeyboardHeight(keyboardHeight.value)

    // Only schedule fit() when the keyboard height actually changed or when
    // this is the first call. The 300ms poll timer fires updateViewport
    // repeatedly; calling fit() on every tick is wasteful and can cause
    // excessive PTY resizes. Container ResizeObserver and window resize
    // events already cover layout changes that need a refit.
    if (keyboardHeight.value !== prevShared || prevShared === undefined) {
      lastSharedKeyboardHeight = keyboardHeight.value
      scheduleFit()
    }
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

    // window.resize fires when innerHeight changes.
    window.addEventListener('resize', updateViewport)

    // Belt-and-suspenders: poll the real values. Some WebViews fire none of the
    // above when the keyboard opens even though innerHeight/visualViewport change.
    pollTimer = setInterval(updateViewport, POLL_INTERVAL_MS)
  }

  function stopWatching() {
    if (fitTimer) {
      clearTimeout(fitTimer)
      fitTimer = null
    }
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
    resizeObserver?.disconnect()
    resizeObserver = null

    window.removeEventListener('resize', updateViewport)
    if (window.visualViewport) {
      window.visualViewport.removeEventListener('resize', updateViewport)
    }

    // Reset adjustResize flag when keyboard closes
    setAdjustResize(false)
    lastSharedKeyboardHeight = undefined
  }

  return {
    viewportHeight,
    keyboardHeight,
    fitTerminal,
    startWatching,
    stopWatching,
  }
}