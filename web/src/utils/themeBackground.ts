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
 * Build the wallpaper image URL for a bare file name (the server-resolved
 * `active_file`). Only meaningful when a wallpaper file is set; otherwise
 * returns an empty string.
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

/** Extensions GET /api/file/thumb can rasterize; anything else falls back. */
const THUMB_RASTER_EXTS = ['.png', '.jpg', '.jpeg', '.gif']

/**
 * URL for a small JPEG preview of a wallpaper.
 *
 * Prefers GET /api/file/thumb, which scales server-side: the settings panel
 * draws a 72px tile, and serving the full image meant downloading 2.4MB for the
 * Bing 4K wallpaper (a ~470x waste) on every render.
 *
 * Falls back to the full-size endpoint when the thumbnail route cannot serve
 * the file — notably SVG, which /api/file/thumb does not rasterize (it handles
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
    ? `/api/file/thumb?path=${encodeURIComponent(absPath)}&w=${THUMB_WIDTH}`
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

  const alpha = Number.isFinite(panelOpacity) ? Math.min(1, Math.max(0.5, panelOpacity)) : 0.85
  // Store the panel opacity as a <percentage> so the CSS color-mix stops are
  // plain percentages (calc() inside color-mix trips some CSS minifiers).
  el.style.setProperty('--panel-alpha', `${Math.round(alpha * 1000) / 10}%`)

  el.classList.toggle('wallpaper-active', active)
  appLog.d('ThemeBg', `applyWallpaper file=${wallpaperFile || '(none)'} alpha=${alpha} dark=${dark} url=${url || 'none'}`)
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
 *
 * The server resolves which image is active (mode + enabled + selection) and
 * exposes it as `active_file`, so the client reads that rather than
 * reimplementing the precedence.
 */
export function resolveWallpaperState(appearance: Record<string, unknown> | undefined): WallpaperState {
  if (!appearance) return 'unknown'
  // Treat "key absent" as unknown rather than "no wallpaper", so the UI does
  // not briefly show an empty state while config is still loading.
  if (appearance.active_file === undefined) return 'unknown'
  return resolveActiveFile(appearance) ? 'set' : 'unset'
}

/** Wallpaper source currently in effect. */
export type WallpaperMode = 'none' | 'local' | 'bing'

/** Resolve the active wallpaper source from the server config. */
export function resolveWallpaperMode(appearance: Record<string, unknown> | undefined): WallpaperMode {
  const mode = appearance?.wallpaper_mode
  if (mode === 'local' || mode === 'bing') return mode
  return 'none'
}

/** Whether the wallpaper layer is globally enabled (default: enabled). */
export function resolveWallpaperEnabled(appearance: Record<string, unknown> | undefined): boolean {
  if (!appearance || appearance.wallpaper_enabled === undefined) return true
  return appearance.wallpaper_enabled !== false
}

/**
 * Bare name of the wallpaper currently displayed, or '' when none is active.
 *
 * `active_file` is authoritative, including an empty string, which is how the
 * server reports "no wallpaper" (globally disabled, nothing selected, or
 * nothing cached yet).
 */
export function resolveActiveFile(appearance: Record<string, unknown> | undefined): string {
  const active = appearance?.active_file
  return typeof active === 'string' ? active : ''
}

/** One entry in the local wallpaper gallery, as returned by GET /api/config. */
export interface GalleryItem {
  file: string
  name: string
  uploaded_at: number
  size: number
  /** Absolute path, for GET /api/file/thumb (which takes a path, not a name). */
  abs_path?: string
}

/** Gallery items from the server config, defaulting to an empty list. */
export function resolveGalleryItems(appearance: Record<string, unknown> | undefined): GalleryItem[] {
  const local = appearance?.local as Record<string, unknown> | undefined
  const items = local?.items
  return Array.isArray(items) ? (items as GalleryItem[]) : []
}

/** Bare name of the selected gallery image, or ''. */
export function resolveGallerySelected(appearance: Record<string, unknown> | undefined): string {
  const local = appearance?.local as Record<string, unknown> | undefined
  const selected = local?.selected
  return typeof selected === 'string' ? selected : ''
}

