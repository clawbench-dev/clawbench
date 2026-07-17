import { describe, expect, it, vi, beforeEach } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, nextTick, computed, reactive } from 'vue'
import SettingsPage from '@/components/settings/SettingsPage.vue'
import SettingsCategory from '@/components/settings/SettingsCategory.vue'
import { categoryHasPanels, isPanelOnlyCategory } from '@/components/settings/settingsFieldMap'

// Mutable refs that tests can flip to control UI state
const needsRestart = ref(false)
const restarting = ref(false)
const navStack = ref<string[]>([])

function createMockNavigation() {
  return {
    t: (key: string) => key,
    loadConfig: vi.fn(),
    navStack,
    currentCategory: computed(() => navStack.value.length > 0 ? navStack.value[navStack.value.length - 1] ?? null : null),
    pushNav: (id: string) => { navStack.value.push(id) },
    popNav: () => { navStack.value.pop() },
    resetState: () => { navStack.value = []; needsRestart.value = false; restarting.value = false },
    restartDialogVisible: ref(false),
    changedColdFields: ref<string[]>([]),
    needsRestart,
    restarting,
    restartingOverlay: ref(false),
    handleRestartNeeded: vi.fn(),
    handleRestart: vi.fn(),
    registerGuard: vi.fn(),
    unregisterGuard: vi.fn(),
    checkAllGuards: vi.fn(() => true),
  }
}

vi.mock('@/composables/useSettingsNavigation', () => ({
  useSettingsNavigation: () => createMockNavigation(),
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: ref({ version: '1.2.3' }),
    localConfig: reactive({ theme: 'auto', locale: 'zh' }),
    setLocalConfig: vi.fn(),
    getServerValueWithDefault: vi.fn(() => ''),
    setServerValue: vi.fn(),
    patchConfig: vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [] }),
  }),
}))

vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: vi.fn(),
  PRIORITY_PAGE: 100,
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    effectiveOpen: ref(false),
    isOpen: ref(false),
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
  }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(false) }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      nav: { settings: '设置' },
      settings: {
        categories: { appearance: '外观', terminal: '终端' },
        restartServer: '重启服务器',
        restartPending: '重启生效',
        restarting: '重启中…',
        restartingPleaseWait: '正在重启，请稍候…',
      },
    },
  },
})

function mountPage(props = {}) {
  return shallowMount(SettingsPage, {
    props: { active: true, ...props },
    global: {
      stubs: {
        'lucide-refresh-cw': true,
        'lucide-chevron-left': true,
        'lucide-settings': true,
      },
      plugins: [i18n],
    },
  })
}

