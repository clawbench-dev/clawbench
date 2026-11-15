import { Network, ArrowLeftRight } from 'lucide-vue-next'
import { store } from '@/stores/app'
import { usePortForward } from '@/composables/usePortForward'
import { getNative } from '@/utils/clawbenchNative'
import { portForwardUnavailable } from '@/utils/portForwardUtils'
import type { DockPlugin } from '@/composables/dockPluginTypes'

const { sshInfo, transportAllowsH2 } = usePortForward()

/**
 * Port mapping is unusable only when NEITHER wire can carry it: the SSH
 * listener is off AND the configured transport cannot use h2. Keying off SSH
 * alone hid the tab on installs that forward over the stream tunnel.
 */
export default {
  id: 'proxy',
  titleKey: 'nav.portForward',
  descriptionKey: 'settings.dockPlugins.desc.proxy',
  group: 'project',
  closable: true,
  capability: 'portForward',
  settingsCategory: 'portForward',
  icon: Network,
  settingsPage: {
    group: 'dockModules',
    icon: ArrowLeftRight,
    kind: 'group',
  },
  badge: () => ({ count: store.state.portForwardEnabledCount }),
  available: () =>
    !portForwardUnavailable(
      sshInfo.value?.enabled,
      transportAllowsH2.value,
      getNative()?.getCapabilities?.().tunnel !== false,
    ),
  unavailableReasonKey: 'settings.dockPlugins.unavailable.proxy',
  panel: {
    load: () => import('@/components/proxy/ProxyPanelContent.vue'),
  },
} satisfies DockPlugin
