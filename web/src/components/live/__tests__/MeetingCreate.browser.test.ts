import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MeetingCreate from '../MeetingCreate.vue'

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
  ArrowLeft: { name: 'ArrowLeftIcon', render: () => null },
  Mic: { name: 'MicIcon', render: () => null },
  Monitor: { name: 'MonitorIcon', render: () => null },
  NotebookPen: { name: 'NotebookPenIcon', render: () => null },
  Volume2: { name: 'Volume2Icon', render: () => null },
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

// The create page reads the account nickname (user.nickname) for its required
// identity field, and writes an edit back through the settings API.
const settingsApi = vi.hoisted(() => ({
  serverConfig: { value: {} as Record<string, unknown> },
  setServerValue: vi.fn(),
}))
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: settingsApi.serverConfig,
    setServerValue: settingsApi.setServerValue,
  }),
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
  return mount(MeetingCreate, {
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
  localStorage.clear()
  settingsApi.serverConfig.value = {}
  settingsApi.setServerValue.mockReset()
  settingsApi.setServerValue.mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
  ;(shareApi.state as { value: string }).value = 'idle'
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('MeetingCreate in a browser', () => {
  it('offers the voice tile and a usable start button when there is no source list', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // A browser's own picker IS the source choice, so an empty list is not a
    // problem: only the voice tile is rendered, and it is selected by default.
    expect(wrapper.findAll('.meeting-source')).toHaveLength(1)
    expect(wrapper.find('[data-source="voice"]').exists()).toBe(true)

    const start = wrapper.find('[data-action="meeting-start"]')
    expect(start.exists()).toBe(true)
    // The nickname is required, so fill it before checking the start button.
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    expect(start.attributes('disabled')).toBeUndefined()
  })

  it('cannot start without a nickname', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    const start = wrapper.find('[data-action="meeting-start"]')
    expect(start.attributes('disabled')).toBeDefined()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    expect(start.attributes('disabled')).toBeUndefined()
  })

  it('pre-fills the account nickname', async () => {
    stubCaptureApi(true)
    settingsApi.serverConfig.value = { user: { nickname: '泷泽' } }
    const wrapper = mountPanel()
    await flushPromises()

    expect((wrapper.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('泷泽')
  })

  it('does not pre-fill the fallback literal', async () => {
    stubCaptureApi(true)
    settingsApi.serverConfig.value = { user: { nickname: 'User' } }
    const wrapper = mountPanel()
    await flushPromises()

    expect((wrapper.find('[data-role="meeting-nick"]').element as HTMLInputElement).value).toBe('')
  })

  it('explains an insecure origin instead of blaming the client', async () => {
    stubCaptureApi(false)
    const original = Object.getOwnPropertyDescriptor(window, 'isSecureContext')
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    try {
      const wrapper = mountPanel()
      await flushPromises()

      expect(wrapper.find('.meeting-warn').text()).toBe(enMessages.liveShare.insecureContext)
      // Nothing can be started: the button is present but disabled rather than
      // hidden, so the footer does not jump around between hosts.
      expect(wrapper.find('[data-action="meeting-start"]').attributes('disabled')).toBeDefined()
    } finally {
      if (original) Object.defineProperty(window, 'isSecureContext', original)
      else delete (window as unknown as Record<string, unknown>).isSecureContext
    }
  })
})
