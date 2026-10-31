import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'
import enMessages from '@/i18n/locales/en'
import NotesPanel from '../NotesPanel.vue'

vi.mock('lucide-vue-next', () => ({
  NotebookPen: { name: 'NotebookPenIcon', render: () => null },
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

// The notes state machine is a module singleton shared with the share panel;
// the panel only reads it, so a small reactive stub is enough here.
const notesApi = vi.hoisted(() => ({}) as Record<string, unknown>)
vi.mock('@/composables/useLiveNotes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useLiveNotes')>()
  Object.assign(notesApi, {
    phase: ref('idle'),
    segments: ref([]),
    minutes: ref(''),
    minutesFile: ref(''),
    minutesPhase: ref('idle'),
    revisePhase: ref('idle'),
    reviseError: ref(''),
    artifacts: ref({}),
    error: ref(''),
    pending: ref(0),
    lost: ref(0),
    truncated: ref(false),
    start: vi.fn(),
    addViewer: vi.fn(),
    removeViewer: vi.fn(),
    stopCapture: vi.fn(async () => undefined),
    finishUploads: vi.fn(async () => undefined),
    finish: vi.fn(async () => undefined),
    generateMinutes: vi.fn(async () => undefined),
    refresh: vi.fn(async () => undefined),
    reset: vi.fn(),
  })
  return { ...actual, useLiveNotes: () => notesApi }
})

const shareState = vi.hoisted(() => ({ current: null }) as { current: null | { value: string } })
vi.mock('@/composables/useLiveShare', async () => {
  const { ref: makeRef } = await import('vue')
  shareState.current = makeRef('idle')
  return { useLiveShare: () => ({ state: shareState.current }) }
})

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })

const SESSION = {
  dir: '2026-10-31-1732-周会-a1b2c3',
  dirPath: '/proj/.clawbench/notes/2026-10-31-1732-周会-a1b2c3',
  title: '周会',
  startedAt: 1_761_900_000_000,
  updatedAt: 1_761_900_600_000,
  hasMinutes: true,
  hasTranscript: true,
  hasRevised: true,
  audioFiles: 4,
}

const UNTITLED = {
  ...SESSION,
  dir: '2026-10-31-1800-a1b2c3',
  dirPath: '/proj/.clawbench/notes/2026-10-31-1800-a1b2c3',
  title: '',
  hasMinutes: false,
  hasTranscript: false,
  hasRevised: false,
  audioFiles: 0,
}

function stubFetch(sessions: unknown[] = [SESSION, UNTITLED]) {
  const fetchMock = vi.fn(async (url: string) => {
    if (url === '/api/live-share/notes/session') {
      return { ok: true, json: async () => ({ token: 'a1b2c3d4' }) }
    }
    if (url.startsWith('/api/live-share/notes/sessions/artifact')) {
      return { ok: true, json: async () => ({ kind: 'minutes', content: 'MINUTES BODY' }) }
    }
    return { ok: true, json: async () => ({ sessions }) }
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function mountPanel(props: Record<string, unknown> = { active: true }) {
  return mount(NotesPanel, { props, global: { plugins: [i18n] } })
}

beforeEach(() => {
  vi.unstubAllGlobals()
  shareState.current!.value = 'idle'
  ;(notesApi.phase as { value: string }).value = 'idle'
})

describe('NotesPanel history', () => {
  it('lists past sessions newest-first, with an untitled fallback', async () => {
    stubFetch()
    const wrapper = mountPanel()
    await flushPromises()

    const rows = wrapper.findAll('.notes-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('周会')
    expect(rows[0].text()).toContain('Clips: 4')
    // A folder recorded without a title still gets a readable row rather than
    // an empty label.
    expect(rows[1].text()).toContain('Untitled note')
  })

  it('opens a session, reads its minutes, and returns to the list', async () => {
    const fetchMock = stubFetch()
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('.notes-row')[0].trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(`artifact?dir=${encodeURIComponent(SESSION.dir)}&kind=minutes`),
    )
    expect(wrapper.find('[data-role="notes-detail"]').text()).toContain('MINUTES BODY')

    await wrapper.find('[data-action="notes-back"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.notes-row')).toHaveLength(2)
  })

  it('opens the artifact in the editor with the path separator the server used', async () => {
    stubFetch()
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('.notes-row')[0].trigger('click')
    await flushPromises()
    await wrapper.find('[data-action="notes-open-file"]').trigger('click')

    expect(wrapper.emitted('open-file')?.[0]).toEqual([`${SESSION.dirPath}/minutes.md`])
  })

  it('shows the failure instead of an empty archive when the request fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) })))
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain(enMessages.liveShare.history.loadFailed)
    expect(wrapper.findAll('.notes-row')).toHaveLength(0)
  })
})

describe('NotesPanel recording', () => {
  it('refuses to start while a share owns the notes state machine', async () => {
    stubFetch([])
    shareState.current!.value = 'publishing'
    const wrapper = mountPanel()
    await flushPromises()

    const start = wrapper.find('[data-action="notes-start"]')
    expect(start.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain(enMessages.liveShare.solo.busy)
  })

  it('stops its own recording when a share starts', async () => {
    stubFetch([])
    // The real recorder needs a microphone; the panel only has to drive the
    // shared state machine, so the capture side is stubbed down to its API.
    vi.stubGlobal(
      'AudioContext',
      class {
        close = async () => undefined
      },
    )
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: async () => ({ getTracks: () => [{ stop: vi.fn() }] }) },
    })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('[data-action="notes-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-role="notes-recording"]').exists()).toBe(true)

    shareState.current!.value = 'publishing'
    await flushPromises()

    expect(notesApi.stopCapture).toHaveBeenCalled()
    // The panel leaves the recording view rather than showing another
    // session's transcript.
    expect(wrapper.find('[data-role="notes-recording"]').exists()).toBe(false)
  })
})
