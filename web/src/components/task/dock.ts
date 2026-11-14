import { store } from '@/stores/app'
import { useTaskTab } from '@/composables/useTaskTab'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

const { loadTasks } = useTaskTab()

/** Tasks runtime: unread badge + the "load on open" side effect + panel. */
export const tasksDock: DockPluginRuntime = {
  badge: () => ({ count: store.state.taskUnreadCount }),
  onEnter: () => {
    void loadTasks()
  },
  panel: {
    load: () => import('./TaskTab.vue'),
    on: (host) => ({ 'open-file': host.openTaskFile }),
  },
}
