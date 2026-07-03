import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, DOMWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import SettingsGroupPanel from '@/components/settings/SettingsGroupPanel.vue'
import { type ConfigGroup } from '@/components/settings/settingsFieldMap'

// ── Mock composables ──────────────────────────────
const mockPatchConfig = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [] })
const mockToastShow = vi.fn()

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({ patchConfig: mockPatchConfig }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// ── i18n ──────────────────────────────
const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          ttsEngine: 'TTS引擎', ttsEngineEdge: 'Edge', ttsEnginePiper: 'Piper', ttsEngineKokoro: 'Kokoro',
          ttsVoice: '语音', ttsSpeed: '语速', ttsMaxCacheFiles: '缓存上限',
          piperModelPath: 'Piper模型路径', piperNoiseScale: '噪声比例',
          ttsPiperHeader: 'Piper设置',
          summarizeBackend: '摘要方式', summarizeDisabled: '禁用', summarizeSimple: '简单', summarizeApi: 'API',
          summarizeModel: '摘要模型',
          apiHeader: 'API', apiBaseUrl: 'API地址', apiKey: 'API密钥',
          portForwardEnabled: '启用端口转发', portForwardPort: '端口',
          ragBaseUrl: '嵌入接口地址', ragModel: '嵌入模型',
          groupSave: '保存', groupSaving: '保存中...', groupCancel: '取消',
          groupNoConfig: '无需配置', groupUnsavedDiscard: '有未保存的更改，确定丢弃？',
          switchOn: '开', switchOff: '关',
        },
        categories: { rag: 'RAG' },
      },
    },
  },
})

