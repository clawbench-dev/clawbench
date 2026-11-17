import { beforeEach, describe, expect, it, vi } from 'vitest'

// Plain ref-like objects: the runtime only reads `.value`, and this test does
// not need Vue reactivity. Keeping them inside vi.hoisted avoids the TDZ that
// top-level `const` mocks hit when vi.mock is hoisted above them.
const mocks = vi.hoisted(() => ({
  storeState: {
    currentDir: '/tmp',
    chatUnreadCount: 0,
    gitWorkingTreeChangeCount: 0,
    taskUnreadCount: 0,
    terminalSessionCount: 0,
    portForwardEnabledCount: 0,
  },
  loadFiles: vi.fn(),
  loadSessions: vi.fn(),
  loadTasks: vi.fn(),
  serverConfig: { value: { forge: { enabled: true }, mail: { enabled: true } } as Record<string, any> },
  forgeUnread: { value: 0 },
  mailTotal: { value: 0 },
  platform: { value: 'github' },
  terminalEnabled: { value: true },
  platformSupported: { value: true },
  sshInfo: { value: { enabled: true } as { enabled: boolean } | null },
  // Whether `/api/ssh/info` answered. A failed request leaves `sshInfo` null
  // without disabling anything (see portForwardUnavailable's `sshLoaded`).
  sshInfoResolved: { value: true },
  transportH2: { value: true },
  liveState: { value: 'idle' },
  startMail: vi.fn(),
  stopMail: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  store: {
    state: mocks.storeState,
    loadFiles: mocks.loadFiles,
  },
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({ serverConfig: mocks.serverConfig }),
}))

vi.mock('@/composables/useForgeUnread', () => ({
  useForgeUnread: () => ({ forgeUnreadCount: mocks.forgeUnread }),
}))

vi.mock('@/composables/useForgeBinding', () => ({
  useForgeBinding: () => ({ platform: mocks.platform }),
  forgeDockIconKind: (p: string) => (p === 'gitlab' ? 'gitlab' : 'github'),
  forgeTabTitleKey: (p: string) => (p === 'gitlab' ? 'nav.forgeGitlab' : 'nav.forgeGithub'),
}))

vi.mock('@/composables/useMailUnread', () => ({
  useMailUnread: () => ({ total: mocks.mailTotal }),
  startMailUnreadPolling: mocks.startMail,
  stopMailUnreadPolling: mocks.stopMail,
}))

vi.mock('@/composables/useTerminalStatus', () => ({
  useTerminalStatus: () => ({
    terminalRuntimeEnabled: mocks.terminalEnabled,
    platformSupported: mocks.platformSupported,
  }),
}))

vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({
    sshInfo: mocks.sshInfo,
    sshInfoResolved: mocks.sshInfoResolved,
    transportAllowsH2: mocks.transportH2,
  }),
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => undefined,
}))

vi.mock('@/composables/useLiveShare', () => ({
  useLiveShare: () => ({ state: mocks.liveState }),
}))

vi.mock('@/composables/useChatSession', () => ({
  loadSessionsOnce: mocks.loadSessions,
}))

vi.mock('@/composables/useTaskTab', () => ({
  useTaskTab: () => ({ loadTasks: mocks.loadTasks }),
}))

vi.mock('@/i18n', () => ({
  default: { global: { t: (key: string) => key } },
}))

import {
  isDockPluginAvailable,
  resolveDockBadge,
  resolveDockTitle,
  runDockOnEnter,
  startDockBackground,
} from '@/composables/dockPluginRuntime'

