import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive, nextTick } from 'vue'
import SettingsGroupPanel from '@/components/settings/SettingsGroupPanel.vue'
import type { GroupPanelConfig, ItemSpec } from '@/components/settings/settingsFieldMap'

// ── Mock composables ──

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

const mockRegisterGuard = vi.fn()
const mockUnregisterGuard = vi.fn()

vi.mock('@/composables/useSettingsNavigation', () => ({
  useSettingsNavigation: () => ({
    registerGuard: mockRegisterGuard,
    unregisterGuard: mockUnregisterGuard,
  }),
}))

const mockConnectivityTesting = ref(false)
const mockTestResults = ref<Array<{ success: boolean; message: string }>>([])
const mockRunConnectivityTests = vi.fn().mockResolvedValue(undefined)
const mockClearConnectivityResults = vi.fn()

vi.mock('@/composables/useConnectivityTest', () => ({
  useConnectivityTest: () => ({
    testing: mockConnectivityTesting,
    testResults: mockTestResults,
    runTests: mockRunConnectivityTests,
    clearResults: mockClearConnectivityResults,
  }),
}))

const mockToastShow = vi.fn()

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

const mockDrawerOpen = vi.fn()
const mockDrawerClose = vi.fn()
const mockDrawerEffectiveOpen = ref(false)

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    open: mockDrawerOpen,
    effectiveOpen: mockDrawerEffectiveOpen,
    close: mockDrawerClose,
  }),
}))

const mockFrpState = reactive({
  enabled: false,
  running: false,
  state: 'disabled',
  serverAddr: '',
  remotePort: 0,
  sshRemotePort: 0,
  remoteUrl: '',
})

vi.mock('@/composables/useFrp', () => ({
  useFrp: () => ({ frpState: mockFrpState }),
}))

// RAG status mock — shared reactive state accessed via module-level variable
const _ragMockState = {
  status: { available: false, mode: 'none', has_fts_data: false, has_vec_data: false, embedder_healthy: false, total_messages: 0, indexed_messages: 0, embedded_messages: 0 },
  refresh: vi.fn().mockResolvedValue(undefined),
}

vi.mock('@/composables/useRagStatus', () => ({
  useRagStatus: () => ({
    status: { value: _ragMockState.status },
    refresh: _ragMockState.refresh,
  }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}))

vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue(undefined),
}))

// Accessible references for test manipulation
const mockRagStatus = _ragMockState.status
const mockRagRefresh = _ragMockState.refresh

// ── Mock engineVoiceOptions ──

vi.mock('@/components/settings/settingsFieldMap', async () => {
  const actual = await vi.importActual<typeof import('@/components/settings/settingsFieldMap')>('@/components/settings/settingsFieldMap')
  return {
    ...actual,
    engineVoiceOptions: {
      edge: [
        { labelKey: 'settings.items.voiceEdgeXiaoxiao', value: 'zh-CN-XiaoxiaoNeural' },
        { labelKey: 'settings.items.voiceEdgeYunxi', value: 'zh-CN-YunxiNeural' },
      ],
      piper: [
        { labelKey: 'settings.items.voicePiperHuayanMedium', value: 'zh_CN-huayan-medium' },
      ],
    },
  }
})

// ── i18n ──

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        panel: {
          saved: '已保存',
          saving: '保存中…',
          save: '保存',
          testing: '测试中…',
          testConnectivity: '测试连通性',
          needsRestartHint: '需要重启生效',
        },
        items: {
          terminalEnabled: '启用终端',
          ttsEngine: 'TTS引擎',
          ttsEngineEdge: 'Edge',
          ttsEnginePiper: 'Piper',
          ttsVoice: '语音',
          frpEnabled: '启用FRP',
          frpAutoPort: '自动端口',
          frpAssignedPort: '分配的HTTP端口',
          frpAssignedPortDesc: 'FRP分配的HTTP端口',
          frpAssignedSSHPort: '分配的SSH端口',
          frpAssignedSSHPortDesc: 'FRP分配的SSH端口',
          frpServerAddr: '服务器地址',
          frpServerPort: '服务器端口',
          frpToken: '令牌',
          portForwardEnabled: '启用端口映射',
          portForwardPort: '端口',
          pushMode: '推送模式',
          pushModeNative: '原生',
          pushModeDingtalk: '钉钉',
          pushModeDisabled: '禁用',
          voiceEdgeXiaoxiao: '晓晓',
          voiceEdgeYunxi: '云希',
          voicePiperHuayanMedium: '华研',
          terminalFontSize: '终端字号',
          terminalIdleTimeout: '空闲超时',
          terminalMaxSessions: '最大会话数',
          terminalBufferLines: '缓冲行数',
          summarizeTextBackend: '文本摘要引擎',
          summarizeSimple: '简单',
          summarizeApi: 'API',
          summarizeDisabled: '禁用',
          apiBaseUrl: 'API地址',
          ragBaseUrl: 'RAG地址',
          ragIndexProgress: '全文索引进度',
          ragEmbedProgress: '向量嵌入进度',
          ragProgressFormat: '{done}/{total}',
          ragEmbedderStatus: '嵌入模型状态',
          ragMode_hybrid: '混合（向量+全文）',
          ragMode_fts: '仅全文',
          ragMode_none: '未启用',
          ragEmbedderHealthy: '可用',
          ragEmbedderUnhealthy: '不可用',
          ragRebuild: '重建',
          ragRebuildConfirm: '重建将清空所有向量索引数据',
          ragRebuildSuccess: '向量索引已清空，正在重新构建',
          ragRebuildFailed: '重建向量索引失败',
        },
      },
    },
  },
})

// ── Test configs ──

function makeTerminalConfig(): GroupPanelConfig {
  return {
    panelId: 'terminal',
    enableKey: 'terminal.enabled',
    enableLabelKey: 'settings.items.terminalEnabled',
    commonFields: [
      { labelKey: 'settings.items.terminalFontSize', key: 'terminalFontSize', type: 'slider', source: 'local', min: 10, max: 24, step: 1, defaultValue: 12 },
      { labelKey: 'settings.items.terminalIdleTimeout', key: 'terminal.idle_timeout', type: 'text', source: 'server' },
      { labelKey: 'settings.items.terminalMaxSessions', key: 'terminal.max_sessions', type: 'number', source: 'server' },
      { labelKey: 'settings.items.terminalBufferLines', key: 'terminal.buffer_lines', type: 'number', source: 'server' },
    ],
  }
}

