import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

/**
 * The self-describing part of the registry: panels that load lazily, the props
 * a host hands a panel, and the events a panel emits back.
 *
 * These are the seams between a manifest and its component. They only run when
 * a tab is opened, which is exactly why they are worth pinning here: a wrong
 * export path would throw only in the browser, and an event name that drifts
 * from what the SFC emits fails silently (an object key is not
 * compiler-normalized the way `@event-name` in a template is).
 */

const clearCwd = vi.fn()
const requestedCwd = ref('/tmp/here')
const platformUnsupported = ref(false)
const loadTasks = vi.fn()

vi.mock('@/stores/app', () => ({ store: { state: {}, loadFiles: vi.fn() } }))
vi.mock('@/composables/useTaskTab', () => ({ useTaskTab: () => ({ loadTasks }) }))
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
  useMailUnread: () => ({ total: ref(0) }),
  startMailUnreadPolling: vi.fn(),
  stopMailUnreadPolling: vi.fn(),
}))
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => ({ getCapabilities: () => ({ tunnel: true }) }),
}))

const load = async (id: string) => (await import(`@/plugins/${id}/plugin.ts`)).default

const PANELS_WITH_COMPONENT = ['browse', 'view'] as const

describe('plugin panel wiring', () => {
  it('hands the terminal panel its cwd request and the unsupported flag', async () => {
    const terminal = await load('terminal')
    const props = terminal.panel?.props?.({} as never, {
      terminal: { requestedCwd, platformUnsupported },
    } as never) as Record<string, unknown>
    // The panel renders a dedicated empty state from this flag; without it an
    // unsupported platform shows a dead terminal.
    expect(props).toEqual({ requestedCwd, platformUnsupported })
  })

  it('clears the cwd request when the panel reports it handled', async () => {
    clearCwd.mockClear()
    const terminal = await load('terminal')
    const on = terminal.panel?.on?.({} as never, { terminal: { clearCwd } } as never) as Record<
      string,
      () => void
    >
    // The event name is the SFC's kebab-case emit; an object key is a runtime
    // string, so it must be spelled exactly as the component emits it.
    expect(Object.keys(on)).toEqual(['cwd-handled'])
    on['cwd-handled']()
    expect(clearCwd).toHaveBeenCalled()
  })

  it('routes the forge panel request-project event to the chat tab', async () => {
    const forge = await load('forge')
    const openTab = vi.fn()
    const on = forge.panel?.on?.({ openTab } as never) as Record<string, () => void>
    on['request-project']()
    // "No repository bound" means: go where project switching lives.
    expect(openTab).toHaveBeenCalledWith('chat')
    expect(on['request-project']).toBeTypeOf('function')
  })

  it('gives the forge panel the host project path', async () => {
    const forge = await load('forge')
    const props = forge.panel?.props?.({ projectPath: '/proj/a' } as never) as Record<string, unknown>
    expect(props).toEqual({ projectPath: '/proj/a' })
  })

  it('sends the tasks panel open-file to the host task handler', async () => {
    const tasks = await load('tasks')
    const openTaskFile = vi.fn()
    const on = tasks.panel?.on?.({} as never, { task: { openTaskFile } } as never) as Record<
      string,
      (p: string) => void
    >
    on['open-file']('src/a.go')
    expect(openTaskFile).toHaveBeenCalledWith('src/a.go')
  })

  it('opens a new message via the mail panel host hook', async () => {
    const mail = await load('mail')
    const openSettingsCategory = vi.fn()
    const on = mail.panel?.on?.({ openSettingsCategory } as never) as Record<string, (p: string) => void>
    // The panel decides WHICH settings category; the host only navigates.
    on['open-settings']('mail')
    expect(openSettingsCategory).toHaveBeenCalledWith('mail')
  })

  it('parks a create-meeting return before opening the meeting settings', async () => {
    const { setMeetingCreateReturn } = await import('@/composables/useMeetingViewState')
    const live = await load('live')
    const openSettingsCategory = vi.fn()
    const on = live.panel?.on?.({ openSettingsCategory } as never, {
      meeting: { quote: vi.fn() },
    } as never) as Record<string, () => void>
    // Without the parked request, re-entering the tab drops the create form.
    expect(Object.keys(on)).toEqual(['quote', 'relaySettings'])
    on.relaySettings()
    expect(setMeetingCreateReturn).toHaveBeenCalled()
    expect(openSettingsCategory).toHaveBeenCalledWith('meeting')
  })

  it('leaves the panel import to the host (no module pulled in at manifest load)', async () => {
    for (const id of PANELS_WITH_COMPONENT) {
      const plugin = await load(id)
      // A manifest that imported its panel eagerly would pull the whole panel
      // graph into the dock's initial chunk — the registry imports every
      // manifest at startup.
      expect(plugin.panel, id).toBeUndefined()
    }
  })
})
