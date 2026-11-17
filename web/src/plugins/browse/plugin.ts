import { FolderOpen, FolderTree } from 'lucide-vue-next'
import { store } from '@/stores/app'
import type { DockPlugin } from '@/composables/dockPluginTypes'

/**
 * File manager. The dock's `browse` tab used to be a hard-coded
 * `if (tab === 'browse')` inside `switchTab`; the "load on open" side effect
 * lives here now.
 *
 * The panel itself stays in App.vue: it needs the file manager's large prop
 * surface (entries, sort, selection, …) and is not a generic plugin panel.
 */
export default {
  id: 'browse',
  titleKey: 'nav.fileManager',
  descriptionKey: 'settings.dockPlugins.desc.browse',
  group: 'workspace',
  closable: false,
  settingsCategory: 'files',
  icon: FolderOpen,
  settingsPage: {
    group: 'dockModules',
    icon: FolderTree,
    kind: 'flat',
  },
  onEnter: () => {
    void store.loadFiles(store.state.currentDir, false, 0, true)
  },
} satisfies DockPlugin
