import { Clock, ListTodo } from 'lucide-vue-next'
import { store } from '@/stores/app'
import { useTaskTab } from '@/composables/useTaskTab'
import type { DockPlugin } from '@/composables/dockPluginTypes'

const { loadTasks } = useTaskTab()

/** Tasks: unread badge + the "load on open" side effect + panel. */
export default {
  id: 'tasks',
  titleKey: 'nav.tasks',
  descriptionKey: 'settings.dockPlugins.desc.tasks',
  group: 'project',
  closable: true,
  feature: 'tasks.enabled',
  settingsCategory: 'tasks',
  icon: Clock,
  settingsPage: {
    group: 'dockModules',
    icon: ListTodo,
    kind: 'flat',
  },
  badge: () => ({ count: store.state.taskUnreadCount }),
  // A task list has three independent states worth showing on the tab: unread
  // results, work in progress, and a just-finished run worth a flash.
  appearance: {
    unread: () => store.state.taskUnreadCount > 0,
    running: () => store.state.taskRunning,
    justCompleted: () => store.state.taskJustCompleted,
  },
  onEnter: () => {
    void loadTasks()
  },
  panel: {
    load: () => import('@/components/task/TaskTab.vue'),
    on: (_host, extras) => ({ 'open-file': extras.task.openTaskFile }),
  },
} satisfies DockPlugin
