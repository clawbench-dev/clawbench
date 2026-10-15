import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive, nextTick } from 'vue'
import SettingsGroupPanel from '@/components/settings/SettingsGroupPanel.vue'
import type { GroupPanelConfig } from '@/components/settings/settingsFieldMap'

// ── Mock loadThemesModule to control success/failure ──

const mockLoadThemes = vi.hoisted(() => vi.fn())
vi.mock('@/utils/terminalThemes', async () => {
  const actual = await vi.importActual<typeof import('@/utils/terminalThemes')>('@/utils/terminalThemes')
  return {
    ...actual,
    loadThemesModule: mockLoadThemes,
  }
})

// ── Mock composables (copy pattern from SettingsGroupPanel.test.ts) ──

const mockInitSnapshot = vi.fn()
const mockHandleSave = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [] })
const localValues = reactive<Record<string, unknown>>({})
const mockSaving = ref(false)
const mockServerError = ref('')
const mockHotReloadWarning = ref('')
const mockHasFailedSave = ref(false)
const mockHasChanges = ref(false)
const mockCanSave = ref(true)
const mockNeedsRestartHint = ref(false)

vi.mock('@/composables/usePanelSnapshot', () => ({
  usePanelSnapshot: () => ({
    localValues,
    saving: mockSaving,
    serverError: mockServerError,
    hotReloadWarning: mockHotReloadWarning,
    hasFailedSave: mockHasFailedSave,
    hasChanges: mockHasChanges,
    canSave: mockCanSave,
    needsRestartHint: mockNeedsRestartHint,
    initSnapshot: mockInitSnapshot,
    handleSave: mockHandleSave,
  }),
}))

const mockGetServerValueWithDefault = vi.fn()
const settingsLocalConfig = reactive<Record<string, unknown>>({ theme: 'auto', locale: 'zh' })

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValueWithDefault: mockGetServerValueWithDefault,
    localConfig: settingsLocalConfig,
  }),
}))

vi.mock('@/composables/useSettingsNavigation', () => ({
  useSettingsNavigation: () => ({
    registerGuard: vi.fn(),
    unregisterGuard: vi.fn(),
  }),
}))

vi.mock('@/composables/useConnectivityTest', () => ({
  useConnectivityTest: () => ({
    testing: ref(false),
    testResults: ref([]),
    runTests: vi.fn().mockResolvedValue(undefined),
    clearResults: vi.fn(),
  }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    open: vi.fn(),
    effectiveOpen: ref(false),
    close: vi.fn(),
  }),
}))

vi.mock('@/composables/useRagStatus', () => ({
  useRagStatus: () => ({
    status: { value: { available: false, mode: 'none', has_fts_data: false, has_vec_data: false, embedder_healthy: false, total_messages: 0, indexed_messages: 0, embedded_messages: 0 } },
    refresh: vi.fn().mockResolvedValue(undefined),
  }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}))

vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('@/components/settings/settingsFieldMap', async () => {
  const actual = await vi.importActual<typeof import('@/components/settings/settingsFieldMap')>('@/components/settings/settingsFieldMap')
  return { ...actual, engineVoiceOptions: { edge: [], piper: [] } }
})

// Mock lucide icons + BottomSheet teleport
vi.mock('lucide-vue-next', () => ({
  ChevronRight: { name: 'ChevronRight', template: '<span class="icon-chevron-right" />' },
  ListChecks: { name: 'ListChecks', template: '<span class="icon-list-checks" />' },
  Eye: { name: 'Eye', template: '<span class="icon-eye" />' },
  EyeOff: { name: 'EyeOff', template: '<span class="icon-eyeoff" />' },
  RefreshCw: { name: 'RefreshCw', template: '<span class="icon-refresh" />' },
  RotateCcw: { name: 'RotateCcw', template: '<span class="icon-rebuild" />' },
  ChevronsUpDown: { name: 'ChevronsUpDown', template: '<span class="icon-chevron" />' },
  Palette: { name: 'Palette', template: '<span class="icon-palette" />' },
  Sun: { name: 'Sun', template: '<span class="lucide-sun" />' },
  Moon: { name: 'Moon', template: '<span class="lucide-moon" />' },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { ok: '确定', retry: '重试' },
      terminal: { themeFollowApp: '跟随 App 主题' },
      settings: {
        panel: { saved: '已保存', saving: '保存中…', save: '保存', testing: '测试中…', testConnectivity: '测试连通性', needsRestartHint: '' },
        items: {
          terminalEnabled: '启用终端',
          terminalTheme: '配色主题',
          terminalThemeDesc: '终端配色方案',
          terminalThemeLoadFailed: '主题配色加载失败，请重试',
          terminalFontSize: '终端字号',
          terminalIdleTimeout: '空闲超时',
          terminalMaxSessions: '最大会话数',
          terminalBufferLines: '缓冲行数',
        },
      },
    },
  },
})

function makeTerminalConfig(): GroupPanelConfig {
  return {
    panelId: 'terminal',
    enableKey: 'terminal.enabled',
    enableLabelKey: 'settings.items.terminalEnabled',
    commonFields: [
      { labelKey: 'settings.items.terminalTheme', descriptionKey: 'settings.items.terminalThemeDesc', key: 'terminalTheme', type: 'select', source: 'local', defaultValue: 'auto' },
    ],
  }
}

