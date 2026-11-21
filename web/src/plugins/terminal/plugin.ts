import { SquareTerminal as TerminalIcon, SquareTerminal } from 'lucide-vue-next'
import { store } from '@/stores/app'
import { useTerminalStatus } from '@/composables/useTerminalStatus'
import type { DockPlugin } from '@/composables/dockPluginTypes'

const { terminalRuntimeEnabled, platformSupported } = useTerminalStatus()

/**
 * Terminal.
 *
 * `available` mirrors the old `isTerminalDisabled` check: the server's runtime
 * status decides whether the entry exists. `leaveWhenUnavailable` keeps the
 * platform-unsupported case on the tab so the panel can render its dedicated
 * "unsupported" empty state — the old watcher's `!isPlatformUnsupported` guard.
 */
export default {
  id: 'terminal',
  order: 60,
  titleKey: 'terminal.title',
  descriptionKey: 'settings.dockPlugins.desc.terminal',
  group: 'project',
  closable: true,
  feature: 'terminal.enabled',
  capability: 'terminal',
  settingsCategory: 'terminal',
  icon: TerminalIcon,
  settingsPage: {
    group: 'dockModules',
    icon: SquareTerminal,
    kind: 'group',
  },
  badge: () => ({ count: store.state.terminalSessionCount }),
  available: () => terminalRuntimeEnabled.value === true,
  leaveWhenUnavailable: () => platformSupported.value !== false,
  unavailableReasonKey: 'settings.dockPlugins.unavailable.terminal',
  // Clicking the dock entry means "open a terminal", not "open the terminal at
  // the directory the file manager asked for" — the file manager's own
  // `open-terminal` action sets that request, and it must not leak into a later
  // plain click.
  onClick: (_host, extras) => extras.terminal.clearCwd(),
  panel: {
    load: () => import('@/components/terminal/TerminalPanelContent.vue'),
    props: (_host, extras) => ({
      requestedCwd: extras.terminal.requestedCwd,
      platformUnsupported: extras.terminal.platformUnsupported,
    }),
    on: (_host, extras) => ({ 'cwd-handled': extras.terminal.clearCwd }),
  },
} satisfies DockPlugin
