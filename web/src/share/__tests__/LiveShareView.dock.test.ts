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
  },
}))
vi.mock('@/share/shareMode', () => ({ getShareToken: () => 'tok' }))

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
          fullscreen: '全屏',
          exitFullscreen: '退出全屏',
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
})

function mountView() {
  return mount(LiveShareView, { global: { plugins: [i18n] } })
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
})
