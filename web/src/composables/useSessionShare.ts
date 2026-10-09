/**
 * Module-level singleton share state for conversation share links.
 *
 * Mirrors useFileShare, but keyed by SESSION id rather than file path. The
 * session list needs to show a "shared" badge on rows that already have a public
 * link, and that state changes from a different component (SessionShareDialog),
 * so it lives in a module-level reactive Set that any component can read
 * (isSessionShared) or mutate (markShared / markUnshared) without prop drilling.
 *
 * The authoritative list is fetched once by SharedSessionsDrawer, which seeds
 * the whole Set in a single request. There is deliberately no per-session
 * refresh helper: unlike a file path (resolved on demand when a preview opens),
 * a session row has no single-session status endpoint, so one list call is the
 * only way to learn the state — asking per row would be N requests.
 */

import { reactive } from 'vue'

// reactive(new Set()) so add/delete mutations trigger Vue reactivity in computed
// properties (isSessionShared) across components.
const sharedSessionIds = reactive(new Set<string>())

function markShared(sessionId: string): void {
    if (sessionId) sharedSessionIds.add(sessionId)
}

function markUnshared(sessionId: string): void {
    if (sessionId) sharedSessionIds.delete(sessionId)
}

function isSessionShared(sessionId: string): boolean {
    return !!sessionId && sharedSessionIds.has(sessionId)
}

/** Replace the whole set (used after loading the authoritative list). */
function setSharedSessionIds(ids: string[]): void {
    sharedSessionIds.clear()
    for (const id of ids) {
        if (id) sharedSessionIds.add(id)
    }
}

/** Clear all cached share state (used when the app project changes). */
function resetSessionShareState(): void {
    sharedSessionIds.clear()
}

export function useSessionShare() {
    return {
        markShared,
        markUnshared,
        isSessionShared,
        setSharedSessionIds,
        resetSessionShareState,
    }
}
