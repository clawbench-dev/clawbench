import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, shallowRef } from 'vue'
import enMessages from '@/i18n/locales/en'
import LiveSharePanel from '../LiveSharePanel.vue'

/**
 * The panel is a view: what it owns is WHICH surface each tool opens on.
 * Both the drawing surface and the member's screen must come up in their own
 * window (Document PiP) rather than inline — drawn inline the canvas is
 * squeezed into the panel and the strokes land somewhere else.
 */

vi.mock('lucide-vue-next', () => ({
  MonitorUp: { name: 'MonitorUpIcon', render: () => null },
  X: { name: 'XIcon', render: () => null },
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => null,
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
    getAudioCaptureSupport: vi.fn(async () => false),
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
}

function fakeWindow(): FakeWindow {
  return {
    document: document.implementation.createHTMLDocument('pip'),
    close: vi.fn(),
    focus: vi.fn(),
    addEventListener: () => undefined,
  }
}

function mountPanel() {
  // A fresh instance per mount: installing the same one twice trips
  // vue-i18n's "already set up" guard.
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(LiveSharePanel, {
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
  ;(shareApi.state as { value: string }).value = 'publishing'
  ;(shareApi.screenSharer as { value: string }).value = ''
  ;(shareApi.remoteScreenStream as { value: unknown }).value = null
  ;(notesApi.phase as { value: string }).value = 'idle'
})

describe('LiveSharePanel windows', () => {
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

  it('falls back to the inline overlay when the host has no Document PiP', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="annotate"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-role="annotate-overlay"]').exists()).toBe(true)
  })

  it("shows the member's screen in its own window with a dock", async () => {
    const win = fakeWindow()
    const requestWindow = vi.fn(async () => win as unknown as Window)
    vi.stubGlobal('documentPictureInPicture', { requestWindow })
    ;(shareApi.screenSharer as { value: string }).value = 'v1'
    ;(shareApi.remoteScreenStream as { value: unknown }).value = { id: 'screen' }

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-action="open-screen-window"]').trigger('click')
    await flushPromises()

    expect(requestWindow).toHaveBeenCalledTimes(1)
    const dock = win.document.querySelector('.live-window__dock')
    expect(dock).not.toBeNull()
    expect(dock?.textContent).toContain('Stop sharing')
    expect(win.document.querySelector('.live-window__video')).not.toBeNull()
  })
})
