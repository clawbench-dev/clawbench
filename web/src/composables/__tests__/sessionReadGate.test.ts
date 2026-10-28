import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

/**
 * sessionReadGate is the ONE statement of "the user is looking at this reply,
 * so clear its badge":
 *
 *     read ⇔ chat panel on screen AND app in foreground AND not a replay
 *
 * Before this module the rule existed in six copies that had drifted: some
 * checked the foreground, one also checked the replay flag, NONE checked
 * whether the chat panel was actually on screen. A resume that landed on the
 * file manager, or a wide-screen layout with the chat column collapsed, cleared
 * a badge the user never saw.
 *
 * The tests below pin the three inputs, the ordering contract (the list refresh
 * must run after the read SETTLES), and the fail-open default.
 */

const readMock = vi.fn()
vi.mock('@/utils/sessionRead', () => ({
  markSessionRead: (...args: unknown[]) => readMock(...args),
}))

const foreground = { value: true }
vi.mock('@/composables/useAppForeground', () => ({
  useAppForeground: () => ({ appInForeground: foreground }),
}))

import {
  markVisibleSessionRead,
  registerChatPanelVisibility,
  isChatPanelOnScreen,
  _resetChatPanelVisibilityForTest,
} from '@/composables/sessionReadGate'

describe('sessionReadGate', () => {
  beforeEach(() => {
    readMock.mockReset().mockResolvedValue(undefined)
    foreground.value = true
    _resetChatPanelVisibilityForTest()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('marks read when the panel is on screen and the app is in the foreground', async () => {
    registerChatPanelVisibility(() => true)
    const onMarked = vi.fn()

    const issued = await markVisibleSessionRead('sess-1', { onMarked })

    expect(issued).toBe(true)
    expect(readMock).toHaveBeenCalledTimes(1)
    // No project_path: the cookie fallback is the correct proof of ownership
    // for a same-project read.
    expect(readMock.mock.calls[0]).toEqual(['sess-1', undefined])
    expect(onMarked).toHaveBeenCalledTimes(1)
  })

  it('does nothing without a session id', async () => {
    registerChatPanelVisibility(() => true)
    const onMarked = vi.fn()

    expect(await markVisibleSessionRead('', { onMarked })).toBe(false)

    expect(readMock).not.toHaveBeenCalled()
    expect(onMarked).not.toHaveBeenCalled()
  })

  it('leaves the badge alone while the app is backgrounded', async () => {
    // A reply landing while the app is paused must stay unread: the Android
    // floating window surfaces exactly that state.
    registerChatPanelVisibility(() => true)
    foreground.value = false

    expect(await markVisibleSessionRead('sess-1')).toBe(false)
    expect(readMock).not.toHaveBeenCalled()
  })

  it('leaves the badge alone while the chat panel is off screen', async () => {
    // The case no earlier copy of the rule handled: foregrounded, but the user
    // is looking at the file manager / a collapsed chat column.
    registerChatPanelVisibility(() => false)

    expect(await markVisibleSessionRead('sess-1')).toBe(false)
    expect(readMock).not.toHaveBeenCalled()
  })

  it('leaves the badge alone for a replayed completion', async () => {
    // A terminal event replayed after a disconnect means the user was NOT
    // watching when it finished — which is why it is unread.
    registerChatPanelVisibility(() => true)

    expect(await markVisibleSessionRead('sess-1', { replaying: true })).toBe(false)
    expect(readMock).not.toHaveBeenCalled()
  })

  it('forwards project_path for a cross-project read', async () => {
    // Without it the backend falls back to the cookie project and 403s a
    // session that belongs to another project.
    registerChatPanelVisibility(() => true)

    await markVisibleSessionRead('sess-2', { projectPath: '/project/B' })

    expect(readMock.mock.calls[0]).toEqual(['sess-2', '/project/B'])
  })

  it('refreshes the list only after the read settles', async () => {
    // The list response carries the per-session unread count, so refreshing
    // first reads the pre-read state and re-lights the badge this call clears.
    registerChatPanelVisibility(() => true)
    let release: (() => void) | null = null
    readMock.mockImplementation(() => new Promise<void>((r) => { release = r }))
    const onMarked = vi.fn()

    const done = markVisibleSessionRead('sess-1', { onMarked })
    await Promise.resolve()
    expect(onMarked, 'list refreshed while the read was in flight').not.toHaveBeenCalled()

    release!()
    await done
    expect(onMarked).toHaveBeenCalledTimes(1)
  })

  it('does not call onMarked when the read is gated out', async () => {
    // The gate's callers use onMarked for the badge refresh; running it for a
    // read that never happened would only cause a pointless list request.
    registerChatPanelVisibility(() => false)
    const onMarked = vi.fn()

    await markVisibleSessionRead('sess-1', { onMarked })
    expect(onMarked).not.toHaveBeenCalled()
  })

  it('fails OPEN when no predicate is registered', async () => {
    // This gate can only ever RESTRICT behaviour that used to be unconditional.
    // An unregistered host (tests driving the composable directly, a future
    // embedder) must keep its read calls rather than silently losing them.
    expect(isChatPanelOnScreen()).toBe(true)

    const issued = await markVisibleSessionRead('sess-1')

    expect(issued).toBe(true)
    expect(readMock).toHaveBeenCalledTimes(1)
  })

  it('reads the predicate live, not as a snapshot', async () => {
    // Visibility changes while the app runs; a captured boolean would decide
    // for a layout the user has since left.
    let visible = false
    registerChatPanelVisibility(() => visible)

    expect(await markVisibleSessionRead('sess-1')).toBe(false)

    visible = true
    expect(await markVisibleSessionRead('sess-1')).toBe(true)
    expect(readMock).toHaveBeenCalledTimes(1)
  })

  it('replaces a previous predicate rather than stacking them', async () => {
    // App.vue registers once, but a hot reload or a test that registers twice
    // must not leave the old predicate deciding.
    registerChatPanelVisibility(() => false)
    registerChatPanelVisibility(() => true)

    expect(await markVisibleSessionRead('sess-1')).toBe(true)
  })
})