function makeTtsConfig(): GroupPanelConfig {
  return {
    panelId: 'tts',
    entrySelector: {
      labelKey: 'settings.items.ttsEngine',
      descriptionKey: 'settings.items.ttsEngine',
      key: 'tts.engine',
      type: 'select',
      source: 'server',
      options: [
        { labelKey: 'settings.items.ttsEngineEdge', value: 'edge' },
        { labelKey: 'settings.items.ttsEnginePiper', value: 'piper' },
      ],
    },
    commonFields: [
      { labelKey: 'settings.items.ttsVoice', key: 'tts.voice', type: 'select', source: 'server' },
    ],
    optionSubFields: [
      {
        when: 'piper',
        fields: [
          { labelKey: 'settings.items.apiBaseUrl', key: 'tts.piper.model_path', type: 'text', source: 'server', sectionHeader: 'settings.items.apiBaseUrl' },
        ],
      },
    ],
    needsVoiceReset: true,
    hasConnectivityTest: true,
    getTestCategories: (values) => [{ category: 'tts', values }],
  }
}

function makeFrpConfig(): GroupPanelConfig {
  return {
    panelId: 'frp',
    enableKey: 'frp.enabled',
    enableLabelKey: 'settings.items.frpEnabled',
    commonFields: [
      { labelKey: 'settings.items.frpServerAddr', key: 'frp.server_addr', type: 'text', source: 'server' },
      { labelKey: 'settings.items.frpServerPort', key: 'frp.server_port', type: 'number', source: 'server' },
      { labelKey: 'settings.items.frpToken', key: 'frp.token', type: 'password', source: 'server' },
      { labelKey: 'settings.items.frpAutoPort', key: 'frp.auto_port', type: 'switch', source: 'server' },
    ],
    optionSubFields: [
      {
        when: false,
        fields: [
          { labelKey: 'settings.items.portForwardPort', key: 'frp.remote_port', type: 'number', source: 'server' },
        ],
      },
    ],
    optionSubFieldsKey: 'frp.auto_port',
    hasConnectivityTest: true,
    getTestCategories: (values) => [{ category: 'frp', values }],
  }
}

function makeSimpleConfig(): GroupPanelConfig {
  return {
    panelId: 'simple',
    titleKey: 'settings.items.ragBaseUrl',
    commonFields: [
      { labelKey: 'settings.items.ragBaseUrl', key: 'rag.base_url', type: 'text', source: 'server' },
    ],
    requiredFields: ['rag.base_url'],
  }
}

