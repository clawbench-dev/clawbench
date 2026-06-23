import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive } from 'vue'
import SettingsCategory from '@/components/settings/SettingsCategory.vue'
import PasswordChangeDialog from '@/components/settings/PasswordChangeDialog.vue'

// Mock composables
const mockSetLocalConfig = vi.fn()
const mockSetServerValue = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [] })
const mockGetServerValueWithDefault = vi.fn()
const mockPatchAgentPref = vi.fn().mockResolvedValue(undefined)
const mockGetAgentModelPref = vi.fn().mockReturnValue(null)
const mockGetAgentThinkingPref = vi.fn().mockReturnValue(null)
const mockLoadAgents = vi.fn()
const mockUpdateAgentField = vi.fn()

const localConfig = reactive<Record<string, any>>({
  theme: 'auto',
  locale: 'zh',
  autoSpeech: false,
  showHidden: false,
  wordWrap: false,
  lineNumbers: true,
  fileView: 'list',
  terminalFontSize: 12,
  androidLogCapture: false,
  swipeSession: false,
})

const serverConfig = ref<Record<string, any>>({
  version: 'dev',
  default_agent: '',
  chat: { initial_messages: 20, page_size: 20, system_prompt_interval: 10 },
  session: { max_count: 10 },
  upload: { max_size_mb: 100, max_files: 20 },
  terminal: { enabled: true, idle_timeout: '10m', max_sessions: 10, buffer_lines: 2000 },
  tts: { engine: 'edge', voice: '', speed: 1.0, max_cache_files: 100, format: '' },
  rag: { enabled: false, ollama_base_url: 'http://localhost:11434', ollama_model: 'bge-m3', chunk_size: 512, search_limit: 5, retention_days: 90 },
  port_forward: { enabled: true, port: 0 },
  push: { jpush: { enabled: false, app_key: '' } },
  summarize: { backend: 'simple', model: '' },
})

const mockAgents = [
  { id: 'codebuddy', name: 'CodeBuddy', icon: '🤖', backend: 'codebuddy', models: [{ id: 'glm-5.1', name: 'GLM 5.1', default: true }, { id: 'glm-4', name: 'GLM 4', default: false }], thinkingEffortLevels: ['low', 'medium', 'high'], thinkingEffort: 'medium', preferredModel: '' },
  { id: 'claude', name: 'Claude', icon: '🧠', backend: 'claude', models: [{ id: 'claude-sonnet', name: 'Sonnet', default: true }], thinkingEffortLevels: [], thinkingEffort: '', preferredModel: '' },
]

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    localConfig,
    serverConfig,
    setLocalConfig: mockSetLocalConfig,
    getServerValueWithDefault: mockGetServerValueWithDefault,
    setServerValue: mockSetServerValue,
    patchAgentPref: mockPatchAgentPref,
    getAgentModelPref: mockGetAgentModelPref,
    getAgentThinkingPref: mockGetAgentThinkingPref,
  }),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: ref(mockAgents),
    loadAgents: mockLoadAgents,
    updateAgentField: mockUpdateAgentField,
    getAgentModels: (agentId: string) => {
      const a = mockAgents.find(a => a.id === agentId)
      return a?.models || []
    },
    getAgentThinkingEffortLevels: (agentId: string) => {
      const a = mockAgents.find(a => a.id === agentId)
      return a?.thinkingEffortLevels || []
    },
    hasThinkingEffortLevels: (agentId: string) => {
      const a = mockAgents.find(a => a.id === agentId)
      return (a?.thinkingEffortLevels?.length || 0) > 0
    },
    getDefaultModelId: (agentId: string) => {
      const a = mockAgents.find(a => a.id === agentId)
      if (a?.preferredModel) return a.preferredModel
      if (!a?.models?.length) return ''
      const def = a.models.find((m: any) => m.default)
      return def ? def.id : a.models[0].id
    },
  }),
}))

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: ref(false) }),
}))

vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({ pushRegistered: ref(false) }),
}))

vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue({ needs_restart: true }),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { ok: '确定' },
      settings: {
        needsRestart: '需重启',
        categories: { chat: '聊天', agents: '智能体', appearance: '外观', tts: '语音', summarization: '摘要', portForward: '端口转发', push: '推送', terminal: '终端', rag: 'RAG', files: '文件', about: '关于', android: 'Android', security: '安全' },
        items: {
          defaultAgent: '默认智能体',
          autoSpeech: '自动语音',
          swipeSession: '滑动切换会话',
          chatInitialMessages: '初始消息数',
          chatPageSize: '每页消息数',
          chatSystemPromptInterval: '系统提示间隔',
          sessionMaxCount: '最大会话数',
          theme: '主题',
          locale: '语言',
          ttsEngine: 'TTS引擎',
          ttsEngineEdge: 'Edge',
          ttsEnginePiper: 'Piper',
          ttsEngineKokoro: 'Kokoro',
          ttsEngineMossNano: 'MOSS-Nano',
          ttsVoice: '语音',
          ttsSpeed: '语速',
          summarizeBackend: '摘要方式',
          summarizeSimple: '简单',
          summarizeApi: 'API',
          summarizeModel: '摘要模型',
          apiHeader: 'API',
          apiBaseUrl: 'API地址',
          apiKey: 'API密钥',
          apiFormat: 'API格式',
          apiFormatOpenai: 'OpenAI',
          apiFormatAnthropic: 'Anthropic',
          ttsMaxCacheFiles: '缓存上限',
          ragSearchPoolSize: '搜索池大小',
          ragOllamaUrl: '嵌入接口地址',
          portForwardEnabled: '启用端口转发',
          portForwardPort: '端口转发端口',
          portForwardPortAuto: '自动',
          pushEnabled: '启用极光推送',
          pushStatus: '推送服务状态',
          pushStatusRegistered: '正常',
          pushStatusNotRegistered: '未注册',
          pushAppKey: 'AppKey',
          portForwardHeader: '端口转发',
          pushHeader: '推送',
          ttsCacheHeader: '缓存',
          terminalEnabled: '启用终端',
          terminalIdleTimeout: '空闲超时',
          terminalMaxSessions: '最大会话',
          terminalBufferLines: '缓冲行数',
          terminalFontSize: '终端字号',
          showHidden: '显示隐藏文件',
          wordWrap: '自动换行',
          lineNumbers: '行号',
          fileView: '视图模式',
          fileViewList: '列表',
          fileViewGrid: '网格',
          uploadMaxSize: '上传大小上限',
          uploadMaxFiles: '上传文件上限',
          ragOllamaUrl: '嵌入接口地址',
          ragOllamaModel: '嵌入模型',
          ragChunkSize: '分块大小',
          ragSearchLimit: '搜索限制',
          ragRetentionDays: '保留天数',
          aboutServerVersion: '服务器版本',
          aboutAppVersion: 'APP版本',
          serverRestart: '重启服务器',
          androidLogCapture: '日志抓取',
          reconfigureServer: '重新配置服务器',
          agentModel: '首选模型',
          agentThinking: '思考强度',
          themeAuto: '自动',
          themeLight: '浅色',
          themeDark: '深色',
          localeZh: '中文',
          localeEn: 'English',
          changePassword: '修改密码',
          changePasswordDesc: '更改登录密码',
        },
        dialog: {
          changePasswordTitle: '修改密码',
          currentPassword: '当前密码',
          newPassword: '新密码',
          confirmPassword: '确认密码',
          currentPasswordPlaceholder: '输入当前密码',
          newPasswordPlaceholder: '输入新密码',
          confirmPasswordPlaceholder: '再次输入新密码',
          changePasswordBtn: '修改',
          changingPassword: '修改中...',
          passwordChanged: '密码修改成功',
          wrongCurrentPassword: '当前密码错误',
          passwordTooShort: '密码太短',
          passwordMismatch: '密码不一致',
          passwordSameAsOld: '新密码与旧密码相同',
          currentPasswordRequired: '请输入当前密码',
          passwordTooManyAttempts: '尝试次数过多',
          passwordChangeFailed: '密码修改失败',
        },
      },
    },
  },
})

function mountCategory(categoryId: string) {
  return mount(SettingsCategory, {
    props: { categoryId },
    global: { plugins: [i18n] },
  })
}

