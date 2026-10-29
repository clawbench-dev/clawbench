import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

/**
 * Viewer state machine → UI, and the microphone toggle.
 *
 * The dock test covers layout/controls; this one covers what the overlay SAYS
 * and what the mic does when the server refuses. Both are invisible from the
 * markup: `overlayDesc` branches on a failure reason that only arrives through
 * a callback, and the mic is optimistic (it flips before the server answers).
 */

type Cbs = Record<string, (v?: unknown) => void>
let lastViewer: { opts?: { callbacks?: Cbs } } | null = null
let startTalkingResult = true

vi.mock('@/share/liveViewer', () => ({
  LiveViewer: class {
    opts: { callbacks?: Cbs }
    constructor(opts: { callbacks?: Cbs }) {
      this.opts = opts
      lastViewer = this as never
    }
    connect(): void {}
    close(): void {}
    startTalking(): Promise<boolean> {
      return Promise.resolve(startTalkingResult)
    }
    stopTalking(): Promise<void> {
      return Promise.resolve()
    }
  },
}))
let shareToken: string | null = 'tok'
vi.mock('@/share/shareMode', () => ({ getShareToken: () => shareToken }))

import LiveShareView from '@/share/LiveShareView.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      share: {
        live: {
          title: '直播',
          micToggle: '麦克风',
          micOn: '麦克风已开',
          micOff: '麦克风已关',
          micOnHint: '其他人可以听到你说话',
          micOffHint: '点击打开麦克风发言',
          statusConnecting: '连接中…',
          statusLive: '直播中',
          statusNegotiating: '协商中…',
          statusEnded: '已结束',
          statusFull: '房间已满',
          statusFailed: '连接失败',
          endedTitle: '直播已结束',
          endedDesc: '发布者已停止共享',
          fullTitle: '房间已满',
          fullDesc: '稍后再试',
          failedTitle: '连接失败',
          failedDesc: '请检查网络',
          failedIceDesc: '无法建立媒体连接',
          retry: '重试',
          enableSound: '开启声音',
          fullscreen: '全屏',
          exitFullscreen: '退出全屏',
        },
      },
    },
  },
})

beforeEach(() => {
  lastViewer = null
  startTalkingResult = true
  shareToken = 'tok'
  Object.defineProperty(HTMLMediaElement.prototype, 'play', {
    configurable: true,
    writable: true,
    value: vi.fn(async () => undefined),
  })
})

afterEach(() => {
  // @ts-expect-error -- jsdom has no real implementation to restore to.
  delete HTMLMediaElement.prototype.play
})

function mountView() {
  return mount(LiveShareView, { global: { plugins: [i18n] } })
}

describe('LiveShareView overlay states', () => {
  it('fails immediately when the URL carries no share token', async () => {
    shareToken = null
    const w = mountView()
    await flushPromises()

    // Nothing to dial: the viewer must not be constructed at all, otherwise it
    // would open a socket to /share/null.
    expect(lastViewer).toBeNull()
    expect(w.find('.live-overlay-title').text()).toContain('连接失败')
    // No reason was reported, so the generic copy applies — not the ICE one.
    expect(w.find('.live-overlay-desc').text()).toContain('请检查网络')
    w.unmount()
  })

  it('shows the status text while negotiating', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStateChange?.('negotiating')
    await flushPromises()

    expect(w.find('.live-overlay-desc').text()).toContain('协商中')
    w.unmount()
  })

  it('shows the ended copy and a retry button when the publisher stops', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStateChange?.('ended')
    await flushPromises()

    expect(w.find('.live-overlay-title').text()).toContain('直播已结束')
    expect(w.find('.live-overlay-desc').text()).toContain('发布者已停止共享')
    expect(w.find('.live-retry').exists()).toBe(true)
    w.unmount()
  })

  it('shows the room-full copy', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStateChange?.('full')
    await flushPromises()

    expect(w.find('.live-overlay-title').text()).toContain('房间已满')
    expect(w.find('.live-overlay-desc').text()).toContain('稍后再试')
    w.unmount()
  })

  it('uses the ICE-specific description when the failure reason is `ice`', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStateChange?.('failed', { reason: 'ice' })
    await flushPromises()

    // A generic "check your network" is wrong here: the socket worked, the
    // media path did not — the two need different advice.
    expect(w.find('.live-overlay-desc').text()).toContain('无法建立媒体连接')
    w.unmount()
  })

  it('falls back to the generic description for any other failure reason', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStateChange?.('failed', { reason: 'signal' })
    await flushPromises()

    expect(w.find('.live-overlay-desc').text()).toContain('请检查网络')
    w.unmount()
  })

  it('retry tears the old viewer down before dialling again', async () => {
    const w = mountView()
    await flushPromises()
    const first = lastViewer

    lastViewer?.opts?.callbacks?.onStateChange?.('ended')
    await flushPromises()
    await w.find('.live-retry').trigger('click')
    await flushPromises()

    // A new viewer instance means `connect()` ran again; reusing the dead one
    // would leave the viewer on the ended screen.
    expect(lastViewer).not.toBe(first)
    expect(w.find('.live-overlay-title').exists()).toBe(false)
    w.unmount()
  })

  it('clears the talking state when the stream ends', async () => {
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onTalkEnabled?.(true)
    await flushPromises()

    await w.find('[data-action="mic-toggle"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-action="mic-toggle"]').attributes('aria-checked')).toBe('true')

    lastViewer?.opts?.callbacks?.onStateChange?.('ended')
    await flushPromises()

    // The button survives (the room switch is still on), but it must no longer
    // claim to be sending: an ended stream cannot carry audio.
    const toggle = w.find('[data-action="mic-toggle"]')
    expect(toggle.exists()).toBe(true)
    expect(toggle.attributes('aria-checked')).toBe('false')
    w.unmount()
  })
})

