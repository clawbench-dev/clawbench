import { GitBranch, History } from 'lucide-vue-next'
import { store } from '@/stores/app'
import type { DockPlugin } from '@/composables/dockPluginTypes'
import { category as settingsCategory } from './settings'

/** Git history: working-tree badge + the project history panel. */
export default {
  id: 'history',
  order: 30,
  titleKey: 'git.history.projectHistory',
  descriptionKey: 'settings.dockPlugins.desc.history',
  group: 'workspace',
  closable: true,
  settingsCategory,
  icon: GitBranch,
  settingsPage: {
    group: 'dockModules',
    icon: History,
    kind: 'flat',
  },
  badge: () => ({ count: store.state.gitWorkingTreeChangeCount }),
  panel: {
    load: () => import('@/components/git/GitHistoryContent.vue'),
    props: () => ({ mode: 'project' }),
    on: (host) => ({ 'open-file': host.openFile }),
  },
} satisfies DockPlugin
