import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

/**
 * The two timers the viewer page owns: the negotiation watchdog and the
 * connection-quality poll.
 *
 * Both are invisible in the markup until they misfire, which is exactly what
 * happened:
 *
 *  1. Negotiation has no failure frame. If the publisher cannot answer (an
 *     m-line layout from a different bundle generation, an SDP the browser
 *     refuses) the viewer spun on "协商中…" forever — no error, no retry, and a
 *     reload usually reproduced it.
 *  2. The stats interval was only cleared on unmount, so a viewer that stayed
 *     on the ended screen kept polling a closed peer connection every 3s.
 */

type Cbs = Record<string, (v?: unknown) => void>
let lastViewer: { opts?: { callbacks?: Cbs } } | null = null

const { closeSpy, statsSpy } = vi.hoisted(() => ({
  closeSpy: vi.fn(),
  statsSpy: vi.fn(async () => ({ rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null })),
}))

vi.mock('@/share/liveViewer', () => ({
  LiveViewer: class {
    opts: { callbacks?: Cbs }
    viewerId = 'v-test'
    constructor(opts: { callbacks?: Cbs }) {
      this.opts = opts
      lastViewer = this as never
    }
    connect(): void {}
    close = closeSpy
    sendControl(): boolean {
      return true
    }
    getStats = statsSpy
  },
}))
vi.mock('@/share/shareMode', () => ({ getShareToken: () => 'tok' }))

import LiveShareView from '@/share/LiveShareView.vue'
import { passJoinGate } from './joinGateTestHelper'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { close: '关闭' },
      share: {
        live: {
          title: '直播',
          joinSubtitle: '主持人邀请你加入会议。',
          joinNick: '你的称谓',
          joinNickPlaceholder: '例如：小明',
          joinNickHint: '称谓会记住。',
          joinEnter: '加入会议',
          statusConnecting: '连接中…',
          statusNegotiating: '协商中…',
          statusLive: '直播中',
          statusEnded: '已结束',
          statusFailed: '连接失败',
          failedTitle: '连接失败',
          failedDesc: '请检查网络',
          failedIceDesc: '无法建立媒体连接',
          retry: '重试',
          noPictureTitle: '暂未共享画面',
          noPictureDesc: '主持人可以随时打开屏幕共享',
          chat: '聊天',
          chatTitle: '聊天',
          chatEmpty: '还没有消息',
          chatPlaceholder: '说点什么…',
          chatSend: '发送',
          micToggle: '麦克风',
          micOn: '麦克风已开',
          micOff: '麦克风已关',
          micOnHint: '其他人可以听到你说话',
          micOffHint: '点击打开麦克风发言',
          enableSound: '开启声音',
          fullscreen: '全屏',
          exitFullscreen: '退出全屏',
          viewerName: '观众 {id}',
          hostRole: '主持人',
          participants: '成员',
          qualityGood: '流畅',
          qualityFair: '一般',
          qualityPoor: '差',
        },
      },
    },
  },
})

beforeEach(() => {
  vi.useFakeTimers()
  lastViewer = null
  closeSpy.mockClear()
  statsSpy.mockClear()
  Object.defineProperty(HTMLMediaElement.prototype, 'play', {
    configurable: true,
    writable: true,
    value: vi.fn(async () => undefined),
  })
})

afterEach(() => {
  vi.useRealTimers()
  // @ts-expect-error -- jsdom has no real implementation to restore to.
  delete HTMLMediaElement.prototype.play
})

/** Mount, pass the gate, and let the viewer be constructed. */
async function mountView() {
  const w = mount(LiveShareView, { global: { plugins: [i18n] } })
  passJoinGate(w)
  await vi.advanceTimersByTimeAsync(0)
  return w
}