/** Bing wallpaper state, as returned by GET /api/config or the status endpoint. */
export interface BingStatus {
  enabled: boolean
  file: string
  last_success_date: string
  copyright: string
  title: string
  mkt: string
  last_error: string
  last_attempt_at: number
  /** Absolute path, for GET /api/file/thumb. */
  abs_path?: string
}

const EMPTY_BING_STATUS: BingStatus = {
  enabled: false,
  file: '',
  last_success_date: '',
  copyright: '',
  title: '',
  mkt: '',
  last_error: '',
  last_attempt_at: 0,
}

/** Bing state from the server config. */
export function resolveBingStatus(appearance: Record<string, unknown> | undefined): BingStatus {
  const bing = appearance?.bing as Partial<BingStatus> | undefined
  if (!bing) return { ...EMPTY_BING_STATUS }
  return { ...EMPTY_BING_STATUS, ...bing }
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

/**
 * Whether the Bing wallpaper is enabled but has no cached image yet.
 *
 * This is the state right after a fresh install: the server enables the Bing
 * wallpaper at startup and fetches its first image in the background, so the
 * first config response has no image. Callers use this to poll briefly so the
 * factory wallpaper appears without a manual refresh.
 */
export function isBingFirstImagePending(appearance: Record<string, unknown> | undefined): boolean {
  if (resolveWallpaperMode(appearance) !== 'bing') return false
  if (!resolveWallpaperEnabled(appearance)) return false
  return resolveActiveFile(appearance) === ''
}

// ── API calls ─────────────────────────────────────────────────────────────────

/**
 * Set the wallpaper from a server-side image file: read its bytes via
 * /api/local-file/, upload them into the local gallery, then select the new
 * entry as the active wallpaper.
 *
 * The gallery is the only wallpaper store — a file picked in the file viewer
 * becomes a normal gallery entry the user can manage alongside uploads.
 */
export async function setWallpaperFromPath(path: string): Promise<WallpaperStateResult> {
  const resp = await fetch(buildLocalFileUrl(path))
  if (!resp.ok) throw new Error(`read wallpaper source failed: HTTP ${resp.status}`)
  const blob = await resp.blob()
  const name = path.split('/').pop() || 'wallpaper'
  const { items, errors } = await uploadGalleryImages([new File([blob], name, { type: blob.type })])
  if (!items.length) throw new Error(errors[0]?.error || 'upload wallpaper failed')
  return selectGalleryItem(items[0].file)
}

// ── Gallery API ───────────────────────────────────────────────────────────────

/** Result of a batch upload: accepted items plus per-file failures. */
export interface GalleryUploadResult {
  items: GalleryItem[]
  errors: { name: string; error: string }[]
}

/** Wallpaper state returned by the mutation endpoints. */
export interface WallpaperStateResult {
  mode: string
  enabled: boolean
  active_file: string
  selected: string
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

/** Remove one gallery image via DELETE /api/theme/local/item. */
export async function deleteGalleryItem(name: string): Promise<WallpaperStateResult> {
  const resp = await fetch(`/api/theme/local/item?name=${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!resp.ok) throw new Error(`delete gallery item failed: HTTP ${resp.status}`)
  return (await resp.json()) as WallpaperStateResult
}

/** Make a gallery image the active wallpaper via POST /api/theme/local/select. */
export async function selectGalleryItem(name: string): Promise<WallpaperStateResult> {
  const resp = await fetch('/api/theme/local/select', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!resp.ok) throw new Error(`select gallery item failed: HTTP ${resp.status}`)
  return (await resp.json()) as WallpaperStateResult
}

// ── Mode / Bing API ───────────────────────────────────────────────────────────

/**
 * Switch the active wallpaper source and/or toggle the global wallpaper switch
 * via POST /api/theme/wallpaper. Only the supplied fields are applied.
 */
export async function setWallpaperMode(patch: { mode?: WallpaperMode; enabled?: boolean }): Promise<WallpaperStateResult> {
  const resp = await fetch('/api/theme/wallpaper', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  })
  if (!resp.ok) throw new Error(`set wallpaper mode failed: HTTP ${resp.status}`)
  return (await resp.json()) as WallpaperStateResult
}

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

/**
 * Map a UI locale to the Bing market parameter. Mirrors the server-side
 * mapping in internal/wallpaper so the persisted market matches what the
 * fetch worker would choose on its own.
 */
export function bingMktForLocale(locale: string): string {
  return locale.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US'
}
