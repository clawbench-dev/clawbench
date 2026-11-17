import { BookOpen } from 'lucide-vue-next'
import type { DockPlugin } from '@/composables/dockPluginTypes'

/**
 * Knowledge base: multiple collections (global or per project), each a set of
 * files indexed for keyword + semantic search. It sits next to 邮件 because
 * both are reference material rather than project state.
 */
export default {
  id: 'kb',
  titleKey: 'nav.knowledgeBase',
  descriptionKey: 'settings.dockPlugins.desc.kb',
  group: 'personal',
  closable: true,
  feature: 'kb.enabled',
  settingsCategory: 'kb',
  icon: BookOpen,
  settingsPage: {
    group: 'dockModules',
    icon: BookOpen,
    kind: 'flat',
  },
  panel: {
    load: () => import('@/components/kb/KnowledgePanel.vue'),
  },
} satisfies DockPlugin
