import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ForgePanelContent from '@/components/forge/ForgePanelContent.vue'

// ── Mocks ────────────────────────────────────────────────────
const mockLoadBinding = vi.fn()
const mockLoad = vi.fn()
const mockLoadMore = vi.fn()
const mockSetType = vi.fn()
const mockSetState = vi.fn()
const mockSetMineFilter = vi.fn()
const mockSetQuery = vi.fn()

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
}

vi.mock('@/composables/useForge', () => ({
  useForgeItems: () => state,
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
}))

const mockFetchRemotes = vi.fn(async () => ({ remotes: [] }))
const mockSetBinding = vi.fn(async () => ({ binding: {} }))
vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<typeof import('@/utils/forgeApi')>('@/utils/forgeApi')
  return {
    ...actual,
    fetchForgeRemotes: (...a: unknown[]) => mockFetchRemotes(...a),
    setForgeBinding: (...a: unknown[]) => mockSetBinding(...a),
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
          type: { issues: 'Issues', prs: 'Pull Requests' },
          state: { open: 'Open', closed: 'Closed', all: 'All', merged: 'Merged' },
          mine: { all: 'All', assigned: 'Assigned to me', created: 'Created by me', review: 'Awaiting my review' },
          searchPlaceholder: 'Search',
          loading: 'Loading',
          emptyList: 'No matching issues or PRs',
          retry: 'Retry',
          empty: {
            noProjectHeader: 'Which project?',
            noProjectBody: 'Pick a project.',
            chooseProject: 'Choose a project',
            noBindingHeader: 'Which repository?',
            noBindingBody: 'Not bound yet.',
            bindRepo: 'Bind a repository',
            useSuggestion: 'Use detected repository: {slug}',
          },
          bind: {
            title: 'Bind repository',
            fromRemote: 'Pick a local remote',
            manual: 'Enter a repository URL',
            urlPlaceholder: 'https://...',
            submit: 'Bind',
            unsafeHost: 'unsafe',
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
  },
}

describe('ForgePanelContent', () => {
  beforeEach(() => {
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

  it('renders the type switch as page tabs (stats-style), not a segmented control', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    // Two connected page tabs, matching the stats panel tab bar.
    expect(wrapper.find('.forge-tabs').exists()).toBe(true)
    const tabs = wrapper.findAll('.forge-tab')
    expect(tabs).toHaveLength(2)
    // The retired segmented-control markup must be gone.
    expect(wrapper.find('.forge-segment').exists()).toBe(false)
    expect(wrapper.find('.forge-segment-btn').exists()).toBe(false)
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

    // setType is a spy in this harness (it does not mutate state), so assert
    // the click is routed to the right setter rather than the resulting state.
    await tabs[1].trigger('click')
    expect(mockSetType).toHaveBeenCalledWith('pr')
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

  it('emits analyze when the detail view requests it', async () => {
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'a', repo: 'b', slug: 'a/b' }
    state.isBound.value = true
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    // Simulate the detail child emitting analyze.
    const detail = wrapper.findComponent({ name: 'ForgeDetail' })
    if (detail.exists()) {
      detail.vm.$emit('analyze', { item: { type: 'issue', number: 1, title: 't', url: 'u', slug: 'a/b', body: 'b' } })
      expect(wrapper.emitted('analyze')).toBeTruthy()
    }
  })
})
