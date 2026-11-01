import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { ref } from 'vue'
import enMessages from '@/i18n/locales/en'
import MeetingList from '../MeetingList.vue'

/**
 * The meeting list is the tab's landing view. The two things worth guarding:
 * a record with no artifacts is still listed (a meeting that recorded nothing
 * still happened), and the two entry points live in the footer.
 */

vi.mock('lucide-vue-next', () => {
  const stub = { render: () => null }
  return {
    AlertTriangle: stub,
    MonitorUp: stub,
    Check: stub,
    CheckCircle2: stub,
    Link: stub,
    Mic: stub,
    Plus: stub,
    Video: stub,
    RefreshCw: stub,
    RotateCw: stub,
    RotateCcw: stub,
    Trash2: stub,
  }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/utils/attachDrag', () => ({
  cleanupDragGhost: vi.fn(),
  buildAttachDragImage: vi.fn(),
}))

const confirmMock = vi.hoisted(() => vi.fn(async () => true))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: confirmMock }),
}))

const sessions = ref<unknown[]>([])
const loading = ref(false)
const loadMock = vi.hoisted(() => vi.fn(async () => undefined))
const removeMock = vi.hoisted(() => vi.fn(async () => ({ ok: true })))
vi.mock('@/composables/useMeetingHistory', async (importOriginal) => {
  // Keep the real helpers (isStaleGenerating drives the "interrupted" chip);
  // only the composable's I/O is stubbed.
  const actual = await importOriginal<typeof import('@/composables/useMeetingHistory')>()
  return {
    ...actual,
    useMeetingHistory: () => ({
      sessions,
      loading,
      error: ref(''),
      load: loadMock,
      remove: removeMock,
    }),
  }
})

function mountList() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingList, {
    props: { active: true },
    global: { plugins: [i18n], stubs: { LoadingIndicator: true } },
  })
}

function record(over: Record<string, unknown> = {}) {
  return {
    token: 'tok-1',
    title: 'Release review',
    startedAt: 1_700_000_000_000,
    endedAt: 1_700_002_880_000,
    hasMinutes: true,
    hasTranscript: true,
    hasRevised: false,
    audioFiles: 12,
    chatCount: 4,
    status: 'ready',
    kind: 'video',
    speakerCount: 2,
    updatedAt: 1_700_002_880_000,
    ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  sessions.value = []
  loading.value = false
})

describe('MeetingList header', () => {
  it('draws the title, count and refresh in a single bar', () => {
    sessions.value = [record(), record({ token: 'two' })]
    const w = mountList()

    expect(w.findAll('.meeting-header')).toHaveLength(1)
    // The second bar was the bug: the tab title and the list title stacked.
    expect(w.find('.meeting-toolbar').exists()).toBe(false)
    const header = w.find('.meeting-header')
    expect(header.find('.meeting-header__title').text()).toBe(enMessages.meeting.list.title)
    expect(header.find('.meeting-header__count').text()).toBe('2 total')
    expect(header.find('[data-action="meeting-refresh"]').exists()).toBe(true)
  })

  it('reloads through the shared refresh button', async () => {
    const w = mountList()
    loadMock.mockClear()

    await w.find('[data-action="meeting-refresh"]').trigger('click')

    expect(loadMock).toHaveBeenCalledTimes(1)
  })

  it('spins and blocks re-entry while a reload is in flight', async () => {
    const w = mountList()
    const btn = () => w.find('[data-action="meeting-refresh"]')

    expect(btn().classes()).not.toContain('refresh-spin--active')

    loading.value = true
    await flushPromises()

    // The spin is the feedback the old bare icon button never gave.
    expect(btn().classes()).toContain('refresh-spin--active')
    expect(btn().attributes('disabled')).toBeDefined()

    loading.value = false
    await flushPromises()
    expect(btn().classes()).not.toContain('refresh-spin--active')
  })
})