function mountPanel() {
  return mount(SettingsGroupPanel, {
    props: {
      config: makeTerminalConfig(),
      showTitle: false,
    },
    global: {
      stubs: { BottomSheet: { template: '<div class="bs-stub"><slot name="header" /><slot /></div>', props: ['open'] } },
      plugins: [i18n],
    },
  })
}

/** 两个真实主题 + auto，模拟加载成功后的 themes 数据。 */
function makeThemes() {
  return {
    Dracula: { foreground: '#f8f8f2', background: '#1e1f29', cursor: '#bbbbbb', black: '#000000', red: '#ff5555', green: '#50fa7b', yellow: '#f1fa8c', blue: '#bd93f9', magenta: '#ff79c6', cyan: '#8be9fd', white: '#bbbbbb', brightBlack: '#555555', brightRed: '#ff5555', brightGreen: '#50fa7b', brightYellow: '#f1fa8c', brightBlue: '#bd93f9', brightMagenta: '#ff79c6', brightCyan: '#8be9fd', brightWhite: '#ffffff' },
    Atom: { foreground: '#c5c8c6', background: '#1d1f21', cursor: '#c5c8c6', black: '#000000', red: '#cc6666', green: '#b5bd68', yellow: '#f0c674', blue: '#81a2be', magenta: '#b294bb', cyan: '#8abeb7', white: '#c5c8c6', brightBlack: '#666666', brightRed: '#cc6666', brightGreen: '#b5bd68', brightYellow: '#f0c674', brightBlue: '#81a2be', brightMagenta: '#b294bb', brightCyan: '#8abeb7', brightWhite: '#ffffff' },
  }
}

describe('SettingsGroupPanel terminalTheme lazy-load', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localValues['terminal.enabled'] = true
    mockLoadThemes.mockReset()
  })

  async function openThemePicker() {
    const wrapper = mountPanel()
    await nextTick()
    const item = wrapper.findAll('.settings-item').find(i => i.text().includes('配色主题'))
    expect(item, 'terminalTheme SettingsItem should exist').toBeTruthy()
    await item!.trigger('click')
    await nextTick()
    return wrapper
  }

  it('loads terminal themes on open and fills preview colors (no error banner)', async () => {
    mockLoadThemes.mockResolvedValue(makeThemes())
    const wrapper = await openThemePicker()
    await new Promise(r => setTimeout(r, 50))
    await nextTick()

    // No error banner after successful load
    expect(wrapper.find('.theme-picker-error').exists()).toBe(false)

    // Shells get real background colors
    const shells = wrapper.findAll('.tpc-shell')
    expect(shells.length).toBeGreaterThan(0)
    const colored = shells.filter(s => (s.attributes('style') ?? '').includes('background'))
    expect(colored.length).toBeGreaterThan(0)
    // Not all placeholders
    expect(wrapper.findAll('.tpc-shell--placeholder').length).toBeLessThan(shells.length)
  })

  it('auto-retries after a transient failure and fills colors without manual action', async () => {
    vi.useFakeTimers()
    try {
      // First attempt fails, the auto-retry succeeds
      mockLoadThemes.mockRejectedValueOnce(new Error('chunk load failed'))
        .mockResolvedValueOnce(makeThemes())

      const wrapper = await openThemePicker()
      await vi.advanceTimersByTimeAsync(100)
      await nextTick()

      // Error banner may flash during the failed window, but after the
      // auto-retry delay elapses it should have recovered.
      await vi.advanceTimersByTimeAsync(1600)
      await nextTick()

      expect(mockLoadThemes).toHaveBeenCalledTimes(2)
      // Banner gone, colors filled
      expect(wrapper.find('.theme-picker-error').exists()).toBe(false)
      const shells = wrapper.findAll('.tpc-shell')
      expect(shells.length).toBeGreaterThan(0)
      expect(wrapper.findAll('.tpc-shell--placeholder').length).toBeLessThan(shells.length)
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows a retry banner after auto-retries are exhausted, and recovers on manual retry', async () => {
    vi.useFakeTimers()
    try {
      // First attempt + 3 auto-retries all fail, then a manual retry succeeds
      mockLoadThemes.mockRejectedValue(new Error('chunk load failed'))
      const wrapper = await openThemePicker()
      await vi.advanceTimersByTimeAsync(100)
      await nextTick()

      // Banner should NOT be visible while auto-retries remain
      expect(wrapper.find('.theme-picker-error').exists()).toBe(false)

      // Advance through the 3 auto-retry delays (1.5s each)
      await vi.advanceTimersByTimeAsync(1600 * 3)
      await nextTick()

      // Auto-retries exhausted → banner appears
      const banner = wrapper.find('.theme-picker-error')
      expect(banner.exists()).toBe(true)
      expect(banner.text()).toContain('主题配色加载失败')
      expect(mockLoadThemes).toHaveBeenCalledTimes(4)

      // Now allow the next call to succeed and click manual retry
      mockLoadThemes.mockResolvedValue(makeThemes())
      await wrapper.find('.theme-picker-error-retry').trigger('click')
      await vi.advanceTimersByTimeAsync(100)
      await nextTick()

      expect(wrapper.find('.theme-picker-error').exists()).toBe(false)
      const shells = wrapper.findAll('.tpc-shell')
      expect(shells.length).toBeGreaterThan(0)
      expect(wrapper.findAll('.tpc-shell--placeholder').length).toBeLessThan(shells.length)
    } finally {
      vi.useRealTimers()
    }
  })
})
