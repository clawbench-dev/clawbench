import { Github, Gitlab } from 'lucide-vue-next'
import i18n from '@/i18n'
import { forgeDockIconKind, forgeTabTitleKey, useForgeBinding } from '@/composables/useForgeBinding'
import { useForgeUnread } from '@/composables/useForgeUnread'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

const { forgeUnreadCount } = useForgeUnread()
const { platform } = useForgeBinding()

/**
 * Forge is the one plugin whose icon and title depend on runtime state: the
 * tab names and draws the integration the project is actually bound to.
 */
export const forgeDock: DockPluginRuntime = {
  icon: () => (forgeDockIconKind(platform.value) === 'gitlab' ? Gitlab : Github),
  title: () => i18n.global.t(forgeTabTitleKey(platform.value)),
  badge: () => ({ count: forgeUnreadCount.value }),
  // Resolved per call, not at module load: reading the config module while
  // this file is being imported touches it before the test/host mock is ready
  // (TDZ in SettingsCategory's mock). A composable call is cheap and returns
  // the same shared refs.
  available: () => {
    const { serverConfig } = useSettingsConfig()
    return (serverConfig.value?.forge as Record<string, unknown> | undefined)?.enabled !== false
  },
  unavailableReasonKey: 'settings.dockPlugins.unavailable.forge',
  panel: {
    load: () => import('./ForgePanelContent.vue'),
    props: (host) => ({ projectPath: host.projectPath }),
    on: (host) => ({ 'request-project': host.requestProject }),
  },
}
