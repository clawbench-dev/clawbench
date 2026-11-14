import { store } from '@/stores/app'
import { usePortForward } from '@/composables/usePortForward'
import { getNative } from '@/utils/clawbenchNative'
import { portForwardUnavailable } from '@/utils/portForwardUtils'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

const { sshInfo, transportAllowsH2 } = usePortForward()

/**
 * Port mapping is unusable only when NEITHER wire can carry it: the SSH
 * listener is off AND the configured transport cannot use h2. Keying off SSH
 * alone hid the tab on installs that forward over the stream tunnel.
 */
export const proxyDock: DockPluginRuntime = {
  badge: () => ({ count: store.state.portForwardEnabledCount }),
  available: () =>
    !portForwardUnavailable(
      sshInfo.value?.enabled,
      transportAllowsH2.value,
      getNative()?.getCapabilities?.().tunnel !== false,
    ),
  unavailableReasonKey: 'settings.dockPlugins.unavailable.proxy',
  panel: {
    load: () => import('./ProxyPanelContent.vue'),
  },
}
