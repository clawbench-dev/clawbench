import { ref } from 'vue'
import { isNativeApp, isDesktopApp as detectDesktopApp, getNativeHostPlatform } from '@/utils/clawbenchNative'

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
const isHarmonyApp = ref(false)

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
 * Re-reads the host axes from the native bridge.
 *
 * Deliberately NOT one-shot: the bridge is installed by the host at document
 * start, but a page that evaluates before that injection (or a host that adds
 * it later) would otherwise latch "web" for the rest of the session, and every
 * consumer — Android CSS compensations, HarmonyOS layout, background policy —
 * would be wrong until a reload. Re-reading costs a few synchronous bridge
 * calls, and the refs only notify when a value actually changed.
 */
function readHostAxes() {
  try {
    if (window !== window.top) return
    isAppMode.value = isNativeApp()
    isDesktopApp.value = isAppMode.value && detectDesktopApp()
    isHarmonyApp.value = isAppMode.value && getNativeHostPlatform() === 'harmonyos'
  } catch {
    // window.top access may throw in cross-origin iframe — treat as web mode
  }
  // Android WebView ONLY. See WEBVIEW_MODE_ATTR for why the desktop shell must
  // not get the attribute despite reporting isNativeApp() === true. Kept in
  // sync with the axes (not set-once) so a bridge that appears late also gets
  // the attribute, and a non-Android host never inherits a stale one.
  if (typeof document === 'undefined') return
  const androidWebview = isAppMode.value && !isDesktopApp.value && !isHarmonyApp.value
  const hasAttr = document.documentElement.hasAttribute(WEBVIEW_MODE_ATTR)
  if (androidWebview && !hasAttr) document.documentElement.setAttribute(WEBVIEW_MODE_ATTR, '')
  else if (!androidWebview && hasAttr) document.documentElement.removeAttribute(WEBVIEW_MODE_ATTR)
}

/**
 * Detects if the app is running inside a native host (top-level frame).
 * Top-frame check is critical: a child iframe inherits the bridge but must run
 * in web mode (no port forward button, no native auto-login, etc.).
 */
export function useAppMode() {
  readHostAxes()
  return { isAppMode, isDesktopApp, isHarmonyApp }
}
