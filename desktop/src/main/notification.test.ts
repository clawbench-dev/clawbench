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
    closeHandler: { current: null as null | (() => void) },
    closeMock: vi.fn(),
    sent: [] as Array<{ channel: string; nav: unknown }>,
    winState: { minimized: false, visible: true, loading: false, focused: false, destroyed: false },
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
      if (event === 'close') h.closeHandler.current = cb
      return this
    }
    show() { h.showMock() }
    close() { h.closeMock() }
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
        isFocused: () => h.winState.focused,
        isDestroyed: () => h.winState.destroyed,
        restore: () => { h.calls.restore++; h.winState.minimized = false },
        show: () => { h.calls.show++; h.winState.visible = true },
        focus: () => { h.calls.focus++ },
      }
    : null),
}))

import {
  showTerminalNotification,
  dismissTerminalNotification,
  getPendingNavigationJson,
  _resetActiveNotificationsForTesting,
} from './notification'
import { markRendererReady, markRendererLoading, resetRendererReady } from './navReady'
import { NAV_CHANNELS, WINDOW_STATE_CHANNEL, PORT_REBOUND_CHANNEL } from '../shared/types'

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

  it('mirrors the window-state channel literal', () => {
    // Same constraint as NAV_CHANNELS: the preload cannot import shared/types,
    // so it hard-codes the channel name. A rename on one side only would make
    // the maximize glyph stop updating with no error anywhere.
    expect(preloadSrc).toContain(`'${WINDOW_STATE_CHANNEL}'`)
  })

  it('mirrors the port-rebound channel literal', () => {
    // Same constraint again: a rename on one side only would silently drop the
    // reconnect drift signal, leaving the server registry keyed to a dead port.
    expect(preloadSrc).toContain(`'${PORT_REBOUND_CHANNEL}'`)
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
    h.closeHandler.current = null
    h.showMock.mockReset()
    h.closeMock.mockReset()
    h.windowAvailable = true
    h.winState.minimized = false
    h.winState.loading = false
    // Default to a window that is NOT on screen: these cases are about
    // notification delivery and click routing, and a visible window now
    // suppresses the notification entirely. The suppression cases below set
    // visibility explicitly.
    h.winState.visible = false
    h.winState.focused = false
    h.winState.destroyed = false
    h.calls.restore = 0
    h.calls.show = 0
    h.calls.focus = 0
    resetRendererReady()
    _resetActiveNotificationsForTesting()
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

  it('carries the forge item target through to the renderer', () => {
    // The click must deep-link to the ITEM, not just raise the tab. The target
    // is opaque data to the main process — it is forwarded verbatim.
    markRendererReady()
    showTerminalNotification('owner/repo · 合并请求 #42 · 已合并', 'title', {
      projectPath: '/p',
      forge: true,
      forgeTarget: { type: 'pr', number: 42, runId: 0, itemKey: 'pr/42', projectPath: '/p' },
    })
    clickLatest()

    expect(h.sent[0].channel).toBe('clawbench-open-forge')
    expect((h.sent[0].nav as NotificationNav).forgeTarget).toEqual({
      type: 'pr', number: 42, runId: 0, itemKey: 'pr/42', projectPath: '/p',
    })
  })

  it('carries a pipeline target keyed by its run id', () => {
    // A pipeline's number is always 0, so the run id is the only identity —
    // losing it would leave the click unable to open (or mark read) the run.
    markRendererReady()
    showTerminalNotification('owner/repo · 仓库流水线 · 流水线完成', 'title', {
      projectPath: '/p',
      forge: true,
      forgeTarget: { type: 'pipeline', number: 0, runId: 555, itemKey: 'pipeline/run:555', projectPath: '/p' },
    })
    clickLatest()

    const nav = h.sent[0].nav as NotificationNav
    expect(nav.forgeTarget?.itemKey).toBe('pipeline/run:555')
    expect(nav.forgeTarget?.runId).toBe(555)
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

  // ── 可见即抑制：窗口开着就不弹系统通知 ──

  it('suppresses the notification when the window is visible and focused', () => {
    // 用户正看着 ClawBench，系统通知只会重复应用内完成卡片已经展示的内容。
    markRendererReady()
    h.winState.visible = true
    h.winState.minimized = false
    h.winState.focused = true

    showTerminalNotification('done', 'body', { sessionId: 's1' })

    expect(h.showMock).not.toHaveBeenCalled()
    expect(h.clickHandler.current).toBeNull()
  })

  it('also suppresses when the window is visible but NOT focused', () => {
    // 判定只看可见性、不看焦点：窗口开在副屏或被别的应用盖住时，用户仍是
    // "开着这个应用"，弹系统通知反而打扰他正在做的事。若按焦点判定，随手点
    // 一下别的窗口就会开始收到通知，行为会变得难以预期。
    markRendererReady()
    h.winState.visible = true
    h.winState.minimized = false
    h.winState.focused = false

    showTerminalNotification('done', 'body', { sessionId: 's1' })

    expect(h.showMock).not.toHaveBeenCalled()
    expect(h.clickHandler.current).toBeNull()
  })

  it('still notifies when the window is minimized, even if it reports focus', () => {
    // 最小化 = 屏幕上没有它，用户不可能看到任何东西。渲染层的
    // document.hasFocus() 在这种状态下仍可能为真，所以判定必须看主进程的
    // 窗口状态而不是信任渲染层。
    markRendererReady()
    h.winState.minimized = true
    h.winState.visible = false
    h.winState.focused = true

    showTerminalNotification('done', 'body', { sessionId: 's1' })

    expect(h.showMock).toHaveBeenCalledTimes(1)
  })

  it('still notifies when the window is hidden, even if it reports focus', () => {
    markRendererReady()
    h.winState.visible = false
    h.winState.minimized = false
    h.winState.focused = true

    showTerminalNotification('done', 'body', { sessionId: 's1' })

    expect(h.showMock).toHaveBeenCalledTimes(1)
  })

  it('still notifies when the window is destroyed (no window on screen)', () => {
    markRendererReady()
    h.winState.destroyed = true
    h.winState.visible = true

    showTerminalNotification('done', 'body', { sessionId: 's1' })

    expect(h.showMock).toHaveBeenCalledTimes(1)
  })

  it('does not treat a missing window as "user is watching"', () => {
    // 没有窗口可查时不能误判成"用户在看"而静默丢掉通知。
    markRendererReady()
    h.windowAvailable = false

    showTerminalNotification('done', 'body', { sessionId: 's1' })

    expect(h.showMock).toHaveBeenCalledTimes(1)
  })
})

// ── 已读后主动消除通知 ──
//
// 缺陷：通知只在用户点它自己时才消失；手动打开 App 读完后，旧通知仍留在通知
// 中心，再点会重复派发深链。修复=保留句柄 + dismissTerminalNotification。

describe('dismissTerminalNotification', () => {
  beforeEach(() => {
    h.sent.length = 0
    h.clickHandler.current = null
    h.closeHandler.current = null
    h.showMock.mockReset()
    h.closeMock.mockReset()
    h.windowAvailable = true
    h.winState.minimized = true
    h.winState.visible = false
    h.winState.destroyed = false
    resetRendererReady()
    markRendererReady()
    _resetActiveNotificationsForTesting()
  })

  it('closes the retained notification for a read session', () => {
    showTerminalNotification('done', 'body', { sessionId: 's1' })
    expect(h.showMock).toHaveBeenCalledTimes(1)

    dismissTerminalNotification(undefined, 's1')

    expect(h.closeMock).toHaveBeenCalledTimes(1)
  })

  it('closes the retained notification for a read task', () => {
    showTerminalNotification('done', 'body', { taskId: '7', executionId: 'e1' })

    dismissTerminalNotification('7', undefined)

    expect(h.closeMock).toHaveBeenCalledTimes(1)
  })

  it('a task notification is keyed by its task, not its session', () => {
    // A task notification carries BOTH ids; keying it by session would let a
    // later session read dismiss the wrong notification.
    showTerminalNotification('done', 'body', { taskId: '7', sessionId: 's1' })

    dismissTerminalNotification(undefined, 's1') // session read: must NOT match
    expect(h.closeMock).not.toHaveBeenCalled()

    dismissTerminalNotification('7', undefined) // task read: must match
    expect(h.closeMock).toHaveBeenCalledTimes(1)
  })

  it('is a no-op when nothing was shown for that subject', () => {
    dismissTerminalNotification('nonexistent', undefined)
    expect(h.closeMock).not.toHaveBeenCalled()
  })

  it('is a no-op with no ids', () => {
    showTerminalNotification('done', 'body', { sessionId: 's1' })
    dismissTerminalNotification(undefined, undefined)
    expect(h.closeMock).not.toHaveBeenCalled()
  })

  it('dismisses only once — a second dismiss is a no-op', () => {
    showTerminalNotification('done', 'body', { sessionId: 's1' })
    dismissTerminalNotification(undefined, 's1')
    dismissTerminalNotification(undefined, 's1')
    expect(h.closeMock).toHaveBeenCalledTimes(1)
  })

  it('forgets the handle when the OS closes the notification', () => {
    // If the user dismissed it themselves, the map must not keep a stale
    // handle (a later dismiss would close an unrelated, reused instance).
    showTerminalNotification('done', 'body', { sessionId: 's1' })
    h.closeHandler.current?.() // simulate the OS/user close event

    dismissTerminalNotification(undefined, 's1')
    expect(h.closeMock).not.toHaveBeenCalled()
  })

  it('replaces (closes) the previous notification for the same subject', () => {
    // A task emits `running` then `completed`; both map to the same key. Without
    // replace-by-key, BOTH stay in the tray and a dismiss only closes one.
    showTerminalNotification('started', 'body', { taskId: '7' })
    h.closeMock.mockClear()
    showTerminalNotification('done', 'body', { taskId: '7' })

    expect(h.closeMock).toHaveBeenCalledTimes(1) // the superseded one is closed

    dismissTerminalNotification('7', undefined)
    expect(h.closeMock).toHaveBeenCalledTimes(2) // and the current one on read
  })

  it('a superseded notification close does not evict its successor', () => {
    // The replaced instance's `close` event fires; it must not delete the key
    // now owned by the newer handle.
    showTerminalNotification('started', 'body', { taskId: '7' })
    const supersededClose = h.closeHandler.current
    showTerminalNotification('done', 'body', { taskId: '7' })

    supersededClose?.() // the old instance's close event arrives late

    dismissTerminalNotification('7', undefined)
    expect(h.closeMock).toHaveBeenCalled() // successor still dismissible
  })
})
