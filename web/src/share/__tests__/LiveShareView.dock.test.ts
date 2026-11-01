import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

/**
 * The viewer's control dock.
 *
 * Two behaviours matter and neither is visible from the markup alone:
 *   1. the fullscreen button acts on the ROOT element (so the dock survives),
 *      not on the <video> (which would trap the user with no way back);
 *   2. the icon follows the real browser state, including leaving fullscreen
 *      with Esc — which never reaches our click handler.
 */

// No real PeerConnection in jsdom.
/** Captured so a test can drive the viewer's callbacks (title, talk state). */
let lastViewer: { opts?: { callbacks?: Record<string, (v?: unknown) => void> } } | null = null

vi.mock('@/share/liveViewer', () => ({
  LiveViewer: class {
    opts: { callbacks?: Record<string, (v?: unknown) => void> }
    constructor(opts: { callbacks?: Record<string, (v?: unknown) => void> }) {
      this.opts = opts
      lastViewer = this as never
    }
    connect(): void {}
    close(): void {}
    startTalking(): Promise<boolean> {
      return Promise.resolve(true)
    }
    stopTalking(): Promise<void> {
      return Promise.resolve()
    }
    // Control channel + stats: the real viewer has these, and the component
    // now touches them on `playing`.
    viewerId = 'v-test'
    sendControl(): boolean {
      return true
    }
    getStats() {
      return Promise.resolve({ rttMs: null, lossPct: null, bitrateKbps: null, jitterMs: null })
    }
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
      share: {
        live: {
          title: '直播',
          micToggle: '麦克风',
          micOn: '麦克风已开',
          micOff: '麦克风已关',
          micOnHint: '其他人可以听到你说话',
          micOffHint: '点击打开麦克风发言',
          micNeedsHttps: '需要 HTTPS 才能使用麦克风',
          statusConnecting: '连接中…',
          joinSubtitle: '主持人邀请你加入会议。填一个昵称，发布端与其他观众会看到它。',
          joinNick: '你的昵称',
          joinNickPlaceholder: '例如：小明',
          joinNickHint: '昵称会记住，下次加入不用再填。',
          joinEnter: '加入会议',
          noPictureTitle: '暂未共享画面',
          noPictureDesc: '主持人可以随时打开屏幕共享或摄像头',
          statusLive: '直播中',
          fullscreen: '全屏',
          exitFullscreen: '退出全屏',
          notes: '听记',
          notesTitle: 'AI 听记',
          notesWaiting: '等待转写…',
          shareScreen: '共享屏幕',
          stopSharing: '停止共享',
          shareScreenHint: '把你的屏幕共享给所有人',
          stopSharingHint: '停止共享你的屏幕',
          youAreSharing: '你正在共享屏幕',
          viewingSharer: '成员 {id} 正在共享屏幕',
          backToHost: '看主持人',
          viewShared: '看共享',
          shareFailed: '无法开始共享',
          screenBusy: '已有成员在共享屏幕',
          screenDisabled: '主持人未开启屏幕共享',
          screenNeedsHttps: '需要 HTTPS',
        },
      },
    },
  },
})

let fullscreenEl: Element | null = null
let requestFullscreen: ReturnType<typeof vi.fn>
let exitFullscreen: ReturnType<typeof vi.fn>

beforeEach(() => {
  lastViewer = null
  fullscreenEl = null
  requestFullscreen = vi.fn(async () => undefined)
  exitFullscreen = vi.fn(async () => undefined)

  Object.defineProperty(HTMLElement.prototype, 'requestFullscreen', {
    configurable: true,
    writable: true,
    value: requestFullscreen,
  })
  Object.defineProperty(document, 'exitFullscreen', {
    configurable: true,
    writable: true,
    value: exitFullscreen,
  })
  Object.defineProperty(document, 'fullscreenElement', {
    configurable: true,
    get: () => fullscreenEl,
  })
  Object.defineProperty(HTMLMediaElement.prototype, 'play', {
    configurable: true,
    writable: true,
    value: vi.fn(async () => undefined),
  })
})

