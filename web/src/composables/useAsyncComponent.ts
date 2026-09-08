import type { AsyncComponentOptions } from 'vue'
import AsyncComponentLoader from '@/components/common/AsyncComponentLoader.vue'
import AsyncComponentError from '@/components/common/AsyncComponentError.vue'
import { appLog } from '@/utils/appLog'

interface AsyncComponentConfig {
    loader: () => Promise<unknown>
    /** Retry delay between attempts (ms). */
    retryDelay?: number
    /** Max loader retries before showing the error component. */
    maxRetries?: number
    /**
     * Invoked when a stale-page chunk failure is detected (page from a
     * previous build referencing hashed chunks that no longer exist on the
     * server). Defaults to a full page reload; injectable for tests.
     */
    onStaleChunk?: () => void
}

const DEFAULT_RETRY_DELAY = 800
const DEFAULT_MAX_RETRIES = 3

/** Errors browsers report when a dynamically imported chunk cannot be fetched. */
const STALE_CHUNK_ERROR_RE = /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module/i

/** sessionStorage key + window for the auto-reload guard (prevents reload loops). */
const STALE_RELOAD_GUARD_KEY = 'asyncComponent:staleReloadAt'
const STALE_RELOAD_GUARD_WINDOW_MS = 60_000

function isStaleChunkError(error: unknown): boolean {
    return STALE_CHUNK_ERROR_RE.test(error instanceof Error ? error.message : String(error))
}

/**
 * Claims the one auto-reload allowed within the guard window.
 * Returns false when a reload already happened recently (loop guard).
 */
function claimStaleReload(): boolean {
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
 * Build options for Vue's defineAsyncComponent with bounded auto-retry and a
 * visible error fallback.
 *
 * Background: the component chunk is fetched via dynamic import() over the
 * network (or an SSH tunnel). A transient fetch failure ("Failed to fetch
 * dynamically imported module") makes the loader reject. Vue's
 * defineAsyncComponent caches the rejected pendingRequest permanently unless an
 * `onError` handler retries (which resets pendingRequest), so a one-shot failure
 * would otherwise leave the pane stuck until a full refresh. This wraps the
 * loader with a bounded retry and falls back to an error component (with a
 * manual retry button) instead of showing a permanent blank/loading state.
 *
 * Stale-page recovery: after a rebuild the server immediately drops the old
 * hashed chunks, so a page still running the previous build can never load a
 * lazily imported component — retrying is pointless. From the second
 * consecutive chunk-fetch failure on, one automatic page reload (rate limited
 * to once per minute) picks up the new build instead of dead-ending in the
 * error component.
 */
export function buildAsyncComponentOptions(config: AsyncComponentConfig): AsyncComponentOptions {
    const { loader, retryDelay = DEFAULT_RETRY_DELAY, maxRetries = DEFAULT_MAX_RETRIES } = config
    const onStaleChunk = config.onStaleChunk ?? (() => window.location.reload())

    return {
        loader,
        loadingComponent: AsyncComponentLoader,
        errorComponent: AsyncComponentError,
        onError: (error, retry, fail, attempts) => {
            if (attempts > 1 && isStaleChunkError(error) && claimStaleReload()) {
                appLog.w('AsyncComponent', 'stale chunk load failure, reloading page to pick up new build', error.message)
                onStaleChunk()
                return
            }
            appLog.w('AsyncComponent', 'async component load failed, retrying', error.message)
            if (attempts <= maxRetries) {
                setTimeout(retry, retryDelay)
            } else {
                appLog.e('AsyncComponent', 'async component load failed after retries', error)
                fail()
            }
        },
    }
}