describe('MeetingList', () => {
  it('lists a record with no artifacts instead of hiding it', () => {
    sessions.value = [record({ token: 'empty', title: '', hasMinutes: false, hasTranscript: false, audioFiles: 0, chatCount: 0 })]
    const w = mountList()

    expect(w.findAll('.meeting-row')).toHaveLength(1)
    expect(w.find('.meeting-row__title').text()).toBe(enMessages.meeting.list.untitled)
    expect(w.text()).toContain(enMessages.meeting.list.noArtifacts)
  })

  it('shows the duration only when the meeting ended', () => {
    sessions.value = [record(), record({ token: 'live', endedAt: 0 })]
    const w = mountList()

    expect(w.text()).toContain('48 min')
    expect(w.text()).toContain(enMessages.meeting.list.running)
  })

  it('puts both entry points in the footer', async () => {
    const w = mountList()

    await w.find('[data-action="meeting-create"]').trigger('click')
    await w.find('[data-action="meeting-join"]').trigger('click')

    expect(w.emitted('create')).toHaveLength(1)
    expect(w.emitted('join')).toHaveLength(1)
  })

  it('opens a record by token', async () => {
    sessions.value = [record()]
    const w = mountList()

    await w.find('.meeting-row').trigger('click')
    expect(w.emitted('open')?.[0]).toEqual(['tok-1'])
  })

  it('asks before deleting, then removes the record', async () => {
    sessions.value = [record()]
    const w = mountList()

    await w.find('[data-action="meeting-delete-tok-1"]').trigger('click')
    await flushPromises()

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(removeMock).toHaveBeenCalledWith('tok-1')
  })

  it('does not delete when the confirmation is refused', async () => {
    confirmMock.mockResolvedValueOnce(false)
    sessions.value = [record()]
    const w = mountList()

    await w.find('[data-action="meeting-delete-tok-1"]').trigger('click')
    await flushPromises()

    expect(removeMock).not.toHaveBeenCalled()
  })
})

