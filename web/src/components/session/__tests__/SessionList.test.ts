import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, nextTick } from 'vue'
import SessionList from '@/components/session/SessionList.vue'
import { RunningSweepDirective } from '@/directives/runningSweep'
import { LongPressDirective } from '@/directives/longPress'

// UI zoom factor driving toFixedCSS()/getZoomedViewport() in the component's
// clamp math. Defaults to 1 (no zoom); individual tests raise it to prove the
// menu is clamped in getBoundingClientRect() space rather than raw viewport px.
const scaleHolder = vi.hoisted(() => ({ value: 1 }))
// The rename dialog offers "auto-generate" only when the AI summary model is
// configured. serverConfig is a real ref so the component's read is reactive.
const settingsHolder = vi.hoisted(() => ({ serverConfig: null as any }))
vi.mock('@/composables/useSettingsConfig', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useSettingsConfig')>()
  const { ref } = await import('vue')
  settingsHolder.serverConfig = ref<Record<string, unknown>>({})
  return {
    ...actual,
    getUIScale: () => scaleHolder.value,
    toFixedCSS: (v: number) => v / scaleHolder.value,
    getZoomedViewport: () => ({ width: 1024, height: 768 }),
    useSettingsConfig: () => ({ serverConfig: settingsHolder.serverConfig }),
  }
})

const { mockGetAgentBackend, mockGetAgentName, mockDialogHolder, mockReconcileRunningSessions, mockRemoveEventHandler, mockEventHolder, mockStore, mockCrossState, mockDraggable, mockToastShow, mockGenerateSessionTitle } = await vi.hoisted(async () => {
  // The real store exposes a reactive() state, so tests that mutate
  // state.projectRoot (or sessionListVersion) must trigger the component's
  // watchers. A plain object would silently not, making such a test vacuous.
  const { reactive } = await import('vue')
  // projectRoot/homeDir are required by useCrossProjectSessions' "exclude the
  // current project" filter — without them the filter compares against
  // undefined and the test can never exercise the exclusion.
  const mockStore = { state: reactive({ chatSessionPageSize: 10, sessionListVersion: 0, sessionCount: 0, projectRoot: '/proj/current', homeDir: '/home/u' }) }
  const mockCrossState: {
    groups: any
    loading: any
    loaded: any
    total: any
    refresh: any
    scheduleRefresh: any
  } = {
    groups: null,
    loading: null,
    loaded: null,
    total: null,
    refresh: vi.fn(),
    scheduleRefresh: vi.fn(),
  }
  return {
    mockGetAgentBackend: vi.fn(() => ''),
    mockGetAgentName: vi.fn(() => 'Agent'),
    mockDialogHolder: { confirm: null as null | ((m: string, o?: any) => Promise<boolean>), prompt: null as null | ((m: string, o?: any) => Promise<string | null>), lastOptions: null as any },
    mockReconcileRunningSessions: vi.fn(),
    mockRemoveEventHandler: vi.fn(),
    mockEventHolder: { handler: null as null | ((event: string) => void) },
    mockStore,
    mockCrossState,
    // Captured by the VueDraggable stub so tests can emit events and read the
    // props the component bound (notably `handle`).
    mockDraggable: { emit: null as null | ((event: string, ...args: any[]) => void), props: null as null | Record<string, unknown> },
    mockToastShow: vi.fn(),
    mockGenerateSessionTitle: vi.fn(),
  }
})

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
  }
})
vi.mock('@/composables/useLocale', () => ({
  useLocale: () => ({ currentLocale: { value: 'en' } }),
  gt: (key: string) => key,
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('@/stores/app', () => ({
  store: mockStore,
}))
vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({
    onEvent: (handler: any) => { mockEventHolder.handler = handler; return mockRemoveEventHandler },
  }),
}))
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgentBackend: mockGetAgentBackend, getAgentName: mockGetAgentName }),
}))
// VueDraggable needs a real DOM root it can measure; in jsdom its mounted hook
// throws "Root element not found" and takes the whole component down. The stub
// renders the slot (so rows still mount) and records the handlers so tests can
// drive a drag directly.
vi.mock('vue-draggable-plus', () => ({
  VueDraggable: defineComponent({
    name: 'VueDraggable',
    // Declared (not left to fallthrough) so the "prop is absent" assertions are
    // meaningful: an undeclared prop would land in attrs and read undefined even
    // when the component set it.
    props: ['modelValue', 'handle', 'tag', 'animation', 'delay', 'delayOnTouchOnly', 'filter', 'preventOnFilter', 'onMove', 'draggable'],
    emits: ['update:modelValue', 'start', 'end'],
    setup(props, { slots, emit }) {
      mockDraggable.emit = emit
      mockDraggable.props = props as Record<string, unknown>
      return () => h('div', { class: 'vdp-stub' }, slots.default?.())
    },
  }),
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: (m: string, o?: any) => { mockDialogHolder.lastOptions = o; return mockDialogHolder.confirm!(m, o) },
    prompt: (m: string, o?: any) => { mockDialogHolder.lastOptions = o; return mockDialogHolder.prompt!(m, o) },
  }),
}))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: () => ({ runningSessionsVersion: { value: 0 } }),
  reconcileRunningSessions: mockReconcileRunningSessions,
  generateSessionTitle: mockGenerateSessionTitle,
}))
vi.mock('@/composables/useCrossProjectSessions', async () => {
  // Real refs, not plain {value} boxes: Vue only auto-unwraps actual refs in
  // templates, and the component reads crossGroups.length / crossLoading there.
  const { ref } = await import('vue')
  mockCrossState.groups = ref([])
  mockCrossState.loading = ref(false)
  mockCrossState.loaded = ref(true)
  mockCrossState.total = ref(0)
  return { useCrossProjectSessions: () => mockCrossState }
})
vi.mock('@/utils/format', () => ({ formatRelativeTime: (d: string) => d || 'now' }))
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', template: '<span class="agent-icon-stub" />' },
}))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div class="loading-stub" />' },
}))
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: { name: 'ModalDialog', template: '<div class="modal-stub" />' },
}))
// The tag dialog owns its own API calls; stub it so these tests exercise the
// list's wiring (open/seed) without hitting fetch for the candidate list.
vi.mock('@/components/session/SessionTagDialog.vue', () => ({
  default: {
    name: 'SessionTagDialog',
    props: ['open', 'sessionId', 'initialTags'],
    template: '<div class="tag-dialog-stub" />',
  },
}))
class MockIntersectionObserver {
  callback: any
  constructor(cb: any) { this.callback = cb }
  observe() {}
  disconnect() {}
  unobserve() {}
}
vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

function sessionsFixture() {
  return {
    s1: { id: 's1', title: 'Session 1', createdAt: '2025-01-01', updatedAt: '2025-01-05', agentId: 'agent-1', backend: 'cli', model: 'gpt-4' },
    s2: { id: 's2', title: 'Session 2', createdAt: '2025-01-02', updatedAt: '2025-01-06', agentId: 'agent-2', backend: 'acp' },
  }
}

