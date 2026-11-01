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
  Video: { name: 'VideoIcon', render: () => null },
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

/** Whether this machine reports a camera; several tests flip it. */
const cameraDevice = vi.hoisted(() => ({ has: true }))

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
    hasCameraDevice: vi.fn(async () => cameraDevice.has),
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
  cameraDevice.has = true
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('MeetingCreate in a browser', () => {
  it('offers both meeting types and keeps start usable with no source list', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // A browser's own picker IS the source choice, so an empty list is not a
    // problem: video is selected by default and falls back to the camera.
    expect(wrapper.find('[data-type="voice"]').exists()).toBe(true)
    expect(wrapper.find('[data-type="video"]').exists()).toBe(true)
    expect(wrapper.find('.meeting-type--on').attributes('data-type')).toBe('video')

    const start = wrapper.find('[data-action="meeting-start"]')
    expect(start.exists()).toBe(true)
    // The nickname is required, so fill it before checking the start button.
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    expect(start.attributes('disabled')).toBeUndefined()
  })

  it('refuses a video meeting with no camera and no screen', async () => {
    stubCaptureApi(true)
    cameraDevice.has = false
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    const start = wrapper.find('[data-action="meeting-start"]')
    // Screen sharing is on by default and a browser can always pick one, so
    // the invalid state has to be reached explicitly.
    expect(start.attributes('disabled')).toBeUndefined()

    await wrapper.find('[data-role="meeting-screen"]').setValue(false)
    // "Video meeting" with nothing to show would start a voice meeting under a
    // false name, so it is blocked with an explanation rather than silently
    // downgraded.
    expect(start.attributes('disabled')).toBeDefined()
    expect(wrapper.find('.meeting-warn').text()).toBe(enMessages.meeting.create.noPicture)

    // A voice meeting needs no picture at all.
    await wrapper.find('[data-type="voice"]').trigger('click')
    expect(start.attributes('disabled')).toBeUndefined()
  })

  it('starts a voice meeting with no video flag and no screen source', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-type="voice"]').trigger('click')
    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()

    expect(shareApi.start).toHaveBeenCalledWith(
      expect.objectContaining({ video: false, camera: false, sourceId: '' }),
    )
  })

  it('sends the camera flag for a video meeting', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await wrapper.find('[data-role="meeting-camera"]').setValue(true)
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()

    expect(shareApi.start).toHaveBeenCalledWith(
      expect.objectContaining({ video: true, camera: true }),
    )
  })

  it('disables the camera option when the machine has none', async () => {
    stubCaptureApi(true)
    cameraDevice.has = false
    const wrapper = mountPanel()
    await flushPromises()

    const camera = wrapper.find('[data-role="meeting-camera"]')
    expect(camera.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain(enMessages.meeting.create.cameraNoDevice)
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
    // Neither capture API exists on an insecure origin, camera included — the
    // probe goes through the same `navigator.mediaDevices`.
    cameraDevice.has = false
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
