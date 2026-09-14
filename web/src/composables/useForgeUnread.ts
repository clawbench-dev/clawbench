import { ref } from 'vue'
import { fetchForgeUnread, markForgeRead } from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'

const TAG = 'UseForgeUnread'

// Module-level singleton: the unread count is a single global number shown on
// the dock badge, so every consumer must see the same value.
const forgeUnreadCount = ref(0)

// Debounce timer for onForgeEvent — coalesces a burst of WS events (a sync can
// derive many at once) into a single count fetch.
let refetchTimer: ReturnType<typeof setTimeout> | null = null

// Monotonic request sequence. Only the newest refresh may write the count.
//
// This replaces an earlier `markingReadInProgress` flag, which only the
// (now-unused) markRead() set — so on the mark-all path the flag was never
// armed and a GET issued before the POST committed could land afterwards and
// restore the pre-clear count, leaving the badge non-zero after an explicit
// "mark all read".
let refreshSeq = 0

// The in-flight mark-all, so two lists clearing at once issue ONE request and
// share its outcome.
let markAllInFlight: Promise<number> | null = null

/**
 * useForgeUnread tracks the unread forge count for the active project.
 *
 * The count is "how many ITEMS have new activity", scoped to the project's bound
 * repository, and is server-authoritative. That is what makes the badge match
 * what the user can see: the panel's rows carry the same per-item flag, so the
 * number always corresponds to rows they can find and clear.
 *
 * It is independent of the notification toggles: it answers "are there new
 * changes", not "did we notify".
 */
export function useForgeUnread() {
    async function refresh() {
        const seq = ++refreshSeq
        try {
            const res = await fetchForgeUnread()
            // Superseded by a newer request (or by a mark-all): a slower earlier
            // response must not clobber the newer value.
            if (seq !== refreshSeq) return
            forgeUnreadCount.value = res.count ?? 0
        } catch (err) {
            // A failed poll must not clear the badge or surface an error; the
            // count simply stays at its last known value.
            appLog.w(TAG, 'refresh failed', err)
        }
    }

    /**
     * Called when a live forge_event arrives over WS.
     *
     * Re-derives the authoritative count rather than incrementing locally: the
     * server count is the only value that stays correct after the user has
     * opened the tab (which clears the badge) or after a missed event. This is
     * the same event-driven pattern the task/terminal/proxy badges use.
     */
    function onForgeEvent() {
        if (refetchTimer) clearTimeout(refetchTimer)
        refetchTimer = setTimeout(() => {
            refetchTimer = null
            void refresh()
        }, 200)
    }

    /**
     * Mark the whole bound repository read (the "mark all read" action).
     *
     * Resolves with the remaining count. Concurrent callers share one request.
     * Rejects if the write failed, so callers can restore their optimistic UI.
     */
    async function markAllRead(): Promise<number> {
        if (markAllInFlight) return markAllInFlight
        // Nothing to clear: skip the write rather than sending a no-op request.
        if (forgeUnreadCount.value === 0) return 0
        // Invalidate any in-flight refresh: its response predates this write.
        refreshSeq++
        const before = forgeUnreadCount.value
        // Optimistic: clear immediately, then persist.
        forgeUnreadCount.value = 0

        markAllInFlight = (async () => {
            try {
                // No itemKey: mark the whole bound repository read.
                const res = await markForgeRead()
                // Trust the server's remainder rather than assuming zero — it
                // also covers activity that arrived while the request was in
                // flight.
                forgeUnreadCount.value = res.count ?? 0
                return res.count ?? 0
            } catch (err) {
                appLog.w(TAG, 'markAllRead failed', err)
                // Restore, so the badge does not claim everything was seen.
                forgeUnreadCount.value = before
                throw err
            } finally {
                markAllInFlight = null
            }
        })()
        return markAllInFlight
    }

    return { forgeUnreadCount, refresh, onForgeEvent, markAllRead }
}
