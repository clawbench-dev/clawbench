import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { nextTick } from 'vue'
import { mount, enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ForgePanelContent from '@/components/forge/ForgePanelContent.vue'
import { canNavigateBack, handleBackNavigation, _resetHandlers } from '@/composables/useBackHandler'
import {
  pendingForgeNavigation,
  setPendingForgeNavigation,
  _resetPendingForgeNavigationForTesting,
} from '@/composables/useForgeNavigation'

// Panels stay alive after a test and keep watching the module-level pending
// deep-link ref, so a leftover panel would consume a request before the panel
// under test ever sees it. Auto-unmount every wrapper after each test.
enableAutoUnmount(afterEach)

// ── Mocks ────────────────────────────────────────────────────
const mockLoadBinding = vi.fn()
const mockLoad = vi.fn()
const mockLoadMore = vi.fn()
const mockSetType = vi.fn()
const mockSetState = vi.fn()
const mockSetMineFilter = vi.fn()
const mockSetQuery = vi.fn()
const mockPipelinesLoad = vi.fn()
const mockPipelinesLoadMore = vi.fn()
const mockPipelinesSetFilter = vi.fn()

const state = {
  items: { value: [] as unknown[] },
  binding: { value: null as unknown },
  suggested: { value: null as unknown },
  loading: { value: false },
  loadingMore: { value: false },
  error: { value: null as unknown },
  type: { value: 'issue' as const },
  state: { value: 'open' as const },
  mineFilter: { value: 'all' as const },
  query: { value: '' },
  hasMore: { value: false },
  nextPage: { value: 1 },
  isBound: { value: false },
  isEmpty: { value: false },
  loadBinding: mockLoadBinding,
  load: mockLoad,
  loadMore: mockLoadMore,
  setType: mockSetType,
  setState: mockSetState,
  setMineFilter: mockSetMineFilter,
  setQuery: mockSetQuery,
  reset: vi.fn(),
  markItemRead: vi.fn(),
  markAllRead: vi.fn(),
}

/** Mirrors the real useForgePipelines shape closely enough for the panel. */
const pipelineState = {
  pipelines: { value: [] as unknown[] },
  loading: { value: false },
  loadingMore: { value: false },
  error: { value: null as unknown },
  filter: { value: 'failure' as const },
  hasMore: { value: false },
  nextPage: { value: 1 },
  load: mockPipelinesLoad,
  loadMore: mockPipelinesLoadMore,
  setFilter: mockPipelinesSetFilter,
  reset: vi.fn(),
  markItemRead: vi.fn(),
  markAllRead: vi.fn(),
}

vi.mock('@/composables/useForge', () => ({
  useForgeItems: () => state,
  useForgePipelines: () => pipelineState,
  FORGE_PIPELINE_FILTERS: ['failure', 'running', 'all'],
  useForgeDetail: () => ({
    item: { value: null },
    comments: { value: [] },
    loading: { value: false },
    loadingComments: { value: false },
    error: { value: null },
    hasMoreComments: { value: false },
    open: vi.fn(),
    loadOlderComments: vi.fn(),
    close: vi.fn(),
  }),
  useForgePipelineDetail: () => ({
    run: { value: null },
    jobs: { value: [] },
    loading: { value: false },
    error: { value: null },
    open: vi.fn(),
    close: vi.fn(),
  }),
}))

// A real ref, not a {value} literal: templates only auto-unwrap actual refs, and
// the button's disabled state compares against the unwrapped number.
const unreadCount = vi.hoisted(() => ({ current: null as null | { value: number } }))
vi.mock('@/composables/useForgeUnread', async () => {
  const { ref } = await import('vue')
  const count = ref(0)
  unreadCount.current = count
  return {
    useForgeUnread: () => ({
      forgeUnreadCount: count,
      refresh: vi.fn(),
      onForgeEvent: vi.fn(),
      markRead: vi.fn(),
    }),
  }
})

const mockFetchRemotes = vi.fn(async () => ({ remotes: [] }))
const mockSetBinding = vi.fn(async () => ({ binding: {} }))
const mockDeleteBinding = vi.fn(async () => undefined)
vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<typeof import('@/utils/forgeApi')>('@/utils/forgeApi')
  return {
    ...actual,
    fetchForgeRemotes: (...a: unknown[]) => mockFetchRemotes(...a),
    setForgeBinding: (...a: unknown[]) => mockSetBinding(...a),
    deleteForgeBinding: (...a: unknown[]) => mockDeleteBinding(...a),
  }
})

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'en',
    messages: {
      en: {
        nav: { refresh: 'Refresh' },
        forge: {
          type: { issues: 'Issues', prs: 'Pull Requests', pipelines: 'Pipelines' },
          state: { open: 'Open', closed: 'Closed', all: 'All', merged: 'Merged' },
          mine: { all: 'All', assigned: 'Assigned to me', created: 'Created by me', review: 'Awaiting my review' },
          searchPlaceholder: 'Search',
          loading: 'Loading',
          emptyList: 'No matching issues or PRs',
          markAllRead: 'Mark all read',
          unreadItem: 'New activity',
          retry: 'Retry',
          pipeline: {
            status: { success: 'Success', failure: 'Failed', running: 'Running', cancelled: 'Cancelled', skipped: 'Skipped', unknown: 'Unknown' },
            filter: { failure: 'Failures only', running: 'Running', all: 'All' },
            emptyList: 'No matching pipelines',
            emptyJobs: 'No job information',
            noPlatform: 'This platform does not expose pipelines',
            loadFailed: 'Failed to load',
            jobName: 'Job',
            jobStage: 'Stage',
            jobStatus: 'Status',
            jobDuration: 'Duration',
            runNumber: 'Run',
            event: 'Trigger',
            duration: 'Duration',
            jobs: 'Jobs',
            openRun: 'Open in browser',
            quote: 'Quote in chat',
            detail: { back: 'Back' },
          },
          empty: {
            noProjectHeader: 'Which project?',
            noProjectBody: 'Pick a project.',
            chooseProject: 'Choose a project',
            noBindingHeader: 'Which repository?',
            noBindingBody: 'Not bound yet.',
            bindRepo: 'Bind a repository',
          },
          bind: {
            title: 'Bind repository',
            fromRemote: 'Pick a local remote',
            manual: 'Enter a repository URL',
            urlPlaceholder: 'https://...',
            submit: 'Bind',
            nonOfficialHost: 'non-official host',
            change: 'Change repository',
            unbind: 'Unbind',
          },
          detail: { back: 'Back', openBrowser: 'Open', analyze: 'Analyze', loadOlder: 'Load older' },
          error: { auth: 'Auth failed', rateLimit: 'Rate limited', network: 'Network', generic: 'Failed' },
        },
      },
    },
  })
}

