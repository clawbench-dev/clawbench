import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, defineComponent } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string, params?: any) => params ? `${k}:${JSON.stringify(params)}` : k, locale: ref('en') }),
  createI18n: (opts: any) => ({
    global: { t: (k: string) => k, locale: ref(opts?.locale ?? 'en') },
    install() {},
  }),
}))

const { pushNav, popNav, truncateNav, handleRestartNeeded, handleRestart, checkAllGuards, mockNavStack, mockCurrentCategory, mockRestartDialogVisible, mockChangedColdFields, mockNeedsRestart, mockRestarting, mockServerConfig } = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue')
  const ns = ref<string[]>([])
  const cc = ref<string | null>(null)
  const rdv = ref(false)
  const ccf = ref<string[]>([])
  const nr = ref(false)
  const rs = ref(false)
  const sc = ref<{ version: string } | null>({ version: '1.2.3' })
  return {
    pushNav: vi.fn((id: string) => { ns.value.push(id) }),
    popNav: vi.fn(() => { ns.value.pop() }),
    truncateNav: vi.fn((depth: number) => { ns.value.splice(depth) }),
    handleRestartNeeded: vi.fn(),
    handleRestart: vi.fn(),
    checkAllGuards: vi.fn(() => true),
    mockNavStack: ns,
    mockCurrentCategory: cc,
    mockRestartDialogVisible: rdv,
    mockChangedColdFields: ccf,
    mockNeedsRestart: nr,
    mockRestarting: rs,
    mockServerConfig: sc,
    loadConfig: vi.fn(),
  }
})

// NOTE: the settings-nav deep-link relies on a REAL module-level ref so that
// SettingsPage's `watch(pendingSettingsCategory)` fires when the test mutates
// it (a ref created via require('vue') in vi.hoisted belongs to a different
// Vue copy and cannot trigger the component's watcher). We therefore re-use
// the real module's exports and only stub useSettingsNavigation() itself.
vi.mock('@/composables/useSettingsNavigation', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useSettingsNavigation')>()
  return {
    ...actual,
    useSettingsNavigation: () => ({
      t: (k: string) => k,
      navStack: mockNavStack,
      currentCategory: mockCurrentCategory,
      pushNav,
      popNav,
      truncateNav,
      restartDialogVisible: mockRestartDialogVisible,
      changedColdFields: mockChangedColdFields,
      needsRestart: mockNeedsRestart,
      restarting: mockRestarting,
      handleRestartNeeded,
      handleRestart,
      checkAllGuards,
      loadConfig: vi.fn(),
    }),
  }
})

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: mockServerConfig,
  }),
}))

const mockGetAgent = vi.fn(() => ({ name: 'Agent One' }))
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgent: mockGetAgent }),
}))

const mockDialogConfirm = vi.fn(() => Promise.resolve(true))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockDialogConfirm }),
}))

vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: vi.fn(),
  PRIORITY_PAGE: 100,
}))

vi.mock('@/components/settings/settingsFieldMap', () => ({
  isSubPageRoute: (id: string) => id.includes(':') && !id.startsWith('agents:'),
  getSubPageTitleKey: (id: string) => `settings.sub.${id}`,
}))

const SettingsIndexStub = defineComponent({
  name: 'SettingsIndex',
  emits: ['navigate'],
  setup() { return {} },
  template: '<div class="settings-index-stub" @click="$emit(\'navigate\', \'general\')" />',
})

const SettingsCategoryStub = defineComponent({
  name: 'SettingsCategory',
  props: { categoryId: { default: '' } },
  emits: ['navigate', 'restart-needed', 'restart-requested'],
  setup() { return {} },
  template: `<div class="settings-category-stub" :data-cat="String(categoryId || '')" @click="$emit('navigate', 'agents:abc')" />`,
})

const SettingsRestartDialogStub = defineComponent({
  name: 'SettingsRestartDialog',
  props: ['changedFields'],
  emits: ['restart', 'later'],
  setup() { return {} },
  template: '<div class="restart-dialog-stub" @click="$emit(\'later\')" />',
})

