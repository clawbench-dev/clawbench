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
    // The component calls these on `playing`; without them the watcher throws
    // and the failure is swallowed by Vue.
    viewerId = 'v-test'
    sendControl(): boolean {
      return true
    }
    getStats() {
      return Promise.resolve({ rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null })
    }
  },
}))
let shareToken: string | null = 'tok'
vi.mock('@/share/shareMode', () => ({
  getShareToken: () => shareToken,
  isShareMode: () => shareToken !== null && shareToken !== '',
}))

import LiveShareView from '@/share/LiveShareView.vue'
import { passJoinGate } from './joinGateTestHelper'
import { readWebFile } from '@/testUtils/readWebFile'

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
          joinSubtitle: '主持人邀请你加入会议。填一个称谓，发布端与其他观众会看到它。',
          joinNick: '你的称谓',
          joinNickPlaceholder: '例如：小明',
          joinNickHint: '称谓会记住，下次加入不用再填。',
          joinEnter: '加入会议',
          nameTaken: '该称谓已被会议中的其他人使用，请换一个。',
          noPictureTitle: '暂未共享画面',
          noPictureDesc: '主持人可以随时打开屏幕共享或摄像头',
          statusLive: '直播中',
          statusNegotiating: '协商中…',
          statusEnded: '已结束',
          statusFull: '房间已满',
          statusFailed: '连接失败',
          endedTitle: '直播已结束',
          endedDesc: '发布者已停止共享',
          kickedTitle: '已被移出会议',
          kickedDesc: '主持人已把你移出这场会议',
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
  // The microphone switch is gated on getUserMedia being reachable, so these
  // tests need a viewer that can actually talk. The gate itself is covered by
  // LiveShareView.dock.test.ts; here it would only stop every mic test from
  // reaching the state machine they are about.
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia: vi.fn() },
  })
})

afterEach(() => {
  // @ts-expect-error -- jsdom has no real implementation to restore to.
  delete HTMLMediaElement.prototype.play
  // Undo the microphone-capability override from the talking-state test: a
  // leftover `mediaDevices` would decide the gate for every later test here.
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: undefined })
})

function mountView() {
  const w = mount(LiveShareView, { global: { plugins: [i18n] } })
  passJoinGate(w)
  return w
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

  it('switches to the meeting record when the publisher stops', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStateChange?.('ended')
    await flushPromises()

    // The room is gone but the record is permanent: the member lands on the
    // same four surfaces the host sees, not on a dead "share ended" card.
    expect(w.find('.share-meeting').exists()).toBe(true)
    expect(w.find('.live-overlay-title').exists()).toBe(false)
    w.unmount()
  })

  it('opens the record straight away when the link is opened after the meeting', async () => {
    // `meta.ended` is what the dispatcher resolved before mounting: no nickname
    // gate (there is no room to join) and no socket.
    const w = mount(LiveShareView, {
      props: { meta: { ended: true, title: 'Release review' } },
      global: { plugins: [i18n] },
    })
    await flushPromises()

    expect(w.find('.join-gate').exists()).toBe(false)
    expect(lastViewer).toBeNull()
    expect(w.find('.share-meeting').exists()).toBe(true)
    expect(w.text()).toContain('Release review')
    w.unmount()
  })

  it('keeps the overlay when the viewer was removed from the room', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStateChange?.('ended', { reason: 'kicked' })
    await flushPromises()

    // Being removed is not the meeting ending: the copy has to say which one
    // happened, and the record is not the answer to "why am I out".
    expect(w.find('.live-overlay-title').text()).toContain('已被移出会议')
    expect(w.find('.share-meeting').exists()).toBe(false)
    w.unmount()
  })

  it('returns to the name field when the room refuses a duplicate name', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onStateChange?.('name-taken')
    await flushPromises()

    // The fix is another name, not a retry: the gate comes back with the reason
    // under the field and the refused name still there to edit. A terminal
    // overlay would offer a retry that fails identically.
    expect(w.find('[data-role="join-nick"]').exists()).toBe(true)
    expect((w.find('[data-role="join-nick"]').element as HTMLInputElement).value).toBe('Tester')
    expect(w.find('[data-role="join-error"]').text()).toContain('该称谓已被会议中的其他人使用')
    expect(w.find('.live-overlay-title').exists()).toBe(false)
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

    lastViewer?.opts?.callbacks?.onStateChange?.('failed', { reason: 'ice' })
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

    // A transport failure, not the meeting ending: `ended` now replaces the
    // stage with the record, so the button would simply be gone.
    lastViewer?.opts?.callbacks?.onStateChange?.('failed', { reason: 'ice' })
    await flushPromises()

    // The button survives (the room switch is still on), but it must no longer
    // claim to be sending: a dead stream cannot carry audio.
    const toggle = w.find('[data-action="mic-toggle"]')
    expect(toggle.exists()).toBe(true)
    expect(toggle.attributes('aria-checked')).toBe('false')
    w.unmount()
  })
})

/**
 * The "no picture yet" placeholder and the status overlay are ONE slot: both are
 * `inset: 0` and centre their content, so whichever pair of conditions is true
 * renders two sets of copy on top of each other.
 *
 * The realistic sequence that produced the bug: the host stops its picture
 * (`picture-state { video: false }` leaves `videoEnabled` false) and then the
 * media path fails. Nothing rolls `videoEnabled` back, so "not sharing yet" and
 * "connection failed" were both true and their titles/descriptions interleaved.
 *
 * (`ended` is covered separately: the meeting ending now replaces the whole
 * stage with the record, so there is no overlay left to collide with.)
 */