afterEach(() => {
  // @ts-expect-error -- jsdom has no real implementation to restore to.
  delete HTMLMediaElement.prototype.play
  // @ts-expect-error -- same.
  delete HTMLElement.prototype.requestFullscreen
  // @ts-expect-error -- same.
  delete document.exitFullscreen
  Object.defineProperty(document, 'fullscreenElement', {
    configurable: true,
    get: () => null,
  })
  // The microphone tests override both probes; leaving them overridden would
  // decide the gate for every later test in the file.
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: undefined })
})

function mountView() {
  const w = mount(LiveShareView, { global: { plugins: [i18n] } })
  passJoinGate(w)
  return w
}

describe('LiveShareView control dock', () => {
  it('offers fullscreen even when the publisher never allowed speech', async () => {
    const w = mountView()
    await flushPromises()

    // The dock is always mounted; the mic button is not. Fullscreen is useful
    // on its own, so gating the whole dock on `talkEnabled` would be wrong.
    expect(w.find('[data-action="fullscreen"]').exists()).toBe(true)
    expect(w.find('[data-action="mic-toggle"]').exists()).toBe(false)

    w.unmount()
  })

  it('requests fullscreen on the root element, not the video', async () => {
    const w = mountView()
    await flushPromises()

    await w.find('[data-action="fullscreen"]').trigger('click')
    await flushPromises()

    // The root, because the dock lives inside it: going fullscreen on the
    // <video> alone would hide the only way back out.
    expect(requestFullscreen).toHaveBeenCalledTimes(1)
    expect((requestFullscreen.mock.contexts[0] as Element).className).toContain('live-share-view')

    w.unmount()
  })

  it('exits fullscreen when already in it', async () => {
    const w = mountView()
    await flushPromises()
    fullscreenEl = w.element

    await w.find('[data-action="fullscreen"]').trigger('click')
    await flushPromises()

    expect(exitFullscreen).toHaveBeenCalledTimes(1)
    expect(requestFullscreen).not.toHaveBeenCalled()

    w.unmount()
  })

  it('follows fullscreen changes it did not initiate (Esc, F11)', async () => {
    const w = mountView()
    await flushPromises()

    expect(w.find('[data-action="fullscreen"]').text()).toContain('全屏')

    // Entering via the browser's own shortcut never runs our handler.
    fullscreenEl = w.element
    document.dispatchEvent(new Event('fullscreenchange'))
    await flushPromises()

    expect(w.find('[data-action="fullscreen"]').text()).toContain('退出全屏')

    fullscreenEl = null
    document.dispatchEvent(new Event('fullscreenchange'))
    await flushPromises()

    expect(w.find('[data-action="fullscreen"]').text()).toContain('全屏')

    w.unmount()
  })

  it('replaces the page header: the logo lives in the dock', async () => {
    const w = mountView()
    await flushPromises()

    // The 44px bar cost picture height and duplicated state the overlay already
    // reports; the brand moved into the dock instead.
    expect(w.find('.live-topbar').exists()).toBe(false)
    expect(w.find('.live-dock .live-dock-logo').exists()).toBe(true)

    w.unmount()
  })

  it('shows the connection state in the overlay, which the header used to do', async () => {
    const w = mountView()
    await flushPromises()

    // `connecting` is the initial state and has no title/description of its
    // own, so the status text is the only thing explaining the spinner.
    expect(w.find('.live-overlay-desc').text()).toContain('连接中')

    w.unmount()
  })

  it('mirrors the share title into the tab title', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onTitle?.('设计评审')
    await flushPromises()

    // Without the header, the tab is the only place the title still shows.
    expect(document.title).toBe('设计评审 · ClawBench')

    w.unmount()
  })

  it('marks the brand as live only while playing', async () => {
    const w = mountView()
    await flushPromises()

    expect(w.find('.live-dock-dot').exists()).toBe(false)

    lastViewer?.opts?.callbacks?.onStateChange?.('playing')
    await flushPromises()

    expect(w.find('.live-dock-dot').exists()).toBe(true)

    w.unmount()
  })

  it('drops the theme switch', async () => {
    const w = mountView()
    await flushPromises()

    // Removed deliberately: the page has one palette and a viewer cannot be
    // expected to theme someone else's share.
    expect(w.find('.share-theme-btn').exists()).toBe(false)

    w.unmount()
  })

  it('offers "enable sound" inside the dock, not floating over the picture', async () => {
    const w = mountView()
    await flushPromises()

    // A stream with audio is what raises the prompt.
    lastViewer?.opts?.callbacks?.onStream?.({
      getAudioTracks: () => [{}],
    } as unknown as MediaStream)
    await flushPromises()

    const btn = w.find('[data-action="enable-sound"]')
    expect(btn.exists()).toBe(true)
    // Same rule as the other controls: one dock, nothing else over the video.
    expect(btn.element.closest('.live-dock')).not.toBeNull()

    await btn.trigger('click')
    await flushPromises()

    expect(w.find('[data-action="enable-sound"]').exists()).toBe(false)

    w.unmount()
  })

  it('offers the transcript only when the publisher enabled AI 听记', async () => {
    const w = mountView()
    await flushPromises()

    // A share without 听记 must not show a button that opens an empty panel.
    expect(w.find('[data-action="notes"]').exists()).toBe(false)

    lastViewer?.opts?.callbacks?.onNotesEnabled?.(true)
    await flushPromises()
    expect(w.find('[data-action="notes"]').exists()).toBe(true)

    // Turning it off (or the share ending) removes the button again.
    lastViewer?.opts?.callbacks?.onNotesEnabled?.(false)
    await flushPromises()
    expect(w.find('[data-action="notes"]').exists()).toBe(false)

    w.unmount()
  })

  it('offers screen sharing only when the host allows it, disabled over http', async () => {
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-action="screen-share"]').exists()).toBe(false)

    lastViewer?.opts?.callbacks?.onScreenEnabled?.(true)
    await flushPromises()
    const btn = w.find('[data-action="screen-share"]')
    expect(btn.exists()).toBe(true)
    // jsdom is not a secure context and has no mediaDevices: exactly the
    // situation of an http:// LAN page. The button must explain, not vanish.
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.attributes('title')).toContain('HTTPS')

    w.unmount()
  })

  it('tells a browser user what only the client can do', async () => {
    const w = mountView()
    await flushPromises()

    lastViewer?.opts?.callbacks?.onScreenEnabled?.(true)
    await flushPromises()

    // jsdom has no capture API, which is exactly the plain-HTTP browser case:
    // the button stays disabled and a hint points at the desktop client.
    expect(w.find('[data-action="screen-share"]').attributes('disabled')).toBeDefined()
    const hint = w.find('[data-role="client-hint"]')
    expect(hint.exists()).toBe(true)

    await hint.find('.live-client-hint__close').trigger('click')
    expect(w.find('[data-role="client-hint"]').exists()).toBe(false)

    w.unmount()
  })

  it('stops listening after unmount', async () => {
    const w = mountView()
    await flushPromises()
    const remove = vi.spyOn(document, 'removeEventListener')

    w.unmount()

    expect(remove).toHaveBeenCalledWith('fullscreenchange', expect.any(Function))
  })

  it('leaves the layout alone when the browser refuses fullscreen', async () => {
    requestFullscreen.mockRejectedValueOnce(new Error('NotAllowedError'))
    const w = mountView()
    await flushPromises()

    await w.find('[data-action="fullscreen"]').trigger('click')
    await flushPromises()

    // A refusal must not leave the UI claiming to be fullscreen.
    expect(w.find('[data-action="fullscreen"]').text()).toContain('全屏')

    w.unmount()
  })

  it('disables the microphone switch when the page cannot reach getUserMedia', async () => {
    // A plain http://LAN page: `navigator.mediaDevices` does not exist, so
    // startTalking() can only fail. A live-looking switch that silently flips
    // back is worse than a disabled one that says why.
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: false })
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onTalkEnabled?.(true)
    await flushPromises()

    const btn = w.find('[data-action="mic-toggle"]')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.attributes('title')).toContain('HTTPS')

    w.unmount()
  })

  it('enables the microphone switch on a secure origin', async () => {
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn() },
    })
    const w = mountView()
    await flushPromises()
    lastViewer?.opts?.callbacks?.onTalkEnabled?.(true)
    await flushPromises()

    const btn = w.find('[data-action="mic-toggle"]')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeUndefined()
    expect(btn.attributes('title')).toBe('点击打开麦克风发言')

    w.unmount()
  })
})

