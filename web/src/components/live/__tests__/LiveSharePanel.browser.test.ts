import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import LiveSharePanel from '../LiveSharePanel.vue'

/**
 * The publisher setup form in a plain browser.
 *
 * A browser has no source list — its own picker is the source choice — so the
 * form must not read the empty list as "nothing to share", and "start" must be
 * clickable without a tile selected. The other half is the message shown when
 * the page cannot capture at all: an insecure origin is not a stale client, and
 * telling the user to update theirs sends them looking in the wrong place.
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
    state: r('idle'),
    token: r(''),
    shareUrl: r(''),
    shareLinks: r([]),
    title: r(''),
    viewers: r([]),
    talkEnabled: r(false),
    screenEnabled: r(false),
    screenSharer: r(''),
    remoteScreenStream: sr(null),
    error: r(''),
    levels: r({}),
    previewStream: sr(null),
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
    getAudioCaptureSupport: vi.fn(async () => true),
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

function mountPanel() {
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

/** Install (or remove) the browser capture API, as a secure/insecure page would. */
function stubCaptureApi(available: boolean): void {
  vi.stubGlobal('navigator', available ? { mediaDevices: { getDisplayMedia: vi.fn() } } : {})
}

beforeEach(() => {
  vi.clearAllMocks()
  ;(shareApi.state as { value: string }).value = 'idle'
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('LiveSharePanel in a browser', () => {
  it('offers the picker-driven start form without any source tile', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('[data-role="capture-unsupported"]').exists()).toBe(false)
    // The empty list is not a problem here: the system dialog decides.
    expect(wrapper.find('[data-role="no-sources"]').text()).toBe(
      enMessages.liveShare.browserCaptureHint,
    )
    expect(wrapper.find('.live-source').exists()).toBe(false)

    const start = wrapper.find('[data-action="start-share"]')
    expect(start.exists()).toBe(true)
    expect(start.attributes('disabled')).toBeUndefined()
  })

  it('explains an insecure origin instead of blaming the client', async () => {
    stubCaptureApi(false)
    const original = Object.getOwnPropertyDescriptor(window, 'isSecureContext')
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    try {
      const wrapper = mountPanel()
      await flushPromises()

      expect(wrapper.find('[data-role="capture-unsupported"]').text()).toBe(
        enMessages.liveShare.insecureContext,
      )
      expect(wrapper.find('[data-action="start-share"]').exists()).toBe(false)
    } finally {
      if (original) Object.defineProperty(window, 'isSecureContext', original)
      else delete (window as unknown as Record<string, unknown>).isSecureContext
    }
  })
})
