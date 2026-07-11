import { ref } from 'vue'

// Module-level singleton — all consumers share the same state
const isAppMode = ref(false)
let initialized = false

/**
 * Detects if the app is running inside the Android native WebView (top-level frame).
 *
 * Two conditions must both be true:
 * 1. AndroidNative.isNativeApp() returns true (JS Bridge injected by native app)
 * 2. window === window.top (we are in the top-level frame, not an iframe)
 *
 * Condition 2 is critical: Android's addJavascriptInterface injects the bridge
 * into ALL frames including child iframes. If a child iframe inherits the bridge
 * but should run in web mode (no port forward button, no native auto-login, etc.),
 * the top-frame check prevents false positives.
 * User-Agent is intentionally NOT checked — it's inherited by iframes and
 * causes false positives in port-forwarded pages.
 */
export function useAppMode() {
  if (!initialized) {
    initialized = true
    try {
      // Must be in the top-level frame — iframe is always web mode
      if (window !== window.top) return { isAppMode }
      if (typeof (window as any).AndroidNative !== 'undefined') {
        const flag = (window as any).AndroidNative.isNativeApp?.()
        // WebView may box booleans / return 1 — avoid === true
        isAppMode.value = flag === true || flag === 1 || flag === 'true' || flag === '1'
      }
    } catch {
      // window.top access may throw in cross-origin iframe — treat as web mode
    }
    // Mark <html> with data-app-mode for WebView-specific CSS overrides
    if (isAppMode.value) {
      document.documentElement.setAttribute('data-app-mode', '')
    }
  }
  return { isAppMode }
}
