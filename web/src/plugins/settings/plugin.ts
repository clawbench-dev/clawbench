import { Settings } from 'lucide-vue-next'
import type { DockPlugin } from '@/composables/dockPluginTypes'

/**
 * Settings: the panel is its only dock resource. Settings must stay available
 * even when everything else is hidden — it is the only way back to the plugin
 * manager.
 */
export default {
  id: 'settings',
  order: 140,
  titleKey: 'nav.settings',
  descriptionKey: 'settings.dockPlugins.desc.settings',
  group: 'system',
  closable: false,
  icon: Settings,
  panel: {
    load: () => import('@/components/settings/SettingsPage.vue'),
  },
} satisfies DockPlugin
