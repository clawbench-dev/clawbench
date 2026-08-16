import { watch, onScopeDispose, type Ref } from 'vue'
import { useTerminalKeyboard } from './useTerminalKeyboard'
import { usePlatformDetect } from './usePlatformDetect'

/**
 * Fallback poll interval. Some Android WebViews never fire a visualViewport
 * 'resize' when the soft keyboard opens — polling guarantees the Dock-hide
 * state converges even on those platforms.
 */
const POLL_INTERVAL_MS = 200

/**
 * Single owner of soft-keyboard detection for the terminal.
 *
 * App.vue activates this while the terminal tab is active and reads the shared
 * useTerminalKeyboard singleton to hide the Dock / shrink the app container.
 * Detection is deliberately independent of the terminal panel's own lifecycle
 * (which only handles xterm sizing/fit) — that coupling is what made the old
 * design fragile (missed startWatching, stale singleton, container-null races).
 */
export function useTerminalKeyboardDetect(terminalActive: Ref<boolean>) {
  const { setKeyboardHeight, setAdjustResize, fullScreenHeight } = useTerminalKeyboard()
  const { isPC } = usePlatformDetect()

  let active = false
  let pollTimer: ReturnType<typeof setInterval> | null = null

  function computeKeyboardHeight(): number {
    // A non-touch desktop has no soft keyboard; a window resize must not be
    // misread as a keyboard opening (which would hide the bottom Dock).
    // Requiring BOTH the PC UA and zero touch points means a touch device whose
    // UA is misdetected as a PC still gets keyboard detection.
    if (isPC.value && typeof navigator !== 'undefined' && navigator.maxTouchPoints === 0) {
      return 0
    }

    const innerHeight = window.innerHeight
    const vv = window.visualViewport
    if (!vv) return 0

    // Non-adjustResize (iOS / PWA standalone): the layout viewport keeps its
    // size while the visualViewport shrinks under the keyboard.
    const vvKeyboard = innerHeight - vv.height - vv.offsetTop

    // Android adjustResize (native WebView): innerHeight itself shrinks.
    const resizeKeyboard = fullScreenHeight - innerHeight

    // Detect adjustResize so App.vue can skip CSS bottom compensation on
    // platforms where position:fixed containers auto-adjust.
    setAdjustResize(resizeKeyboard > 0)

    return Math.max(vvKeyboard, resizeKeyboard, 0)
  }

  function update() {
    setKeyboardHeight(computeKeyboardHeight())
  }

  function start() {
    if (active) return
    active = true
    update()
    window.addEventListener('resize', update)
    window.visualViewport?.addEventListener('resize', update)
    pollTimer = setInterval(update, POLL_INTERVAL_MS)
  }

  function stop() {
    if (!active) return
    active = false
    window.removeEventListener('resize', update)
    window.visualViewport?.removeEventListener('resize', update)
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
    // Leaving the terminal tab must clear the keyboard state so the Dock is
    // never left hidden by a stale height.
    setKeyboardHeight(0)
    setAdjustResize(false)
  }

  watch(terminalActive, (act) => {
    if (act) start()
    else stop()
  }, { immediate: true })

  onScopeDispose(stop)

  return { start, stop }
}