import { appLog, flushLogNow } from './appLog'

/**
 * Recovery from a Vue component whose update keeps throwing.
 *
 * Why this exists
 * ---------------
 * Vue runs component updates through a queue. The update function is wrapped in
 * `callWithErrorHandling`, so a throw is reported to `app.config.errorHandler`
 * and the queue moves on — but the component's `subTree` was already replaced
 * with the new (half-patched) tree before the throw. From then on every re-render
 * of that component patches a tree containing child vnodes that were never
 * mounted, and Vue's own update bookkeeping throws again (typically
 * `Cannot read properties of null (reading 'emitsOptions')` in
 * `shouldUpdateComponent`). The subtree can never recover by itself: each
 * interaction produces the same error and the visible UI never changes, which
 * the user experiences as "the whole panel is dead, every tab click does
 * nothing".
 *
 * There is no way to repair a half-patched tree from outside, so the only real
 * recovery is reloading the page — the thing the user ends up doing by hand.
 * This module decides when that is warranted: a single render error is normal
 * (and already handled by whatever component threw), but the SAME error
 * recurring means the app is stuck, so reload once instead of leaving the user
 * with a dead UI.
 *
 * Guards: a reload budget per tab session plus a cool-down, so a page that
 * crashes deterministically on load cannot reload itself in a loop.
 */

const TAG = 'RenderCrash'

/** Same (component, message) this many times inside the window = stuck. */
const REPEAT_THRESHOLD = 2
const REPEAT_WINDOW_MS = 10_000
/** Minimum gap between two automatic reloads. */
const RELOAD_COOLDOWN_MS = 60_000
/** Hard cap per tab session, even if the cool-down has passed. */
const MAX_RELOADS = 2
/** Give the log flush a beat to leave before the page unloads. */
const RELOAD_DELAY_MS = 200

/**
 * Messages that can only come from Vue's own patcher walking a tree it no
 * longer understands (a child vnode that was never mounted, so `component` is
 * null). There is nothing to wait for: the subtree is already corrupt and the
 * very next render of that ancestor throws again — reload on the first one.
 */
const CORRUPTION_RE = /emitsOptions|Cannot read properties of null/

const RELOAD_COUNT_KEY = 'renderCrash:reloads'
const RELOAD_AT_KEY = 'renderCrash:reloadAt'

export interface RenderCrashOptions {
    /** Performs the reload. Injectable for tests. */
    reload?: () => void
    /** Clock. Injectable for tests. */
    now?: () => number
    /** 0 = reload synchronously (tests); production waits for the log flush. */
    reloadDelayMs?: number
    storage?: Pick<Storage, 'getItem' | 'setItem'>
}

export interface RenderCrashRecovery {
    /**
     * Record one `component update` error. Returns true when this call started
     * a reload.
     */
    note: (message: string, componentName: string) => boolean
    /** Reset the in-memory repeat tracker (tests / HMR). */
    reset: () => void
}

function safeStorage(): Pick<Storage, 'getItem' | 'setItem'> | null {
    try {
        if (typeof sessionStorage === 'undefined') return null
        // Touch it: some privacy modes throw on access rather than returning null.
        sessionStorage.getItem(RELOAD_COUNT_KEY)
        return sessionStorage
    } catch {
        return null
    }
}

export function createRenderCrashRecovery(options: RenderCrashOptions = {}): RenderCrashRecovery {
    const reload = options.reload ?? (() => window.location.reload())
    const now = options.now ?? (() => Date.now())
    const reloadDelayMs = options.reloadDelayMs ?? RELOAD_DELAY_MS
    const storage = options.storage ?? safeStorage()

    let lastKey = ''
    let firstSeenAt = 0
    let count = 0

    const readNumber = (key: string): number => {
        try {
            return Number(storage?.getItem(key) || 0) || 0
        } catch {
            return 0
        }
    }
    const writeNumber = (key: string, value: number): void => {
        try {
            storage?.setItem(key, String(value))
        } catch {
            /* storage full / disabled — the in-memory tracker still applies */
        }
    }

    const reset = (): void => {
        lastKey = ''
        firstSeenAt = 0
        count = 0
    }

    const note = (message: string, componentName: string): boolean => {
        const at = now()
        const key = `${componentName}\u0000${message}`
        const corrupt = CORRUPTION_RE.test(message)

        if (key !== lastKey || at - firstSeenAt > REPEAT_WINDOW_MS) {
            lastKey = key
            firstSeenAt = at
            count = 1
            if (corrupt) {
                // The tree is already unusable; the repeat would only come from
                // the user's next click. Reload now.
                count = REPEAT_THRESHOLD
            } else {
                appLog.w(
                    TAG,
                    `component update failed once in <${componentName || '?'}> — watching for a repeat: ${message}`,
                )
                return false
            }
        } else {
            count += 1
        }

        if (count < REPEAT_THRESHOLD) return false

        const reloads = readNumber(RELOAD_COUNT_KEY)
        if (reloads >= MAX_RELOADS) {
            appLog.e(
                TAG,
                `component update keeps failing in <${componentName || '?'}> but ${reloads} auto-reloads already used — reload by hand: ${message}`,
            )
            return false
        }
        const lastReloadAt = readNumber(RELOAD_AT_KEY)
        if (lastReloadAt && at - lastReloadAt < RELOAD_COOLDOWN_MS) {
            appLog.w(
                TAG,
                `component update keeps failing in <${componentName || '?'}> but a reload just happened — waiting for the cool-down: ${message}`,
            )
            return false
        }

        writeNumber(RELOAD_COUNT_KEY, reloads + 1)
        writeNumber(RELOAD_AT_KEY, at)
        appLog.e(
            TAG,
            `component update failed ${count}x in <${componentName || '?'}> — the UI cannot repaint, reloading: ${message}`,
        )
        reset()
        // Let the buffered log (which names the component) reach the server
        // before the page goes away; the relay's fetch is `keepalive`.
        flushLogNow()
        if (reloadDelayMs > 0) setTimeout(reload, reloadDelayMs)
        else reload()
        return true
    }

    return { note, reset }
}

/** The app-wide instance used by `main.ts`. */
export const renderCrashRecovery = createRenderCrashRecovery()