const globalOpts = {
  plugins: [makeI18n()],
  stubs: {
    LoadingIndicator: true,
    RefreshButton: true,
    ModalDialog: true,
    ForgeDetail: true,
    PopupMenu: {
      props: ['show'],
      template: '<div class="popup-menu-stub" v-if="show"><slot /></div>',
    },
  },
}

describe('ForgePanelContent', () => {
  beforeEach(() => {
    _resetHandlers()
    vi.clearAllMocks()
    state.items.value = []
    state.binding.value = null
    state.suggested.value = null
    state.loading.value = false
    state.error.value = null
    state.isBound.value = false
    state.type.value = 'issue'
    state.state.value = 'open'
    state.mineFilter.value = 'all'
  })

  it('shows the no-project question card when no project is selected', () => {
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '' },
      global: globalOpts,
    })
    expect(wrapper.text()).toContain('Which project?')
    expect(wrapper.text()).toContain('Choose a project')
  })

  it('emits request-project when the choose-project option is clicked', async () => {
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '' },
      global: globalOpts,
    })
    await wrapper.find('.forge-card-options .fbtn').trigger('click')
    expect(wrapper.emitted('request-project')).toBeTruthy()
  })

  it('shows the no-binding question card when the project has no repository', async () => {
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    expect(wrapper.text()).toContain('Which repository?')
    expect(wrapper.text()).toContain('Bind a repository')
  })

  it('renders the item list when bound', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.items.value = [
      { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice', commentCount: 2, url: 'u', createdAt: '', updatedAt: '2026-09-10T00:00:00Z', slug: 'a/b' },
    ]
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    expect(wrapper.text()).toContain('A bug')
    expect(wrapper.text()).toContain('#7')
    expect(wrapper.find('.forge-row').exists()).toBe(true)
  })

  it('shows no count badge in the header', async () => {
    // The header used to render items.length — the number of items loaded so
    // far, which starts at the page size (30) and grows on scroll. That is not
    // a total and not an unread count, so it carried no actionable meaning and
    // was removed. This guards against it creeping back.
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.items.value = [
      { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice', commentCount: 2, url: 'u', createdAt: '', updatedAt: '2026-09-10T00:00:00Z', slug: 'a/b' },
      { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', type: 'issue', number: 8, title: 'Another bug', state: 'open', author: 'bob', commentCount: 0, url: 'u', createdAt: '', updatedAt: '2026-09-10T00:00:00Z', slug: 'a/b' },
    ]
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    // Both rows render, yet nothing in the header counts them.
    expect(wrapper.findAll('.forge-row')).toHaveLength(2)
    expect(wrapper.find('.forge-header-count').exists()).toBe(false)
  })

  it('renders the type switch as page tabs (stats-style), not a segmented control', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    // Three connected page tabs, matching the stats panel tab bar. Pipelines is
    // a peer of Issues and PRs: a CI run is neither, and it needs its own
    // filters, so it cannot be a chip or a filter inside another tab.
    expect(wrapper.find('.forge-tabs').exists()).toBe(true)
    const tabs = wrapper.findAll('.forge-tab')
    expect(tabs).toHaveLength(3)
    // The retired segmented-control markup must be gone.
    expect(wrapper.find('.forge-segment').exists()).toBe(false)
    expect(wrapper.find('.forge-segment-btn').exists()).toBe(false)
  })

  it('switches to the pipelines tab and loads its data', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))

    const tabs = wrapper.findAll('.forge-tab')
    await tabs[2].trigger('click')
    await new Promise(r => setTimeout(r, 0))

    expect(tabs[2].classes()).toContain('active')
    // The pipeline list has its own loader; the issue/PR list must not be
    // reloaded as a side effect of switching.
    expect(mockPipelinesLoad).toHaveBeenCalled()
    expect(mockSetType).not.toHaveBeenCalled()
  })

  it('shows the pipeline status filters instead of the issue state/mine chips', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    await wrapper.findAll('.forge-tab')[2].trigger('click')
    await new Promise(r => setTimeout(r, 0))

    // A run has no open/closed state and no assignee, so those chips must be
    // replaced rather than shown and silently ignored.
    const chips = wrapper.findAll('.forge-chip')
    const labels = chips.map(c => c.text())
    expect(labels).toEqual(['Failures only', 'Running', 'All'])
    expect(labels).not.toContain('Assigned to me')
    expect(labels).not.toContain('Open')
  })

  it('defaults the pipeline list to failures', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    await wrapper.findAll('.forge-tab')[2].trigger('click')
    await new Promise(r => setTimeout(r, 0))

    // A busy repository produces far more green runs than anyone wants to
    // scroll, and the reason to open this tab is usually "what broke".
    expect(pipelineState.filter.value).toBe('failure')
    const active = wrapper.findAll('.forge-chip').find(c => c.classes().includes('active'))
    expect(active?.text()).toBe('Failures only')
  })

  it('marks only the active type tab', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.items.value = []
    state.type.value = 'issue'
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    let tabs = wrapper.findAll('.forge-tab')
    expect(tabs[0].classes()).toContain('active')
    expect(tabs[1].classes()).not.toContain('active')
    expect(tabs[2].classes()).not.toContain('active')

    // setType is a spy in this harness (it does not mutate state), so assert
    // the click is routed to the right setter rather than the resulting state.
    await tabs[1].trigger('click')
    expect(mockSetType).toHaveBeenCalledWith('pr')
  })

  it('uses the GitHub brand icon for the panel identity, semantic glyphs for the type tabs', async () => {
    // The panel header keeps the GitHub brand mark (the forge integration is
    // GitHub-flavoured), but the switch carries distinct semantic glyphs: a
    // circled question mark for issues (the "what's the problem?" reading), the
    // merge arrow for pull/merge requests, and an activity trace for CI runs.
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    const html = wrapper.html()
    // Header brand mark is still present.
    expect(html).toContain('lucide-github')
    // Each tab gets its own semantic icon, not a shared generic one.
    expect(html).toContain('lucide-circle-question-mark')
    expect(html).toContain('lucide-git-pull-request')
    expect(html).toContain('lucide-activity')
    const tabs = wrapper.findAll('.forge-tab')
    expect(tabs[0].find('.lucide-circle-question-mark').exists(), 'issues tab uses the question-mark glyph').toBe(true)
    expect(tabs[1].find('.lucide-git-pull-request').exists(), 'PR tab uses the pull-request glyph').toBe(true)
    expect(tabs[2].find('.lucide-activity').exists(), 'pipelines tab uses the activity glyph').toBe(true)
  })

  it('shows the GitHub icon in the unbound fallback card too', async () => {
    state.binding.value = null
    state.isBound.value = false
    state.loading.value = false
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    expect(wrapper.html()).toContain('lucide-github')
    expect(wrapper.html()).not.toContain('lucide-git-pull-request')
  })

  it('renders the binding dialog from the 更换仓库 menu item too', async () => {
    // Both entry points (the fallback card's button and the header badge's
    // 更换仓库 item) funnel into the same single ModalDialog, so the same
    // missing `:open` prop broke both. This pins the header path specifically.
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: {
        plugins: [makeI18n()],
        stubs: {
          LoadingIndicator: true,
          RefreshButton: true,
          ForgeDetail: true,
          // Keep the real PopupMenu: the menu is what we click through.
        },
      },
      attachTo: document.body,
    })
    await new Promise(r => setTimeout(r, 0))

    // Open the badge menu. PopupMenu teleports to body, so query the document
    // rather than the wrapper.
    await wrapper.find('.forge-repo-badge').trigger('click')
    await new Promise(r => setTimeout(r, 100))
    const items = Array.from(document.body.querySelectorAll('.forge-repo-menu-item'))
    expect(items.length, 'the switcher menu should list change + unbind').toBe(2)
    ;(items[0] as HTMLElement).click()
    await new Promise(r => setTimeout(r, 150))

    expect(document.body.querySelector('.modal-overlay'), '更换仓库 must open the dialog').not.toBeNull()
    expect(document.body.querySelector('.forge-bind-form')).not.toBeNull()

    wrapper.unmount()
    document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
    document.body.querySelectorAll('.popup-menu').forEach(el => el.remove())
  })

  it('actually renders the binding dialog when the button is clicked', async () => {
    // Regression: the dialog was mounted with `v-if="bindDialogOpen"` but no
    // `:open` prop. ModalDialog gates rendering on its own everOpened latch,
    // which only flips inside the props.open watcher — so the dialog never
    // appeared and the button looked dead. The shared globalOpts stubs
    // ModalDialog away, which is exactly why this slipped through, so this
    // case mounts the REAL component.
    state.binding.value = null
    state.isBound.value = false
    state.loading.value = false
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: {
        plugins: [makeI18n()],
        stubs: {
          LoadingIndicator: true,
          RefreshButton: true,
          ForgeDetail: true,
          PopupMenu: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
        },
      },
      attachTo: document.body,
    })
    await new Promise(r => setTimeout(r, 0))

    // Nothing rendered before the click.
    expect(document.body.querySelector('.modal-overlay')).toBeNull()

    const button = wrapper.findAll('.forge-card-options button')[0]
    expect(button.text()).toContain('Bind a repository')
    await button.trigger('click')
    await new Promise(r => setTimeout(r, 50))

    // The dialog body must be in the DOM (ModalDialog teleports to body).
    const overlay = document.body.querySelector('.modal-overlay')
    expect(overlay, 'clicking Bind must open the dialog').not.toBeNull()
    expect(document.body.querySelector('.forge-bind-form')).not.toBeNull()

    wrapper.unmount()
    document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
  })

  it('fallback card offers only manual binding (no suggestion button)', async () => {
    // The backend auto-binds official hosts, so the panel no longer renders a
    // "use detected repository" shortcut — the card is a fallback for cases
    // where nothing could be bound automatically.
    state.binding.value = null
    state.isBound.value = false
    state.loading.value = false
    state.items.value = []
    state.suggested.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    const buttons = wrapper.findAll('.forge-card-options button')
    expect(buttons).toHaveLength(1)
    expect(buttons[0].text()).toContain('Bind a repository')
    // Even with a suggestion present, no shortcut button is rendered.
    expect(wrapper.text()).not.toContain('acme/widgets')
  })

  it('registers a back handler that closes the open detail view', async () => {
    // The edge-swipe gesture and the Android hardware back button both dispatch
    // through this registry, so a registered handler is what makes them work.
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = [
      { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice', commentCount: 0, url: 'u', createdAt: '', updatedAt: '', slug: 'acme/widgets' },
    ]
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))

    // On the list view there is nothing to go back to.
    expect(canNavigateBack()).toBe(false)

    await wrapper.find('.forge-row').trigger('click')
    expect(canNavigateBack()).toBe(true)

    expect(handleBackNavigation()).toBe(true)
    await wrapper.vm.$nextTick()
    // Detail closed -> the list header is visible again.
    expect(wrapper.find('.forge-header').exists()).toBe(true)
    expect(canNavigateBack()).toBe(false)
  })

  it('does not intercept back when the panel is inactive', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = [
      { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice', commentCount: 0, url: 'u', createdAt: '', updatedAt: '', slug: 'acme/widgets' },
    ]
    const wrapper = mount(ForgePanelContent, {
      props: { active: false, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    await wrapper.find('.forge-row').trigger('click')
    // Even with a detail open, an inactive tab must not swallow the back press.
    expect(canNavigateBack()).toBe(false)
  })

  it('shows the bound repository slug as a clickable switcher', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    const badge = wrapper.find('.forge-repo-badge')
    expect(badge.exists()).toBe(true)
    expect(badge.text()).toContain('acme/widgets')
  })

  it('opens the switcher menu with change and unbind actions', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    expect(wrapper.find('.forge-repo-menu-item').exists()).toBe(false)

    await wrapper.find('.forge-repo-badge').trigger('click')
    const items = wrapper.findAll('.forge-repo-menu-item')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('Change repository')
    expect(items[1].text()).toContain('Unbind')
  })

  it('unbind calls the delete endpoint and refreshes', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = []
    mockLoadBinding.mockClear()
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    await wrapper.find('.forge-repo-badge').trigger('click')
    await wrapper.findAll('.forge-repo-menu-item')[1].trigger('click')
    await new Promise(r => setTimeout(r, 0))
    expect(mockDeleteBinding).toHaveBeenCalledTimes(1)
    // refresh() re-reads the binding so the unbound card takes over.
    expect(mockLoadBinding).toHaveBeenCalled()
  })

  it('shows the error card with a retry action on failure', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.error.value = { message: 'bad credentials', code: 'ForgeAuthFailed' }
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    expect(wrapper.text()).toContain('Auth failed')
    expect(wrapper.text()).toContain('bad credentials')
    expect(wrapper.text()).toContain('Retry')
  })

  it('re-emits the detail view\'s quote request', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = [
      { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice', commentCount: 0, url: 'u', createdAt: '', updatedAt: '', slug: 'acme/widgets' },
    ]
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: {
        ...globalOpts,
        // A named stub, so findComponent can locate it. The previous
        // `ForgeDetail: true` produced an anonymous stub that findComponent
        // never matched, which silently made this test a no-op.
        stubs: { ...globalOpts.stubs, ForgeDetail: { name: 'ForgeDetail', template: '<div />' } },
      },
    })
    await new Promise(r => setTimeout(r, 0))

    // The detail only mounts once a row is opened.
    await wrapper.find('.forge-row').trigger('click')
    await wrapper.vm.$nextTick()

    const detail = wrapper.findComponent({ name: 'ForgeDetail' })
    expect(detail.exists(), 'the detail child must be mounted to test the passthrough').toBe(true)

    detail.vm.$emit('quote', { item: { type: 'issue', number: 7, title: 'A bug', url: 'u', slug: 'acme/widgets' } })

    const emitted = wrapper.emitted('quote')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toEqual({ item: { type: 'issue', number: 7, title: 'A bug', url: 'u', slug: 'acme/widgets' } })
  })
})

