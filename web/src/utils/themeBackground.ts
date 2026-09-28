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
import { buildLocalFileUrl } from '@/utils/download'
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
 * Build the wallpaper image URL for a bare file name. Only meaningful when a
 * wallpaper file is resolved; otherwise returns an empty string.
 *
 * Served from the gallery/Bing image endpoint, which resolves the name to its
 * theme subdirectory (local/ or bing/) — the same endpoint gallery thumbnails
 * use, so unprefixed legacy names are no longer a special case.
 */
function buildImageUrl(file: string): string {
  if (!file) return ''
  imageUrlNonce += 1
  return `/api/file/theme-wallpaper?name=${encodeURIComponent(file)}&v=${Date.now()}-${imageUrlNonce}`
}

/**
 * URL for one specific gallery/Bing image, by bare file name. Used for gallery
 * thumbnails so each tile shows its own image rather than the active one.
 *
 * The URL is cached per file name: gallery tiles call this during every render
 * (e.g. when `busy` toggles), and a fresh version query each time would treat
 * every tile as a new URL and re-download the whole gallery, bypassing the
 * server's immutable caching.
 */
const galleryUrlCache = new Map<string, string>()

/**
 * Preview thumbnail width in CSS pixels. The gallery tile is 72px wide, so 144
 * covers a 2x device pixel ratio without waste.
 */
export const THUMB_WIDTH = 144

/** Extensions GET /api/fs/thumb can rasterize; anything else falls back. */
const THUMB_RASTER_EXTS = ['.png', '.jpg', '.jpeg', '.gif']

/**
 * URL for a small JPEG preview of a wallpaper.
 *
 * Prefers GET /api/fs/thumb, which scales server-side: the settings panel
 * draws a 72px tile, and serving the full image meant downloading 2.4MB for the
 * Bing 4K wallpaper (a ~470x waste) on every render.
 *
 * Falls back to the full-size endpoint when the thumbnail route cannot serve
 * the file — notably SVG, which /api/fs/thumb does not rasterize (it handles
 * png/jpg/gif only) but which the wallpaper endpoint serves under a sandbox CSP.
 * A missing absPath (older server, or an unresolvable name) also falls back.
 */
export function galleryImageUrl(name: string, absPath?: string): string {
  const cached = galleryUrlCache.get(name)
  if (cached) return cached

  const version = `${Date.now()}-${(imageUrlNonce += 1)}`
  const ext = name.slice(name.lastIndexOf('.')).toLowerCase()
  const canThumb = !!absPath && THUMB_RASTER_EXTS.includes(ext)

  const url = canThumb
    ? `/api/fs/thumb?target=${encodeURIComponent(absPath)}&w=${THUMB_WIDTH}`
    : `/api/file/theme-wallpaper?name=${encodeURIComponent(name)}&v=${version}`

  galleryUrlCache.set(name, url)
  return url
}

/**
 * Drop cached thumbnail URLs for the given file names (or all of them), forcing
 * a re-fetch on the next render. Call after an upload/replace that reuses a name
 * with new bytes, since the version query must change to bypass the cache.
 */
