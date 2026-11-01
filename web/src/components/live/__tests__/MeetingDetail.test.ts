import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MeetingDetail from '../MeetingDetail.vue'

/**
 * The detail view is the SAME screen for a meeting that just ended and one
 * clicked in the list. What matters here: the three tabs read three different
 * sources, chat comes from the database, and a job interrupted by a restart is
 * not left spinning forever.
 */

vi.mock('lucide-vue-next', () => {
  const stub = { render: () => null }
  return {
    ArrowLeft: stub,
    Captions: stub,
    Copy: stub,
    ExternalLink: stub,
    FileQuestion: stub,
    FileText: stub,
    MessageSquare: stub,
    MessagesSquare: stub,
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

vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))
// The record mirrors the reader's own lines by matching the stored speaker name
// against the local nickname; pin that nickname so the test is not dependent on
// whatever this machine's config says.
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({ serverConfig: { value: { user: { nickname: 'Alex' } } } }),
}))

// The resource list is its own composable with its own test; here it only
// needs to exist (the detail reads its count for the tab badge).
vi.mock('@/composables/useMeetingResources', () => ({
  useMeetingResources: () => ({
    groups: ref([]),
    loading: ref(false),
    error: ref(''),
    total: ref(0),
    reload: vi.fn(),
  }),
}))
vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

const confirmMock = vi.hoisted(() => vi.fn(async () => true))
vi.mock('@/composables/useDialog', () => ({ useDialog: () => ({ confirm: confirmMock }) }))

const getMock = vi.hoisted(() => vi.fn())
const artifactMock = vi.hoisted(() => vi.fn())
const chatMock = vi.hoisted(() => vi.fn())
const removeMock = vi.hoisted(() => vi.fn(async () => ({ ok: true })))
vi.mock('@/composables/useMeetingHistory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useMeetingHistory')>()
  return {
    ...actual,
    useMeetingHistory: () => ({
      get: getMock,
      artifact: artifactMock,
      chat: chatMock,
      remove: removeMock,
    }),
  }
})

function mountDetail() {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingDetail, {
    props: { token: 'tok-1' },
    global: {
      plugins: [i18n],
      stubs: {
        LoadingIndicator: true,
        SpeakerAvatar: { props: ['name'], template: '<span class="sa" :data-name="name" />' },
        // The real body mounts TableRowModal/CodeLinkPreview and the full
        // markdown pipeline; that wiring is covered by MeetingMarkdown.test.ts.
        MeetingMarkdown: { props: ['content', 'path'], template: '<div class="meeting-md">{{ content }}</div>' },
        MeetingResources: { props: ['groups', 'loading', 'error', 'total', 'folderPath'], template: '<div class="meeting-resources-stub" />' },
        TableRowModal: true,
        CodeLinkPreview: true,
      },
    },
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
    audioFiles: 3,
    chatCount: 2,
    status: 'ready',
    updatedAt: Date.now(),
    dirPath: '/p/.clawbench/notes/2026-11-01-1430-review-abc123',
    ...over,
  }
}

/** One stored transcript, exactly as the server writes it. */
const VTT = `WEBVTT

mic
00:00:00.000 --> 00:00:01.000
<v 我>hi
`

beforeEach(() => {
  vi.clearAllMocks()
  getMock.mockResolvedValue(record())
  artifactMock.mockImplementation(async (_token: string, kind: string) => {
    if (kind === 'minutes') return { content: '# MINUTES', file: 'minutes.md' }
    if (kind === 'transcript') return { content: VTT, file: 'transcript.vtt' }
    return null
  })
  chatMock.mockResolvedValue({
    messages: [
      { id: 1, clientId: 'c1', speaker: 'Alex', text: 'hello', at: 1_700_000_001_000 },
    ],
    count: 1,
  })
})