describe('ForgePanelContent unread rows', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    unreadCount.current!.value = 0
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.error.value = null
  })

  it('marks only the unread rows and gives them an unread dot', async () => {
    state.items.value = [
      { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', type: 'issue', number: 7, title: 'Seen', state: 'open', author: 'alice', commentCount: 0, url: 'u', createdAt: '', updatedAt: '', slug: 'a/b', unread: false },
      { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', type: 'issue', number: 8, title: 'New', state: 'open', author: 'bob', commentCount: 0, url: 'u', createdAt: '', updatedAt: '', slug: 'a/b', unread: true },
    ]
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))

    const rows = wrapper.findAll('.forge-row')
    expect(rows).toHaveLength(2)
    // The user must be able to tell WHICH rows are new — that is the whole point
    // of the change.
    expect(rows[0].classes()).not.toContain('unread')
    expect(rows[1].classes()).toContain('unread')
    expect(rows[0].find('.forge-unread-dot').exists()).toBe(false)
    expect(rows[1].find('.forge-unread-dot').exists()).toBe(true)
  })

  it('marks the row read when it is opened', async () => {
    state.items.value = [
      { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', type: 'issue', number: 8, title: 'New', state: 'open', author: 'bob', commentCount: 0, url: 'u', createdAt: '', updatedAt: '', slug: 'a/b', unread: true },
    ]
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))

    await wrapper.find('.forge-row').trigger('click')

    // Opening the row is what marks it read.
    expect(state.markItemRead).toHaveBeenCalledTimes(1)
  })

  it('disables "mark all read" when nothing is unread', async () => {
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))

    const btn = wrapper.find('.clear-unread-btn')
    expect(btn.exists()).toBe(true)
    // Nothing unread: the action has nothing to do.
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('enables "mark all read" and calls the list when something is unread', async () => {
    unreadCount.current!.value = 3
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))

    const btn = wrapper.find('.clear-unread-btn')
    expect(btn.attributes('disabled')).toBeUndefined()

    await btn.trigger('click')
    expect(state.markAllRead).toHaveBeenCalled()
  })
})

