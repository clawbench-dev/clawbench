import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import { _resetMeetingOptions, useMeetingOptions } from '@/composables/useMeetingOptions'
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
  // The capture options are module-level singletons shared with the running
  // meeting; a test that flips the camera would otherwise leak into the next
  // one and make the expectations order-dependent.
  _resetMeetingOptions()
  settingsApi.serverConfig.value = {}
  settingsApi.setServerValue.mockReset()
  settingsApi.setServerValue.mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
  ;(shareApi.state as { value: string }).value = 'idle'
  ;(shareApi.viewers as { value: unknown[] }).value = []
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

  it('describes the camera/screen combination in one line, four states', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    const picture = () => wrapper.find('[data-role="meeting-picture"]')
    // Screen sharing is on by default (the browser's own picker picks it) and
    // the camera is opt-in, so the form opens on "screen only".
    expect(picture().classes()).toContain('meeting-hint')
    expect(picture().text()).toBe(enMessages.meeting.create.pictureScreen)

    await wrapper.find('[data-role="meeting-camera"]').setValue(true)
    expect(picture().text()).toBe(enMessages.meeting.create.pictureBoth)

    await wrapper.find('[data-role="meeting-screen"]').setValue(false)
    expect(picture().text()).toBe(enMessages.meeting.create.pictureCamera)

    // "Neither" is the only combination that cannot start, so it is the only
    // one that turns into a warning — the state and the warning are one line,
    // not two.
    await wrapper.find('[data-role="meeting-camera"]').setValue(false)
    expect(picture().classes()).toContain('meeting-warn')
    expect(picture().text()).toBe(enMessages.meeting.create.noPicture)
  })

  it('does not repeat a description under each simple toggle', async () => {
    stubCaptureApi(true)
    const wrapper = mountPanel()
    await flushPromises()

    // Only the camera/screen pair needs explaining (their combination decides
    // the picture); the rest name themselves.
    expect(wrapper.findAll('[data-role="meeting-picture"]')).toHaveLength(1)
    const text = wrapper.text()
    const dropped = [
      'cameraOn',
      'cameraOff',
      'micOn',
      'micOff',
      'systemAudioHint',
      'notesOn',
      'notesOff',
    ] as const
    for (const key of dropped) {
      expect(text, `meeting.create.${key} should not be rendered`).not.toContain(
        enMessages.meeting.create[key],
      )
    }
  })

  it('opens with the microphone on, and leaves it editable', async () => {
    stubCaptureApi(true)
    // The option is a singleton shared with the running stage, so a previous
    // meeting may have left it muted. The form must still open with it ON: the
    // host wants to be heard without having to remember to tick a box.
    const { microphone } = useMeetingOptions()
    microphone.value = false

    const wrapper = mountPanel()
    await flushPromises()

    const mic = wrapper.find('[data-role="meeting-mic"]')
    expect((mic.element as HTMLInputElement).checked).toBe(true)
    // A default, not a lock: it can still be turned off, before and during.
    expect(mic.attributes('disabled')).toBeUndefined()
    await mic.setValue(false)
    expect((mic.element as HTMLInputElement).checked).toBe(false)

    // ...and switching to a voice meeting does not force it back on.
    await wrapper.find('[data-type="voice"]').trigger('click')
    await flushPromises()
    expect((mic.element as HTMLInputElement).checked).toBe(false)
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

  // The transcript labels a viewer by nickname once the join frame has
  // delivered it, so the stored lines (and the minutes built from them) name
  // people instead of numbering them.
  it('labels viewer channels with the nickname once it is known', async () => {
    stubCaptureApi(true)
    ;(shareApi.viewers as { value: Array<{ id: string; name: string; talk: string }> }).value = [
      { id: 'v1', name: '张三', talk: 'muted' },
      { id: 'v2', name: '  ', talk: 'muted' },
    ]
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-role="meeting-nick"]').setValue('Alex')
    await wrapper.find('[data-action="meeting-start"]').trigger('click')
    await flushPromises()

    const args = (shareApi.start as ReturnType<typeof vi.fn>).mock.calls[0][0]
    const label = args.notesLabel as (channel: string) => string
    expect(label('mic')).toBe(enMessages.liveShare.notes.speakerMe)
    expect(label('viewer:v1')).toBe('张三')
    // No nickname yet: the ordinal keeps the line attributable.
    expect(label('viewer:v2')).toBe('Viewer 2')
  })

  it('disables the camera option when the machine has none', async () => {
    stubCaptureApi(true)
    cameraDevice.has = false
    const wrapper = mountPanel()
    await flushPromises()

    const camera = wrapper.find('[data-role="meeting-camera"]')
    expect(camera.attributes('disabled')).toBeDefined()
    // The single description line carries the reason instead of a hint under
    // the disabled checkbox.
    expect(wrapper.find('[data-role="meeting-picture"]').text()).toBe(
      enMessages.meeting.create.cameraNoDevice,
    )

    // With no camera the only way to a picture is a screen, and "neither" says
    // exactly that rather than telling the user to turn on a camera they lack.
    await wrapper.find('[data-role="meeting-screen"]').setValue(false)
    expect(wrapper.find('.meeting-warn').text()).toBe(
      enMessages.meeting.create.noPictureNoCamera,
    )
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
