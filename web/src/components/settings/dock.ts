import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

/**
 * Settings runtime: the panel is its only dock resource. Settings must stay
 * available even when everything else is hidden — it is the only way back to
 * the plugin manager.
 */
export const settingsDock: DockPluginRuntime = {
  panel: {
    load: () => import('./SettingsPage.vue'),
  },
}
