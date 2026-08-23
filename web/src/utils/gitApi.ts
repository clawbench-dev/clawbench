// Git API request helper.
//
// All git endpoints (project-history, working-tree, history, commit-files,
// file-diff, diff, verify-commits, load-more, search pagination) go through
// this wrapper instead of bare fetch. Bare fetch has no timeout and no abort
// support: if a request hangs (WebView network stall, server git command
// wedged), the loading state in GitHistoryContent/GitHistoryDrawer can never
// be reset because the awaiting promise never settles.
//
// gitFetch mirrors the pattern in @/utils/api (API_TIMEOUT_MS + abort signal)
// but returns a raw Response so callers keep their existing response handling.
import i18n from '@/i18n'

function localeHeaders(): Record<string, string> {
    return { 'X-Locale': i18n.global.locale.value as string }
}

// Default timeout for git requests (10 seconds, same as @/utils/api).
export const GIT_TIMEOUT_MS = 10_000

/** Error thrown when a git request times out. */
export class GitTimeoutError extends Error {
    constructor(url: string) {
        super(`git request timed out: ${url}`)
        this.name = 'GitTimeoutError'
    }
}

export interface GitFetchOptions {
    /** External signal — aborts the request when it fires. */
    signal?: AbortSignal
    /** Override the default timeout (ms). Defaults to GIT_TIMEOUT_MS. */
    timeoutMs?: number
}

/**
 * fetch with a built-in timeout and optional external abort signal.
 *
 * - Times out after `timeoutMs` (default GIT_TIMEOUT_MS) → rejects with
 *   GitTimeoutError.
 * - Aborts immediately when the external signal is already aborted, and
 *   aborts on subsequent external aborts.
 * - The returned promise settles in every case (resolve or reject), so
 *   callers' finally blocks always run.
 */
export function gitFetch(url: string, opts: GitFetchOptions = {}): Promise<Response> {
    const controller = new AbortController()
    let timedOut = false
    const timer = setTimeout(() => {
        timedOut = true
        controller.abort()
    }, opts.timeoutMs ?? GIT_TIMEOUT_MS)

    // If the external signal is already aborted, abort immediately.
    if (opts.signal?.aborted) {
        clearTimeout(timer)
        controller.abort()
    }

    // Forward external aborts to our controller.
    const onExternalAbort = () => controller.abort()
    opts.signal?.addEventListener('abort', onExternalAbort)

    return fetch(url, { headers: localeHeaders(), signal: controller.signal }).catch((err: unknown) => {
        // Distinguish error sources by the timedOut flag, not by inspecting
        // opts.signal.aborted after the fact. That post-hoc check has a race:
        // if the internal timeout fired and the external signal happens to
        // abort in the same tick, a timeout would be misreported as an
        // AbortError (and silently swallowed by callers).
        if (timedOut) throw new GitTimeoutError(url)
        // Otherwise surface the original error: an AbortError when the
        // external signal aborted, or the underlying network error
        // (e.g. TypeError "Failed to fetch") when the request failed.
        throw err
    }).finally(() => {
        clearTimeout(timer)
        opts.signal?.removeEventListener('abort', onExternalAbort)
    })
}

/**
 * Sequence guard for overlapping loads.
 *
 * Git history can load from multiple entry points (manual refresh, tab
 * re-activation, load-more, search). A slow first request can otherwise
 * resolve after a faster second one and overwrite fresh data — or reset the
 * loading flag while a newer request is still in flight. Each caller obtains
 * a token; `isCurrent(token)` is only true for the most recent token issued
 * by THIS guard, so stale responses can be discarded safely. Tokens are
 * opaque object references compared by identity, so tokens from different
 * guards can never collide.
 */
export interface SeqToken { __seqToken: true }
export function createSeqGuard(): { token: () => SeqToken; isCurrent: (t: SeqToken) => boolean } {
    let current: SeqToken | null = null
    return {
        token: () => {
            current = { __seqToken: true }
            return current
        },
        isCurrent: (t: SeqToken) => t === current,
    }
}
