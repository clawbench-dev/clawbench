import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, type Ref } from 'vue'
import enMessages from '@/i18n/locales/en'
import MeetingStage from '../MeetingStage.vue'

/**
 * 截图 is only meaningful while a picture exists.
 *
 * The capture samples the host's own screen/window or a member's share
 * (`startShots` picks the same pair), so with nobody sharing the button could
 * only upload a blank frame — and a button that answers "nothing happened" is
 * worse than no button.
 */

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<typeof import('lucide-vue-next')>()
  const stub = (name: string) => ({ name, props: { size: Number }, render: () => null })
  return {
    ...actual,
    ImageIcon: stub('ImageIcon'),
    MonitorUp: stub('MonitorUp'),
    PenLine: stub('PenLine'),
    PhoneOff: stub('PhoneOff'),
    Mic: stub('Mic'),
    NotebookPen: stub('NotebookPen'),
    Captions: stub('Captions'),
  }
})

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
    shareLinks: r([]),
    title: r('demo'),
    viewers: r([]),
    talkEnabled: r(false),
    screenEnabled: r(true),
    screenSharer: r(''),
    remoteScreenStream: sr(null),
    error: r(''),
    levels: r({}),
    previewStream: sr(null),
    captureKind: r<string | null>('screen'),
    captureSourceId: r('screen:0:0'),
    videoEnabled: r(false),
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
    setViewerMuted: vi.fn(),
    kickViewer: vi.fn(),
    muteAll: vi.fn(),
    setScreenEnabled: vi.fn(),
    revokeScreen: vi.fn(),
    sendControl: vi.fn(),
    sendControlTo: vi.fn(),
    onControl: vi.fn(() => () => undefined),
    switchSource: vi.fn(async () => true),
    setControlSupported: vi.fn(),
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

const shotsApi = vi.hoisted(() => ({} as Record<string, unknown>))
vi.mock('@/composables/useMeetingShots', async () => {
  const { ref: r } = await import('vue')
  Object.assign(shotsApi, {
    active: r(true),
    paused: r(false),
    count: r(0),
    lost: r(0),
    items: r([]),
    start: vi.fn(),
    stop: vi.fn(),
    pause: vi.fn(),
    resume: vi.fn(),
    captureNow: vi.fn(),
    noteSourceChanged: vi.fn(),
  })
  return { useMeetingShots: () => shotsApi, _resetMeetingShotsForTest: vi.fn() }
})

function mountStage() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingStage, {
    global: {
      plugins: [i18n],
      stubs: {
        ShareAddressList: true,
        LiveChatPanel: true,
        LoadingIndicator: true,
        MemberScreenCard: true,
        MemberScreenSurface: true,
        LiveAnnotateOverlay: { template: '<div data-role="annotate-overlay" />' },
      },
    },
  })
}

function setRef(key: string, value: unknown): void {
  ;(shareApi[key] as Ref<unknown>).value = value
}

beforeEach(() => {
  vi.clearAllMocks()
  setRef('state', 'publishing')
  setRef('captureKind', null)
  setRef('screenSharer', '')
  setRef('error', '')
  ;(shotsApi.active as Ref<boolean>).value = true
  ;(shotsApi.paused as Ref<boolean>).value = false
})

describe('manual key-frame button visibility', () => {
  it('is hidden when neither the host nor a member shares a screen', async () => {
    const wrapper = mountStage()
    await flushPromises()
    expect(wrapper.find('[data-action="shot-now"]').exists()).toBe(false)
  })

  it('appears when the host starts sharing', async () => {
    const wrapper = mountStage()
    await flushPromises()
    expect(wrapper.find('[data-action="shot-now"]').exists()).toBe(false)

    setRef('captureKind', 'screen')
    await nextTick()
    expect(wrapper.find('[data-action="shot-now"]').exists()).toBe(true)
  })

  it('appears when a member shares, even though the host is not', async () => {
    setRef('captureKind', null)
    setRef('screenSharer', 'v1')
    const wrapper = mountStage()
    await flushPromises()
    expect(wrapper.find('[data-action="shot-now"]').exists()).toBe(true)
  })

  it('disappears again when the last share is revoked', async () => {
    setRef('captureKind', 'screen')
    const wrapper = mountStage()
    await flushPromises()
    expect(wrapper.find('[data-action="shot-now"]').exists()).toBe(true)

    setRef('captureKind', null)
    await nextTick()
    expect(wrapper.find('[data-action="shot-now"]').exists()).toBe(false)
  })

  it('draws a kept frame in the live transcript instead of only counting it', async () => {
    setRef('captureKind', 'screen')
    ;(notesApi.phase as Ref<string>).value = 'recording'
    ;(shotsApi.items as Ref<Array<Record<string, unknown>>>).value = [
      { id: 8, at: 1_700_000_005_000, reason: 'manual', ref: '' },
    ]
    const wrapper = mountStage()
    await flushPromises()

    await wrapper.find('[data-live-tab="notes"]').trigger('click')

    const shot = wrapper.find('[data-role="notes-pane"] [data-role="shot-open"]')
    expect(shot.exists()).toBe(true)
    expect(shot.find('img').attributes('src')).toContain('/api/share/tok-1/notes/shot?id=8')
  })

  it('stays hidden while key frames are paused, even with a picture', async () => {
    setRef('captureKind', 'screen')
    ;(shotsApi.paused as Ref<boolean>).value = true
    const wrapper = mountStage()
    await flushPromises()
    expect(wrapper.find('[data-action="shot-now"]').exists()).toBe(false)
  })
})
