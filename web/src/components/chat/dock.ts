import { store } from '@/stores/app'
import { loadSessionsOnce } from '@/composables/useChatSession'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

/**
 * Chat is special: it lives in the right pane, not in the left-dock manifest.
 * It still uses the same runtime contract for its badge and its "load sessions
 * when opened" side effect, so the host has one place to look.
 */
export const chatDock: DockPluginRuntime = {
  badge: () => ({ count: store.state.chatUnreadCount }),
  onEnter: () => {
    void loadSessionsOnce()
  },
}
