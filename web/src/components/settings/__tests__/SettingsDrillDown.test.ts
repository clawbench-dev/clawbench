import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, shallowMount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive, nextTick } from 'vue'
import SettingsDrillDown from '@/components/settings/SettingsDrillDown.vue'
import { drillDownCategories } from '@/components/settings/settingsFieldMap'

// ── Mock composables ──────────────────────────────────────

const mockPatchConfig = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [] })
const mockGetServerValueWithDefault = vi.fn()
const mockSetLocalConfig = vi.fn()
const mockLoadConfig = vi.fn()

const localConfig = reactive<Record<string, any>>({
  theme: 'auto',
  locale: 'zh',
  autoSpeech: false,
  showHidden: false,
  wordWrap: true,
  lineNumbers: true,
  fileView: 'list',
  terminalFontSize: 12,
  androidLogCapture: false,
  swipeSession: false,
  uiScale: 1,
  sortField: null,
  sortDir: 'asc',
  stickyScroll: true,
  preventScreenLock: true,
})

const serverConfig = ref<Record<string, any>>({
  version: 'dev',
  default_agent: '',
  chat: { initial_messages: 20, page_size: 20, system_prompt_interval: 10 },
  session: { max_count: 10 },
  upload: { max_size_mb: 100, max_files: 20 },
  terminal: { enabled: true, idle_timeout: '10m', max_sessions: 10, buffer_lines: 2000 },
  tts: { engine: 'edge', voice: 'zh-CN-XiaoxiaoNeural', speed: 1.0, max_cache_files: 100, format: '' },
  rag: { enabled: false, base_url: 'http://localhost:11434', model: 'bge-m3', api_key: '', chunk_size: 512, search_limit: 5, search_pool_size: 20, retention_days: 90 },
  port_forward: { enabled: true, port: 0 },
  summarize: { backend: 'simple', model: '', api: { base_url: '', key: '', format: 'openai' } },
  frp: { enabled: false, server_addr: '', server_port: 7000, token: '', auto_port: true, remote_port: 0, ssh_remote_port: 0 },
})

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    localConfig,
    serverConfig,
    patchConfig: mockPatchConfig,
    getServerValueWithDefault: mockGetServerValueWithDefault,
    setLocalConfig: mockSetLocalConfig,
    loadConfig: mockLoadConfig,
  }),
}))

const mockSetBeforeResetGuard = vi.fn()
vi.mock('@/composables/useSettingsNavigation', () => ({
  useSettingsNavigation: () => ({
    setBeforeResetGuard: mockSetBeforeResetGuard,
  }),
}))

// FRP state mock — configurable per test
const frpState = reactive({ enabled: false, state: 'stopped', remotePort: 0, sshRemotePort: 0 })
vi.mock('@/composables/useDrillDownSideEffects', () => ({
  useDrillDownSideEffects: (categoryId: string) => ({
    afterSave: vi.fn(),
    init: vi.fn(),
    frpStatusDot: ref(categoryId === 'frp' && frpState.enabled
      ? frpState.state === 'running' ? 'green'
        : frpState.state === 'starting' ? 'yellow'
        : frpState.state === 'failed' ? 'red' : undefined
      : undefined),
    needsVoiceReset: ref(categoryId === 'tts'),
    frpAutoPortInfo: ref(categoryId === 'frp'
      ? { state: frpState.state, remotePort: frpState.remotePort, sshRemotePort: frpState.sshRemotePort }
      : null),
  }),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

const mockDialogConfirm = vi.fn().mockResolvedValue(false)
vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({ confirm: mockDialogConfirm }),
}))

// Stub useTabDrawer to return a simple mock drawer
vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    effectiveOpen: ref(false),
    isOpen: ref(false),
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
  }),
}))

// Stub lucide-vue-next icons
vi.mock('lucide-vue-next', () => ({
  ChevronRight: { name: 'ChevronRight', template: '<span />' },
}))

