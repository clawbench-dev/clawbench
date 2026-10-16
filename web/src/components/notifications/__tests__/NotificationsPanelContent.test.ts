import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { h, ref } from 'vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

// Minimal icon stubs — the panel imports many lucide icons, and the shared
// mock in this repo is per-file, so declare exactly what it uses.
vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({ name, render: () => h('span', { class: `icon-${name}` }) })
  const names = [
    'Bell', 'BellOff', 'Folder', 'Unlink', 'Trash2', 'AlertCircle',
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
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}))

vi.mock('@/components/common/MenuSelect.vue', () => ({
  default: {
    name: 'MenuSelect',
    template: '<div class="menu-select-stub"><slot /></div>',
    props: ['modelValue', 'options'],
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
  store: { state: { projectRoot: '/proj' } },
}))

import NotificationsPanelContent from '@/components/notifications/NotificationsPanelContent.vue'

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
})
