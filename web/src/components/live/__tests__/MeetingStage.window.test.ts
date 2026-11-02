import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, shallowRef } from 'vue'
import enMessages from '@/i18n/locales/en'
import MeetingStage from '../MeetingStage.vue'

// Every mounted panel keeps watching the share, so a leftover one would open a
// window of its own as soon as the next test gives the mock a stream.
enableAutoUnmount(afterEach)

/**
 * The panel is a view: what it owns is WHICH surface each tool opens on.
 * Both the drawing surface and the member's screen must come up in their own
 * window (Document PiP) rather than inline — drawn inline the canvas is
 * squeezed into the panel and the strokes land somewhere else.
 */

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<typeof import('lucide-vue-next')>()
  const stub = (name: string) => ({ name, props: { size: Number }, render: () => null })
  // Spread the real module: a narrow mock turns any icon a shared child adds
  // (the source grid's check mark, the microphone list's) into `undefined` and
  // the whole panel fails to render.
  return {
    ...actual,
    Monitor: stub('Monitor'),
    MonitorUp: stub('MonitorUp'),
    MonitorOff: stub('MonitorOff'),
    Check: stub('Check'),
    Plus: stub('Plus'),
    Mic: stub('Mic'),
    NotebookPen: stub('NotebookPen'),
    Video: stub('Video'),
    Volume2: stub('Volume2'),
    X: stub('X'),
  }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const toastShow = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: toastShow }),
}))

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

const nativeBridge = vi.hoisted(() => ({ current: null as unknown }))
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => nativeBridge.current,
  // These tests run in a browser, not the desktop shell: no popup window.
  isDesktopApp: () => false,
}))

vi.mock('@/utils/remoteControlSupport', () => ({
  resolveRemoteControlSupport: async () => false,
}))

const shareApi = vi.hoisted(() => ({} as Record<string, unknown>))
vi.mock('@/composables/useLiveShare', async () => {
  const { ref: r, shallowRef: sr } = await import('vue')
  Object.assign(shareApi, {
    state: r('publishing'),
    token: r('tok-1'),
    shareUrl: r('https://h/share/tok-1'),
    shareLinks: r([{ label: 'LAN', url: 'https://h/share/tok-1' }]),
    title: r('demo'),
    viewers: r([{ id: 'v1', name: '', talk: 'muted' }]),
    talkEnabled: r(false),
    screenEnabled: r(true),
    screenSharer: r(''),
    remoteScreenStream: sr(null),
    error: r(''),
    levels: r({}),
    previewStream: sr({ id: 'preview' }),
    captureKind: r('screen'),
    captureSourceId: r(''),
    videoEnabled: r(true),
    cameraEnabled: r(false),
    previewCameraStream: sr(null),
    micEnabled: r(true),
    setMicEnabled: vi.fn(),
    setMicChoice: vi.fn(async () => true),
    activeMicDevice: r(''),
    setCameraDevice: vi.fn(async () => true),
    startScreen: vi.fn(async () => true),
    stopScreen: vi.fn(async () => undefined),
    startNotes: vi.fn(() => true),
    stopNotes: vi.fn(async () => undefined),
    start: vi.fn(async () => true),
    stop: vi.fn(async () => undefined),
    setTalkEnabled: vi.fn(),
    revokeTalk: vi.fn(),
    muteAll: vi.fn(),
    setScreenEnabled: vi.fn(),
    revokeScreen: vi.fn(),
    sendControl: vi.fn(),
    sendControlTo: vi.fn(),
    onControl: vi.fn(() => () => undefined),
    switchSource: vi.fn(async () => true),
  })
  return {
    useLiveShare: () => shareApi,
    listCaptureSources: vi.fn(async () => []),
    hasCameraDevice: vi.fn(async () => true),
    listAudioInputs: vi.fn(async () => []),
    listVideoInputs: vi.fn(async () => []),
  }
})

