import { ref } from 'vue'
import { isNativeApp } from '@/utils/clawbenchNative'

// Module-level singleton — all consumers share the same state
const isAppMode = ref(false)
let initialized = false

/**
 * Detects if the app is running inside a native host (top-level frame).
 * Top-frame check is critical: a child iframe inherits the bridge but must run
 * in web mode (no port forward button, no native auto-login, etc.).
 */
export function useAppMode() {
  if (!initialized) {
    initialized = true
    try {
      if (window !== window.top) return { isAppMode }
      isAppMode.value = isNativeApp()
    } catch {
      // window.top access may throw in cross-origin iframe — treat as web mode
    }
    if (isAppMode.value) {
      document.documentElement.setAttribute('data-app-mode', '')
    }
  }
  return { isAppMode }
}