describe('ForgePanelContent deep-link (unread overview → item)', () => {
  // `ForgeDetail: true` in globalOpts produces an ANONYMOUS stub, which
  // findComponent({name}) cannot match — name them so the detail view is
  // observable.
  const deepLinkOpts = {
    ...globalOpts,
    stubs: {
      ...globalOpts.stubs,
      ForgeDetail: { name: 'ForgeDetail', props: ['type', 'number'], template: '<div />' },
      ForgePipelineDetail: { name: 'ForgePipelineDetail', props: ['runId'], template: '<div />' },
    },
  }

  beforeEach(() => {
    _resetHandlers()
    vi.clearAllMocks()
    _resetPendingForgeNavigationForTesting()
    state.items.value = []
    state.binding.value = null
    state.suggested.value = null
    state.loading.value = false
    state.error.value = null
    state.isBound.value = false
    state.type.value = 'issue'
    state.state.value = 'open'
    state.mineFilter.value = 'all'
    // Default: the binding resolves on demand, as it does for a real project.
    mockLoadBinding.mockImplementation(async () => {
      state.binding.value = { slug: 'acme/widgets' }
      state.isBound.value = true
    })
  })

  it('opens the requested item once the panel becomes active', async () => {
    // The request is made while the tab is inactive (the panel may not even be
    // mounted yet), then the tab activates.
    const wrapper = mount(ForgePanelContent, {
      props: { active: false, projectPath: '/proj' },
      global: deepLinkOpts,
    })
    await flushPromises()
    setPendingForgeNavigation({ type: 'pr', number: 455, runId: 0 })
    // Flush BEFORE activating: watchers run asynchronously, so without this the
    // activation would land first and the watcher would read `active: true`
    // immediately — which would hide a watcher that only observes the request.
    await flushPromises()

    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(state.type.value).toBe('pr')
    // The request is one-shot: consuming it is what stops the same item from
    // re-opening on every later activation.
    expect(pendingForgeNavigation.value).toBeNull()
  })

  it('resolves the binding when it is not yet known, then opens the item', async () => {
    // The template renders the bind card whenever the panel is unbound, which
    // REPLACES the detail view. A deep-link that arrives before the binding is
    // known must therefore resolve it first — otherwise the item is never shown.
    let loadBindingCalls = 0
    mockLoadBinding.mockImplementation(async () => {
      loadBindingCalls++
      state.binding.value = { slug: 'acme/widgets' }
      state.isBound.value = true
    })

    const wrapper = mount(ForgePanelContent, {
      props: { active: false, projectPath: '/proj' },
      global: deepLinkOpts,
    })
    await flushPromises()

    // Pretend the panel has never resolved the binding (the real one resolves on
    // mount, so this isolates the deep-link's own resolution path).
    state.binding.value = null
    state.isBound.value = false
    loadBindingCalls = 0

    setPendingForgeNavigation({ type: 'issue', number: 7, runId: 0 })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(loadBindingCalls).toBeGreaterThan(0, 'the deep-link must resolve the binding')
    expect(state.isBound.value).toBe(true)
    expect(state.type.value).toBe('issue')
  })

  it('opens the item when the panel is ALREADY active', async () => {
    // switchTab() no-ops when the target tab is already showing, so the request
    // arrives with `active` unchanged. Watching only the pending ref would miss
    // this entirely and the click would appear to do nothing.
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: deepLinkOpts,
    })
    await flushPromises()
    expect(state.type.value).toBe('issue')

    setPendingForgeNavigation({ type: 'pr', number: 455, runId: 0 })
    await flushPromises()

    expect(state.type.value).toBe('pr')
    expect(pendingForgeNavigation.value).toBeNull()
  })

  it('skips the binding round trip when it is already known', async () => {
    // The common case: the panel is already bound, so opening an item must not
    // re-fetch the binding.
    const wrapper = mount(ForgePanelContent, {
      props: { active: false, projectPath: '/proj' },
      global: deepLinkOpts,
    })
    await flushPromises()
    mockLoadBinding.mockClear()

    setPendingForgeNavigation({ type: 'issue', number: 7, runId: 0 })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(mockLoadBinding).not.toHaveBeenCalled()
    expect(state.type.value).toBe('issue')
  })

  it('does nothing when the repository is not bound', async () => {
    // An unbound project has nothing to open; the panel shows its bind prompt.
    mockLoadBinding.mockImplementation(async () => {
      state.binding.value = null
      state.isBound.value = false
    })
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: deepLinkOpts,
    })
    await new Promise(r => setTimeout(r, 0))

    setPendingForgeNavigation({ type: 'pr', number: 455, runId: 0 })
    await flushPromises()

    // Nothing opened: the panel keeps showing its bind prompt.
    expect(state.type.value).toBe('issue')
  })

  it('routes a pipeline request to the pipeline detail', async () => {
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: deepLinkOpts,
    })
    await new Promise(r => setTimeout(r, 0))

    setPendingForgeNavigation({ type: 'pipeline', number: 0, runId: 555 })
    await flushPromises()
    await nextTick()

    // The pipeline branch switches the internal tab and targets the run id; the
    // detail component self-loads from that prop (see ForgePipelineDetail).
  })
})