describe('LiveShareView status copy table', () => {
  // Every state must map to its OWN string. An empty or shared string would
  // leave the viewer looking at "连接中…" after the publisher ended.
  const statusKeys: Array<[string, string]> = [
    ['playing', '直播中'],
    ['connecting', '连接中'],
    ['negotiating', '协商中'],
    ['ended', '已结束'],
    ['full', '房间已满'],
    ['failed', '连接失败'],
  ]

  it.each(statusKeys)('reports a distinct status line for %s', async (state, expected) => {
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onStateChange?.(state)
    await flushPromises()

    const text = (w.vm as any).$.setupState.statusText
    expect(text).toContain(expected)
    w.unmount()
  })

  it('has no overlay title/description for the non-terminal states', async () => {
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onStateChange?.('playing')
    await flushPromises()

    const st = (w.vm as any).$.setupState
    // A stale title from a previous state would sit on top of a playing video.
    expect(st.overlayTitle).toBe('')
    expect(st.overlayDesc).toBe('')
    w.unmount()
  })
})

describe('LiveShareView playback failures', () => {
  it('keeps the stream attached when autoplay is refused', async () => {
    ;(HTMLMediaElement.prototype.play as unknown as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error('NotAllowedError'),
    )
    const w = mountView()
    await flushPromises()

    const stream = { getAudioTracks: () => [] } as unknown as MediaStream
    lastViewer?.opts?.callbacks?.onStream?.(stream)
    await flushPromises()

    // Autoplay refusal is not a stream failure: the element still holds the
    // stream, so the "enable sound" gesture can start it later.
    expect((w.find('video').element as HTMLVideoElement).srcObject).toBe(stream)
    w.unmount()
  })
})

describe('LiveShareView microphone toggle', () => {
  it('flips the button optimistically, then reverts when the server refuses', async () => {
    startTalkingResult = false
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onTalkEnabled?.(true)
    await flushPromises()

    await w.find('[data-action="mic-toggle"]').trigger('click')
    await flushPromises()

    // The room switch was off: the optimistic "on" must not stick, or the
    // viewer believes it is being heard while nothing is sent.
    expect(w.find('[data-action="mic-toggle"]').attributes('aria-checked')).toBe('false')
    w.unmount()
  })

  it('turns the mic off on a second click', async () => {
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onTalkEnabled?.(true)
    await flushPromises()

    await w.find('[data-action="mic-toggle"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-action="mic-toggle"]').attributes('aria-checked')).toBe('true')

    await w.find('[data-action="mic-toggle"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-action="mic-toggle"]').attributes('aria-checked')).toBe('false')
    w.unmount()
  })

  it('reverts the button when the publisher revokes speech', async () => {
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onTalkEnabled?.(true)
    await flushPromises()

    await w.find('[data-action="mic-toggle"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-action="mic-toggle"]').attributes('aria-checked')).toBe('true')

    lastViewer?.opts?.callbacks?.onTalkRevoked?.()
    await flushPromises()

    expect(w.find('[data-action="mic-toggle"]').attributes('aria-checked')).toBe('false')
    w.unmount()
  })
})

describe('LiveShareView sound unlock', () => {
  it('does not prompt for sound on a silent stream', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStream?.({
      getAudioTracks: () => [],
    } as unknown as MediaStream)
    await flushPromises()

    expect(w.find('[data-action="enable-sound"]').exists()).toBe(false)
    w.unmount()
  })

  it('unmutes the element when sound is unlocked', async () => {
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onStream?.({
      getAudioTracks: () => [{}],
    } as unknown as MediaStream)
    await flushPromises()

    const video = w.find('video').element as HTMLVideoElement
    expect(video.muted).toBe(true)

    await w.find('[data-action="enable-sound"]').trigger('click')
    await flushPromises()

    expect(video.muted).toBe(false)
    w.unmount()
  })
})
