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

// Race guard: while markRead is in flight, a refetch triggered by a WS event
// must not restore the pre-clear count. Mirrors the same guard in useTaskTab.
let markingReadInProgress = false

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
        try {
            const res = await fetchForgeUnread()
            // Don't let a stale in-flight response undo an optimistic clear.
            if (markingReadInProgress) return
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

    async function markRead() {
        if (forgeUnreadCount.value === 0) return
        // Optimistic: clear immediately, then persist.
        forgeUnreadCount.value = 0
        markingReadInProgress = true
        try {
            // No itemKey: mark the whole bound repository read.
            const res = await markForgeRead()
            // Trust the server's remainder rather than assuming zero — it also
            // covers activity that arrived while the request was in flight.
            forgeUnreadCount.value = res.count ?? 0
        } catch (err) {
            appLog.w(TAG, 'markRead failed', err)
            void refresh()
        } finally {
            markingReadInProgress = false
        }
    }

    return { forgeUnreadCount, refresh, onForgeEvent, markRead }
}
