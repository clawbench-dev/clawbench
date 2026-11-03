import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MeetingShareDetail from '../MeetingShareDetail.vue'
import type { ShareMeetingRecord } from '../liveNotesClient'

/**
 * The viewer's meeting record.
 *
 * What matters here is that a link opened AFTER the meeting still shows the
 * four surfaces the host sees (minutes, transcript, proofread, chat), that the
 * chat is the archived one from the server rather than whatever this page
 * happened to hold in memory, and that a deleted record says so instead of
 * rendering an empty shell.
 */

// Spread the real module: a narrow mock turns any icon a shared child adds
// (TranscriptList's lightbox view button, …) into `undefined` and the whole
// record fails to render.
vi.mock('lucide-vue-next', async (importOriginal) => {
  const actual = await importOriginal<typeof import('lucide-vue-next')>()
  const stub = { render: () => null }
  return {
    ...actual,
    Captions: stub,
    Copy: stub,
    FileQuestion: stub,
    FileText: stub,
    ImageIcon: stub,
    MessagesSquare: stub,
    MonitorUp: stub,
    SpellCheck: stub,
    Trash2: stub,
  }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))

const fetchRecordMock = vi.hoisted(() => vi.fn())
vi.mock('../liveNotesClient', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../liveNotesClient')>()
  return { ...actual, fetchMeetingRecord: fetchRecordMock }
})

function record(over: Partial<ShareMeetingRecord> = {}): ShareMeetingRecord {
  return {
    title: 'Release review',
    startedAt: 1_700_000_000_000,
    endedAt: 1_700_002_880_000,
    ended: true,
    segments: [
      { id: 1, channel: 'mic', speaker: '我', startedAt: 1_700_000_001_000, durationMs: 2000, text: 'hi' },
    ],
    files: [],
    shots: [],
    minutes: { status: 'ready', markdown: '# MINUTES' },
    chat: [{ speaker: 'Alex', text: 'hello', at: 1_700_000_001_000 }],
    ...over,
  }
}

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })

function mountDetail(props: Record<string, unknown> = {}) {
  return mount(MeetingShareDetail, {
    props,
    global: {
      plugins: [i18n],
      stubs: {
        LoadingIndicator: true,
        // The real button owns the theme popup; this view only has to place it.
        ShareThemeButton: { template: '<button class="theme-stub" />' },
        SpeakerAvatar: { props: ['name'], template: '<span class="sa" :data-name="name" />' },
        // The real body mounts the whole markdown pipeline; that wiring is
        // covered by MeetingMarkdown.test.ts.
        MeetingMarkdown: { props: ['content', 'path'], template: '<div class="meeting-md">{{ content }}</div>' },
        TableRowModal: true,
        CodeLinkPreview: true,
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.removeItem('clawbench_meeting_nick')
  fetchRecordMock.mockResolvedValue(record())
})

describe('MeetingShareDetail', () => {
  it('wears the public share topbar: brand mark, title, theme picker', async () => {
    const w = mountDetail({ title: 'Release review' })
    await flushPromises()

    // A member followed a link and is not inside the app: the page has to say
    // whose page it is, exactly like the file/session share pages do.
    const bar = w.find('.share-topbar')
    expect(bar.exists()).toBe(true)
    const logo = bar.find('img.share-logo')
    expect(logo.exists()).toBe(true)
    expect(logo.attributes('src')).toBe('/logo-64.png')
    expect(logo.attributes('alt')).toBe('ClawBench')
    expect(bar.find('[data-role="record-title"]').text()).toBe('Release review')
    expect(bar.find('.theme-stub').exists()).toBe(true)
  })

  it('names the record and marks it as ended', async () => {
    const w = mountDetail({ title: 'Release review' })
    await flushPromises()

    expect(w.find('[data-role="record-title"]').text()).toBe('Release review')
    expect(w.find('[data-role="meeting-ended"]').text()).toBe(enMessages.meeting.share.ended)
    // Date + duration, from the record itself.
    expect(w.find('.share-meeting__meta').text()).toContain('48')
  })

  it('shows the minutes first, with all four tabs', async () => {
    const w = mountDetail()
    await flushPromises()

    const tabs = w.findAll('.share-meeting__tab').map((tab) => tab.attributes('data-tab'))
    expect(tabs).toEqual(['minutes', 'transcript', 'revised', 'chat'])
    expect(w.find('.meeting-md').html()).toContain('MINUTES')
  })

  it('draws the stored transcript as speaker rows', async () => {
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')

    const rows = w.findAll('[data-role="transcript"] .transcript__row')
    expect(rows).toHaveLength(1)
    expect(rows[0].find('.transcript__speaker').text()).toBe('我')
    expect(rows[0].find('.transcript__text').text()).toBe('hi')
  })

  it('draws the meeting frames inline, and offers no delete', async () => {
    fetchRecordMock.mockResolvedValue(
      record({ shots: [{ id: 8, at: 1_700_000_001_500, reason: 'manual' }] }),
    )
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')

    // The same row component the host's record draws: a member reads the
    // timeline with the pictures in it, not a transcript without them.
    const shot = w.find('[data-role="transcript"] [data-role="shot-open"]')
    expect(shot.exists()).toBe(true)
    expect(shot.find('img').attributes('src')).toContain('notes/shot?id=8')
    // Deleting is the publisher's alone.
    expect(w.find('[data-role="shot-delete"]').exists()).toBe(false)
  })

  it('draws the proofread transcript from the record payload', async () => {
    fetchRecordMock.mockResolvedValue(
      record({ minutes: { status: 'ready', markdown: '# MINUTES', revise: { status: 'ready', text: '[00:00] 我：hi' } } }),
    )
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="revised"]').trigger('click')

    const rows = w.findAll('[data-role="revised"] .transcript__row')
    expect(rows).toHaveLength(1)
    expect(rows[0].find('.transcript__speaker').text()).toBe('我')
  })

  it('reads the archived chat from the server and mirrors the viewer’s own lines', async () => {
    localStorage.setItem('clawbench_meeting_nick', 'Alex')
    fetchRecordMock.mockResolvedValue(
      record({
        chat: [
          { speaker: '小明', text: '早', at: 1_700_000_001_000 },
          { speaker: 'Alex', text: '早啊', at: 1_700_000_002_000 },
        ],
      }),
    )
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="chat"]').trigger('click')

    const rows = w.findAll('.chat-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].classes()).not.toContain('chat-row--self')
    expect(rows[1].classes()).toContain('chat-row--self')
    expect(w.find('[data-tab="chat"]').text()).toContain('2')
  })

  it('says the record is gone when the token no longer resolves', async () => {
    fetchRecordMock.mockResolvedValue(null)
    const w = mountDetail()
    await flushPromises()

    const card = w.find('[data-role="meeting-missing"]')
    expect(card.exists()).toBe(true)
    expect(card.text()).toContain(enMessages.meeting.detail.notFoundTitle)
    expect(w.find('.share-meeting__tabs').exists()).toBe(false)
  })

  it('keeps polling while the minutes are still being generated', async () => {
    vi.useFakeTimers()
    // Just ended: the job is alive, not a leftover from a crashed process.
    fetchRecordMock.mockResolvedValue(
      record({ endedAt: Date.now(), minutes: { status: 'generating' } }),
    )
    const w = mountDetail()
    try {
      await flushPromises()
      expect(fetchRecordMock).toHaveBeenCalledTimes(1)
      expect(w.text()).toContain(enMessages.liveShare.notes.generating)

      await vi.advanceTimersByTimeAsync(2500)
      expect(fetchRecordMock).toHaveBeenCalledTimes(2)
    } finally {
      w.unmount()
      vi.useRealTimers()
    }
  })

  it('stops polling a record that has settled', async () => {
    vi.useFakeTimers()
    const w = mountDetail()
    try {
      await flushPromises()
      expect(fetchRecordMock).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(2500 * 4)
      expect(fetchRecordMock).toHaveBeenCalledTimes(1)
    } finally {
      w.unmount()
      vi.useRealTimers()
    }
  })
})