describe('negotiation watchdog', () => {
  /**
   * The watchdog is armed explicitly in `start()` — not only from the state
   * watcher — because `connecting` is the value `state` was CREATED with, so no
   * watcher callback ever fires for it. Without that line the most common stuck
   * case (a socket that never opens) waits forever, and every other test here
   * still passes: they all push a state change first, which arms it through the
   * watcher instead.
   */
  it('gives up on a negotiation that never even starts', async () => {
    const w = await mountView()
    // No callback at all: this is the state right after joining, before the
    // socket has reported anything.
    expect(w.find('.live-overlay-desc').text()).toContain('连接中')

    await vi.advanceTimersByTimeAsync(15_000)
    await vi.advanceTimersByTimeAsync(0)

    expect(closeSpy).toHaveBeenCalled()
    expect(w.find('.live-overlay-title').text()).toContain('连接失败')
    expect(w.find('.live-retry').exists()).toBe(true)
    w.unmount()
  })

  it('gives up on an answer that never comes, with copy and a retry', async () => {
    const w = await mountView()
    const first = lastViewer
    lastViewer?.opts?.callbacks?.onStateChange?.('negotiating')
    await vi.advanceTimersByTimeAsync(0)
    expect(w.find('.live-overlay-desc').text()).toContain('协商中')

    // One second short of the deadline: still waiting, not failed yet.
    await vi.advanceTimersByTimeAsync(14_999)
    expect(w.find('.live-retry').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(1)
    await vi.advanceTimersByTimeAsync(0)

    // The half-open session is released, not left dialling behind the overlay.
    expect(closeSpy).toHaveBeenCalled()
    expect(w.find('.live-overlay-title').text()).toContain('连接失败')
    // "The peer link was never built", not "check your network": the socket was
    // fine, so the generic advice would send the viewer looking in the wrong
    // place.
    expect(w.find('.live-overlay-desc').text()).toContain('无法建立媒体连接')
    expect(w.find('.live-retry').exists()).toBe(true)

    // The retry has to actually re-dial: a dead viewer kept in place would put
    // the page back on the same stuck screen.
    await w.find('.live-retry').trigger('click')
    await vi.advanceTimersByTimeAsync(0)
    expect(lastViewer).not.toBe(first)
    expect(w.find('.live-overlay-title').exists()).toBe(false)

    w.unmount()
  })

  it('does not fire once the picture is playing', async () => {
    const w = await mountView()
    lastViewer?.opts?.callbacks?.onStateChange?.('negotiating')
    await vi.advanceTimersByTimeAsync(0)

    lastViewer?.opts?.callbacks?.onStateChange?.('playing')
    await vi.advanceTimersByTimeAsync(60_000)

    // A viewer watching for a minute must not be thrown into "failed" because
    // the watchdog was left armed.
    expect(w.find('.live-overlay-title').exists()).toBe(false)
    expect(closeSpy).not.toHaveBeenCalled()
    w.unmount()
  })

  it('does not fire while the viewer is still at the nickname gate', async () => {
    // No join yet, so no socket and nothing to time out. A watchdog armed at
    // setup would fail the gate screen while the viewer is still typing.
    const w = mount(LiveShareView, { global: { plugins: [i18n] } })
    await vi.advanceTimersByTimeAsync(60_000)
    expect(w.find('[data-role="join-nick"]').exists()).toBe(true)
    expect(w.find('.live-overlay-title').exists()).toBe(false)
    w.unmount()
  })
})

describe('quality polling', () => {
  it('stops polling once the picture is gone', async () => {
    const w = await mountView()
    lastViewer?.opts?.callbacks?.onStateChange?.('playing')
    await vi.advanceTimersByTimeAsync(0)

    await vi.advanceTimersByTimeAsync(3_000)
    expect(statsSpy).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(3_000)
    expect(statsSpy).toHaveBeenCalledTimes(2)

    lastViewer?.opts?.callbacks?.onStateChange?.('ended')
    await vi.advanceTimersByTimeAsync(0)
    const afterEnd = statsSpy.mock.calls.length

    await vi.advanceTimersByTimeAsync(30_000)
    expect(statsSpy.mock.calls.length).toBe(afterEnd)
    // The interval itself is gone, not merely short-circuited inside its
    // callback — otherwise it keeps waking up for as long as the page is open.
    expect(vi.getTimerCount()).toBe(0)

    w.unmount()
  })

  it('leaves no timer behind on unmount', async () => {
    const w = await mountView()
    lastViewer?.opts?.callbacks?.onStateChange?.('playing')
    await vi.advanceTimersByTimeAsync(3_000)

    w.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
