import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

/** Share center runtime: the panel is its only dock resource. */
export const shareDock: DockPluginRuntime = {
  panel: {
    load: () => import('./ShareCenterPanel.vue'),
  },
}