describe('MeetingDetail', () => {
  it('draws a card, not a stray sentence, when the record is gone', async () => {
    getMock.mockResolvedValue(null)
    const w = mountDetail()
    await flushPromises()

    const card = w.find('.meeting-empty')
    expect(card.exists()).toBe(true)
    expect(card.find('.meeting-empty__title').text()).toBe(enMessages.meeting.detail.notFoundTitle)
    expect(card.find('.meeting-empty__desc').text()).toBe(enMessages.meeting.detail.notFoundDesc)
    // No tabs for a record that is not there.
    expect(w.find('.meeting-tabs').exists()).toBe(false)
  })

  it('renders the minutes through the shared document body', async () => {
    const w = mountDetail()
    await flushPromises()

    expect(w.find('.meeting-md').html()).toContain('MINUTES')
    // The minutes tab no longer duplicates the artifact list: that moved to
    // the resources tab.
    expect(w.findAll('.meeting-file')).toHaveLength(0)
  })

  it('adds a resources tab alongside minutes, transcript and chat', async () => {
    const w = mountDetail()
    await flushPromises()

    const tabs = w.findAll('.meeting-tab').map((tab) => tab.attributes('data-tab'))
    expect(tabs).toEqual(['minutes', 'transcript', 'chat', 'resources'])
    await w.find('[data-tab="resources"]').trigger('click')
    expect(w.find('.meeting-resources-stub').exists()).toBe(true)
  })

  it('reads chat from the database and offers copy-all', async () => {
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="chat"]').trigger('click')
    expect(w.text()).toContain('hello')
    // Same row component as the live chat: avatar, name/time, bubble — and the
    // reader's own lines are mirrored, like every other chat.
    const rows = w.findAll('.chat-row')
    expect(rows).toHaveLength(1)
    expect(rows[0].find('.sa').attributes('data-name')).toBe('Alex')
    expect(rows[0].find('.chat-row__text').text()).toBe('hello')
    expect(rows[0].classes()).toContain('chat-row--self')
    expect(w.find('[data-tab="chat"]').text()).toContain('1')
    expect(w.findAll('.meeting-footer .fbtn').some((b) => b.text() === enMessages.meeting.detail.copyAll)).toBe(true)
  })

  it('says the job was interrupted instead of spinning forever', async () => {
    getMock.mockResolvedValue(
      record({ status: 'generating', updatedAt: Date.now() - 11 * 60 * 1000 }),
    )
    const w = mountDetail()
    await flushPromises()

    expect(w.text()).toContain(enMessages.meeting.detail.generatingStale)
    expect(w.find('.meeting-generating').exists()).toBe(false)
  })

  it('shows a spinner while the job is genuinely running', async () => {
    getMock.mockResolvedValue(record({ status: 'generating', updatedAt: Date.now() }))
    const w = mountDetail()
    await flushPromises()

    expect(w.find('.meeting-generating').exists()).toBe(true)
  })

  it('treats a meeting with no artifacts as a normal empty record', async () => {
    getMock.mockResolvedValue(
      record({ hasMinutes: false, hasTranscript: false, hasRevised: false, audioFiles: 0, dirPath: undefined }),
    )
    artifactMock.mockResolvedValue(null)
    const w = mountDetail()
    await flushPromises()

    expect(w.find('.meeting-empty').exists()).toBe(true)
    expect(w.text()).toContain(enMessages.meeting.detail.emptyMinutesTitle)
    expect(w.text()).toContain(enMessages.meeting.detail.emptyMinutesDesc)
    expect(w.findAll('.meeting-file')).toHaveLength(0)
  })

  it('renders only the empty state when a meeting has no transcript', async () => {
    // The template comment above the transcript <pre> used to contain the
    // two-dash arrow. An HTML comment ends at the first close marker, so the
    // rest of the comment ("` arrows. -->") leaked into the pane as visible
    // text above the empty state. Assert the pane contains exactly the card.
    getMock.mockResolvedValue(record({ hasTranscript: false }))
    artifactMock.mockImplementation(async (_token: string, kind: string) =>
      kind === 'minutes' ? { content: '# MINUTES', file: 'minutes.md' } : null,
    )
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')

    const pane = w.find('.meeting-body')
    expect(pane.find('.meeting-empty').exists()).toBe(true)
    // `textContent` concatenates the two block elements with no separator; the
    // exact comparison is what catches a leaked HTML comment.
    expect(pane.text().trim()).toBe(
      enMessages.meeting.detail.emptyTranscriptTitle + enMessages.meeting.detail.emptyTranscriptDesc,
    )
  })

  // The stored artifact is a subtitle track. The tab is a reading surface, so
  // the header, cue ids and timestamps must not reach it — and it is drawn as
  // the same speaker rows the live panel used, not as flattened text.
  it('shows the transcript as the same speaker rows, not as subtitle markup', async () => {
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')

    const pane = w.find('[data-role="transcript"]')
    const rows = pane.findAll('.transcript__row')
    expect(rows).toHaveLength(1)
    expect(rows[0].find('.transcript__speaker').text()).toBe('我')
    expect(rows[0].find('.transcript__text').text()).toBe('hi')
    expect(rows[0].find('.sa').attributes('data-name')).toBe('我')
    expect(pane.text()).not.toContain('WEBVTT')
    expect(pane.text()).not.toContain('<v')
    expect(pane.text()).not.toContain('00:00:00')
  })

  it('asks before deleting and then leaves the view', async () => {
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-action="meeting-delete"]').trigger('click')
    await flushPromises()

    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(removeMock).toHaveBeenCalledWith('tok-1')
    expect(w.emitted('back')).toHaveLength(1)
  })

  it('emits the token when quoting', async () => {
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-action="meeting-quote"]').trigger('click')
    expect(w.emitted('quote')?.[0]).toEqual(['tok-1'])
  })

  it('polls a just-ended meeting without blanking the panel', async () => {
    // The minutes job is requested by the host after its upload queue drains,
    // so a freshly ended record sits at `idle` for a while and this view polls
    // it. Each tick used to set `loading`, replacing the whole panel below the
    // header with a centered spinner — the reported "it refreshes itself every
    // few seconds". Polling must be silent; only the first paint may show it.
    vi.useFakeTimers()
    const now = Date.now()
    const rec = record({ status: 'idle', hasMinutes: false, endedAt: now - 60_000, updatedAt: now })
    // The poll's own request is held open so the assertion lands WHILE it is in
    // flight: `loading` only lives for a microtask, so checking after the tick
    // settles passes either way.
    let releasePoll: (() => void) | null = null
    getMock
      .mockImplementationOnce(async () => rec)
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            releasePoll = () => resolve(rec)
          }),
      )
    artifactMock.mockImplementation(async (_token: string, kind: string) =>
      kind === 'transcript' ? { content: VTT, file: 'transcript.vtt' } : null,
    )

    const w = mountDetail()
    try {
      expect(w.find('.meeting-center').exists()).toBe(true) // first paint shows loading

      await flushPromises()
      expect(w.find('.meeting-center').exists()).toBe(false)
      expect(getMock).toHaveBeenCalledTimes(1)

      await vi.advanceTimersByTimeAsync(2500)
      await nextTick()

      expect(getMock).toHaveBeenCalledTimes(2) // the poll really ran
      expect(w.find('.meeting-center').exists()).toBe(false)
      expect(w.find('.meeting-tabs').exists()).toBe(true)
      expect(w.find('.meeting-body').text()).toContain(enMessages.meeting.detail.emptyMinutesTitle)

      releasePoll?.()
      await flushPromises()
      expect(w.find('.meeting-center').exists()).toBe(false)
      expect(w.find('.meeting-tabs').exists()).toBe(true)
    } finally {
      w.unmount()
      vi.useRealTimers()
    }
  })
})

describe('MeetingDetail chat sides', () => {
  it('keeps other people on the left and the reader on the right', async () => {
    chatMock.mockResolvedValue({
      messages: [
        { id: 1, clientId: 'c1', speaker: '小明', text: '早', at: 1_700_000_001_000 },
        { id: 2, clientId: 'c2', speaker: 'Alex', text: '早啊', at: 1_700_000_002_000 },
      ],
      count: 2,
    })
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="chat"]').trigger('click')

    const rows = w.findAll('.chat-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].classes()).not.toContain('chat-row--self')
    expect(rows[1].classes()).toContain('chat-row--self')
  })
})