vi.mock('lucide-vue-next', () => ({
  RefreshCw: { template: '<svg />' },
  ChevronLeft: { template: '<svg />' },
  Settings: { template: '<svg />' },
}))

import SettingsPage from '@/components/settings/SettingsPage.vue'

// Real module exports (see the vi.mock factory above — the real module's
// pending ref is used so SettingsPage's module-level watch fires on mutation).
import { pendingSettingsCategory } from '@/composables/useSettingsNavigation'

const i18n = (require('vue-i18n') as any).createI18n({ legacy: false, locale: 'en' })

function mountPage(props: Record<string, unknown> = {}, initialState: Record<string, any> = {}) {
  mockNavStack.value = []
  mockCurrentCategory.value = null
  mockRestartDialogVisible.value = false
  mockChangedColdFields.value = []
  mockNeedsRestart.value = false
  mockRestarting.value = false

  if ('navStack' in initialState) mockNavStack.value = initialState.navStack
  if ('currentCategory' in initialState) mockCurrentCategory.value = initialState.currentCategory
  if ('restartDialogVisible' in initialState) mockRestartDialogVisible.value = initialState.restartDialogVisible
  if ('changedColdFields' in initialState) mockChangedColdFields.value = initialState.changedColdFields
  if ('needsRestart' in initialState) mockNeedsRestart.value = initialState.needsRestart
  if ('restarting' in initialState) mockRestarting.value = initialState.restarting
  if ('pendingCategory' in initialState) pendingSettingsCategory.value = initialState.pendingCategory
  else pendingSettingsCategory.value = null

  return mount(SettingsPage, {
    props: { ...props },
    global: {
      plugins: [i18n],
      stubs: {
        SettingsIndex: SettingsIndexStub,
        SettingsCategory: SettingsCategoryStub,
        SettingsRestartDialog: SettingsRestartDialogStub,
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mockNavStack.value = []
  mockCurrentCategory.value = null
  mockRestartDialogVisible.value = false
  mockNeedsRestart.value = false
  mockRestarting.value = false
  mockChangedColdFields.value = []
  pendingSettingsCategory.value = null
})

describe('SettingsPage — mount', () => {
  it('mounts without errors', () => {
    const wrapper = mountPage()
    expect(wrapper.exists()).toBe(true)
    expect(wrapper.find('.settings-page').exists()).toBe(true)
  })

  it('renders SettingsIndex when navStack is empty', () => {
    const wrapper = mountPage()
    expect(wrapper.find('.settings-index-stub').exists()).toBe(true)
  })

  it('renders SettingsCategory when navStack has entries', () => {
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    expect(wrapper.find('.settings-category-stub').exists()).toBe(true)
  })
})

describe('SettingsPage — header', () => {
  it('shows default header when navStack empty', () => {
    const wrapper = mountPage()
    expect(wrapper.find('.settings-page__header').exists()).toBe(true)
  })

  it('shows version when serverVersion is set', () => {
    const wrapper = mountPage()
    expect(wrapper.find('.settings-page__version').exists()).toBe(true)
    expect(wrapper.text()).toContain('1.2.3')
  })

  it('shows the breadcrumb (and no back button) when navStack non-empty', () => {
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    expect(wrapper.find('.settings-breadcrumb').exists()).toBe(true)
    // The header has no back button any more — the root crumb is the way back,
    // matching the task panel's header.
    expect(wrapper.find('.settings-page__back').exists()).toBe(false)
  })

  it('clicking the root crumb goes back (no guard violations)', async () => {
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    await wrapper.findAll('.crumb')[0].trigger('click')
    await flushPromises()
    expect(checkAllGuards).toHaveBeenCalled()
    expect(truncateNav).toHaveBeenCalledWith(0)
  })

  it('root crumb cancels if confirm returns false', async () => {
    checkAllGuards.mockReturnValueOnce(false)
    mockDialogConfirm.mockResolvedValueOnce(false)
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    await wrapper.findAll('.crumb')[0].trigger('click')
    await flushPromises()
    expect(truncateNav).not.toHaveBeenCalled()
  })
})

describe('SettingsPage — deep link from theme picker (more appearance options)', () => {
  it('opens the pending category on first mount (lazy TabPanel mount)', async () => {
    const wrapper = mountPage({}, { pendingCategory: 'appearance' })
    await flushPromises()
    expect(pushNav).toHaveBeenCalledWith('appearance')
    expect(pendingSettingsCategory.value).toBeNull()
    wrapper.unmount()
  })

  it('opens the pending category when the settings tab becomes active', async () => {
    const wrapper = mountPage({}, { active: false })
    pendingSettingsCategory.value = 'appearance'
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(pushNav).toHaveBeenCalledWith('appearance')
    expect(pendingSettingsCategory.value).toBeNull()
    wrapper.unmount()
  })

  it('opens the pending category when settings is already the active tab (switchTab no-op)', async () => {
    const wrapper = mountPage({ active: true })
    pendingSettingsCategory.value = 'appearance'
    await wrapper.vm.$nextTick()
    await flushPromises()
    expect(pushNav).toHaveBeenCalledWith('appearance')
    expect(pendingSettingsCategory.value).toBeNull()
    wrapper.unmount()
  })

  it('does not duplicate a category that is already open', async () => {
    const wrapper = mountPage({ active: true }, { navStack: ['appearance'], currentCategory: 'appearance' })
    pendingSettingsCategory.value = 'appearance'
    await wrapper.vm.$nextTick()
    await flushPromises()
    expect(pushNav).not.toHaveBeenCalled()
    expect(pendingSettingsCategory.value).toBeNull()
    wrapper.unmount()
  })

  it('confirms before leaving a category with unsaved changes', async () => {
    checkAllGuards.mockReturnValueOnce(false)
    mockDialogConfirm.mockResolvedValueOnce(false) // user keeps editing
    const wrapper = mountPage({ active: true }, { navStack: ['general'], currentCategory: 'general' })
    pendingSettingsCategory.value = 'appearance'
    await wrapper.vm.$nextTick()
    await flushPromises()
    expect(mockDialogConfirm).toHaveBeenCalled()
    expect(pushNav).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('opens the category after the user confirms discarding unsaved changes', async () => {
    checkAllGuards.mockReturnValueOnce(false)
    mockDialogConfirm.mockResolvedValueOnce(true) // discard
    const wrapper = mountPage({ active: true }, { navStack: ['general'], currentCategory: 'general' })
    pendingSettingsCategory.value = 'appearance'
    await wrapper.vm.$nextTick()
    await flushPromises()
    expect(pushNav).toHaveBeenCalledWith('appearance')
    expect(pendingSettingsCategory.value).toBeNull()
    wrapper.unmount()
  })

  it('does nothing when no category is pending', async () => {
    const wrapper = mountPage({ active: true })
    await wrapper.vm.$nextTick()
    await flushPromises()
    expect(pushNav).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('SettingsPage — restart dialog', () => {
  it('shows restart dialog when restartDialogVisible=true', () => {
    const wrapper = mountPage({}, { restartDialogVisible: true, changedColdFields: ['foo'] })
    expect(wrapper.find('.restart-dialog-stub').exists()).toBe(true)
  })

  it('shows footer restart button when needsRestart=true', () => {
    const wrapper = mountPage({}, { needsRestart: true })
    expect(wrapper.find('.settings-restart-btn').exists()).toBe(true)
  })

  it('clicking footer restart button calls handleRestart', async () => {
    const wrapper = mountPage({}, { needsRestart: true })
    await wrapper.find('.settings-restart-btn').trigger('click')
    expect(handleRestart).toHaveBeenCalled()
  })
})

describe('SettingsPage — breadcrumbs', () => {
  it('renders a single root crumb when no category is open', () => {
    const wrapper = mountPage()
    // The index view shows the plain title, not the breadcrumb.
    expect(wrapper.find('.settings-breadcrumb').exists()).toBe(false)
    expect(wrapper.find('.settings-page__title').text()).toBe('nav.settings')
  })

  it('renders root + category crumbs for a normal category', () => {
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    const crumbs = wrapper.findAll('.crumb')
    expect(crumbs.map(c => c.text())).toEqual(['nav.settings', 'settings.categories.general'])
  })

  it('renders a three-level trail for a sub-page route', () => {
    const wrapper = mountPage({}, { navStack: ['tts', 'tts:tts_engine'], currentCategory: 'tts:tts_engine' })
    const crumbs = wrapper.findAll('.crumb')
    expect(crumbs.map(c => c.text())).toEqual([
      'nav.settings',
      'settings.categories.tts',
      'settings.sub.tts:tts_engine',
    ])
  })

  it('uses the agent name for agents:{id} routes', () => {
    const wrapper = mountPage({}, { navStack: ['agents:abc'], currentCategory: 'agents:abc' })
    const crumbs = wrapper.findAll('.crumb')
    expect(crumbs.map(c => c.text())).toEqual(['nav.settings', 'Agent One'])
  })

  it('falls back to categories.agents when the agent is not found', () => {
    mockGetAgent.mockReturnValueOnce(null)
    const wrapper = mountPage({}, { navStack: ['agents:unknown'], currentCategory: 'agents:unknown' })
    const crumbs = wrapper.findAll('.crumb')
    expect(crumbs.map(c => c.text())).toEqual(['nav.settings', 'settings.categories.agents'])
  })

  it('marks only the last crumb as current', () => {
    const wrapper = mountPage({}, { navStack: ['tts', 'tts:tts_engine'], currentCategory: 'tts:tts_engine' })
    const crumbs = wrapper.findAll('.crumb')
    expect(crumbs[0].classes()).not.toContain('current')
    expect(crumbs[1].classes()).not.toContain('current')
    expect(crumbs[2].classes()).toContain('current')
  })

  it('clicking the root crumb truncates the stack to depth 0', async () => {
    const wrapper = mountPage({}, { navStack: ['tts', 'tts:tts_engine'], currentCategory: 'tts:tts_engine' })
    await wrapper.findAll('.crumb')[0].trigger('click')
    await flushPromises()
    expect(truncateNav).toHaveBeenCalledWith(0)
  })

  it('clicking an intermediate crumb truncates to that depth', async () => {
    const wrapper = mountPage({}, { navStack: ['tts', 'tts:tts_engine'], currentCategory: 'tts:tts_engine' })
    await wrapper.findAll('.crumb')[1].trigger('click')
    await flushPromises()
    expect(truncateNav).toHaveBeenCalledWith(1)
  })

  it('clicking the current crumb does nothing', async () => {
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    await wrapper.findAll('.crumb')[1].trigger('click')
    await flushPromises()
    expect(truncateNav).not.toHaveBeenCalled()
  })

  it('confirms before leaving when a panel has unsaved changes', async () => {
    checkAllGuards.mockReturnValueOnce(false)
    mockDialogConfirm.mockResolvedValueOnce(false) // keep editing
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    await wrapper.findAll('.crumb')[0].trigger('click')
    await flushPromises()
    expect(mockDialogConfirm).toHaveBeenCalled()
    expect(truncateNav).not.toHaveBeenCalled()
  })

  it('jumps when the user confirms discarding unsaved changes', async () => {
    checkAllGuards.mockReturnValueOnce(false)
    mockDialogConfirm.mockResolvedValueOnce(true) // discard
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    await wrapper.findAll('.crumb')[0].trigger('click')
    await flushPromises()
    expect(truncateNav).toHaveBeenCalledWith(0)
  })
})

describe('SettingsPage — serverVersion computed', () => {
  it('returns version string from serverConfig', () => {
    const wrapper = mountPage()
    const vm = wrapper.vm as any
    expect(vm.serverVersion).toBe('1.2.3')
  })
})