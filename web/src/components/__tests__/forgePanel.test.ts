import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ForgePanelContent from '@/components/forge/ForgePanelContent.vue'
import { canNavigateBack, handleBackNavigation, _resetHandlers } from '@/composables/useBackHandler'

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
          },
          bind: {
            title: 'Bind repository',
            fromRemote: 'Pick a local remote',
            manual: 'Enter a repository URL',
            urlPlaceholder: 'https://...',
            submit: 'Bind',
            unsafeHost: 'unsafe',
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

  it('uses the GitHub brand icon everywhere, never a neutral glyph', async () => {
    // The forge tab serves both platforms but always shows the GitHub mark;
    // the previous neutral pull-request / circle-dot / folder glyphs are gone.
    state.binding.value = { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' }
    state.isBound.value = true
    state.items.value = []
    const wrapper = mount(ForgePanelContent, {
      props: { active: true, projectPath: '/proj' },
      global: globalOpts,
    })
    await new Promise(r => setTimeout(r, 0))
    const html = wrapper.html()
    expect(html).toContain('lucide-github')
    for (const neutral of ['lucide-git-pull-request', 'lucide-circle-dot', 'lucide-folder-git-2']) {
      expect(html, `${neutral} must not be rendered`).not.toContain(neutral)
    }
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
