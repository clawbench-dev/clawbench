import { Github, Gitlab } from 'lucide-vue-next'
import i18n from '@/i18n'
import { forgeDockIconKind, forgeTabTitleKey, useForgeBinding } from '@/composables/useForgeBinding'
import { useForgeUnread } from '@/composables/useForgeUnread'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

const { forgeUnreadCount } = useForgeUnread()
const { platform } = useForgeBinding()
const { serverConfig } = useSettingsConfig()

/**
 * Forge is the one plugin whose icon and title depend on runtime state: the
 * tab names and draws the integration the project is actually bound to.
 */
export const forgeDock: DockPluginRuntime = {
  icon: () => (forgeDockIconKind(platform.value) === 'gitlab' ? Gitlab : Github),
  title: () => i18n.global.t(forgeTabTitleKey(platform.value)),
  badge: () => ({ count: forgeUnreadCount.value }),
  available: () => (serverConfig.value?.forge as Record<string, unknown> | undefined)?.enabled !== false,
  unavailableReasonKey: 'settings.dockPlugins.unavailable.forge',
  panel: {
    load: () => import('./ForgePanelContent.vue'),
    props: (host) => ({ projectPath: host.projectPath }),
    on: (host) => ({ 'request-project': host.requestProject }),
  },
}
