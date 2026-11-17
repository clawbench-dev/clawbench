import { MessageSquare } from 'lucide-vue-next'
import type { Ref } from 'vue'
import { store } from '@/stores/app'
import { loadSessionsOnce } from '@/composables/useChatSession'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import type { DockPlugin } from '@/composables/dockPluginTypes'

// Lazy: `useSessionIdentity()` builds a large API object on every call, and the
// appearance predicate runs on each render. Cache the one ref it needs.
let runningSessions: Ref<{ size: number }> | null = null
function hasRunningSession(): boolean {
  if (!runningSessions) runningSessions = useSessionIdentity().runningSessions as unknown as Ref<{ size: number }>
  return runningSessions.value.size > 0
}

/**
 * Chat is special: it lives in the right pane, not in the left-dock manifest.
 * It still uses the same runtime contract for its badge, its button appearance
 * and its "load sessions when opened" side effect, so the host has one place to
 * look.
 *
 * The narrow dock renders it with the same button component as every other tab,
 * which is why it declares an icon and a title here.
 */
export default {
  id: 'chat',
  titleKey: 'nav.chat',
  icon: MessageSquare,
  badge: () => ({ count: store.state.chatUnreadCount }),
  // Unread conversations and a running session both mark the chat entry, the
  // same way the tasks entry is marked.
  appearance: {
    unread: () => store.state.chatUnreadCount > 0,
    running: hasRunningSession,
  },
  onEnter: () => {
    void loadSessionsOnce()
  },
} satisfies DockPlugin
