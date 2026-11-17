import { describe, expect, it, vi } from 'vitest'

/**
 * The forge jump has the most rules of any deeplink, and every one of them
 * exists because of a real stranding bug. What is pinned here, in the order the
 * handler applies them:
 *
 *  1. A blocked tab refuses BEFORE publishing a target or arming a Back origin —
 *     otherwise a notification queued while forge was off would jump into a
 *     stale item the next time the panel mounted.
 *  2. The pending target is stamped with the project it belongs to, so a
 *     still-mounted old-project panel cannot consume a target meant for another.
 *  3. A refused cross-project switch (project gone / outside the roots) drops
 *     the target instead of leaving it armed for some later activation.
 *  4. The tab opens on both the resolved and the rejected path, because landing
 *     on the panel you can actually see beats nothing happening.
 */

const pendingTargets: unknown[] = []
const pendingGithub = { kind: 'issue', number: 7 }

vi.mock('@/composables/useForgeNavigation', () => ({
  forgeTargetFromDetail: (detail: unknown) => {
    const d = detail as { target?: unknown; forgeTarget?: unknown } | undefined
    return d?.target ?? d?.forgeTarget ?? null
  },
  setPendingForgeTarget: (t: unknown) => pendingTargets.push(t),
  clearPendingForgeTarget: () => pendingTargets.push('cleared'),
}))

vi.mock('@/composables/useForgeBinding', () => ({
  platform: { value: 'github' },
  forgeDockIconKind: () => 'github',
  forgeTabTitleKey: () => 'forge.tabTitle.github',
  useForgeBinding: () => ({ platform: { value: 'github' } }),
}))

vi.mock('@/composables/useForgeUnread', () => ({
  useForgeUnread: () => ({ forgeUnreadCount: { value: 2 } }),
}))

vi.mock('@/i18n', () => ({
  default: { global: { t: (k: string) => k } },
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

/** An async chain cannot be awaited to "done" without draining its ticks. */
const drain = () => new Promise((resolve) => setTimeout(resolve, 0))

async function forgePlugin() {
  return (await import('@/plugins/forge/plugin')).default
}

function jump(plugin: Awaited<ReturnType<typeof forgePlugin>>) {
  const found = plugin.deeplinks?.find((d) => d.event === 'clawbench-open-forge')
  if (!found) throw new Error('clawbench-open-forge deeplink is gone')
  return found
}

function harness(opts: { current?: string; blocked?: boolean; switchTo?: string } = {}) {
  const current = opts.current ?? '/proj/a'
  let project = current
  return {
    switchTab: vi.fn(),
    tabBlocked: vi.fn(() => opts.blocked === true),
    currentProject: () => project,
    recordNotificationOrigin: vi.fn(),
    hotSwitchProject: vi.fn((p: string) => {
      // The host refuses an unusable project by resolving without switching.
      if (opts.switchTo !== 'refuse') project = p
      return Promise.resolve()
    }),
  }
}

describe('forge dock plugin deeplink', () => {
  it('publishes the target and opens the tab for a same-project jump', async () => {
    const plugin = await forgePlugin()
    pendingTargets.length = 0
    const ctx = harness()

    jump(plugin).handler({ projectPath: '/proj/a', target: pendingGithub }, ctx)

    expect(pendingTargets[0]).toEqual({ ...pendingGithub, projectPath: '/proj/a' })
    expect(ctx.switchTab).toHaveBeenCalledWith('forge')
    expect(ctx.recordNotificationOrigin).toHaveBeenCalled()
  })

  it('refuses a blocked tab before writing any navigation state', async () => {
    const plugin = await forgePlugin()
    pendingTargets.length = 0
    const ctx = harness({ blocked: true })

    jump(plugin).handler({ projectPath: '/proj/a', target: pendingGithub }, ctx)

    // The only allowed write is the cleanup.
    expect(pendingTargets).toEqual(['cleared'])
    expect(ctx.switchTab).not.toHaveBeenCalled()
  })

  it('drops the target when a cross-project switch was refused', async () => {
    const plugin = await forgePlugin()
    pendingTargets.length = 0
    // The host resolves without switching (project gone / outside the roots).
    const ctx = harness({ current: '/proj/a', switchTo: 'refuse' })

    jump(plugin).handler({ projectPath: '/proj/b', target: pendingGithub }, ctx)
    await drain()

    expect(pendingTargets).toContain('cleared')
    // Still opens the tab: the panel on screen is the current project's.
    expect(ctx.switchTab).toHaveBeenCalledWith('forge')
  })

  it('keeps the target when the cross-project switch landed', async () => {
    const plugin = await forgePlugin()
    pendingTargets.length = 0
    const ctx = harness({ current: '/proj/a' })

    jump(plugin).handler({ projectPath: '/proj/b', target: pendingGithub }, ctx)
    await drain()

    expect(pendingTargets[0]).toEqual({ ...pendingGithub, projectPath: '/proj/b' })
    expect(pendingTargets).not.toContain('cleared')
  })

  it('opens the tab even when the switch throws', async () => {
    const plugin = await forgePlugin()
    pendingTargets.length = 0
    const ctx = harness({ current: '/proj/a' })
    ctx.hotSwitchProject.mockImplementationOnce(() => Promise.reject(new Error('boom')))

    jump(plugin).handler({ projectPath: '/proj/b' }, ctx)
    await drain()

    expect(ctx.switchTab).toHaveBeenCalledWith('forge')
    expect(pendingTargets).toContain('cleared')
  })

  it('names and draws the bound service rather than the neutral label', async () => {
    const plugin = await forgePlugin()
    // The dock resolves the title through the runtime platform; the manifest's
    // titleKey is only the fallback the panel uses when nothing is bound.
    expect(plugin.title?.()).toBe('forge.tabTitle.github')
    expect(plugin.badge?.()).toEqual({ count: 2 })
  })
})
