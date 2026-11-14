import { useMailUnread, startMailUnreadPolling, stopMailUnreadPolling } from '@/composables/useMailUnread'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

const { total } = useMailUnread()
const { serverConfig } = useSettingsConfig()

/**
 * Mail runtime. The unread poller is the only background resource in the dock:
 * it exists solely to feed this badge, so the host starts it only while the
 * entry is visible and the server feature is on.
 */
export const mailDock: DockPluginRuntime = {
  badge: () => ({ count: total.value }),
  available: () => (serverConfig.value?.mail as Record<string, unknown> | undefined)?.enabled !== false,
  unavailableReasonKey: 'settings.dockPlugins.unavailable.mail',
  panel: {
    load: () => import('./MailPanel.vue'),
    on: (host) => ({ 'open-settings': host.openMailSettings }),
  },
  startBackground: () => {
    startMailUnreadPolling()
    return stopMailUnreadPolling
  },
}