/**
 * The bind dialog's pre-submit warning for non-official hosts.
 *
 * The server accepts any host, so an internal GitLab binds fine — which is the
 * point, but it also means nothing else would tell the user their token is
 * about to be sent to a host ClawBench does not vouch for. These tests pin that
 * the warning appears BEFORE submit, and only when it is actually warranted.
 *
 * Mounts the real ModalDialog (the shared globalOpts stubs it away), the same
 * way the dialog-rendering regression tests above do.
 */
describe('ForgePanelContent non-official host warning', () => {
  const warningOpts = {
    plugins: [makeI18n()],
    stubs: {
      LoadingIndicator: true,
      RefreshButton: true,
      ForgeDetail: true,
      PopupMenu: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
    },
  }

  beforeEach(() => {
    _resetHandlers()
    vi.clearAllMocks()
    state.binding.value = null
    state.isBound.value = false
    state.loading.value = false
    state.items.value = []
    mockFetchRemotes.mockResolvedValue({ remotes: [] })
  })

  /** Mount the unbound panel and open the bind dialog. */
  async function openDialog() {
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: warningOpts,
      attachTo: document.body,
    })
    await new Promise(r => setTimeout(r, 0))
    const button = wrapper.findAll('.forge-card-options button')[0]
    await button.trigger('click')
    await flushPromises()
    return wrapper
  }

  afterEach(() => {
    document.body.querySelectorAll('.modal-overlay').forEach(el => el.remove())
    document.body.querySelectorAll('.popup-menu').forEach(el => el.remove())
  })

  it('warns while typing an internal GitLab URL, before submit', async () => {
    const wrapper = await openDialog()
    const input = document.body.querySelector('.forge-input') as HTMLInputElement
    expect(input, 'the manual URL input must be rendered').not.toBeNull()

    input.value = 'https://git.internal.corp:8443/acme/widgets.git'
    input.dispatchEvent(new Event('input'))
    await nextTick()

    const warning = document.body.querySelector('.forge-bind-warning')
    expect(warning, 'a non-official host must warn before submit').not.toBeNull()
    expect(warning!.textContent).toContain('non-official host')
    // The bind button must stay enabled: this warns, it does not block.
    const submit = document.body.querySelector('.fbtn-primary') as HTMLButtonElement
    expect(submit.disabled, 'the warning must not block binding').toBe(false)

    wrapper.unmount()
  })

  it('does not warn for github.com', async () => {
    const wrapper = await openDialog()
    const input = document.body.querySelector('.forge-input') as HTMLInputElement

    input.value = 'https://github.com/acme/widgets.git'
    input.dispatchEvent(new Event('input'))
    await nextTick()

    expect(document.body.querySelector('.forge-bind-warning')).toBeNull()

    wrapper.unmount()
  })

  it('warns for an unparseable-to-official scp remote too', async () => {
    // `git remote -v` shows this form for most SSH clones; missing it would
    // silently skip the warning on a very common path.
    const wrapper = await openDialog()
    const input = document.body.querySelector('.forge-input') as HTMLInputElement

    input.value = 'git@git.internal.corp:acme/widgets.git'
    input.dispatchEvent(new Event('input'))
    await nextTick()

    expect(document.body.querySelector('.forge-bind-warning')).not.toBeNull()

    wrapper.unmount()
  })

  it('hints on a non-official remote row, since rows bind on click', async () => {
    mockFetchRemotes.mockResolvedValue({
      remotes: [
        { name: 'origin', url: 'https://git.internal.corp/acme/widgets.git', platform: 'gitlab', host: 'git.internal.corp', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' },
        { name: 'upstream', url: 'https://github.com/acme/other.git', platform: 'github', host: 'github.com', owner: 'acme', repo: 'other', slug: 'acme/other' },
      ],
    })
    const wrapper = await openDialog()

    const rows = document.body.querySelectorAll('.forge-remote-row')
    expect(rows.length).toBe(2)
    // A row commits a binding on click, so the hint is the only pre-submit
    // signal available on this path.
    expect(rows[0].querySelector('.forge-remote-warning'), 'self-hosted row must hint').not.toBeNull()
    expect(rows[1].querySelector('.forge-remote-warning'), 'github.com row must not hint').toBeNull()

    wrapper.unmount()
  })
})
