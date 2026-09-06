/**
 * Custom wallpaper runtime manager.
 *
 * Reads the active wallpaper file name + panel opacity from the server config
 * (`appearance.wallpaper_file` / `appearance.panel_opacity`) and applies them
 * to the document as CSS variables consumed by the .wallpaper-layer and the
 * wallpaper-active surface rules (see web/css/base.css):
 *
 *   --wallpaper-url   → url(/api/file/theme-background) or `none`
 *   --wallpaper-scrim → translucent black overlay, strength follows the
 *                       resolved theme's light/dark base
 *   --panel-alpha     → opacity multiplier for main work-panel surfaces
 *
 * Applying also toggles the `wallpaper-active` class on <html> so surface
 * translucency is scoped to "a wallpaper is actually set" — without it every
 * surface renders byte-identical to pre-wallpaper.
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

/** URL of the served wallpaper image, including a version query for cache busting. */
export function wallpaperImageUrl(): string {
  return buildImageUrl()
}

/** Scrim overlay color for a resolved theme base. */
export function wallpaperScrim(dark: boolean): string {
  return dark ? 'rgba(0, 0, 0, 0.35)' : 'rgba(0, 0, 0, 0.12)'
}

/**
 * Apply (or clear) the wallpaper effect for the given state.
 * `wallpaperFile` — active file name from server config ('' = none).
 * `panelOpacity`  — 0.7..1.0 opacity multiplier (clamped).
 * `dark`          — current resolved theme is dark (drives scrim strength).
 * `forceBust`     — when true, regenerate the image URL even if the file name is
 *                   unchanged. Pass after an upload/replace that reuses the same
 *                   file name (new bytes must bypass the immutable cache).
 *
 * The image URL is only rewritten when the file name changes (or forceBust is
 * set); alpha/scrim updates never re-download the wallpaper.
 */
export function applyWallpaper(wallpaperFile: string, panelOpacity: number, dark: boolean, forceBust = false): void {
  const el = document.documentElement
  const active = !!wallpaperFile

  if (wallpaperFile !== lastAppliedFile) {
    lastImageUrl = active ? buildImageUrl() : null
    lastAppliedFile = wallpaperFile
  } else if (forceBust && active) {
    lastImageUrl = buildImageUrl()
  }

  el.style.setProperty('--wallpaper-url', lastImageUrl ? `url("${lastImageUrl}")` : 'none')
  el.style.setProperty('--wallpaper-scrim', active ? wallpaperScrim(dark) : 'transparent')

  const alpha = Number.isFinite(panelOpacity) ? Math.min(1, Math.max(0.7, panelOpacity)) : 0.85
  // Store the panel opacity as a <percentage> so the CSS color-mix stops are
  // plain percentages (calc() inside color-mix trips some CSS minifiers).
  el.style.setProperty('--panel-alpha', `${Math.round(alpha * 1000) / 10}%`)

  el.classList.toggle('wallpaper-active', active)
  appLog.d('ThemeBg', `applyWallpaper file=${wallpaperFile || '(none)'} alpha=${alpha} dark=${dark} url=${lastImageUrl ?? 'none'}`)
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