const notesApi = vi.hoisted(() => ({} as Record<string, unknown>))
vi.mock('@/composables/useLiveNotes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useLiveNotes')>()
  const { ref: r } = await import('vue')
  Object.assign(notesApi, {
    phase: r('idle'),
    segments: r([]),
    minutes: r(''),
    minutesPhase: r('idle'),
    revisePhase: r('idle'),
    reviseError: r(''),
    artifacts: r({}),
    error: r(''),
    pending: r(0),
    lost: r(0),
    truncated: r(false),
    start: vi.fn(),
    addViewer: vi.fn(),
    removeViewer: vi.fn(),
    stopCapture: vi.fn(async () => undefined),
    finishUploads: vi.fn(async () => undefined),
    generateMinutes: vi.fn(async () => undefined),
    refresh: vi.fn(async () => undefined),
    reset: vi.fn(),
  })
  return { ...actual, useLiveNotes: () => notesApi }
})

interface FakeWindow {
  document: Document
  close: ReturnType<typeof vi.fn>
  focus: ReturnType<typeof vi.fn>
  addEventListener: (type: string, cb: () => void) => void
  firePagehide: () => void
}

function fakeWindow(): FakeWindow {
  const listeners: Array<() => void> = []
  return {
    document: document.implementation.createHTMLDocument('pip'),
    close: vi.fn(),
    focus: vi.fn(),
    addEventListener: (type: string, cb: () => void) => {
      if (type === 'pagehide') listeners.push(cb)
    },
    firePagehide: () => listeners.forEach((f) => f()),
  }
}

function mountPanel() {
  // A fresh instance per mount: installing the same one twice trips
  // vue-i18n's "already set up" guard.
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingStage, {
    global: {
      plugins: [i18n],
      stubs: {
        ShareAddressList: true,
        LiveChatPanel: true,
        LoadingIndicator: true,
        LiveAnnotateOverlay: { template: '<div data-role="annotate-overlay" />' },
      },
    },
  })
}

beforeEach(() => {
  vi.unstubAllGlobals()
  toastShow.mockClear()
  nativeBridge.current = null
  ;(shareApi.state as { value: string }).value = 'publishing'
  ;(shareApi.screenSharer as { value: string }).value = ''
  ;(shareApi.remoteScreenStream as { value: unknown }).value = null
  ;(notesApi.phase as { value: string }).value = 'idle'
})