// ── Test group definitions ──────────────────────────────
const ttsGroup: ConfigGroup = {
  groupId: 'tts-group', entryType: 'select',
  entryField: {
    labelKey: 'settings.items.ttsEngine', key: 'tts.engine', type: 'select', source: 'server',
    options: [
      { labelKey: 'settings.items.ttsEngineEdge', value: 'edge' },
      { labelKey: 'settings.items.ttsEnginePiper', value: 'piper' },
      { labelKey: 'settings.items.ttsEngineKokoro', value: 'kokoro' },
    ],
  },
  commonFields: [
    { labelKey: 'settings.items.ttsVoice', key: 'tts.voice', type: 'select', source: 'server' },
    { labelKey: 'settings.items.ttsSpeed', key: 'tts.speed', type: 'slider', source: 'server', min: 0.5, max: 3, step: 0.1 },
  ],
  optionSubFields: [
    { when: 'edge', fields: [] },
    { when: 'piper', fields: [
      { labelKey: 'settings.items.piperModelPath', key: 'tts.piper.model_path', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsPiperHeader' },
      { labelKey: 'settings.items.piperNoiseScale', key: 'tts.piper.noise_scale', type: 'number', source: 'server' },
    ]},
  ],
}

const summarizeGroup: ConfigGroup = {
  groupId: 'summarize-group', entryType: 'select',
  entryField: {
    labelKey: 'settings.items.summarizeBackend', key: 'summarize.backend', type: 'select', source: 'server',
    options: [
      { labelKey: 'settings.items.summarizeDisabled', value: '' },
      { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
      { labelKey: 'settings.items.summarizeApi', value: 'api' },
    ],
  },
  commonFields: [
    { labelKey: 'settings.items.summarizeModel', key: 'summarize.model', type: 'text', source: 'server' },
  ],
  optionSubFields: [
    { when: 'simple', fields: [] },
    { when: 'api', fields: [
      { labelKey: 'settings.items.apiBaseUrl', key: 'summarize.api.base_url', type: 'text', source: 'server', sectionHeader: 'settings.items.apiHeader' },
      { labelKey: 'settings.items.apiKey', key: 'summarize.api.key', type: 'password', source: 'server' },
    ]},
  ],
  nonExpandValues: [''],
  commonFieldsVisibleWhen: ['api'],
}

const ragGroup: ConfigGroup = {
  groupId: 'rag-group', titleKey: 'settings.categories.rag', entryType: 'header',
  entryField: { labelKey: 'settings.categories.rag', key: '_rag-header', type: 'header', source: 'server' },
  commonFields: [
    { labelKey: 'settings.items.ragBaseUrl', key: 'rag.base_url', type: 'text', source: 'server' },
    { labelKey: 'settings.items.ragModel', key: 'rag.model', type: 'text', source: 'server' },
  ],
}

const portForwardGroup: ConfigGroup = {
  groupId: 'port-forward-group', entryType: 'switch',
  entryField: { labelKey: 'settings.items.portForwardEnabled', key: 'port_forward.enabled', type: 'switch', source: 'server', needsRestart: true },
  optionSubFields: [
    { when: true, fields: [
      { labelKey: 'settings.items.portForwardPort', key: 'port_forward.port', type: 'number', source: 'server', needsRestart: true },
    ]},
  ],
  nonExpandValues: [false],
}

// ── Mount helper ──────────────────────────────
function mountGroup(
  group: ConfigGroup,
  fieldValues: Record<string, any>,
  extraProps: Record<string, any> = {},
) {
  return mount(SettingsGroupPanel, {
    props: { group, fieldValues, ...extraProps },
    global: {
      plugins: [i18n],
      stubs: { SettingsItem: true, BottomSheet: false },
    },
  })
}

/** Force Vue to re-render after internal state changes (needed for Vue 3.5 + VTU). */
async function flush(wrapper: ReturnType<typeof mount>) {
  await nextTick()
  wrapper.vm.$forceUpdate()
  await nextTick()
}

/** Toggle a checkbox input by setting .checked and dispatching change. */
async function toggleCheckbox(input: DOMWrapper<HTMLInputElement>, checked: boolean) {
  const el = input.element
  el.checked = checked
  el.dispatchEvent(new Event('change'))
}

/** Get internal setup state. */
function getState(wrapper: ReturnType<typeof mount>) {
  return (wrapper.vm as any).$.setupState
}

describe('SettingsGroupPanel (drill-down detail page)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPatchConfig.mockResolvedValue({ needsRestart: false, changedColdFields: [] })
  })

  // ─── 1. Always renders detail content ──────────────────────
  describe('detail page rendering', () => {
    it('renders entry row with label and current value for select group', () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge' })
      // Entry row shows label + current value
      expect(wrapper.find('.settings-group__entry-row').exists()).toBe(true)
      expect(wrapper.find('.settings-group__entry-label').text()).toContain('TTS引擎')
      expect(wrapper.find('.settings-group__entry-value').text()).toContain('Edge')
    })

    it('renders BottomSheet for select group', () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge' })
      // BottomSheet is rendered (closed by default)
      expect(wrapper.findComponent({ name: 'BottomSheet' }).exists()).toBe(true)
    })

    it('opens entry picker BottomSheet on entry row click', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge' })
      await wrapper.find('.settings-group__entry-row').trigger('click')
      await flush(wrapper)
      expect(getState(wrapper).entryPickerOpen).toBe(true)
    })

    it('renders switch toggle for switch group', () => {
      const wrapper = mountGroup(portForwardGroup, { 'port_forward.enabled': true, 'port_forward.port': 0 })
      expect(wrapper.find('.settings-group__switch-input').exists()).toBe(true)
      expect(wrapper.find('.settings-group__switch-input').element.checked).toBe(true)
    })

    it('renders common fields for all groups', () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.voice': '', 'tts.speed': 1.0 })
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBeGreaterThanOrEqual(2) // voice + speed
    })

    it('renders Save and Cancel buttons', () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge' })
      expect(wrapper.find('.settings-group__btn--save').exists()).toBe(true)
      expect(wrapper.find('.settings-group__btn--cancel').exists()).toBe(true)
    })
  })

  // ─── 2. Snapshot on mount ──────────────────────
  describe('snapshot on mount', () => {
    it('creates snapshot of fieldValues on mount', () => {
      const fv = { 'tts.engine': 'edge', 'tts.speed': 1.0 }
      const wrapper = mountGroup(ttsGroup, fv)
      const state = getState(wrapper)
      expect(state.snapshot).toBeTruthy()
      expect(state.snapshot['tts.engine']).toBe('edge')
      expect(state.snapshot['tts.speed']).toBe(1.0)
    })

    it('localValues initialized from fieldValues on mount', () => {
      const fv = { 'tts.engine': 'edge', 'tts.speed': 1.0 }
      const wrapper = mountGroup(ttsGroup, fv)
      const state = getState(wrapper)
      expect(state.localValues['tts.engine']).toBe('edge')
      expect(state.localValues['tts.speed']).toBe(1.0)
    })
  })

  // ─── 3. Cancel behavior ──────────────────────
  describe('cancel', () => {
    it('emits navigate-back on cancel click', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge' })
      await wrapper.find('.settings-group__btn--cancel').trigger('click')
      expect(wrapper.emitted('navigate-back')).toBeTruthy()
      expect(wrapper.emitted('navigate-back')!.length).toBe(1)
    })
  })

  // ─── 4. Save behavior ──────────────────────
  describe('save', () => {
    it('PATCHes only changed fields', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.voice': '', 'tts.speed': 1.0 })
      await flush(wrapper)

      getState(wrapper).setLocalValue('tts.speed', 2.0)
      await nextTick()

      await wrapper.find('.settings-group__btn--save').trigger('click')
      await flush(wrapper)

      expect(mockPatchConfig).toHaveBeenCalledTimes(1)
      const changes = mockPatchConfig.mock.calls[0][0]
      expect(changes.tts.speed).toBe(2.0)
    })

    it('emits save-result and navigate-back on success', async () => {
      mockPatchConfig.mockResolvedValue({ needsRestart: true, changedColdFields: ['tts.speed'] })
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.speed': 1.0 })
      await flush(wrapper)

      getState(wrapper).setLocalValue('tts.speed', 2.0)
      await nextTick()

      await wrapper.find('.settings-group__btn--save').trigger('click')
      await flush(wrapper)

      const results = wrapper.emitted('save-result')!
      expect(results[results.length - 1][0]).toEqual({ needsRestart: true, changedColdFields: ['tts.speed'] })
      expect(wrapper.emitted('navigate-back')).toBeTruthy()
    })

    it('skips empty password fields in diff', async () => {
      const fv = { 'summarize.backend': 'api', 'summarize.model': '', 'summarize.api.base_url': '', 'summarize.api.key': 'old-key' }
      const wrapper = mountGroup(summarizeGroup, fv)
      await flush(wrapper)

      const state = getState(wrapper)
      state.setLocalValue('summarize.api.base_url', 'https://api.example.com')
      state.setLocalValue('summarize.api.key', '')
      await nextTick()

      await wrapper.find('.settings-group__btn--save').trigger('click')
      await flush(wrapper)

      const changes = mockPatchConfig.mock.calls[0][0]
      expect(changes.summarize.api.base_url).toBe('https://api.example.com')
      expect(changes.summarize?.api?.key).toBeUndefined()
    })

    it('emits navigate-back (no PATCH) when no fields changed', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.speed': 1.0 })
      await flush(wrapper)

      await wrapper.find('.settings-group__btn--save').trigger('click')
      await flush(wrapper)

      expect(mockPatchConfig).not.toHaveBeenCalled()
      expect(wrapper.emitted('navigate-back')).toBeTruthy()
    })

    it('shows toast on PATCH failure and stays on page', async () => {
      mockPatchConfig.mockRejectedValueOnce(new Error('network error'))
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.speed': 1.0 })
      await flush(wrapper)

      getState(wrapper).setLocalValue('tts.speed', 2.0)
      await nextTick()

      await wrapper.find('.settings-group__btn--save').trigger('click')
      await flush(wrapper)

      expect(mockToastShow).toHaveBeenCalled()
      // No navigate-back emitted on failure
      expect(wrapper.emitted('navigate-back')).toBeFalsy()
    })
  })

  // ─── 5. Entry selector local preview ──────────────────────
  describe('entry selector local preview', () => {
    it('switches option sub-fields when entry selection changes via state', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.voice': '', 'tts.speed': 1.0 })
      await flush(wrapper)

      // Edge: common fields visible (voice, speed) via SettingsItem stubs
      let items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBeGreaterThanOrEqual(2) // voice + speed

      // Change entry value to Piper via setLocalValue
      getState(wrapper).setLocalValue('tts.engine', 'piper')
      await flush(wrapper)

      // Now Piper fields should be visible (more SettingsItem stubs)
      items = wrapper.findAllComponents({ name: 'SettingsItem' })
      // Common (2) + Piper-specific (2) = 4
      expect(items.length).toBeGreaterThanOrEqual(4)
    })

    it('updates entry display label when value changes', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.voice': '', 'tts.speed': 1.0 })
      await flush(wrapper)
      expect(wrapper.find('.settings-group__entry-value').text()).toContain('Edge')

      getState(wrapper).setLocalValue('tts.engine', 'piper')
      await flush(wrapper)
      expect(wrapper.find('.settings-group__entry-value').text()).toContain('Piper')
    })
  })

  // ─── 7. Dynamic options ──────────────────────
  describe('dynamic options', () => {
    it('renders panel fields when fieldOptions provided', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.voice': '' }, {
        fieldOptions: { 'tts.voice': [{ label: 'Xiaoxiao', value: 'zh-CN-XiaoxiaoNeural' }] },
      })
      await flush(wrapper)

      // SettingsItem stubs should be rendered for common fields
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBeGreaterThanOrEqual(2) // voice + speed
    })
  })

  // ─── 8. Password handling ──────────────────────
  describe('password handling', () => {
    it('skips null password in diff', async () => {
      const fv = { 'summarize.backend': 'api', 'summarize.model': '', 'summarize.api.base_url': '', 'summarize.api.key': 'existing-key' }
      const wrapper = mountGroup(summarizeGroup, fv)
      await flush(wrapper)

      const state = getState(wrapper)
      state.setLocalValue('summarize.api.base_url', 'https://new.url')
      state.setLocalValue('summarize.api.key', null)
      await nextTick()

      await wrapper.find('.settings-group__btn--save').trigger('click')
      await flush(wrapper)

      const changes = mockPatchConfig.mock.calls[0][0]
      expect(changes.summarize.api.base_url).toBe('https://new.url')
      expect(changes.summarize?.api?.key).toBeUndefined()
    })
  })

  // ─── 9. commonFieldsVisibleWhen boundary ──────────────────────
  describe('commonFieldsVisibleWhen', () => {
    it('hides common fields when entry value not in commonFieldsVisibleWhen', async () => {
      const wrapper = mountGroup(summarizeGroup, { 'summarize.backend': 'simple', 'summarize.model': '' })
      await flush(wrapper)

      // 'simple' not in commonFieldsVisibleWhen → model hidden
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBe(0)
    })

    it('shows common fields when entry value is in commonFieldsVisibleWhen', async () => {
      const fv = { 'summarize.backend': 'api', 'summarize.model': '', 'summarize.api.base_url': '', 'summarize.api.key': '' }
      const wrapper = mountGroup(summarizeGroup, fv)
      await flush(wrapper)

      // 'api' is in commonFieldsVisibleWhen → model + base_url + key = 3 items
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBeGreaterThanOrEqual(3)
    })

    it('always shows common fields when commonFieldsVisibleWhen is undefined', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.voice': '', 'tts.speed': 1.0 })
      await flush(wrapper)

      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBeGreaterThanOrEqual(2) // voice + speed
    })
  })

  // ─── 10. RAG flat group ──────────────────────
  describe('RAG flat group', () => {
    it('renders all common fields for header group', async () => {
      const wrapper = mountGroup(ragGroup, { 'rag.base_url': 'http://localhost:11434', 'rag.model': 'bge-m3' })
      await flush(wrapper)

      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      expect(items.length).toBeGreaterThanOrEqual(2) // url + model
    })
  })

  // ─── 11. Section headers ──────────────────────
  describe('section headers', () => {
    it('renders section header before option-specific fields', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'piper', 'tts.voice': '', 'tts.speed': 1.0, 'tts.piper.model_path': '' })
      await flush(wrapper)

      const headers = wrapper.findAll('.settings-group__section-header')
      expect(headers.length).toBeGreaterThanOrEqual(1)
      expect(headers[0].text()).toContain('Piper设置')
    })
  })

  // ─── 12. Save button disabled while saving ──────────────────────
  describe('saving state', () => {
    it('disables save button and shows saving text while saving', async () => {
      let resolvePatch!: (v: any) => void
      mockPatchConfig.mockReturnValue(new Promise(r => { resolvePatch = r }))

      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.speed': 1.0 })
      await flush(wrapper)

      getState(wrapper).setLocalValue('tts.speed', 2.0)
      await nextTick()

      const saveBtn = wrapper.find('.settings-group__btn--save')
      await saveBtn.trigger('click')
      await flush(wrapper)

      expect(saveBtn.attributes('disabled')).toBeDefined()
      expect(saveBtn.text()).toContain('保存中')

      resolvePatch({ needsRestart: false, changedColdFields: [] })
      await flush(wrapper)
    })
  })

  // ─── 14. deepSetByDotPath ──────────────────────
  describe('deepSetByDotPath (via save)', () => {
    it('builds nested object from dot-path keys', async () => {
      const fv = { 'summarize.backend': 'api', 'summarize.model': '', 'summarize.api.base_url': 'old-url', 'summarize.api.key': '' }
      const wrapper = mountGroup(summarizeGroup, fv)
      await flush(wrapper)

      getState(wrapper).setLocalValue('summarize.api.base_url', 'https://new.example.com')
      await nextTick()

      await wrapper.find('.settings-group__btn--save').trigger('click')
      await flush(wrapper)

      expect(mockPatchConfig).toHaveBeenCalledTimes(1)
      expect(mockPatchConfig.mock.calls[0][0].summarize.api.base_url).toBe('https://new.example.com')
    })
  })

  // ─── 15. Port forward group ──────────────────────
  describe('port forward group', () => {
    it('renders switch toggle for port forward group', async () => {
      const wrapper = mountGroup(portForwardGroup, { 'port_forward.enabled': true, 'port_forward.port': 0 })
      await flush(wrapper)
      const panelSwitch = wrapper.find('.settings-group__switch-input')
      expect(panelSwitch.exists()).toBe(true)
      expect(panelSwitch.element.checked).toBe(true)
    })

    it('emits save-result with needsRestart when port changed', async () => {
      mockPatchConfig.mockResolvedValue({ needsRestart: true, changedColdFields: ['port_forward.port'] })
      const wrapper = mountGroup(portForwardGroup, { 'port_forward.enabled': true, 'port_forward.port': 0 })
      await flush(wrapper)

      getState(wrapper).setLocalValue('port_forward.port', 8080)
      await nextTick()

      await wrapper.find('.settings-group__btn--save').trigger('click')
      await flush(wrapper)

      const results = wrapper.emitted('save-result')!
      expect(results[results.length - 1][0]).toEqual({ needsRestart: true, changedColdFields: ['port_forward.port'] })
    })
  })

  // ─── 16. Entry picker select via handleEntrySelect ──────────────────────
  describe('entry picker', () => {
    it('handleEntrySelect updates local entry value and closes picker', async () => {
      const wrapper = mountGroup(ttsGroup, { 'tts.engine': 'edge', 'tts.voice': '', 'tts.speed': 1.0 })
      await flush(wrapper)

      // Open picker
      getState(wrapper).entryPickerOpen = true
      await flush(wrapper)

      // Call handleEntrySelect directly (simulating clicking an option in the BottomSheet)
      getState(wrapper).handleEntrySelect('piper')
      await flush(wrapper)

      expect(getState(wrapper).localValues['tts.engine']).toBe('piper')
      expect(getState(wrapper).entryPickerOpen).toBe(false)
    })
  })
})
