import { ref, watch } from 'vue'
import { useAppMode } from './useAppMode'

const ua = typeof navigator !== 'undefined' ? navigator.userAgent : ''
export const isHarmonyUA = /HarmonyOS|OpenHarmony/i.test(ua)

/** Android UA (browser) — excludes Windows Phone spoofing */
export const isAndroidUA = /Android/i.test(ua) && !/Windows Phone/i.test(ua) && !isHarmonyUA

/** iOS UA (iPhone, iPad, iPod) — classic iOS UA string */
export const isIOSUA = /iPhone|iPad|iPod/i.test(ua)

/** iPadOS 13+ desktop-mode UA: sends "Macintosh" but has touch capability (maxTouchPoints > 0).
 * Real Macs have maxTouchPoints = 0; iPads requesting desktop sites have maxTouchPoints > 0. */
export const isIPadOSUA = /Macintosh/i.test(ua)
  && typeof navigator !== 'undefined'
  && navigator.maxTouchPoints > 0

/** Windows desktop/browser UA (NT kernel) — PowerShell/Win32-OpenSSH download path */
export const isWindowsUA = /Windows NT/i.test(ua)

/** Real macOS desktop UA — Macintosh + no touch (excludes iPadOS desktop-mode) */
export const isMacDesktopUA = /Macintosh/i.test(ua)
  && typeof navigator !== 'undefined'
  && navigator.maxTouchPoints === 0

/** Linux desktop browser UA — excludes Android (mobile UA carries Linux + Android) */
export const isLinuxDesktopUA = /Linux/i.test(ua)
  && !/Android/i.test(ua) && !isHarmonyUA

/**
 * Any mobile/touch OS user agent: Android, iOS, or iPadOS in desktop mode.
 *
 * iPadOS 13+ is the trap — it sends a "Macintosh" UA, so it looks like macOS
 * unless `maxTouchPoints` is consulted. Callers that mean "is this a desktop
 * computer?" must use this, not `!/Android/ && !/iPhone/`, or an iPad is handed
 * the macOS build of the desktop app.
 */
export const isMobileOSUA = isAndroidUA || isIOSUA || isIPadOSUA || isHarmonyUA

// ─── The three orthogonal axes ────────────────────────────────────────────────
//
// "What kind of machine is this?" used to be answered by a single conflated
// `isPC`, which mixed three unrelated questions. Every consumer now picks the
// axis it actually means; see the axis docs below for which is which.
//
//   1. HOST     — which shell is running the app (Electron / Android / browser)
//   2. INPUT    — the primary pointing device (touch vs mouse + keyboard)
//   3. VIEWPORT — how much room there is (isWideScreen, in useWideScreenLayout)
//
// They are genuinely independent. An Electron window can be narrow (host says
// desktop, viewport says compact). A touchscreen laptop reports a fine pointer
// (host says browser, input says mouse). An Android tablet in landscape is
// wide (host says Android, viewport says wide). Collapsing them into one
// boolean is what made the old `isPC` wrong at every one of those edges.

/**
 * HOST axis. Which shell hosts the app. Mirrors useAppMode(), which reads the
 * native bridge; in a plain browser all three resolve to web. Not a one-shot
 * snapshot — see `applyHostAxes` for why it follows the source.
 *
 * `isElectron` is the desktop shell; `isAndroidApp` is the Android WebView.
 * The two are NOT interchangeable: several behaviours exist only for Android
 * (where the OS suspends background connections, the soft keyboard resizes the
 * viewport, and the native bridge has Android-only methods). Gating those on
 * "native host" alone silently applies them to Electron too.
 */
const isElectron = ref(false)
const isAndroidApp = ref(false)
const isHarmonyApp = ref(false)
const isWebApp = ref(false)

/**
 * True in ANY native host. Use this only when the capability genuinely exists
 * on both shells (e.g. the `shareFile` bridge method, which Electron and
 * Android each implement). When the feature is Android-only — soft-keyboard
 * quirks, background-suspension policy, Android-specific permissions — test
 * `isAndroidApp` instead: the old `isAppMode` was routinely mistaken for
 * "Android" and silently applied those behaviours to the desktop shell.
 */
const isNativeApp = ref(false)