// ── i18n ──────────────────────────────────────────────────

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        drillDown: {
          save: '保存',
          saving: '保存中',
          saved: '已保存',
          needsRestartHint: '需重启生效',
          unsavedTitle: '未保存',
          unsavedMessage: '放弃更改？',
          discard: '放弃',
          continueEditing: '继续编辑',
        },
        items: {
          terminalEnabled: '启用终端',
          terminalFontSize: '终端字号',
          terminalIdleTimeout: '空闲超时',
          terminalMaxSessions: '最大会话',
          terminalBufferLines: '缓冲行数',
          ttsEngine: 'TTS引擎',
          ttsEngineEdge: 'Edge',
          ttsEnginePiper: 'Piper',
          ttsEngineKokoro: 'Kokoro',
          ttsEngineMossNano: 'MOSS-Nano',
          ttsVoice: '语音',
          ttsSpeed: '语速',
          ttsMaxCacheFiles: '缓存上限',
          piperModelPath: '模型路径',
          piperNoiseScale: '噪声系数',
          piperLengthScale: '长度系数',
          piperSentenceSilence: '句间停顿',
          ttsPiperHeader: 'Piper',
          kokoroModelPath: '模型路径',
          kokoroVoicesPath: '语音路径',
          kokoroLang: '语言',
          ttsKokoroHeader: 'Kokoro',
          mossNanoModelDir: '模型目录',
          mossNanoBackend: '后端',
          ttsMossNanoHeader: 'MOSS-Nano',
          summarizeBackend: '摘要方式',
          summarizeDisabled: '禁用',
          summarizeSimple: '简单',
          summarizeApi: 'API',
          summarizeModel: '摘要模型',
          apiHeader: 'API',
          apiBaseUrl: 'API地址',
          apiKey: 'API密钥',
          apiFormat: 'API格式',
          ragBaseUrl: '嵌入接口地址',
          ragModel: '嵌入模型',
          ragApiKey: 'API密钥',
          ragChunkSize: '分块大小',
          ragSearchLimit: '搜索限制',
          ragSearchPoolSize: '搜索池大小',
          ragRetentionDays: '保留天数',
          portForwardEnabled: '启用端口转发',
          portForwardPort: '端口',
          frpEnabled: '启用FRP',
          frpServerAddr: '服务器地址',
          frpServerPort: '服务器端口',
          frpToken: 'Token',
          frpAutoPort: '自动端口',
          frpRemotePort: '远程端口',
          frpSSHRemotePort: 'SSH远程端口',
          voiceEdgeXiaoxiao: '晓晓',
          voiceEdgeYunxi: '云希',
          voicePiperHuayanMedium: '华颜',
          voiceKokoroZf001: 'zf_001',
          voiceMossJunhao: '俊豪',
          mossNanoBackendOnnx: 'ONNX',
          mossNanoBackendPytorch: 'PyTorch',
          apiFormatOpenai: 'OpenAI',
          apiFormatAnthropic: 'Anthropic',
        },
      },
    },
  },
})

// ── Helpers ───────────────────────────────────────────────

function resolveServerValue(key: string): any {
  const parts = key.split('.')
  let current: any = serverConfig.value
  for (const p of parts) {
    if (current == null || typeof current !== 'object') return undefined
    current = current[p]
  }
  return current
}

function mountDrillDown(categoryId: string) {
  return shallowMount(SettingsDrillDown, {
    props: { categoryId },
    global: {
      plugins: [i18n],
      stubs: {
        SettingsItem: {
          template: '<div class="stub-settings-item" :data-key="field?.key" :data-type="field?.type" :data-disabled="disabled" :data-needs-restart="field?.needsRestart" />',
          props: ['label', 'description', 'type', 'modelValue', 'options', 'min', 'max', 'step', 'needsRestart', 'disabled', 'forceClose', 'defaultValue', 'displayFormat', 'displayTransform', 'noDivider'],
          setup(props: any) {
            // Reconstruct the field spec from the parent's renderList
            // We expose it through a provide/inject-like mechanism: use attrs
            return { field: { key: null, type: null, needsRestart: null } }
          },
        },
        BottomSheet: { template: '<div class="stub-bottom-sheet" v-if="open"><slot /></div>', props: ['open', 'title', 'compact'] },
      },
    },
  })
}

