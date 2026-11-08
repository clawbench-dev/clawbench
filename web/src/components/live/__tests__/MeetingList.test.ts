import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { computed, nextTick, ref } from 'vue'
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
    ArrowLeft: stub,
    MonitorUp: stub,
    Check: stub,
    CheckCircle2: stub,
    Link: stub,
    MessageSquare: stub,
    Mic: stub,
    Plus: stub,
    Video: stub,
    RefreshCw: stub,
    RotateCw: stub,
    RotateCcw: stub,
    Trash2: stub,
    ShieldAlert: stub,
    Upload: stub,
  }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/utils/attachDrag', () => ({
  cleanupDragGhost: vi.fn(),
  buildAttachDragImage: vi.fn(),
}))

const toastShow = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: toastShow }) }))

const confirmMock = vi.hoisted(() => vi.fn(async () => true))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: confirmMock }),
}))

const sessions = ref<unknown[]>([])
const loading = ref(false)
const importAvailable = ref(true)
const loadMock = vi.hoisted(() => vi.fn(async () => undefined))
const removeMock = vi.hoisted(() => vi.fn(async () => ({ ok: true })))
const importMock = vi.hoisted(() => vi.fn(async () => ({ ok: true, token: 'imported-token' })))
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
      importAvailability: computed(() => ({ available: importAvailable.value, reason: '' })),
      load: loadMock,
      remove: removeMock,
      importFile: importMock,
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
   * VISITS. While this client is in a meeting the two entry actions are gone —
   * starting or joining a second meeting would strand the running one — and
   * the bar's only action is the way back.
   */
  it('replaces the entry actions with "back to meeting" while a meeting runs', async () => {
    const idle = mountList()
    expect(idle.find('[data-action="meeting-create"]').exists()).toBe(true)
    expect(idle.find('[data-action="meeting-join"]').exists()).toBe(true)
    expect(idle.find('[data-action="meeting-resume"]').exists()).toBe(false)

    const live = mountList({ liveToken: 'tok-1' })
    expect(live.find('[data-action="meeting-create"]').exists()).toBe(false)
    expect(live.find('[data-action="meeting-join"]').exists()).toBe(false)
    // Not in the header: the list's header is status only (title, count,
    // refresh), and the action belongs with the other footer actions.
    expect(live.find('.meeting-header [data-action="meeting-resume"]').exists()).toBe(false)

    const resume = live.find('.meeting-footer [data-action="meeting-resume"]')
    expect(resume.exists()).toBe(true)
    expect(resume.text()).toBe(enMessages.meeting.list.liveResume)
    expect(resume.classes()).toContain('fbtn-primary')

    await resume.trigger('click')
    expect(live.emitted('resume')).toHaveLength(1)
  })

  /**
   * A meeting can be running in this project while THIS client is not its
   * publisher (another device hosts it). The rows above are the server's
   * truth, so the bar must agree with them: no create (one room per project —
   * a create would reuse the live room and fail its publisher attach), and
   * the primary action joins the running one directly.
   */
  it('offers to join the running meeting when another client hosts it', async () => {
    sessions.value = [
      record({ token: 'live-tok', endedAt: 0, hasMinutes: false, hasTranscript: false, hasRevised: false }),
    ]
    const w = mountList()

    expect(w.find('[data-action="meeting-create"]').exists()).toBe(false)
    expect(w.find('[data-action="meeting-join"]').exists()).toBe(false)
    expect(w.find('[data-action="meeting-resume"]').exists()).toBe(false)

    const join = w.find('[data-action="meeting-join-live"]')
    expect(join.exists()).toBe(true)
    expect(join.text()).toBe(enMessages.meeting.list.joinLive)
    expect(join.classes()).toContain('fbtn-primary')

    await join.trigger('click')
    expect(w.emitted('joinLive')?.[0]).toEqual(['live-tok'])
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

  it('previews the record rows while the first load is in flight', () => {
    // The rows are cards, so the wait shows that shape. A lone ring said
    // "waiting" but nothing about what was coming, and collapsed the panel to
    // one glyph — the dock-panel rule the other tabs already follow.
    loading.value = true
    const w = mountList()

    const skeleton = w.find('.panel-skeleton')
    expect(skeleton.exists()).toBe(true)
    expect(skeleton.classes()).toContain('panel-skeleton--list')
    expect(w.findAll('.meeting-row')).toHaveLength(0)
  })

  it('shows the records instead of the placeholder once loaded', () => {
    sessions.value = [record()]
    const w = mountList()

    expect(w.find('.panel-skeleton').exists()).toBe(false)
    expect(w.findAll('.meeting-row')).toHaveLength(1)
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

describe('MeetingList HTTPS banner', () => {
  const originalSecure = Object.getOwnPropertyDescriptor(window, 'isSecureContext')
  const originalNative = Object.getOwnPropertyDescriptor(window, 'ClawBenchNative')
  const originalLocation = Object.getOwnPropertyDescriptor(window, 'location')

  /**
   * `secure` is the shell's own `isSecureContext`, which is TRUE on http (the
   * desktop promotes its origin so capture works) — that is the combination the
   * banner has to survive. What decides the banner is the protocol.
   */
  function setEnv(native: unknown, protocol: 'http:' | 'https:', secure: boolean): void {
    Object.defineProperty(window, 'isSecureContext', { value: secure, configurable: true })
    Object.defineProperty(window, 'ClawBenchNative', { value: native, configurable: true })
    const origin = `${protocol}//localhost:20000`
    Object.defineProperty(window, 'location', {
      value: { protocol, origin, href: `${origin}/`, replace: vi.fn() },
      configurable: true,
      writable: true,
    })
  }

  afterEach(() => {
    if (originalSecure) Object.defineProperty(window, 'isSecureContext', originalSecure)
    else delete (window as unknown as Record<string, unknown>).isSecureContext
    if (originalNative) Object.defineProperty(window, 'ClawBenchNative', originalNative)
    else delete (window as unknown as Record<string, unknown>).ClawBenchNative
    if (originalLocation) Object.defineProperty(window, 'location', originalLocation)
  })

  // The shell promotes its own origin to a secure context, so capture works
  // there — but the link it hands out does not get that treatment. That gap is
  // invisible until a guest cannot talk, which is what this banner says.
  it('shows the notice in the desktop shell on an http origin', async () => {
    setEnv({ isDesktopApp: () => true }, 'http:', true)
    const w = mountList()
    await flushPromises()

    expect(w.find('[data-role="https-notice"]').exists()).toBe(true)
    expect(w.find('[data-action="https-notice-generate"]').exists()).toBe(true)
  })

  // A plain browser cannot capture at all; the create page blocks with the full
  // notice, so repeating it on the list would say the same thing twice. An
  // https server has no such gap to report.
  it('stays hidden in a browser and on an https origin', async () => {
    setEnv(null, 'http:', false)
    const browser = mountList()
    await flushPromises()
    expect(browser.find('[data-role="https-notice"]').exists()).toBe(false)

    setEnv({ isDesktopApp: () => true }, 'https:', true)
    const secure = mountList()
    await flushPromises()
    expect(secure.find('[data-role="https-notice"]').exists()).toBe(false)
  })
})

/**
 * Dropping a recording is the only entry point that turns an existing file into
 * a meeting. The panel owns three decisions: which drops count, whether the
 * server can decode them, and what happens after the upload.
 */
describe('MeetingList import', () => {
  function dropPayload(files: File[]) {
    return { dataTransfer: { files, types: ['Files'] } }
  }

  function mediaFile(name = 'standup.m4a', type = 'audio/mp4') {
    return new File(['audio'], name, { type, lastModified: 1_700_000_000_000 })
  }

  beforeEach(() => {
    importAvailable.value = true
    importMock.mockClear()
  })

  it('imports a dropped recording and opens the new record', async () => {
    const w = mountList()
    await flushPromises()

    await w.find('.meeting-panel').trigger('drop', dropPayload([mediaFile()]))
    await flushPromises()

    expect(importMock).toHaveBeenCalledTimes(1)
    expect(importMock.mock.calls[0][0]).toBeInstanceOf(File)
    expect((importMock.mock.calls[0][0] as File).name).toBe('standup.m4a')
    // The new meeting is the only destination worth opening.
    expect(loadMock).toHaveBeenCalled()
    expect(w.emitted('open')?.[0]).toEqual(['imported-token'])
  })

  it('accepts a video container a browser cannot play', async () => {
    const w = mountList()
    await flushPromises()

    await w.find('.meeting-panel').trigger('drop', dropPayload([
      new File(['v'], 'screen recording.mkv', { type: '' }),
    ]))
    await flushPromises()

    expect(importMock).toHaveBeenCalledTimes(1)
  })

  it('ignores a dropped file that is not media', async () => {
    const w = mountList()
    await flushPromises()

    await w.find('.meeting-panel').trigger('drop', dropPayload([
      new File(['x'], 'notes.txt', { type: 'text/plain' }),
    ]))
    await flushPromises()

    expect(importMock).not.toHaveBeenCalled()
    expect(w.emitted('open')).toBeUndefined()
    // Silence would read as "the drop did nothing"; say why.
    expect(toastShow).toHaveBeenCalledWith(
      'Only audio and video files can be imported',
      expect.anything(),
    )
  })

  it('does not upload when the server cannot decode media', async () => {
    importAvailable.value = false
    const w = mountList()
    await flushPromises()

    await w.find('.meeting-panel').trigger('drop', dropPayload([mediaFile()]))
    await flushPromises()

    // Uploading gigabytes to be told "no ffmpeg" at the end is the failure this
    // guard exists to prevent.
    expect(importMock).not.toHaveBeenCalled()
  })

  it('shows the drop hint only while an OS file drag is over the panel', async () => {
    const w = mountList()
    await flushPromises()
    expect(w.find('.meeting-drop-hint').exists()).toBe(false)

    await w.find('.meeting-panel').trigger('dragenter', dropPayload([mediaFile()]))
    expect(w.find('.meeting-drop-hint').exists()).toBe(true)

    await w.find('.meeting-panel').trigger('dragleave', dropPayload([]))
    expect(w.find('.meeting-drop-hint').exists()).toBe(false)
  })

  it('offers the same path from the footer for devices without drag', async () => {
    const w = mountList()
    await flushPromises()

    const clickSpy = vi.spyOn(HTMLInputElement.prototype, 'click').mockImplementation(() => {})
    await w.find('[data-action="meeting-import"]').trigger('click')
    expect(clickSpy).toHaveBeenCalled()
    clickSpy.mockRestore()
  })

  it('does not report a failure when nothing was dropped', async () => {
    const w = mountList()
    await flushPromises()

    await w.find('.meeting-panel').trigger('drop', dropPayload([]))
    await flushPromises()

    expect(importMock).not.toHaveBeenCalled()
    expect(w.emitted('open')).toBeUndefined()
  })
})

describe('MeetingList quote action', () => {
  it('gives every row the same quote button as the other lists', () => {
    sessions.value = [record(), record({ token: 'tok-2' })]
    const w = mountList()

    const rows = w.findAll('.meeting-row')
    expect(rows).toHaveLength(2)
    for (const row of rows) {
      expect(row.find('.meeting-row__quote').exists(), 'each row must expose a quote action').toBe(true)
    }
  })

  it('hands the token to the host without opening the record', async () => {
    // The host owns the fetch + composer (the same handler as the detail
    // header), so the row only forwards the token.
    sessions.value = [record({ token: 'tok-9' })]
    const w = mountList()

    await w.find('.meeting-row__quote').trigger('click')

    expect(w.emitted('quote')).toEqual([['tok-9']])
    expect(w.emitted('open'), 'quoting must not open the record').toBeUndefined()
  })
})