/**
 * INPUT axis. True when the PRIMARY pointer is coarse (a finger/stylus with no
 * mouse). Drives everything that is really about "can this user hover, right-
 * click, double-click, or hold a key while clicking".
 *
 * Prefers the media query, which describes the real device; falls back to the
 * mobile UA when matchMedia is unavailable (jsdom, very old engines). The
 * fallback matters: jsdom is the test environment, and returning `false` there
 * would make every touch-branch test take the desktop path.
 *
 * Read once, like the rest of this module — the media query can in principle
 * flip when a 2-in-1 detaches its keyboard, but re-reading it reactively would
 * re-run thumbnail sizing and layout computeds mid-session. A reload is the
 * expected way to pick up a pointer change (same as the old behaviour).
 */
const isTouchPrimary = ref(false)

let platformInitialized = false
/** Stops the host-axis watcher (set up on init, dropped by the test hooks). */
let stopFollowingHost: (() => void) | null = null

/** `(pointer: coarse)` with a UA fallback for engines without matchMedia. */
function detectTouchPrimary(): boolean {
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    try {
      return window.matchMedia('(pointer: coarse)').matches
    } catch {
      // Some engines throw on an unsupported query — fall through to the UA.
    }
  }
  return isMobileOSUA
}

/**
 * Copies the host axes out of `useAppMode()`.
 *
 * `isAndroidApp` is NOT just `isAppMode`: both native shells report true there,
 * and every Android-only behaviour would then leak onto Electron. `isHarmonyApp`
 * is not interchangeable with either — see the HOST axis note above.
 */
function applyHostAxes(src: ReturnType<typeof useAppMode>) {
  isElectron.value = src.isDesktopApp.value
  isHarmonyApp.value = src.isHarmonyApp?.value === true
  isAndroidApp.value = src.isAppMode.value && !src.isDesktopApp.value && !isHarmonyApp.value
  isWebApp.value = !src.isAppMode.value
  // Roll-up: `isAppMode` is isNativeApp() from the bridge, which is true for
  // both shells. Kept as its own ref (rather than a computed) so the test hook
  // can keep it consistent without re-reading the bridge.
  isNativeApp.value = src.isAppMode.value
}

/**
 * Initializes the host and input axes once, then keeps the host ones in sync
 * with `useAppMode()`.
 *
 * The input axis is read once on purpose (see `isTouchPrimary`). The host axes
 * follow the source instead of being a snapshot: `useAppMode()` re-reads the
 * bridge on every call, so a host that reports itself after the first read —
 * the case that made HarmonyOS fall back to the desktop attachment layout —
 * still reaches every consumer through this watcher. A latched copy would
 * never recover without a reload.
 */
export function usePlatformDetect() {
  if (!platformInitialized) {
    platformInitialized = true
    const source = useAppMode()
    applyHostAxes(source)
    isTouchPrimary.value = detectTouchPrimary()
    stopFollowingHost = watch(
      [
        () => source.isAppMode.value,
        () => source.isDesktopApp.value,
        () => source.isHarmonyApp?.value === true,
      ],
      () => applyHostAxes(source),
    )
  }
  return { isElectron, isAndroidApp, isHarmonyApp, isWebApp, isNativeApp, isTouchPrimary }
}

/** Test hook — force the axes for unit tests. Marks the module initialized so
 *  usePlatformDetect() will not overwrite them. */
export function _setPlatformForTest(v: {
  isElectron?: boolean
  isAndroidApp?: boolean
  isHarmonyApp?: boolean
  isWebApp?: boolean
  isTouchPrimary?: boolean
}) {
  if (v.isElectron !== undefined) isElectron.value = v.isElectron
  if (v.isAndroidApp !== undefined) isAndroidApp.value = v.isAndroidApp
  if (v.isHarmonyApp !== undefined) isHarmonyApp.value = v.isHarmonyApp
  if (v.isWebApp !== undefined) isWebApp.value = v.isWebApp
  if (v.isTouchPrimary !== undefined) isTouchPrimary.value = v.isTouchPrimary
  // Derived: keep the native flag consistent with whichever host axes were set,
  // so a test that only names isAndroidApp/isElectron still gets isNativeApp.
  isNativeApp.value = isElectron.value || isAndroidApp.value || isHarmonyApp.value
  // The forced values must win: drop the follow watcher, otherwise the next
  // bridge change would overwrite what the test just set.
  stopFollowingHost?.()
  stopFollowingHost = null
  platformInitialized = true
}

export function _resetPlatformForTest() {
  stopFollowingHost?.()
  stopFollowingHost = null
  platformInitialized = false
  isElectron.value = false
  isAndroidApp.value = false
  isHarmonyApp.value = false
  isWebApp.value = false
  isNativeApp.value = false
  isTouchPrimary.value = false
}
