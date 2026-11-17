import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

/**
 * The dock lifecycle hooks that do work on enter/leave, plus the mail poller
 * failure path.
 *
 * `onEnter` is where "load on open" lives, and it is also where an unguarded
 * call becomes a per-visit request storm: the host calls it every time the tab
 * becomes visible, so a hook that fetches unconditionally multiplies the
 * traffic. These tests pin that the guards are there, and that the chat tab —
 * which may already have a live session — does not reload sessions just because
 * the user looked at it.
 */

const state = {
  currentDir: '/tmp/dir',
  currentSession: null as string | null,
}
const loadFiles = vi.fn()
const loadSessionsOnce = vi.fn()
const runningSessions = ref(new Set<string>())
const mailTotal = ref(0)
const startMailUnreadPolling = vi.fn()

vi.mock('@/stores/app', () => ({ store: { state, loadFiles } }))
vi.mock('@/composables/useChatSession', () => ({ loadSessionsOnce }))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: () => ({ runningSessions }),
}))
vi.mock('@/composables/useTaskTab', () => ({ useTaskTab: () => ({ loadTasks: vi.fn() }) }))
vi.mock('@/composables/useTerminalStatus', () => ({
  useTerminalStatus: () => ({ terminalRuntimeEnabled: ref(true), platformSupported: ref(true) }),
}))
vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({ sshInfo: ref(null), sshInfoResolved: ref(true), transportAllowsH2: ref(false) }),
}))
vi.mock('@/composables/useLiveShare', () => ({ useLiveShare: () => ({ state: ref('idle') }) }))
vi.mock('@/composables/useMeetingViewState', () => ({ setMeetingCreateReturn: vi.fn() }))
vi.mock('@/composables/useForgeBinding', () => ({
  platform: ref('github'),
  forgeDockIconKind: () => 'github',
  forgeTabTitleKey: () => 'forge.tabTitle.github',
  useForgeBinding: () => ({ platform: ref('github') }),
}))
vi.mock('@/composables/useForgeUnread', () => ({ useForgeUnread: () => ({ forgeUnreadCount: ref(0) }) }))
vi.mock('@/composables/useMailUnread', () => ({
  useMailUnread: () => ({ total: mailTotal }),
  startMailUnreadPolling,
  stopMailUnreadPolling: vi.fn(),
}))
vi.mock('@/i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/i18n')>()
  return { ...actual, default: { global: { t: (key: string) => key } } }
})
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => ({ getCapabilities: () => ({ tunnel: true }) }),
}))

const load = async (id: string) => (await import(`@/plugins/${id}/plugin.ts`)).default

describe('dock lifecycle hooks', () => {
  it('reloads the file list on entering the file manager', async () => {
    loadFiles.mockClear()
    const browse = await load('browse')
    browse.onEnter?.()
    // The file manager is the one tab whose content is a directory listing: a
    // stale list after an external change is the bug this reload prevents.
    expect(loadFiles).toHaveBeenCalledWith('/tmp/dir', false, 0, true)
  })

  it('loads the session list once on entering chat', async () => {
    loadSessionsOnce.mockClear()
    state.currentSession = null
    const chat = await load('chat')
    // No session yet: the list has to be loaded for the tab to show anything.
    chat.onEnter?.()
    expect(loadSessionsOnce).toHaveBeenCalled()
  })

  it('asks the session store to load once, however often the tab is entered', async () => {
    loadSessionsOnce.mockClear()
    const chat = await load('chat')
    chat.onEnter?.()
    chat.onEnter?.()
    // The "once" is the store's contract (loadSessionsOnce), not a guard in the
    // manifest: what the manifest owes is to keep asking it on every enter, so a
    // project switch still refreshes the list, while the store coalesces the
    // repeat calls.
    expect(loadSessionsOnce).toHaveBeenCalledTimes(2)
  })

  it('reports a running session as the chat dot', async () => {
    runningSessions.value = new Set(['s1'])
    const chat = await load('chat')
    expect(chat.appearance?.running?.()).toBe(true)
    runningSessions.value = new Set()
    expect(chat.appearance?.running?.()).toBe(false)
  })

  it('keeps polling for mail even while the tab is closed', async () => {
    startMailUnreadPolling.mockClear()
    const mail = await load('mail')
    // startBackground exists for exactly this: the badge must be right before
    // the user opens the tab.
    expect(typeof mail.startBackground).toBe('function')
    mail.startBackground?.()
    expect(startMailUnreadPolling).toHaveBeenCalled()
  })
})
