import { defineComponent, h, type AsyncComponentOptions } from 'vue'
import AsyncComponentLoader from '@/components/common/AsyncComponentLoader.vue'
import AsyncComponentError from '@/components/common/AsyncComponentError.vue'
import { appLog } from '@/utils/appLog'
import { claimChunkReload, isStaleChunkError, reloadForStaleChunk } from '@/utils/chunkLoadRecovery'

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
    /**
     * Invoked when the user clicks Retry on the error component after the
     * loader gave up. Defaults to a full page reload; injectable for tests.
     */
    onRetry?: () => void
}

const DEFAULT_RETRY_DELAY = 800
const DEFAULT_MAX_RETRIES = 3

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
 *
 * The error component's Retry button must actually retry: Vue renders
 * `errorComponent` with only an `error` prop, so a bare `AsyncComponentError`
 * emits `retry` into the void and the pane is stuck until the user restarts
 * the app. The component is therefore wrapped in a shim that maps that click
 * to a page reload — the loader's `retry()` closure is not reachable from
 * here, and by this point the chunk has already failed `maxRetries` times.
 */
export function buildAsyncComponentOptions(config: AsyncComponentConfig): AsyncComponentOptions {
    const { loader, retryDelay = DEFAULT_RETRY_DELAY, maxRetries = DEFAULT_MAX_RETRIES } = config
    const onRetry = config.onRetry ?? (() => window.location.reload())

    // A caller-supplied hook replaces the reload but still respects the shared
    // loop guard (tests use it to observe the decision without navigating).
    const recoverStaleChunk = (message: string) => {
        if (config.onStaleChunk) {
            if (!claimChunkReload()) return
            appLog.w('AsyncComponent', 'stale chunk load failure, recovering', message)
            config.onStaleChunk()
            return
        }
        reloadForStaleChunk(message)
    }

    const ErrorWithRetry = defineComponent({
        name: 'AsyncComponentErrorWithRetry',
        inheritAttrs: false,
        setup(_, { attrs }) {
            return () => h(AsyncComponentError, { ...attrs, onRetry })
        },
    })

    return {
        loader,
        loadingComponent: AsyncComponentLoader,
        errorComponent: ErrorWithRetry,
        onError: (error, retry, fail, attempts) => {
            if (attempts > 1 && isStaleChunkError(error)) {
                recoverStaleChunk(error.message)
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
