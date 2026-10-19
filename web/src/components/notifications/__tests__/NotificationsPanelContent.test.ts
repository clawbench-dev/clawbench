import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { h, ref } from 'vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

// Minimal icon stubs — the panel imports many lucide icons, and the shared
// mock in this repo is per-file, so declare exactly what it uses.
vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({ name, render: () => h('span', { class: `icon-${name}` }) })
  const names = [
    'Inbox', 'Folder', 'Unlink', 'Trash2', 'AlertCircle', 'Layers',
    'Bot', 'Clock', 'GitMerge', 'Check', 'X', 'ShieldAlert',
  ]
  return Object.fromEntries(names.map(n => [n, stub(n)]))
})

// The shared notification state the panel reads.
const items = ref<unknown[]>([])
const total = ref(0)
const hasMore = ref(false)
const loading = ref(false)
const loadingMore = ref(false)
const error = ref('')
const filterKind = ref('')
const filterRange = ref('')
const filterProject = ref('')
const projectOptions = ref<{ id: number; path: string }[]>([])
const mockLoad = vi.fn()
const mockLoadMore = vi.fn()
const mockLoadProjects = vi.fn()
const mockSetKind = vi.fn()
const mockSetRange = vi.fn()
const mockSetProject = vi.fn()
const mockClear = vi.fn()
const mockToastShow = vi.fn()
// Controllable so a test can make the cross-project confirmation be declined.
const mockConfirm = vi.fn().mockResolvedValue(true)

vi.mock('@/composables/useNotifications', () => ({
  ALL_PROJECTS: '',
  useNotifications: () => ({
    items, total, hasMore, loading, loadingMore, error,
    filterKind, filterRange, filterProject, projectOptions,
    load: mockLoad,
    loadMore: mockLoadMore,
    loadProjects: mockLoadProjects,
    setKind: mockSetKind,
    setRange: mockSetRange,
    setProject: mockSetProject,
    clear: mockClear,
  }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockConfirm }),
}))

vi.mock('@/components/common/MenuSelect.vue', () => ({
  default: {
    name: 'MenuSelect',
    // Render the `options` slot (the project picker supplies custom rows) so
    // those rows are assertable; fall back to a plain marker otherwise.
    template: '<div class="menu-select-stub"><slot name="options" /><slot /></div>',
    props: ['modelValue', 'options', 'appSurface', 'menuTitle', 'menuWidth'],
    emits: ['update:modelValue'],
  },
}))

vi.mock('@/components/common/RefreshButton.vue', () => ({
  default: { name: 'RefreshButton', template: '<button class="refresh-stub" />', props: ['loading', 'disabled'] },
}))

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<span class="loading-stub" />', props: ['size', 'inline'] },
}))

vi.mock('@/stores/app', () => ({
  // Mutable so a test can install the RAW projectRoot that POST /api/project
  // hands back (see the "raw vs canonical" cases below).
  store: { state: { projectRoot: '/proj' } },
}))

import NotificationsPanelContent from '@/components/notifications/NotificationsPanelContent.vue'
import { store } from '@/stores/app'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en, zh } })

function mountPanel() {
  return mount(NotificationsPanelContent, {
    props: { active: true },
    global: { plugins: [i18n] },
  })
}

const notif = (over: Record<string, unknown> = {}) => ({
  id: 1,
  kind: 'session',
  eventType: 'completed',
  title: 'A session',
  body: 'body',
  projectId: 1,
  projectPath: '/proj',
  sessionId: 's1',
  createdAt: '2026-10-15T12:00:00.000Z',
  ...over,
})