describe('MeetingStage windows', () => {
  it('opens the drawing surface in its own window instead of inline', async () => {
    const win = fakeWindow()
    const requestWindow = vi.fn(async () => win as unknown as Window)
    vi.stubGlobal('documentPictureInPicture', { requestWindow })

    const wrapper = mountPanel()
    await flushPromises()
    // Nothing is drawn until asked for.
    expect(wrapper.find('[data-role="annotate-overlay"]').exists()).toBe(false)

    await wrapper.find('[data-action="annotate"]').trigger('click')
    await flushPromises()

    expect(requestWindow).toHaveBeenCalledTimes(1)
    // The surface lives in the new document, not in the panel.
    expect(win.document.querySelector('[data-role="annotate-overlay"]')).not.toBeNull()
    expect(wrapper.find('[data-role="annotate-overlay"]').exists()).toBe(false)
  })

  it('brings the drawing surface back after its window went away', async () => {
    const first = fakeWindow()
    const second = fakeWindow()
    const requestWindow = vi
      .fn()
      .mockResolvedValueOnce(first as unknown as Window)
      .mockResolvedValueOnce(second as unknown as Window)
    vi.stubGlobal('documentPictureInPicture', { requestWindow })

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-action="annotate"]').trigger('click')
    await flushPromises()
    expect(first.document.querySelector('[data-role="annotate-overlay"]')).not.toBeNull()

    // The host closed the surface (or it was dismissed): the button has to
    // bring a new one up, not answer "already open" for a window that is gone.
    first.firePagehide()
    await flushPromises()
    expect(wrapper.find('[data-role="annotate-overlay"]').exists()).toBe(false)

    await wrapper.find('[data-action="annotate"]').trigger('click')
    await flushPromises()
    expect(requestWindow).toHaveBeenCalledTimes(2)
    expect(second.document.querySelector('[data-role="annotate-overlay"]')).not.toBeNull()
  })

  it('closes the drawing surface when the picture goes away', async () => {
    const win = fakeWindow()
    vi.stubGlobal('documentPictureInPicture', { requestWindow: vi.fn(async () => win as unknown as Window) })
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-action="annotate"]').trigger('click')
    await flushPromises()
    expect(win.document.querySelector('[data-role="annotate-overlay"]')).not.toBeNull()

    try {
      // Nothing left to draw on — and the button that could reopen it is hidden
      // once there is no screen, so an empty window would be a dead end.
      ;(shareApi.previewStream as { value: unknown }).value = null
      await flushPromises()
      expect(win.close).toHaveBeenCalled()
    } finally {
      ;(shareApi.previewStream as { value: unknown }).value = { id: 'preview' }
    }
  })

  it('refuses to annotate inline when the host has no Document PiP', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="annotate"]').trigger('click')
    await flushPromises()

    // The drawing surface must never land in the panel. With no window to put
    // it in, the tool says so instead of degrading into an in-dialog canvas.
    expect(wrapper.find('[data-role="annotate-overlay"]').exists()).toBe(false)
    expect(toastShow).toHaveBeenCalledWith(
      expect.stringContaining('separate window'),
      expect.anything(),
    )
  })

  it('draws on the desktop overlay when the shell provides one', async () => {
    const requestWindow = vi.fn(async () => fakeWindow() as unknown as Window)
    vi.stubGlobal('documentPictureInPicture', { requestWindow })
    const openAnnotateOverlay = vi.fn(async () => ({ opened: true, inkViaControl: true }))
    let onInk: ((msg: string) => void) | undefined
    nativeBridge.current = {
      openAnnotateOverlay,
      closeAnnotateOverlay: vi.fn(async () => undefined),
      onAnnotateInk: (cb: (msg: string) => void) => {
        onInk = cb
        return () => undefined
      },
      onAnnotateClosed: () => () => undefined,
    }

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-action="annotate"]').trigger('click')
    await flushPromises()

    // The desktop overlay wins: no second window, nothing inline.
    expect(openAnnotateOverlay).toHaveBeenCalledTimes(1)
    expect(requestWindow).not.toHaveBeenCalled()
    expect(wrapper.find('[data-role="annotate-overlay"]').exists()).toBe(false)

    // Strokes from the overlay go out over the control channel.
    onInk?.('{"t":"ink","phase":"move","x":0.25,"y":0.5,"color":"#ff3b30","width":4}')
    expect(shareApi.sendControl).toHaveBeenCalledWith(
      expect.objectContaining({ t: 'ink', x: 0.25, y: 0.5 }),
    )
  })

  it('keeps the preview surface when a single window is being shared', async () => {
    const win = fakeWindow()
    vi.stubGlobal('documentPictureInPicture', { requestWindow: vi.fn(async () => win as unknown as Window) })
    const openAnnotateOverlay = vi.fn(async () => ({ opened: true, inkViaControl: true }))
    nativeBridge.current = { openAnnotateOverlay }
    ;(shareApi.captureKind as { value: string }).value = 'window'

    try {
      const wrapper = mountPanel()
      await flushPromises()
      await wrapper.find('[data-action="annotate"]').trigger('click')
      await flushPromises()

      // The overlay covers the whole screen; the audience only sees that one
      // window, so the strokes would land outside the picture.
      expect(openAnnotateOverlay).not.toHaveBeenCalled()
      expect(win.document.querySelector('[data-role="annotate-overlay"]')).not.toBeNull()
    } finally {
      ;(shareApi.captureKind as { value: string }).value = 'screen'
    }
  })

  it('falls back to its own window when the desktop overlay refuses to open', async () => {
    const win = fakeWindow()
    vi.stubGlobal('documentPictureInPicture', { requestWindow: vi.fn(async () => win as unknown as Window) })
    nativeBridge.current = {
      openAnnotateOverlay: vi.fn(async () => ({ opened: false, inkViaControl: false })),
    }

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-action="annotate"]').trigger('click')
    await flushPromises()

    expect(win.document.querySelector('[data-role="annotate-overlay"]')).not.toBeNull()
    expect(wrapper.find('[data-role="annotate-overlay"]').exists()).toBe(false)
  })

  it("fills the screen with the member's picture as soon as it arrives", async () => {
    const requestWindow = vi.fn(async () => fakeWindow() as unknown as Window)
    vi.stubGlobal('documentPictureInPicture', { requestWindow })
    ;(shareApi.screenSharer as { value: string }).value = 'v1'
    ;(shareApi.remoteScreenStream as { value: unknown }).value = { id: 'screen' }

    const wrapper = mountPanel()
    await flushPromises()

    // No click: the host is presenting, so the picture has to come to them.
    const full = document.querySelector('[data-role="screen-fullscreen"]')
    expect(full).not.toBeNull()
    expect(full?.querySelector('.member-screen__video')).not.toBeNull()
    // ...over the whole app, not inside the dock tab it belongs to.
    expect(wrapper.element.contains(full)).toBe(false)
    // ...and the panel never draws a preview of it.
    expect(wrapper.find('video').exists()).toBe(false)
    // The picture arrives full screen; the window is opt-in from its dock.
    expect(requestWindow).not.toHaveBeenCalled()
    expect(full?.querySelector('[data-action="screen-switch"]')?.textContent).toContain(
      'Open in a window',
    )
  })

  it('moves the picture between the screen and its own window', async () => {
    const win = fakeWindow()
    const requestWindow = vi.fn(async () => win as unknown as Window)
    vi.stubGlobal('documentPictureInPicture', { requestWindow })
    ;(shareApi.screenSharer as { value: string }).value = 'v1'
    ;(shareApi.remoteScreenStream as { value: unknown }).value = { id: 'screen' }

    const wrapper = mountPanel()
    await flushPromises()

    const switchBtn = document.querySelector('[data-action="screen-switch"]') as HTMLElement
    switchBtn.click()
    await flushPromises()

    // One surface, two frames: the window gets the very same picture and dock.
    expect(requestWindow).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[data-role="screen-fullscreen"]')).toBeNull()
    expect(win.document.querySelector('.member-screen__video')).not.toBeNull()
    const dock = win.document.querySelector('.live-dock')
    expect(dock?.textContent).toContain('Stop sharing')
    const back = win.document.querySelector('[data-action="screen-switch"]') as HTMLElement
    expect(back.textContent).toContain('Full screen')

    // ...and the same button brings it back, so the move is not one-way.
    back.click()
    await flushPromises()
    expect(win.close).toHaveBeenCalled()
    expect(document.querySelector('[data-role="screen-fullscreen"]')).not.toBeNull()
  })

  it('stops the share from the picture dock', async () => {
    ;(shareApi.screenSharer as { value: string }).value = 'v1'
    ;(shareApi.remoteScreenStream as { value: unknown }).value = { id: 'screen' }

    mountPanel()
    await flushPromises()

    ;(document.querySelector('[data-action="screen-stop"]') as HTMLElement).click()
    expect(shareApi.revokeScreen).toHaveBeenCalledWith('v1')
  })

  it('keeps a way back in the panel once the host closes the picture', async () => {
    ;(shareApi.screenSharer as { value: string }).value = 'v1'
    ;(shareApi.remoteScreenStream as { value: unknown }).value = { id: 'screen' }

    const wrapper = mountPanel()
    await flushPromises()

    ;(document.querySelector('[data-action="screen-close"]') as HTMLElement).click()
    await flushPromises()

    // Closing it was a decision; it does not come back on its own, but the
    // panel offers both frames again — and still no inline picture.
    expect(document.querySelector('[data-role="screen-fullscreen"]')).toBeNull()
    expect(wrapper.find('video').exists()).toBe(false)
    expect(wrapper.find('[data-role="screen-actions"]').exists()).toBe(true)

    await wrapper.find('[data-action="show-screen-fullscreen"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-role="screen-fullscreen"]')).not.toBeNull()
  })

  it('does not offer a window the host cannot open', async () => {
    ;(shareApi.screenSharer as { value: string }).value = 'v1'
    ;(shareApi.remoteScreenStream as { value: unknown }).value = { id: 'screen' }

    const wrapper = mountPanel()
    await flushPromises()

    // Android WebView / Firefox: no Document PiP, so no switch button anywhere
    // (a button that cannot do anything is worse than none).
    expect(document.querySelector('[data-role="screen-fullscreen"]')).not.toBeNull()
    expect(document.querySelector('[data-action="screen-switch"]')).toBeNull()
    ;(document.querySelector('[data-action="screen-close"]') as HTMLElement).click()
    await flushPromises()
    expect(wrapper.find('[data-action="open-screen-window"]').exists()).toBe(false)
    expect(wrapper.find('[data-action="show-screen-fullscreen"]').exists()).toBe(true)
  })
})
