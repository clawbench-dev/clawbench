import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * Plugin-declared window listeners.
 *
 * The shell used to own one `handleOpenXxx` per plugin plus the matching
 * add/removeEventListener pair. Now the plugin declares the event and the host
 * installs it — which makes two things load-bearing and easy to break:
 *   - the SAME listener reference is removed (a stale listener runs every deep
 *     link twice after a remount);
 *   - one event has exactly one owner (two plugins claiming it would either
 *     double-navigate or silently drop one).
 */

type Handler = (detail: unknown, ctx: unknown) => void

const state = vi.hoisted(() => ({
  links: [] as { pluginId: string; event: string; handler: Handler }[],
}))

vi.mock('@/composables/dockPluginRuntime', () => ({
  dockDeepLinks: () =>
    state.links.map(({ pluginId, event, handler }) => ({
      pluginId,
      link: { event, handler },
    })),
}))

import { installPluginDeeplinks } from '@/composables/usePluginDeeplinks'

const handlerA = vi.fn<Handler>()
const handlerB = vi.fn<Handler>()

const ctx = {
  switchTab: vi.fn(),
  hotSwitchProject: vi.fn(async () => {}),
  tabBlocked: vi.fn(() => false),
  recordNotificationOrigin: vi.fn(),
  currentProject: vi.fn(() => '/p'),
}

beforeEach(() => {
  handlerA.mockClear()
  handlerB.mockClear()
  state.links = [
    { pluginId: 'forge', event: 'evt-a', handler: handlerA },
    { pluginId: 'mail', event: 'evt-b', handler: handlerB },
  ]
  for (const fn of Object.values(ctx)) (fn as ReturnType<typeof vi.fn>).mockClear()
})

describe('installPluginDeeplinks', () => {
  it('installs one listener per declared link and passes the detail through', () => {
    const uninstall = installPluginDeeplinks(ctx)

    window.dispatchEvent(new CustomEvent('evt-a', { detail: { id: 1 } }))
    window.dispatchEvent(new CustomEvent('evt-b', { detail: { id: 2 } }))

    expect(handlerA).toHaveBeenCalledWith({ id: 1 }, ctx)
    expect(handlerB).toHaveBeenCalledWith({ id: 2 }, ctx)

    uninstall()
  })

  it('removes exactly what it installed', () => {
    const uninstall = installPluginDeeplinks(ctx)
    uninstall()

    window.dispatchEvent(new CustomEvent('evt-a', { detail: {} }))
    expect(handlerA).not.toHaveBeenCalled()
  })

  it('survives being installed twice without doubling the handlers', () => {
    // A remount installs again before the previous uninstaller ran (HMR, project
    // switch). Each install owns its own listener, so the uninstallers must be
    // independent — but the handler must not run twice for one dispatch.
    const first = installPluginDeeplinks(ctx)
    first()
    const second = installPluginDeeplinks(ctx)

    window.dispatchEvent(new CustomEvent('evt-a', { detail: {} }))
    expect(handlerA).toHaveBeenCalledTimes(1)

    second()
  })

  it('keeps the first owner when two plugins claim one event', () => {
    state.links = [
      { pluginId: 'forge', event: 'evt-a', handler: handlerA },
      { pluginId: 'mail', event: 'evt-a', handler: handlerB },
    ]
    const uninstall = installPluginDeeplinks(ctx)

    window.dispatchEvent(new CustomEvent('evt-a', { detail: {} }))
    // Only the first declaration is wired; the collision is reported, not
    // silently resolved by "last one wins".
    expect(handlerA).toHaveBeenCalledTimes(1)

    uninstall()
  })
})
