import { FileText, Eye } from 'lucide-vue-next'
import type { DockPlugin } from '@/composables/dockPluginTypes'

/**
 * File preview. Like the file manager, its panel is rendered directly by
 * App.vue (it owns the viewer's prop surface), so this plugin declares only its
 * manifest entry.
 */
export default {
  id: 'view',
  titleKey: 'nav.fileView',
  descriptionKey: 'settings.dockPlugins.desc.view',
  group: 'workspace',
  closable: false,
  settingsCategory: 'fileView',
  icon: FileText,
  settingsPage: {
    group: 'dockModules',
    icon: Eye,
    kind: 'flat',
  },
} satisfies DockPlugin
