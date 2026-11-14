import { store } from '@/stores/app'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

/** Git history runtime: working-tree badge + the project history panel. */
export const historyDock: DockPluginRuntime = {
  badge: () => ({ count: store.state.gitWorkingTreeChangeCount }),
  panel: {
    load: () => import('./GitHistoryContent.vue'),
    props: () => ({ mode: 'project' }),
    on: (host) => ({ 'open-file': host.openFile }),
  },
}
