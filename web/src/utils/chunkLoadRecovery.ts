import { appLog } from './appLog'

/**
 * Recovery from stale-build chunk failures, shared by every lazy-load path.
 *
 * A page served from a previous build keeps referencing content-hashed chunk
 * filenames. After a rebuild those files are gone, so every later dynamic
 * import 404s. Retrying cannot help — the hash is not coming back — and the
 * only fix is loading the new index.html.
 *
 * The two symptoms this prevents:
 *  - `defineAsyncComponent` without an `errorComponent` renders its
 *    `loadingComponent` forever after the loader rejects (Vue sets `error` but
 *    has nothing else to render), i.e. a pane that spins until the user
 *    restarts the app.
 *  - the same rejection surfaces as a console "Unable to preload CSS" error
 *    with no user-visible recovery.
 */

/** Errors browsers report when a dynamically imported chunk cannot be fetched. */
export const STALE_CHUNK_ERROR_RE =
  /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|Unable to preload CSS/i

export function isStaleChunkError(error: unknown): boolean {
  return STALE_CHUNK_ERROR_RE.test(error instanceof Error ? error.message : String(error))
}

/** sessionStorage key + window for the auto-reload guard (prevents reload loops). */
const STALE_RELOAD_GUARD_KEY = 'chunkLoad:staleReloadAt'
const STALE_RELOAD_GUARD_WINDOW_MS = 60_000

/**
 * Claims the one auto-reload allowed within the guard window.
 * Returns false when a reload already happened recently (loop guard).
 *
 * Shared by the per-component path and the global `vite:preloadError` hook so
 * a single stale build can only cost the user one reload.
 */
export function claimChunkReload(): boolean {
  try {
    const last = Number(sessionStorage.getItem(STALE_RELOAD_GUARD_KEY) || 0)
    if (Date.now() - last < STALE_RELOAD_GUARD_WINDOW_MS) return false
    sessionStorage.setItem(STALE_RELOAD_GUARD_KEY, String(Date.now()))
    return true
  } catch {
    // sessionStorage unavailable (privacy mode / disabled storage) —
    // allow the reload; the page state is broken either way.
    return true
  }
}

/**
 * Reload once to pick up the current build, or report that the guard blocked
 * it. Returns whether a reload was started.
 */
export function reloadForStaleChunk(reason: string): boolean {
  if (!claimChunkReload()) {
    appLog.w('ChunkRecovery', `stale chunk (${reason}) but a reload already happened recently — not looping`)
    return false
  }
  appLog.w('ChunkRecovery', `stale chunk (${reason}), reloading to pick up the current build`)
  window.location.reload()
  return true
}

/**
 * Install the document-level hook for Vite's preload failures.
 *
 * Vite dispatches a cancelable `vite:preloadError` event for every failed
 * chunk preload (JS and CSS). Listening here is the safety net for lazy
 * imports built with a bare `defineAsyncComponent` (no error handling of their
 * own) and for non-component dynamic imports: without it those failures are
 * invisible and the affected pane stays on its loading state forever.
 *
 * A SINGLE failure does not reload. A transient fetch failure is
 * indistinguishable from a stale build by message alone, and reloading on one
 * would throw away in-progress input for something that likely recovers by
 * itself. A stale build, by contrast, fails in a burst — every chunk the pane
 * references is gone, so one open produced nine 404s in ~20 ms (measured on a
 * real client log). Two distinct failures inside {@link BURST_WINDOW_MS} is
 * therefore the stale-build signal.
 *
 * The event is deliberately NOT prevented. Vite's `handlePreloadError` treats
 * `preventDefault` as "the caller handled it", and for the module import that
 * means `.catch(handlePreloadError)` resolves to `undefined` — Vue then warns
 * "Async component loader resolved to undefined" and renders its loading
 * component forever, which is the very symptom this hook exists to remove.
 */
const BURST_WINDOW_MS = 10_000
let firstFailureAt = 0

/** Reset the burst detector. For testing only. */
export function _resetChunkLoadRecovery(): void {
  firstFailureAt = 0
}

export function installChunkLoadRecovery(): void {
  window.addEventListener('vite:preloadError', (event) => {
    const err = (event as Event & { payload?: unknown }).payload
    const message = err instanceof Error ? err.message : String(err ?? 'unknown preload failure')
    const now = Date.now()
    const inBurst = firstFailureAt !== 0 && now - firstFailureAt <= BURST_WINDOW_MS
    if (!inBurst) {
      firstFailureAt = now
      appLog.w('ChunkRecovery', `chunk preload failed (${message}); not reloading on a single failure`)
      return
    }
    firstFailureAt = 0
    reloadForStaleChunk(message)
  })
}
