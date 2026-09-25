/**
 * useMediaWatch — keeps locally-served images in sync with the files on disk.
 *
 * Why this exists
 * ---------------
 * Markdown previews, chat messages and the file-manager grids render images
 * through `v-html` or plain `<img src>`. When the underlying file is rewritten
 * in the background (the AI redraws a diagram, an export regenerates a PNG),
 * the *markdown source* has not changed — so Vue has no reason to re-render,
 * and the `<img>` keeps its already-loaded bytes. Bumping the `?t=` param at
 * render time cannot help either: the render already happened.
 *
 * Two mechanisms are therefore needed:
 *
 * 1. **Discovery** — the file watcher can only report changes for paths it
 *    watches. The set of images currently on screen is discovered by scanning
 *    the live DOM, so the watcher is told exactly which files matter
 *    (`mediaPaths`). This covers every surface at once, including `v-html`
 *    output that no component owns element-by-element.
 *
 * 2. **Cache-busting** — when the watcher reports a change, `bumpVersion()`
 *    rewrites the `src` / `data-full-src` of the matching `<img>` elements in
 *    place. This deliberately bypasses Vue: the element is patched directly, so
 *    a surface that never re-renders still gets the new bytes. Vue-bound URLs
 *    additionally read `mediaVersionFor()` in their computed, so a later
 *    re-render cannot revert to the stale URL.
 *
 * The version map is module-level singleton state: one file has one version
 * across every surface showing it, and the watcher connection is per-app.
 */

import { ref, type Ref } from 'vue'
import { appLog } from '@/utils/appLog'

const TAG = 'MediaWatch'

/**
 * Current project root, used to relativize absolute media paths.
 *
 * Injected rather than read from the app store: importing the store pulls in
 * `utils/api` → `i18n`, and this module is imported by many components whose
 * tests stub `vue-i18n` minimally. Callers (the file watcher) set it once.
 */
let projectRoot = ''

/** Set the project root used to relativize absolute paths. */
export function setMediaProjectRoot(root: string): void {
  projectRoot = root || ''
}

/**
 * Normalize a path for identity comparison: forward slashes, no leading "./",
 * no duplicate or trailing slashes.
 *
 * Inlined rather than imported from `@/utils/path.ts` on purpose: this module
 * is a global DOM observer imported by many components, and several of their
 * tests partially mock `path.ts` with only the handful of helpers they need.
 * Adding a new import from there would break those mocks for reasons unrelated
 * to what they test. The function is small and self-contained, so the
 * duplication is the cheaper trade.
 */
function normalizePath(path: string): string {
  return path
    .replace(/\\/g, '/')
    .replace(/^\.\/+/, '')
    .replace(/\/+/g, '/')
    .replace(/\/+$/, '')
}

/**
 * Upper bound on paths handed to the watcher. The backend caps at the same
 * number (`service.MaxMediaWatchPaths`); each distinct parent directory costs
 * one inotify watch, so an unbounded list would exhaust the process limit.
 */
export const MAX_MEDIA_WATCH_PATHS = 200

// ─── Version map (module-level singleton) ────────────────────────────────────

/**
 * Project-relative path → monotonic version. Reactive so Vue-bound image URLs
 * recompute; read via `mediaVersionFor`.
 */
export const mediaVersions: Ref<Record<string, number>> = ref({})

/** Version of a media file, or 0 when it has never been reported as changed. */
export function mediaVersionFor(path: string): number {
  if (!path) return 0
  return mediaVersions.value[normalizePath(path)] ?? 0
}

/**
 * Remove any existing `t=` cache-buster and tidy the separators it leaves
 * behind, so a new one can be appended anywhere in the query string.
 *
 * The value is treated as OPAQUE — callers emit several shapes
 * (`<timestamp>`, and `<timestamp>.<version>` from the image viewer), so a
 * digits-only pattern would silently mangle the dotted form into the path
 * (e.g. `a.png?t=1.0` → `a.png.0`, a 404). Matches up to the next `&`.
 *
 * This is the single implementation: the lightbox used to carry its own
 * digits-only copy, which is exactly how the dotted form broke it.
 */
export function stripVersionParam(url: string): string {
  if (!url) return url
  return url
    .replace(/([?&])t=[^&]*/g, '$1')
    .replace(/\?&+/g, '?')
    .replace(/&{2,}/g, '&')
    .replace(/[?&]+$/, '')
}

/** Append (or replace) a `t=` cache-buster on a URL. */
export function withVersionParam(url: string, version: number): string {
  if (!url) return url
  const stripped = stripVersionParam(url)
  const sep = stripped.includes('?') ? '&' : '?'
  return `${stripped}${sep}t=${version}`
}

// ─── URL / element → project-relative path ───────────────────────────────────

