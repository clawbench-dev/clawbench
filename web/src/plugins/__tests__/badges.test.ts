import { describe, expect, it, vi } from 'vitest'
import { reactive, ref } from 'vue'

/**
 * Behaviour test for the numeric badges and the two host questions the dock
 * asks about a tab (is it usable here, and should it stay put when not).
 *
 * A badge is a callback read on every render; a snapshot taken at import would
 * show a stale count (or none). And `leaveWhenUnavailable` is the difference
 * between hiding a tab and showing its "not supported here" empty state — the
 * terminal panel needs the latter so it can explain itself instead of vanishing.
 */

const state = reactive({
  taskUnreadCount: 4,
  taskRunning: true,
  taskJustCompleted: false,
  terminalSessionCount: 2,
  portForwardEnabledCount: 3,
})

const loadTasks = vi.fn()
const clearCwd = vi.fn()
const terminalRuntimeEnabled = ref(true)
const platformSupported = ref<boolean | undefined>(true)
const sshInfo = ref<{ enabled?: boolean } | null>({ enabled: false })
const sshInfoResolved = ref(true)
const transportAllowsH2 = ref(false)

vi.mock('@/stores/app', () => ({ store: { state } }))
vi.mock('@/composables/useTaskTab', () => ({ useTaskTab: () => ({ loadTasks }) }))
vi.mock('@/composables/useTerminalStatus', () => ({
  useTerminalStatus: () => ({ terminalRuntimeEnabled, platformSupported }),
}))
vi.mock('@/composables/useForgeUnread', () => ({ useForgeUnread: () => ({ forgeUnreadCount: { value: 0 } }) }))
vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({ sshInfo, sshInfoResolved, transportAllowsH2 }),
}))
const tunnelSupported = ref(true)
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => ({ getCapabilities: () => ({ tunnel: tunnelSupported.value }) }),
}))

const load = async (id: string) => (await import(`@/plugins/${id}/plugin.ts`)).default

describe('dock badges and host questions', () => {
  it('reads the task badge live rather than snapshotting it', async () => {
    const tasks = await load('tasks')
    expect(tasks.badge?.()).toEqual({ count: 4 })
    state.taskUnreadCount = 0
    expect(tasks.badge?.()).toEqual({ count: 0 })
    state.taskUnreadCount = 4
  })

  it('exposes the three independent task states the dock renders', async () => {
    const tasks = await load('tasks')
    expect(tasks.appearance?.unread?.()).toBe(true)
    expect(tasks.appearance?.running?.()).toBe(true)
    expect(tasks.appearance?.justCompleted?.()).toBe(false)
  })

  it('reloads the task list when the tab is entered', async () => {
    loadTasks.mockClear()
    const tasks = await load('tasks')
    tasks.onEnter?.()
    expect(loadTasks).toHaveBeenCalledTimes(1)
  })

  it('keeps the terminal available only while the runtime reports it', async () => {
    const terminal = await load('terminal')
    expect(terminal.badge?.()).toEqual({ count: 2 })
    expect(terminal.available?.()).toBe(true)
    terminalRuntimeEnabled.value = false
    expect(terminal.available?.()).toBe(false)
    terminalRuntimeEnabled.value = true
  })

  it('leaves the unsupported terminal tab in place so the panel can explain itself', async () => {
    const terminal = await load('terminal')
    // The panel has a dedicated "unsupported on this platform" empty state; the
    // tab must stay reachable for the user to see it.
    platformSupported.value = false
    expect(terminal.leaveWhenUnavailable?.()).toBe(false)
    platformSupported.value = true
    expect(terminal.leaveWhenUnavailable?.()).toBe(true)
  })

  it('clears a file-manager cwd request on a plain dock click', async () => {
    clearCwd.mockClear()
    const terminal = await load('terminal')
    terminal.onClick?.({} as never, { terminal: { clearCwd } } as never)
    // Otherwise the terminal opened by a later click would land in a directory
    // the file manager asked for in an unrelated action.
    expect(clearCwd).toHaveBeenCalled()
  })

  it('hides port mapping only when neither wire can carry it', async () => {
    const proxy = await load('proxy')
    expect(proxy.badge?.()).toEqual({ count: 3 })
    // SSH off AND no h2: genuinely unusable.
    expect(proxy.available?.()).toBe(false)
    // The stream tunnel can carry it, so the tab must come back.
    transportAllowsH2.value = true
    expect(proxy.available?.()).toBe(true)
    transportAllowsH2.value = false

    // Nor on a host whose native bridge has no tunnel at all: there the h2
    // path is not available either, so both wires are gone.
    tunnelSupported.value = false
    expect(proxy.available?.()).toBe(false)
    tunnelSupported.value = true
  })

  it('treats an unanswered SSH probe as "keep the tab", not "SSH is off"', async () => {
    const proxy = await load('proxy')
    // The request has not landed: unknown is not a reason to hide a tab the
    // user may well be able to use.
    sshInfoResolved.value = false
    sshInfo.value = null
    expect(proxy.available?.()).toBe(true)
    sshInfoResolved.value = true
    sshInfo.value = { enabled: false }
  })
})
