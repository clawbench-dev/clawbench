import { BarChart3, ChartColumn } from 'lucide-vue-next'
import type { DockPlugin } from '@/composables/dockPluginTypes'

/** Usage statistics: the panel is its only dock resource. */
export default {
  id: 'stats',
  titleKey: 'nav.stats',
  descriptionKey: 'settings.dockPlugins.desc.stats',
  group: 'project',
  closable: true,
  settingsCategory: 'stats',
  icon: BarChart3,
  settingsPage: {
    group: 'dockModules',
    icon: ChartColumn,
    kind: 'flat',
  },
  panel: {
    load: () => import('@/components/stats/StatsTabHost.vue'),
  },
} satisfies DockPlugin