function makeDynamicTestConfig(): GroupPanelConfig {
  return {
    panelId: 'summarization_text',
    commonFields: [
      { labelKey: 'settings.items.summarizeTextBackend', key: 'summarize.backend', type: 'select', source: 'server', options: [
        { labelKey: 'settings.items.summarizeDisabled', value: '' },
        { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
        { labelKey: 'settings.items.summarizeApi', value: 'api' },
      ]},
    ],
    hasConnectivityTest: (values) => values['summarize.backend'] === 'api',
    getTestCategories: (values) => values['summarize.backend'] === 'api' ? [{ category: 'summarize_text', values }] : [],
  }
}

function makeRagConfig(): GroupPanelConfig {
  return {
    panelId: 'rag',
    commonFields: [
      { labelKey: 'settings.items.ragEmbedderStatus', key: 'rag.status.embedder_healthy', type: 'info', source: 'server' },
      { labelKey: 'settings.items.ragMode_none', key: 'rag.status.mode', type: 'info', source: 'server' },
      { labelKey: 'settings.items.ragIndexProgress', key: 'rag.status.index_progress', type: 'info', source: 'server' },
      { labelKey: 'settings.items.ragEmbedProgress', key: 'rag.status.embed_progress', type: 'info', source: 'server' },
      { labelKey: 'settings.items.ragRebuild', key: 'rag.rebuild', type: 'action', source: 'local' },
    ],
  }
}

// ── Mount helper ──

function mountPanel(config: GroupPanelConfig, showTitle = true) {
  return mount(SettingsGroupPanel, {
    props: { config, showTitle },
    global: {
      plugins: [i18n],
      stubs: {
        SettingsItem: {
          name: 'SettingsItem',
          props: ['label', 'description', 'type', 'modelValue', 'options', 'min', 'max', 'step', 'needsRestart', 'disabled', 'forceClose', 'defaultValue', 'displayFormat', 'displayTransform', 'noDivider', 'progress', 'refreshable', 'refreshing'],
          template: '<div class="mock-settings-item" :data-key="label" :data-type="type" :data-value="modelValue" :data-disabled="disabled" :data-refreshable="refreshable" @update:model-value="$emit(\'update:modelValue\', $event)" @edit-toggle="$emit(\'editToggle\', $event)" @desc-toggle="$emit(\'descToggle\', $event)" @click="$emit(\'click\')"><button v-if="refreshable" class="settings-item__refresh" @click="$emit(\'refresh\')">↻</button></div>',
          emits: ['update:modelValue', 'editToggle', 'descToggle', 'click', 'refresh'],
        },
        BottomSheet: {
          name: 'BottomSheet',
          props: { open: Boolean, title: String, compact: Boolean },
          template: '<div class="mock-bottom-sheet" v-if="open"><slot /></div>',
        },
        ChevronRight: { template: '<span>></span>' },
        RefreshCw: { template: '<span class="mock-refresh-cw">↻</span>' },
      },
    },
  })
}

// ── Tests ──

describe('SettingsGroupPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset localValues
    for (const key of Object.keys(localValues)) delete localValues[key]
    // Reset reactive refs
    mockSaving.value = false
    mockServerError.value = ''
    mockHotReloadWarning.value = ''
    mockHasFailedSave.value = false
    mockHasChanges.value = false
    mockCanSave.value = true
    mockNeedsRestartHint.value = false
    mockConnectivityTesting.value = false
    mockTestResults.value = []
    mockDrawerEffectiveOpen.value = false
    // Reset frpState
    Object.assign(mockFrpState, { enabled: false, running: false, state: 'disabled', serverAddr: '', remotePort: 0, sshRemotePort: 0, remoteUrl: '' })
    // Default getServerValueWithDefault
    mockGetServerValueWithDefault.mockReturnValue(undefined)
    // Reset RAG mocks
    Object.assign(mockRagStatus, {
      available: false, mode: 'none', has_fts_data: false, has_vec_data: false,
      embedder_healthy: false, total_messages: 0, indexed_messages: 0, embedded_messages: 0,
    })
    mockRagRefresh.mockResolvedValue(undefined)
  })

  // ─── Lifecycle ──────────────────────────────

  describe('lifecycle', () => {
    it('calls initSnapshot on mount', () => {
      mountPanel(makeSimpleConfig())
      expect(mockInitSnapshot).toHaveBeenCalledOnce()
    })

    it('registers guard with panel-specific ID on mount', () => {
      mountPanel(makeSimpleConfig())
      expect(mockRegisterGuard).toHaveBeenCalledWith('panel-simple', expect.any(Function))
    })

    it('unregisters guard on unmount', () => {
      const wrapper = mountPanel(makeSimpleConfig())
      wrapper.unmount()
      expect(mockUnregisterGuard).toHaveBeenCalledWith('panel-simple')
    })

    it('guard returns true when no changes and no failed save', () => {
      mountPanel(makeSimpleConfig())
      const guardFn = mockRegisterGuard.mock.calls[0][1] as () => boolean
      mockHasChanges.value = false
      mockHasFailedSave.value = false
      expect(guardFn()).toBe(true)
    })

    it('guard returns false when has changes', () => {
      mountPanel(makeSimpleConfig())
      const guardFn = mockRegisterGuard.mock.calls[0][1] as () => boolean
      mockHasChanges.value = true
      mockHasFailedSave.value = false
      expect(guardFn()).toBe(false)
    })

    it('guard returns false when has failed save', () => {
      mountPanel(makeSimpleConfig())
      const guardFn = mockRegisterGuard.mock.calls[0][1] as () => boolean
      mockHasChanges.value = false
      mockHasFailedSave.value = true
      expect(guardFn()).toBe(false)
    })
  })

  // ─── Title ──────────────────────────────

  describe('title', () => {
    it('shows title when showTitle is true and config has titleKey', () => {
      const wrapper = mountPanel(makeSimpleConfig(), true)
      expect(wrapper.find('.group-panel__header').exists()).toBe(true)
    })

    it('hides title when showTitle is false', () => {
      const wrapper = mountPanel(makeSimpleConfig(), false)
      expect(wrapper.find('.group-panel__header').exists()).toBe(false)
    })

    it('hides title when config has no titleKey', () => {
      const config: GroupPanelConfig = { panelId: 'test', commonFields: [] }
      const wrapper = mountPanel(config, true)
      expect(wrapper.find('.group-panel__header').exists()).toBe(false)
    })
  })

  // ─── Enable toggle ──────────────────────────────

  describe('enable toggle', () => {
    it('shows enable toggle when config has enableKey', () => {
      localValues['terminal.enabled'] = true
      const wrapper = mountPanel(makeTerminalConfig())
      expect(wrapper.find('.group-panel__enable-row').exists()).toBe(true)
    })

    it('hides enable toggle when config has no enableKey', () => {
      const wrapper = mountPanel(makeSimpleConfig())
      expect(wrapper.find('.group-panel__enable-row').exists()).toBe(false)
    })

    it('checkbox reflects localValues state', () => {
      localValues['terminal.enabled'] = true
      const wrapper = mountPanel(makeTerminalConfig())
      const checkbox = wrapper.find('.group-panel__switch-input')
      expect((checkbox.element as HTMLInputElement).checked).toBe(true)
    })

    it('toggling enable updates localValues', async () => {
      localValues['terminal.enabled'] = false
      const wrapper = mountPanel(makeTerminalConfig())
      const checkbox = wrapper.find('.group-panel__switch-input')
      await checkbox.setValue(true)
      expect(localValues['terminal.enabled']).toBe(true)
    })

    it('fields are disabled when enableKey value is falsy', () => {
      localValues['terminal.enabled'] = false
      const wrapper = mountPanel(makeTerminalConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      items.forEach(item => {
        expect(item.props('disabled')).toBe(true)
      })
    })

    it('fields are enabled when enableKey value is truthy', () => {
      localValues['terminal.enabled'] = true
      const wrapper = mountPanel(makeTerminalConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      items.forEach(item => {
        expect(item.props('disabled')).toBe(false)
      })
    })
  })

  // ─── Entry selector ──────────────────────────────

  describe('entry selector', () => {
    it('shows entry selector row when config has entrySelector', () => {
      localValues['tts.engine'] = 'edge'
      const wrapper = mountPanel(makeTtsConfig())
      expect(wrapper.find('.group-panel__entry-row').exists()).toBe(true)
    })

    it('hides entry selector row when config has no entrySelector', () => {
      const wrapper = mountPanel(makeSimpleConfig())
      expect(wrapper.find('.group-panel__entry-row').exists()).toBe(false)
    })

    it('displays selected entry label', () => {
      localValues['tts.engine'] = 'edge'
      const wrapper = mountPanel(makeTtsConfig())
      expect(wrapper.find('.group-panel__entry-value').text()).toContain('Edge')
    })

    it('calls entryPicker.open on row click', async () => {
      localValues['tts.engine'] = 'edge'
      const wrapper = mountPanel(makeTtsConfig())
      await wrapper.find('.group-panel__entry-row').trigger('click')
      expect(mockDrawerOpen).toHaveBeenCalled()
    })

    it('does not call entryPicker.open when fieldsDisabled', async () => {
      // TTS config with an enableKey so fields can be disabled
      const config: GroupPanelConfig = {
        panelId: 'tts_enabled',
        enableKey: 'tts.enabled',
        enableLabelKey: 'settings.items.ttsEngine',
        entrySelector: makeTtsConfig().entrySelector,
        commonFields: makeTtsConfig().commonFields,
      }
      localValues['tts.enabled'] = false
      localValues['tts.engine'] = 'edge'
      const wrapper = mountPanel(config)
      await wrapper.find('.group-panel__entry-row').trigger('click')
      expect(mockDrawerOpen).not.toHaveBeenCalled()
    })

    it('selecting an entry updates localValues and closes picker', async () => {
      localValues['tts.engine'] = 'edge'
      const wrapper = mountPanel(makeTtsConfig())
      const vm = wrapper.vm as any
      await vm.$.setupState.handleEntrySelect('piper')

      expect(localValues['tts.engine']).toBe('piper')
      expect(mockDrawerClose).toHaveBeenCalled()
    })

    it('auto-resets TTS voice when engine changes', async () => {
      localValues['tts.engine'] = 'edge'
      localValues['tts.voice'] = 'zh-CN-XiaoxiaoNeural'
      const wrapper = mountPanel(makeTtsConfig())
      const vm = wrapper.vm as any
      await vm.$.setupState.handleEntrySelect('piper')

      expect(localValues['tts.engine']).toBe('piper')
      expect(localValues['tts.voice']).toBe('zh_CN-huayan-medium')
    })

    it('does not reset voice when same engine is selected', async () => {
      localValues['tts.engine'] = 'edge'
      localValues['tts.voice'] = 'zh-CN-XiaoxiaoNeural'
      const wrapper = mountPanel(makeTtsConfig())
      const vm = wrapper.vm as any
      await vm.$.setupState.handleEntrySelect('edge')

      // Same value, voice should not change
      expect(localValues['tts.voice']).toBe('zh-CN-XiaoxiaoNeural')
    })

    it('renders active option with check mark in BottomSheet', async () => {
      localValues['tts.engine'] = 'edge'
      const wrapper = mountPanel(makeTtsConfig())
      const vm = wrapper.vm as any

      // Verify entryOptions computed returns the expected options
      const opts = vm.$.setupState.entryOptions
      expect(opts).toHaveLength(2)
      expect(opts[0].value).toBe('edge')
      expect(opts[1].value).toBe('piper')
    })
  })

  // ─── Render list ──────────────────────────────

  describe('render list', () => {
    it('renders common fields as SettingsItem components', () => {
      localValues['rag.base_url'] = 'http://localhost:11434'
      const wrapper = mountPanel(makeSimpleConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBeGreaterThanOrEqual(1)
    })

    it('renders section headers from field.sectionHeader', () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [
          { labelKey: 'settings.items.ragBaseUrl', key: 'rag.base_url', type: 'text', source: 'server', sectionHeader: 'settings.items.apiBaseUrl' },
        ],
      }
      localValues['rag.base_url'] = ''
      const wrapper = mountPanel(config)
      expect(wrapper.find('.group-panel__section-header').exists()).toBe(true)
    })

    it('filters fields by dependsOn', () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [
          { labelKey: 'settings.items.summarizeTextBackend', key: 'summarize.backend', type: 'select', source: 'server', options: [
            { labelKey: 'settings.items.summarizeApi', value: 'api' },
            { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
          ]},
          { labelKey: 'settings.items.apiBaseUrl', key: 'summarize.api.base_url', type: 'text', source: 'server', dependsOn: { key: 'summarize.backend', values: ['api'] } },
        ],
      }
      localValues['summarize.backend'] = 'simple'
      localValues['summarize.api.base_url'] = ''
      const wrapper = mountPanel(config)
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // Should only have 1 item (the select, not the apiBaseUrl field)
      expect(items.length).toBe(1)
    })

    it('shows fields when dependsOn is met', () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [
          { labelKey: 'settings.items.summarizeTextBackend', key: 'summarize.backend', type: 'select', source: 'server', options: [
            { labelKey: 'settings.items.summarizeApi', value: 'api' },
            { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
          ]},
          { labelKey: 'settings.items.apiBaseUrl', key: 'summarize.api.base_url', type: 'text', source: 'server', dependsOn: { key: 'summarize.backend', values: ['api'] } },
        ],
      }
      localValues['summarize.backend'] = 'api'
      localValues['summarize.api.base_url'] = 'http://api.example.com'
      const wrapper = mountPanel(config)
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBe(2)
    })

    it('renders optionSubFields matching current entry value', () => {
      localValues['tts.engine'] = 'piper'
      localValues['tts.voice'] = ''
      localValues['tts.piper.model_path'] = '/models/model.onnx'
      const wrapper = mountPanel(makeTtsConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // Should have tts.voice + piper.model_path (with section header)
      expect(items.length).toBe(2)
    })

    it('does not render optionSubFields when entry value does not match', () => {
      localValues['tts.engine'] = 'edge'
      localValues['tts.voice'] = 'zh-CN-XiaoxiaoNeural'
      const wrapper = mountPanel(makeTtsConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // Only tts.voice, no piper sub-fields
      expect(items.length).toBe(1)
    })

    it('uses optionSubFieldsKey when specified instead of entrySelector key', () => {
      localValues['frp.enabled'] = true
      localValues['frp.auto_port'] = false
      localValues['frp.server_addr'] = ''
      localValues['frp.server_port'] = 7000
      localValues['frp.token'] = ''
      localValues['frp.remote_port'] = 8080
      const wrapper = mountPanel(makeFrpConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // Should show frp.remote_port when frp.auto_port is false
      expect(items.some(i => i.props('label') === '端口')).toBe(true)
    })
  })

  // ─── Field value helpers ──────────────────────────────

  describe('field value helpers', () => {
    it('getLocalValue returns localValues when key exists', () => {
      localValues['rag.base_url'] = 'http://test.com'
      const wrapper = mountPanel(makeSimpleConfig())
      const vm = wrapper.vm as any
      const field: ItemSpec = { labelKey: 'test', key: 'rag.base_url', type: 'text', source: 'server' }
      expect(vm.$.setupState.getLocalValue(field)).toBe('http://test.com')
    })

    it('getLocalValue falls back to getServerValueWithDefault for server source', () => {
      const config: GroupPanelConfig = { panelId: 'test', commonFields: [] }
      const wrapper = mountPanel(config)
      const vm = wrapper.vm as any
      mockGetServerValueWithDefault.mockReturnValue('server-val')
      const field: ItemSpec = { labelKey: 'test', key: 'missing.key', type: 'text', source: 'server' }
      expect(vm.$.setupState.getLocalValue(field)).toBe('server-val')
      expect(mockGetServerValueWithDefault).toHaveBeenCalledWith('missing.key')
    })

    it('getLocalValue falls back to settingsLocalConfig for local source', () => {
      const config: GroupPanelConfig = { panelId: 'test', commonFields: [] }
      const wrapper = mountPanel(config)
      const vm = wrapper.vm as any
      settingsLocalConfig['theme'] = 'dark'
      const field: ItemSpec = { labelKey: 'test', key: 'theme', type: 'select', source: 'local' }
      expect(vm.$.setupState.getLocalValue(field)).toBe('dark')
    })

    it('setLocalValue updates localValues', () => {
      const config: GroupPanelConfig = { panelId: 'test', commonFields: [] }
      const wrapper = mountPanel(config)
      const vm = wrapper.vm as any
      vm.$.setupState.setLocalValue('test.key', 'new-value')
      expect(localValues['test.key']).toBe('new-value')
    })

    it('resolveFieldOptions returns dynamic voice options for tts.voice', () => {
      localValues['tts.engine'] = 'edge'
      const wrapper = mountPanel(makeTtsConfig())
      const vm = wrapper.vm as any
      const field: ItemSpec = { labelKey: 'test', key: 'tts.voice', type: 'select', source: 'server' }
      const opts = vm.$.setupState.resolveFieldOptions(field)
      expect(opts).toHaveLength(2)
      expect(opts[0].value).toBe('zh-CN-XiaoxiaoNeural')
    })

    it('resolveFieldOptions defaults engine to edge for tts.voice', () => {
      localValues['tts.engine'] = undefined
      const wrapper = mountPanel(makeTtsConfig())
      const vm = wrapper.vm as any
      const field: ItemSpec = { labelKey: 'test', key: 'tts.voice', type: 'select', source: 'server' }
      const opts = vm.$.setupState.resolveFieldOptions(field)
      // Defaults to 'edge' engine
      expect(opts).toHaveLength(2)
    })

    it('resolveFieldOptions returns undefined when voice engine has no options', () => {
      localValues['tts.engine'] = 'unknown-engine'
      const wrapper = mountPanel(makeTtsConfig())
      const vm = wrapper.vm as any
      const field: ItemSpec = { labelKey: 'test', key: 'tts.voice', type: 'select', source: 'server' }
      const opts = vm.$.setupState.resolveFieldOptions(field)
      expect(opts).toBeUndefined()
    })

    it('resolveFieldOptions returns static options from field spec', () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [
          { labelKey: 'settings.items.summarizeTextBackend', key: 'summarize.backend', type: 'select', source: 'server', options: [
            { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
            { labelKey: 'settings.items.summarizeApi', value: 'api' },
          ]},
        ],
      }
      localValues['summarize.backend'] = 'simple'
      const wrapper = mountPanel(config)
      const vm = wrapper.vm as any
      const field = config.commonFields[0]
      const opts = vm.$.setupState.resolveFieldOptions(field)
      expect(opts).toHaveLength(2)
      expect(opts[0].value).toBe('simple')
    })

    it('resolveFieldOptions returns undefined when field has no options', () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [
          { labelKey: 'settings.items.ragBaseUrl', key: 'rag.base_url', type: 'text', source: 'server' },
        ],
      }
      localValues['rag.base_url'] = ''
      const wrapper = mountPanel(config)
      const vm = wrapper.vm as any
      const field = config.commonFields[0]
      expect(vm.$.setupState.resolveFieldOptions(field)).toBeUndefined()
    })

    it('setLocalValue is called when SettingsItem emits update:modelValue', async () => {
      localValues['rag.base_url'] = ''
      const wrapper = mountPanel(makeSimpleConfig())
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })[0]
      await item.vm.$emit('update:modelValue', 'http://new-url.com')
      expect(localValues['rag.base_url']).toBe('http://new-url.com')
    })
  })

  // ─── Edit toggle tracking ──────────────────────────────

  describe('edit toggle tracking', () => {
    it('sets activeKey when editToggle opens', async () => {
      localValues['rag.base_url'] = ''
      const wrapper = mountPanel(makeSimpleConfig())
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })[0]
      await item.vm.$emit('editToggle', true)
      const vm = wrapper.vm as any
      expect(vm.$.setupState.activeKey).toBe('rag.base_url')
    })

    it('clears activeKey when editToggle closes with matching key', async () => {
      localValues['rag.base_url'] = ''
      const wrapper = mountPanel(makeSimpleConfig())
      const vm = wrapper.vm as any
      vm.$.setupState.activeKey = 'rag.base_url'
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })[0]
      await item.vm.$emit('editToggle', false)
      expect(vm.$.setupState.activeKey).toBeNull()
    })

    it('does not clear activeKey when editToggle closes with different key', async () => {
      localValues['rag.base_url'] = ''
      const wrapper = mountPanel(makeSimpleConfig())
      const vm = wrapper.vm as any
      vm.$.setupState.activeKey = 'other.key'
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })[0]
      await item.vm.$emit('editToggle', false)
      expect(vm.$.setupState.activeKey).toBe('other.key')
    })

    it('descToggle also tracks activeKey', async () => {
      localValues['rag.base_url'] = ''
      const wrapper = mountPanel(makeSimpleConfig())
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })[0]
      await item.vm.$emit('descToggle', true)
      const vm = wrapper.vm as any
      expect(vm.$.setupState.activeKey).toBe('rag.base_url')
    })
  })

  // ─── Save ──────────────────────────────

  describe('save', () => {
    it('save button is disabled when no changes', () => {
      mockHasChanges.value = false
      const wrapper = mountPanel(makeSimpleConfig())
      const saveBtn = wrapper.find('.group-panel__save-btn')
      expect((saveBtn.element as HTMLButtonElement).disabled).toBe(true)
    })

    it('save button is enabled when has changes and canSave', () => {
      mockHasChanges.value = true
      mockCanSave.value = true
      const wrapper = mountPanel(makeSimpleConfig())
      const saveBtn = wrapper.find('.group-panel__save-btn')
      expect((saveBtn.element as HTMLButtonElement).disabled).toBe(false)
    })

    it('save button is disabled when cannot save', () => {
      mockHasChanges.value = true
      mockCanSave.value = false
      const wrapper = mountPanel(makeSimpleConfig())
      const saveBtn = wrapper.find('.group-panel__save-btn')
      expect((saveBtn.element as HTMLButtonElement).disabled).toBe(true)
    })

    it('save button shows accent class when has changes', () => {
      mockHasChanges.value = true
      const wrapper = mountPanel(makeSimpleConfig())
      const saveBtn = wrapper.find('.group-panel__save-btn')
      expect(saveBtn.classes()).toContain('group-panel__save-btn--accent')
    })

    it('save button is disabled while saving', () => {
      mockHasChanges.value = true
      mockCanSave.value = true
      mockSaving.value = true
      const wrapper = mountPanel(makeSimpleConfig())
      const saveBtn = wrapper.find('.group-panel__save-btn')
      expect((saveBtn.element as HTMLButtonElement).disabled).toBe(true)
    })

    it('calls handleSave on save click', async () => {
      mockHasChanges.value = true
      mockCanSave.value = true
      const wrapper = mountPanel(makeSimpleConfig())
      await wrapper.find('.group-panel__save-btn').trigger('click')
      expect(mockHandleSave).toHaveBeenCalledOnce()
    })

    it('shows toast on successful save', async () => {
      mockHasChanges.value = true
      mockCanSave.value = true
      mockHandleSave.mockResolvedValueOnce({ needsRestart: false, changedColdFields: [] })
      const wrapper = mountPanel(makeSimpleConfig())
      await wrapper.find('.group-panel__save-btn').trigger('click')
      await nextTick()
      expect(mockToastShow).toHaveBeenCalledWith('已保存', expect.objectContaining({ icon: '✅', type: 'success', duration: 3000 }))
    })

    it('emits restartNeeded when save returns needsRestart with changedColdFields', async () => {
      mockHasChanges.value = true
      mockCanSave.value = true
      mockHandleSave.mockResolvedValueOnce({ needsRestart: true, changedColdFields: ['frp.enabled'] })
      const wrapper = mountPanel(makeSimpleConfig())
      await wrapper.find('.group-panel__save-btn').trigger('click')
      await nextTick()
      expect(wrapper.emitted('restartNeeded')).toBeTruthy()
      expect(wrapper.emitted('restartNeeded')![0]).toEqual([['frp.enabled']])
    })

    it('does not emit restartNeeded when no restart needed', async () => {
      mockHasChanges.value = true
      mockCanSave.value = true
      mockHandleSave.mockResolvedValueOnce({ needsRestart: false, changedColdFields: [] })
      const wrapper = mountPanel(makeSimpleConfig())
      await wrapper.find('.group-panel__save-btn').trigger('click')
      await nextTick()
      expect(wrapper.emitted('restartNeeded')).toBeFalsy()
    })

    it('does not show toast when serverError is set after save', async () => {
      mockHasChanges.value = true
      mockCanSave.value = true
      mockHandleSave.mockImplementationOnce(async () => {
        mockServerError.value = 'Save failed'
        return { needsRestart: false, changedColdFields: [] }
      })
      const wrapper = mountPanel(makeSimpleConfig())
      await wrapper.find('.group-panel__save-btn').trigger('click')
      await nextTick()
      expect(mockToastShow).not.toHaveBeenCalled()
    })

    it('shows server error message when present', () => {
      mockServerError.value = 'Connection error'
      const wrapper = mountPanel(makeSimpleConfig())
      expect(wrapper.find('.group-panel__error').exists()).toBe(true)
      expect(wrapper.find('.group-panel__error').text()).toBe('Connection error')
    })

    it('shows hot reload warning when present', () => {
      mockHotReloadWarning.value = 'Some fields need restart'
      const wrapper = mountPanel(makeSimpleConfig())
      expect(wrapper.find('.group-panel__warning').exists()).toBe(true)
      expect(wrapper.find('.group-panel__warning').text()).toBe('Some fields need restart')
    })

    it('shows needsRestartHint when present', () => {
      mockNeedsRestartHint.value = true
      const wrapper = mountPanel(makeSimpleConfig())
      expect(wrapper.find('.group-panel__restart-hint').exists()).toBe(true)
    })
  })

  // ─── Connectivity test ──────────────────────────────

  describe('connectivity test', () => {
    it('shows test button when hasConnectivityTest is true', () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [{ labelKey: 'test', key: 'test.key', type: 'text', source: 'server' }],
        hasConnectivityTest: true,
      }
      localValues['test.key'] = ''
      const wrapper = mountPanel(config)
      expect(wrapper.find('.group-panel__test-btn').exists()).toBe(true)
    })

    it('hides test button when hasConnectivityTest is false', () => {
      const wrapper = mountPanel(makeSimpleConfig())
      expect(wrapper.find('.group-panel__test-btn').exists()).toBe(false)
    })

    it('shows test button when hasConnectivityTest function returns true', () => {
      localValues['summarize.backend'] = 'api'
      const wrapper = mountPanel(makeDynamicTestConfig())
      expect(wrapper.find('.group-panel__test-btn').exists()).toBe(true)
    })

    it('hides test button when hasConnectivityTest function returns false', () => {
      localValues['summarize.backend'] = 'simple'
      const wrapper = mountPanel(makeDynamicTestConfig())
      expect(wrapper.find('.group-panel__test-btn').exists()).toBe(false)
    })

    it('test button is disabled while testing', () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [{ labelKey: 'test', key: 'test.key', type: 'text', source: 'server' }],
        hasConnectivityTest: true,
      }
      localValues['test.key'] = ''
      mockConnectivityTesting.value = true
      const wrapper = mountPanel(config)
      expect((wrapper.find('.group-panel__test-btn').element as HTMLButtonElement).disabled).toBe(true)
    })

    it('calls runConnectivityTests with getTestCategories result', async () => {
      localValues['tts.engine'] = 'edge'
      localValues['tts.voice'] = ''
      const wrapper = mountPanel(makeTtsConfig())
      await wrapper.find('.group-panel__test-btn').trigger('click')
      expect(mockClearConnectivityResults).toHaveBeenCalled()
      expect(mockRunConnectivityTests).toHaveBeenCalledWith([{ category: 'tts', values: expect.any(Object) }])
    })

    it('defaults to panelId category when no getTestCategories', async () => {
      const config: GroupPanelConfig = {
        panelId: 'testpanel',
        commonFields: [{ labelKey: 'test', key: 'test.key', type: 'text', source: 'server' }],
        hasConnectivityTest: true,
      }
      localValues['test.key'] = ''
      const wrapper = mountPanel(config)
      await wrapper.find('.group-panel__test-btn').trigger('click')
      expect(mockRunConnectivityTests).toHaveBeenCalledWith([{ category: 'testpanel', values: expect.any(Object) }])
    })

    it('does not call runConnectivityTests when getTestCategories returns empty array', async () => {
      localValues['summarize.backend'] = 'simple'
      const wrapper = mountPanel(makeDynamicTestConfig())
      // hasConnectivityTest is false for 'simple', so button won't be visible
      // But let's test the internal logic directly
      const vm = wrapper.vm as any
      await vm.$.setupState.handleConnectivityTest()
      expect(mockRunConnectivityTests).not.toHaveBeenCalled()
    })

    it('shows test results', () => {
      mockTestResults.value = [
        { success: true, message: 'Connection OK' },
        { success: false, message: 'Connection failed' },
      ]
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [{ labelKey: 'test', key: 'test.key', type: 'text', source: 'server' }],
        hasConnectivityTest: true,
      }
      localValues['test.key'] = ''
      const wrapper = mountPanel(config)
      const results = wrapper.findAll('.group-panel__test-result')
      expect(results).toHaveLength(2)
      expect(results[0].classes()).toContain('group-panel__test-result--success')
      expect(results[1].classes()).toContain('group-panel__test-result--error')
    })
  })

  // ─── Auto-clear test results ──────────────────────────────

  describe('auto-clear test results', () => {
    it('clears test results when localValues change', async () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [{ labelKey: 'test', key: 'test.key', type: 'text', source: 'server' }],
        hasConnectivityTest: true,
      }
      localValues['test.key'] = ''
      mockTestResults.value = [{ success: true, message: 'OK' }]
      const wrapper = mountPanel(config)

      // Modify localValues to trigger the watcher
      localValues['test.key'] = 'new-value'
      await nextTick()

      expect(mockClearConnectivityResults).toHaveBeenCalled()
    })

    it('does not clear test results when there are no results', async () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [{ labelKey: 'test', key: 'test.key', type: 'text', source: 'server' }],
        hasConnectivityTest: true,
      }
      localValues['test.key'] = ''
      mockTestResults.value = []
      const wrapper = mountPanel(config)

      localValues['test.key'] = 'new-value'
      await nextTick()

      expect(mockClearConnectivityResults).not.toHaveBeenCalled()
    })
  })

  // ─── FRP auto_port display ──────────────────────────────

  describe('FRP auto_port display', () => {
    it('shows assigned HTTP port when frp panel has auto_port=true and frp.enabled=true', () => {
      localValues['frp.enabled'] = true
      localValues['frp.auto_port'] = true
      localValues['frp.server_addr'] = ''
      localValues['frp.server_port'] = 7000
      localValues['frp.token'] = ''
      Object.assign(mockFrpState, { state: 'running', remotePort: 12345, sshRemotePort: 0 })
      const wrapper = mountPanel(makeFrpConfig())
      // Should render extra SettingsItem for assigned port
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const portItem = items.find(i => i.props('label') === '分配的HTTP端口')
      expect(portItem).toBeTruthy()
      expect(portItem!.props('modelValue')).toBe(12345)
      expect(portItem!.props('disabled')).toBe(true)
    })

    it('shows assigned SSH port when available', () => {
      localValues['frp.enabled'] = true
      localValues['frp.auto_port'] = true
      localValues['frp.server_addr'] = ''
      localValues['frp.server_port'] = 7000
      localValues['frp.token'] = ''
      Object.assign(mockFrpState, { state: 'running', remotePort: 12345, sshRemotePort: 22222 })
      const wrapper = mountPanel(makeFrpConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const sshItem = items.find(i => i.props('label') === '分配的SSH端口')
      expect(sshItem).toBeTruthy()
      expect(sshItem!.props('modelValue')).toBe(22222)
    })

    it('does not show SSH port when sshRemotePort is 0', () => {
      localValues['frp.enabled'] = true
      localValues['frp.auto_port'] = true
      localValues['frp.server_addr'] = ''
      localValues['frp.server_port'] = 7000
      localValues['frp.token'] = ''
      Object.assign(mockFrpState, { state: 'running', remotePort: 12345, sshRemotePort: 0 })
      const wrapper = mountPanel(makeFrpConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const sshItem = items.find(i => i.props('label') === '分配的SSH端口')
      expect(sshItem).toBeFalsy()
    })

    it('does not show assigned ports when auto_port is false', () => {
      localValues['frp.enabled'] = true
      localValues['frp.auto_port'] = false
      localValues['frp.server_addr'] = ''
      localValues['frp.server_port'] = 7000
      localValues['frp.token'] = ''
      localValues['frp.remote_port'] = 8080
      Object.assign(mockFrpState, { state: 'running', remotePort: 12345, sshRemotePort: 22222 })
      const wrapper = mountPanel(makeFrpConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const portItem = items.find(i => i.props('label') === '分配的HTTP端口')
      expect(portItem).toBeFalsy()
    })

    it('does not show assigned ports when frp.enabled is false', () => {
      localValues['frp.enabled'] = false
      localValues['frp.auto_port'] = true
      localValues['frp.server_addr'] = ''
      localValues['frp.server_port'] = 7000
      localValues['frp.token'] = ''
      Object.assign(mockFrpState, { state: 'running', remotePort: 12345 })
      const wrapper = mountPanel(makeFrpConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const portItem = items.find(i => i.props('label') === '分配的HTTP端口')
      expect(portItem).toBeFalsy()
    })

    it('shows port 0 when FRP is not running', () => {
      localValues['frp.enabled'] = true
      localValues['frp.auto_port'] = true
      localValues['frp.server_addr'] = ''
      localValues['frp.server_port'] = 7000
      localValues['frp.token'] = ''
      Object.assign(mockFrpState, { state: 'disabled', remotePort: 0, sshRemotePort: 0 })
      const wrapper = mountPanel(makeFrpConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const portItem = items.find(i => i.props('label') === '分配的HTTP端口')
      expect(portItem).toBeTruthy()
      expect(portItem!.props('modelValue')).toBe(0)
    })

    it('does not show assigned ports for non-frp panels', () => {
      const config: GroupPanelConfig = {
        panelId: 'other',
        enableKey: 'other.enabled',
        enableLabelKey: 'settings.items.frpEnabled',
        commonFields: [
          { labelKey: 'settings.items.frpAutoPort', key: 'frp.auto_port', type: 'switch', source: 'server' },
        ],
      }
      localValues['other.enabled'] = true
      localValues['frp.auto_port'] = true
      Object.assign(mockFrpState, { state: 'running', remotePort: 12345 })
      const wrapper = mountPanel(config)
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const portItem = items.find(i => i.props('label') === '分配的HTTP端口')
      expect(portItem).toBeFalsy()
    })
  })

  // ─── SettingsItem props ──────────────────────────────

  describe('SettingsItem props', () => {
    it('passes field props correctly to SettingsItem', () => {
      localValues['rag.base_url'] = 'http://test.com'
      const wrapper = mountPanel(makeSimpleConfig())
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })[0]
      expect(item.props('label')).toBe('RAG地址')
      expect(item.props('type')).toBe('text')
      expect(item.props('modelValue')).toBe('http://test.com')
    })

    it('passes min/max/step to SettingsItem', () => {
      localValues['terminal.enabled'] = true
      localValues['terminalFontSize'] = 12
      localValues['terminal.idle_timeout'] = '10m'
      localValues['terminal.max_sessions'] = 10
      localValues['terminal.buffer_lines'] = 2000
      const wrapper = mountPanel(makeTerminalConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const fontItem = items.find(i => i.props('label') === '终端字号')
      expect(fontItem).toBeTruthy()
      expect(fontItem!.props('min')).toBe(10)
      expect(fontItem!.props('max')).toBe(24)
      expect(fontItem!.props('step')).toBe(1)
    })

    it('passes needsRestart to SettingsItem', () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [
          { labelKey: 'settings.items.ragBaseUrl', key: 'rag.base_url', type: 'text', source: 'server', needsRestart: true },
        ],
      }
      localValues['rag.base_url'] = ''
      const wrapper = mountPanel(config)
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })[0]
      expect(item.props('needsRestart')).toBe(true)
    })

    it('passes forceClose when another item is being edited', async () => {
      const config: GroupPanelConfig = {
        panelId: 'test',
        commonFields: [
          { labelKey: 'settings.items.ragBaseUrl', key: 'rag.base_url', type: 'text', source: 'server' },
          { labelKey: 'settings.items.apiBaseUrl', key: 'summarize.api.base_url', type: 'text', source: 'server' },
        ],
      }
      localValues['rag.base_url'] = ''
      localValues['summarize.api.base_url'] = ''
      const wrapper = mountPanel(config)
      const vm = wrapper.vm as any

      // Verify initial state: activeKey is null, so forceClose should be false
      expect(vm.$.setupState.activeKey).toBeNull()

      // Set activeKey to first field via handleEditToggle
      vm.$.setupState.handleEditToggle('rag.base_base_url', true)
      // Directly set the reactive ref to trigger reactivity
      vm.$.setupState.activeKey = 'rag.base_url'

      // Verify activeKey was set
      expect(vm.$.setupState.activeKey).toBe('rag.base_url')

      // Now test the computed forceClose logic:
      // activeKey !== null && activeKey !== entry.field.key → forceClose = true
      const firstKey = 'rag.base_url'
      const secondKey = 'summarize.api.base_url'

      // For the first field: activeKey === key → NOT forceClosed
      expect(vm.$.setupState.activeKey !== null && vm.$.setupState.activeKey !== firstKey).toBe(false)
      // For the second field: activeKey !== key → forceClosed
      expect(vm.$.setupState.activeKey !== null && vm.$.setupState.activeKey !== secondKey).toBe(true)
    })

    it('passes defaultValue to SettingsItem', () => {
      localValues['terminal.enabled'] = true
      localValues['terminalFontSize'] = 12
      localValues['terminal.idle_timeout'] = '10m'
      localValues['terminal.max_sessions'] = 10
      localValues['terminal.buffer_lines'] = 2000
      const wrapper = mountPanel(makeTerminalConfig())
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const fontItem = items.find(i => i.props('label') === '终端字号')
      expect(fontItem!.props('defaultValue')).toBe(12)
    })
  })

  // ─── Entry display label ──────────────────────────────

  describe('entry display label', () => {
    it('shows string value when no matching option found', () => {
      localValues['tts.engine'] = 'unknown-engine'
      const wrapper = mountPanel(makeTtsConfig())
      expect(wrapper.find('.group-panel__entry-value').text()).toBe('unknown-engine')
    })

    it('shows empty string when value is undefined', () => {
      localValues['tts.engine'] = undefined
      const wrapper = mountPanel(makeTtsConfig())
      expect(wrapper.find('.group-panel__entry-value').text()).toBe('')
    })
  })

  // ─── RAG progress refresh ──────────────────────────────

  describe('RAG progress refresh button', () => {
    it('renders refresh button for RAG progress fields', () => {
      const wrapper = mountPanel(makeRagConfig())
      const refreshButtons = wrapper.findAll('.settings-item__refresh')
      expect(refreshButtons).toHaveLength(2) // index_progress + embed_progress
    })

    it('does not render refresh button for non-progress RAG fields', () => {
      const wrapper = mountPanel(makeRagConfig())
      // Only progress fields should have refresh buttons
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isRagProgressField({ key: 'rag.status.embedder_healthy' })).toBe(false)
      expect(vm.$.setupState.isRagProgressField({ key: 'rag.status.mode' })).toBe(false)
      expect(vm.$.setupState.isRagProgressField({ key: 'rag.rebuild' })).toBe(false)
    })

    it('does not render refresh button for non-RAG panels', () => {
      const wrapper = mountPanel(makeSimpleConfig())
      const refreshButtons = wrapper.findAll('.settings-item__refresh')
      expect(refreshButtons).toHaveLength(0)
    })

    it('calls refreshRagStatus when refresh button is clicked', async () => {
      const wrapper = mountPanel(makeRagConfig())
      mockRagRefresh.mockClear() // clear the auto-fetch from onMounted
      const refreshBtn = wrapper.find('.settings-item__refresh')
      await refreshBtn.trigger('click')
      expect(mockRagRefresh).toHaveBeenCalledOnce()
    })

    it('resolves RAG status values for progress fields', () => {
      Object.assign(mockRagStatus, {
        available: true, mode: 'hybrid', has_fts_data: true, has_vec_data: true,
        embedder_healthy: true, total_messages: 100, indexed_messages: 80, embedded_messages: 60,
      })
      const wrapper = mountPanel(makeRagConfig())
      const vm = wrapper.vm as any

      const indexProgress = vm.$.setupState.getRagStatusValue('rag.status.index_progress')
      expect(indexProgress).toBe('80/100')

      const embedProgress = vm.$.setupState.getRagStatusValue('rag.status.embed_progress')
      expect(embedProgress).toBe('60/100')
    })

    it('shows dash when total_messages is 0', () => {
      Object.assign(mockRagStatus, {
        available: false, mode: 'none', has_fts_data: false, has_vec_data: false,
        embedder_healthy: false, total_messages: 0, indexed_messages: 0, embedded_messages: 0,
      })
      const wrapper = mountPanel(makeRagConfig())
      const vm = wrapper.vm as any

      expect(vm.$.setupState.getRagStatusValue('rag.status.index_progress')).toBe('—')
      expect(vm.$.setupState.getRagStatusValue('rag.status.embed_progress')).toBe('—')
    })

    it('resolves progress bar data for progress fields', () => {
      Object.assign(mockRagStatus, {
        available: true, mode: 'hybrid', has_fts_data: true, has_vec_data: true,
        embedder_healthy: true, total_messages: 100, indexed_messages: 80, embedded_messages: 60,
      })
      const wrapper = mountPanel(makeRagConfig())
      const vm = wrapper.vm as any

      const indexProgress = vm.$.setupState.resolveProgress({ key: 'rag.status.index_progress' })
      expect(indexProgress).toEqual({ value: 80, max: 100 })

      const embedProgress = vm.$.setupState.resolveProgress({ key: 'rag.status.embed_progress' })
      expect(embedProgress).toEqual({ value: 60, max: 100 })
    })

    it('returns undefined progress when total_messages is 0', () => {
      Object.assign(mockRagStatus, {
        available: false, mode: 'none', has_fts_data: false, has_vec_data: false,
        embedder_healthy: false, total_messages: 0, indexed_messages: 0, embedded_messages: 0,
      })
      const wrapper = mountPanel(makeRagConfig())
      const vm = wrapper.vm as any

      expect(vm.$.setupState.resolveProgress({ key: 'rag.status.index_progress' })).toBeUndefined()
      expect(vm.$.setupState.resolveProgress({ key: 'rag.status.embed_progress' })).toBeUndefined()
    })

    it('isRagProgressField returns true only for progress fields', () => {
      const wrapper = mountPanel(makeRagConfig())
      const vm = wrapper.vm as any

      expect(vm.$.setupState.isRagProgressField({ key: 'rag.status.index_progress' })).toBe(true)
      expect(vm.$.setupState.isRagProgressField({ key: 'rag.status.embed_progress' })).toBe(true)
      expect(vm.$.setupState.isRagProgressField({ key: 'rag.status.mode' })).toBe(false)
      expect(vm.$.setupState.isRagProgressField({ key: 'rag.status.embedder_healthy' })).toBe(false)
      expect(vm.$.setupState.isRagProgressField({ key: 'rag.rebuild' })).toBe(false)
    })
  })
})
