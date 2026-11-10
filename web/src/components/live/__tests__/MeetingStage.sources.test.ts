import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, shallowRef } from 'vue'
import enMessages from '@/i18n/locales/en'
import { _resetCaptureSources, rememberCaptureSources } from '@/utils/captureSourceCache'
import MeetingStage from '../MeetingStage.vue'

/**
 * The mid-meeting source swap renders the same tiles as the create page.
 *
 * It used to render bare names: the host picked a window to share from a column
 * of identical-looking strings, while the very same list one screen earlier
 * (创建会议) carried a still frame per source. The frame is the only thing that
 * tells two windows of the same app apart, so the picker must carry it too.
 */

vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<typeof import('lucide-vue-next')>()
  const stub = (name: string) => ({ name, props: { size: Number }, render: () => null })
  // Spread the real module so icons added by the shared source grid resolve
  // instead of becoming undefined and crashing the render.
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

const listSources = vi.hoisted(() => vi.fn(async () => [] as unknown[]))

const shareApi = vi.hoisted(() => ({} as Record<string, unknown>))
vi.mock('@/composables/useLiveShare', async () => {
  const { ref: r, shallowRef: sr } = await import('vue')
  Object.assign(shareApi, {
    state: r('publishing'),
    token: r('tok-1'),
    shareUrl: r('https://h/share/tok-1'),
    shareLinks: r([{ label: 'LAN', url: 'https://h/share/tok-1' }]),
    title: r('demo'),
    viewers: r([]),
    talkEnabled: r(false),
    screenEnabled: r(true),
    screenSharer: r(''),
    remoteScreenStream: sr(null),
    error: r(''),
    levels: r({}),
    previewStream: sr({ id: 'preview' }),
    captureKind: r('screen'),
    captureSourceId: r('screen:0:0'),
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
    setViewerMuted: vi.fn(),
    kickViewer: vi.fn(),
    muteAll: vi.fn(),
    setScreenEnabled: vi.fn(),
    revokeScreen: vi.fn(),
    sendControl: vi.fn(),
    sendControlTo: vi.fn(),
    onControl: vi.fn(() => () => undefined),
    switchSource: vi.fn(async () => true),
    // Unmounting the stage closes the annotation surface, which clears the ink
    // mirror it kept.
    clearInkMirror: vi.fn(),
    // The panel pushes the native injector probe into the room so
    // viewers can grey their "request control" button.
    setControlSupported: vi.fn(),
  })
  return {
    useLiveShare: () => shareApi,
    listCaptureSources: listSources,
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
    liveLines: r({}),
    minutes: r(''),
    minutesPhase: r('idle'),
    revisePhase: r('idle'),
    reviseError: r(''),
    artifacts: r({}),
    error: r(''),
    pending: r(0),
    lost: r(0),
    truncated: r(false),
    health: r('ok'),
    silentForMs: r(0),
    deadChannels: r([]),
    connection: r('online'),
    audioSuspended: r(false),
    setConnectionState: vi.fn(),
    setAudioSuspended: vi.fn(),
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

async function openPicker() {
  // Nothing is enumerated until the host asks to share: the grid opens with the
  // "选择共享画面" tile, and clicking it is what raises the OS picker. The panel
  // is mounted for the whole meeting, so asking at mount time would raise that
  // dialog on every visit to the tab.
  const wrapper = mountPanel()
  await flushPromises()
  await wrapper.find('[data-source="__pick__"]').trigger('click')
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  // The enumerated list is cached module-wide (so a remount does not collapse
  // the grid); each test has to start from "the OS was never asked".
  _resetCaptureSources()
  // A desktop host: only it can enumerate sources, and only it shows the
  // picker (a browser goes straight to its own dialog).
  nativeBridge.current = { listCaptureSources: vi.fn(async () => '[]') }
  ;(shareApi.state as { value: string }).value = 'publishing'
  ;(shareApi.captureKind as { value: string }).value = 'screen'
  ;(shareApi.error as { value: string }).value = ''
  ;(notesApi.phase as { value: string }).value = 'idle'
})

describe('MeetingStage source picker', () => {
  // The stage stays mounted for as long as the meeting panel is open (it is a
  // v-show sibling of the record list), so enumerating screens here would make
  // the OS raise its screen-share dialog merely because the tab was opened —
  // on Wayland that request IS the dialog.
  it('does not enumerate screens just because the panel is mounted', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(listSources).not.toHaveBeenCalled()
    expect(wrapper.find('[data-source="__pick__"]').exists()).toBe(true)
    // Nothing was asked, so "there is nothing to share" would be a guess.
    expect(wrapper.find('[data-role="stage-sources"]').exists()).toBe(false)
  })

  it('draws the shared source grid and swaps to the tile that was clicked', async () => {
    listSources.mockResolvedValueOnce([
      { id: 'screen:0:0', name: 'Entire Screen', kind: 'screen', thumbnail: 'data:image/png;base64,AAA' },
      { id: 'window:1:0', name: 'Terminal', kind: 'window', thumbnail: '' },
    ])

    const wrapper = await openPicker()
    const tiles = wrapper.findAll('.capture-source')

    // The "不共享" tile first, then the two sources — the same three choices the
    // create page offered one screen earlier.
    expect(tiles).toHaveLength(3)
    // The still frame is what makes the pick possible at all: two windows of
    // the same app are indistinguishable by name.
    expect(tiles[1].find('.capture-source__thumb img').attributes('src')).toBe('data:image/png;base64,AAA')
    expect(tiles[2].find('.capture-source__name').text()).toBe('Terminal')

    await tiles[2].trigger('click')
    await flushPromises()

    expect(shareApi.switchSource).toHaveBeenCalledWith('window:1:0')
    // The grid stays: it is the live picture state, not a popup that closes.
    expect(wrapper.find('.capture-source').exists()).toBe(true)
  })

  it('reports a failed swap as a toast, with no inline copy left in the panel', async () => {
    listSources.mockResolvedValueOnce([
      { id: 'screen:0:0', name: 'Entire Screen', kind: 'screen', thumbnail: '' },
      { id: 'window:1:0', name: 'Terminal', kind: 'window', thumbnail: '' },
    ])
    ;(shareApi.switchSource as ReturnType<typeof vi.fn>).mockResolvedValueOnce(false)
    ;(shareApi.error as { value: string }).value = 'Could not switch; keeping the current picture'

    const wrapper = await openPicker()
    await wrapper.findAll('.capture-source')[2].trigger('click')
    await flushPromises()

    expect(toastShow).toHaveBeenCalledWith('Could not switch; keeping the current picture', {
      type: 'error',
      icon: 'warning',
    })
    // The toast is the ONLY surface: a persistent copy under the tabs outlived
    // the toast and stayed there until the next successful action.
    expect(wrapper.find('.live-dialog-error').exists()).toBe(false)
  })

  // The panel is re-created whenever the project changes (the app container is
  // keyed by project), and the meeting keeps running through it. The grid used
  // to come back as a single "选择共享画面" tile, so the host had to ask the OS a
  // second time for screens it had already listed.
  it('keeps the grid after the panel is re-created', async () => {
    listSources.mockResolvedValue([
      { id: 'screen:0:0', name: 'Entire Screen', kind: 'screen', thumbnail: '' },
      { id: 'window:1:0', name: 'Terminal', kind: 'window', thumbnail: '' },
    ])

    const first = await openPicker()
    expect(first.findAll('.capture-source')).toHaveLength(3)
    first.unmount()

    const again = mountPanel()
    await flushPromises()

    // The tiles are back without a second enumeration — asking again would
    // raise the OS screen-share dialog on every remount (the whole reason the
    // list is not probed at mount time).
    expect(again.findAll('.capture-source')).toHaveLength(3)
    expect(listSources).toHaveBeenCalledTimes(1)
    expect(again.find('[data-source="__pick__"]').exists()).toBe(false)
  })

  // A meeting started from 创建会议: the form already asked the OS for the list,
  // so the stage draws the same tiles instead of an empty picker.
  it('draws the list the create form already asked for', async () => {
    rememberCaptureSources([
      { id: 'screen:0:0', name: 'Entire Screen', kind: 'screen', thumbnail: '' },
      { id: 'window:1:0', name: 'Terminal', kind: 'window', thumbnail: '' },
    ])

    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('.capture-source')).toHaveLength(3)
    expect(wrapper.find('[data-source="__pick__"]').exists()).toBe(false)
    expect(listSources).not.toHaveBeenCalled()
    // The live source is the selected tile, as before.
    expect(wrapper.find('[data-source="screen:0:0"]').classes()).toContain('capture-source--on')
  })
})
