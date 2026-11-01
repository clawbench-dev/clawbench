import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
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
    CalendarDays: stub,
    Check: stub,
    ChevronRight: stub,
    Link: stub,
    Plus: stub,
    RefreshCw: stub,
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
const removeMock = vi.hoisted(() => vi.fn(async () => ({ ok: true })))
vi.mock('@/composables/useMeetingHistory', () => ({
  useMeetingHistory: () => ({
    sessions,
    loading: ref(false),
    error: ref(''),
    load: vi.fn(async () => undefined),
    remove: removeMock,
  }),
}))

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
    updatedAt: 1_700_002_880_000,
    ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  sessions.value = []
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
