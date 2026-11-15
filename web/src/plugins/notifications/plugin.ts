import { Inbox } from 'lucide-vue-next'
import type { DockPlugin } from '@/composables/dockPluginTypes'

/**
 * Notification log: the panel is its only dock resource.
 *
 * Inbox, not Bell: this tab is a browsable, append-only log of what happened,
 * not an alert channel. A bell would read as "something needs your attention"
 * and imply the unread state the log deliberately does not have.
 *
 * It is closable but carries NO `feature`: hiding it only removes the entry.
 * The log keeps being appended (notifications are push + an append-only record;
 * the dock tab is just one way to read it), so this switch must not touch the
 * server.
 */
export default {
  id: 'notifications',
  titleKey: 'nav.notifications',
  descriptionKey: 'settings.dockPlugins.desc.notifications',
  group: 'system',
  closable: true,
  settingsCategory: 'notifications',
  icon: Inbox,
  // Its settings page is a whole custom panel (retention + clear), not a field
  // list — so the plugin contributes the component and the settings category
  // dispatcher looks it up in the registry instead of a hand-written if-chain.
  settingsPage: {
    group: 'dockModules',
    icon: Inbox,
    kind: 'custom',
    component: () => import('@/components/settings/NotificationsSetting.vue'),
  },
  panel: {
    load: () => import('@/components/notifications/NotificationsPanelContent.vue'),
  },
} satisfies DockPlugin