describe('SessionList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetch.mockReset()
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    mockDialogHolder.prompt = vi.fn().mockResolvedValue(null)
    mockDialogHolder.lastOptions = null
    mockEventHolder.handler = null
    mockStore.state.sessionListVersion = 0
    mockStore.state.projectRoot = '/proj/current'
    mockCrossState.groups.value = []
    mockCrossState.loading.value = false
    mockCrossState.loaded.value = true
    mockCrossState.total.value = 0
    scaleHolder.value = 1
    settingsHolder.serverConfig.value = {}
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  async function mountList(props = {}) {
    const wrapper = mount(SessionList, {
      props: { currentSessionId: 's1', runningSessionIds: new Set(), ...props },
      // long-press is registered (it is global in the real app) even though the
      // list must not use it: without it here a re-added v-long-press would be
      // an unresolved directive, attach nothing, and the absence test would pass
      // vacuously.
      global: { directives: { 'running-sweep': RunningSweepDirective, 'long-press': LongPressDirective } },
    })
    await flushPromises()
    return wrapper
  }

  /**
   * Open the session menu the way the ⋮ button does — the only entry point (no
   * right-click, no long-press; see the absence tests). Tests click the real
   * element rather than reaching into an internal helper.
   */
  async function openRowMenu(wrapper: any, sessionId: string) {
    const btn = wrapper.find(`[data-session-id="${sessionId}"] .session-more-btn`)
    expect(btn.exists(), `row ${sessionId} should have a ⋮ button`).toBe(true)
    await btn.trigger('click')
    await nextTick()
  }

  // ── Share state on the context menu ──
  //
  // The state indicator lives on the "Share conversation" menu item rather
  // than a row badge: the badge was decorative (no click target) and showed
  // state in a different place from the action. Mirrors the file header, whose
  // "Share link" item highlights and relabels when a link exists.
  //
  // The i18n mock in this file returns the RAW KEY, so assertions match on
  // sessionShare.button / sessionShare.buttonActive rather than English text.
  it('seeds the share set on mount so the menu state is right without opening the drawer', async () => {
    const { useSessionShare } = await import('@/composables/useSessionShare')
    const { resetSessionShareState, isSessionShared } = useSessionShare()
    resetSessionShareState()

    mockFetch.mockImplementation((url: string) => {
      if (String(url).includes('/api/share/session/list')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ shares: [{ sessionId: 's1', token: 't1' }] }) })
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    })

    await mountList()
    await flushPromises()

    expect(isSessionShared('s1')).toBe(true)
    resetSessionShareState()
  })

  it('renders no share badge on the rows (state moved to the context menu)', async () => {
    const { useSessionShare } = await import('@/composables/useSessionShare')
    const { resetSessionShareState, markShared } = useSessionShare()
    resetSessionShareState()

    const s1 = sessionsFixture().s1
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [s1], hasMore: false }) })

    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()

    markShared(s1.id)
    await nextTick()

    expect(wrapper.findAll('.session-item-shared')).toHaveLength(0)

    resetSessionShareState()
  })

  it('highlights and relabels the share menu item when the session is shared', async () => {
    const { useSessionShare } = await import('@/composables/useSessionShare')
    const { resetSessionShareState, markShared } = useSessionShare()
    resetSessionShareState()

    const s1 = sessionsFixture().s1
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [s1], hasMore: false }) })

    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()

    // The menu is Teleported to body, so query there. openRowMenu clicks the
    // real ⋮ button, the only menu entry point.
    await openRowMenu(wrapper, s1.id)

    const findShareItem = () => {
      const menus = document.body.querySelectorAll('.context-menu.visible')
      const menu = menus[menus.length - 1]
      return Array.from(menu.querySelectorAll('.context-menu-item')).find(i => (i.textContent || '').includes('sessionShare.')) as HTMLElement | undefined
    }

    // Unshared: plain label, no active state.
    const before = findShareItem()
    expect(before).toBeTruthy()
    expect(before!.classList.contains('active')).toBe(false)
    expect(before!.textContent).toContain('sessionShare.button')

    // Shared: highlighted and relabelled. The item is keyed off
    // contextMenu.sessionId, so it reacts without being reopened.
    markShared(s1.id)
    await nextTick()

    const after = findShareItem()
    expect(after!.classList.contains('active')).toBe(true)
    expect(after!.textContent).toContain('sessionShare.buttonActive')

    resetSessionShareState()
    wrapper.unmount()
  })

  it('renders sessions from API', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)
  })

  it('emits select with sessionId and backend', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    await wrapper.vm.selectSession('s1', 'cli')
    expect(wrapper.emitted('select')![0]).toEqual(['s1', 'cli'])
  })

  it('marks running sessions from prop', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList({ runningSessionIds: new Set(['s1']) })
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.visibleRows[0].running).toBe(true)
  })

  it('renders the sweep band as a real element only on running rows', async () => {
    // The band must be a real node, not a `::after`: its travel is driven by
    // v-running-sweep through the Web Animations API, which cannot target a
    // pseudo-element. And it must be scoped to running rows, since it is the
    // visual signal for them.
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1, sessionsFixture().s2], hasMore: false }) })
    const wrapper = await mountList({ runningSessionIds: new Set(['s1']) })
    await wrapper.vm.loadSessions()
    await flushPromises()

    const rows = wrapper.findAll('.session-row')
    expect(rows).toHaveLength(2)
    const runningRow = wrapper.find('[data-session-id="s1"]')
    const idleRow = wrapper.find('[data-session-id="s2"]')
    expect(runningRow.find('.session-running-band').exists(), 'running row should carry the band').toBe(true)
    expect(idleRow.find('.session-running-band').exists(), 'idle row should not').toBe(false)
  })

  it('emits archive after confirmation', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    await wrapper.vm.archiveSession('s1')
    expect(wrapper.emitted('archive')).toBeTruthy()
  })

  it('emits destroy via dialog extra action', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    await wrapper.vm.archiveSession('s1')
    const onExtra = mockDialogHolder.lastOptions?.onExtraAction
    expect(typeof onExtra).toBe('function')
    onExtra()
    expect(wrapper.emitted('destroy')![0]).toEqual(['s1'])
  })

  it('destroyFromMenu confirms then emits destroy', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    await wrapper.vm.destroyFromMenu('s1')
    expect(mockDialogHolder.lastOptions?.confirmText).toBe('common.remove')
    expect(mockDialogHolder.lastOptions?.dangerous).toBe(true)
    expect(wrapper.emitted('destroy')![0]).toEqual(['s1'])
  })

  it('destroyFromMenu does not emit when confirmation is declined', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(false)
    await wrapper.vm.destroyFromMenu('s1')
    expect(wrapper.emitted('destroy')).toBeFalsy()
  })

  it('loads the whole list in one request, without a limit or cursor', async () => {
    // No lazy loading: the list is fetched complete so drag-reorder always has
    // every row, and there is no cursor bookkeeping to get wrong.
    const s1 = sessionsFixture().s1
    const s2 = sessionsFixture().s2
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [s1, s2], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()

    const listCalls = mockFetch.mock.calls.filter((c: unknown[]) => String(c[0]).includes('/api/ai/sessions'))
    // Every list request is the same full fetch — no second, cursor-based call.
    expect(listCalls.length).toBeGreaterThanOrEqual(1)
    for (const call of listCalls) {
      const url = String(call[0])
      expect(url).not.toContain('limit=')
      expect(url).not.toContain('cursor')
    }
    expect(wrapper.vm.sessions.length).toBe(2)
  })

  it('sends the tag filter with the full-list request', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()

    mockFetch.mockClear()
    wrapper.vm.activeTag = 'bug'
    await wrapper.vm.loadSessions()
    await flushPromises()

    const listCall = mockFetch.mock.calls.find((c: unknown[]) => String(c[0]).includes('/api/ai/sessions'))
    expect(listCall).toBeTruthy()
    expect(String(listCall![0])).toContain('tag=bug')
  })

  it('addSessionLocally prepends session', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    wrapper.vm.addSessionLocally({ id: 's9', title: 'S9', createdAt: '2025-01-09', updatedAt: '2025-01-09', agentId: 'agent-1', backend: 'cli' })
    await nextTick()
    expect(wrapper.vm.sessions[0].id).toBe('s9')
  })

  it('handles fetch error gracefully', async () => {
    mockFetch.mockRejectedValue(new Error('network'))
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(0)
  })

  it('keeps the existing list visible during a background reload (no clear-then-refill flash)', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)

    // Server state changed — next fetch returns s2. The reload must NOT show the
    // loading spinner while the old list (s1) is still rendered; the flag is only
    // for an empty list on first load.
    let resolveFetch
    mockFetch.mockReturnValueOnce(new Promise(r => { resolveFetch = r }))
    const reloadPromise = wrapper.vm.loadSessions()
    await nextTick()
    expect(wrapper.vm.loading).toBe(false)
    expect(wrapper.vm.sessions[0].id).toBe('s1')

    resolveFetch({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    await reloadPromise
    await flushPromises()
    expect(wrapper.vm.sessions[0].id).toBe('s2')
  })

  it('renders the list through VueDraggable so rows can be reordered', async () => {
    // The drag container replaced TransitionGroup: SortableJS owns the list
    // root, and running both would fight over the move animation.
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.find('.vdp-stub').exists()).toBe(true)
    expect(wrapper.findAll('.session-row').length).toBe(1)
  })

  describe('derived-session groups (issue #477)', () => {
    /** A root plus a two-generation chain forked from it. */
    const root = { id: 'root', title: 'Topic', createdAt: '2025-01-01', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }
    const fork1 = { id: 'f1', title: '🔀 Topic', sourceSessionId: 'root', createdAt: '2025-01-02', updatedAt: '2025-01-02', agentId: 'agent-1', backend: 'cli' }
    const fork2 = { id: 'f2', title: '🔀 🔀 Topic', sourceSessionId: 'f1', createdAt: '2025-01-03', updatedAt: '2025-01-03', agentId: 'agent-1', backend: 'cli' }
    const other = { id: 'other', title: 'Unrelated', createdAt: '2025-01-04', updatedAt: '2025-01-04', agentId: 'agent-1', backend: 'cli' }

    async function mountGrouped(sessions: any[] = [root, fork1, fork2, other]) {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions, hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      return wrapper
    }

    it('folds a fork chain under its root and leaves unrelated sessions flat', async () => {
      const wrapper = await mountGrouped()
      // Only the root and the unrelated session are top-level rows in the drag
      // array; the two forks hang off the root's group.
      expect(wrapper.vm.sessions.map((s: any) => s.id)).toEqual(['root', 'other'])
      expect(wrapper.findAll('.session-row').map(r => r.attributes('data-session-id')))
        .toEqual(['root', 'f1', 'f2', 'other'])
    })

    it('renders a group header under the anchor with the member count', async () => {
      const wrapper = await mountGrouped()
      const header = wrapper.find('.session-group-header')
      expect(header.exists()).toBe(true)
      expect(header.find('.session-group-count').text()).toBe('2')
    })

    it('labels each member with its generation', async () => {
      const wrapper = await mountGrouped()
      expect(wrapper.find('[data-session-id="f1"] .session-fork-gen').text()).toBe('session.forkGeneration')
      expect(wrapper.find('[data-session-id="f2"] .session-fork-gen').exists()).toBe(true)
      // The anchor row carries no generation chip — it is generation 0.
      expect(wrapper.find('[data-session-id="root"] .session-fork-gen').exists()).toBe(false)
    })

    it('collapses and expands the group from its header', async () => {
      const wrapper = await mountGrouped()
      await wrapper.find('.session-group-header').trigger('click')
      await nextTick()
      // Collapsed: members are gone from the DOM entirely (not merely hidden),
      // so the drag and keyboard indexes only ever see visible rows.
      expect(wrapper.findAll('.session-row').map(r => r.attributes('data-session-id')))
        .toEqual(['root', 'other'])
      expect(wrapper.find('.session-group-header .session-group-count').text()).toBe('2')

      await wrapper.find('.session-group-header').trigger('click')
      await nextTick()
      expect(wrapper.findAll('.session-row').map(r => r.attributes('data-session-id')))
        .toEqual(['root', 'f1', 'f2', 'other'])
    })

    it('expands the group holding the current session', async () => {
      // A deep link (notification / cross-project jump) can land on a member
      // whose anchor was collapsed earlier; that must not hide the open row.
      const wrapper = await mountGrouped()
      await wrapper.find('.session-group-header').trigger('click')
      await nextTick()
      expect(wrapper.find('[data-session-id="f1"]').exists()).toBe(false)

      await wrapper.setProps({ currentSessionId: 'f1' })
      await flushPromises()
      expect(wrapper.find('[data-session-id="f1"]').exists()).toBe(true)
    })

    it('admits only top-level rows as drag sources', async () => {
      // `draggable` is what keeps a group member from being dragged out of its
      // group; Sortable then reorders the top-level array, so a drag moves the
      // whole group.
      const wrapper = await mountGrouped()
      expect(mockDraggable.props?.draggable).toBe('.session-row.is-top')
      expect(wrapper.find('[data-session-id="root"]').classes()).toContain('is-top')
      expect(wrapper.find('[data-session-id="f1"]').classes()).toContain('is-fork-member')
    })

    it('resolves the row menu for a group member, which is not in the drag array', async () => {
      // Group members live in membersByAnchor, not in `sessions`. The menu used
      // to resolve ids with sessions.find(), which would silently no-op here.
      const wrapper = await mountGrouped()
      mockDialogHolder.prompt = vi.fn().mockResolvedValue('Renamed fork')
      await openRowMenu(wrapper, 'f1')
      expect(wrapper.vm.contextMenu.sessionId).toBe('f1')

      await wrapper.vm.renameSessionFromMenu('f1')
      await flushPromises()

      const patchCall = mockFetch.mock.calls.find(c => String(c[0]).startsWith('/api/ai/session/update'))
      expect(patchCall).toBeTruthy()
      expect(String(patchCall![0])).toContain(`session_id=${encodeURIComponent('f1')}`)
    })

    it('persists the flattened order so a group travels with its anchor', async () => {
      // The server numbers sort_order per session. Posting only the top-level
      // rows would leave the members behind and the group would reassemble
      // somewhere else on the next load.
      const wrapper = await mountGrouped()
      mockFetch.mockClear()
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ ok: true }) })

      // Drag `other` above the root.
      const arr = wrapper.vm.sessions.slice()
      const [moved] = arr.splice(1, 1)
      arr.splice(0, 0, moved)
      wrapper.vm.sessions = arr
      mockDraggable.emit!('end', { oldIndex: 1, newIndex: 0 })
      await flushPromises()

      const putCall = mockFetch.mock.calls.find(c => String(c[0]) === '/api/ai/sessions/reorder')
      expect(putCall).toBeTruthy()
      // Flattened visible order: other, root, then the root's members.
      expect(JSON.parse((putCall![1] as any).body).ids).toEqual(['other', 'root', 'f1', 'f2'])
    })

    it('keeps an orphaned chain visible as its own group', async () => {
      // The root was hard-deleted; the survivors must not vanish from the list.
      const orphan = { id: 'o1', title: 'Orphan', sourceSessionId: 'gone', createdAt: '2025-01-01', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }
      const orphanChild = { id: 'o2', title: 'Orphan child', sourceSessionId: 'o1', createdAt: '2025-01-02', updatedAt: '2025-01-02', agentId: 'agent-1', backend: 'cli' }
      const wrapper = await mountGrouped([orphan, orphanChild])
      expect(wrapper.findAll('.session-row').map(r => r.attributes('data-session-id')))
        .toEqual(['o1', 'o2'])
      expect(wrapper.find('.session-group-count').text()).toBe('1')
    })

    it('does not group an ACP-loaded session, whose source is a marker string', async () => {
      const loaded = { id: 'acp1', title: 'Loaded', sourceSessionId: 'acp:abc123', createdAt: '2025-01-01', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }
      const wrapper = await mountGrouped([loaded, other])
      expect(wrapper.findAll('.session-group-header').length).toBe(0)
      expect(wrapper.findAll('.session-row').length).toBe(2)
    })
  })

  it('subscribes to session_update WS events and reloads the list (debounced)', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)

    // Server state changes (e.g. another session added) — next load returns s2.
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    mockEventHolder.handler?.('session_update')
    // Debounce is 400ms — advance timers then flush.
    await new Promise(r => setTimeout(r, 500))
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)
    expect(wrapper.vm.sessions[0].id).toBe('s2')
  })

  it('reloads when sessionListVersion bumps (create/archive/destroy/read)', async () => {
    // The watcher calls reload() on every sessionListVersion change. We drive it
    // through the exposed reload() here (the store mock is a plain object, so the
    // production reactive watcher is exercised in the real app; this asserts the
    // reload contract that the watcher invokes).
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)

    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    wrapper.vm.reload()
    await flushPromises()
    await nextTick()
    expect(wrapper.vm.sessions[0].id).toBe('s2')
  })


  describe('pinned marker and keyboard nav', () => {
    // The backend returns the user's manual order (sort_order ASC). The
    // component must render in that same order so useListNav's index maps onto
    // the visible rows.
    const pinnedOld = { id: 'p-old', title: 'Pinned Old', pinned: true, createdAt: '2024-01-01', updatedAt: '2024-01-01', agentId: 'agent-1', backend: 'cli' }
    const newest = { id: 'n-new', title: 'Newest', pinned: false, createdAt: '2025-06-01', updatedAt: '2025-06-01', agentId: 'agent-1', backend: 'cli' }
    const middle = { id: 'n-mid', title: 'Middle', pinned: false, createdAt: '2025-05-01', updatedAt: '2025-05-01', agentId: 'agent-1', backend: 'cli' }

    it('renders one flat list — no pinned/recent section headers', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // The section split is gone: a single .session-rows container holds all
      // three rows, and no group header is rendered in the project pane.
      expect(wrapper.findAll('.session-section').length).toBe(0)
      const rows = wrapper.findAll('.session-rows > .session-row')
      expect(rows.length).toBe(3)
      expect(wrapper.findAll('.session-group-header').length).toBe(0)
    })

    it('marks the pinned row with the row modifier only (no inline glyph)', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // The wedge is a CSS pseudo-element, so the DOM carries only the modifier
      // class — no extra glyph element in the row or its title line.
      const pinnedRow = wrapper.find('[data-session-id="p-old"]')
      expect(pinnedRow.classes()).toContain('pinned')
      expect(pinnedRow.find('.session-pin-icon').exists()).toBe(false)

      // Unpinned rows carry no modifier either.
      for (const id of ['n-new', 'n-mid']) {
        expect(wrapper.find(`[data-session-id="${id}"]`).classes()).not.toContain('pinned')
      }
    })

    it('DOM order matches the rendered row order (the server sort order)', async () => {
      // The array is now returned in the user's manual order; the component must
      // render it as-is. (Pin no longer lifts a row, so the pinned marker in the
      // fixture stays where the server put it.)
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const domOrder = wrapper.findAll('.session-row').map(r => r.attributes('data-session-id'))
      expect(domOrder).toEqual(['p-old', 'n-new', 'n-mid'])
      expect(wrapper.vm.visibleRows.map((r: any) => r.session.id)).toEqual(domOrder)
    })

    // useListKeys installs a document-level listener on mount, so real
    // ArrowDown/Enter events exercise the same path a user hits.
    function pressKey(key: string) {
      document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }))
    }

    it('highlights rows in flat DOM order, pinned row first', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // First ArrowDown highlights index 0 — the pinned row (first in DOM).
      pressKey('ArrowDown')
      await nextTick()
      let highlighted = wrapper.findAll('.session-row.session-row-active')
      expect(highlighted.length).toBe(1)
      expect(highlighted[0].attributes('data-session-id')).toBe('p-old')

      // The flat list means index 1 is simply the next row — no section offset.
      pressKey('ArrowDown')
      await nextTick()
      highlighted = wrapper.findAll('.session-row.session-row-active')
      expect(highlighted.length).toBe(1)
      expect(highlighted[0].attributes('data-session-id')).toBe('n-new')

      pressKey('ArrowDown')
      await nextTick()
      highlighted = wrapper.findAll('.session-row.session-row-active')
      expect(highlighted.length).toBe(1)
      expect(highlighted[0].attributes('data-session-id')).toBe('n-mid')

      wrapper.unmount()
    })

    it('confirming a highlighted pinned row selects that session', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      pressKey('ArrowDown')
      await nextTick()
      pressKey('Enter')
      expect(wrapper.emitted('select')![0]).toEqual(['p-old', 'cli'])

      wrapper.unmount()
    })

    it('confirming a highlighted unpinned row selects the right session', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // Move past the pinned row onto the unpinned one.
      pressKey('ArrowDown')
      await nextTick()
      pressKey('ArrowDown')
      await nextTick()
      pressKey('Enter')
      expect(wrapper.emitted('select')![0]).toEqual(['n-new', 'cli'])

      wrapper.unmount()
    })

    it('renders no pinned marker when nothing is pinned', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      expect(wrapper.findAll('.session-row.pinned').length).toBe(0)
      expect(wrapper.findAll('.session-row').length).toBe(2)
    })

    it('togglePin sends the pinned flag for the menu session and bumps the list version', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ ok: true }) })
      const before = mockStore.state.sessionListVersion
      await wrapper.vm.togglePin('n-new', false)
      await flushPromises()

      const patchCall = mockFetch.mock.calls.find((c: unknown[]) => String(c[0]).startsWith('/api/ai/session/update'))
      expect(patchCall).toBeTruthy()
      expect(String(patchCall![0])).toContain(`session_id=${encodeURIComponent('n-new')}`)
      expect(JSON.parse((patchCall![1] as any).body).pinned).toBe(true)
      expect(mockStore.state.sessionListVersion).toBe(before + 1)
    })

    it('togglePin rolls back the optimistic flag when the request fails', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockResolvedValue({ ok: false, statusText: 'boom', json: () => Promise.resolve({}) })
      await wrapper.vm.togglePin('n-new', false)
      await flushPromises()

      expect(wrapper.vm.sessions.find((s: any) => s.id === 'n-new').pinned).toBe(false)
    })
  })

  it('exposes reload() and removes the WS listener on unmount', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()

    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    wrapper.vm.reload()
    await flushPromises()
    expect(wrapper.vm.sessions[0].id).toBe('s2')

    wrapper.unmount()
    expect(mockRemoveEventHandler).toHaveBeenCalled()
  })

  describe('cross-project tab', () => {
    function crossGroup() {
      return {
        name: '/proj/other',
        displayName: 'other',
        displayPath: '~/proj/other',
        sessions: [
          { id: 'o1', title: 'Other 1', backend: 'cli', agentId: 'agent-1', model: 'gpt-4', running: true, pendingApproval: false, unreadCount: 0, updatedAt: '2025-01-05' },
          { id: 'o2', title: 'Other 2', backend: 'acp', agentId: 'agent-2', running: false, pendingApproval: false, unreadCount: 3, updatedAt: '2025-01-04' },
        ],
      }
    }

    it('defaults to the project tab', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await flushPromises()
      expect(wrapper.vm.activeTab).toBe('project')
      expect(wrapper.find('.session-list-pane--cross').attributes('style')).toContain('display: none')
    })

    it('renders cross-project groups with a project-name header when the prop flips', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      expect(wrapper.findAll('.cross-group').length).toBe(1)
      // Header markup now comes from the shared SessionGroupHeader component.
      expect(wrapper.find('.session-group-title').text()).toBe('other')
      expect(wrapper.find('.session-group-subtitle').text()).toBe('~/proj/other')
      expect(wrapper.findAll('.cross-session-item').length).toBe(2)
    })

    it('shows the active-session count in the cross-project group header', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      // crossGroup() has two active sessions — the count must reflect the group,
      // matching the count badge the Pinned/Recent headers already show.
      expect(wrapper.find('.session-group-count').text()).toBe('2')
    })

    it('collapses and expands a cross-project group from its header', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()

      const header = wrapper.find('.cross-group .session-group-header')
      // Expanded by default: chevron not rotated.
      expect(wrapper.find('.cross-group .session-group-chevron').classes()).not.toContain('collapsed')

      await header.trigger('click')
      expect(wrapper.find('.cross-group .session-group-chevron').classes()).toContain('collapsed')
      // Rows stay mounted (v-show, not v-if) so collapsing never destroys the
      // fetched snapshot — only the rows container is hidden.
      expect(wrapper.findAll('.cross-group .cross-session-item').length).toBe(2)
      expect(wrapper.find('.cross-group-rows').attributes('style')).toContain('display: none')

      await header.trigger('click')
      expect(wrapper.find('.cross-group .session-group-chevron').classes()).not.toContain('collapsed')
      expect(wrapper.find('.cross-group-rows').attributes('style') || '').not.toContain('display: none')
    })

    it('keeps each project group collapsed independently', async () => {
      const second = {
        name: '/proj/second',
        displayName: 'second',
        displayPath: '~/proj/second',
        sessions: [
          { id: 't1', title: 'Second 1', backend: 'cli', agentId: 'agent-1', model: '', running: false, pendingApproval: false, unreadCount: 1, updatedAt: '2025-01-03' },
        ],
      }
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup(), second]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()

      // Collapse only the first group; the second must stay expanded.
      await wrapper.findAll('.cross-group .session-group-header')[0].trigger('click')
      const chevrons = wrapper.findAll('.cross-group .session-group-chevron')
      expect(chevrons[0].classes()).toContain('collapsed')
      expect(chevrons[1].classes()).not.toContain('collapsed')
    })

    it('does not use .session-item for cross rows (keyboard nav index isolation)', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      // Project rows keep .session-item; cross rows must not add to that set,
      // otherwise querySelectorAll('.session-item') indices no longer match
      // useListNav's count.
      expect(wrapper.findAll('.session-item').length).toBe(1)
      expect(wrapper.findAll('.cross-session-item').length).toBe(2)
    })

    it('emits select with the owning project path for cross rows', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      await wrapper.findAll('.cross-session-item')[0].trigger('click')
      expect(wrapper.emitted('select')![0]).toEqual(['o1', 'cli', '/proj/other'])
    })

    it('does not render the row action button on cross rows', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      const crossRows = wrapper.findAll('.cross-session-row')
      expect(crossRows.length).toBe(2)
      for (const row of crossRows) {
        expect(row.find('.session-more-btn').exists()).toBe(false)
      }
    })

    it('shows the empty state when there are no cross-project groups', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = []
      mockCrossState.loaded.value = true
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      expect(wrapper.find('.session-list-pane--cross').text()).toContain('session.crossEmpty')
    })

    it('shows a loading indicator before the first successful load', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = []
      mockCrossState.loaded.value = false
      mockCrossState.loading.value = true
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      expect(wrapper.find('.session-list-pane--cross').find('.loading-stub').exists()).toBe(true)
    })
  })

  describe('drag reordering (issue #492)', () => {
    it('scopes the drag to the row ⋮ button, with no press delay', async () => {
      // The drag must start only from the trailing button so the row body stays
      // free for text selection (desktop) and list scrolling (touch). That also
      // makes the touch press-delay unnecessary — the two gestures can no
      // longer collide, so the row is never hijacked.
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      expect(mockDraggable.props?.handle).toBe('.session-more-btn')
      expect(mockDraggable.props?.delay).toBeUndefined()
      expect(mockDraggable.props?.delayOnTouchOnly).toBeUndefined()
      expect(wrapper.find('.session-more-btn').exists()).toBe(true)
    })

    it('excludes pinned rows from dragging without swallowing their menu click', async () => {
      // `filter` keeps a pinned row from being dragged; preventOnFilter=false is
      // what still lets its ⋮ button open the menu (Sortable's default would
      // preventDefault the press and eat the click).
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      expect(mockDraggable.props?.filter).toBe('.session-row.pinned')
      expect(mockDraggable.props?.preventOnFilter).toBe(false)
    })

    it('keeps the ⋮ menu icon on the button, including while dragging', async () => {
      // The button is both the menu opener and the drag handle, but its icon is
      // always the ⋮ menu glyph — no grip swap while dragging.
      const s1 = sessionsFixture().s1
      const s2 = sessionsFixture().s2
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [s1, s2], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // Lucide icons are anonymous functional components, so assert on the
      // rendered svg rather than by component name.
      const hasIcon = (id: string) =>
        wrapper.find(`[data-session-id="${id}"] .session-more-btn svg`).exists()

      expect(hasIcon('s1')).toBe(true)
      expect(hasIcon('s2')).toBe(true)

      mockDraggable.emit!('start', { item: { dataset: { sessionId: 's2' } } })
      await nextTick()
      // Still the same single icon, not a grip.
      expect(hasIcon('s2')).toBe(true)
      expect(wrapper.find('[data-session-id="s2"] .session-more-btn').findAll('svg').length).toBe(1)

      mockDraggable.emit!('end', { oldIndex: 1, newIndex: 1 })
      await flushPromises()
      expect(hasIcon('s2')).toBe(true)
    })

    it('live guard refuses a drop inside the pinned block, container target included', async () => {
      const pinnedRow = { id: 'p1', title: 'Pinned', pinned: true, createdAt: '2025-01-01', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }
      const plainRow = { id: 's1', title: 'Plain', pinned: false, createdAt: '2025-01-02', updatedAt: '2025-01-02', agentId: 'agent-1', backend: 'cli' }
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedRow, plainRow], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const onMove = mockDraggable.props?.onMove as (evt: any) => boolean
      expect(typeof onMove).toBe('function')
      const pinnedEl = wrapper.find('[data-session-id="p1"]').element
      const plainEl = wrapper.find('[data-session-id="s1"]').element
      // Inserting before the pinned row would put a plain row above it → refuse.
      expect(onMove({ related: pinnedEl, willInsertAfter: false })).toBe(false)
      // Inserting after it stays below the block → allow.
      expect(onMove({ related: plainEl, willInsertAfter: true })).toBe(true)
      // The container target (gap above the first row) has no row element; only
      // appending at the end is safe.
      expect(onMove({ related: wrapper.find('.session-rows').element, willInsertAfter: false })).toBe(false)
      expect(onMove({ related: wrapper.find('.session-rows').element, willInsertAfter: true })).toBe(true)
    })

    it('lifts pinned rows back to the top even when Sortable drops a row above them', async () => {
      // The hard frontend guarantee: Sortable's onMove is advisory and is skipped
      // when the drop lands on the container, so a plain row CAN be left above a
      // pinned one. onDragEnd must re-partition regardless of what Sortable did.
      const pinnedRow = { id: 'p1', title: 'Pinned', pinned: true, sortOrder: 5, createdAt: '2025-01-01', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }
      const a = { id: 'a', title: 'A', pinned: false, sortOrder: 0, createdAt: '2025-01-02', updatedAt: '2025-01-02', agentId: 'agent-1', backend: 'cli' }
      const b = { id: 'b', title: 'B', pinned: false, sortOrder: 1, createdAt: '2025-01-03', updatedAt: '2025-01-03', agentId: 'agent-1', backend: 'cli' }
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedRow, a, b], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockClear()
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ ok: true }) })
      // Simulate the bad outcome: Sortable inserted B at index 0, above the pin.
      wrapper.vm.sessions = [b, pinnedRow, a]
      mockDraggable.emit!('end', { oldIndex: 2, newIndex: 0 })
      await flushPromises()

      // Pinned row is back on top; the plain rows keep their relative order.
      expect(wrapper.vm.sessions.map((s: any) => s.id)).toEqual(['p1', 'b', 'a'])
      // ...and the pinned row's own sortOrder is untouched.
      expect(wrapper.vm.sessions.find((s: any) => s.id === 'p1').sortOrder).toBe(5)
      // Only unpinned rows are persisted.
      const putCall = mockFetch.mock.calls.find(c => String(c[0]) === '/api/ai/sessions/reorder')
      expect(JSON.parse((putCall![1] as any).body).ids).toEqual(['b', 'a'])
    })

    it('keeps a multi-row pinned block intact and ahead of the rest', async () => {
      const p1 = { id: 'p1', title: 'P1', pinned: true, sortOrder: 0, createdAt: '2025-01-01', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }
      const p2 = { id: 'p2', title: 'P2', pinned: true, sortOrder: 1, createdAt: '2025-01-02', updatedAt: '2025-01-02', agentId: 'agent-1', backend: 'cli' }
      const a = { id: 'a', title: 'A', pinned: false, sortOrder: 0, createdAt: '2025-01-03', updatedAt: '2025-01-03', agentId: 'agent-1', backend: 'cli' }
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [p1, p2, a], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockClear()
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ ok: true }) })
      // Sortable scattered the block; both pinned rows must return to the top in
      // their previous relative order.
      wrapper.vm.sessions = [p2, a, p1]
      mockDraggable.emit!('end', { oldIndex: 0, newIndex: 2 })
      await flushPromises()

      expect(wrapper.vm.sessions.map((s: any) => s.id)).toEqual(['p2', 'p1', 'a'])
    })

    /** Emit a Sortable end event, reordering the bound array first as it does. */
    async function drag(wrapper: any, oldIndex: number, newIndex: number) {
      if (oldIndex !== newIndex) {
        const arr = wrapper.vm.sessions.slice()
        const [moved] = arr.splice(oldIndex, 1)
        arr.splice(newIndex, 0, moved)
        wrapper.vm.sessions = arr
      }
      mockDraggable.emit!('end', { oldIndex, newIndex })
      await flushPromises()
    }

    it('persists the new order via PUT /api/ai/sessions/reorder', async () => {
      const s1 = sessionsFixture().s1
      const s2 = sessionsFixture().s2
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [s1, s2], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockClear()
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ ok: true }) })
      await drag(wrapper, 1, 0)

      const putCall = mockFetch.mock.calls.find(c => String(c[0]) === '/api/ai/sessions/reorder')
      expect(putCall).toBeTruthy()
      expect((putCall![1] as any).method).toBe('PUT')
      expect(JSON.parse((putCall![1] as any).body).ids).toEqual(['s2', 's1'])

      // Local sortOrder is renumbered to match the server, so the next
      // pagination cursor is built from the post-drag order rather than the
      // stale pre-drag values.
      expect(wrapper.vm.sessions.map((s: any) => s.sortOrder)).toEqual([0, 1])
    })

    it('posts only the unpinned rows, leaving pinned order untouched', async () => {
      // Pinned rows are positioned by pinned DESC, so they are not part of the
      // drag payload and must not be locally renumbered either.
      const pinnedRow = { id: 'p1', title: 'Pinned', pinned: true, sortOrder: 7, createdAt: '2025-01-01', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }
      const a = { id: 'a', title: 'A', pinned: false, sortOrder: 0, createdAt: '2025-01-02', updatedAt: '2025-01-02', agentId: 'agent-1', backend: 'cli' }
      const b = { id: 'b', title: 'B', pinned: false, sortOrder: 1, createdAt: '2025-01-03', updatedAt: '2025-01-03', agentId: 'agent-1', backend: 'cli' }
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedRow, a, b], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockClear()
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ ok: true }) })
      // Drag B above A (indices 2 -> 1); the pinned row stays at index 0.
      await drag(wrapper, 2, 1)

      const putCall = mockFetch.mock.calls.find(c => String(c[0]) === '/api/ai/sessions/reorder')
      expect(putCall).toBeTruthy()
      expect(JSON.parse((putCall![1] as any).body).ids).toEqual(['b', 'a'])
      // The pinned row keeps its own sort_order rather than being renumbered.
      expect(wrapper.vm.sessions.find((s: any) => s.id === 'p1').sortOrder).toBe(7)
    })

    it('does not persist when a drag ends where it started', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1, sessionsFixture().s2], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockClear()
      await drag(wrapper, 1, 1)

      expect(mockFetch.mock.calls.filter(c => String(c[0]) === '/api/ai/sessions/reorder').length).toBe(0)
    })

    it('a failed reorder rolls back to the server order and toasts', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1, sessionsFixture().s2], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // The reorder PUT fails, then the rollback reload returns the original order.
      mockFetch.mockImplementation((url: string, init?: any) => {
        if (init?.method === 'PUT') return Promise.resolve({ ok: false, statusText: 'boom', json: () => Promise.resolve({}) })
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1, sessionsFixture().s2], hasMore: false }) })
      })
      await drag(wrapper, 1, 0)

      expect(mockToastShow).toHaveBeenCalledWith('session.reorderFailed', expect.anything())
      expect(wrapper.vm.sessions[0].id).toBe('s1')
    })

    it('rename from the menu targets the button-owning session', async () => {
      const s1 = sessionsFixture().s1
      const s2 = sessionsFixture().s2
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [s1, s2], hasMore: false }) })
      mockDialogHolder.prompt = vi.fn().mockResolvedValue('Renamed B')
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // Open s2's menu from its own ⋮ button, then rename from the menu.
      await openRowMenu(wrapper, 's2')
      expect(wrapper.vm.contextMenu.sessionId).toBe('s2')

      await wrapper.vm.renameSessionFromMenu(wrapper.vm.contextMenu.sessionId)
      await flushPromises()

      // The API call must carry s2 (not s1) via the session_id query.
      const patchCall = mockFetch.mock.calls.find(c => (c[0] as string).startsWith('/api/ai/session/update'))
      expect(patchCall).toBeTruthy()
      expect(patchCall![0]).toContain(`session_id=${encodeURIComponent('s2')}`)
      const body = JSON.parse(patchCall![1].body)
      expect(body.title).toBe('Renamed B')
      // Only s2's title updated locally.
      expect(wrapper.vm.sessions.find((s: any) => s.id === 's2').title).toBe('Renamed B')
      expect(wrapper.vm.sessions.find((s: any) => s.id === 's1').title).toBe('Session 1')
    })

    it('rename from the menu omits the generate option when no summary model is configured', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      mockDialogHolder.prompt = vi.fn().mockResolvedValue(null)
      settingsHolder.serverConfig.value = {}
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      await wrapper.vm.renameSessionFromMenu('s1')

      expect(mockDialogHolder.lastOptions.generateText).toBeUndefined()
      expect(mockDialogHolder.lastOptions.onGenerate).toBeUndefined()
    })

    it('rename from the menu offers generate when the summary model is configured', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      mockDialogHolder.prompt = vi.fn().mockResolvedValue(null)
      settingsHolder.serverConfig.value = { ai_summary: { api: { base_url: 'https://summary.example.com' } } }
      mockGenerateSessionTitle.mockResolvedValue('Generated Title')
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      await wrapper.vm.renameSessionFromMenu('s1')

      expect(mockDialogHolder.lastOptions.generateText).toBe('chat.sessionRename.generate')
      const title = await mockDialogHolder.lastOptions.onGenerate()
      expect(title).toBe('Generated Title')
      // The generator is bound to the session being renamed, not the current one.
      expect(mockGenerateSessionTitle).toHaveBeenCalledWith('s1')
    })

    it('rename from the menu toasts when generation yields no title', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      mockDialogHolder.prompt = vi.fn().mockResolvedValue(null)
      settingsHolder.serverConfig.value = { ai_summary: { api: { base_url: 'https://summary.example.com' } } }
      mockGenerateSessionTitle.mockResolvedValue(null)
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      await wrapper.vm.renameSessionFromMenu('s1')
      const result = await mockDialogHolder.lastOptions.onGenerate()

      expect(result).toBeNull()
      expect(mockToastShow).toHaveBeenCalledWith('chat.sessionRename.generateFailed', expect.anything())
    })

    it('pin from the menu toggles the button-owning session', async () => {
      const s1 = sessionsFixture().s1
      const s2 = sessionsFixture().s2
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [s1, s2], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // Open s2's menu from its own ⋮ button, then pin from the menu.
      await openRowMenu(wrapper, 's2')
      expect(wrapper.vm.contextMenu.sessionId).toBe('s2')

      await wrapper.vm.togglePin(wrapper.vm.contextMenu.sessionId, wrapper.vm.contextMenu.pinned)
      await flushPromises()

      const patchCall = mockFetch.mock.calls.find(c => (c[0] as string).startsWith('/api/ai/session/update'))
      expect(patchCall).toBeTruthy()
      expect(patchCall![0]).toContain(`session_id=${encodeURIComponent('s2')}`)
      const body = JSON.parse(patchCall![1].body)
      expect(body.pinned).toBe(true)
      // Optimistic local update applied to s2, not s1.
      expect(wrapper.vm.sessions.find((s: any) => s.id === 's2').pinned).toBe(true)
      expect(wrapper.vm.sessions.find((s: any) => s.id === 's1').pinned).toBeFalsy()
    })
  })

  describe('shared context menu control', () => {
    it('renders the shared .context-menu markup (not the old bespoke classes)', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      await openRowMenu(wrapper, 's1')

      const menus = document.body.querySelectorAll('.context-menu.visible')
      const menu = menus[menus.length - 1]
      expect(menu).toBeTruthy()
      // Reuses the file manager's item class + icon-left layout.
      // pin / rename / set-tags / share / archive / force-delete
      expect(menu!.querySelectorAll('.context-menu-item').length).toBe(6)
      expect(document.body.querySelector('.session-context-menu')).toBeNull()
      wrapper.unmount()
    })

    // ── No right-click and no long-press entry point ──
    //
    // The menu is opened ONLY by the row's ⋮ button. A right-click binding was
    // tried and removed: Android WebView / iOS Safari synthesize a `contextmenu`
    // event for a touch long-press (it is how the platform raises its native
    // selection menu), so binding `@contextmenu` silently re-creates a
    // long-press menu on mobile no matter how the handler filters. Rather than
    // keep that discrimination, the row carries no contextmenu binding at all.
    //
    // These assert the absence on the RENDERED DOM, not on the source: a
    // source-grep would miss a binding that a later edit re-adds under another
    // name, and asserting only on `v-long-press` was the false guard that let
    // the mobile long-press menu through in the first place.

    it('has no contextmenu binding on the rows', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const row = wrapper.find('[data-session-id="s1"]').element as HTMLElement
      const ev = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 50, clientY: 50 })
      row.dispatchEvent(ev)
      await nextTick()

      // Nothing opened, and the event was left alone for the browser (a touch
      // long-press must still get its native selection menu).
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      expect(ev.defaultPrevented).toBe(false)
      wrapper.unmount()
    })

    it('has no contextmenu binding on the ctx-overlay either', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // Open via the button so the overlay exists, then right-click it.
      await openRowMenu(wrapper, 's1')
      const overlays = document.body.querySelectorAll('.ctx-overlay')
      const overlay = overlays[overlays.length - 1] as HTMLElement
      const ev = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 5, clientY: 5 })
      overlay.dispatchEvent(ev)
      await nextTick()

      // The menu stays as it was — no retarget, no reopen.
      expect(wrapper.vm.contextMenu.visible).toBe(true)
      expect(wrapper.vm.contextMenu.sessionId).toBe('s1')
      expect(ev.defaultPrevented).toBe(false)
      wrapper.unmount()
    })

    it('has no long-press directive on the rows (removed on purpose)', async () => {
      // The directive is still globally registered, so a re-added v-long-press
      // would attach silently; assert it is absent.
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const row = wrapper.find('[data-session-id="s1"]').element as HTMLElement
      expect((row as any)._longPress_binding).toBeUndefined()
      expect((row as any)._longPress_cleanup).toBeUndefined()
      wrapper.unmount()
    })

    it('renders a full-viewport .ctx-overlay that closes the menu on click', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      await openRowMenu(wrapper, 's1')

      // Earlier tests leave their own teleported menus open on document.body, so
      // take the newest overlay — the one this wrapper just rendered.
      const overlays = document.body.querySelectorAll('.ctx-overlay')
      const overlay = overlays[overlays.length - 1] as HTMLElement
      expect(overlay).toBeTruthy()
      overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      wrapper.unmount()
    })

    it('anchors the menu under the row button and clamps it within the zoomed viewport', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // jsdom reports a zero-size rect for the button, so the menu opens at the
      // clamped origin. The point is the clamp math: coords are stored in
      // fixed-CSS space, so at zoom 2 they must be pulled inside the 512px-wide
      // CSS viewport rather than overflowing.
      scaleHolder.value = 2
      await openRowMenu(wrapper, 's1')
      await nextTick()

      expect(wrapper.vm.contextMenu.visible).toBe(true)
      expect(wrapper.vm.contextMenu.x).toBeLessThanOrEqual(512 - 8)
      expect(wrapper.vm.contextMenu.y).toBeLessThanOrEqual(384 - 8)
      expect(wrapper.vm.contextMenu.x).toBeGreaterThanOrEqual(8)
      expect(wrapper.vm.contextMenu.y).toBeGreaterThanOrEqual(8)
      wrapper.unmount()
    })

    it('closes the menu when an item is clicked', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1, sessionsFixture().s2], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const menus = () => document.body.querySelectorAll('.context-menu.visible')
      const openFor = async () => { await openRowMenu(wrapper, 's1') }
      // Select by label, not position: the menu order is a presentation detail
      // and inserting an item must not silently retarget these assertions.
      const clickLastMenu = (label: string) => {
        const items = menus()[menus().length - 1].querySelectorAll('.context-menu-item')
        const match = Array.from(items).find((el) => (el.textContent || '').trim() === label)
        if (!match) throw new Error(`context-menu item not found: ${label}`)
        ;(match as HTMLElement).dispatchEvent(new MouseEvent('click', { bubbles: true }))
      }

      // Pin item — must dismiss the menu, not just fire the action.
      await openFor()
      clickLastMenu('common.pin')
      await flushPromises()
      expect(wrapper.vm.contextMenu.visible).toBe(false)

      // Rename item — also dismisses. prompt() resolves null so the
      // action itself is a no-op; the close must not depend on it succeeding.
      mockDialogHolder.prompt = vi.fn().mockResolvedValue(null)
      await openFor()
      clickLastMenu('common.renameSession')
      await flushPromises()
      expect(wrapper.vm.contextMenu.visible).toBe(false)

      // Set-tags item — dismisses and opens the tag dialog.
      await openFor()
      clickLastMenu('common.setTags')
      await nextTick()
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      expect(wrapper.vm.tagDialog.open).toBe(true)

      // Archive item — dismisses and emits.
      await openFor()
      clickLastMenu('common.archive')
      await nextTick()
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      expect(wrapper.emitted('archive')).toBeTruthy()

      // Remove item — dismisses, confirms, then emits destroy. Cancel
      // first: a declined confirm must not destroy anything.
      mockDialogHolder.confirm = vi.fn().mockResolvedValue(false)
      await openFor()
      clickLastMenu('common.remove')
      await flushPromises()
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      expect(wrapper.emitted('destroy')).toBeFalsy()

      mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
      await openFor()
      clickLastMenu('common.remove')
      await flushPromises()
      expect(wrapper.emitted('destroy')![0]).toEqual(['s1'])
      wrapper.unmount()
    })

    it('opens the share dialog for the menu session', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      await openRowMenu(wrapper, wrapper.vm.sessions[0].id)

      const menus = document.body.querySelectorAll('.context-menu.visible')
      const items = menus[menus.length - 1].querySelectorAll('.context-menu-item')
      const share = Array.from(items).find((el) => (el.textContent || '').trim() === 'sessionShare.button')
      expect(share).toBeTruthy()
      ;(share as HTMLElement).dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()

      // The dialog opens for the right session and the menu dismisses.
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      expect(wrapper.vm.shareDialog.sessionId).toBe(wrapper.vm.sessions[0].id)
      wrapper.unmount()
    })
    it('opens the tag dialog seeded with the session tags', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ ...sessionsFixture().s1, tags: [{ name: 'bug' }, { name: 'urgent' }] }],
          hasMore: false,
        }),
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      wrapper.vm.openTagDialogFromMenu('s1')
      expect(wrapper.vm.tagDialog.open).toBe(true)
      expect(wrapper.vm.tagDialog.sessionId).toBe('s1')
      // Seeded from the already-loaded list so the checkboxes paint instantly.
      expect(wrapper.vm.tagDialog.initialTags).toEqual(['bug', 'urgent'])
      wrapper.unmount()
    })

    it('does not open the tag dialog for an unknown session', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      wrapper.vm.openTagDialogFromMenu('nope')
      expect(wrapper.vm.tagDialog.open).toBe(false)
      wrapper.unmount()
    })
  })

  describe('session tag row', () => {
    it('renders one chip per tag with a stable accent variable', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ ...sessionsFixture().s1, tags: [{ name: 'bug' }, { name: 'urgent' }] }],
          hasMore: false,
        }),
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const chips = wrapper.findAll('.session-tag')
      expect(chips.length).toBe(2)
      expect(chips.map(c => c.text())).toEqual(['bug', 'urgent'])
      // The accent is carried inline (light+dark) so the CSS can pick per theme.
      expect(chips[0].attributes('style')).toContain('--tag-accent-light')
      wrapper.unmount()
    })

    it('omits the tag row entirely when the session has no tags', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // An empty tag row would still consume the info column's gap and shift
      // the meta line, so the wrapper must not render at all.
      expect(wrapper.find('.session-item-tags').exists()).toBe(false)
      wrapper.unmount()
    })

    it('separates the tag row from the meta line above it', async () => {
      // The parent's uniform gap (2px) reads fine between two text lines, whose
      // line-height half-leading pads them out, but leaves the bordered chips
      // visually flush against the meta line — they read as its continuation
      // rather than a row of their own. The tag row must add its own spacing.
      const src = String((await import('@/components/session/SessionList.vue?raw')).default)
      const rule = src.replace(/\/\*[\s\S]*?\*\//g, '')
        .match(/\.session-item-tags\s*\{[^}]*\}/)?.[0]
      expect(rule, '.session-item-tags should exist').toBeTruthy()
      expect(rule).toMatch(/margin-top:\s*var\(--space-2\)/)
      // Whitespace only: a border/background here would double up with the row
      // separator just below and fight the active/running row backgrounds.
      expect(rule).not.toMatch(/border/)
      expect(rule).not.toMatch(/background/)
    })

    it('wraps the tag row instead of clipping the overflow', async () => {
      // The row was nowrap + overflow:hidden, so any tag past the row's width
      // was invisible and unclickable — the tags existed but could not be read.
      // jsdom has no layout engine, so assert the declarations that allow
      // wrapping rather than a rendered geometry.
      const src = String((await import('@/components/session/SessionList.vue?raw')).default)
      const rule = src.replace(/\/\*[\s\S]*?\*\//g, '')
        .match(/\.session-item-tags\s*\{[^}]*\}/)?.[0]
      expect(rule, '.session-item-tags should exist').toBeTruthy()
      expect(rule).toMatch(/flex-wrap:\s*wrap/)
      expect(rule).not.toMatch(/overflow:\s*hidden/)
    })

    it('lets a long tag name ellipsise instead of widening the row', async () => {
      const src = String((await import('@/components/session/SessionList.vue?raw')).default)
      const rule = src.replace(/\/\*[\s\S]*?\*\//g, '')
        .match(/\.session-tag\s*\{[^}]*\}/)?.[0]
      expect(rule, '.session-tag should exist').toBeTruthy()
      // flex-shrink:0 with a long name would push the chip past the pane edge;
      // min-width:0 is what actually lets text-overflow engage in a flex child.
      expect(rule).toMatch(/flex-shrink:\s*1/)
      expect(rule).toMatch(/min-width:\s*0/)
      expect(rule).toMatch(/text-overflow:\s*ellipsis/)
    })

    it('gives the same tag the same color across sessions', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { ...sessionsFixture().s1, tags: [{ name: 'shared' }] },
            { ...sessionsFixture().s2, tags: [{ name: 'shared' }] },
          ],
          hasMore: false,
        }),
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const chips = wrapper.findAll('.session-tag')
      expect(chips.length).toBe(2)
      // Same name ⇒ same color, regardless of which session it sits on.
      expect(chips[0].attributes('style')).toBe(chips[1].attributes('style'))
      wrapper.unmount()
    })
  })
  describe('tag filter bar', () => {
    // The filter bar fetches its own chip set via apiGet (global fetch) while
    // the list uses fetch directly — route by URL so each call gets the right
    // payload.
    function routeFetch({ sessions = [], hasMore = false, tags = [] } = {}) {
      mockFetch.mockImplementation((url: string) => {
        if (String(url).includes('/api/ai/session/tags')) {
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ tags }) })
        }
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions, hasMore }) })
      })
    }

    it('hides the filter bar when the project has no tags in use', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
      wrapper.unmount()
    })

    it('renders one chip per in-use tag and requests only in-use tags', async () => {
      routeFetch({
        sessions: [sessionsFixture().s1],
        tags: [{ name: 'bug', scope: 'project', count: 2 }, { name: 'urgent', scope: 'global', count: 1 }],
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const chips = wrapper.findAll('.session-tag-filter-chip')
      expect(chips.length).toBe(2)
      expect(chips[0].text()).toContain('bug')
      // The unused-global case is excluded server-side; assert the client asks
      // for that view rather than the full candidate list.
      const tagCall = mockFetch.mock.calls.find(c => String(c[0]).includes('/api/ai/session/tags'))
      expect(String(tagCall![0])).toContain('inUse=1')
      wrapper.unmount()
    })

    it('clicking a chip sends the tag to the server and marks it active', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      mockFetch.mockClear()
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })

      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()

      const listCall = mockFetch.mock.calls.find(c => String(c[0]).includes('/api/ai/sessions'))
      expect(String(listCall![0])).toContain('tag=bug')
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')
      wrapper.unmount()
    })

    it('clicking the active chip again clears the filter', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')

      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.find('.session-tag-filter-chip').classes()).not.toContain('active')
      wrapper.unmount()
    })

    it('clears the filter when the applied tag disappears from the in-use list', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')

      // The tag is deleted (or its last session dropped it): the bar would
      // vanish, so the filter must not stay applied invisibly.
      routeFetch({ sessions: [sessionsFixture().s1], tags: [] })
      await wrapper.vm.loadFilterTags()
      await flushPromises()

      expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
      // And the list is no longer constrained by the dead tag.
      mockFetch.mockClear()
      routeFetch({ sessions: [sessionsFixture().s1], tags: [] })
      await wrapper.vm.loadSessions()
      await flushPromises()
      const listCall = mockFetch.mock.calls.find(c => String(c[0]).includes('/api/ai/sessions'))
      expect(String(listCall![0])).not.toContain('tag=')
      wrapper.unmount()
    })

    it('clears the filter when the project changes, even if the tag name exists there too', async () => {
      // The new project deliberately also has a "bug" tag in use. Without the
      // explicit clear in the projectRoot watcher, loadFilterTags would find the
      // name present and keep the filter — carrying project A's selection into a
      // different project, where it means nothing. (If the new project had no
      // tags, the auto-clear would mask this and the test would pass either way.)
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')

      mockStore.state.projectRoot = '/proj/other'
      routeFetch({ sessions: [], tags: [{ name: 'bug', scope: 'project', count: 4 }] })
      await nextTick()
      await flushPromises()

      // The chip is still offered (the tag exists here) but must be INACTIVE.
      const chip = wrapper.find('.session-tag-filter-chip')
      expect(chip.exists()).toBe(true)
      expect(chip.classes()).not.toContain('active')
      wrapper.unmount()
    })


    it('re-fetches the list after the applied tag disappears (no dead filter)', async () => {
      // Regression: the chip set and the list are refreshed by the same watcher,
      // but the list request captures activeTag synchronously. If the tag is
      // cleared AFTER that request is issued, the list stays filtered by a tag
      // whose chip is already gone — the user cannot clear it from the UI.
      const route = (sessions: any[], tags: any[]) => {
        mockFetch.mockImplementation((url: string) => {
          if (String(url).includes('/api/ai/session/tags')) {
            return Promise.resolve({ ok: true, json: () => Promise.resolve({ tags }) })
          }
          const u = String(url)
          const filtered = u.includes('tag=bug') ? sessions.filter((x: any) => x.tagged) : sessions
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: filtered, hasMore: false }) })
        })
      }
      const all = [
        { id: 's1', title: 'A', createdAt: '2025-01-01', updatedAt: '2025-01-01', tagged: true },
        { id: 's2', title: 'B', createdAt: '2025-01-02', updatedAt: '2025-01-02', tagged: false },
      ]
      route(all, [{ name: 'bug', scope: 'project', count: 1 }])
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.vm.sessions.length).toBe(1)

      // The tag is deleted elsewhere: the version bump drives the refresh.
      route(all, [])
      mockStore.state.sessionListVersion++
      await nextTick()
      await flushPromises()
      await flushPromises()

      // Bar is gone AND the list is no longer constrained by the dead tag.
      expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
      expect(wrapper.vm.sessions.length).toBe(2)
      wrapper.unmount()
    })

    it('keeps chips and filter when the tag fetch fails', async () => {
      // A transient API failure must not be read as "this project has no tags":
      // that would hide the bar and silently drop the user's filter, with no way
      // to restore it.
      let failTags = false
      mockFetch.mockImplementation((url: string) => {
        if (String(url).includes('/api/ai/session/tags')) {
          if (failTags) return Promise.reject(new Error('network'))
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ tags: [{ name: 'bug', scope: 'project', count: 1 }] }) })
        }
        const u = String(url)
        const all = [
          { id: 's1', title: 'A', createdAt: '2025-01-01', updatedAt: '2025-01-01', tagged: true },
          { id: 's2', title: 'B', createdAt: '2025-01-02', updatedAt: '2025-01-02', tagged: false },
        ]
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: u.includes('tag=bug') ? all.filter(x => x.tagged) : all, hasMore: false }) })
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()

      failTags = true
      await wrapper.vm.loadFilterTags()
      await flushPromises()

      expect(wrapper.find('.session-tag-filter').exists()).toBe(true)
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')
      wrapper.unmount()
    })

    it('hides the filter bar on the cross-project tab', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList({ activeTab: 'project' })
      await wrapper.vm.loadSessions()
      await flushPromises()
      expect(wrapper.find('.session-tag-filter').exists()).toBe(true)

      await wrapper.setProps({ activeTab: 'cross' })
      await nextTick()
      expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
      wrapper.unmount()
    })
  })
})
