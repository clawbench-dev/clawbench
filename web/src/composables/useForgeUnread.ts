import { ref } from 'vue'
import { fetchForgeUnread, markForgeRead } from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'

const TAG = 'UseForgeUnread'

// Module-level singleton: the unread count is a single global number shown on
// the dock badge, so every consumer must see the same value.
const forgeUnreadCount = ref(0)

/**
 * useForgeUnread tracks the unread forge-event count.
 *
 * The count is server-authoritative and independent of the notification
 * toggles: it answers "are there new changes", not "did we notify". Opening the
 * Issues & PRs tab clears it.
 */
export function useForgeUnread() {
    async function refresh() {
        try {
            const res = await fetchForgeUnread()
            forgeUnreadCount.value = res.count ?? 0
        } catch (err) {
            // A failed poll must not clear the badge or surface an error; the
            // count simply stays at its last known value.
            appLog.w(TAG, 'refresh failed', err)
        }
    }

    /** Bump locally when a live forge_event arrives over WS. */
    function bump() {
        forgeUnreadCount.value += 1
    }

    async function markRead() {
        if (forgeUnreadCount.value === 0) return
        // Optimistic: clear immediately, then persist.
        forgeUnreadCount.value = 0
        try {
            await markForgeRead()
        } catch (err) {
            appLog.w(TAG, 'markRead failed', err)
            void refresh()
        }
    }

    return { forgeUnreadCount, refresh, bump, markRead }
}