/**
 * The publisher's camera as a picture-in-picture tile.
 *
 * The tile only exists when the meeting shows a screen AND a camera; a
 * camera-only meeting puts the camera on the main picture instead, and a
 * camera image must not offer remote control — there is no desktop behind it.
 */
describe('LiveShareView camera tile', () => {
  it('shows the camera tile next to a screen, and hides it when the camera goes off', async () => {
    const w = mountView()
    await flushPromises()
    const cb = lastViewer?.opts?.callbacks ?? {}

    // Default state: screen share, no camera — the element is mounted but not
    // shown, so a later stream needs no re-render to attach.
    expect(w.find('[data-role="camera-pip"]').attributes('style')).toContain('display: none')

    cb.onHostScreen?.(true)
    cb.onCameraMode?.(true)
    cb.onCameraStream?.({ id: 'cam' } as unknown as MediaStream)
    await flushPromises()

    expect(w.find('[data-role="camera-pip"]').attributes('style') ?? '').not.toContain('display: none')
    // The camera must never take over the main <video>: that element carries
    // the screen and the mixed audio.
    expect((w.find('video.live-video').element as HTMLVideoElement).srcObject).toBeFalsy()

    cb.onCameraMode?.(false)
    await flushPromises()
    expect(w.find('[data-role="camera-pip"]').attributes('style')).toContain('display: none')

    w.unmount()
  })

  it('keeps remote control off a camera-only meeting', async () => {
    const w = mountView()
    await flushPromises()
    const cb = lastViewer?.opts?.callbacks ?? {}

    cb.onHostScreen?.(false)
    await flushPromises()
    // A pointer mapped onto a camera image would inject clicks at nonsense
    // coordinates, so the button is not offered at all.
    expect(w.find('[data-action="request-control"]').exists()).toBe(false)

    cb.onHostScreen?.(true)
    await flushPromises()
    expect(w.find('[data-action="request-control"]').exists()).toBe(true)

    w.unmount()
  })

  it('says there is no picture instead of leaving a black rectangle', async () => {
    const w = mountView()
    await flushPromises()
    const cb = lastViewer?.opts?.callbacks ?? {}

    // Camera-only meeting with the camera switched off: no picture at all. The
    // placeholder does not name a kind of meeting — it says the host can turn
    // one on at any moment.
    cb.onVideoMode?.(false)
    cb.onHostScreen?.(false)
    cb.onCameraMode?.(false)
    await flushPromises()

    expect(w.find('[data-role="no-picture"]').exists()).toBe(true)
    expect(w.find('[data-role="camera-off"]').exists()).toBe(false)
    expect(w.find('[data-role="voice-stage"]').exists()).toBe(false)

    // The picture coming back mid-meeting removes it again.
    cb.onVideoMode?.(true)
    cb.onCameraMode?.(true)
    await flushPromises()
    expect(w.find('[data-role="no-picture"]').exists()).toBe(false)

    w.unmount()
  })
})
