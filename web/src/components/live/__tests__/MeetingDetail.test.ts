import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
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
  return { ArrowLeft: stub, Copy: stub, ExternalLink: stub, MessageSquare: stub, Trash2: stub }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/utils/attachDrag', () => ({
  cleanupDragGhost: vi.fn(),
  buildAttachDragImage: vi.fn(),
}))

vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))
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
    audioFiles: 3,
    chatCount: 2,
    status: 'ready',
    updatedAt: Date.now(),
    dirPath: '/p/.clawbench/notes/2026-11-01-1430-review-abc123',
    ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  getMock.mockResolvedValue(record())
  artifactMock.mockImplementation(async (_token: string, kind: string) => {
    if (kind === 'minutes') return { content: '# MINUTES', file: 'minutes.md' }
    if (kind === 'transcript') return { content: '1\n00:00 --> 00:01\nhi', file: 'transcript.srt' }
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
  it('renders the minutes and the artifacts that exist', async () => {
    const w = mountDetail()
    await flushPromises()

    expect(w.find('.meeting-md').html()).toContain('MINUTES')
    const files = w.findAll('.meeting-file')
    expect(files).toHaveLength(3) // minutes + transcript + audio, no revised
    expect(w.text()).toContain('minutes.md')
  })

  it('reads chat from the database and offers copy-all', async () => {
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="chat"]').trigger('click')
    expect(w.text()).toContain('hello')
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

    expect(w.text()).toContain(enMessages.meeting.detail.noMinutes)
    expect(w.findAll('.meeting-file')).toHaveLength(0)
  })

  it('renders only the empty hint when a meeting has no transcript', async () => {
    // The template comment above the transcript <pre> used to contain the
    // two-dash arrow. An HTML comment ends at the first close marker, so the
    // rest of the comment ("` arrows. -->") leaked into the pane as visible
    // text above the empty hint. Assert the pane contains exactly the hint.
    getMock.mockResolvedValue(record({ hasTranscript: false }))
    artifactMock.mockImplementation(async (_token: string, kind: string) =>
      kind === 'minutes' ? { content: '# MINUTES', file: 'minutes.md' } : null,
    )
    const w = mountDetail()
    await flushPromises()

    await w.find('[data-tab="transcript"]').trigger('click')

    expect(w.find('.meeting-body').text().trim()).toBe(enMessages.meeting.detail.noTranscript)
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
})
