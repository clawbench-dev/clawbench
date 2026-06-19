/**
 * Global back navigation handler for drill-down pages.
 *
 * When the user swipes from the right edge (or presses Android back),
 * each feature can register a "can go back" check and a "go back" action.
 *
 * Handlers are sorted by explicit **priority** (higher = checked first).
 * This makes the dispatch order deterministic regardless of component
 * mount timing, which was the root cause of the file-overlay vs browse
 * back-handler bug.
 *
 * Priority tiers:
 *   1000  — overlays / modals that must intercept back before anything else
 *   100   — normal drill-down pages (settings, git-history, tasks, browse)
 *
 * If no feature handles back, the system back gesture proceeds normally
 * (which on Android would exit the app).
 */

export type BackHandler = {
    /** Unique ID for the handler (e.g., 'file-overlay', 'browse', 'settings') */
    id: string
    /** Returns true if this feature can navigate back */
    canGoBack: () => boolean
    /** Perform the back navigation */
    goBack: () => void
    /**
     * Priority — higher value = checked first.
     * Use the `PRIORITY_*` constants to pick a tier.
     */
    priority: number
}

/** Overlay-level handlers (file overlay, search drawer, etc.) */
export const PRIORITY_OVERLAY = 1000

/** Normal drill-down page handlers (settings, git-history, tasks, browse) */
export const PRIORITY_PAGE = 100

const handlers: BackHandler[] = []

/** @internal Reset all handlers — for tests only */
export function _resetHandlers() {
    handlers.length = 0
}

/**
 * Register a back navigation handler for a feature.
 * Returns an unregister function.
 */
export function registerBackHandler(handler: BackHandler): () => void {
    handlers.push(handler)
    return () => {
        const idx = handlers.indexOf(handler)
        if (idx !== -1) handlers.splice(idx, 1)
    }
}

/**
 * Attempt to handle a back navigation event.
 * Returns true if a handler consumed the event (navigated back).
 *
 * Iterates handlers by priority (highest first). Among handlers with
 * the same priority, the most recently registered one wins.
 */
export function handleBackNavigation(): boolean {
    const sorted = handlers
        .map((h, idx) => ({ h, idx }))
        .sort((a, b) => b.h.priority - a.h.priority || b.idx - a.idx)

    for (const { h } of sorted) {
        if (h.canGoBack()) {
            h.goBack()
            return true
        }
    }
    return false
}

/**
 * Check if any registered handler can navigate back.
 */
export function canNavigateBack(): boolean {
    return handlers.some(h => h.canGoBack())
}

// --- Exit confirmation (double-back-to-exit) ---

const EXIT_CONFIRM_TIMEOUT = 2000 // ms — second back must be within this window
let lastExitRequestTime = 0

/**
 * Request exit confirmation. Returns true if this is the second request
 * within the timeout window (meaning the user wants to actually exit).
 *
 * On the first call, returns false (user should see a "swipe again to exit" tip).
 * On the second call within EXIT_CONFIRM_TIMEOUT ms, returns true.
 */
export function requestExitConfirm(): boolean {
    const now = Date.now()
    if (now - lastExitRequestTime < EXIT_CONFIRM_TIMEOUT) {
        lastExitRequestTime = 0
        return true // confirmed — proceed to exit
    }
    lastExitRequestTime = now
    return false // first press — show tip, don't exit
}

/** @internal Reset exit state — for tests only */
export function _resetExitConfirm() {
    lastExitRequestTime = 0
}