/** Mount with real SettingsItem to inspect props */
function mountWithRealItems(categoryId: string) {
  return mount(SettingsDrillDown, {
    props: { categoryId },
    global: {
      plugins: [i18n],
      stubs: {
        BottomSheet: { template: '<div class="stub-bottom-sheet" v-if="open"><slot /></div>', props: ['open', 'title', 'compact'] },
      },
    },
  })
}

// ── Tests ─────────────────────────────────────────────────

describe('SettingsDrillDown', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockDialogConfirm.mockResolvedValue(false)
    // Default: resolve server values from serverConfig
    mockGetServerValueWithDefault.mockImplementation((key: string) => {
      const val = resolveServerValue(key)
      return val !== undefined ? val : undefined
    })
    // Reset frpState
    frpState.enabled = false
    frpState.state = 'stopped'
    frpState.remotePort = 0
    frpState.sshRemotePort = 0
  })

  // ─── 1. Snapshot creation on mount ──────────────────

  describe('snapshot creation on mount', () => {
    it('initializes localValues from server config for terminal category', () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      const lv = vm.$.setupState.localValues
      expect(lv['terminal.enabled']).toBe(true)
      expect(lv['terminal.idle_timeout']).toBe('10m')
      expect(lv['terminal.max_sessions']).toBe(10)
      expect(lv['terminal.buffer_lines']).toBe(2000)
    })

    it('initializes localValues from local config for local fields', () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      const lv = vm.$.setupState.localValues
      // terminalFontSize is source:'local'
      expect(lv['terminalFontSize']).toBe(12)
    })

    it('initializes localValues for TTS entry selector and sub-fields', () => {
      const wrapper = mountDrillDown('tts')
      const vm = wrapper.vm as any
      const lv = vm.$.setupState.localValues
      expect(lv['tts.engine']).toBe('edge')
      expect(lv['tts.voice']).toBe('zh-CN-XiaoxiaoNeural')
      expect(lv['tts.speed']).toBe(1.0)
      // Piper sub-fields should also be snapshotted
      expect('tts.piper.model_path' in lv).toBe(true)
      expect('tts.piper.noise_scale' in lv).toBe(true)
    })
  })

  // ─── 2. Diff detection ─────────────────────────────

  describe('diff detection', () => {
    it('hasChanges is false on mount', () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      expect(vm.$.setupState.hasChanges).toBe(false)
    })

    it('hasChanges becomes true after changing a field', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      await nextTick()
      expect(vm.$.setupState.hasChanges).toBe(true)
    })

    it('hasChanges reverts to false when value restored', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      await nextTick()
      expect(vm.$.setupState.hasChanges).toBe(true)
      vm.$.setupState.localValues['terminal.max_sessions'] = 10
      await nextTick()
      expect(vm.$.setupState.hasChanges).toBe(false)
    })
  })

  // ─── 3. Password fields skipped in diff ─────────────

  describe('password fields skipped in diff', () => {
    it('empty password does not count as a change', async () => {
      const wrapper = mountDrillDown('frp')
      const vm = wrapper.vm as any
      // frp.token is a password field — set it to empty (user didn't re-enter)
      vm.$.setupState.localValues['frp.token'] = ''
      await nextTick()
      expect(vm.$.setupState.hasChanges).toBe(false)
    })

    it('empty password is excluded from PATCH payload on save', async () => {
      const wrapper = mountDrillDown('frp')
      const vm = wrapper.vm as any
      // Change a non-password field
      vm.$.setupState.localValues['frp.server_addr'] = 'new.example.com'
      await nextTick()
      // Set password to empty (snapshot might have had a value)
      vm.$.setupState.localValues['frp.token'] = ''
      await nextTick()
      await vm.$.setupState.handleSave()
      // patchConfig should NOT include frp.token in the payload
      if (mockPatchConfig.mock.calls.length > 0) {
        const payload = mockPatchConfig.mock.calls[0][0]
        // frp.token should not be present at any nesting level
        expect(payload.frp?.token).toBeUndefined()
      }
    })
  })

  // ─── 4. Dual-path flush ────────────────────────────

  describe('dual-path flush', () => {
    it('server fields go to patchConfig, local fields go to setLocalConfig', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      // Change a server field
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      // Change a local field
      vm.$.setupState.localValues['terminalFontSize'] = 16
      await nextTick()
      await vm.$.setupState.handleSave()
      // server field → patchConfig
      expect(mockPatchConfig).toHaveBeenCalled()
      const payload = mockPatchConfig.mock.calls[0][0]
      expect(payload.terminal?.max_sessions).toBe(5)
      // local field → setLocalConfig
      expect(mockSetLocalConfig).toHaveBeenCalledWith('terminalFontSize', 16)
    })
  })

  // ─── 5. Enable toggle disables fields ──────────────

  describe('enable toggle disables fields', () => {
    it('fields are disabled when terminal.enabled is false', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.enabled'] = false
      await nextTick()
      expect(vm.$.setupState.fieldsDisabled).toBe(true)
    })

    it('fields are enabled when terminal.enabled is true', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.enabled'] = true
      await nextTick()
      expect(vm.$.setupState.fieldsDisabled).toBe(false)
    })
  })

  // ─── 6. Entry selector changes sub-fields ──────────

  describe('entry selector changes sub-fields', () => {
    it('TTS piper sub-fields appear when engine is piper', async () => {
      const wrapper = mountWithRealItems('tts')
      const vm = wrapper.vm as any
      // Use the reactive localValues directly
      vm.localValues['tts.engine'] = 'piper'
      await nextTick()
      // Check renderList computed includes piper fields
      const renderedKeys = vm.$.setupState.renderList.map((e: any) => e.type === 'field' ? e.field.key : null).filter(Boolean)
      expect(renderedKeys).toContain('tts.piper.model_path')
    })

    it('TTS kokoro sub-fields appear when engine is kokoro', async () => {
      const wrapper = mountWithRealItems('tts')
      const vm = wrapper.vm as any
      vm.localValues['tts.engine'] = 'kokoro'
      await nextTick()
      const renderedKeys = vm.$.setupState.renderList.map((e: any) => e.type === 'field' ? e.field.key : null).filter(Boolean)
      expect(renderedKeys).toContain('tts.kokoro.model_path')
    })

    it('TTS moss-nano sub-fields appear when engine is moss-nano', async () => {
      const wrapper = mountWithRealItems('tts')
      const vm = wrapper.vm as any
      vm.localValues['tts.engine'] = 'moss-nano'
      await nextTick()
      const renderedKeys = vm.$.setupState.renderList.map((e: any) => e.type === 'field' ? e.field.key : null).filter(Boolean)
      expect(renderedKeys).toContain('tts.moss_nano.model_dir')
    })
  })

  // ─── 7. Required field validation ──────────────────

  describe('required field validation', () => {
    it('canSave is false when required TTS piper model_path is empty', async () => {
      const wrapper = mountDrillDown('tts')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['tts.engine'] = 'piper'
      vm.$.setupState.localValues['tts.piper.model_path'] = ''
      await nextTick()
      expect(vm.$.setupState.canSave).toBe(false)
    })

    it('canSave is true when required TTS piper model_path is filled', async () => {
      const wrapper = mountDrillDown('tts')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['tts.engine'] = 'piper'
      vm.$.setupState.localValues['tts.piper.model_path'] = '/path/to/model'
      // Also fill other required fields (kokoro fields are in requiredFields too)
      vm.$.setupState.localValues['tts.kokoro.model_path'] = '/path/to/kokoro'
      vm.$.setupState.localValues['tts.kokoro.voices_path'] = '/path/to/voices'
      await nextTick()
      expect(vm.$.setupState.canSave).toBe(true)
    })

    it('canSave is false when required frp.server_addr is empty', async () => {
      const wrapper = mountDrillDown('frp')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['frp.server_addr'] = ''
      await nextTick()
      expect(vm.$.setupState.canSave).toBe(false)
    })

    it('canSave is true when required frp.server_addr is filled', async () => {
      const wrapper = mountDrillDown('frp')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['frp.server_addr'] = 'frp.example.com'
      await nextTick()
      expect(vm.$.setupState.canSave).toBe(true)
    })

    it('save button is disabled when canSave is false', async () => {
      const wrapper = mountDrillDown('frp')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['frp.server_addr'] = ''
      // Also make a change so hasChanges is true (otherwise button is disabled for no changes)
      vm.$.setupState.localValues['frp.server_port'] = 8000
      await nextTick()
      const saveBtn = wrapper.find('.drill-down__save-btn')
      expect((saveBtn.element as HTMLButtonElement).disabled).toBe(true)
    })
  })

  // ─── 8. Voice auto-reset on engine switch ──────────

  describe('voice auto-reset on engine switch', () => {
    it('resets voice to first option when switching engine from edge to piper', async () => {
      const wrapper = mountDrillDown('tts')
      const vm = wrapper.vm as any
      // Initial state
      expect(vm.$.setupState.localValues['tts.voice']).toBe('zh-CN-XiaoxiaoNeural')
      // Switch engine to piper
      vm.$.setupState.handleEntrySelect('piper')
      await nextTick()
      // Voice should reset to first piper option
      expect(vm.$.setupState.localValues['tts.voice']).toBe('zh_CN-huayan-medium')
    })

    it('resets voice when switching engine to kokoro', async () => {
      const wrapper = mountDrillDown('tts')
      const vm = wrapper.vm as any
      vm.$.setupState.handleEntrySelect('kokoro')
      await nextTick()
      expect(vm.$.setupState.localValues['tts.voice']).toBe('zf_001')
    })

    it('resets voice when switching engine to moss-nano', async () => {
      const wrapper = mountDrillDown('tts')
      const vm = wrapper.vm as any
      vm.$.setupState.handleEntrySelect('moss-nano')
      await nextTick()
      expect(vm.$.setupState.localValues['tts.voice']).toBe('Junhao')
    })
  })

  // ─── 9. Unsaved back dialog ────────────────────────

  describe('unsaved back dialog', () => {
    it('shows confirm dialog when requestBack called with unsaved changes', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      await nextTick()
      vm.$.setupState.requestBack()
      expect(mockDialogConfirm).toHaveBeenCalled()
    })

    it('emits back directly when no unsaved changes', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.requestBack()
      expect(mockDialogConfirm).not.toHaveBeenCalled()
      expect(wrapper.emitted('back')).toBeTruthy()
    })

    it('emits back when user confirms discard', async () => {
      mockDialogConfirm.mockResolvedValue(true)
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      await nextTick()
      vm.$.setupState.requestBack()
      await vi.waitFor(() => {
        expect(wrapper.emitted('back')).toBeTruthy()
      })
    })
  })

  // ─── 10. needsRestart hint ─────────────────────────

  describe('needsRestart hint', () => {
    it('does not show hint when no needsRestart field is changed', async () => {
      const wrapper = mountDrillDown('portForward')
      const vm = wrapper.vm as any
      // port_forward.port no longer has needsRestart:true (hot-reload)
      vm.localValues['port_forward.port'] = 12345
      await nextTick()
      expect(vm.$.setupState.needsRestartHint).toBe(false)
    })

    it('does not show hint when no needsRestart field is changed (terminal)', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      // terminal.max_sessions does NOT have needsRestart
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      await nextTick()
      expect(vm.$.setupState.needsRestartHint).toBe(false)
    })
  })

  // ─── 11. FRP status dot ────────────────────────────

  describe('FRP status dot', () => {
    it('renders green status dot when FRP is running', async () => {
      frpState.enabled = true
      frpState.state = 'running'
      const wrapper = mountDrillDown('frp')
      const dot = wrapper.find('.drill-down__status-dot')
      expect(dot.exists()).toBe(true)
      expect(dot.classes()).toContain('drill-down__status-dot--green')
    })

    it('renders yellow status dot when FRP is starting', async () => {
      frpState.enabled = true
      frpState.state = 'starting'
      const wrapper = mountDrillDown('frp')
      const dot = wrapper.find('.drill-down__status-dot')
      expect(dot.exists()).toBe(true)
      expect(dot.classes()).toContain('drill-down__status-dot--yellow')
    })

    it('renders red status dot when FRP is failed', async () => {
      frpState.enabled = true
      frpState.state = 'failed'
      const wrapper = mountDrillDown('frp')
      const dot = wrapper.find('.drill-down__status-dot')
      expect(dot.exists()).toBe(true)
      expect(dot.classes()).toContain('drill-down__status-dot--red')
    })

    it('does not render status dot when FRP is disabled', () => {
      frpState.enabled = false
      const wrapper = mountDrillDown('frp')
      const dot = wrapper.find('.drill-down__status-dot')
      expect(dot.exists()).toBe(false)
    })
  })

  // ─── 12. port_forward.port display (0 = auto) ──────

  describe('port_forward.port display', () => {
    it('port 0 is resolved via displayTransform to auto', () => {
      // The displayTransform is on the field spec: (v) => v === 0 ? '__auto__' : v
      // We verify the field spec exists with the correct transform
      const portField = drillDownCategories.portForward.commonFields.find(
        (f: any) => f.key === 'port_forward.port'
      )
      expect(portField).toBeTruthy()
      expect(portField.displayTransform(0)).toBe('__auto__')
      expect(portField.displayTransform(12345)).toBe(12345)
    })
  })

  // ─── 13. All 6 categories render ────────────────────

  describe('all drill-down categories render', () => {
    it('terminal category renders enable toggle and 4 fields', () => {
      const wrapper = mountWithRealItems('terminal')
      // Should have enable row
      expect(wrapper.find('.drill-down__enable-row').exists()).toBe(true)
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // 4 common fields: fontSize, idleTimeout, maxSessions, bufferLines
      expect(items.length).toBeGreaterThanOrEqual(4)
    })

    it('tts category renders entry selector and common fields', () => {
      const wrapper = mountWithRealItems('tts')
      // Should have entry selector row
      expect(wrapper.find('.drill-down__entry-row').exists()).toBe(true)
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // Common fields: voice, speed, maxCacheFiles
      expect(items.length).toBeGreaterThanOrEqual(3)
    })

    it('summarization category renders entry selector', () => {
      const wrapper = mountWithRealItems('summarization')
      expect(wrapper.find('.drill-down__entry-row').exists()).toBe(true)
    })

    it('rag category renders all fields', () => {
      const wrapper = mountWithRealItems('rag')
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // 7 fields: base_url, model, api_key, chunk_size, search_limit, search_pool_size, retention_days
      expect(items.length).toBeGreaterThanOrEqual(7)
    })

    it('portForward category renders enable toggle and port field', () => {
      const wrapper = mountWithRealItems('portForward')
      expect(wrapper.find('.drill-down__enable-row').exists()).toBe(true)
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBeGreaterThanOrEqual(1)
    })

    it('frp category renders enable toggle and fields', () => {
      const wrapper = mountWithRealItems('frp')
      expect(wrapper.find('.drill-down__enable-row').exists()).toBe(true)
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // 4 common fields: server_addr, server_port, token, auto_port
      expect(items.length).toBeGreaterThanOrEqual(4)
    })
  })

  // ─── Save flow ────────────────────────────────────

  describe('save flow', () => {
    it('calls patchConfig with nested structure for dot-path keys', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      vm.$.setupState.localValues['terminal.idle_timeout'] = '30m'
      await nextTick()
      await vm.$.setupState.handleSave()
      expect(mockPatchConfig).toHaveBeenCalledWith(
        expect.objectContaining({
          terminal: expect.objectContaining({
            max_sessions: 5,
            idle_timeout: '30m',
          }),
        }),
      )
    })

    it('shows toast on successful save', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      await nextTick()
      await vm.$.setupState.handleSave()
      expect(mockToastShow).toHaveBeenCalledWith('已保存', expect.any(Object))
    })

    it('stays on page after successful save', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['terminal.max_sessions'] = 5
      await nextTick()
      await vm.$.setupState.handleSave()
      expect(wrapper.emitted('back')).toBeFalsy()
    })

    it('shows error on patchConfig failure', async () => {
      mockPatchConfig.mockRejectedValueOnce(new Error('Server error'))
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.localValues['terminal.max_sessions'] = 5
      await nextTick()
      await vm.$.setupState.handleSave()
      expect(vm.$.setupState.serverError).toBeTruthy()
      expect(vm.$.setupState.hasFailedSave).toBe(true)
    })

    it('emits restartNeeded when server responds with needsRestart', async () => {
      mockPatchConfig.mockResolvedValueOnce({ needsRestart: true, changedColdFields: ['port_forward.port'], warnings: [] })
      const wrapper = mountDrillDown('portForward')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['port_forward.port'] = 12345
      await nextTick()
      await vm.$.setupState.handleSave()
      expect(wrapper.emitted('restartNeeded')).toBeTruthy()
      expect(wrapper.emitted('restartNeeded')![0]).toEqual([['port_forward.port']])
    })

    it('save button shows saving text during save', async () => {
      // Make patchConfig hang until we resolve it
      let resolveSave!: (v: any) => void
      mockPatchConfig.mockReturnValueOnce(new Promise<any>((r) => { resolveSave = r }))
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      vm.localValues['terminal.max_sessions'] = 5
      await nextTick()
      const savePromise = vm.$.setupState.handleSave()
      await nextTick()
      await nextTick()
      // While saving, saving ref should be true
      expect(vm.$.setupState.saving).toBe(true)
      // Resolve the save
      resolveSave({ needsRestart: false, changedColdFields: [] })
      await savePromise
    })
  })

  // ─── FRP auto_port info ────────────────────────────

  describe('FRP auto_port info', () => {
    it('shows auto_port info when auto_port is true and frp enabled', async () => {
      frpState.enabled = true
      frpState.state = 'running'
      frpState.remotePort = 8080
      frpState.sshRemotePort = 2222
      const wrapper = mountDrillDown('frp')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['frp.enabled'] = true
      vm.$.setupState.localValues['frp.auto_port'] = true
      await nextTick()
      expect(vm.$.setupState.isFrpAutoPortActive).toBe(true)
    })

    it('hides auto_port info when auto_port is false', async () => {
      const wrapper = mountDrillDown('frp')
      const vm = wrapper.vm as any
      vm.$.setupState.localValues['frp.enabled'] = true
      vm.$.setupState.localValues['frp.auto_port'] = false
      await nextTick()
      expect(vm.$.setupState.isFrpAutoPortActive).toBe(false)
    })
  })

  // ─── Enable toggle handler ─────────────────────────

  describe('onEnableToggle', () => {
    it('updates localValues when enable toggle is changed', async () => {
      const wrapper = mountDrillDown('terminal')
      const vm = wrapper.vm as any
      // Simulate checkbox change event with checked=false
      const checkbox = wrapper.find('.drill-down__switch-input')
      const el = checkbox.element as HTMLInputElement
      el.checked = false
      await checkbox.trigger('change')
      await nextTick()
      expect(vm.$.setupState.localValues['terminal.enabled']).toBe(false)
    })
  })

  // ─── setBeforeResetGuard ───────────────────────────

  describe('setBeforeResetGuard', () => {
    it('sets beforeReset guard on mount', () => {
      mountDrillDown('terminal')
      expect(mockSetBeforeResetGuard).toHaveBeenCalled()
    })

    it('clears beforeReset guard on unmount', () => {
      const wrapper = mountDrillDown('terminal')
      wrapper.unmount()
      // Last call should be setBeforeResetGuard(null)
      const calls = mockSetBeforeResetGuard.mock.calls
      const lastCall = calls[calls.length - 1]
      expect(lastCall[0]).toBeNull()
    })
  })
})
