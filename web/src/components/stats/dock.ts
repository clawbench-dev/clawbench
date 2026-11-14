import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

/** Usage statistics runtime: the panel is its only dock resource. */
export const statsDock: DockPluginRuntime = {
  panel: {
    load: () => import('./StatsTabHost.vue'),
  },
}
