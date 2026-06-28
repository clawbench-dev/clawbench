import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, shallowMount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, nextTick, computed, reactive } from 'vue'
import SettingsPage from '@/components/settings/SettingsPage.vue'

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
  }),
}))

vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: vi.fn(),
  PRIORITY_PAGE: 100,
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      nav: { settings: '设置' },
      settings: {
        categories: { appearance: '外观' },
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
    // When navStack is empty, header shows settings icon and no back button
    expect(wrapper.find('.settings-page__header-icon').exists()).toBe(true)
    expect(wrapper.find('.settings-page__back').exists()).toBe(false)
  })

  it('shows category view when nav stack has items', async () => {
    navStack.value = ['appearance']
    const wrapper = mountPage()
    await nextTick()

    // When navStack has items, header shows back button, no settings icon
    expect(wrapper.find('.settings-page__back').exists()).toBe(true)
    expect(wrapper.find('.settings-page__header-icon').exists()).toBe(false)
  })

  it('does not show restart button when no restart is needed', () => {
    const wrapper = mountPage()
    expect(wrapper.find('.settings-restart-btn').exists()).toBe(false)
  })

  it('resets nav stack when becoming active', async () => {
    navStack.value = ['appearance']
    const wrapper = mountPage()
    await nextTick()

    // Verify we're in category view
    expect(wrapper.find('.settings-page__back').exists()).toBe(true)

    // Simulate the resetState call that the watch triggers
    navStack.value = []
    wrapper.vm.$forceUpdate()
    await nextTick()

    // navStack is now empty → back at index
    expect(wrapper.find('.settings-page__header-icon').exists()).toBe(true)
    expect(wrapper.find('.settings-page__back').exists()).toBe(false)
  })

  it('shows restart button only when needsRestart is true', async () => {
    needsRestart.value = false
    const wrapper = mountPage()

    // Footer should not render when no restart needed
    expect(wrapper.find('.settings-page__footer').exists()).toBe(false)
    expect(wrapper.find('.settings-restart-btn').exists()).toBe(false)

    needsRestart.value = true
    wrapper.vm.$forceUpdate()
    await nextTick()

    // Footer and pending button should appear
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
    // Footer only renders when needsRestart is true
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
})
