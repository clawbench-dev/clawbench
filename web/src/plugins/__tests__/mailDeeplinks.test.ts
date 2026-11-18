import { afterEach, describe, expect, it, vi } from 'vitest'
import { _resetMailOpenForTest, takePendingMailOpen } from '@/composables/useMailOpen'

/**
 * Behaviour test for the parts of the plugin manifests that only run on an
 * event: the deeplink handlers, the badge callback and the lifecycle hooks.
 *
 * `manifests.test.ts` covers the declarative shape (ids, groups, predicates).
 * What is covered here is what a user can actually trigger — a notification tap
 * landing on a tab, the unread counter feeding a badge, a background poller
 * starting and stopping — because these are the paths a broken refactor would
 * only reveal at runtime, in the places hardest to reproduce by hand.
 *
 * Every assertion is about the host interaction (which tab was switched to,
 * whether the call was refused), not about the handler's internal shape.
 */

const switchTab = vi.fn()
const tabBlocked = vi.fn(() => false)
const recordNotificationOrigin = vi.fn()
const warn = vi.fn()
const startPolling = vi.fn()
const stopPolling = vi.fn()
const total = { value: 0 }

vi.mock('@/composables/useMailUnread', () => ({
  useMailUnread: () => ({ total }),
  startMailUnreadPolling: startPolling,
  stopMailUnreadPolling: stopPolling,
}))

vi.mock('@/utils/appLog', () => ({
  // The manifests log through appLog; a no-op keeps the assertions about the
  // host calls readable, and `warn` lets one test prove the log fired.
  appLog: { d: vi.fn(), i: vi.fn(), w: warn, e: vi.fn() },
}))

async function mailPlugin() {
  return (await import('@/plugins/mail/plugin')).default
}

function ctx() {
  return { switchTab, tabBlocked, recordNotificationOrigin, hotSwitchProject: vi.fn(), currentProject: () => '' }
}

// The parked open-mail id is module state: leaving one behind would make the
// next case's assertions depend on test order.
afterEach(() => {
  _resetMailOpenForTest()
})

describe('mail dock plugin', () => {
  it('feeds its badge from the unread counter, live', async () => {
    const plugin = await mailPlugin()
    total.value = 3
    // The badge reads on every render, so it must be a function of the current
    // value rather than a snapshot taken at import.
    expect(plugin.badge?.()).toEqual({ count: 3 })
    total.value = 0
    expect(plugin.badge?.()).toEqual({ count: 0 })
  })

  it('switches to the mail tab when a quote jumps to its email', async () => {
    const plugin = await mailPlugin()
    const jump = plugin.deeplinks?.find((d) => d.event === 'clawbench-open-mail')
    expect(jump).toBeTruthy()
    jump!.handler({ messageId: 12 }, ctx())
    expect(switchTab).toHaveBeenCalledWith('mail')
    // The jump must arm the notification center as Back's target. It is a
    // no-op for a quote jump (no fromNotificationCenter flag), which is exactly
    // why the handler forwards the raw detail rather than deciding itself.
    expect(recordNotificationOrigin).toHaveBeenCalledWith({ messageId: 12 })
    // The panel is lazily mounted: on a cold jump this handler is the only
    // listener, so the id has to be parked for the mount that follows.
    expect(takePendingMailOpen()).toBe(12)
  })

  it('ignores an open-mail event with no message id, and says so', async () => {
    switchTab.mockClear()
    warn.mockClear()
    const plugin = await mailPlugin()
    const jump = plugin.deeplinks?.find((d) => d.event === 'clawbench-open-mail')
    jump!.handler({}, ctx())
    // Switching would land the user on an empty tab: the event carries no
    // request, so there is nothing to open.
    expect(switchTab).not.toHaveBeenCalled()
    expect(warn).toHaveBeenCalled()
  })

  it('refuses a jump into a blocked tab', async () => {
    switchTab.mockClear()
    tabBlocked.mockReturnValueOnce(true)
    const plugin = await mailPlugin()
    const jump = plugin.deeplinks?.find((d) => d.event === 'clawbench-compose-mail')
    jump!.handler(undefined, ctx())
    // Same rule as every other programmatic path: a hidden/unavailable tab must
    // not be reached by a link, or the user lands on a panel with no entry.
    expect(switchTab).not.toHaveBeenCalled()
    tabBlocked.mockReturnValue(false)
  })

  it('starts the unread poller as a background resource and stops it on teardown', async () => {
    const plugin = await mailPlugin()
    const stop = plugin.startBackground?.()
    expect(startPolling).toHaveBeenCalled()
    // Returning the stopper is the host's only way to release the resource; a
    // hook that starts without returning one leaks a timer per mount.
    expect(typeof stop).toBe('function')
    stop!()
    expect(stopPolling).toHaveBeenCalled()
  })
})
