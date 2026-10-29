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
 * The root attribute that switches on the **Android WebView** rendering
 * compensations (CSS-only; nothing in JS reads it).
 *
 * WHY IT IS NOT `isAppMode`: `isNativeApp()` is true for the Electron desktop
 * shell too, so an attribute keyed on it fired there as well — and every one of
 * its ~16 consumers is an Android workaround that is actively harmful on a
 * desktop Chromium:
 *
 *   - `will-change: transform` on `.chat-panel-content` makes the whole chat
 *     panel a permanent compositing layer, and `contain: paint` on every
 *     `.chat-message` slices the scroller into per-row paint chunks. On the
 *     desktop shell that combination produced a mis-painted row: a single line
 *     of a later thinking block drawn at the top of the message, gone again
 *     after switching sessions (the DOM is rebuilt).
 *   - `-webkit-text-stroke` bold compensation double-darkens glyphs that the
 *     desktop engine already renders with a real bold face.
 *
 * The JS side already draws this distinction everywhere it matters
 * (`App.vue`'s `onAppResume`, `useGlobalEvents`, port-forward resume all write
 * `isAppMode && !isDesktopApp`); the CSS hook was the one place that did not.
 *
 * Renamed from `data-app-mode` so the name can no longer be read as "any app
 * shell" — the failure was partly a naming one.
 */
export const WEBVIEW_MODE_ATTR = 'data-webview-mode'

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
    // Android WebView ONLY. See WEBVIEW_MODE_ATTR for why the desktop shell
    // must not get the attribute despite reporting isNativeApp() === true.
    if (isAppMode.value && !isDesktopApp.value) {
      document.documentElement.setAttribute(WEBVIEW_MODE_ATTR, '')
    }
  }
  return { isAppMode, isDesktopApp }
}