describe('LiveShareView overlay slot exclusivity', () => {
  it('hides the no-picture placeholder whenever a status overlay is up', async () => {
    const w = mountView()
    await flushPromises()
    const cb = lastViewer?.opts?.callbacks ?? {}

    cb.onStateChange?.('playing')
    cb.onVideoMode?.(false)
    await flushPromises()
    expect(w.find('[data-role="no-picture"]').exists()).toBe(true)

    // Host stops sharing, then the peer link fails.
    cb.onStateChange?.('failed', { reason: 'ice' })
    await flushPromises()
    expect(w.find('[data-role="no-picture"]').exists()).toBe(false)
    expect(w.find('.live-overlay-title').text()).toContain('连接失败')

    // Same for a retry that lands back in `connecting`.
    await w.find('.live-retry').trigger('click')
    await flushPromises()
    expect(w.find('[data-role="no-picture"]').exists()).toBe(false)

    // ...and it comes back once the meeting is live again with no picture.
    cb.onStateChange?.('playing')
    await flushPromises()
    expect(w.find('[data-role="no-picture"]').exists()).toBe(true)

    w.unmount()
  })

  it('paints the status overlay opaquely so nothing underneath can bleed through', () => {
    // Belt and braces for the same defect: even if some future block lands in
    // this slot, the overlay must not be see-through. A transparent overlay was
    // half the bug — the copy interleaved instead of one covering the other.
    const src = readWebFile('src/share/LiveShareView.vue')
    const style = src.match(/<style scoped>([\s\S]*)<\/style>/)?.[1]
    expect(style).toBeTruthy()

    const rule = (style as string)
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .match(/(^|\})\s*\.live-overlay\s*\{([^}]*)\}/)
    expect(rule).not.toBeNull()
    expect((rule as RegExpMatchArray)[2]).toMatch(/(^|;)\s*background:\s*var\(--bg-primary/)
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

  it('does not ask again after a re-attach, when the sound is already on', async () => {
    const w = mountView()
    await flushPromises()
    const audioStream = { getAudioTracks: () => [{}] } as unknown as MediaStream
    lastViewer?.opts?.callbacks?.onStream?.(audioStream)
    await flushPromises()

    await w.find('[data-action="enable-sound"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-action="enable-sound"]').exists()).toBe(false)

    // The publisher swapping its picture source re-attaches a stream, and the
    // prompt used to come back on "this stream has audio" alone — over audio
    // that is already playing, so pressing it changed nothing.
    lastViewer?.opts?.callbacks?.onStream?.(audioStream)
    await flushPromises()

    expect(w.find('[data-action="enable-sound"]').exists()).toBe(false)
    expect((w.find('video').element as HTMLVideoElement).muted).toBe(false)
    w.unmount()
  })
})

/**
 * Chat is relayed to everybody and never stored on the viewer's side, so the
 * only bound on it is this one. Without it a long meeting — or one attendee
 * sending frames in a loop — grows memory and per-frame render cost until a
 * phone stalls.
 */
describe('LiveShareView chat bounds', () => {
  // 600 frames each re-render the mounted panel, so this case is deterministic
  // but heavy: under full-suite load it outran the 5s default and failed with a
  // timeout that said nothing about the bound it is testing.
  it('keeps only the newest lines when the meeting floods the chat', { timeout: 30000 }, async () => {
    const w = mountView()
    await flushPromises()
    const cb = lastViewer?.opts?.callbacks ?? {}

    for (let i = 0; i < 600; i++) {
      cb.onControl?.({
        t: 'chat',
        id: `m${i}`,
        from: 'v2',
        name: '小明',
        text: `第 ${i} 句`,
        at: i,
      })
    }
    await flushPromises()

    const msgs = (w.vm as unknown as { $: { setupState: { chatMessages: Array<{ id: string }> } } }).$.setupState
      .chatMessages
    expect(msgs).toHaveLength(500)
    // The newest 500, in order: the reader is following the conversation, not
    // its beginning.
    expect(msgs[0].id).toBe('m100')
    expect(msgs[499].id).toBe('m599')
    w.unmount()
  })
})

describe('LiveShareView remote-control grant addressing', () => {
  it('ignores a grant with no viewer id instead of granting everybody', async () => {
    const w = mountView()
    await flushPromises()
    const cb = lastViewer?.opts?.callbacks ?? {}

    // A grant is addressed to ONE viewer. An empty id is a malformed frame, not
    // a broadcast: treating it as one put every viewer into "controlling", and
    // their keydown handler then swallowed typing in the chat box.
    cb.onControl?.({ t: 'ctrl-grant', viewerId: '', granted: true })
    await flushPromises()

    expect(w.find('[data-role="control-banner"]').exists()).toBe(false)
    w.unmount()
  })

  it('still honours a grant addressed to this viewer', async () => {
    const w = mountView()
    await flushPromises()
    const cb = lastViewer?.opts?.callbacks ?? {}

    cb.onControl?.({ t: 'ctrl-grant', viewerId: 'v-test', granted: true })
    await flushPromises()

    expect(w.find('[data-role="control-banner"]').exists()).toBe(true)
    w.unmount()
  })
})
