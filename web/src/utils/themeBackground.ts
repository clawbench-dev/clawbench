/**
 * Custom wallpaper runtime manager.
 *
 * The rendered wallpaper is an <img> whose src the App root binds to
 * resolveWallpaperUrl() — an <img> src swap reliably re-decodes in Android
 * WebView, unlike a CSS-custom-property-driven background-image (see
 * web/css/base.css .wallpaper-layer). This module additionally owns the
 * document-level side effects that panel translucency needs:
 *
 *   --wallpaper-scrim → translucent overlay color, follows light/dark base
 *   --panel-alpha     → opacity multiplier for main work-panel surfaces
 *   wallpaper-active  → class on <html> toggling the translucent surface rules
 *
 * --wallpaper-url is still written for compatibility but is no longer the
 * rendering source of truth (App.vue binds an <img> src instead).
 *
 * Wallpaper state is tri-state ('unknown' | 'set' | 'unset') because the
 * server config loads asynchronously after mount: until the config arrives we
 * cannot know whether a wallpaper is set, and UI that depends on that (e.g.
 * the panel-opacity slider's disabled state) must not flash a wrong value.
 */

import { appLog } from '@/utils/appLog'
import { isDarkTheme, resolveThemeId } from '@/utils/themeMeta'

export type WallpaperState = 'unknown' | 'set' | 'unset'

// Last applied wallpaper file name + image URL. The URL embeds a version query
// so re-uploads (same file name, new bytes) bypass the immutable cache — but we
// must NOT regenerate it on every call: applyWallpaper is invoked on each
// opacity-slider tick and a fresh URL would trigger a full re-download each time.
let lastAppliedFile = ''
let lastImageUrl: string | null = null
// Monotonic counter appended to the URL so two regenerations within the same
// millisecond (e.g. rapid same-name re-upload) still produce distinct URLs and
// bypass the browser's immutable cache.
let imageUrlNonce = 0

/**
 * Build the wallpaper image URL. Only meaningful when a wallpaper file is set;
 * otherwise returns an empty string.
 */
function buildImageUrl(): string {
  imageUrlNonce += 1
  return `/api/file/theme-background?v=${Date.now()}-${imageUrlNonce}`
}

// ── Share-page (public /share/{token}) wallpaper URLs ───────────────────────
// The share SPA is unauthenticated, so its wallpaper fetches must go through
// the token-scoped public endpoints instead of the auth-protected
// /api/file/theme-background. The capability token is the sole credential.

/** Relative base of the token-scoped wallpaper endpoints. */
export function shareWallpaperApiBase(token: string): string {
  return `/api/share/${encodeURIComponent(token)}`
}

/** Public appearance-config endpoint (wallpaper_file / panel_opacity). */
export function shareAppearanceUrl(token: string): string {
  return `${shareWallpaperApiBase(token)}/appearance`
}

/**
 * Public wallpaper image URL for the share page, with a cache-busting query.
 * Returns an empty string when `token` is blank.
 */
export function resolveShareWallpaperUrl(token: string): string {
  if (!token) return ''
  imageUrlNonce += 1
  return `${shareWallpaperApiBase(token)}/theme-background?v=${Date.now()}-${imageUrlNonce}`
}

/**
 * Shape of GET /api/share/{token}/appearance — the public wallpaper subset of
 * the server config (snake_case mirrors /api/config's appearance section).
 */
export interface ShareAppearance {
  appearance?: {
    wallpaper_file?: string
    panel_opacity?: number
  }
}

/** URL of the served wallpaper image, including a version query for cache busting. */
export function wallpaperImageUrl(): string {
  return buildImageUrl()
}

/** Scrim overlay color for a resolved theme base. */
export function wallpaperScrim(dark: boolean): string {
  return dark ? 'rgba(0, 0, 0, 0.35)' : 'rgba(0, 0, 0, 0.12)'
}

/**
 * Resolve the served image URL for a wallpaper file name, reusing the cached
 * URL while the file name is unchanged so unrelated updates (opacity-scrim
 * ticks, theme changes) never re-download the image. `forceBust` regenerates
 * the URL even for the same file name — pass after an upload/replace that
 * reuses the same name (new bytes must bypass the immutable cache).
 *
 * Returns an empty string when `wallpaperFile` is empty.
 */
export function resolveWallpaperUrl(wallpaperFile: string, forceBust = false): string {
  const active = !!wallpaperFile
  if (wallpaperFile !== lastAppliedFile) {
    lastImageUrl = active ? buildImageUrl() : null
    lastAppliedFile = wallpaperFile
  } else if (forceBust && active) {
    lastImageUrl = buildImageUrl()
  }
  return lastImageUrl ?? ''
}

/**
 * Apply (or clear) the wallpaper effect for the given state.
 * `wallpaperFile` — active file name from server config ('' = none).
 * `panelOpacity`  — 0.5..1.0 opacity multiplier (clamped).
 * `dark`          — current resolved theme is dark (drives scrim strength).
 * `forceBust`     — when true, regenerate the image URL even if the file name is
 *                   unchanged. Pass after an upload/replace that reuses the same
 *                   file name (new bytes must bypass the immutable cache).
 *
 * Maintains the shared file→URL cache (see resolveWallpaperUrl) and writes the
 * scrim/panel-alpha CSS variables + wallpaper-active class. Callers that bind
 * an <img> src use resolveWallpaperUrl() for the URL; the --wallpaper-url CSS
 * variable is kept written here for compatibility but is no longer the
 * rendering source of truth.
 */
