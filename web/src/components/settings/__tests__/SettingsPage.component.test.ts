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

const { pushNav, popNav, handleRestartNeeded, handleRestart, checkAllGuards, mockNavStack, mockCurrentCategory, mockRestartDialogVisible, mockChangedColdFields, mockNeedsRestart, mockRestarting, mockServerConfig } = vi.hoisted(() => {
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

vi.mock('@/composables/useSettingsNavigation', () => ({
  useSettingsNavigation: () => ({
    t: (k: string) => k,
    navStack: mockNavStack,
    currentCategory: mockCurrentCategory,
    pushNav,
    popNav,
    restartDialogVisible: mockRestartDialogVisible,
    changedColdFields: mockChangedColdFields,
    needsRestart: mockNeedsRestart,
    restarting: mockRestarting,
    handleRestartNeeded,
    handleRestart,
    checkAllGuards,
    loadConfig: vi.fn(),
  }),
}))

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

  it('shows back button when navStack non-empty', () => {
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    expect(wrapper.find('.settings-page__back').exists()).toBe(true)
  })

  it('back button triggers handleBack (no guard violations)', async () => {
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    await wrapper.find('.settings-page__back').trigger('click')
    expect(checkAllGuards).toHaveBeenCalled()
    expect(popNav).toHaveBeenCalled()
  })

  it('back button cancels if confirm returns false', async () => {
    checkAllGuards.mockReturnValueOnce(false)
    mockDialogConfirm.mockResolvedValueOnce(false)
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    await wrapper.find('.settings-page__back').trigger('click')
    await flushPromises()
    expect(popNav).not.toHaveBeenCalled()
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

describe('SettingsPage — currentCategoryTitle', () => {
  it('returns empty string when no category', () => {
    const wrapper = mountPage()
    const vm = wrapper.vm as any
    expect(vm.currentCategoryTitle).toBe('')
  })

  it('uses sub-page route title key', () => {
    const wrapper = mountPage({}, { navStack: ['page:detail'], currentCategory: 'page:detail' })
    const vm = wrapper.vm as any
    expect(vm.currentCategoryTitle).toBe('settings.sub.page:detail')
  })

  it('uses settings.categories.X for normal categories', () => {
    const wrapper = mountPage({}, { navStack: ['general'], currentCategory: 'general' })
    const vm = wrapper.vm as any
    expect(vm.currentCategoryTitle).toBe('settings.categories.general')
  })

  it('uses agent name for agents:{id} routes', () => {
    const wrapper = mountPage({}, { navStack: ['agents:abc'], currentCategory: 'agents:abc' })
    const vm = wrapper.vm as any
    expect(vm.currentCategoryTitle).toBe('Agent One')
  })

  it('falls back to categories.agents when agent not found', () => {
    mockGetAgent.mockReturnValueOnce(null)
    const wrapper = mountPage({}, { navStack: ['agents:unknown'], currentCategory: 'agents:unknown' })
    const vm = wrapper.vm as any
    expect(vm.currentCategoryTitle).toBe('settings.categories.agents')
  })
})

describe('SettingsPage — serverVersion computed', () => {
  it('returns version string from serverConfig', () => {
    const wrapper = mountPage()
    const vm = wrapper.vm as any
    expect(vm.serverVersion).toBe('1.2.3')
  })
})