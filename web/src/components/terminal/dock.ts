import { store } from '@/stores/app'
import { useTerminalStatus } from '@/composables/useTerminalStatus'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

const { terminalRuntimeEnabled, platformSupported } = useTerminalStatus()

/**
 * Terminal runtime.
 *
 * `available` mirrors the old `isTerminalDisabled` check: the server's runtime
 * status decides whether the entry exists. `leaveWhenUnavailable` keeps the
 * platform-unsupported case on the tab so the panel can render its dedicated
 * "unsupported" empty state — the old watcher's `!isPlatformUnsupported` guard.
 */
export const terminalDock: DockPluginRuntime = {
  badge: () => ({ count: store.state.terminalSessionCount }),
  available: () => terminalRuntimeEnabled.value === true,
  leaveWhenUnavailable: () => platformSupported.value !== false,
  unavailableReasonKey: 'settings.dockPlugins.unavailable.terminal',
  panel: {
    load: () => import('./TerminalPanelContent.vue'),
    props: (host) => ({
      requestedCwd: host.terminalRequestedCwd,
      platformUnsupported: host.platformUnsupported,
    }),
    on: (host) => ({ 'cwd-handled': host.clearTerminalCwd }),
  },
}
