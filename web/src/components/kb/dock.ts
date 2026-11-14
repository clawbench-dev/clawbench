import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

/** Knowledge base runtime: the panel is its only dock resource. */
export const kbDock: DockPluginRuntime = {
  panel: {
    load: () => import('./KnowledgePanel.vue'),
  },
}
