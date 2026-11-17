import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

/**
 * The meeting jump is the only deeplink that can switch PROJECTS, and that is
 * where it gets subtle: a meeting record is project-scoped, so opening one that
 * belongs to another project against the current one lands on a 404 / empty
 * detail page. The handler therefore hot-switches first and re-dispatches the
 * event once the switch has landed — the re-dispatch is what lets the panel's
 * own listener fetch the record.
 *
 * These tests pin the decision (switch vs. re-dispatch vs. refuse), not the
 * code shape: a future refactor may rewrite the handler freely as long as a
 * cross-project jump still re-dispatches and a same-project one does not.
 */

const liveState = ref('idle')

vi.mock('@/composables/useLiveShare', () => ({
  useLiveShare: () => ({ state: liveState }),
}))

vi.mock('@/composables/useMeetingViewState', () => ({
  setMeetingCreateReturn: vi.fn(),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

async function livePlugin() {
  return (await import('@/plugins/live/plugin')).default
}

function harness(currentProject = '/proj/a') {
  return {
    switchTab: vi.fn(),
    currentProject: () => currentProject,
    recordNotificationOrigin: vi.fn(),
    hotSwitchProject: vi.fn(() => Promise.resolve()),
  }
}

function jump(plugin: Awaited<ReturnType<typeof livePlugin>>) {
  const found = plugin.deeplinks?.find((d) => d.event === 'clawbench-open-meeting')
  if (!found) throw new Error('clawbench-open-meeting deeplink is gone')
  return found
}

describe('meeting dock plugin deeplink', () => {
  it('switches to the meeting tab for a same-project jump, without re-dispatching', async () => {
    const plugin = await livePlugin()
    const ctx = harness('/proj/a')
    const dispatched = vi.fn()
    window.addEventListener('clawbench-open-meeting', dispatched)

    jump(plugin).handler({ token: 't1', projectPath: '/proj/a' }, ctx)

    expect(ctx.switchTab).toHaveBeenCalledWith('live')
    expect(ctx.hotSwitchProject).not.toHaveBeenCalled()
    expect(ctx.recordNotificationOrigin).toHaveBeenCalled()
    // A second dispatch would make the panel fetch the same record twice.
    expect(dispatched).not.toHaveBeenCalled()
    window.removeEventListener('clawbench-open-meeting', dispatched)
  })

  it('ignores an event with no token', async () => {
    const plugin = await livePlugin()
    const ctx = harness()
    jump(plugin).handler({}, ctx)
    // Nothing to open, so navigating would strand the user on an empty panel.
    expect(ctx.switchTab).not.toHaveBeenCalled()
    expect(ctx.hotSwitchProject).not.toHaveBeenCalled()
  })

  it('hot-switches the project first, then re-dispatches so the panel can fetch', async () => {
    const plugin = await livePlugin()
    const ctx = harness('/proj/a')
    const dispatched = vi.fn()
    window.addEventListener('clawbench-open-meeting', dispatched)

    jump(plugin).handler({ token: 't2', projectPath: '/proj/b' }, ctx)

    // The switch is asynchronous: the tab must not claim to be open before the
    // project behind it is the right one.
    expect(ctx.hotSwitchProject).toHaveBeenCalledWith('/proj/b')
    expect(ctx.switchTab).not.toHaveBeenCalled()
    // A cross-project jump must not arm a Back target in the old project.
    expect(ctx.recordNotificationOrigin).not.toHaveBeenCalled()

    await Promise.resolve()
    await Promise.resolve()

    expect(ctx.switchTab).toHaveBeenCalledWith('live')
    expect(dispatched).toHaveBeenCalledTimes(1)
    const event = dispatched.mock.calls[0][0] as CustomEvent
    expect(event.detail).toEqual({ token: 't2' })
    window.removeEventListener('clawbench-open-meeting', dispatched)
  })

  it('still opens the tab when the project switch fails', async () => {
    const plugin = await livePlugin()
    const ctx = harness('/proj/a')
    ctx.hotSwitchProject.mockImplementationOnce(() => Promise.reject(new Error('gone')))

    jump(plugin).handler({ token: 't3', projectPath: '/proj/b' }, ctx)
    await Promise.resolve()
    await Promise.resolve()

    // A dead target must not leave the user on the previous tab with no
    // explanation: open the meeting tab against the current project instead.
    expect(ctx.switchTab).toHaveBeenCalledWith('live')
  })

  it('marks the tab live while a meeting is not idle', async () => {
    const plugin = await livePlugin()
    liveState.value = 'in-meeting'
    expect(plugin.badge?.()).toEqual({ count: 0, live: true })
    expect(plugin.appearance?.liveDot?.()).toBe(true)
    liveState.value = 'idle'
    expect(plugin.badge?.()).toEqual({ count: 0, live: false })
    expect(plugin.appearance?.liveDot?.()).toBe(false)
  })
})
