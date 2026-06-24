import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, ref, defineComponent } from 'vue'
import SessionSettingModal from '@/components/chat/SessionSettingModal.vue'
import { useAgents } from '@/composables/useAgents'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { apiPost } from '@/utils/api'
import { patchAgentPref } from '@/composables/useSettingsConfig'

vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: defineComponent({
    props: { open: Boolean, title: String, zIndex: Number, fullHeight: Boolean },
    emits: ['close'],
    inheritAttrs: true,
    template: '<div class="modal-overlay"><div class="modal-dialog"><div class="modal-header"><slot name="header" /></div><div class="modal-body"><slot /></div></div></div>',
  }),
}))

vi.mock('@/composables/useAgents', () => ({ useAgents: vi.fn(), restoreOriginalModels: vi.fn(), populateACPStateCache: vi.fn().mockResolvedValue(undefined), invalidateACPStateCache: vi.fn() }))
vi.mock('@/composables/useSessionIdentity', () => ({ useSessionIdentity: vi.fn(), clearCommandState: vi.fn(), clearThinkingEffortState: vi.fn(), clearModeState: vi.fn() }))
vi.mock('@/utils/api', () => ({ apiPost: vi.fn().mockResolvedValue({ models: [] }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }), createI18n: () => ({ global: { t: (key: string) => key } }) }))
vi.mock('@/composables/useSettingsConfig', () => ({ patchAgentPref: vi.fn().mockResolvedValue(undefined) }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))
vi.mock('@/composables/useLocale', () => ({ gt: (key: string) => key }))

const mockAgents = {
  agents: ref([{ id: 'claude', name: 'Claude', icon: '🤖', backend: 'claude', models: [{ id: 'm1', name: 'Model1', default: true }], thinkingEffortLevels: ['high'], preferredModel: 'm1', preferredThinkingEffort: '', canRefreshModels: true, acpCommand: 'npx foo', transport: 'acp-stdio' }]),
  getAgentModels: vi.fn(() => [{ id: 'm1', name: 'Model1' }]),
  getAgentThinkingEffortLevels: vi.fn(() => ['high']),
  refreshAgentModels: vi.fn().mockResolvedValue(undefined),
  updateAgentField: vi.fn(),
  getDefaultModelId: vi.fn(() => 'm1'),
  getAgent: vi.fn(() => ({ id: 'claude', acpCommand: 'npx foo', transport: 'acp-stdio' })),
  canRefreshModels: vi.fn(() => true),
  supportsDualTransport: vi.fn(() => true),
  getAgentTransport: vi.fn(() => 'acp-stdio'),
}

const mockIdentity = {
  currentAgentId: ref('claude'),
  currentModelId: ref('m1'),
  currentModelName: ref('Model1'),
  currentThinkingEffort: ref('high'),
  currentTransport: ref('acp-stdio'),
  availableThinkingEfforts: ref([]),
  availableModes: ref([{ id: 'code', name: 'Code' }, { id: 'ask', name: 'Ask' }]),
  currentModeId: ref('code'),
}

describe('debug', () => {
  beforeEach(() => {
    vi.mocked(useAgents).mockReturnValue(mockAgents as any)
    vi.mocked(useSessionIdentity).mockReturnValue(mockIdentity as any)
  })

  it('debug - click tab', async () => {
    const wrapper = mount(SessionSettingModal, { props: { show: true, agentId: 'claude' } })
    await flushPromises()
    // Click the transport tab button
    const tabs = wrapper.findAll('.model-tab')
    await tabs[3].trigger('click') // transport tab
    await flushPromises()
    await nextTick()
    console.log('activeTab after click:', wrapper.vm.activeTab)
    const thinkingItems = wrapper.findAll('.thinking-item')
    console.log('thinking-item count:', thinkingItems.length)
    for (const item of thinkingItems) {
      console.log('thinking-item classes:', item.classes(), 'text:', item.text())
    }
  })
})