export function invalidateGalleryImageUrls(names?: string[]): void {
  if (!names) {
    galleryUrlCache.clear()
    return
  }
  for (const n of names) galleryUrlCache.delete(n)
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
    lastImageUrl = active ? buildImageUrl(wallpaperFile) : null
    lastAppliedFile = wallpaperFile
  } else if (forceBust && active) {
    lastImageUrl = buildImageUrl(wallpaperFile)
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
 * `waveActive`    — when true, the animated wave is the background. It has no
 *                   file, so `wallpaperFile` is empty, but the translucent
 *                   panel surfaces must still be turned on. Defaults to false,
 *                   which keeps every pre-existing 4-arg call behaving exactly
 *                   as before.
 *
 * Maintains the shared file→URL cache (see resolveWallpaperUrl) and writes the
 * scrim/panel-alpha CSS variables + wallpaper-active class. Callers that bind
 * an <img> src use resolveWallpaperUrl() for the URL; the --wallpaper-url CSS
 * variable is kept written here for compatibility but is no longer the
 * rendering source of truth.
 */
export function applyWallpaper(
  wallpaperFile: string,
  panelOpacity: number,
  dark: boolean,
  forceBust = false,
  waveActive = false,
): void {
  const el = document.documentElement
  const hasImage = !!wallpaperFile
  // The scrim only makes sense over an image; the wave needs no darkening.
  const active = hasImage || waveActive
  const url = resolveWallpaperUrl(wallpaperFile, forceBust)

  el.style.setProperty('--wallpaper-url', url ? `url("${url}")` : 'none')
  el.style.setProperty('--wallpaper-scrim', hasImage ? wallpaperScrim(dark) : 'transparent')

  const alpha = Number.isFinite(panelOpacity) ? Math.min(1, Math.max(0.5, panelOpacity)) : 0.85
  // Store the panel opacity as a <percentage> so the CSS color-mix stops are
  // plain percentages (calc() inside color-mix trips some CSS minifiers).
  el.style.setProperty('--panel-alpha', `${Math.round(alpha * 1000) / 10}%`)

  el.classList.toggle('wallpaper-active', active)
  appLog.d('ThemeBg', `applyWallpaper file=${wallpaperFile || '(none)'} wave=${waveActive} alpha=${alpha} dark=${dark} url=${url || 'none'}`)
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
 * Whether an *image* wallpaper is displayed on this device.
 *
 * Careful with the name: `'unset'` means "no image file", NOT "no background".
 * The animated wave has no file, so it reports `'unset'` here while still being
 * a live background — check the mode for that.
 *
 * All three inputs are per-device (mode and enabled from localStorage, the
 * Bing file from the server's cache), so unlike before this is not a question
 * the server answers. The one asynchronous input left is the Bing cache: until
 * /api/config has been read we cannot know whether the Bing image exists yet,
 * which is what `'unknown'` now means.
 */
export function resolveWallpaperState(
  mode: WallpaperMode,
  enabled: boolean,
  activeFile: string,
  configLoaded: boolean,
): WallpaperState {
  if (!enabled) return 'unset'
  if (mode === 'wave') return 'unset' // a live background, but not an image
  if (mode === 'bing' && !configLoaded) return 'unknown'
  return activeFile ? 'set' : 'unset'
}

/** Wallpaper source currently in effect. */
export type WallpaperMode = 'none' | 'local' | 'bing' | 'wave'

/** Resolve the stored wallpaper source, falling back to 'none'. */
export function resolveWallpaperMode(value: unknown): WallpaperMode {
  if (value === 'local' || value === 'bing' || value === 'wave') return value
  return 'none'
}

/**
 * Whether the animated wave is the active background.
 *
 * The wave has no file, so this cannot be derived from the active file — it
 * must read the mode. `enabled` still gates it.
 */
export function isWaveActive(mode: WallpaperMode, enabled: boolean): boolean {
  return mode === 'wave' && enabled
}

/**
 * Bare name of the wallpaper this device should display, or '' when none.
 *
 * The client resolves this itself now: the server no longer knows which device
 * wants which wallpaper, so it cannot compute an authoritative "active file".
 * The Bing name comes from the server's cache; the local name from this
 * device's own selection.
 */
export function resolveActiveFile(mode: WallpaperMode, localSelected: string, bingFile: string): string {
  if (mode === 'local') return localSelected
  if (mode === 'bing') return bingFile
  return '' // 'wave' / 'none' have no file
}

/** One entry in the local wallpaper gallery, as returned by GET /api/config. */
export interface GalleryItem {
  file: string
  name: string
  uploaded_at: number
  size: number
  /** Absolute path, for GET /api/fs/thumb (which takes a path, not a name). */
  abs_path?: string
}

/** Gallery items from the server config, defaulting to an empty list. */
export function resolveGalleryItems(appearance: Record<string, unknown> | undefined): GalleryItem[] {
  const local = appearance?.local as Record<string, unknown> | undefined
  const items = local?.items
  return Array.isArray(items) ? (items as GalleryItem[]) : []
}

/** Bing wallpaper state, as returned by GET /api/config or the status endpoint. */
export interface BingStatus {
  file: string
  last_success_date: string
  copyright: string
  title: string
  last_error: string
  last_attempt_at: number
  /** Absolute path, for GET /api/fs/thumb. */
  abs_path?: string
}

const EMPTY_BING_STATUS: BingStatus = {
  file: '',
  last_success_date: '',
  copyright: '',
  title: '',
  last_error: '',
  last_attempt_at: 0,
}

/** Bing state from the server config. */
export function resolveBingStatus(appearance: Record<string, unknown> | undefined): BingStatus {
  const bing = appearance?.bing as Partial<BingStatus> | undefined
  if (!bing) return { ...EMPTY_BING_STATUS }
  return { ...EMPTY_BING_STATUS, ...bing }
}

/**
 * Compute the effective panel opacity (default 0.85) from the stored local
 * preference. Out-of-range or non-numeric values fall back to the default; the
 * applier additionally clamps to the readable 0.5–1.0 range.
 *
 * This is a per-device display tweak stored in localStorage, alongside blur /
 * edge-fade / wave speed — not a server config value.
 */
export function resolvePanelOpacity(value: unknown): number {
  const v = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(v) && v > 0 && v <= 1 ? v : 0.85
}

/** Compute dark-ness from the resolved theme of the given stored theme value. */
export function currentThemeIsDark(storedTheme: string | undefined): boolean {
  return isDarkTheme(resolveThemeId(storedTheme ?? 'auto'))
}

/**
 * Whether this device wants the Bing wallpaper but no image is cached yet.
 *
 * This is the state right after a server starts: the worker fetches its first
 * Bing image in the background, so the first config response has no file.
 * Callers use this to poll briefly so the image appears without a manual
 * refresh.
 */
export function isBingFirstImagePending(
  mode: WallpaperMode,
  enabled: boolean,
  bingFile: string,
  configLoaded: boolean,
): boolean {
  if (!enabled || mode !== 'bing') return false
  return configLoaded && bingFile === ''
}

// ── API calls ─────────────────────────────────────────────────────────────────

/**
 * Set the wallpaper from a server-side image file: read its bytes via
 * /api/fs/raw/, upload them into the local gallery, then select the new
 * entry as the active wallpaper.
 *
 * The gallery is the only wallpaper store — a file picked in the file viewer
 * becomes a normal gallery entry the user can manage alongside uploads.
 *
 * The selection is written to this device's local config, not the server: the
 * server no longer auto-selects on upload, so the caller is the only place that
 * can make the new image actually appear.
 */
export async function setWallpaperFromPath(path: string, setLocal: (key: string, value: string) => void): Promise<void> {
  const resp = await fetch(buildLocalFileUrl(path))
  if (!resp.ok) throw new Error(`read wallpaper source failed: HTTP ${resp.status}`)
  const blob = await resp.blob()
  const name = path.split('/').pop() || 'wallpaper'
  const { items, errors } = await uploadGalleryImages([new File([blob], name, { type: blob.type })])
  if (!items.length) throw new Error(errors[0]?.error || 'upload wallpaper failed')
  setLocal('wallpaperLocalSelected', items[0].file)
  setLocal('wallpaperMode', 'local')
}

// ── Gallery API ───────────────────────────────────────────────────────────────

/** Result of a batch upload: accepted items plus per-file failures. */
export interface GalleryUploadResult {
  items: GalleryItem[]
  errors: { name: string; error: string }[]
}

/**
 * Upload one or more images into the local gallery via
 * POST /api/theme/local/upload. Uploads are best-effort per file, so a partial
 * failure is reported in `errors` rather than throwing.
 */
export async function uploadGalleryImages(files: File[]): Promise<GalleryUploadResult> {
  const form = new FormData()
  for (const f of files) form.append('files', f)
  const resp = await fetch('/api/theme/local/upload', { method: 'POST', body: form })
  if (!resp.ok) throw new Error(`upload gallery images failed: HTTP ${resp.status}`)
  return (await resp.json()) as GalleryUploadResult
}

/**
 * Remove one gallery image via DELETE /api/theme/local/item.
 *
 * The server drops the file and its gallery entry. Whether this device still
 * points at it is no longer the server's concern — callers that deleted the
 * image currently displayed must clear `wallpaperLocalSelected` themselves
 * (the settings panel does; another device's stale name self-heals on 404).
 */
export async function deleteGalleryItem(name: string): Promise<void> {
  const resp = await fetch(`/api/theme/local/item?name=${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!resp.ok) throw new Error(`delete gallery item failed: HTTP ${resp.status}`)
}

// ── Bing API ──────────────────────────────────────────────────────────────────

/**
 * Ask the server for an immediate Bing fetch via POST /api/theme/bing/sync.
 * The fetch runs in the background; poll fetchBingStatus() for the outcome.
 */
export async function syncBingNow(): Promise<BingStatus> {
  const resp = await fetch('/api/theme/bing/sync', { method: 'POST' })
  if (!resp.ok) throw new Error(`bing sync failed: HTTP ${resp.status}`)
  return (await resp.json()) as BingStatus
}

/** Read the current Bing fetch state via GET /api/theme/bing/status. */
export async function fetchBingStatus(): Promise<BingStatus> {
  const resp = await fetch('/api/theme/bing/status')
  if (!resp.ok) throw new Error(`bing status failed: HTTP ${resp.status}`)
  return (await resp.json()) as BingStatus
}