describe('dock plugin runtime', () => {
  beforeEach(() => {
    mocks.storeState.gitWorkingTreeChangeCount = 0
    mocks.storeState.taskUnreadCount = 0
    mocks.storeState.terminalSessionCount = 0
    mocks.storeState.portForwardEnabledCount = 0
    mocks.forgeUnread.value = 0
    mocks.mailTotal.value = 0
    mocks.platform.value = 'github'
    mocks.terminalEnabled.value = true
    mocks.platformSupported.value = true
    mocks.sshInfo.value = { enabled: true }
    mocks.sshInfoResolved.value = true
    mocks.transportH2.value = true
    mocks.liveState.value = 'idle'
    mocks.serverConfig.value = { forge: { enabled: true }, mail: { enabled: true } }
    mocks.startMail.mockClear()
    mocks.stopMail.mockClear()
    mocks.loadFiles.mockClear()
    mocks.loadSessions.mockClear()
    mocks.loadTasks.mockClear()
  })

  it('reads badges from each plugin source', () => {
    mocks.storeState.gitWorkingTreeChangeCount = 3
    mocks.storeState.taskUnreadCount = 4
    mocks.storeState.terminalSessionCount = 2
    mocks.storeState.portForwardEnabledCount = 1
    mocks.forgeUnread.value = 5
    mocks.mailTotal.value = 6

    expect(resolveDockBadge('history')?.count).toBe(3)
    expect(resolveDockBadge('tasks')?.count).toBe(4)
    expect(resolveDockBadge('terminal')?.count).toBe(2)
    expect(resolveDockBadge('proxy')?.count).toBe(1)
    expect(resolveDockBadge('forge')?.count).toBe(5)
    expect(resolveDockBadge('mail')?.count).toBe(6)
    expect(resolveDockBadge('view')).toBeNull()
  })

  it('reports a meeting as a dot, not a count', () => {
    expect(resolveDockBadge('live')).toEqual({ count: 0, live: false })
    mocks.liveState.value = 'publishing'
    expect(resolveDockBadge('live')).toEqual({ count: 0, live: true })
  })

  it('leaves the feature axis to the server registry', () => {
    // `available()` is a CAPABILITY probe only. Forge and mail are switched off
    // through `/api/features` (see useDockAvailability), so they must not carry a
    // second, local copy of the config check here.
    expect(isDockPluginAvailable('forge')).toBe(true)
    expect(isDockPluginAvailable('mail')).toBe(true)

    mocks.serverConfig.value = { forge: { enabled: false }, mail: { enabled: false } }
    expect(isDockPluginAvailable('forge')).toBe(true)
    expect(isDockPluginAvailable('mail')).toBe(true)
  })

  it('gates terminal/proxy on runtime capability', () => {
    expect(isDockPluginAvailable('terminal')).toBe(true)
    mocks.terminalEnabled.value = false
    expect(isDockPluginAvailable('terminal')).toBe(false)

    expect(isDockPluginAvailable('proxy')).toBe(true)
    mocks.sshInfo.value = { enabled: false }
    mocks.transportH2.value = false
    expect(isDockPluginAvailable('proxy')).toBe(false)

    // An SSH answer that never arrived is NOT "SSH is off": the value never
    // retries, so reading the failed request as disabled hid the tab for the
    // rest of the session.
    mocks.sshInfoResolved.value = false
    mocks.sshInfo.value = null
    expect(isDockPluginAvailable('proxy')).toBe(true)
  })

  it('resolves the forge title from the bound platform', () => {
    expect(resolveDockTitle('forge')).toBe('nav.forgeGithub')
    mocks.platform.value = 'gitlab'
    expect(resolveDockTitle('forge')).toBe('nav.forgeGitlab')
  })

  it('starts and stops the mail background resource', () => {
    const cleanup = startDockBackground('mail')
    expect(mocks.startMail).toHaveBeenCalledTimes(1)
    expect(cleanup).toBeTypeOf('function')

    cleanup!()
    expect(mocks.stopMail).toHaveBeenCalledTimes(1)
  })

  it('runs each plugin onEnter hook', () => {
    runDockOnEnter('browse')
    runDockOnEnter('chat')
    runDockOnEnter('tasks')

    expect(mocks.loadFiles).toHaveBeenCalledWith('/tmp', false, 0, true)
    expect(mocks.loadSessions).toHaveBeenCalledTimes(1)
    expect(mocks.loadTasks).toHaveBeenCalledTimes(1)
  })
})
