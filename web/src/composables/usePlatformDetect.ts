import { ref } from 'vue'
import { useAppMode } from './useAppMode'

const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''

/** Android UA (browser) — excludes Windows Phone spoofing */
export const isAndroidUA = /Android/i.test(ua) && !/Windows Phone/i.test(ua)

/** iOS UA (iPhone, iPad, iPod) — classic iOS UA string */
export const isIOSUA = /iPhone|iPad|iPod/i.test(ua)

/** iPadOS 13+ desktop-mode UA: sends "Macintosh" but has touch capability (maxTouchPoints > 0).
 * Real Macs have maxTouchPoints = 0; iPads requesting desktop sites have maxTouchPoints > 0. */
export const isIPadOSUA = /Macintosh/i.test(ua)
  && typeof navigator !== 'undefined'
  && navigator.maxTouchPoints > 0

const isPC = ref(false)
let platformInitialized = false

/**
 * Detects whether the device is a PC (desktop/laptop with physical keyboard).
 *
 * isPC = true when:
 * - NOT Android UA (mobile phone/tablet browser)
 * - NOT iOS UA (iPhone/iPad classic)
 * - NOT iPadOS 13+ desktop-mode (touch-only device, no physical keyboard)
 * - NOT Android App mode (native WebView)
 *
 * All conditions are static — UA doesn't change during a session and isAppMode
 * is initialized once — so isPC is computed once at init, not a reactive computed.
 *
 * IMPORTANT: useAppMode() must be initialized (called at least once) before
 * usePlatformDetect() reads isAppMode.value. Currently App.vue initializes
 * useAppMode early, so this order dependency is satisfied. If usePlatformDetect()
 * were called before useAppMode(), isAppMode.value would still be its default
 * (false), which is correct (web mode is not PC-blocking, only native app mode is).
 */
export function usePlatformDetect() {
  if (!platformInitialized) {
    platformInitialized = true
    const { isAppMode } = useAppMode()
    isPC.value = !isAppMode.value && !isAndroidUA && !isIOSUA && !isIPadOSUA
  }
  return { isPC }
}

/** Test hook — force isPC value for unit tests. Sets platformInitialized=true so usePlatformDetect() won't overwrite. */
export function _setIsPCForTest(val: boolean) {
  isPC.value = val
  platformInitialized = true
}

export function _resetPlatformForTest() {
  platformInitialized = false
  isPC.value = false
}