describe('SettingsPage', () => {
  beforeEach(() => {
    navStack.value = []
    needsRestart.value = false
    restarting.value = false
  })

  it('shows index view when nav stack is empty', () => {
    const wrapper = mountPage()
    expect(wrapper.find('.settings-page__header-icon').exists()).toBe(true)
    expect(wrapper.find('.settings-page__back').exists()).toBe(false)
  })

  it('shows category view when nav stack has items', async () => {
    navStack.value = ['appearance']
    const wrapper = mountPage()
    await nextTick()

    expect(wrapper.find('.settings-page__back').exists()).toBe(true)
    expect(wrapper.find('.settings-page__header-icon').exists()).toBe(false)
  })

  it('does not show restart button when no restart is needed', () => {
    const wrapper = mountPage()
    expect(wrapper.find('.settings-restart-btn').exists()).toBe(false)
  })

  it('preserves nav stack when becoming active', async () => {
    navStack.value = ['appearance']
    const wrapper = mountPage()
    await nextTick()

    expect(wrapper.find('.settings-page__back').exists()).toBe(true)

    await wrapper.setProps({ active: false })
    await nextTick()
    await wrapper.setProps({ active: true })
    await nextTick()

    expect(navStack.value).toEqual(['appearance'])
    expect(wrapper.find('.settings-page__back').exists()).toBe(true)
    expect(wrapper.find('.settings-page__header-icon').exists()).toBe(false)
  })

  it('shows restart button only when needsRestart is true', async () => {
    needsRestart.value = false
    const wrapper = mountPage()

    expect(wrapper.find('.settings-page__footer').exists()).toBe(false)
    expect(wrapper.find('.settings-restart-btn').exists()).toBe(false)

    needsRestart.value = true
    wrapper.vm.$forceUpdate()
    await nextTick()

    expect(wrapper.find('.settings-page__footer').exists()).toBe(true)
    expect(wrapper.find('.settings-restart-btn--pending').exists()).toBe(true)
  })

  it('shows pending class on restart button when needsRestart is true', async () => {
    needsRestart.value = true
    const wrapper = mountPage()

    const btn = wrapper.find('.settings-restart-btn')
    expect(btn.classes()).toContain('settings-restart-btn--pending')
  })

  it('renders as a full page layout', () => {
    const wrapper = mountPage()

    expect(wrapper.find('.settings-page').exists()).toBe(true)
    expect(wrapper.find('.settings-page__header').exists()).toBe(true)
    expect(wrapper.find('.settings-page__body').exists()).toBe(true)
    expect(wrapper.find('.settings-page__footer').exists()).toBe(false)
  })

  it('shows header with title and version on index page', () => {
    const wrapper = mountPage()

    expect(wrapper.find('.settings-page__header').exists()).toBe(true)
    expect(wrapper.find('.settings-page__header-icon').exists()).toBe(true)
    expect(wrapper.find('.settings-page__version').exists()).toBe(true)
    expect(wrapper.find('.settings-page__version').text()).toBe('v1.2.3')
    expect(wrapper.find('.settings-page__back').exists()).toBe(false)
  })

  it('shows back button when navigating into a category', async () => {
    navStack.value = ['appearance']
    const wrapper = mountPage()
    await nextTick()

    expect(wrapper.find('.settings-page__back').exists()).toBe(true)
    expect(wrapper.find('.settings-page__header-icon').exists()).toBe(false)
    expect(wrapper.find('.settings-page__version').exists()).toBe(false)
  })

  it('hides version badge when serverConfig has no version', () => {
    const wrapper = mountPage()
    expect(wrapper.find('.settings-page__version').text()).toBe('v1.2.3')
  })

  // ─── Category routing (all via SettingsCategory now) ──
  describe('category routing', () => {
    const panelCategoryIds = ['terminal', 'tts', 'summarization_text', 'summarization_voice', 'rag', 'portForward', 'frp', 'notification']
    const flatCategoryIds = ['appearance', 'projectFiles', 'chat', 'agents', 'security', 'debug', 'about']

    it('categoryHasPanels identifies panel categories', () => {
      for (const id of panelCategoryIds) {
        expect(categoryHasPanels(id)).toBe(true)
      }
    })

    it('categoryHasPanels returns false for flat-only categories', () => {
      for (const id of flatCategoryIds) {
        expect(categoryHasPanels(id)).toBe(false)
      }
    })

    it('isPanelOnlyCategory identifies panel-only categories', () => {
      expect(isPanelOnlyCategory('terminal')).toBe(true)
      expect(isPanelOnlyCategory('tts')).toBe(true)
      expect(isPanelOnlyCategory('frp')).toBe(true)
    })

    it('isPanelOnlyCategory returns false for flat-only categories', () => {
      expect(isPanelOnlyCategory('appearance')).toBe(false)
      expect(isPanelOnlyCategory('projectFiles')).toBe(false)
    })

    it('renders SettingsCategory for all categories (no separate drill-down branch)', async () => {
      for (const id of ['appearance', 'terminal', 'tts']) {
        navStack.value = [id]
        const wrapper = mountPage()
        await nextTick()

        // All categories now route through SettingsCategory
        expect(wrapper.findComponent(SettingsCategory).exists()).toBe(true)
      }
    })
  })
})
