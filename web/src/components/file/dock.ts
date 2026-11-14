import { store } from '@/stores/app'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

/**
 * File manager runtime. The dock's `browse` tab used to be a hard-coded
 * `if (tab === 'browse')` inside `switchTab`; it lives here now.
 */
export const browseDock: DockPluginRuntime = {
  onEnter: () => {
    void store.loadFiles(store.state.currentDir, false, 0, true)
  },
}
