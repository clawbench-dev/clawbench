/**
 * Pure decision logic for the desktop splash overlay.
 *
 * The overlay itself (`splash.ts`) and its wiring (`window.ts`, `bridge.ts`) all
 * import `electron` at module scope, so a unit test cannot load them. Everything
 * that is a *decision* rather than an Electron call lives here instead, mirroring
 * how `loadFailure.ts` was split out of `window.ts` for the same reason.
 */

/**
 * The stages shown while the app is starting up.
 *
 * Mirrors the Android splash, whose `getSplashStatusText` buckets WebView load
 * progress into the same four messages. Android also has a fifth ("Almost
 * ready…") for its 90-100% bucket; Electron's `webContents` reports no progress
 * percentage, so that bucket has no equivalent event and is intentionally
 * omitted rather than faked.
 */
export type SplashStage = 'connecting' | 'loading' | 'rendering' | 'initializing'

/** Stage order, so a later event can never move the display backwards. */
export const SPLASH_STAGE_ORDER: readonly SplashStage[] = [
  'connecting',
  'loading',
  'rendering',
  'initializing',
]

/**
 * How long the overlay may stay up after the page reports it finished loading.
 *
 * The JS app dismisses the splash itself via `ClawBenchNative.dismissSplash()`
 * once `initializeApp()` resolves. If that never happens — initialization threw,
 * or the bridge is missing — the user would be stuck behind the overlay with no
 * way forward. Matches Android's `SPLASH_FAILSAFE_MS`.
 */
export const SPLASH_FAILSAFE_MS = 15_000

/**
 * How long to wait for a server navigation before giving up and returning to the
 * login page. Matches Android's `CONNECTION_TIMEOUT_MS`.
 */
export const CONNECTION_TIMEOUT_MS = 90_000

/** The `webContents` event that advances the overlay to `stage`. */
export interface SplashStageEvent {
  /** A `webContents` event name. */
  event: string
  stage: SplashStage
}

/**
 * The events that drive the stage display, in order.
 *
 * `webContents` fires these once per navigation, which is exactly the cadence
 * the display needs: one pass through the four messages per connect attempt.
 */
export const SPLASH_STAGE_EVENTS: readonly SplashStageEvent[] = [
  { event: 'did-start-loading', stage: 'loading' },
  { event: 'dom-ready', stage: 'rendering' },
  { event: 'did-finish-load', stage: 'initializing' },
]

/**
 * Resolve the stage for a `webContents` event, or null when the event does not
 * drive the display.
 *
 * Returning null (rather than a default) keeps an unrelated event from silently
 * resetting the text to the first stage.
 */
export function stageForEvent(event: string): SplashStage | null {
  const match = SPLASH_STAGE_EVENTS.find((e) => e.event === event)
  return match ? match.stage : null
}

/**
 * Whether `next` is a forward move from `current`.
 *
 * A navigation fires its events in order, but a reload or a second navigation
 * while the overlay is still up can re-fire an earlier one. Showing "Loading
 * page…" after "Initializing app…" reads as a stall, so those are ignored.
 * `current` of null (nothing shown yet) always accepts.
 */
export function isForwardStage(current: SplashStage | null, next: SplashStage): boolean {
  if (current === null) return true
  return SPLASH_STAGE_ORDER.indexOf(next) > SPLASH_STAGE_ORDER.indexOf(current)
}

/**
 * Whether a navigation to `url` should show the overlay.
 *
 * Only remote (http/https) navigations do. The first-run login page is a local
 * `file://` document with nothing to wait for, so showing the overlay there
 * would be a one-frame flash of the logo before the login form appears.
 */
export function shouldShowSplash(url: string): boolean {
  return /^https?:\/\//i.test(url)
}

/** Overlay backdrop for a dark theme — `github-dark`'s `--bg-primary`. */
export const SPLASH_BG_DARK = '#161b22'

/** Overlay backdrop for a light theme — `github-light`'s `--bg-primary`. */
export const SPLASH_BG_LIGHT = '#f8f9fa'

/**
 * The colour painted behind the overlay before its HTML has parsed.
 *
 * This is deliberately a two-value approximation rather than the full 36-theme
 * palette. It only covers the handful of frames between the view being shown and
 * `login.html` applying the real theme from `[data-theme]` in its `<head>`;
 * copying the whole palette here would create a third copy to keep in sync
 * (`ThemePalette.java` on Android is that copy, and `theme.test.ts` exists
 * because the two login pages already drifted once).
 *
 * `isDark` comes from the caller, which has `shared/theme.ts` available.
 */
export function splashBackgroundColor(isDark: boolean): string {
  return isDark ? SPLASH_BG_DARK : SPLASH_BG_LIGHT
}
