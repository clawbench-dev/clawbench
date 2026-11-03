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
    ImageIcon: stub,
    MessageSquare: stub,
    MessagesSquare: stub,
    MonitorUp: stub,
    SpellCheck: stub,
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
const shotsMock = vi.hoisted(() => vi.fn(async () => []))
const deleteShotMock = vi.hoisted(() => vi.fn(async () => true))
const regenPlanMock = vi.hoisted(() => vi.fn())
const regenerateMock = vi.hoisted(() => vi.fn())
const cancelRegenMock = vi.hoisted(() => vi.fn(async () => true))
vi.mock('@/composables/useMeetingHistory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useMeetingHistory')>()
  return {
    ...actual,
    useMeetingHistory: () => ({
      get: getMock,
      artifact: artifactMock,
      chat: chatMock,
      shots: shotsMock,
      deleteShot: deleteShotMock,
      remove: removeMock,
      regenPlan: regenPlanMock,
      regenerate: regenerateMock,
      cancelRegeneration: cancelRegenMock,
    }),
  }
})

type DetailTab = 'resources' | 'minutes' | 'transcript' | 'revised' | 'chat'

/**
 * The record opens on 资源 by default, so a test that is about another pane has
 * to say so explicitly — otherwise it silently asserts against the resources
 * tab (which is exactly the kind of test that passes for the wrong reason).
 */
