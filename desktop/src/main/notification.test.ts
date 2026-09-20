import { describe, it, expect, vi, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import type { NotificationNav } from '../shared/types'

// vi.mock factories are hoisted above the file body, so everything they touch
// must be created with vi.hoisted.
const h = vi.hoisted(() => {
  return {
    showMock: vi.fn(),
    clickHandler: { current: null as null | (() => void) },
    sent: [] as Array<{ channel: string; nav: unknown }>,
    winState: { minimized: false, visible: true, loading: false },
    calls: { restore: 0, show: 0, focus: 0 },
    windowAvailable: true,
  }
})

vi.mock('electron', () => ({
  Notification: class {
    static isSupported() { return true }
    constructor(public opts: { title: string; body: string }) {}
    on(event: string, cb: () => void) {
      if (event === 'click') h.clickHandler.current = cb
      return this
    }
    show() { h.showMock() }
  },
}))

vi.mock('./window', () => ({
  getMainWindow: () => (h.windowAvailable
    ? {
        webContents: {
          send: (channel: string, nav: unknown) => { h.sent.push({ channel, nav }) },
          isLoading: () => h.winState.loading,
        },
        isMinimized: () => h.winState.minimized,
        isVisible: () => h.winState.visible,
        restore: () => { h.calls.restore++; h.winState.minimized = false },
        show: () => { h.calls.show++; h.winState.visible = true },
        focus: () => { h.calls.focus++ },
      }
    : null),
}))

import {
  showTerminalNotification,
  getPendingNavigationJson,
} from './notification'
import { markRendererReady, markRendererLoading, resetRendererReady } from './navReady'
import { NAV_CHANNELS } from '../shared/types'

/**
 * The preload cannot import NAV_CHANNELS (sandboxed preloads may not require()
 * local files), so it keeps its own copy. These two guards keep the copy honest:
 * the list must match, and the preload must not grow a local require() that
 * would throw at load time and take down the whole ClawBenchNative bridge.
 */
describe('preload channel list stays in sync', () => {
  const preloadSrc = readFileSync(resolve(__dirname, '../preload/index.ts'), 'utf8')

  it('declares exactly the channels in shared/types', () => {
    for (const channel of NAV_CHANNELS) {
      expect(preloadSrc).toContain(`'${channel}'`)
    }
    // And no extra channel sneaks in.
    const declared = [...preloadSrc.matchAll(/'(clawbench-open-[a-z]+)'/g)].map((m) => m[1])
    expect([...new Set(declared)].sort()).toEqual([...NAV_CHANNELS].sort())
  })

  it('imports nothing but electron (a local require would crash the preload)', () => {
    // Sandboxed preloads cannot resolve relative requires. Only the bare
    // 'electron' specifier is allowed.
    const imports = [...preloadSrc.matchAll(/^\s*import\s[^'"]*['"]([^'"]+)['"]/gm)].map((m) => m[1])
    expect(imports).toEqual(['electron'])
  })
})

function clickLatest(): void {
  const cb = h.clickHandler.current
  if (!cb) throw new Error('no click handler captured — was a notification shown?')
  cb()
}

describe('showTerminalNotification', () => {
  beforeEach(() => {
    h.sent.length = 0
    h.clickHandler.current = null
    h.showMock.mockReset()
    h.windowAvailable = true
    h.winState.minimized = false
    h.winState.visible = true
    h.winState.loading = false
    h.calls.restore = 0
    h.calls.show = 0
    h.calls.focus = 0
    resetRendererReady()
  })

  it('routes a session notification click to clawbench-open-session', () => {
    markRendererReady()
    showTerminalNotification('done', 'body', { sessionId: 's1', projectPath: '/p' })
    clickLatest()

    expect(h.sent).toHaveLength(1)
    expect(h.sent[0].channel).toBe('clawbench-open-session')
    expect((h.sent[0].nav as NotificationNav).sessionId).toBe('s1')
  })

  it('routes a task notification click to clawbench-open-task', () => {
    markRendererReady()
    showTerminalNotification('done', 'body', { taskId: '7', executionId: 'e1' })
    clickLatest()

    expect(h.sent[0].channel).toBe('clawbench-open-task')
    expect((h.sent[0].nav as NotificationNav).taskId).toBe('7')
  })

  it('routes a forge notification click to clawbench-open-forge', () => {
    // Regression: forge notifications carry ONLY projectPath. Without the
    // explicit discriminator the taskId/sessionId branches both miss, so the
    // click fell through to the session channel and the renderer's
    // handleOpenSession bailed on the missing sessionId — silently dropped.
    markRendererReady()
    showTerminalNotification('owner/repo · 议题 #1 · 已合并', 'title', {
      projectPath: '/p',
      forge: true,
    })
    clickLatest()

    expect(h.sent).toHaveLength(1)
    expect(h.sent[0].channel).toBe('clawbench-open-forge')
    expect((h.sent[0].nav as NotificationNav).projectPath).toBe('/p')
  })

  it('restores and shows a minimized window before focusing', () => {
    // Regression: the old code called focus() only, which is a no-op on a
    // minimized window (verified on Linux: isMinimized stays true). Clicking a
    // notification therefore left the window minimized.
    markRendererReady()
    h.winState.minimized = true
    h.winState.visible = false

    showTerminalNotification('done', 'body', { sessionId: 's1' })
    clickLatest()

    expect(h.calls.restore).toBe(1)
    expect(h.calls.show).toBe(1)
    expect(h.calls.focus).toBe(1)
  })

  it('shows a hidden (not minimized) window', () => {
    markRendererReady()
    h.winState.minimized = false
    h.winState.visible = false

    showTerminalNotification('done', 'body', { sessionId: 's1' })
    clickLatest()

    expect(h.calls.restore).toBe(0) // nothing to restore
    expect(h.calls.show).toBe(1)
    expect(h.calls.focus).toBe(1)
  })

  it('does not send to the renderer before it reports ready, and defers instead', () => {
    // Regression: readiness was keyed on webContents.isLoading(), which goes
    // false long before App.vue registers its listeners. A click in that
    // window was sent to a page with no listener and lost forever.
    h.winState.loading = false // page "loaded", but the renderer has NOT said ready
    showTerminalNotification('done', 'body', { sessionId: 'cold-1' })
    clickLatest()

    expect(h.sent).toHaveLength(0)
    expect(getPendingNavigationJson()).toBe(JSON.stringify({ sessionId: 'cold-1' }))
  })

  it('delivers normally once the renderer reports ready', () => {
    markRendererLoading()
    showTerminalNotification('done', 'body', { sessionId: 'cold-1' })
    clickLatest()
    expect(h.sent).toHaveLength(0)

    markRendererReady()
    showTerminalNotification('done', 'body', { sessionId: 'live-1' })
    clickLatest()

    expect(h.sent).toHaveLength(1)
    expect((h.sent[0].nav as NotificationNav).sessionId).toBe('live-1')
  })

  it('consumes the pending navigation exactly once', () => {
    showTerminalNotification('done', 'body', { sessionId: 'cold-1' })
    clickLatest()

    expect(getPendingNavigationJson()).not.toBeNull()
    expect(getPendingNavigationJson()).toBeNull()
  })

  it('does nothing when there is no window', () => {
    markRendererReady()
    h.windowAvailable = false
    showTerminalNotification('done', 'body', { sessionId: 's1' })

    expect(() => clickLatest()).not.toThrow()
    expect(h.sent).toHaveLength(0)
  })

  it('does not throw for a notification with no navigation payload', () => {
    markRendererReady()
    showTerminalNotification('plain', 'body')
    expect(() => clickLatest()).not.toThrow()
    expect(h.sent).toHaveLength(0)
  })
})
