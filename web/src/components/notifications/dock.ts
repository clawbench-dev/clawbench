import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

/** Notification log runtime: the panel is its only dock resource. */
export const notificationsDock: DockPluginRuntime = {
  panel: {
    load: () => import('./NotificationsPanelContent.vue'),
  },
}