describe('NotificationsPanelContent', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockConfirm.mockResolvedValue(true)
    // Reset the (module-level, mutable) project root so the raw/canonical
    // cases cannot leak into other tests.
    store.state.projectRoot = '/proj'
    items.value = []
    total.value = 0
    hasMore.value = false
    loading.value = false
    error.value = ''
    filterKind.value = ''
    filterRange.value = ''
    filterProject.value = ''
    projectOptions.value = []
  })

  it('loads (globally) and fetches project options when the tab becomes active', () => {
    mountPanel()
    // No arguments: scope is global, the project filter lives in the composable.
    expect(mockLoad).toHaveBeenCalledWith()
    expect(mockLoadProjects).toHaveBeenCalled()
  })

  it('renders three filter dropdowns (project / type / time)', () => {
    const w = mountPanel()
    expect(w.findAll('.notif-filters .menu-select-stub')).toHaveLength(3)
  })

  it('renders a card per notification', () => {
    items.value = [notif({ id: 1, title: 'First' }), notif({ id: 2, title: 'Second' })]
    const w = mountPanel()
    expect(w.findAll('.notif-card')).toHaveLength(2)
    expect(w.text()).toContain('First')
    expect(w.text()).toContain('Second')
  })

  it('uses the shield icon for the pending-approval event chip', () => {
    // Approval/permission UI is unified on the shield glyph — the pending chip
    // used to be an AlertTriangle, which read as a generic error.
    items.value = [notif({ id: 1, eventType: 'permission_pending' })]
    const w = mountPanel()
    expect(w.find('.nc-meta .evt .icon-ShieldAlert').exists()).toBe(true)
    expect(w.find('.nc-meta .evt .icon-AlertTriangle').exists()).toBe(false)
  })

  it('clicking a session row dispatches clawbench-open-session', async () => {
    items.value = [notif({ sessionId: 'sess-42' })]
    const w = mountPanel()

    const events: CustomEvent[] = []
    const listener = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('clawbench-open-session', listener)
    await w.find('.notif-card').trigger('click')
    window.removeEventListener('clawbench-open-session', listener)

    expect(events).toHaveLength(1)
    expect(events[0].detail.sessionId).toBe('sess-42')
  })

  it('clicking a task row dispatches clawbench-open-task', async () => {
    items.value = [notif({ kind: 'task', taskId: '7', executionId: '99', sessionId: undefined })]
    const w = mountPanel()

    const events: CustomEvent[] = []
    const listener = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('clawbench-open-task', listener)
    await w.find('.notif-card').trigger('click')
    window.removeEventListener('clawbench-open-task', listener)

    expect(events).toHaveLength(1)
    expect(events[0].detail.taskId).toBe('7')
    expect(events[0].detail.executionId).toBe('99')
  })

  it('clicking a forge row dispatches clawbench-open-forge with a rebuilt target', async () => {
    items.value = [notif({ kind: 'forge', forgeItemKey: 'pr/42', sessionId: undefined })]
    const w = mountPanel()

    const events: CustomEvent[] = []
    const listener = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('clawbench-open-forge', listener)
    await w.find('.notif-card').trigger('click')
    window.removeEventListener('clawbench-open-forge', listener)

    expect(events).toHaveLength(1)
    expect(events[0].detail.target.type).toBe('pr')
    expect(events[0].detail.target.number).toBe(42)
    expect(events[0].detail.target.itemKey).toBe('pr/42')
  })

  it('rebuilds a pipeline target from its run key', async () => {
    items.value = [notif({ kind: 'forge', forgeItemKey: 'pipeline/run:555', sessionId: undefined })]
    const w = mountPanel()

    const events: CustomEvent[] = []
    const listener = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('clawbench-open-forge', listener)
    await w.find('.notif-card').trigger('click')
    window.removeEventListener('clawbench-open-forge', listener)

    expect(events).toHaveLength(1)
    expect(events[0].detail.target.type).toBe('pipeline')
    expect(events[0].detail.target.runId).toBe(555)
  })

  it('a sourceMissing row does NOT navigate and shows a hint instead', async () => {
    // The whole reason sourceMissing exists: a deleted session's own fallback
    // silently redirects to the newest session, which is worse than a hint.
    items.value = [notif({ sourceMissing: true, sessionId: 'sess-gone' })]
    const w = mountPanel()

    const events: CustomEvent[] = []
    const listener = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('clawbench-open-session', listener)
    await w.find('.notif-card').trigger('click')
    window.removeEventListener('clawbench-open-session', listener)

    expect(events).toHaveLength(0)
    expect(mockToastShow).toHaveBeenCalled()
  })

  it('marks a sourceMissing row visually', () => {
    items.value = [notif({ sourceMissing: true })]
    const w = mountPanel()
    expect(w.find('.notif-card').classes()).toContain('is-missing')
    expect(w.find('.nc-gone').exists()).toBe(true)
  })

  it('shows the empty state when there are no rows', () => {
    const w = mountPanel()
    expect(w.text()).toContain('No notifications')
  })

  it('renders the load-more control only when hasMore', async () => {
    items.value = [notif()]
    hasMore.value = false
    let w = mountPanel()
    expect(w.find('.load-more').exists()).toBe(false)
    w.unmount()

    hasMore.value = true
    w = mountPanel()
    expect(w.find('.load-more').exists()).toBe(true)
  })

  // ── Navigation origin + cross-project confirmation ────────────────────────

  it('flags a same-project open so App.vue can record the log as the jump origin', async () => {
    // Without this flag the jump records whatever tab preceded the log, and
    // Back (header / edge swipe / Android) lands somewhere the user never was.
    items.value = [notif({ sessionId: 'sess-42', projectPath: '/proj' })]
    const w = mountPanel()

    const events: CustomEvent[] = []
    const listener = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('clawbench-open-session', listener)
    await w.find('.notif-card').trigger('click')
    window.removeEventListener('clawbench-open-session', listener)

    expect(events[0].detail.fromNotificationCenter).toBe(true)
    // Same project → no confirmation.
    expect(mockConfirm).not.toHaveBeenCalled()
  })

  it('confirms before opening a row from ANOTHER project', async () => {
    items.value = [notif({ sessionId: 'sess-9', projectPath: '/other-proj' })]
    const w = mountPanel()

    const events: CustomEvent[] = []
    const listener = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('clawbench-open-session', listener)
    await w.find('.notif-card').trigger('click')
    await flushPromises()
    window.removeEventListener('clawbench-open-session', listener)

    expect(mockConfirm).toHaveBeenCalledTimes(1)
    // The message must NAME the target project — "switch projects?" without
    // saying which one is not actionable.
    expect(String(mockConfirm.mock.calls[0][0])).toContain('other-proj')
    expect(events).toHaveLength(1)
    expect(events[0].detail.sessionId).toBe('sess-9')
  })

  it('declining the cross-project confirmation cancels the navigation', async () => {
    mockConfirm.mockResolvedValue(false)
    items.value = [notif({ sessionId: 'sess-9', projectPath: '/other-proj' })]
    const w = mountPanel()

    const events: CustomEvent[] = []
    const listener = (e: Event) => events.push(e as CustomEvent)
    window.addEventListener('clawbench-open-session', listener)
    await w.find('.notif-card').trigger('click')
    await flushPromises()
    window.removeEventListener('clawbench-open-session', listener)

    expect(events).toHaveLength(0)
  })

  it('does not confirm for a row with no project attribution', async () => {
    // projectId 0 is the "cannot attribute" sentinel; we cannot name a target
    // project, so there is nothing to confirm and no external claim to make.
    items.value = [notif({ sessionId: 'sess-1', projectPath: '' })]
    const w = mountPanel()

    await w.find('.notif-card').trigger('click')
    await flushPromises()

    expect(mockConfirm).not.toHaveBeenCalled()
    expect(w.find('.nc-proj').exists()).toBe(false)
  })

  // ── External-project distinction: the NAME is the whole marker ────────────

  it('names an other-project row and spells the consequence out in the tooltip', () => {
    items.value = [notif({ projectPath: '/other-proj' })]
    const w = mountPanel()

    const chip = w.find('.nc-proj')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).toContain('other-proj')
    expect(chip.attributes('title')).toContain('other-proj')
  })

  it('uses NO heavier marker than the name — no rail, no extra pill', () => {
    // The name alone is the distinction. An earlier version added a left rail
    // plus an "Other project" pill, which read as status/severity and
    // outweighed the event chip that owns that role. Pin the absence on an
    // EXTERNAL row — asserting it on a same-project row proves nothing, since
    // both markers are already false there.
    items.value = [notif({ projectPath: '/other-proj' })]
    const w = mountPanel()

    expect(w.find('.notif-card').classes()).not.toContain('is-external-project')
    expect(w.find('.nc-external').exists()).toBe(false)
    expect(w.find('.nc-proj').classes()).not.toContain('is-external')
  })

  it('shows NO project label on a same-project row', () => {
    // The absence of a name is the signal. Repeating your own project on every
    // row is noise, and an extra "Other project" pill made the distinction read
    // as a status/severity — which the event chip already owns.
    items.value = [notif({ projectPath: '/proj' })]
    const w = mountPanel()

    expect(w.find('.nc-proj').exists()).toBe(false)
    // No heavier marker either: the card carries no external class at all.
    expect(w.find('.notif-card').classes()).not.toContain('is-external-project')
    expect(w.find('.nc-external').exists()).toBe(false)
  })

  // ── Raw vs canonical projectRoot ──────────────────────────────────────────
  //
  // `n.projectPath` comes from the projects table (canonicalized: no trailing
  // slash, symlinks resolved). `store.projectRoot` comes from the last
  // /api/project response, and the POST branch returns the REQUEST path
  // verbatim. An exact `===` therefore reports the user's OWN project as
  // external whenever those forms differ — labelling every row of the project
  // they are currently in.

  it('treats a trailing-slash projectRoot as the SAME project', () => {
    store.state.projectRoot = '/proj/'
    items.value = [notif({ projectPath: '/proj' })]
    const w = mountPanel()

    expect(w.find('.nc-proj').exists()).toBe(false)
  })

  it('treats a backslash-separated projectRoot as the SAME project', () => {
    store.state.projectRoot = 'E:\\git\\app'
    items.value = [notif({ projectPath: 'E:/git/app' })]
    const w = mountPanel()

    expect(w.find('.nc-proj').exists()).toBe(false)
  })

  it('treats a Windows drive letter with different case as the SAME project', () => {
    store.state.projectRoot = 'e:/git/app'
    items.value = [notif({ projectPath: 'E:/git/app' })]
    const w = mountPanel()

    expect(w.find('.nc-proj').exists()).toBe(false)
  })

  it('still marks a genuinely different project (regression guard)', () => {
    // The canonical comparison must not become a blanket "always same".
    store.state.projectRoot = '/proj/'
    items.value = [notif({ projectPath: '/other' })]
    const w = mountPanel()

    expect(w.find('.nc-proj').exists()).toBe(true)
  })

  // ── Timeline grouping ─────────────────────────────────────────────────────
  //
  // The log used to be a flat list repeating "10-15 12:00" on every row. It is
  // now bucketed by local calendar day with the date spelled out once in a
  // sticky header, and each row carrying only a clock. These pin the grouping
  // key (local midnight, NOT a UTC slice of the ISO string) and the split of
  // information between header and row.

  /** Local-time ISO for a Date, so the fixture is timezone-independent. */
  const localIso = (y: number, mo: number, d: number, h: number, mi: number) =>
    new Date(y, mo, d, h, mi).toISOString()

  it('buckets rows by local calendar day, newest bucket first', () => {
    const today = new Date()
    const yesterday = new Date()
    yesterday.setDate(yesterday.getDate() - 1)
    items.value = [
      notif({ id: 1, createdAt: localIso(today.getFullYear(), today.getMonth(), today.getDate(), 9, 0) }),
      notif({ id: 2, createdAt: localIso(today.getFullYear(), today.getMonth(), today.getDate(), 8, 0) }),
      notif({
        id: 3,
        createdAt: localIso(yesterday.getFullYear(), yesterday.getMonth(), yesterday.getDate(), 22, 0),
      }),
    ]
    const w = mountPanel()

    const groups = w.findAll('.day-group')
    expect(groups).toHaveLength(2)
    expect(groups[0].findAll('.notif-card')).toHaveLength(2)
    expect(groups[1].findAll('.notif-card')).toHaveLength(1)
    expect(groups[0].find('.day-label').text()).toBe('Today')
    expect(groups[1].find('.day-label').text()).toBe('Yesterday')
  })

  it('keeps a late-evening row in its own day (local midnight, not a UTC slice)', () => {
    // A `createdAt.slice(0, 10)` grouping would put 23:30 local in the next
    // day for any UTC+ timezone, which is the whole reason the key is computed
    // from local midnight.
    const d = new Date()
    d.setHours(23, 30, 0, 0)
    const next = new Date(d)
    next.setDate(next.getDate() + 1)
    next.setHours(0, 30, 0, 0)
    items.value = [
      notif({ id: 1, createdAt: d.toISOString() }),
      notif({ id: 2, createdAt: next.toISOString() }),
    ]
    const w = mountPanel()

    expect(w.findAll('.day-group')).toHaveLength(2)
    expect(w.findAll('.day-group')[0].findAll('.notif-card')).toHaveLength(1)
  })

  it('shows a clock in the gutter and no date on the row', () => {
    items.value = [notif({ id: 1, createdAt: localIso(2026, 9, 15, 14, 32) })]
    const w = mountPanel()

    expect(w.find('.nc-time').text()).toBe('14:32')
    // The date belongs to the bucket header only.
    expect(w.find('.nc-header').text()).not.toContain('2026')
  })

  it('carries the full timestamp in a title for the abbreviated clock', () => {
    // Every visible surface abbreviates, so the exact instant must stay
    // reachable somewhere.
    items.value = [notif({ id: 1, createdAt: localIso(2026, 9, 15, 14, 32) })]
    const w = mountPanel()

    expect(w.find('.notif-card').attributes('title')).toBe('2026-10-15 14:32:00')
  })

  it('labels each bucket with its own row count', () => {
    const today = new Date()
    items.value = [
      notif({ id: 1, createdAt: localIso(today.getFullYear(), today.getMonth(), today.getDate(), 9, 0) }),
      notif({ id: 2, createdAt: localIso(today.getFullYear(), today.getMonth(), today.getDate(), 8, 0) }),
    ]
    const w = mountPanel()

    expect(w.find('.day-head .day-count').text()).toBe('2 items')
  })

  it('gives each row a tone class derived from its event', () => {
    // The tone drives the node colour, the rail highlight and the hovered card
    // edge from one variable; without the class the node has no --tone and
    // falls back to `currentColor` (invisible against the card).
    items.value = [
      notif({ id: 1, eventType: 'failed' }),
      notif({ id: 2, eventType: 'permission_pending' }),
      notif({ id: 3, eventType: 'completed' }),
      notif({ id: 4, kind: 'forge', eventType: 'merged' }),
      notif({ id: 5, kind: 'task', eventType: 'running' }),
    ]
    const w = mountPanel()

    const tones = w.findAll('.notif-card').map(c => c.classes().find(x => x.startsWith('tone-')))
    expect(tones).toEqual(['tone-danger', 'tone-warning', 'tone-success', 'tone-purple', 'tone-info'])
  })

  it('keeps the kind glyph in the timeline node, not the card header', () => {
    // The node carries the source type; the header glyph is the narrow-panel
    // fallback and is hidden by CSS at full width.
    items.value = [notif({ id: 1, kind: 'task', eventType: 'completed' })]
    const w = mountPanel()

    expect(w.find('.nc-node .icon-Clock').exists()).toBe(true)
    expect(w.find('.nc-kind .icon-Clock').exists()).toBe(true)
  })

  it('does NOT confirm a cross-project switch for a trailing-slash root', async () => {
    // The confirmation must use the same canonical comparison — otherwise the
    // user gets a "switch project?" dialog for the project they are already in.
    store.state.projectRoot = '/proj/'
    items.value = [notif({ sessionId: 's1', projectPath: '/proj' })]
    const w = mountPanel()

    await w.find('.notif-card').trigger('click')
    await flushPromises()

    expect(mockConfirm).not.toHaveBeenCalled()
  })
})