export function applyWallpaper(wallpaperFile: string, panelOpacity: number, dark: boolean, forceBust = false): void {
  const el = document.documentElement
  const active = !!wallpaperFile
  const url = resolveWallpaperUrl(wallpaperFile, forceBust)

  el.style.setProperty('--wallpaper-url', url ? `url("${url}")` : 'none')
  el.style.setProperty('--wallpaper-scrim', active ? wallpaperScrim(dark) : 'transparent')
  // Store the panel opacity as a <percentage> so the CSS color-mix stops are
  // plain percentages (calc() inside color-mix trips some CSS minifiers).
  el.style.setProperty('--panel-alpha', resolvePanelAlphaCss(panelOpacity))

  el.classList.toggle('wallpaper-active', active)
  appLog.d('ThemeBg', `applyWallpaper file=${wallpaperFile || '(none)'} alpha=${panelOpacity} dark=${dark} url=${url || 'none'}`)
}

/**
 * Clamp + format the panel-opacity as a percentage string for --panel-alpha.
 * Kept separate so the share-page applier and the tests reuse the exact same
 * clamp/rounding as applyWallpaper.
 */
export function resolvePanelAlphaCss(panelOpacity: number): string {
  const alpha = Number.isFinite(panelOpacity) ? Math.min(1, Math.max(0.5, panelOpacity)) : 0.85
  return `${Math.round(alpha * 1000) / 10}%`
}

/**
 * Apply (or clear) the wallpaper effect on the public share SPA.
 * `token`         — share capability token (must be non-empty to fetch images).
 * `wallpaperFile` — active file name from the share appearance endpoint ('' = none).
 * `panelOpacity`  — 0.5..1.0 opacity multiplier (clamped).
 * `dark`          — current resolved theme is dark (drives scrim strength).
 *
 * Same document-level side effects as applyWallpaper (scrim / --panel-alpha /
 * wallpaper-active class), but the <img> src is bound separately via
 * resolveShareWallpaperUrl(token) because the share SPA renders on the
 * unauthenticated /share/{token} origin.
 */
export function applyShareWallpaper(token: string, wallpaperFile: string, panelOpacity: number, dark: boolean): void {
  const el = document.documentElement
  const active = !!wallpaperFile && !!token
  const url = active ? resolveShareWallpaperUrl(token) : ''

  el.style.setProperty('--wallpaper-url', url ? `url("${url}")` : 'none')
  el.style.setProperty('--wallpaper-scrim', active ? wallpaperScrim(dark) : 'transparent')
  el.style.setProperty('--panel-alpha', active ? resolvePanelAlphaCss(panelOpacity) : '100%')

  el.classList.toggle('wallpaper-active', active)
  appLog.d('ThemeBg', `applyShareWallpaper token=${!!token} file=${wallpaperFile || '(none)'} alpha=${panelOpacity} dark=${dark}`)
}

/** Reset the cached image URL/file state (used when the wallpaper is removed). */
export function resetWallpaperUrlCache(): void {
  lastAppliedFile = ''
  lastImageUrl = null
}

/** Reflect a theme change only (scrim strength) without touching the image. */
export function applyWallpaperScrim(dark: boolean): void {
  const el = document.documentElement
  if (el.classList.contains('wallpaper-active')) {
    el.style.setProperty('--wallpaper-scrim', wallpaperScrim(dark))
  }
}

/**
 * Resolve the wallpaper tri-state against the current server config.
 * The `appearance` section exists only after GET /api/config completes.
 */
export function resolveWallpaperState(appearance: Record<string, unknown> | undefined): WallpaperState {
  if (!appearance || appearance.wallpaper_file === undefined) return 'unknown'
  return appearance.wallpaper_file ? 'set' : 'unset'
}

/** Compute the effective panel opacity value (default 0.85). */
export function resolvePanelOpacity(appearance: Record<string, unknown> | undefined): number {
  const v = appearance?.panel_opacity
  return typeof v === 'number' && Number.isFinite(v) && v > 0 && v <= 1 ? v : 0.85
}

/** Compute dark-ness from the resolved theme of the given stored theme value. */
export function currentThemeIsDark(storedTheme: string | undefined): boolean {
  return isDarkTheme(resolveThemeId(storedTheme ?? 'auto'))
}

// ── API calls ─────────────────────────────────────────────────────────────────

/** Result of a successful set operation. */
export interface ThemeBackgroundSetResult {
  file: string
}

/**
 * Set the wallpaper from a server-side file (path-copy mode) via
 * POST /api/theme-background.
 */
export async function setWallpaperFromPath(path: string): Promise<ThemeBackgroundSetResult> {
  const resp = await fetch('/api/theme-background', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  })
  if (!resp.ok) throw new Error(`set wallpaper failed: HTTP ${resp.status}`)
  return (await resp.json()) as ThemeBackgroundSetResult
}

/**
 * Upload a local image file as the wallpaper (multipart mode) via
 * POST /api/theme-background.
 */
export async function uploadWallpaper(file: File): Promise<ThemeBackgroundSetResult> {
  const form = new FormData()
  form.append('file', file)
  const resp = await fetch('/api/theme-background', { method: 'POST', body: form })
  if (!resp.ok) throw new Error(`upload wallpaper failed: HTTP ${resp.status}`)
  return (await resp.json()) as ThemeBackgroundSetResult
}

/** Clear the active wallpaper via DELETE /api/theme-background. */
export async function clearWallpaper(): Promise<void> {
  const resp = await fetch('/api/theme-background', { method: 'DELETE' })
  if (!resp.ok) throw new Error(`clear wallpaper failed: HTTP ${resp.status}`)
}
