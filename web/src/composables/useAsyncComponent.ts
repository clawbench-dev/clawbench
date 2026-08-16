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
 */
export function buildAsyncComponentOptions(config: AsyncComponentConfig): AsyncComponentOptions {
    const { loader, retryDelay = DEFAULT_RETRY_DELAY, maxRetries = DEFAULT_MAX_RETRIES } = config

    return {
        loader,
        loadingComponent: AsyncComponentLoader,
        errorComponent: AsyncComponentError,
        onError: (error, retry, fail, attempts) => {
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