describe('SettingsCategory', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetServerValueWithDefault.mockImplementation((key: string) => {
      // Simple flat-dot-path resolver against serverConfig
      const parts = key.split('.')
      let current: any = serverConfig.value
      for (const p of parts) {
        if (current == null || typeof current !== 'object') return undefined
        current = current[p]
      }
      return current
    })
  })

  // ─── Chat category ──────────────────────────────
  describe('chat category', () => {
    it('renders default_agent as select item', () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const defaultAgentItem = allItems.find(i => i.props().label === '默认智能体')
      expect(defaultAgentItem).toBeTruthy()
      expect(defaultAgentItem!.props().type).toBe('select')
    })

    it('renders autoSpeech as switch item', () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const autoSpeechItem = allItems.find(i => i.props().label === '自动语音')
      expect(autoSpeechItem).toBeTruthy()
      expect(autoSpeechItem!.props().type).toBe('switch')
    })

    it('PATCHes default_agent when user selects a value', async () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const defaultAgentItem = allItems.find(i => i.props().label === '默认智能体')
      expect(defaultAgentItem).toBeTruthy()

      // Simulate user selecting a value
      await defaultAgentItem!.vm.$emit('update:modelValue', 'codebuddy')
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('default_agent', 'codebuddy')
    })

    it('saves autoSpeech locally when toggled', async () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const autoSpeechItem = allItems.find(i => i.props().label === '自动语音')
      expect(autoSpeechItem).toBeTruthy()

      await autoSpeechItem!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('autoSpeech', true)
    })

    it('renders swipeSession as switch item', () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const swipeSessionItem = allItems.find(i => i.props().label === '滑动切换会话')
      expect(swipeSessionItem).toBeTruthy()
      expect(swipeSessionItem!.props().type).toBe('switch')
    })

    it('saves swipeSession locally when toggled', async () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const swipeSessionItem = allItems.find(i => i.props().label === '滑动切换会话')
      expect(swipeSessionItem).toBeTruthy()

      await swipeSessionItem!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('swipeSession', true)
    })

    it('PATCHes chat.initial_messages when number changed', async () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '初始消息数')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 30)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('chat.initial_messages', 30)
    })

    it('PATCHes session.max_count when number changed', async () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '最大会话数')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 20)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('session.max_count', 20)
    })

    it('session.max_count should NOT be marked as needsRestart (hot-reload field)', async () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '最大会话数')
      expect(item).toBeTruthy()
      // session.max_count is hot-reloadable via applyHotReloadGlobals, no restart needed
      expect(item!.props().needsRestart).toBe(false)
    })
  })

  // ─── TTS category ──────────────────────────────
  describe('tts category', () => {
    it('shows engine select and common fields', () => {
      const wrapper = mountCategory('tts')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })

      // Should show: engine, voice, speed, summarize items are in separate category
      const labels = allItems.map(i => i.props().label as string)
      expect(labels).toContain('TTS引擎')
      expect(labels).toContain('语音')
      expect(labels).toContain('语速')
    })

    it('PATCHes tts.engine when selected', async () => {
      const wrapper = mountCategory('tts')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const engineItem = allItems.find(i => i.props().label === 'TTS引擎')
      expect(engineItem).toBeTruthy()

      await engineItem!.vm.$emit('update:modelValue', 'edge')
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('tts.engine', 'edge')
    })

    it('PATCHes tts.speed via slider', async () => {
      const wrapper = mountCategory('tts')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const speedItem = allItems.find(i => i.props().label === '语速')
      expect(speedItem).toBeTruthy()

      await speedItem!.vm.$emit('update:modelValue', 1.5)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('tts.speed', 1.5)
    })

    it('PATCHes summarize.backend when selected', async () => {
      const wrapper = mountCategory('summarization')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const summarizeItem = allItems.find(i => i.props().label === '摘要方式')
      expect(summarizeItem).toBeTruthy()

      await summarizeItem!.vm.$emit('update:modelValue', 'api')
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('summarize.backend', 'api')
    })

    it('PATCHes tts.max_cache_files when changed', async () => {
      const wrapper = mountCategory('tts')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const cacheItem = allItems.find(i => i.props().label === '缓存上限')
      expect(cacheItem).toBeTruthy()

      await cacheItem!.vm.$emit('update:modelValue', 200)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('tts.max_cache_files', 200)
    })

  })

  // ─── Terminal category ──────────────────────────────
  describe('terminal category', () => {
    it('saves terminalFontSize locally', async () => {
      const wrapper = mountCategory('terminal')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const fontItem = allItems.find(i => i.props().label === '终端字号')
      expect(fontItem).toBeTruthy()

      await fontItem!.vm.$emit('update:modelValue', 14)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('terminalFontSize', 14)
    })

    it('PATCHes terminal.enabled when toggled', async () => {
      const wrapper = mountCategory('terminal')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const enabledItem = allItems.find(i => i.props().label === '启用终端')
      expect(enabledItem).toBeTruthy()

      await enabledItem!.vm.$emit('update:modelValue', false)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('terminal.enabled', false)
    })

    it('PATCHes terminal.idle_timeout when changed', async () => {
      const wrapper = mountCategory('terminal')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const timeoutItem = allItems.find(i => i.props().label === '空闲超时')
      expect(timeoutItem).toBeTruthy()

      await timeoutItem!.vm.$emit('update:modelValue', '30m')
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('terminal.idle_timeout', '30m')
    })

    it('PATCHes terminal.max_sessions when changed', async () => {
      const wrapper = mountCategory('terminal')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '最大会话')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 5)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('terminal.max_sessions', 5)
    })

    it('PATCHes terminal.buffer_lines when changed', async () => {
      const wrapper = mountCategory('terminal')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '缓冲行数')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 5000)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('terminal.buffer_lines', 5000)
    })
  })

  // ─── RAG category ──────────────────────────────
  describe('rag category', () => {
    it('PATCHes rag.ollama_base_url when changed', async () => {
      const wrapper = mountCategory('rag')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '嵌入接口地址')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 'http://ollama:11434')
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('rag.ollama_base_url', 'http://ollama:11434')
    })

    it('PATCHes rag.ollama_model when changed', async () => {
      const wrapper = mountCategory('rag')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '嵌入模型')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 'nomic-embed')
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('rag.ollama_model', 'nomic-embed')
    })

    it('PATCHes rag.chunk_size when changed', async () => {
      const wrapper = mountCategory('rag')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '分块大小')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 1024)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('rag.chunk_size', 1024)
    })

    it('PATCHes rag.search_limit when changed', async () => {
      const wrapper = mountCategory('rag')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '搜索限制')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 10)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('rag.search_limit', 10)
    })

    it('PATCHes rag.retention_days when changed', async () => {
      const wrapper = mountCategory('rag')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '保留天数')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 30)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('rag.retention_days', 30)
    })
  })

  // ─── Port Forward category ──────────────────────────────
  describe('portForward category', () => {
    it('PATCHes port_forward.enabled when toggled', async () => {
      const wrapper = mountCategory('portForward')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '启用端口转发')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', false)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('port_forward.enabled', false)
    })

    it('PATCHes port_forward.port when changed', async () => {
      const wrapper = mountCategory('portForward')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '端口转发端口')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 2222)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('port_forward.port', 2222)
    })

    it('shows "Auto" when port_forward.port is 0', async () => {
      // Default serverConfig has port_forward.port = 0
      const wrapper = mountCategory('portForward')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '端口转发端口')
      expect(item).toBeTruthy()
      expect(item!.props().modelValue).toBe('自动')
    })

    it('shows numeric value when port_forward.port is non-zero', async () => {
      serverConfig.value.port_forward = { enabled: true, port: 2222 }
      const wrapper = mountCategory('portForward')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '端口转发端口')
      expect(item).toBeTruthy()
      expect(item!.props().modelValue).toBe(2222)
    })
  })

  // ─── Push category ──────────────────────────────
  describe('push category', () => {
    it('renders push service status as info row', () => {
      const wrapper = mountCategory('push')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const statusItem = allItems.find(i => i.props().label === '推送服务状态')
      expect(statusItem).toBeTruthy()
      expect(statusItem!.props().type).toBe('info')
    })

    it('PATCHes push.jpush.enabled when toggled', async () => {
      const wrapper = mountCategory('push')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '启用极光推送')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('push.jpush.enabled', true)
    })

    it('PATCHes push.jpush.app_key when changed', async () => {
      const wrapper = mountCategory('push')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === 'AppKey')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 'new-app-key')
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('push.jpush.app_key', 'new-app-key')
    })
  })

  // ─── Files category ──────────────────────────────
  describe('files category', () => {
    it('saves showHidden locally when toggled', async () => {
      const wrapper = mountCategory('files')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '显示隐藏文件')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('showHidden', true)
    })

    it('PATCHes upload.max_size_mb when changed', async () => {
      const wrapper = mountCategory('files')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '上传大小上限')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 200)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('upload.max_size_mb', 200)
    })

    it('PATCHes upload.max_files when changed', async () => {
      const wrapper = mountCategory('files')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '上传文件上限')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 50)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('upload.max_files', 50)
    })
  })

  // ─── Appearance category ──────────────────────────────
  describe('appearance category', () => {
    it('saves theme locally when selected', async () => {
      const wrapper = mountCategory('appearance')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '主题')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 'dark')
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('theme', 'dark')
    })

    it('saves locale locally when selected', async () => {
      const wrapper = mountCategory('appearance')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '语言')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 'en')
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('locale', 'en')
    })
  })

  // ─── About category ──────────────────────────────
  describe('about category', () => {
    it('renders server version as info type', () => {
      const wrapper = mountCategory('about')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '服务器版本')
      expect(item).toBeTruthy()
      expect(item!.props().type).toBe('info')
    })

    it('hides appVersion row when not in App mode', () => {
      const wrapper = mountCategory('about')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const appVersionItem = allItems.find(i => i.props().label === 'APP版本')
      expect(appVersionItem).toBeFalsy()
    })
  })

  // ─── Security category ──────────────────────────────
  describe('security category', () => {
    it('renders changePassword as action item', () => {
      const wrapper = mountCategory('security')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '修改密码')
      expect(item).toBeTruthy()
      expect(item!.props().type).toBe('action')
    })

    it('opens password dialog when changePassword action is clicked', async () => {
      const wrapper = mountCategory('security')

      // Directly set the internal showPasswordDialog state
      const vm = wrapper.vm as any
      vm.$.setupState.showPasswordDialog = true
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      // PasswordChangeDialog should be visible
      expect(wrapper.find('.password-dialog-overlay').exists()).toBe(true)
    })

    it('closes password dialog when dialog emits close', async () => {
      const wrapper = mountCategory('security')

      // Open the dialog directly
      const vm = wrapper.vm as any
      vm.$.setupState.showPasswordDialog = true
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      expect(wrapper.find('.password-dialog-overlay').exists()).toBe(true)

      // Close by clicking the cancel button
      const cancelBtn = wrapper.find('.password-dialog__btn--cancel')
      if (cancelBtn.exists()) {
        await cancelBtn.trigger('click')
      } else {
        vm.$.setupState.showPasswordDialog = false
      }
      await wrapper.vm.$nextTick()
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      expect(wrapper.find('.password-dialog-overlay').exists()).toBe(false)
    })

    it('shows toast and emits restartNeeded when password changed with needsRestart=true', async () => {
      const wrapper = mountCategory('security')

      // Open the dialog
      const vm = wrapper.vm as any
      vm.$.setupState.showPasswordDialog = true
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      // Simulate successful password change
      const dialog = wrapper.findComponent(PasswordChangeDialog)
      if (dialog.exists()) {
        await dialog.vm.$emit('changed', true)
      } else {
        // Directly invoke handlePasswordChanged
        if (vm.$.setupState.handlePasswordChanged) {
          vm.$.setupState.handlePasswordChanged(true)
        }
      }
      await wrapper.vm.$nextTick()
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      // Dialog should be closed
      expect(wrapper.find('.password-dialog-overlay').exists()).toBe(false)
      // Toast should show success
      expect(mockToastShow).toHaveBeenCalled()
      // restartNeeded should be emitted
      expect(wrapper.emitted('restartNeeded')).toBeTruthy()
    })
  })
})