describe('MeetingList rows', () => {
  function pillTexts(w: ReturnType<typeof mountList>): string[] {
    return w.findAll('.meeting-pill').map((p) => p.text())
  }

  it('separates the lifecycle from the artifacts', () => {
    // An ended meeting has no lifecycle left to report, so the only pills are
    // what it produced. The old single chip forced the two facts to compete.
    sessions.value = [record()]
    const w = mountList()

    expect(pillTexts(w)).toEqual([
      enMessages.meeting.list.kindVideo,
      enMessages.meeting.list.pillMinutes,
      enMessages.meeting.list.pillTranscript,
    ])
    expect(w.find('.meeting-row').attributes('data-state')).toBe('')
  })

  it('marks a failed minutes job instead of calling it "no artifacts"', () => {
    // A dead job leaves no minutes, so without this branch the row reads as a
    // calm empty record — the one reading that is actively wrong.
    sessions.value = [record({ status: 'failed', hasMinutes: false, hasTranscript: false })]
    const w = mountList()

    expect(pillTexts(w)).toContain(enMessages.meeting.list.failed)
    expect(w.find('.meeting-row').attributes('data-state')).toBe('failed')
  })

  it('marks a generating job that died with the process as interrupted', () => {
    sessions.value = [
      record({ status: 'generating', hasMinutes: false, updatedAt: Date.now() - 11 * 60 * 1000 }),
    ]
    const w = mountList()

    expect(pillTexts(w)).toContain(enMessages.meeting.list.stale)
    expect(w.find('.meeting-row').attributes('data-state')).toBe('stale')
  })

  it('keeps "generating" for a job that is actually still running', () => {
    sessions.value = [record({ status: 'generating', hasMinutes: false, updatedAt: Date.now() })]
    const w = mountList()

    expect(pillTexts(w)).toContain(enMessages.meeting.list.generating)
    expect(w.find('.meeting-row').attributes('data-state')).toBe('generating')
  })

  it('tells a voice meeting from a video one', () => {
    sessions.value = [
      record({ token: 'v', kind: 'video' }),
      record({ token: 'a', kind: 'voice' }),
      // A record written before the column existed: no pill, no guess.
      record({ token: 'old', kind: '' }),
    ]
    const w = mountList()

    const kinds = w.findAll('.meeting-row').map((r) => r.find('.meeting-pill').text())
    expect(kinds).toEqual([
      enMessages.meeting.list.kindVideo,
      enMessages.meeting.list.kindVoice,
      // The "old" row falls through to its first artifact pill.
      enMessages.meeting.list.pillMinutes,
    ])
    expect(w.findAll('.meeting-row')[0].find('.meeting-pill').classes()).toContain('is-video')
    expect(w.findAll('.meeting-row')[1].find('.meeting-pill').classes()).toContain('is-voice')
  })

  it('surfaces the revised draft and the compact counts', () => {
    sessions.value = [record({ hasRevised: true, audioFiles: 12, chatCount: 4, speakerCount: 3 })]
    const w = mountList()

    const meta = w.find('.meeting-row__meta').text()
    expect(meta).toContain('12 clips')
    expect(meta).toContain('4 msgs')
    expect(meta).toContain('3 people')
    // Revised is an artifact, not a number, so it is a pill on the third line.
    expect(pillTexts(w)).toContain(enMessages.meeting.list.pillRevised)
  })

  it('shows one recording instead of a clip count once the audio is merged', () => {
    sessions.value = [record({ hasMeetingAudio: true, audioFiles: 2 })]
    const w = mountList()

    const meta = w.find('.meeting-row__meta').text()
    expect(meta).toContain(enMessages.meeting.list.audioMerged)
    // "2 clips" would be the channel count, which means nothing to the user.
    expect(meta).not.toContain('2 clips')
  })

  it('hides the counts a record does not have', () => {
    sessions.value = [record({ audioFiles: 0, chatCount: 0, speakerCount: 0, hasRevised: false })]
    const w = mountList()

    const meta = w.find('.meeting-row__meta').text()
    expect(meta).not.toContain('clips')
    expect(meta).not.toContain('msgs')
    expect(meta).not.toContain('people')
    expect(pillTexts(w)).not.toContain(enMessages.meeting.list.pillRevised)
  })

  it('says "no artifacts" only for a meeting that has ENDED empty', () => {
    // A running meeting has simply not produced anything yet.
    sessions.value = [
      record({ token: 'ended', hasMinutes: false, hasTranscript: false, hasRevised: false }),
      record({ token: 'live', endedAt: 0, hasMinutes: false, hasTranscript: false, hasRevised: false }),
    ]
    const w = mountList()

    const rows = w.findAll('.meeting-row')
    expect(rows[0].text()).toContain(enMessages.meeting.list.noArtifacts)
    expect(rows[1].text()).not.toContain(enMessages.meeting.list.noArtifacts)
  })
})

/**
 * jsdom applies no stylesheets, so "is the delete button visible" and "did the
 * chevron go away" cannot be asserted on the DOM. Read the source instead.
 */
describe('MeetingList row affordances (source)', () => {
  const src = readFileSync(join(__dirname, '..', 'MeetingList.vue'), 'utf8')

  it('keeps the delete button visible without hover', () => {
    const block = src.match(/\.meeting-row__del \{([^}]*)\}/)?.[1] ?? ''
    expect(block).not.toBe('')
    // The old rule was `opacity: 0` + `.meeting-row:hover .meeting-row__del`,
    // which made delete unreachable on a phone.
    expect(block).not.toContain('opacity')
    expect(src).not.toMatch(/\.meeting-row:hover \.meeting-row__del\s*\{[^}]*opacity/)
  })

  it('does not render a chevron next to the row', () => {
    expect(src).not.toContain('ChevronRight')
  })
})