/**
 * Extract the project-relative file path a media URL points at, or null when
 * the URL is not a watchable local file (external, data:, share-token, svg
 * inline…).
 *
 * Handles both URL shapes the render pipelines emit:
 *   - `/api/fs/raw/<rel>?t=…`            (full-size original)
 *   - `/api/fs/raw/?target=<abs>`        (absolute path form)
 *   - `/api/fs/thumb?target=<rel>&w=…`   (inline thumbnail)
 *
 * The returned path is decoded and normalized so it compares equal to the
 * project-relative path the watcher reports.
 */
export function mediaPathFromUrl(url: string): string | null {
  if (!url) return null
  // Only the two authenticated local-serving endpoints are watchable. Share
  // mode (`/api/share/<token>/local/…`) is a read-only anonymous surface with
  // no watcher connection.
  let pathname: string
  let search: string
  try {
    const parsed = new URL(url, 'http://localhost')
    pathname = parsed.pathname
    search = parsed.search
  } catch {
    return null
  }

  if (pathname === '/api/fs/thumb') {
    const p = new URLSearchParams(search).get('target')
    return p ? normalizePath(safeDecode(p)) : null
  }

  if (pathname === '/api/fs/raw/' || pathname === '/api/fs/raw') {
    const p = new URLSearchParams(search).get('target')
    return p ? toWatchablePath(p) : null
  }

  const prefix = '/api/fs/raw/'
  if (pathname.startsWith(prefix)) {
    const rel = pathname.slice(prefix.length).replace(/^\/+/, '')
    return rel ? toWatchablePath(safeDecode(rel)) : null
  }

  return null
}

/**
 * Normalize a path to the project-relative key used by the registry. Absolute
 * paths under the project root are relativized; anything else is kept as-is and
 * will simply fail backend validation (and be dropped there).
 */
function toWatchablePath(path: string): string {
  const root = projectRoot
  const normalized = normalizePath(path)
  if (!root) return normalized
  const normRoot = normalizePath(root)
  const lower = normalized.toLowerCase()
  const lowerRoot = normRoot.toLowerCase()
  if (lower === lowerRoot) return ''
  if (lower.startsWith(lowerRoot + '/')) {
    return normalized.slice(normRoot.length + 1)
  }
  return normalized
}

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

/** Project-relative path of the media an <img> renders, or null. */
export function mediaPathFromImg(img: Element): string | null {
  const attach = img.getAttribute('data-attach-src')
  if (attach) return normalizePath(attach)
  const full = img.getAttribute('data-full-src')
  if (full) {
    const p = mediaPathFromUrl(full)
    if (p) return p
  }
  const src = img.getAttribute('src')
  return src ? mediaPathFromUrl(src) : null
}

// ─── Live DOM patching ───────────────────────────────────────────────────────

/**
 * Rewrite every on-screen <img> that renders `path` so the browser re-fetches
 * it. Works for `v-html` surfaces because it mutates the elements directly
 * instead of waiting for a re-render that will never come.
 */
export function patchImagesForPath(path: string, version: number): number {
  if (typeof document === 'undefined') return 0
  const key = normalizePath(path)
  let patched = 0
  for (const img of Array.from(document.querySelectorAll('img'))) {
    if (mediaPathFromImg(img) !== key) continue
    const src = img.getAttribute('src')
    if (src) img.setAttribute('src', withVersionParam(src, version))
    const full = img.getAttribute('data-full-src')
    if (full) img.setAttribute('data-full-src', withVersionParam(full, version))
    patched++
  }
  return patched
}

/**
 * Record that a media file changed: bump its version and repaint every element
 * currently showing it. Returns true when the path was one we track.
 */
export function bumpMediaVersion(absOrRelPath: string): boolean {
  const rel = toWatchablePath(absOrRelPath)
  if (!rel) return false
  const next = (mediaVersions.value[rel] ?? 0) + 1
  mediaVersions.value = { ...mediaVersions.value, [rel]: next }
  const patched = patchImagesForPath(rel, next)
  appLog.d(TAG, `media changed: ${rel} (v${next}, ${patched} img patched)`)
  return true
}

// ─── Discovery: which media files are on screen ──────────────────────────────

/** Project-relative paths of every local image currently rendered. Sorted for
 *  a stable identity so the watcher only re-sends when the set really changes. */
export const mediaPaths: Ref<string[]> = ref([])

/** Paths discovered from the live DOM. Authoritative for DOM-rendered images. */
const domPaths = new Set<string>()
/** Refcounted paths registered explicitly by Vue components (e.g. the image
 *  viewer) — kept so a component whose element is not yet in the DOM still gets
 *  its file watched. */
const componentRefCounts = new Map<string, number>()

