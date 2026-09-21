import { ref } from 'vue'
import { isNativeApp, isDesktopApp as detectDesktopApp } from '@/utils/clawbenchNative'

// Module-level singleton — all consumers share the same state
const isAppMode = ref(false)
// True when the native host is the Electron desktop shell rather than the
// Android WebView. Both report isNativeApp() === true, but several behaviours
// only make sense on Android (where the OS kills background connections and
// the app is suspended). The desktop window is merely minimized — it keeps
// running, so it must NOT adopt the Android background policy. Most notably
// useGlobalEvents drops the WebSocket when the app is hidden, which on desktop
// would make notifications impossible to deliver.
const isDesktopApp = ref(false)
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
      if (window !== window.top) return { isAppMode, isDesktopApp }
      isAppMode.value = isNativeApp()
      isDesktopApp.value = isAppMode.value && detectDesktopApp()
    } catch {
      // window.top access may throw in cross-origin iframe — treat as web mode
    }
    if (isAppMode.value) {
      document.documentElement.setAttribute('data-app-mode', '')
    }
  }
  return { isAppMode, isDesktopApp }
}