function mountDetail(initialTab?: DetailTab) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingDetail, {
    props: initialTab ? { token: 'tok-1', initialTab } : { token: 'tok-1' },
    global: {
      plugins: [i18n],
      stubs: {
        LoadingIndicator: true,
        BottomSheet: true,
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

function artifact(over: Record<string, unknown> = {}) {
  return {
    hasContent: true,
    jobStatus: 'idle',
    contentAt: 1_700_002_880_000,
    stale: false,
    ...over,
  }
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
    hasSegments: true,
    hasAudio: true,
    artifacts: {
      minutes: artifact(),
      transcript: artifact(),
      revised: artifact({ hasContent: false, contentAt: 0 }),
    },
    generation: null,
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

/** One stored proofread transcript, exactly as the server writes it. */
const REVISED = `# 校对版转写 · Release review · 2026-11-01 14:30

> 已结合项目上下文修正听写错误与专有名词；时间戳、说话人、行序均未改动。

[00:00] 我：hi
[00:30] 文件：打开 a.md
[00:40] 屏幕：小芳 开始共享屏幕
`

beforeEach(() => {
  vi.clearAllMocks()
  getMock.mockResolvedValue(record())
  artifactMock.mockImplementation(async (_token: string, kind: string) => {
    if (kind === 'minutes') return { content: '# MINUTES', file: 'minutes.md' }
    if (kind === 'transcript') return { content: VTT, file: 'transcript.vtt' }
    return null
  })
  shotsMock.mockResolvedValue([])
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
    const w = mountDetail('minutes')
    await flushPromises()

    expect(w.find('.meeting-md').html()).toContain('MINUTES')
    // The minutes tab no longer duplicates the artifact list: that moved to
    // the resources tab.
    expect(w.findAll('.meeting-file')).toHaveLength(0)
  })

  it('opens on the resources tab, first in the bar', async () => {
    const w = mountDetail()
    await flushPromises()

    const tabs = w.findAll('.meeting-tab').map((tab) => tab.attributes('data-tab'))
    expect(tabs).toEqual(['resources', 'minutes', 'transcript', 'revised', 'chat'])
    // Default: the resource pane is already the one rendered.
    expect(w.find('.meeting-tab--on').attributes('data-tab')).toBe('resources')
    expect(w.find('.meeting-resources-stub').exists()).toBe(true)
  })

  it('shows the AI proofread transcript as the same speaker rows as the transcript', async () => {
    getMock.mockResolvedValue(record({ hasRevised: true }))
    artifactMock.mockImplementation(async (_token: string, kind: string) => {
      if (kind === 'minutes') return { content: '# MINUTES', file: 'minutes.md' }
      if (kind === 'transcript') return { content: VTT, file: 'transcript.vtt' }
      if (kind === 'revised') return { content: REVISED, file: 'transcript-revised.md' }
      return null
    })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })

    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="revised"]').trigger('click')

    const pane = w.find('[data-role="revised"]')
    const rows = pane.findAll('.transcript__row')
    expect(rows).toHaveLength(3)
    expect(rows[0].find('.transcript__speaker').text()).toBe('我')
    expect(rows[0].find('.transcript__text').text()).toBe('hi')
    // File and screen events keep their own glyph rows, not a "文" avatar.
    expect(rows[1].classes()).toContain('transcript__row--file')
    expect(rows[1].find('.transcript__speaker').text()).toBe('文件')
    expect(rows[2].find('.transcript__speaker').text()).toBe('屏幕')
    // The heading and the note are document chrome, not conversation.
    expect(pane.text()).not.toContain('校对版转写')
    expect(pane.text()).not.toContain('已结合项目上下文')

    // Copy takes the proofread text — and the button says what it copies
    // instead of claiming it copies the minutes.
    const copyBtn = w.findAll('.meeting-footer .fbtn').find((b) => b.text() === enMessages.common.copy)
    expect(copyBtn).toBeTruthy()
    await copyBtn!.trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('我：hi\n\n文件：打开 a.md\n\n屏幕：小芳 开始共享屏幕')
  })

  it('says there is no proofread transcript when the meeting has none', async () => {
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="revised"]').trigger('click')

    const card = w.find('[data-role="revised-empty"]')
    expect(card.exists()).toBe(true)
    expect(card.find('.meeting-empty__title').text()).toBe(enMessages.meeting.detail.emptyRevisedTitle)
    expect(card.find('.meeting-empty__desc').text()).toBe(enMessages.meeting.detail.emptyRevisedDesc)
  })

  it('already shows the proofread transcript while the minutes are still generating', async () => {
    // Proofreading runs BEFORE summarisation, so its file exists while the
    // minutes are still being written. The tab must show it instead of a
    // spinner that hides work already done.
    getMock.mockResolvedValue(record({ hasRevised: true }))
    artifactMock.mockImplementation(async (_token: string, kind: string) =>
      kind === 'revised' ? { content: REVISED, file: 'transcript-revised.md' } : null,
    )

    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="revised"]').trigger('click')
    expect(w.findAll('[data-role="revised"] .transcript__row')).toHaveLength(3)
    expect(w.find('.meeting-generating').exists()).toBe(false)

    w.unmount()
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
    getMock.mockResolvedValue(record({
      artifacts: {
        minutes: artifact({ jobStatus: 'failed', errorCode: 'interrupted', error: 'interrupted' }),
        transcript: artifact(),
        revised: artifact({ hasContent: false, contentAt: 0 }),
      },
    }))
    const w = mountDetail('minutes')
    await flushPromises()

    expect(w.text()).toContain(enMessages.meeting.detail.artifact.interrupted)
    expect(w.find('.meeting-generating').exists()).toBe(false)
  })

  it('shows a spinner while the job is genuinely running', async () => {
    getMock.mockResolvedValue(record({
      hasMinutes: false,
      artifacts: {
        minutes: artifact({ hasContent: false, jobStatus: 'running', contentAt: 0 }),
        transcript: artifact(),
        revised: artifact({ hasContent: false, contentAt: 0 }),
      },
      generation: { step: 'minutes', remaining: [], startedAt: Date.now() },
    }))
    artifactMock.mockResolvedValue(null)
    const w = mountDetail('minutes')
    await flushPromises()

    expect(w.find('.meeting-generating').exists()).toBe(true)
  })

  it('treats a meeting with no artifacts as a normal empty record', async () => {
    getMock.mockResolvedValue(
      record({ hasMinutes: false, hasTranscript: false, hasRevised: false, audioFiles: 0, dirPath: undefined }),
    )
    artifactMock.mockResolvedValue(null)
    const w = mountDetail('minutes')
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
      enMessages.meeting.detail.emptyTranscriptTitle
        + enMessages.meeting.detail.emptyTranscriptDesc
        + enMessages.meeting.detail.artifact.reasr,
    )
  })

  it('says the transcript is still coming right after a meeting ends', async () => {
    // The publisher writes transcript.vtt only after draining its upload queue
    // (up to 3 minutes), so for a while the file legitimately does not exist.
    // "听记没有开启" was a lie in exactly the window a reader opens this tab.
    getMock.mockResolvedValue(
      record({ hasTranscript: false, hasMinutes: false, endedAt: Date.now() - 60_000 }),
    )
    artifactMock.mockResolvedValue(null)
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')
    await flushPromises()

    const card = w.find('[data-role="transcript-pending"]')
    expect(card.exists()).toBe(true)
    expect(card.find('.meeting-empty__title').text()).toBe(enMessages.meeting.detail.transcriptPendingTitle)
    expect(card.find('.meeting-empty__desc').text()).toBe(enMessages.meeting.detail.transcriptPendingDesc)
    // It must not also claim the notes were off.
    expect(w.text()).not.toContain(enMessages.meeting.detail.emptyTranscriptDesc)

    w.unmount()
  })

  it('falls back to "no transcript" once the settling window has passed', async () => {
    // Same absent file, but the meeting ended long ago: nothing is coming.
    getMock.mockResolvedValue(record({ hasTranscript: false }))
    artifactMock.mockResolvedValue(null)
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')
    await flushPromises()

    expect(w.find('[data-role="transcript-pending"]').exists()).toBe(false)
    expect(w.text()).toContain(enMessages.meeting.detail.emptyTranscriptTitle)

    w.unmount()
  })

  it('does not blame the setting when the transcript file exists but cannot be read', async () => {
    // `hasTranscript` comes from probing the folder, so the file IS there; an
    // empty list means the artifact request failed (or the file has no cue we
    // can draw). Telling the reader AI notes were off sends them to a setting
    // that was on.
    artifactMock.mockImplementation(async (_token: string, kind: string) =>
      kind === 'minutes' ? { content: '# MINUTES', file: 'minutes.md' } : { content: '', file: 'transcript.vtt' },
    )
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')
    await flushPromises()

    const card = w.find('[data-role="transcript-unreadable"]')
    expect(card.exists()).toBe(true)
    expect(card.find('.meeting-empty__title').text()).toBe(enMessages.meeting.detail.transcriptUnreadableTitle)
    expect(w.text()).not.toContain(enMessages.meeting.detail.emptyTranscriptDesc)

    w.unmount()
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

  it('stops polling when it is closed while a poll is in flight', async () => {
    // `onBeforeUnmount` only clears the timer that is already scheduled. A load
    // that is in flight at that moment used to return, find the record still
    // `generating`, and re-arm the timer — so a closed view kept requesting
    // record + minutes + transcript + chat every 2.5 s until the process ended.
    vi.useFakeTimers()
    const rec = record({ hasMinutes: false, generation: { step: 'minutes', remaining: [], startedAt: Date.now() } })
    let releasePoll: (() => void) | null = null
    getMock
      .mockImplementationOnce(async () => rec)
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            releasePoll = () => resolve(rec)
          }),
      )

    const w = mountDetail()
    try {
      await flushPromises()
      await vi.advanceTimersByTimeAsync(2500)
      expect(getMock).toHaveBeenCalledTimes(2) // the poll really is in flight

      w.unmount()
      releasePoll?.()
      await flushPromises()

      await vi.advanceTimersByTimeAsync(2500 * 4)
      expect(getMock).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it('polls a just-ended meeting without blanking the panel', async () => {
    // The minutes job is requested by the host after its upload queue drains,
    // so a freshly ended record sits at `idle` for a while and this view polls
    // it. Each tick used to set `loading`, replacing the whole panel below the
    // header with a centered spinner — the reported "it refreshes itself every
    // few seconds". Polling must be silent; only the first paint may show it.
    vi.useFakeTimers()
    const now = Date.now()
    const rec = record({
      hasMinutes: false,
      endedAt: now - 60_000,
      updatedAt: now,
      artifacts: {
        minutes: artifact({ hasContent: false, contentAt: 0 }),
        transcript: artifact(),
        revised: artifact({ hasContent: false, contentAt: 0 }),
      },
    })
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

    const w = mountDetail('minutes')
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

describe('MeetingDetail regeneration', () => {
  it('opens the confirmation sheet with the server plan and starts the job', async () => {
    getMock.mockResolvedValue(record({
      hasMinutes: false,
      artifacts: {
        minutes: artifact({ hasContent: false, contentAt: 0 }),
        transcript: artifact(),
        revised: artifact({ hasContent: false, contentAt: 0 }),
      },
    }))
    artifactMock.mockResolvedValue(null)
    regenPlanMock.mockResolvedValue({
      target: 'minutes',
      steps: [{ kind: 'minutes', mode: 'model', reason: 'target' }],
      warnings: [],
      blocked: null,
    })
    const next = record({ generation: { step: 'minutes', remaining: [], startedAt: Date.now() } })
    regenerateMock.mockResolvedValue({ ok: true, session: next })

    const w = mountDetail('minutes')
    await flushPromises()

    await w.find('[data-action="meeting-generate-minutes"]').trigger('click')
    await flushPromises()
    expect(regenPlanMock).toHaveBeenCalledWith('tok-1', 'minutes', true)

    const sheet = w.findComponent({ name: 'MeetingRegenerateSheet' })
    expect(sheet.exists()).toBe(true)
    await sheet.vm.$emit('confirm')
    await flushPromises()

    expect(regenerateMock).toHaveBeenCalledWith('tok-1', 'minutes', true)
    expect(sheet.props('open')).toBe(false)
    w.unmount()
  })

  it('asks the server to cancel a running job from the status bar', async () => {
    getMock.mockResolvedValue(record({
      hasMinutes: false,
      generation: { step: 'minutes', remaining: [], startedAt: Date.now() },
      artifacts: {
        minutes: artifact({ hasContent: false, jobStatus: 'running', contentAt: 0 }),
        transcript: artifact(),
        revised: artifact({ hasContent: false, contentAt: 0 }),
      },
    }))
    artifactMock.mockResolvedValue(null)

    const w = mountDetail('minutes')
    await flushPromises()

    await w.find('[data-action="meeting-artifact-cancel"]').trigger('click')
    await flushPromises()
    expect(cancelRegenMock).toHaveBeenCalledWith('tok-1')
    w.unmount()
  })
})

describe('MeetingDetail key frames', () => {
  it('merges frames into the transcript by offset and deletes one', async () => {
    shotsMock.mockResolvedValue([
      // Deliberately out of order: the view must sort, not append.
      { id: 7, at: 1_700_000_030_000, offsetMs: 30_000, reason: 'file', ref: 'docs/arch.md', fileName: 'shots/a.webp' },
      { id: 8, at: 1_700_000_005_000, offsetMs: 5_000, reason: 'manual', fileName: 'shots/b.webp' },
    ])
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')
    const rows = w.findAll('[data-role="shot-open"]')
    expect(rows).toHaveLength(2)

    // 5s comes before 30s, and the 0s utterance stays first.
    const labels = w.findAll('.transcript__shot-meta').map((n) => n.text())
    expect(labels[0]).toContain(enMessages.meeting.shots.reasonManual)
    expect(labels[1]).toContain('arch.md')

    const src = rows[0].find('img').attributes('src') ?? ''
    expect(src).toContain('/api/fs/thumb?target=')
    expect(decodeURIComponent(src)).toContain('shots/b.webp')

    await w.findAll('[data-role="shot-delete"]')[0].trigger('click')
    await flushPromises()
    expect(deleteShotMock).toHaveBeenCalledWith('tok-1', 8)
    expect(w.findAll('[data-role="shot-open"]')).toHaveLength(1)
  })

  it('does not claim "no transcript" when only frames exist', async () => {
    getMock.mockResolvedValue(record({ hasTranscript: false }))
    artifactMock.mockResolvedValue(null)
    shotsMock.mockResolvedValue([
      { id: 9, at: 1_700_000_002_000, offsetMs: 2_000, reason: 'manual', fileName: 'shots/c.webp' },
    ])
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')
    expect(w.find('[data-role="transcript"]').exists()).toBe(true)
    expect(w.find('[data-role="transcript-unreadable"]').exists()).toBe(false)
    expect(w.findAll('[data-role="shot-open"]')).toHaveLength(1)
  })
})
