import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { nextTick, ref } from 'vue'
import { MonitorUp } from 'lucide-vue-next'
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
  // Keep the real helpers (the state labels live in i18n); only the
  // composable's I/O is stubbed.
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

const mountedWrappers: Array<{ unmount: () => void }> = []

function mountList(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  const wrapper = mount(MeetingList, {
    props: { active: true, ...props },
    global: { plugins: [i18n], stubs: { LoadingIndicator: true } },
  })
  mountedWrappers.push(wrapper)
  return wrapper
}

afterEach(() => {
  // A mounted list keeps a poll timer while any row is generating. Without
  // unmounting, every later test inherits it (and the shared sessions ref).
  while (mountedWrappers.length) mountedWrappers.pop()?.unmount()
  vi.useRealTimers()
})

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
    audioFiles: 12,
    chatCount: 4,
    hasSegments: true,
    hasAudio: true,
    artifacts: {
      minutes: artifact(),
      transcript: artifact(),
      revised: artifact({ hasContent: false, contentAt: 0 }),
    },
    generation: null,
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

  /**
   * The list is reachable mid-meeting now, which makes it a place the host
   * VISITS. The chip is the way back; without it the trip is one-way.
   */
  it('offers the way back to a running meeting, and only while it runs', async () => {
    const idle = mountList()
    expect(idle.find('[data-action="meeting-resume"]').exists()).toBe(false)

    const live = mountList({ liveToken: 'tok-1' })
    const chip = live.find('.meeting-header [data-action="meeting-resume"]')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).toBe(enMessages.meeting.list.liveResume)

    await chip.trigger('click')
    expect(live.emitted('resume')).toHaveLength(1)
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
  it('draws the shared empty-state card instead of a bare line', () => {
    // Other dock tabs (git history, forge, tasks) all answer "nothing here yet"
    // with a centred glyph + title + one line of context. The list used to be a
    // 28px icon over a sentence, stuck to the top of the scroll area.
    const w = mountList()

    const card = w.find('.meeting-empty')
    expect(card.exists()).toBe(true)
    expect(card.findComponent(MonitorUp).exists()).toBe(true)
    expect(card.find('.meeting-empty__title').text()).toBe(enMessages.meeting.list.empty)
    expect(card.find('.meeting-empty__desc').text()).toBe(enMessages.meeting.list.emptyDesc)
  })

  it('prints no hint line under the list', () => {
    // The old footer explained where records come from ("this project's meeting
    // database") on every visit. It told the reader nothing they could act on.
    sessions.value = [record()]
    const w = mountList()

    expect(w.find('.meeting-hint').exists()).toBe(false)
  })

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
      enMessages.meeting.list.pillMinutes,
      enMessages.meeting.list.pillTranscript,
    ])
    expect(w.find('.meeting-row').attributes('data-state')).toBe('')
  })

  it('marks a failed minutes job instead of calling it "no artifacts"', () => {
    // A dead job leaves no minutes, so without this branch the row reads as a
    // calm empty record — the one reading that is actively wrong.
    sessions.value = [record({
      hasMinutes: false,
      hasTranscript: false,
      artifacts: {
        minutes: artifact({ hasContent: false, jobStatus: 'failed', contentAt: 0 }),
        transcript: artifact({ hasContent: false, contentAt: 0 }),
        revised: artifact({ hasContent: false, contentAt: 0 }),
      },
    })]
    const w = mountList()

    expect(pillTexts(w)).toContain(enMessages.meeting.list.failed)
    expect(w.find('.meeting-row').attributes('data-state')).toBe('failed')
  })

  it('marks an interrupted job as failed (the server normalizes it)', () => {
    sessions.value = [record({
      hasMinutes: false,
      artifacts: {
        minutes: artifact({ hasContent: false, jobStatus: 'failed', errorCode: 'interrupted', contentAt: 0 }),
        transcript: artifact(),
        revised: artifact({ hasContent: false, contentAt: 0 }),
      },
    })]
    const w = mountList()

    expect(pillTexts(w)).toContain(enMessages.meeting.list.failed)
    expect(w.find('.meeting-row').attributes('data-state')).toBe('failed')
  })

  it('marks a downstream artifact that is behind its upstream as needing an update', () => {
    sessions.value = [record({
      artifacts: {
        minutes: artifact({ stale: true }),
        transcript: artifact(),
        revised: artifact(),
      },
    })]
    const w = mountList()

    expect(pillTexts(w)).toContain(enMessages.meeting.list.updating)
    expect(w.find('.meeting-row').attributes('data-state')).toBe('stale')
  })

  it('keeps "generating" for a job that is actually still running', () => {
    sessions.value = [record({
      hasMinutes: false,
      generation: { step: 'minutes', remaining: [], startedAt: Date.now() },
    })]
    const w = mountList()

    expect(pillTexts(w)).toContain(enMessages.meeting.list.regenerating.replace('{kind}', enMessages.meeting.detail.tabMinutes))
    expect(w.find('.meeting-row').attributes('data-state')).toBe('generating')
  })

  it('draws no meeting-type badge: meetings have no type', () => {
    // The record no longer carries a kind at all; a badge would have nothing
    // left to draw, and the title starts the row in the slot it used to take.
    sessions.value = [record({ token: 'v' }), record({ token: 'a' })]
    const w = mountList()

    expect(w.find('.meeting-kind').exists()).toBe(false)
    const head = w.findAll('.meeting-row')[0].find('.meeting-row__head')
    expect(head.element.firstElementChild?.classList.contains('meeting-row__title')).toBe(true)
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
 * The meta line's first item used to be a raw `11/01 17:35`. It now speaks the
 * app's relative language, with one exception: past a day the shared formatter
 * says "1 day ago", and yesterday is a thing the reader can name.
 *
 * The clock is pinned — every expectation below is a calendar claim (is this
 * yesterday, or two days ago?), so a real `Date.now()` would make the result
 * depend on what time of day the suite happens to run.
 */
describe('MeetingList time column', () => {
  const NOON = new Date(2026, 10, 1, 10, 0, 0) // Nov 1 2026, 10:00 local

  beforeEach(() => {
    // Only the clock is faked, so nothing else in the component's lifecycle
    // (its own interval, vue-i18n's handshake) changes behaviour.
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(NOON)
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  function timeOf(over: Record<string, unknown>) {
    sessions.value = [record(over)]
    return mountList().find('.meeting-row__m')
  }

  it('counts minutes and hours inside the day', () => {
    expect(timeOf({ startedAt: NOON.getTime() - 5 * 60_000 }).text()).toBe('5 min ago')
    expect(timeOf({ startedAt: NOON.getTime() - 3 * 3_600_000 }).text()).toBe('3 hr ago')
  })

  it('names yesterday instead of "1 day ago"', () => {
    expect(timeOf({ startedAt: new Date(2026, 9, 31, 9, 0).getTime() }).text()).toBe('Yesterday')
  })

  it('counts days up to a week, then falls back to the date', () => {
    expect(timeOf({ startedAt: new Date(2026, 9, 30, 9, 0).getTime() }).text()).toBe('2 days ago')
    // Ten days out the shared formatter has run out of relative words.
    expect(timeOf({ startedAt: new Date(2026, 9, 22, 9, 0).getTime() }).text()).toBe('10/22/2026')
  })

  it('keeps the exact clock reachable as a tooltip', () => {
    // The visible label no longer carries the hour, so the tooltip is the only
    // place left that can answer "when exactly was this meeting?".
    expect(timeOf({ startedAt: new Date(2026, 9, 30, 9, 0).getTime() }).attributes('title'))
      .toBe('2026-10-30 09:00:00')
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

  it('starts all three tiers at the card edge', () => {
    // The meta and tags lines used to be indented by the width of the
    // meeting-type badge, so the three tiers lined up under the title's text.
    // The badge is gone (see the DOM test above); an indent left behind would
    // put an empty column in front of those two lines.
    const blocks = src.match(/\.[\w-]+(?:\s*,\s*\.[\w-]+)*\s*\{[^}]*\}/g) ?? []
    expect(blocks.length).toBeGreaterThan(0)
    const indented = blocks.filter(
      (b) => /\.meeting-row__(meta|tags)/.test(b) && /padding-left/.test(b),
    )
    expect(indented).toEqual([])
  })
})

describe('MeetingList regeneration polling', () => {
  it('polls only while a row is generating and stops when it settles', async () => {
    vi.useFakeTimers()
    try {
      sessions.value = [record({
        hasMinutes: false,
        generation: { step: 'minutes', remaining: [], startedAt: Date.now() },
      })]
      const w = mountList()
      await flushPromises()
      expect(loadMock).toHaveBeenCalledTimes(1)

      vi.advanceTimersByTime(5000)
      await flushPromises()
      expect(loadMock).toHaveBeenCalledTimes(2)

      sessions.value = [record({ generation: null })]
      await nextTick()
      vi.advanceTimersByTime(5000)
      await flushPromises()
      expect(loadMock).toHaveBeenCalledTimes(2)

    } finally {
      vi.useRealTimers()
    }
  })
})