function recomputeMediaPaths(): void {
  const next = new Set(domPaths)
  for (const [path, count] of componentRefCounts) {
    if (count > 0) next.add(path)
  }
  const sorted = [...next].sort()
  // Cap: the backend enforces the same ceiling and drops the overflow, so
  // truncating here keeps the wire payload honest about what is actually
  // watched. Sorted order makes the truncation deterministic.
  const capped = sorted.length > MAX_MEDIA_WATCH_PATHS ? sorted.slice(0, MAX_MEDIA_WATCH_PATHS) : sorted
  const current = mediaPaths.value
  if (capped.length === current.length && capped.every((p, i) => p === current[i])) return
  mediaPaths.value = capped
}

/** Rescan the document for local images. Coalesced by `scheduleScan`. */
export function syncMediaPathsFromDom(): void {
  if (typeof document === 'undefined') return
  const next = new Set<string>()
  for (const img of Array.from(document.querySelectorAll('img'))) {
    const path = mediaPathFromImg(img)
    if (path) next.add(path)
  }
  domPaths.clear()
  for (const p of next) domPaths.add(p)
  recomputeMediaPaths()
}

/** Register a path for watching, independent of the DOM. Returns a release fn. */
export function trackMediaPath(path: string): () => void {
  const key = toWatchablePath(path)
  if (!key) return () => {}
  componentRefCounts.set(key, (componentRefCounts.get(key) ?? 0) + 1)
  recomputeMediaPaths()
  return () => {
    const n = componentRefCounts.get(key) ?? 0
    if (n <= 1) componentRefCounts.delete(key)
    else componentRefCounts.set(key, n - 1)
    recomputeMediaPaths()
  }
}

// ─── Mutation observer ───────────────────────────────────────────────────────

let observer: MutationObserver | null = null
let scanScheduled = false

function scheduleScan(): void {
  if (scanScheduled) return
  scanScheduled = true
  const run = () => {
    scanScheduled = false
    syncMediaPathsFromDom()
  }
  // rAF keeps the scan off the critical path of the mutation that triggered it;
  // a full <img> sweep is cheap but can fire many times during a stream.
  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(run)
  else setTimeout(run, 0)
}

/**
 * Start watching the document for image additions/removals. Idempotent — safe
 * to call from every mount. Called by the file watcher, which is mounted once
 * for the whole app.
 */
export function ensureMediaObserver(): void {
  if (observer || typeof document === 'undefined' || typeof MutationObserver === 'undefined') return
  observer = new MutationObserver((records) => {
    // Streaming rewrites text nodes constantly. A full <img> sweep on every
    // mutation would run thousands of times per turn, so bail out early unless
    // something actually touched an image (or added/removed nodes, which may
    // contain one).
    if (!records.some(mutationTouchesMedia)) return
    scheduleScan()
  })
  observer.observe(document.body, {
    childList: true,
    subtree: true,
    // Attribute changes matter because a Vue re-render may swap `src` in place
    // without adding or removing the element.
    attributes: true,
    attributeFilter: ['src', 'data-full-src', 'data-attach-src'],
  })
  syncMediaPathsFromDom()
}

/**
 * True when a mutation could have changed the set of rendered images: either
 * nodes were added/removed, or an image's src-bearing attribute changed.
 *
 * Exported for direct testing — the filter is a performance guard whose effect
 * is not otherwise observable (a needless scan still finds the same images).
 */
export function mutationTouchesMedia(m: MutationRecord): boolean {
  if (m.type === 'attributes') {
    const el = m.target as Element
    return el.tagName === 'IMG' || !!el.querySelector?.('img')
  }
  if (m.type !== 'childList') return false
  const relevant = (node: Node): boolean => {
    if (node.nodeType !== Node.ELEMENT_NODE) return false
    const el = node as Element
    return el.tagName === 'IMG' || !!el.querySelector?.('img')
  }
  for (const node of Array.from(m.addedNodes)) {
    if (relevant(node)) return true
  }
  for (const node of Array.from(m.removedNodes)) {
    if (relevant(node)) return true
  }
  return false
}

/**
 * Drop all discovered paths and recorded versions, keeping the observer
 * attached. Called on project switch: the version map is keyed by
 * project-relative paths, so a path like `assets/logo.png` in the new project
 * would otherwise inherit the old project's version (a harmless but pointless
 * cache-bust), and paths from the old project would keep being reported to the
 * watcher until the next scan.
 *
 * The DOM is rescanned immediately so the new project's images are picked up
 * without waiting for a mutation.
 */
export function clearMediaWatchState(): void {
  domPaths.clear()
  componentRefCounts.clear()
  mediaVersions.value = {}
  mediaPaths.value = []
  syncMediaPathsFromDom()
}

/** Tear the observer down and clear all state (tests / app teardown). */
export function resetMediaWatch(): void {
  observer?.disconnect()
  observer = null
  domPaths.clear()
  componentRefCounts.clear()
  mediaVersions.value = {}
  mediaPaths.value = []
}

/** Test-only: force a synchronous rescan. */
export function _syncMediaPathsForTesting(): void {
  syncMediaPathsFromDom()
}
